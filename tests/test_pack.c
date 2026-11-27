// SPDX-License-Identifier: GPL-3.0-or-later
#include <stdio.h>
#include <stdlib.h>

#include "mp_analyze.h"
#include "mp_idx3.h"
#include "mp_pack.h"
#include "mp_platform.h"
#include "mp_rules.h"
#include "mp_test.h"
#include "mp_zpk.h"

#define TMP MP_TEST_TMP "/pack"
#define BASIC MP_FIXTURES_DIR "/basic.exfat"

// ---- test utilities ----------------------------------------------------------------

typedef struct {
    char **v;
    size_t n, cap;
} strlist;

static int list_cb(void *ctx, const char *rel, int is_dir, uint64_t size, int64_t mtime) {
    strlist *l = ctx;
    (void)size;
    (void)mtime;
    if (is_dir) return 0;
    if (l->n == l->cap) l->v = realloc(l->v, (l->cap = l->cap ? 2 * l->cap : 256) * sizeof *l->v);
    l->v[l->n++] = mp_strdup(rel);
    return 0;
}

static int cmp_str(const void *a, const void *b) { return strcmp(*(char *const *)a, *(char *const *)b); }

static strlist list_dir(const char *dir) {
    strlist l = {0};
    mp_dir_walk(dir, list_cb, &l, NULL);
    if (l.n > 1) qsort(l.v, l.n, sizeof *l.v, cmp_str);
    return l;
}

static void free_list(strlist *l) {
    for (size_t i = 0; i < l->n; i++) free(l->v[i]);
    free(l->v);
}

static uint8_t *slurp(const char *path, size_t *len) {
    mp_file *f = NULL;
    uint64_t size = 0;
    size_t got = 0;
    *len = 0;
    if (mp_file_open_read(path, &f, NULL) != 0) return NULL;
    mp_file_size(f, &size, NULL);
    uint8_t *b = malloc((size_t)size + 1);
    mp_file_pread(f, b, (size_t)size, 0, &got, NULL);
    mp_file_close(f);
    *len = got;
    return b;
}

// Deletes the contents of a test output folder (files and subfolders, ASCII
// paths inside MP_TEST_TMP), to start clean.
#ifdef _WIN32
#include <direct.h>
#define rmdir_ _rmdir
#else
#include <unistd.h>
#define rmdir_ rmdir
#endif

typedef struct {
    const char *root;
    strlist dirs;
} rmctx;

static int rm_cb(void *ctx, const char *rel, int is_dir, uint64_t size, int64_t mtime) {
    rmctx *c = ctx;
    (void)size;
    (void)mtime;
    char p[2048];
    snprintf(p, sizeof p, "%s/%s", c->root, rel);
    if (is_dir)
        list_cb(&c->dirs, p, 0, 0, 0);
    else
        remove(p);
    return 0;
}

static void clear_dir(const char *dir) {
    if (strncmp(dir, MP_TEST_TMP, strlen(MP_TEST_TMP)) != 0) return;   // never outside the tests
    rmctx c = {dir, {0}};
    mp_dir_walk(dir, rm_cb, &c, NULL);
    for (size_t i = c.dirs.n; i-- > 0;) rmdir_(c.dirs.v[i]);   // from the inside out
    free_list(&c.dirs);
}

static mp_pack_opts quiet(unsigned threads) {
    mp_pack_opts o;
    mp_pack_opts_default(&o);
    o.threads = threads;
    o.progress = 0;
    return o;
}

static int pack_to(const char *src, const char *dir, const mp_pack_opts *o, mp_pack_result *r, mp_err *e) {
    clear_dir(dir);
    mp_pack_out out;
    if (mp_pack_out_dir(dir, &out, e) != 0) return -1;
    int rc = mp_pack_run(src, &out, o, r, e);
    out.destroy(out.ctx);
    return rc;
}

static mp_zpk *open_zpk(const char *dir, mp_zpk_volsrc *vs, uint8_t **index) {
    char p[2048];
    size_t len = 0;
    snprintf(p, sizeof p, "%s/ampr_zstd.index", dir);
    *index = slurp(p, &len);
    mp_zpk *z = NULL;
    if (!*index || mp_zpk_volsrc_dir(dir, vs, NULL) != 0 || mp_zpk_open(*index, len, vs, &z, NULL) != 0)
        return NULL;
    return z;
}

// ---- tests ----------------------------------------------------------------------------------

MP_TEST(pack_folder_roundtrip) {
    mp_pack_opts o = quiet(4);
    mp_pack_result r;
    mp_err e = {{0}};
    MP_CHECK(pack_to(BASIC, TMP "/basic", &o, &r, &e) == 0);
    if (e.msg[0]) fprintf(stderr, "  %s\n", e.msg);
    mp_source *s = NULL;
    MP_CHECK(mp_source_open(BASIC, &s, &e) == 0);
    if (!s) return;
    mp_rules *rules = mp_rules_new();
    mp_zpk_volsrc vs;
    uint8_t *index = NULL;
    mp_zpk *z = open_zpk(TMP "/basic", &vs, &index);
    MP_CHECK(z != NULL);
    size_t packed = 0, loose = 0;
    for (size_t i = 0; z && i < mp_source_count(s); i++) {
        const mp_entry *en = mp_source_entry(s, i);
        uint8_t *a = malloc((size_t)en->size + 1), *b = NULL;
        mp_source_read(s, i, 0, a, (size_t)en->size, &e);
        if (mp_rules_classify(rules, en->path, en->size) == MP_LOOSE) {
            char p[2048];
            size_t len = 0;
            snprintf(p, sizeof p, "%s/%s", TMP "/basic", en->path);
            b = slurp(p, &len);
            uint64_t size = 0;
            int64_t mt = 0;
            if (!b || len != en->size || memcmp(a, b, len) || mp_stat(p, &size, &mt) != 0 || mt != en->mtime)
                MP_FAIL_("loose file differs (content or date): %s", en->path);
            loose++;
        } else {
            size_t k = 0;
            while (k < mp_zpk_count(z) && strcmp(mp_zpk_path(z, k), en->path)) k++;
            b = malloc((size_t)en->size + 1);
            if (k == mp_zpk_count(z) || mp_zpk_read_file(z, k, 0, b, (size_t)en->size, &e) != 0 ||
                memcmp(a, b, (size_t)en->size))
                MP_FAIL_("packed file differs: %s", en->path);
            packed++;
        }
        free(a);
        free(b);
    }
    MP_CHECK_EQ_U64(packed, r.packed_files);
    MP_CHECK_EQ_U64(loose, r.loose_files);
    size_t len = 0;
    uint8_t *idx = slurp(TMP "/basic/ampr_emu.index", &len);
    mp_idx3_entry *v = NULL;
    size_t m = 0;
    MP_CHECK(idx && mp_idx3_parse(idx, len, &v, &m, &e) == 0);
    MP_CHECK_EQ_U64(m, mp_source_count(s));
    mp_idx3_entries_free(v, m);
    free(idx);
    if (z) mp_zpk_close(z);
    mp_zpk_volsrc_free(&vs);
    free(index);
    mp_rules_free(rules);
    mp_source_close(s);
}

static int same_trees(const char *a, const char *b) {
    strlist la = list_dir(a), lb = list_dir(b);
    int same = la.n == lb.n && la.n > 0;
    for (size_t i = 0; same && i < la.n; i++) {
        char pa[2048], pb[2048];
        size_t x = 0, y = 0;
        same = !strcmp(la.v[i], lb.v[i]);
        snprintf(pa, sizeof pa, "%s/%s", a, la.v[i]);
        snprintf(pb, sizeof pb, "%s/%s", b, lb.v[i]);
        uint8_t *ba = slurp(pa, &x), *bb = slurp(pb, &y);
        if (same) same = ba && bb && x == y && !memcmp(ba, bb, x);
        if (!same) MP_FAIL_("different: %s", la.v[i]);
        free(ba);
        free(bb);
    }
    free_list(&la);
    free_list(&lb);
    return same;
}

MP_TEST(pack_threads_identical) {
    mp_pack_opts o1 = quiet(1), o4 = quiet(4);
    mp_pack_result r;
    mp_err e = {{0}};
    MP_CHECK(pack_to(BASIC, TMP "/t1", &o1, &r, &e) == 0);
    MP_CHECK(pack_to(BASIC, TMP "/t4", &o4, &r, &e) == 0);
    MP_CHECK(same_trees(TMP "/t1", TMP "/t4"));
}

MP_TEST(pack_matches_analyzer) {
    mp_pack_opts o = quiet(4);
    mp_pack_result r;
    mp_err e = {{0}};
    MP_CHECK(pack_to(BASIC, TMP "/an", &o, &r, &e) == 0);
    mp_analyze_opts ao;
    mp_analyze_opts_default(&ao);
    ao.full = 1;
    ao.progress = 0;
    ao.dedup = 0;
    ao.transport = 0;
    mp_report rep;
    MP_CHECK(mp_analyze(BASIC, &ao, &rep, &e) == 0);
    MP_CHECK_EQ_U64(r.stored_bytes, rep.est_packed[MP_CFG_V1_L19]);
    mp_report_free(&rep);
}

MP_TEST(pack_refuses_case_dups) {
    mp_pack_opts o = quiet(2);
    mp_pack_result r;
    mp_err e = {{0}};
    clear_dir(TMP "/casedup");
    MP_CHECK(pack_to(MP_FIXTURES_DIR "/casedup.exfat", TMP "/casedup", &o, &r, &e) == -1);
    MP_CHECK_CONTAINS(e.msg, "case");
    MP_CHECK(mp_dir_is_empty(TMP "/casedup") != 0);   // nothing has been written: empty or not even created
}

// With an output folder that did not exist, a rejected source does not create it.
MP_TEST(pack_dir_not_created_on_refusal) {
    mp_pack_opts o = quiet(2);
    mp_pack_result r;
    mp_err e = {{0}};
    const char *dir = TMP "/nueva_no_creada";
    clear_dir(dir);
#ifdef _WIN32
    _rmdir(dir);
#else
    rmdir(dir);
#endif
    MP_CHECK(mp_path_kind(dir) == 0);
    mp_pack_out out;
    MP_CHECK(mp_pack_out_dir(dir, &out, &e) == 0);
    MP_CHECK(mp_pack_run(MP_FIXTURES_DIR "/casedup.exfat", &out, &o, &r, &e) == -1);
    out.destroy(out.ctx);
    MP_CHECK(mp_path_kind(dir) == 0);
}

// A loose pack volume without its index would clash with the new volumes: it is rejected beforehand.
MP_TEST(pack_refuses_stale_volume) {
    mp_err e = {{0}};
    MP_CHECK(mp_make_dirs(TMP "/stale_src", &e) == 0);
    MP_CHECK(mp_write_file(TMP "/stale_src/eboot.bin", "EBOOT", 5, &e) == 0);
    MP_CHECK(mp_write_file(TMP "/stale_src/datos.bin", "datos datos datos datos", 23, &e) == 0);
    MP_CHECK(mp_write_file(TMP "/stale_src/ampr_zstd-000.pak", "volumen viejo", 13, &e) == 0);
    mp_pack_opts o = quiet(2);
    mp_pack_result r;
    MP_CHECK(pack_to(TMP "/stale_src", TMP "/stale_out", &o, &r, &e) == -1);
    MP_CHECK_CONTAINS(e.msg, "no ampr_zstd.index");
    MP_CHECK_STR(r.code, "packs_damaged");
}

// A packed folder is packed again from the files it was made of (test_import.c tests it whole).
MP_TEST(pack_takes_packed_source) {
    mp_pack_opts o = quiet(2);
    mp_pack_result r;
    mp_err e = {{0}};
    MP_CHECK(pack_to(TMP "/basic", TMP "/repack", &o, &r, &e) == 0);
    MP_CHECK(r.from_packed == 1);
    MP_CHECK(same_trees(TMP "/basic", TMP "/repack"));
}

MP_TEST(pack_replaces_ampr_files) {
    mp_err e = {{0}};
    const char *src = TMP "/src_ampr";
    MP_CHECK(mp_make_dirs(TMP "/src_ampr/fakelib", &e) == 0);
    uint8_t *data = malloc(500000);
    for (size_t i = 0; i < 500000; i++) data[i] = (uint8_t)("datos del juego "[i % 16]);
    MP_CHECK(mp_write_file(TMP "/src_ampr/datos.bin", data, 500000, &e) == 0);
    MP_CHECK(mp_write_file(TMP "/src_ampr/eboot.bin", "EBOOT", 5, &e) == 0);
    MP_CHECK(mp_write_file(TMP "/src_ampr/ampr_emu.index", "VIEJO", 5, &e) == 0);
    MP_CHECK(mp_write_file(TMP "/src_ampr/fakelib/libSceAmpr.sprx", "runtime antiguo sin zstd", 24, &e) == 0);
    const char rt[] = "runtime nuevo con AMPRZPK1 dentro";
    MP_CHECK(mp_write_file(TMP "/runtime.sprx", rt, sizeof rt - 1, &e) == 0);
    mp_pack_opts o = quiet(2);
    o.runtime = TMP "/runtime.sprx";
    mp_pack_result r;
    MP_CHECK(pack_to(src, TMP "/out_ampr", &o, &r, &e) == 0);
    MP_CHECK(r.runtime_without_zstd == 0);
    size_t len = 0;
    uint8_t *got = slurp(TMP "/out_ampr/fakelib/libSceAmpr.sprx", &len);
    MP_CHECK(got && len == sizeof rt - 1 && !memcmp(got, rt, len));
    free(got);
    uint8_t *idx = slurp(TMP "/out_ampr/ampr_emu.index", &len);
    mp_idx3_entry *v = NULL;
    size_t m = 0;
    MP_CHECK(idx && mp_idx3_parse(idx, len, &v, &m, &e) == 0);
    int has_old = 0, rt_ok = 0, has_data = 0;
    for (size_t i = 0; i < m; i++) {
        has_old |= !strcmp(v[i].app0_path, "/app0/ampr_emu.index");
        rt_ok |= !strcmp(v[i].app0_path, "/app0/fakelib/libSceAmpr.sprx") && v[i].size == sizeof rt - 1;
        has_data |= !strcmp(v[i].app0_path, "/app0/datos.bin") && v[i].size == 500000;
    }
    MP_CHECK(!has_old && rt_ok && has_data);
    mp_idx3_entries_free(v, m);
    free(idx);
    // Without --runtime: the old one stays and a warning is given.
    o.runtime = NULL;
    MP_CHECK(pack_to(src, TMP "/out_ampr2", &o, &r, &e) == 0);
    MP_CHECK(r.runtime_without_zstd == 1);
    free(data);
}

MP_TEST(pack_many_small_files) {
    mp_err e = {{0}};
    const char *src = TMP "/many_src";
    char p[512];
    uint8_t buf[3000];
    strlist have = list_dir(src);
    if (have.n != 20001) {   // generated once (eboot.bin + 20,000 files)
        for (int d = 0; d < 200; d++) {
            snprintf(p, sizeof p, "%s/d%03d", src, d);
            mp_make_dirs(p, &e);
            for (int f = 0; f < 100; f++) {
                size_t n = 1 + (size_t)((d * 100 + f) * 37) % 3000;
                for (size_t k = 0; k < n; k++) buf[k] = (uint8_t)('a' + (k + (size_t)f) % 26);
                snprintf(p, sizeof p, "%s/d%03d/f%03d.dat", src, d, f);
                mp_write_file(p, buf, n, &e);
            }
        }
        snprintf(p, sizeof p, "%s/eboot.bin", src);
        mp_write_file(p, "E", 1, &e);
    }
    free_list(&have);
    mp_pack_opts o = quiet(8);
    mp_pack_result r;
    uint64_t t0 = mp_now_ns();
    MP_CHECK(pack_to(src, TMP "/many_out", &o, &r, &e) == 0);
    MP_CHECK(mp_now_ns() - t0 < 60000000000ull);
    MP_CHECK_EQ_U64(r.packed_files, 20000);
    size_t len = 0;
    uint8_t *idx = slurp(TMP "/many_out/ampr_emu.index", &len);
    mp_idx3_entry *v = NULL;
    size_t m = 0;
    MP_CHECK(idx && mp_idx3_parse(idx, len, &v, &m, &e) == 0);
    MP_CHECK_EQ_U64(m, 20001);
    mp_idx3_entries_free(v, m);
    free(idx);
    mp_zpk_volsrc vs;
    uint8_t *index = NULL;
    mp_zpk *z = open_zpk(TMP "/many_out", &vs, &index);
    MP_CHECK(z && mp_zpk_count(z) == 20000);
    for (size_t i = 0; z && i < mp_zpk_count(z); i += 97) {   // a spread-out sample
        uint64_t n = mp_zpk_size(z, i);
        uint8_t a[3000], b[3000];
        snprintf(p, sizeof p, "%s/%s", src, mp_zpk_path(z, i));
        size_t got = 0;
        uint8_t *orig = slurp(p, &got);
        if (!orig || got != n || mp_zpk_read_file(z, i, 0, a, (size_t)n, &e) != 0 || memcmp(a, orig, (size_t)n))
            MP_FAIL_("different: %s", mp_zpk_path(z, i));
        (void)b;
        free(orig);
    }
    if (z) mp_zpk_close(z);
    mp_zpk_volsrc_free(&vs);
    free(index);
}

int main(void) {
    MP_RUN(pack_folder_roundtrip);
    MP_RUN(pack_threads_identical);
    MP_RUN(pack_matches_analyzer);
    MP_RUN(pack_refuses_case_dups);
    MP_RUN(pack_dir_not_created_on_refusal);
    MP_RUN(pack_refuses_stale_volume);
    MP_RUN(pack_takes_packed_source);
    MP_RUN(pack_replaces_ampr_files);
    MP_RUN(pack_many_small_files);
    MP_TEST_MAIN_END();
}
