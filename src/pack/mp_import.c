// SPDX-License-Identifier: GPL-3.0-or-later
// A game packed already with zstd or LZ4 packs as a source of its original files (mp_import.h): its loose
// files, the AMPR files and the other tool's runtime left out, and the files of its packs, read decompressed
// from them. The volumes are read through the source, so the packs may be in a folder, an image or a package.
#include "mp_import.h"

#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "mp_complete.h"
#include "mp_idx3.h"
#include "mp_lz4pk.h"
#include "mp_pack.h"
#include "mp_platform.h"
#include "mp_source_impl.h"

#define ZPK_INDEX "ampr_zstd.index"
#define DUMP_INDEX "ampr_emu.index"
#define ZPK_INDEX_MAX (512u << 20)  // what ampr-emu-ng takes (AMPR_EMU_PACK_INDEX_MAX_BYTES)
#define DUMP_INDEX_MAX (512u << 20)
#define APP0_LEN 6u                 // "/app0/" before each path of ampr_emu.index
#define KEPT_BUCKETS 4096u
#define KEPT_MAX (512ull << 20)     // stored bytes kept for --keep-packs at most (the read-ahead holds 256 MiB)

// The stored bytes of a compressed block a read decoded, until the writer takes them (mp_import_keep).
typedef struct kept {
    uint64_t chunk;
    uint32_t len;
    struct kept *next;
    uint8_t data[];
} kept;

typedef struct {
    mp_source *base;   // the source as it is
    mp_zpk *z;
    mp_zpk_volsrc vs;
    mp_lz4pk *l;       // or LZ4 packs
    long *vol;         // the base's entry of each volume, -1 when it has none
    uint32_t nvol;
    long *bi;          // of each entry: its entry in the base, or -1
    long *zi;          // or its file in the packs, or -1
    long *li;          // of a file of the packs: its loose copy in the base, compared as it is read, or -1
    volatile int failed;   // a block of the packs read was not what the runtime takes, or did not decode
    volatile int differs;  // or a loose copy was not the packed file
    mp_mutex *mu;          // for what follows
    uint64_t vol_bytes;    // read from the volumes
    int keeping;           // mp_import_keep
    kept **kept;           // KEPT_BUCKETS lists, by chunk record
    uint64_t kept_bytes;
} imp;

static int lower(int c) { return c >= 'A' && c <= 'Z' ? c + 32 : c; }

static int ieq(const char *a, const char *b) {
    for (;; a++, b++) {
        if (lower((unsigned char)*a) != lower((unsigned char)*b)) return 0;
        if (!*a) return 1;
    }
}

static int istarts(const char *s, const char *prefix) {
    for (; *prefix; s++, prefix++)
        if (lower((unsigned char)*s) != lower((unsigned char)*prefix)) return 0;
    return 1;
}

int mp_import_left_out(const char *path) {
    return mp_pack_already_packed(path) || mp_import_lz4(path) || ieq(path, DUMP_INDEX) ||
           ieq(path, "ampr_commands.bin") || ieq(path, "apr_emu.log") || ieq(path, "ampr_emu.log") ||
           ieq(path, MP_RUNTIME_PATH);
}

int mp_import_lz4(const char *path) { return istarts(path, "ampr_assets"); }

// The number of the volume a root file is (ampr_zstd-000.pak, as the index names them, ASCII case aside), or -1;
// *exact, whether it is named exactly so.
static long volume_number(const char *path, int *exact) {
    if (strchr(path, '/') || !istarts(path, "ampr_zstd-")) return -1;
    const char *d = path + 10;
    unsigned long v = 0;
    size_t k = 0;
    for (; d[k] >= '0' && d[k] <= '9' && k < 5; k++) v = v * 10 + (unsigned long)(d[k] - '0');
    if (k == 0 || v >= MP_ZPK_MAX_VOLUMES || !ieq(d + k, ".pak")) return -1;
    char name[32];
    snprintf(name, sizeof name, "ampr_zstd-%03lu.pak", v);
    *exact = !strcmp(name, path);
    return ieq(name, path) ? (long)v : -1;
}

// The entry of that path: the one named exactly so, as the runtime opens it, else one that differs only in
// ASCII case (a file system that ignores case); -1 for none.
static long find_ci(mp_source *s, const char *path) {
    long i = mp_source_find(s, path);
    for (size_t k = 0; i < 0 && k < mp_source_count(s); k++)
        if (ieq(mp_source_entry(s, k)->path, path)) i = (long)k;
    return i;
}

static int read_whole(mp_source *s, size_t i, size_t max, uint8_t **out, size_t *len, mp_err *e) {
    const mp_entry *en = mp_source_entry(s, i);
    *out = NULL;
    *len = 0;
    if (en->size > max) {
        mp_err_set(e, "%s is too large (%llu bytes)", en->path, (unsigned long long)en->size);
        return -1;
    }
    uint8_t *b = malloc((size_t)en->size + 1);
    if (!b) {
        mp_err_set(e, "out of memory");
        return -1;
    }
    if (mp_source_read(s, i, 0, b, (size_t)en->size, e) != 0) {
        free(b);
        return -1;
    }
    *out = b;
    *len = (size_t)en->size;
    return 0;
}

// ---- the volumes, read from the source --------------------------------------------------------

// A read that fails is the source's (a drive gone, an image cut short): not what the packs hold.
static int vol_read(void *ctx, uint32_t vol, uint64_t off, void *buf, size_t n, mp_err *e) {
    imp *x = ctx;
    if (vol >= x->nvol || x->vol[vol] < 0) {
        mp_err_set(e, "volume ampr_zstd-%03u.pak is missing", vol);
        return -1;
    }
    mp_mutex_lock(x->mu);
    x->vol_bytes += n;
    mp_mutex_unlock(x->mu);
    return mp_source_read(x->base, (size_t)x->vol[vol], off, buf, n, e);
}

static int lvol_read(void *ctx, uint32_t vol, uint64_t off, void *buf, size_t n, mp_err *e) {
    imp *x = ctx;
    if (vol >= x->nvol || x->vol[vol] < 0) {
        mp_err_set(e, "volume %u of the LZ4 packs is missing", vol);
        return -1;
    }
    mp_mutex_lock(x->mu);
    x->vol_bytes += n;
    mp_mutex_unlock(x->mu);
    return mp_source_read(x->base, (size_t)x->vol[vol], off, buf, n, e);
}

static uint64_t vol_size(void *ctx, uint32_t vol) {
    imp *x = ctx;
    return vol < x->nvol && x->vol[vol] >= 0 ? mp_source_entry(x->base, (size_t)x->vol[vol])->size : 0;
}

// ---- the source -------------------------------------------------------------------------------

static size_t kept_bucket(uint64_t chunk) { return (size_t)(((chunk ^ (chunk >> 29)) * 0x9E3779B97F4A7C15ull) >> 52); }

// A compressed block just decoded and checked: its stored bytes kept for the writer, while keeping (a block
// stored raw is the bytes read: nothing to keep). Without memory, or past KEPT_MAX, the writer reads it again.
static void imp_seen(void *ctx, uint64_t chunk, const uint8_t *stored, uint32_t len) {
    imp *x = ctx;
    if (!(chunk >> 62)) return;
    kept *k = malloc(sizeof *k + len);
    if (!k) return;
    k->chunk = chunk;
    k->len = len;
    memcpy(k->data, stored, len);
    mp_mutex_lock(x->mu);
    if (!x->keeping || !x->kept || x->kept_bytes + len > KEPT_MAX) {
        mp_mutex_unlock(x->mu);
        free(k);
        return;
    }
    size_t b = kept_bucket(chunk);
    k->next = x->kept[b];
    x->kept[b] = k;
    x->kept_bytes += len;
    mp_mutex_unlock(x->mu);
}

static void kept_free(imp *x) {
    for (size_t b = 0; x->kept && b < KEPT_BUCKETS; b++)
        while (x->kept[b]) {
            kept *k = x->kept[b];
            x->kept[b] = k->next;
            free(k);
        }
    x->kept_bytes = 0;
}

// The loose copy of file i of the packs against what was read of it (buf, len at off): the same bytes, or the
// game has two files of one name.
static int same_as_loose(imp *x, mp_source *s, size_t i, uint64_t off, const uint8_t *buf, size_t len, mp_err *e) {
    size_t piece = len < (1u << 20) ? len : (1u << 20);
    uint8_t *b = malloc(piece ? piece : 1);
    if (!b) {
        mp_err_set(e, "out of memory");
        return -1;
    }
    int rc = 0;
    for (size_t done = 0; !rc && done < len; done += piece) {
        size_t k = len - done < piece ? len - done : piece;
        if (mp_source_read(x->base, (size_t)x->li[i], off + done, b, k, e) != 0) {
            rc = -1;
        } else if (memcmp(b, buf + done, k)) {
            mp_err_set(e, "%s is both loose and in its packs, and the two copies differ: keep only one",
                       s->entries[i].path);
            x->differs = 1;
            x->failed = 1;
            rc = -1;
        }
    }
    free(b);
    return rc;
}

static int imp_read(mp_source *s, size_t i, uint64_t off, void *buf, size_t len, mp_err *e) {
    imp *x = s->impl;
    if (x->zi[i] < 0) return mp_source_read(x->base, (size_t)x->bi[i], off, buf, len, e);
    int bad = 0;
    if (x->l) {
        if (mp_lz4pk_read_file(x->l, (size_t)x->zi[i], off, buf, len, &bad, e) != 0) {
            if (bad) x->failed = 1;
            return -1;
        }
    } else if (mp_zpk_read_file_ex(x->z, (size_t)x->zi[i], off, buf, len, x->keeping ? imp_seen : NULL, x, &bad, e) !=
               0) {   // keeping is set before the readers start and cleared after they end
        if (bad) x->failed = 1;
        return -1;
    }
    return x->li[i] >= 0 ? same_as_loose(x, s, i, off, buf, len, e) : 0;
}

static void imp_free(imp *x) {
    if (!x) return;
    mp_zpk_close(x->z);
    mp_lz4pk_close(x->l);
    mp_source_close(x->base);
    kept_free(x);
    free(x->kept);
    free(x->vol);
    free(x->bi);
    free(x->zi);
    free(x->li);
    mp_mutex_free(x->mu);
    free(x);
}

static void imp_close(mp_source *s) { imp_free(s->impl); }

static const char *imp_id(const mp_source *s) { return mp_source_package_id(((const imp *)s->impl)->base); }

mp_zpk *mp_import_zpk(mp_source *s) { return s && s->close == imp_close ? ((imp *)s->impl)->z : NULL; }

int mp_import_failed(mp_source *s) { return s && s->close == imp_close && ((imp *)s->impl)->failed; }

int mp_import_differs(mp_source *s) { return s && s->close == imp_close && ((imp *)s->impl)->differs; }

uint64_t mp_import_volume_bytes(mp_source *s) {
    if (!s || s->close != imp_close) return 0;
    imp *x = s->impl;
    mp_mutex_lock(x->mu);
    uint64_t n = x->vol_bytes;
    mp_mutex_unlock(x->mu);
    return n;
}

void mp_import_keep(mp_source *s, int on) {
    if (!s || s->close != imp_close) return;
    imp *x = s->impl;
    mp_mutex_lock(x->mu);
    if (on && !x->kept) x->kept = calloc(KEPT_BUCKETS, sizeof *x->kept);   // none: every block is read again
    x->keeping = on && x->kept;
    if (!on) kept_free(x);
    mp_mutex_unlock(x->mu);
}

int mp_import_take(void *ctx, uint64_t chunk, void *buf, size_t cap, uint32_t *len) {
    mp_source *s = ctx;
    if (!s || s->close != imp_close) return 0;
    imp *x = s->impl;
    kept *k = NULL;
    mp_mutex_lock(x->mu);
    if (x->kept) {
        kept **at = &x->kept[kept_bucket(chunk)];
        while (*at && (*at)->chunk != chunk) at = &(*at)->next;
        if ((k = *at)) {
            *at = k->next;
            x->kept_bytes -= k->len;
        }
    }
    mp_mutex_unlock(x->mu);
    if (!k) return 0;
    int got = k->len <= cap;
    if (got) {
        memcpy(buf, k->data, k->len);
        *len = k->len;
    }
    free(k);
    return got;
}

typedef struct {
    mp_entry en;
    long bi, zi;
    long li;    // of a file of the packs: its loose copy, or -1
    long row;   // its row of ampr_emu.index, or -1
} rec;

static int cmp_rec(const void *a, const void *b) { return strcmp(((const rec *)a)->en.path, ((const rec *)b)->en.path); }

// What a row of ampr_emu.index that is not there may be without the game lacking it: what is left out, OS
// junk and what dump tools leave behind (a tool may take those out of the folder and keep its list).
static int may_lack(const char *rel) {
    return mp_import_left_out(rel) || mp_complete_is_junk(rel) || mp_complete_is_leftover(rel);
}

static int refuse(mp_import_info *info, const char *code, mp_err *e, const char *path, const char *fmt, const char *what) {
    char m[400];
    snprintf(m, sizeof m, fmt, what);
    mp_err_set(e, "%s: %s", path, m);
    info->code = code;
    return -1;
}

// A path of the game, folded to ASCII lower case (as the runtime compares them), and where it comes from.
typedef struct {
    char *low;
    const char *path;
    int packed;
} folded;

static int cmp_folded(const void *a, const void *b) { return strcmp(((const folded *)a)->low, ((const folded *)b)->low); }

// A file whose path is a folder of another file too (ASCII case aside): the indexes would hold it as both, and
// no file system can. 0, 1 with *file and *under (the two) and *packed (one of them is in the packs), -1 out of
// memory.
static int file_and_folder(const rec *v, size_t n, const char **file, const char **under, int *packed) {
    folded *f = calloc(n ? n : 1, sizeof *f);
    char *key = NULL;
    int rc = f ? 0 : -1;
    for (size_t i = 0; i < n && !rc; i++) {
        size_t len = strlen(v[i].en.path);
        if (!(f[i].low = malloc(len + 1))) {
            rc = -1;
            break;
        }
        for (size_t k = 0; k <= len; k++) f[i].low[k] = (char)lower((unsigned char)v[i].en.path[k]);
        f[i].path = v[i].en.path;
        f[i].packed = v[i].zi >= 0;
    }
    if (!rc) qsort(f, n, sizeof *f, cmp_folded);
    // what is under "p/", if anything, is where "p/" would sort
    for (size_t i = 0; i < n && !rc; i++) {
        size_t len = strlen(f[i].low);
        char *k2 = realloc(key, len + 2);
        if (!k2) {
            rc = -1;
            break;
        }
        key = k2;
        memcpy(key, f[i].low, len);
        key[len] = '/';
        key[len + 1] = 0;
        size_t lo = 0, hi = n;
        while (lo < hi) {
            size_t mid = lo + (hi - lo) / 2;
            if (strcmp(f[mid].low, key) < 0)
                lo = mid + 1;
            else
                hi = mid;
        }
        if (lo < n && !strncmp(f[lo].low, key, len + 1)) {
            *file = f[i].path;
            *under = f[lo].path;
            *packed = f[i].packed || f[lo].packed;
            rc = 1;
        }
    }
    for (size_t i = 0; f && i < n; i++) free(f[i].low);
    free(f);
    free(key);
    return rc;
}

static int cmp_row(const void *a, const void *b) {
    const char *x = ((const mp_idx3_entry *)a)->app0_path, *y = ((const mp_idx3_entry *)b)->app0_path;
    for (;; x++, y++) {
        int p = lower((unsigned char)*x), q = lower((unsigned char)*y);
        if (p != q || !p) return p - q;
    }
}

// The entry of a name the runtime opens: the one named exactly so, else in another case; -1 for none.
static long find_name(mp_source *s, const char *name) { return find_ci(s, name); }

// LZ4 packs: the manifest (ix), the volumes it names, its CRC sidecar and runtime profile when they are there.
// Its records are the list of the game's files (rows, with their sizes and dates, in folded path order); the
// packed ones are given as files of the packs (v, n) and the base's entries that are volumes are marked (vol).
static int open_lz4(const char *path, imp *x, size_t ix, uint8_t *volume, mp_idx3_entry **rows_out, size_t *nrows_out,
                    mp_import_info *info, mp_err *e) {
    mp_source *base = x->base;
    uint8_t *d = NULL;
    size_t len = 0;
    mp_err ze = {{0}};
    int older = 0, rc = -1;
    if (read_whole(base, ix, ZPK_INDEX_MAX, &d, &len, &ze) != 0)
        return refuse(info, MP_IMPORT_DAMAGED, e, path, "its ampr_assets.index cannot be read: %s", ze.msg);
    if (mp_lz4pk_open(d, len, &x->l, &older, &ze) != 0) {
        free(d);
        return refuse(info, older ? MP_IMPORT_LZ4 : MP_IMPORT_DAMAGED, e, path,
                      older ? "%s" : "its LZ4 packs are damaged: %s", ze.msg);
    }
    free(d);
    d = NULL;
    uint32_t np = mp_lz4pk_pack_count(x->l);
    x->nvol = np;
    if (!(x->vol = malloc((np ? np : 1) * sizeof *x->vol))) goto oom;
    for (uint32_t k = 0; k < np; k++) {
        x->vol[k] = find_name(base, mp_lz4pk_pack_name(x->l, k));
        if (x->vol[k] < 0) {
            char m[300];
            snprintf(m, sizeof m, "volume %s of its LZ4 packs is missing", mp_lz4pk_pack_name(x->l, k));
            return refuse(info, MP_IMPORT_DAMAGED, e, path, "%s", m);
        }
        volume[x->vol[k]] = 1;
    }
    mp_lz4pk_volsrc vs = {lvol_read, vol_size, x};
    if (mp_lz4pk_volumes(x->l, &vs, &ze) != 0)
        return refuse(info, MP_IMPORT_DAMAGED, e, path, "its LZ4 packs are damaged: %s", ze.msg);
    // the sidecars: each one, when it is there, must be of these packs and right
    long ci = find_name(base, MP_LZ4PK_CRC), ri = find_name(base, MP_LZ4PK_RUNTIME);
    if (ci >= 0 && (read_whole(base, (size_t)ci, ZPK_INDEX_MAX, &d, &len, &ze) != 0 || mp_lz4pk_crcs(x->l, d, len, &ze) != 0)) {
        free(d);
        return refuse(info, MP_IMPORT_DAMAGED, e, path, "its LZ4 packs are damaged: %s", ze.msg);
    }
    free(d);
    d = NULL;
    if (ri >= 0 && (read_whole(base, (size_t)ri, 4096, &d, &len, &ze) != 0 || mp_lz4pk_runtime(x->l, d, len, &ze) != 0)) {
        free(d);
        return refuse(info, MP_IMPORT_DAMAGED, e, path, "its LZ4 packs are damaged: %s", ze.msg);
    }
    free(d);
    size_t nf = mp_lz4pk_count(x->l);
    mp_idx3_entry *rows = calloc(nf ? nf : 1, sizeof *rows);
    if (!rows) goto oom;
    for (size_t i = 0; i < nf; i++) {
        const char *p = mp_lz4pk_path(x->l, i);
        size_t pl = strlen(p);
        if (!(rows[i].app0_path = malloc(pl + APP0_LEN + 1))) {
            mp_idx3_entries_free(rows, nf);
            goto oom;
        }
        memcpy(rows[i].app0_path, "/app0/", APP0_LEN);
        memcpy(rows[i].app0_path + APP0_LEN, p, pl + 1);
        rows[i].size = mp_lz4pk_size(x->l, i);
        rows[i].mtime = mp_lz4pk_mtime(x->l, i);
    }
    if (nf > 1) qsort(rows, nf, sizeof *rows, cmp_row);
    for (size_t i = 1; i < nf; i++)
        if (!cmp_row(&rows[i - 1], &rows[i])) {
            char m[300];
            snprintf(m, sizeof m, "%s is twice in its ampr_assets.index", rows[i].app0_path + APP0_LEN);
            mp_idx3_entries_free(rows, nf);
            return refuse(info, MP_IMPORT_DAMAGED, e, path, "%s", m);
        }
    *rows_out = rows;
    *nrows_out = nf;
    info->packed = 1;
    info->packs = "lz4";
    info->volumes = np;
    rc = 0;
    return rc;
oom:
    mp_err_set(e, "out of memory");
    info->code = NULL;
    return -1;
}

// The packed game base (with its index ix, zstd's or LZ4's) as the source of its original files; base is taken
// when it succeeds.
static int open_packed(const char *path, mp_source *base, size_t ix, int lz4, mp_source **out, mp_import_info *info,
                       mp_err *e) {
    imp *x = calloc(1, sizeof *x);
    mp_source *s = calloc(1, sizeof *s);
    uint8_t *zidx = NULL, *didx = NULL;
    size_t zlen = 0, dlen = 0, nrows = 0, n = 0;
    mp_idx3_entry *rows = NULL;
    uint8_t *used = NULL, *exact = NULL, *volume = NULL;
    rec *v = NULL;
    mp_err ze = {{0}};
    int rc = -1;
    if (!x || !s) {
        free(s);
        free(x);
        mp_err_set(e, "out of memory");
        return -1;
    }
    x->base = base;
    x->mu = mp_mutex_new();
    size_t nb = mp_source_count(base), nz = 0;
    volume = calloc(nb ? nb : 1, 1);   // the base's entries that are volumes of LZ4 packs
    if (!x->mu || !volume) goto oom;
    if (lz4) {
        if (open_lz4(path, x, ix, volume, &rows, &nrows, info, e) != 0) goto done;
        nz = mp_lz4pk_count(x->l);
    } else {
        x->vol = malloc(MP_ZPK_MAX_VOLUMES * sizeof *x->vol);
        exact = calloc(MP_ZPK_MAX_VOLUMES, 1);
        if (!x->vol || !exact) goto oom;
        for (uint32_t k = 0; k < MP_ZPK_MAX_VOLUMES; k++) x->vol[k] = -1;
        // each volume: the file named exactly as the index names it, as the runtime opens it; else one of the
        // same name in another case (a file system that ignores case)
        for (size_t i = 0; i < nb; i++) {
            int ex = 0;
            long vn = volume_number(mp_source_entry(base, i)->path, &ex);
            if (vn >= 0 && (x->vol[vn] < 0 || (ex && !exact[vn]))) {
                x->vol[vn] = (long)i;
                exact[vn] = (uint8_t)ex;
            }
        }
        // the packs: the index, then every volume it names, checked as the console checks them
        if (read_whole(base, ix, ZPK_INDEX_MAX, &zidx, &zlen, &ze) != 0) {
            rc = refuse(info, MP_IMPORT_DAMAGED, e, path, "its ampr_zstd.index cannot be read: %s", ze.msg);
            goto done;
        }
        x->nvol = zlen >= MP_ZPK_HEADER ? (uint32_t)(zidx[20] | zidx[21] << 8 | zidx[22] << 16 | (uint32_t)zidx[23] << 24) : 0;
        if (x->nvol > MP_ZPK_MAX_VOLUMES) x->nvol = MP_ZPK_MAX_VOLUMES;   // mp_zpk_open refuses it
        x->vs.read = vol_read;
        x->vs.size = vol_size;
        x->vs.ctx = x;
        if (mp_zpk_open(zidx, zlen, &x->vs, &x->z, &ze) != 0) {
            rc = refuse(info, MP_IMPORT_DAMAGED, e, path, "its packs are damaged: %s", ze.msg);
            goto done;
        }
        info->packed = 1;
        info->packs = "zstd";
        info->level = mp_zpk_level(x->z);
        info->volumes = mp_zpk_volume_count(x->z);
        // ampr_emu.index: the date of every packed file, and the list of the files of the game
        long di = find_ci(base, DUMP_INDEX);
        if (di < 0) {
            rc = refuse(info, MP_IMPORT_DAMAGED, e, path, "it has packs but no %s, which gives the dates of the packed files",
                        DUMP_INDEX);
            goto done;
        }
        if (read_whole(base, (size_t)di, DUMP_INDEX_MAX, &didx, &dlen, &ze) != 0 ||
            mp_idx3_parse(didx, dlen, &rows, &nrows, &ze) != 0) {
            rc = refuse(info, MP_IMPORT_DAMAGED, e, path, "its ampr_emu.index is damaged: %s", ze.msg);
            goto done;
        }
        nz = mp_zpk_count(x->z);
    }
    const char *list = lz4 ? "ampr_assets.index" : "ampr_emu.index";
    used = calloc(nrows ? nrows : 1, 1);
    v = calloc(nb + nz + 1, sizeof *v);
    if (!used || !v) goto oom;
    for (size_t i = 0; i < nb; i++) {
        const mp_entry *en = mp_source_entry(base, i);
        if (mp_import_left_out(en->path) || volume[i]) continue;
        long r = mp_idx3_find(rows, nrows, en->path);
        if (r >= 0) used[r] = 1;
        v[n].en = *en;
        v[n].bi = (long)i;
        v[n].zi = -1;
        v[n].li = -1;
        v[n].row = r;
        n++;
    }
    for (size_t i = 0; i < nz; i++) {
        if (lz4 && !mp_lz4pk_packed(x->l, i)) continue;   // a loose file it lists
        const char *p = lz4 ? mp_lz4pk_path(x->l, i) : mp_zpk_path(x->z, i);
        uint64_t size = lz4 ? mp_lz4pk_size(x->l, i) : mp_zpk_size(x->z, i);
        long r = mp_idx3_find(rows, nrows, p);
        if (r >= 0) used[r] = 1;
        if (mp_import_left_out(p)) continue;   // inside the packs or loose, the same
        char m[300];
        if (r < 0 || rows[r].size != size) {
            snprintf(m, sizeof m, r < 0 ? "%s is in its packs but its %s does not list it" : "%s is not of the size its %s says",
                     p, list);
            rc = refuse(info, MP_IMPORT_DAMAGED, e, path, "%s", m);
            goto done;
        }
        info->packed_files++;   // of the game: what is left out of it is not
        info->packed_bytes += size;
        v[n].en.path = (char *)p;
        v[n].en.size = size;
        v[n].en.mtime = lz4 ? mp_lz4pk_mtime(x->l, i) : rows[r].mtime;
        v[n].bi = -1;
        v[n].zi = (long)i;
        v[n].li = -1;
        v[n].row = r;
        n++;
    }
    // a path loose and in the packs: the packed file kept beside the packs (a game packed in place, which the
    // runtime plays from the packs) is taken once, from the packs, its loose copy compared as it is read; of
    // another size, or twice in the packs, it is refused
    if (n > 1) qsort(v, n, sizeof *v, cmp_rec);
    size_t w = 0;
    for (size_t i = 0; i < n; i++) {
        if (!w || strcmp(v[w - 1].en.path, v[i].en.path)) {
            v[w++] = v[i];
            continue;
        }
        rec *a = &v[w - 1], *b = &v[i], *packed_one = a->zi >= 0 ? a : b, *loose = a->zi >= 0 ? b : a;
        char m[300];
        if ((a->zi >= 0) == (b->zi >= 0)) {
            rc = refuse(info, MP_IMPORT_TWICE, e, path, "%s is twice in its packs: keep only one", a->en.path);
            goto done;
        }
        if (loose->en.size != packed_one->en.size) {
            snprintf(m, sizeof m, "%s is both loose and in its packs, and the two copies differ (%llu and %llu bytes): keep only one",
                     a->en.path, (unsigned long long)loose->en.size, (unsigned long long)packed_one->en.size);
            rc = refuse(info, MP_IMPORT_TWICE, e, path, "%s", m);
            goto done;
        }
        packed_one->li = loose->bi;
        *a = *packed_one;
    }
    n = w;
    const char *file = NULL, *under = NULL;
    int packed = 0, ff = file_and_folder(v, n, &file, &under, &packed);
    if (ff < 0) goto oom;
    if (ff) {
        char m[300];
        snprintf(m, sizeof m, "%s is both a file and the folder of %s: keep only one", file, under);
        rc = refuse(info, packed ? MP_IMPORT_TWICE : MP_IMPORT_DAMAGED, e, path, "%s", m);
        goto done;
    }
    // each loose file it lists of the size it says (the runtime serves that one): else a copy cut short
    for (size_t i = 0; i < n; i++)
        if (v[i].zi < 0 && v[i].row >= 0 && rows[v[i].row].size != v[i].en.size && !may_lack(v[i].en.path)) {
            char m[300];
            snprintf(m, sizeof m, "%s is not of the size its %s says (%llu bytes, not %llu): a copy cut short?",
                     v[i].en.path, list, (unsigned long long)v[i].en.size, (unsigned long long)rows[v[i].row].size);
            rc = refuse(info, MP_IMPORT_MISSING, e, path, "%s", m);
            goto done;
        }
    size_t missing = 0, first = 0;
    for (size_t r = 0; r < nrows; r++) {
        if (used[r] || may_lack(rows[r].app0_path + APP0_LEN)) continue;
        if (!missing++) first = r;
    }
    if (missing) {
        char m[300];
        snprintf(m, sizeof m, "its %s lists %s, which is not there (%zu %s missing)", list,
                 rows[first].app0_path + APP0_LEN, missing, missing == 1 ? "file" : "files");
        rc = refuse(info, MP_IMPORT_MISSING, e, path, "%s", m);
        goto done;
    }
    s->entries = calloc(n ? n : 1, sizeof *s->entries);
    x->bi = malloc((n ? n : 1) * sizeof *x->bi);
    x->zi = malloc((n ? n : 1) * sizeof *x->zi);
    x->li = malloc((n ? n : 1) * sizeof *x->li);
    if (!s->entries || !x->bi || !x->zi || !x->li) goto oom;
    for (size_t i = 0; i < n; i++) {
        s->entries[i] = v[i].en;
        if (!(s->entries[i].path = mp_strdup(v[i].en.path))) goto oom;
        s->n = i + 1;
        x->bi[i] = v[i].bi;
        x->zi[i] = v[i].zi;
        x->li[i] = v[i].li;
    }
    s->kind = base->kind;
    s->inner = base->inner;
    s->container = base->container;
    s->compressed = base->compressed;
    s->dlc_package = base->dlc_package;
    s->readers = base->readers;   // its readers by its kind: decoding keeps up with any disk
    s->package_id = base->package_id ? imp_id : NULL;
    s->ndirs = base->ndirs;
    s->read = imp_read;
    s->close = imp_close;
    s->impl = x;
    *out = s;
    x = NULL;
    s = NULL;
    rc = 0;
    goto done;
oom:
    mp_err_set(e, "out of memory");
    info->code = NULL;
done:
    if (s) {
        for (size_t i = 0; i < s->n; i++) free(s->entries[i].path);
        free(s->entries);
        free(s);
    }
    if (x) x->base = NULL;   // the caller's still
    imp_free(x);
    free(v);
    free(used);
    free(exact);
    free(volume);
    mp_idx3_entries_free(rows, nrows);
    free(didx);
    free(zidx);
    return rc;
}

int mp_import_open(const char *path, int what, mp_source **out, mp_import_info *info, mp_err *e) {
    *out = NULL;
    memset(info, 0, sizeof *info);
    mp_source *s = NULL;
    if (mp_source_open_as(path, what, &s, e) != 0) return -1;
    if (mp_import_source(path, s, what, out, info, e) != 0) {
        mp_source_close(s);
        return -1;
    }
    return 0;
}

int mp_import_source(const char *path, mp_source *s, int what, mp_source **out, mp_import_info *info, mp_err *e) {
    *out = NULL;
    memset(info, 0, sizeof *info);
    long ix = -1, vol = -1, lx = -1, lz4 = -1;
    for (size_t i = 0; i < mp_source_count(s); i++) {
        const char *p = mp_source_entry(s, i)->path;
        // the indexes named exactly so, as the runtime opens them; else one in another case
        if (ieq(p, MP_LZ4PK_INDEX)) {
            if (lx < 0 || !strcmp(p, MP_LZ4PK_INDEX)) lx = (long)i;
        } else if (mp_import_lz4(p)) {
            if (lz4 < 0) lz4 = (long)i;
        } else if (ieq(p, ZPK_INDEX)) {
            if (ix < 0 || !strcmp(p, ZPK_INDEX)) ix = (long)i;
        } else if (vol < 0 && mp_pack_already_packed(p)) {
            vol = (long)i;
        }
    }
    if ((lx >= 0 || lz4 >= 0) && (ix >= 0 || vol >= 0))
        return refuse(info, MP_IMPORT_LZ4, e, path, "%s",
                      "it has zstd and LZ4 packs at once (the console's runtime reads only the zstd ones): give it as it "
                      "was dumped");
    if ((ix < 0 && vol >= 0) || (lx < 0 && lz4 >= 0)) {
        char m[300];
        snprintf(m, sizeof m, "it has %s but no %s: its packs cannot be read", mp_source_entry(s, (size_t)(vol >= 0 ? vol : lz4))->path,
                 vol >= 0 ? ZPK_INDEX : MP_LZ4PK_INDEX);
        return refuse(info, MP_IMPORT_DAMAGED, e, path, "%s", m);
    }
    if (ix < 0 && lx < 0) {
        *out = s;
        return 0;
    }
    mp_source *g = NULL;
    if (open_packed(path, s, (size_t)(lx >= 0 ? lx : ix), lx >= 0, &g, info, e) != 0) return -1;
    // a game has eboot.bin, loose or packed; without it, it is a DLC (pack --dlc refuses those packed already)
    if (what == MP_OPEN_GAME && find_ci(g, "eboot.bin") < 0) {
        ((imp *)g->impl)->base = NULL;   // s stays the caller's
        mp_source_close(g);
        return refuse(info, MP_IMPORT_DLC, e, path, "%s",
                      "it has no eboot.bin: it is a DLC packed already, which MaxPack does not take yet: give it as it "
                      "was dumped");
    }
    *out = g;
    return 0;
}
