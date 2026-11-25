// SPDX-License-Identifier: GPL-3.0-or-later
// The PS5 debug package reader (make_fpkg.py's packages): the basic game as a stored, a Kraken and a
// mixed-Kraken package lists and reads as basic.manifest.tsv says, in pieces of every size and from
// several threads; the passcode (the default, another one, a wrong one); what is refused (retail,
// patch, additional content, other content, PS4, not a package) and every damaged or hostile package
// with its message; a package a builder made of a game folder holds the folder's files and exactly the
// differences game.differ.tsv lists, with the builder's dates; and deterministic mutations (bytes
// flipped in the header, the CNT, the outer PFS's metadata, the map and the inner PFS, packages cut
// short) end in a clean error or a read, never a crash or a hang.
#include <stdlib.h>
#include <zlib.h>

#include "mp_fixture.h"
#include "mp_hash.h"
#include "mp_platform.h"
#include "mp_source.h"
#include "mp_source_impl.h"
#include "mp_test.h"

#define FX MP_FIXTURES_DIR
#define PASSCODE "MaxPackTestPasscode-0123456789ab"
#define BUILD_TIME 1730000000

typedef struct {
    uint64_t size;
    unsigned long crc;
    int64_t mtime;
    char *path;
} man_line;

static man_line *g_man;
static size_t g_nman;
static char *g_manbuf;

static char *slurp(const char *path, size_t *len) {
    size_t n = 0;
    uint8_t *b = mpt_slurp(path, &n);
    if (!b) return NULL;
    b[n] = 0;
    *len = n;
    return (char *)b;
}

static void load_manifest(void) {
    if (g_man) return;
    size_t len = 0;
    g_manbuf = slurp(FX "/basic.manifest.tsv", &len);
    g_man = calloc(1024, sizeof *g_man);
    for (char *line = g_manbuf; line && *line;) {
        char *nl = strchr(line, '\n');
        if (nl) *nl = 0;
        char *t1 = strchr(line, '\t'), *t2 = t1 ? strchr(t1 + 1, '\t') : NULL, *t3 = t2 ? strchr(t2 + 1, '\t') : NULL;
        if (t3) {
            g_man[g_nman].size = strtoull(line, NULL, 10);
            g_man[g_nman].crc = strtoul(t1 + 1, NULL, 16);
            g_man[g_nman].mtime = strtoll(t2 + 1, NULL, 10);
            g_man[g_nman].path = t3 + 1;
            g_nman++;
        }
        line = nl ? nl + 1 : NULL;
    }
}

static mp_source *open_fx(const char *name, const char *passcode, mp_err *e) {
    char p[1024];
    snprintf(p, sizeof p, FX "/%s", name);
    mp_source *s = NULL;
    if (mp_source_open_passcode(p, passcode, &s, e) != 0) return NULL;
    return s;
}

// CRC-32 of the whole file i, read in pieces of `piece` bytes; -1 on a read error.
static long long crc_of(mp_source *s, size_t i, size_t piece, mp_err *e) {
    uint64_t size = mp_source_entry(s, i)->size, off = 0;
    uint8_t *buf = malloc(piece);
    uLong crc = crc32(0, Z_NULL, 0);
    while (buf && off < size) {
        size_t k = size - off > piece ? piece : (size_t)(size - off);
        if (mp_source_read(s, i, off, buf, k, e) != 0) {
            free(buf);
            return -1;
        }
        crc = crc32(crc, buf, (uInt)k);
        off += k;
    }
    free(buf);
    return (long long)crc;
}

static const char *forms[] = {"basic.pkg", "basic_k.pkg", "basic_x.pkg"};

MP_TEST(fpkg_like_manifest) {
    load_manifest();
    for (size_t f = 0; f < sizeof forms / sizeof forms[0]; f++) {
        mp_err e = {{0}};
        mp_source *s = open_fx(forms[f], NULL, &e);
        if (!s) {
            MP_FAIL_("%s does not open: %s", forms[f], e.msg);
            continue;
        }
        MP_CHECK_EQ_U64(mp_source_count(s), g_nman);
        MP_CHECK_EQ_U64(mp_source_dir_count(s), 7);
        size_t bad = 0;
        for (size_t i = 0; i < g_nman && i < mp_source_count(s); i++) {
            const mp_entry *en = mp_source_entry(s, i);
            // pieces of 4 MiB like the read-ahead, and of 1000 bytes that cross every block edge
            long long a = crc_of(s, i, 4u << 20, &e), b = crc_of(s, i, 1000, &e);
            if (strcmp(en->path, g_man[i].path) || en->size != g_man[i].size || en->mtime != g_man[i].mtime ||
                a != (long long)g_man[i].crc || b != a) {
                if (bad++ < 3) MP_FAIL_("%s: %s differs (%s)", forms[f], g_man[i].path, e.msg);
            }
        }
        MP_CHECK_EQ_U64(bad, 0);
        mp_source_close(s);
    }
}

MP_TEST(fpkg_kinds) {
    const char *names[] = {"basic.pkg", "basic_k.pkg"};
    for (int k = 0; k < 2; k++) {
        mp_err e = {{0}};
        mp_source *s = open_fx(names[k], NULL, &e);
        if (!s) {
            MP_FAIL_("%s: %s", names[k], e.msg);
            continue;
        }
        char t[96];
        MP_CHECK(mp_source_kind_of(s) == MP_SRC_FPKG);
        MP_CHECK_STR(mp_source_kind_name(s), "fpkg");
        MP_CHECK_STR(mp_source_inner_name(s), "");
        MP_CHECK(mp_source_compressed(s) == k);
        MP_CHECK_STR(mp_source_kind_text(s, t, sizeof t), k ? "PS5 package (FPKG), compressed" : "PS5 package (FPKG)");
        MP_CHECK(mp_source_readers(s) == 16);
        mp_source_close(s);
    }
}

// Ranges of any offset and length read the same bytes as the same file of the exFAT image.
MP_TEST(fpkg_ranges) {
    mp_err e = {{0}};
    mp_source *a = NULL, *b = open_fx("basic_k.pkg", NULL, &e);
    MP_CHECK(mp_source_open(FX "/basic.exfat", &a, &e) == 0);
    if (!a || !b) {
        MP_FAIL_("%s", e.msg);
        mp_source_close(a);
        mp_source_close(b);
        return;
    }
    uint8_t *x = malloc(1u << 20), *y = malloc(1u << 20);
    uint32_t r = 7;
    size_t bad = 0;
    for (int k = 0; k < 3000; k++) {
        r = r * 1103515245u + 12345u;
        size_t i = (r >> 8) % mp_source_count(a);
        uint64_t size = mp_source_entry(a, i)->size;
        if (!size) continue;
        r = r * 1103515245u + 12345u;
        uint64_t off = (r >> 4) % size;
        r = r * 1103515245u + 12345u;
        size_t len = (size_t)((r >> 4) % (size - off < (1u << 20) ? size - off : (1u << 20))) + 1;
        if (off + len > size) len = (size_t)(size - off);
        if (mp_source_read(a, i, off, x, len, &e) != 0 || mp_source_read(b, i, off, y, len, &e) != 0 ||
            memcmp(x, y, len) != 0)
            bad++;
    }
    MP_CHECK_EQ_U64(bad, 0);
    free(x);
    free(y);
    mp_source_close(a);
    mp_source_close(b);
}

MP_TEST(fpkg_passcode) {
    mp_err e = {{0}};
    mp_source *s = open_fx("pass.pkg", NULL, &e);
    MP_CHECK(s == NULL);
    MP_CHECK_CONTAINS(e.msg, "this package was made with another passcode; give it with --passcode");
    s = open_fx("pass.pkg", "WrongPasscode-0123456789abcdefgh", &e);
    MP_CHECK(s == NULL);
    MP_CHECK_CONTAINS(e.msg, "another passcode than the one given");
    s = open_fx("pass.pkg", PASSCODE, &e);
    MP_CHECK(s != NULL);
    if (s) {
        MP_CHECK(mp_source_find(s, "data/a.bin") >= 0);
        MP_CHECK(crc_of(s, (size_t)mp_source_find(s, "data/a.bin"), 65536, &e) >= 0);
        mp_source_close(s);
    }
    // the passcode of the process: wrong ones refused, then used by mp_source_open, then back to the default
    MP_CHECK(mp_source_set_passcode("short") == -1);
    MP_CHECK(mp_source_set_passcode("MaxPackTestPasscode 0123456789ab") == -1);   // a space
    MP_CHECK(mp_source_set_passcode(PASSCODE) == 0);
    MP_CHECK(mp_source_open(FX "/pass.pkg", &s, &e) == 0);
    mp_source_close(s);
    MP_CHECK(mp_source_open(FX "/basic.pkg", &s, &e) == -1);   // made with the default
    MP_CHECK_CONTAINS(e.msg, "than the one given");
    MP_CHECK(mp_source_set_passcode(NULL) == 0);
    MP_CHECK(mp_source_open(FX "/basic.pkg", &s, &e) == 0);
    mp_source_close(s);
    // 32 '0' given is the default
    MP_CHECK(mp_source_open_passcode(FX "/basic.pkg", "00000000000000000000000000000000", &s, &e) == 0);
    mp_source_close(s);
}

MP_TEST(fpkg_refused) {
    const char *cases[][2] = {
        {"retail.pkg", "retail packages need keys this tool does not have"},
        {"patch.pkg", "a patch (update) package"},
        {"dlc.pkg", "additional content (DLC)"},
        {"dlc_data.pkg", "with --dlc"},
        {"other.pkg", "content type 0x1A"},
        {"ps4.pkg", "MaxPack is for PS5 games"},
        {"notpkg.pkg", "not a PS5 package"},
    };
    for (size_t i = 0; i < sizeof cases / sizeof cases[0]; i++) {
        mp_err e = {{0}};
        mp_source *s = open_fx(cases[i][0], NULL, &e);
        MP_CHECK(s == NULL);
        mp_source_close(s);
        if (!strstr(e.msg, cases[i][1])) MP_FAIL_("%s: \"%s\" does not say \"%s\"", cases[i][0], e.msg, cases[i][1]);
    }
}

MP_TEST(fpkg_bad) {
    size_t len = 0;
    char *tsv = slurp(FX "/bad_fpkg.tsv", &len);
    MP_CHECK(tsv != NULL);
    size_t n = 0;
    for (char *line = tsv; line && *line;) {
        char *nl = strchr(line, '\n');
        if (nl) *nl = 0;
        char *tab = strchr(line, '\t');
        if (tab) {
            *tab = 0;
            mp_err e = {{0}};
            mp_source *s = open_fx(line, NULL, &e);
            if (s) {
                MP_FAIL_("%s opens", line);
                mp_source_close(s);
            } else if (!strstr(e.msg, tab + 1)) {
                MP_FAIL_("%s: \"%s\" does not say \"%s\"", line, e.msg, tab + 1);
            }
            n++;
        }
        line = nl ? nl + 1 : NULL;
    }
    MP_CHECK(n >= 20);
    free(tsv);
}

// A damaged Kraken block: the package opens, the file it is in does not read, the others do.
MP_TEST(fpkg_damaged_block) {
    mp_err e = {{0}};
    mp_source *s = open_fx("kraken.bad.pkg", NULL, &e);
    if (!s) {
        MP_FAIL_("%s", e.msg);
        return;
    }
    size_t failed = 0;
    for (size_t i = 0; i < mp_source_count(s); i++)
        if (crc_of(s, i, 65536, &e) < 0) {
            failed++;
            MP_CHECK_STR(mp_source_entry(s, i)->path, "data/a.bin");
            MP_CHECK_CONTAINS(e.msg, "does not decode");
        }
    MP_CHECK_EQ_U64(failed, 1);
    mp_source_close(s);
}

// Stored without encryption (the "PPRPLAIN-NOAUTH!" seed), compressed, and the same game as a folder.
MP_TEST(fpkg_plain_and_mini) {
    const char *names[] = {"plain.pkg", "mini.pkg", "mini_k.pkg"};
    mp_err e = {{0}};
    mp_source *dir = NULL;
    MP_CHECK(mp_source_open(FX "/mini", &dir, &e) == 0);
    for (int k = 0; dir && k < 3; k++) {
        mp_source *s = open_fx(names[k], NULL, &e);
        if (!s) {
            MP_FAIL_("%s: %s", names[k], e.msg);
            continue;
        }
        MP_CHECK_EQ_U64(mp_source_count(s), mp_source_count(dir));
        for (size_t i = 0; i < mp_source_count(dir) && i < mp_source_count(s); i++) {
            MP_CHECK_STR(mp_source_entry(s, i)->path, mp_source_entry(dir, i)->path);
            MP_CHECK(crc_of(s, i, 1u << 20, &e) == crc_of(dir, i, 1u << 20, &e));
        }
        mp_source_close(s);
    }
    mp_source_close(dir);
}

// SHA-256 of a whole file, as hex.
static int sha_of(mp_source *s, size_t i, char hex[65], mp_err *e) {
    uint64_t size = mp_source_entry(s, i)->size, off = 0;
    uint8_t *buf = malloc(1u << 20), d[32];
    mp_sha256 h;
    mp_sha256_init(&h);
    while (buf && off < size) {
        size_t k = size - off > (1u << 20) ? 1u << 20 : (size_t)(size - off);
        if (mp_source_read(s, i, off, buf, k, e) != 0) {
            free(buf);
            return -1;
        }
        mp_sha256_update(&h, buf, k);
        off += k;
    }
    free(buf);
    mp_sha256_final(&h, d);
    mp_sha256_hex(d, hex);
    return 0;
}

// A builder's package of game/: every file of the folder with its bytes, plus or changed exactly the
// files game.differ.tsv lists, every date the builder's.
MP_TEST(fpkg_same_game) {
    mp_err e = {{0}};
    mp_source *dir = NULL, *pkg = open_fx("game.pkg", NULL, &e);
    MP_CHECK(mp_source_open(FX "/game", &dir, &e) == 0);
    size_t len = 0;
    char *tsv = slurp(FX "/game.differ.tsv", &len);
    if (!dir || !pkg || !tsv) {
        MP_FAIL_("game, game.pkg or game.differ.tsv: %s", e.msg);
        mp_source_close(dir);
        mp_source_close(pkg);
        free(tsv);
        return;
    }
    char added[4096] = "", changed[4096] = "";
    size_t na = 0, nc = 0;
    for (size_t i = 0; i < mp_source_count(pkg); i++) {
        const mp_entry *en = mp_source_entry(pkg, i);
        MP_CHECK_EQ_U64(en->mtime, BUILD_TIME);
        long j = mp_source_find(dir, en->path);
        if (j < 0) {
            snprintf(added + strlen(added), sizeof added - strlen(added), "added\t%s\n", en->path);
            na++;
            continue;
        }
        char a[65], b[65];
        MP_CHECK(sha_of(pkg, i, a, &e) == 0 && sha_of(dir, (size_t)j, b, &e) == 0);
        if (strcmp(a, b) || en->size != mp_source_entry(dir, (size_t)j)->size) {
            snprintf(changed + strlen(changed), sizeof changed - strlen(changed), "changed\t%s\n", en->path);
            nc++;
        }
    }
    for (size_t j = 0; j < mp_source_count(dir); j++)   // nothing of the folder is left out
        if (mp_source_find(pkg, mp_source_entry(dir, j)->path) < 0)
            MP_FAIL_("%s is not in the package", mp_source_entry(dir, j)->path);
    // every line of the list is one of ours, and the other way round
    size_t listed = 0;
    for (char *line = tsv; line && *line;) {
        char *nl = strchr(line, '\n');
        if (nl) *nl = 0;
        if (line[0] && line[0] != '#') {
            char want[600];
            snprintf(want, sizeof want, "%s\n", line);
            if (!strstr(!strncmp(line, "added", 5) ? added : changed, want)) MP_FAIL_("not seen: %s", line);
            listed++;
        }
        line = nl ? nl + 1 : NULL;
    }
    MP_CHECK_EQ_U64(listed, na + nc);
    MP_CHECK(nc >= 1 && na >= 5);
    MP_CHECK_EQ_U64(mp_source_dir_count(pkg), mp_source_dir_count(dir) + 0);   // about/ and changeinfo/ were already there
    free(tsv);
    mp_source_close(dir);
    mp_source_close(pkg);
}

// Several threads read the files of a Kraken package at once (its block cache is shared).
typedef struct {
    mp_source *s;
    unsigned seed;
    int bad;
} par_arg;

static void par_reader(void *arg) {
    par_arg *a = arg;
    mp_err e = {{0}};
    for (size_t k = 0; k < g_nman; k++) {
        size_t i = (k * 7 + a->seed) % g_nman;
        if (crc_of(a->s, i, 1000 + 4096 * a->seed, &e) != (long long)g_man[i].crc) a->bad++;
    }
}

MP_TEST(fpkg_parallel_reads) {
    load_manifest();
    const char *names[] = {"basic_k.pkg", "basic.pkg"};
    for (int f = 0; f < 2; f++) {
        mp_err e = {{0}};
        mp_source *s = open_fx(names[f], NULL, &e);
        if (!s) {
            MP_FAIL_("%s", e.msg);
            continue;
        }
        par_arg a[8];
        mp_thread *t[8];
        for (unsigned k = 0; k < 8; k++) {
            a[k].s = s;
            a[k].seed = k;
            a[k].bad = 0;
            MP_CHECK(mp_thread_start(&t[k], par_reader, &a[k]) == 0);
        }
        int bad = 0;
        for (unsigned k = 0; k < 8; k++) {
            mp_thread_join(t[k]);
            bad += a[k].bad;
        }
        MP_CHECK_EQ_U64(bad, 0);
        mp_source_close(s);
    }
}

// ---- mutations -----------------------------------------------------------------------------

typedef struct {
    const uint8_t *p;
} mem_impl;

static int mem_read(mp_image *m, void *buf, size_t len, uint64_t off, mp_err *e) {
    (void)e;
    memcpy(buf, ((mem_impl *)m->impl)->p + off, len);
    return 0;
}
static void mem_close(mp_image *m) { free(m->impl); }

static mp_image *mem_image(const uint8_t *p, size_t n) {
    mp_image *m = calloc(1, sizeof *m);
    mem_impl *x = calloc(1, sizeof *x);
    x->p = p;
    m->size = n;
    m->name = mp_strdup("(memory)");
    m->read = mem_read;
    m->close = mem_close;
    m->impl = x;
    return m;
}

static uint32_t rng = 88172645u;
static uint32_t rnd(void) {
    rng ^= rng << 13;
    rng ^= rng >> 17;
    rng ^= rng << 5;
    return rng;
}

// Opens the package in memory and reads every file whole: either a clean error (a message) or all read.
static int try_pkg(const uint8_t *p, size_t n, unsigned *opened) {
    mp_err e = {{0}};
    mp_source *s = NULL;
    if (mp_source_open_image(mem_image(p, n), 1, NULL, &s, &e) != 0) return e.msg[0] != 0;
    (*opened)++;
    int ok = 1;
    uint8_t *buf = malloc(1u << 20);
    for (size_t i = 0; ok && buf && i < mp_source_count(s); i++) {
        uint64_t size = mp_source_entry(s, i)->size;
        if (size > (64u << 20)) continue;   // a size changed by the mutation into something large
        for (uint64_t off = 0; off < size;) {
            size_t k = size - off > (1u << 20) ? 1u << 20 : (size_t)(size - off);
            e.msg[0] = 0;
            if (mp_source_read(s, i, off, buf, k, &e) != 0) {
                ok = e.msg[0] != 0;
                break;
            }
            off += k;
        }
    }
    free(buf);
    mp_source_close(s);
    return ok;
}

static void mutate(const char *name, unsigned flips) {
    char p[512];
    snprintf(p, sizeof p, FX "/%s", name);
    size_t n = 0;
    uint8_t *img = mpt_slurp(p, &n);
    snprintf(p, sizeof p, FX "/%s.meta", name);
    size_t ml = 0;
    char *meta = slurp(p, &ml);
    if (!img || !meta) {
        MP_FAIL_("%s or its .meta is missing", name);
        free(img);
        free(meta);
        return;
    }
    uint64_t off[256], len[256], total = 0;
    size_t nr = 0;
    for (char *line = meta; line && *line && nr < 256;) {
        char *nl = strchr(line, '\n');
        if (nl) *nl = 0;
        char *tab = strchr(line, '\t');
        if (tab) {
            off[nr] = strtoull(line, NULL, 10);
            len[nr] = strtoull(tab + 1, NULL, 10);
            if (off[nr] + len[nr] <= n) total += len[nr++];
        }
        line = nl ? nl + 1 : NULL;
    }
    uint8_t *m = malloc(n);
    unsigned bad = 0, opened = 0;
    uint64_t t0 = mp_now_ns();
    for (unsigned k = 0; k < flips && total; k++) {
        memcpy(m, img, n);
        // 1 to 3 bytes, each set to a random value
        for (unsigned j = 0, nb = 1 + rnd() % 3; j < nb; j++) {
            uint64_t at = ((uint64_t)rnd() << 16 ^ rnd()) % total;
            size_t r = 0;
            while (at >= len[r]) at -= len[r++];
            m[off[r] + at] = (uint8_t)rnd();
        }
        if (!try_pkg(m, n, &opened)) bad++;
    }
    // cut short at 64 places, and in the middle of each region
    for (unsigned k = 0; k < 64 + nr; k++) {
        size_t cut = k < 64 ? (size_t)(((uint64_t)n * k) / 64) : (size_t)(off[k - 64] + len[k - 64] / 2);
        if (!try_pkg(img, cut, &opened)) bad++;
    }
    uint64_t ms = (mp_now_ns() - t0) / 1000000;
    fprintf(stderr, "  %s: %u mutations, %u opened, %" PRIu64 " ms\n", name, flips + 64 + (unsigned)nr, opened, ms);
    MP_CHECK_EQ_U64(bad, 0);
    MP_CHECK(opened < flips + 64 + nr);   // most damage is found
    free(m);
    free(img);
    free(meta);
}

MP_TEST(fpkg_mutations) {
    mutate("basic.pkg", 1500);
    mutate("basic_k.pkg", 1000);
}

// A DLC package opens as a DLC (or as anything) and never as a game: the files, bytes and dates of the
// folder it was made of, and its content ID; a license-only one has only sce_sys/ files. A game's
// package is not a DLC. A folder opens the same whatever it is asked for.
MP_TEST(fpkg_dlc) {
    mp_err e = {{0}};
    mp_source *pkg = NULL, *dir = NULL;
    MP_CHECK(mp_source_open_as(FX "/dlc_data.pkg", MP_OPEN_DLC, &pkg, &e) == 0);
    MP_CHECK(mp_source_open_as(FX "/dlc_data", MP_OPEN_DLC, &dir, &e) == 0);
    if (pkg && dir) {
        MP_CHECK(mp_source_is_dlc_package(pkg));
        MP_CHECK(!mp_source_is_dlc_package(dir));
        MP_CHECK_STR(mp_source_package_id(pkg), "UP0000-PPSA00000_00-MAXPACKDLCDATA01");
        MP_CHECK_STR(mp_source_kind_name(pkg), "fpkg");
        MP_CHECK_EQ_U64(mp_source_count(pkg), mp_source_count(dir));
        for (size_t i = 0; i < mp_source_count(pkg) && i < mp_source_count(dir); i++) {
            const mp_entry *a = mp_source_entry(pkg, i), *b = mp_source_entry(dir, i);
            MP_CHECK_STR(a->path, b->path);
            MP_CHECK_EQ_U64(a->size, b->size);
            MP_CHECK_EQ_U64(a->mtime, b->mtime);
            char x[65], y[65];
            MP_CHECK(sha_of(pkg, i, x, &e) == 0 && sha_of(dir, i, y, &e) == 0);
            MP_CHECK_STR(x, y);
        }
    } else {
        MP_FAIL_("dlc_data: %s", e.msg);
    }
    mp_source_close(pkg);
    mp_source_close(dir);
    pkg = NULL;
    MP_CHECK(mp_source_open_as(FX "/dlc_license.pkg", MP_OPEN_DLC, &pkg, &e) == 0);
    if (pkg) {
        MP_CHECK_EQ_U64(mp_source_count(pkg), 2);
        for (size_t i = 0; i < mp_source_count(pkg); i++) MP_CHECK(!strncmp(mp_source_entry(pkg, i)->path, "sce_sys/", 8));
        MP_CHECK_STR(mp_source_package_id(pkg), "UP0000-PPSA00000_00-MAXPACKLICENSE01");
    }
    mp_source_close(pkg);
    // anything: a game's package and a DLC's
    pkg = NULL;
    MP_CHECK(mp_source_open_as(FX "/mini.pkg", MP_OPEN_ANY, &pkg, &e) == 0);
    MP_CHECK(pkg && !mp_source_is_dlc_package(pkg));
    mp_source_close(pkg);
    pkg = NULL;
    MP_CHECK(mp_source_open_as(FX "/dlc.pkg", MP_OPEN_ANY, &pkg, &e) == 0);
    MP_CHECK(pkg && mp_source_is_dlc_package(pkg));
    mp_source_close(pkg);
    // a game's package is not a DLC
    pkg = NULL;
    MP_CHECK(mp_source_open_as(FX "/mini.pkg", MP_OPEN_DLC, &pkg, &e) == -1 && pkg == NULL);
    MP_CHECK_CONTAINS(e.msg, "a game's package, not additional content (DLC)");
    // a DLC's package is not a game
    MP_CHECK(mp_source_open_as(FX "/dlc_data.pkg", MP_OPEN_GAME, &pkg, &e) == -1 && pkg == NULL);
    MP_CHECK_CONTAINS(e.msg, "additional content (DLC), not a game");
}

int main(void) {
    MP_RUN(fpkg_like_manifest);
    MP_RUN(fpkg_kinds);
    MP_RUN(fpkg_ranges);
    MP_RUN(fpkg_passcode);
    MP_RUN(fpkg_refused);
    MP_RUN(fpkg_dlc);
    MP_RUN(fpkg_bad);
    MP_RUN(fpkg_damaged_block);
    MP_RUN(fpkg_plain_and_mini);
    MP_RUN(fpkg_same_game);
    MP_RUN(fpkg_parallel_reads);
    MP_RUN(fpkg_mutations);
    free(g_man);
    free(g_manbuf);
    MP_TEST_MAIN_END();
}
