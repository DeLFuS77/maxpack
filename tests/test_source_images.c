// SPDX-License-Identifier: GPL-3.0-or-later
// The UFS2, PFS and PFSC readers (make_images.py's fixtures): the same game in every form lists and
// reads as its manifest says; holes, fragments and indirect blocks; the kind told by the signature;
// containers and how deep they nest; every damaged or hostile image stops with its message; and
// deterministic mutations (bytes flipped in the metadata, images cut short) end in a clean error or
// a read, never a crash or a hang.
#include <stdlib.h>
#include <zlib.h>
#if defined(_MSC_VER) && defined(_DEBUG)
#include <crtdbg.h>
#endif

#include "mp_fixture.h"
#include "mp_platform.h"
#include "mp_source.h"
#include "mp_source_impl.h"
#include "mp_test.h"

#define FX MP_FIXTURES_DIR
#define TMP MP_TEST_TMP "/images"

typedef struct {
    uint64_t size;
    unsigned long crc;
    int64_t mtime;
    char *path;
} man_line;

static man_line *g_man;
static size_t g_nman;

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
    char *buf = slurp(FX "/basic.manifest.tsv", &len);
    g_man = calloc(1024, sizeof *g_man);
    for (char *line = buf; line && *line;) {
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

static mp_source *open_fx(const char *name, mp_err *e) {
    char p[1024];
    snprintf(p, sizeof p, FX "/%s", name);
    mp_source *s = NULL;
    if (mp_source_open(p, &s, e) != 0) return NULL;
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

static const char *forms[] = {"basic.exfat",        "basic.ffpkg",        "profile.ffpkg",          "basic.ffpfs",
                              "basic_c.ffpfs",      "basic.exfat.ffpfsc", "basic.ffpkg.ffpfsc",     "basic.ffpfs.ffpfsc",
                              "basic_raw.exfat.ffpfsc", "nest3.ffpfsc"};

// Every form of the basic game: the manifest's paths, sizes, dates and contents, and its folders.
MP_TEST(images_like_manifest) {
    load_manifest();
    for (size_t f = 0; f < sizeof forms / sizeof forms[0]; f++) {
        mp_err e = {{0}};
        mp_source *s = open_fx(forms[f], &e);
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

MP_TEST(images_kinds) {
    struct {
        const char *file, *kind, *inner;
        int compressed;
    } k[] = {{"basic.exfat", "exfat", "", 0},          {"basic.ffpkg", "ufs2", "", 0},
             {"basic.ffpfs", "pfs", "", 0},            {"basic_c.ffpfs", "pfs", "", 1},
             {"basic.exfat.ffpfsc", "pfsc", "exfat", 1}, {"basic.ffpkg.ffpfsc", "pfsc", "ufs2", 1},
             {"basic.ffpfs.ffpfsc", "pfsc", "pfs", 1}, {"basic_raw.exfat.ffpfsc", "pfsc", "exfat", 0},
             {"nest3.ffpfsc", "pfsc", "exfat", 1}};
    for (size_t i = 0; i < sizeof k / sizeof k[0]; i++) {
        mp_err e = {{0}};
        mp_source *s = open_fx(k[i].file, &e);
        if (!s) {
            MP_FAIL_("%s: %s", k[i].file, e.msg);
            continue;
        }
        MP_CHECK_STR(mp_source_kind_name(s), k[i].kind);
        MP_CHECK_STR(mp_source_inner_name(s), k[i].inner);
        MP_CHECK_EQ_U64(mp_source_compressed(s), k[i].compressed);
        MP_CHECK(mp_source_readers(s) >= 4);
        mp_source_close(s);
    }
    mp_err e = {{0}};
    mp_source *s = open_fx("basic.exfat.ffpfsc", &e);
    char buf[64];
    if (s) MP_CHECK_STR(mp_source_kind_text(s, buf, sizeof buf), "PFS container with an exFAT image inside, compressed");
    MP_CHECK(s && mp_source_kind_of(s) == MP_SRC_PFS);
    mp_source_close(s);
}

// Holes read as zeros, and the blocks around them as written.
MP_TEST(ufs2_holes) {
    mp_err e = {{0}};
    mp_source *s = open_fx("basic.ffpkg", &e);
    if (!s) {
        MP_FAIL_("%s", e.msg);
        return;
    }
    long z = mp_source_find(s, "zeros.bin"), v = mp_source_find(s, "data/vdl.bin");
    MP_CHECK(z >= 0 && v >= 0);
    uint8_t *buf = malloc(524288);
    memset(buf, 0x55, 524288);
    int ok = z >= 0 && mp_source_read(s, (size_t)z, 0, buf, 524288, &e) == 0;
    for (size_t i = 0; ok && i < 524288; i++) ok = buf[i] == 0;
    MP_CHECK(ok);
    memset(buf, 0x55, 15000);
    ok = v >= 0 && mp_source_read(s, (size_t)v, 5000, buf, 15000, &e) == 0;
    for (size_t i = 0; ok && i < 15000; i++) ok = buf[i] == 0;
    MP_CHECK(ok);
    free(buf);
    mp_source_close(s);
}

// Double and triple indirect blocks; the sparse file is larger than its image.
MP_TEST(ufs2_indirect) {
    size_t len = 0;
    char *tsv = slurp(FX "/indirect.tsv", &len);
    mp_err e = {{0}};
    mp_source *s = open_fx("indirect.ffpkg", &e);
    if (!s || !tsv) {
        MP_FAIL_("indirect.ffpkg: %s", e.msg);
        free(tsv);
        return;
    }
    int checks = 0;
    for (char *line = tsv; line && *line;) {
        char *nl = strchr(line, '\n');
        if (nl) *nl = 0;
        char *t1 = strchr(line, '\t'), *t2 = t1 ? strchr(t1 + 1, '\t') : NULL;
        if (t1 && t2) {
            *t1 = *t2 = 0;
            if (!strcmp(line, "at")) {   // at <offset> <text>: triple.bin has that text there
                long i = mp_source_find(s, "triple.bin");
                uint64_t off = strtoull(t1 + 1, NULL, 10);
                char got[64] = {0};
                size_t n = strlen(t2 + 1);
                MP_CHECK(i >= 0 && mp_source_read(s, (size_t)i, off, got, n, &e) == 0 && !memcmp(got, t2 + 1, n));
                checks++;
            } else {
                long i = mp_source_find(s, line);
                MP_CHECK(i >= 0);
                if (i >= 0) {
                    MP_CHECK_EQ_U64(mp_source_entry(s, (size_t)i)->size, strtoull(t1 + 1, NULL, 10));
                    if (t2[1] != '-') MP_CHECK(crc_of(s, (size_t)i, 1u << 20, &e) == (long long)strtoul(t2 + 1, NULL, 16));
                }
                checks++;
            }
        }
        line = nl ? nl + 1 : NULL;
    }
    MP_CHECK_EQ_U64(checks, 7);
    // a stretch of a hole in the triple indirect range, and the image is smaller than the file
    long i = mp_source_find(s, "triple.bin");
    uint8_t buf[8192];
    memset(buf, 0x55, sizeof buf);
    int ok = i >= 0 && mp_source_read(s, (size_t)i, 900000000, buf, sizeof buf, &e) == 0;
    for (size_t k = 0; ok && k < sizeof buf; k++) ok = buf[k] == 0;
    MP_CHECK(ok);
    uint64_t img = 0;
    mp_stat(FX "/indirect.ffpkg", &img, NULL);
    MP_CHECK(i >= 0 && mp_source_entry(s, (size_t)i)->size > img * 100);
    mp_source_close(s);
    free(tsv);
}

// Each damaged image of bad.tsv fails to open with the words given there.
MP_TEST(images_bad) {
    size_t len = 0;
    char *tsv = slurp(FX "/bad.tsv", &len);
    MP_CHECK(tsv != NULL);
    size_t n = 0;
    for (char *line = tsv; line && *line;) {
        char *nl = strchr(line, '\n');
        if (nl) *nl = 0;
        char *tab = strchr(line, '\t');
        if (tab) {
            *tab = 0;
            mp_err e = {{0}};
            mp_source *s = open_fx(line, &e);
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
    MP_CHECK(n >= 40);
    free(tsv);
}

// The kind comes from the contents: a UFS2 image named .exfat, a container named .ffpkg, no
// extension at all.
MP_TEST(images_by_signature) {
    mp_err e = {{0}};
    mp_make_dirs(TMP, &e);
    const char *pairs[][3] = {{"basic.ffpkg", TMP "/renamed.exfat", "ufs2"},
                              {"basic.exfat.ffpfsc", TMP "/renamed.ffpkg", "pfsc"},
                              {"basic.ffpfs", TMP "/no_extension", "pfs"}};
    for (size_t i = 0; i < 3; i++) {
        char from[512];
        snprintf(from, sizeof from, FX "/%s", pairs[i][0]);
        mpt_copy_file(from, pairs[i][1]);
        mp_source *s = NULL;
        MP_CHECK(mp_source_open(pairs[i][1], &s, &e) == 0);
        if (s) MP_CHECK_STR(mp_source_kind_name(s), pairs[i][2]);
        mp_source_close(s);
    }
}

// The exFAT reader reads the same through an image in memory as from the file.
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

MP_TEST(exfat_from_image) {
    load_manifest();
    size_t n = 0;
    uint8_t *b = mpt_slurp(FX "/basic.exfat", &n);
    mp_err e = {{0}};
    mp_source *s = NULL;
    MP_CHECK(b && mp_exfat_open_image(mem_image(b, n), &s, &e) == 0);
    if (s) {
        MP_CHECK_EQ_U64(mp_source_count(s), g_nman);
        size_t bad = 0;
        for (size_t i = 0; i < g_nman && i < mp_source_count(s); i++)
            bad += crc_of(s, i, 65536, &e) != (long long)g_man[i].crc;
        MP_CHECK_EQ_U64(bad, 0);
    }
    mp_source_close(s);
    // cut short in memory: the same message as the truncated file
    MP_CHECK(mp_exfat_open_image(mem_image(b, 300), &s, &e) == -1);
    MP_CHECK_CONTAINS(e.msg, "truncated");
    free(b);
}

// Several threads read the files of a container at once (its block cache is shared).
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
        if (crc_of(a->s, i, 3000 + a->seed * 1000, &e) != (long long)g_man[i].crc) a->bad++;
    }
}

MP_TEST(images_parallel_reads) {
    load_manifest();
    const char *names[] = {"basic.exfat.ffpfsc", "basic_c.ffpfs", "basic.ffpkg.ffpfsc"};
    for (size_t f = 0; f < 3; f++) {
        mp_err e = {{0}};
        mp_source *s = open_fx(names[f], &e);
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

static uint32_t rng = 88172645u;
static uint32_t rnd(void) {
    rng ^= rng << 13;
    rng ^= rng >> 17;
    rng ^= rng << 5;
    return rng;
}

// Opens the image in memory and reads every file whole: either a clean error (a message) or all read.
static int try_image(const uint8_t *p, size_t n, unsigned *opened) {
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
    uint64_t off[4096], len[4096], total = 0;
    size_t nr = 0;
    for (char *line = meta; line && *line && nr < 4096;) {
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
        // 1 to 3 bytes of the metadata, each set to a random value
        for (unsigned j = 0, nb = 1 + rnd() % 3; j < nb; j++) {
            uint64_t at = ((uint64_t)rnd() << 16 ^ rnd()) % total;
            size_t r = 0;
            while (at >= len[r]) at -= len[r++];
            m[off[r] + at] = (uint8_t)rnd();
        }
        if (!try_image(m, n, &opened)) bad++;
    }
    // cut short at 64 places, and at the end of each region
    for (unsigned k = 0; k < 64 + nr; k++) {
        size_t cut = k < 64 ? (size_t)(((uint64_t)n * k) / 64) : (size_t)(off[k - 64] + len[k - 64] / 2);
        if (!try_image(img, cut, &opened)) bad++;
    }
    uint64_t ms = (mp_now_ns() - t0) / 1000000;
    fprintf(stderr, "  %s: %u mutations, %u opened, %" PRIu64 " ms\n", name, flips + 64 + (unsigned)nr, opened, ms);
    MP_CHECK_EQ_U64(bad, 0);
    MP_CHECK(opened < flips + 64 + nr);   // most damage is found
    free(m);
    free(img);
    free(meta);
}

MP_TEST(images_mutations) {
    mutate("basic.ffpkg", 1500);
    mutate("basic.ffpfs", 1500);
    mutate("basic_c.ffpfs", 1000);
    mutate("basic.exfat.ffpfsc", 500);
    mutate("basic.ffpkg.ffpfsc", 500);
    mutate("basic.ffpfs.ffpfsc", 500);
    // made by MkPFS (tests/data)
    mutate("mkpfs_plain.ffpfs", 500);
    mutate("mkpfs_raw.ffpfs", 500);
    mutate("mkpfs_exfat.ffpfsc", 300);
    mutate("mkpfs_ufs2.ffpfsc", 300);
    mutate("mkpfs_pfs.ffpfsc", 300);
}

int main(void) {
#if defined(_MSC_VER) && defined(_DEBUG)
    // A Debug build checks the heap on every allocation and says what it finds on stderr (no dialog).
    for (int t = 0; t < _CRT_ERRCNT; t++) {
        _CrtSetReportMode(t, _CRTDBG_MODE_FILE);
        _CrtSetReportFile(t, _CRTDBG_FILE_STDERR);
    }
    _CrtSetDbgFlag(_CrtSetDbgFlag(_CRTDBG_REPORT_FLAG) | _CRTDBG_CHECK_EVERY_1024_DF);
#endif
    MP_RUN(images_like_manifest);
    MP_RUN(images_kinds);
    MP_RUN(ufs2_holes);
    MP_RUN(ufs2_indirect);
    MP_RUN(images_bad);
    MP_RUN(images_by_signature);
    MP_RUN(exfat_from_image);
    MP_RUN(images_parallel_reads);
    MP_RUN(images_mutations);
    MP_TEST_MAIN_END();
}
