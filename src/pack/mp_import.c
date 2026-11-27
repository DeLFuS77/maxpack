// SPDX-License-Identifier: GPL-3.0-or-later
// A game packed already with zstd packs as a source of its original files (mp_import.h): its loose files,
// the AMPR files and the other tool's runtime left out, and the files of its packs, read decompressed from
// them. The volumes are read through the source, so the packs may be in a folder, an image or a package.
#include "mp_import.h"

#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "mp_complete.h"
#include "mp_idx3.h"
#include "mp_pack.h"
#include "mp_platform.h"
#include "mp_source_impl.h"

#define ZPK_INDEX "ampr_zstd.index"
#define DUMP_INDEX "ampr_emu.index"
#define ZPK_INDEX_MAX (512u << 20)  // what ampr-emu-ng takes (AMPR_EMU_PACK_INDEX_MAX_BYTES)
#define DUMP_INDEX_MAX (512u << 20)
#define APP0_LEN 6u                 // "/app0/" before each path of ampr_emu.index

typedef struct {
    mp_source *base;   // the source as it is
    mp_zpk *z;
    mp_zpk_volsrc vs;
    long *vol;         // the base's entry of each volume, -1 when it has none
    uint32_t nvol;
    long *bi;          // of each entry: its entry in the base, or -1
    long *zi;          // or its file in the packs, or -1
    volatile int failed;   // a block of the packs read was not what the runtime takes, or did not decode
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
    return mp_pack_already_packed(path) || ieq(path, DUMP_INDEX) || ieq(path, "ampr_commands.bin") ||
           ieq(path, "apr_emu.log") || ieq(path, "ampr_emu.log") || ieq(path, MP_RUNTIME_PATH);
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
    return mp_source_read(x->base, (size_t)x->vol[vol], off, buf, n, e);
}

static uint64_t vol_size(void *ctx, uint32_t vol) {
    imp *x = ctx;
    return vol < x->nvol && x->vol[vol] >= 0 ? mp_source_entry(x->base, (size_t)x->vol[vol])->size : 0;
}

// ---- the source -------------------------------------------------------------------------------

static int imp_read(mp_source *s, size_t i, uint64_t off, void *buf, size_t len, mp_err *e) {
    imp *x = s->impl;
    if (x->zi[i] < 0) return mp_source_read(x->base, (size_t)x->bi[i], off, buf, len, e);
    int bad = 0;
    if (mp_zpk_read_file_ex(x->z, (size_t)x->zi[i], off, buf, len, NULL, NULL, &bad, e) != 0) {
        if (bad) x->failed = 1;
        return -1;
    }
    return 0;
}

static void imp_free(imp *x) {
    if (!x) return;
    mp_zpk_close(x->z);
    mp_source_close(x->base);
    free(x->vol);
    free(x->bi);
    free(x->zi);
    free(x);
}

static void imp_close(mp_source *s) { imp_free(s->impl); }

static const char *imp_id(const mp_source *s) { return mp_source_package_id(((const imp *)s->impl)->base); }

mp_zpk *mp_import_zpk(mp_source *s) { return s && s->close == imp_close ? ((imp *)s->impl)->z : NULL; }

int mp_import_failed(mp_source *s) { return s && s->close == imp_close && ((imp *)s->impl)->failed; }

typedef struct {
    mp_entry en;
    long bi, zi;
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

// The packed game base (with its index ix) as the source of its original files; base is taken when it succeeds.
static int open_packed(const char *path, mp_source *base, size_t ix, mp_source **out, mp_import_info *info, mp_err *e) {
    imp *x = calloc(1, sizeof *x);
    mp_source *s = calloc(1, sizeof *s);
    uint8_t *zidx = NULL, *didx = NULL;
    size_t zlen = 0, dlen = 0, nrows = 0, n = 0;
    mp_idx3_entry *rows = NULL;
    uint8_t *used = NULL, *exact = NULL;
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
    x->vol = malloc(MP_ZPK_MAX_VOLUMES * sizeof *x->vol);
    exact = calloc(MP_ZPK_MAX_VOLUMES, 1);
    if (!x->vol || !exact) goto oom;
    for (uint32_t k = 0; k < MP_ZPK_MAX_VOLUMES; k++) x->vol[k] = -1;
    size_t nb = mp_source_count(base);
    // each volume: the file named exactly as the index names it, as the runtime opens it; else one of the same
    // name in another case (a file system that ignores case)
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
    info->level = mp_zpk_level(x->z);
    info->volumes = mp_zpk_volume_count(x->z);
    info->packed_files = mp_zpk_count(x->z);
    for (size_t i = 0; i < info->packed_files; i++) info->packed_bytes += mp_zpk_size(x->z, i);
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
    used = calloc(nrows ? nrows : 1, 1);
    size_t nz = mp_zpk_count(x->z);
    v = calloc(nb + nz + 1, sizeof *v);
    if (!used || !v) goto oom;
    for (size_t i = 0; i < nb; i++) {
        const mp_entry *en = mp_source_entry(base, i);
        if (mp_import_left_out(en->path)) continue;
        long r = mp_idx3_find(rows, nrows, en->path);
        if (r >= 0) used[r] = 1;
        v[n].en = *en;
        v[n].bi = (long)i;
        v[n].zi = -1;
        v[n].row = r;
        n++;
    }
    for (size_t i = 0; i < nz; i++) {
        const char *p = mp_zpk_path(x->z, i);
        long r = mp_idx3_find(rows, nrows, p);
        if (r >= 0) used[r] = 1;
        if (mp_import_left_out(p)) continue;   // inside the packs or loose, the same
        if (r < 0) {
            rc = refuse(info, MP_IMPORT_DAMAGED, e, path, "%s is in its packs but its ampr_emu.index does not list it", p);
            goto done;
        }
        if (rows[r].size != mp_zpk_size(x->z, i)) {
            rc = refuse(info, MP_IMPORT_DAMAGED, e, path, "%s is not of the size its ampr_emu.index says", p);
            goto done;
        }
        v[n].en.path = (char *)p;
        v[n].en.size = mp_zpk_size(x->z, i);
        v[n].en.mtime = rows[r].mtime;
        v[n].bi = -1;
        v[n].zi = (long)i;
        v[n].row = r;
        n++;
    }
    // a path loose and in the packs, or a file and a folder at once
    if (n > 1) qsort(v, n, sizeof *v, cmp_rec);
    for (size_t i = 1; i < n; i++)
        if (!strcmp(v[i - 1].en.path, v[i].en.path)) {
            rc = refuse(info, MP_IMPORT_TWICE, e, path, "%s is both loose and in its packs: keep only one", v[i].en.path);
            goto done;
        }
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
            snprintf(m, sizeof m, "%s is not of the size its ampr_emu.index says (%llu bytes, not %llu): a copy cut short?",
                     v[i].en.path, (unsigned long long)v[i].en.size, (unsigned long long)rows[v[i].row].size);
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
        snprintf(m, sizeof m, "its ampr_emu.index lists %s, which is not there (%zu %s missing)",
                 rows[first].app0_path + APP0_LEN, missing, missing == 1 ? "file" : "files");
        rc = refuse(info, MP_IMPORT_MISSING, e, path, "%s", m);
        goto done;
    }
    s->entries = calloc(n ? n : 1, sizeof *s->entries);
    x->bi = malloc((n ? n : 1) * sizeof *x->bi);
    x->zi = malloc((n ? n : 1) * sizeof *x->zi);
    if (!s->entries || !x->bi || !x->zi) goto oom;
    for (size_t i = 0; i < n; i++) {
        s->entries[i] = v[i].en;
        if (!(s->entries[i].path = mp_strdup(v[i].en.path))) goto oom;
        s->n = i + 1;
        x->bi[i] = v[i].bi;
        x->zi[i] = v[i].zi;
    }
    s->kind = base->kind;
    s->inner = base->inner;
    s->container = base->container;
    s->compressed = base->compressed;
    s->dlc_package = base->dlc_package;
    s->readers = base->readers;   // its readers by its kind: decoding zstd keeps up with any disk
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
    long ix = -1, vol = -1;
    for (size_t i = 0; i < mp_source_count(s); i++) {
        const char *p = mp_source_entry(s, i)->path;
        if (mp_import_lz4(p)) {
            return refuse(info, MP_IMPORT_LZ4, e, path, "%s",
                          "it is packed with LZ4 packs (ampr_assets), which MaxPack does not read yet: give it as "
                          "it was dumped, or packed with zstd packs");
        }
        // the index named exactly so, as the runtime opens it; else one in another case
        if (ieq(p, ZPK_INDEX) && (ix < 0 || !strcmp(p, ZPK_INDEX))) ix = (long)i;
        else if (vol < 0 && mp_pack_already_packed(p)) vol = (long)i;
    }
    if (ix < 0 && vol >= 0) {
        char m[300];
        snprintf(m, sizeof m, "it has %s but no ampr_zstd.index: its packs cannot be read",
                 mp_source_entry(s, (size_t)vol)->path);
        return refuse(info, MP_IMPORT_DAMAGED, e, path, "%s", m);
    }
    if (ix < 0) {
        *out = s;
        return 0;
    }
    mp_source *g = NULL;
    if (open_packed(path, s, (size_t)ix, &g, info, e) != 0) return -1;
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
