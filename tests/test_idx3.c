// SPDX-License-Identifier: GPL-3.0-or-later
#include <stdio.h>
#include <stdlib.h>

#include "mp_hash.h"
#include "mp_idx3.h"
#include "mp_pack.h"
#include "mp_platform.h"
#include "mp_source.h"
#include "mp_test.h"

#define TMP MP_TEST_TMP "/idx3"

static uint32_t rd32(const uint8_t *p) { return p[0] | p[1] << 8 | p[2] << 16 | (uint32_t)p[3] << 24; }
static uint64_t rd64(const uint8_t *p) { return rd32(p) | (uint64_t)rd32(p + 4) << 32; }

MP_TEST(idx3_two_rows) {
    mp_idx3_row rows[2] = {{"B.bin", 5, 100}, {"a.bin", 7, 200}};
    uint8_t *d = NULL;
    size_t n = 0;
    mp_err e = {{0}};
    MP_CHECK(mp_idx3_build(rows, 2, &d, &n, &e) == 0);
    if (!d) return;
    MP_CHECK(memcmp(d, "AMPRIDX3", 8) == 0);
    MP_CHECK_EQ_U64(rd32(d + 8), 3);
    MP_CHECK_EQ_U64(rd32(d + 12), 24);
    MP_CHECK_EQ_U64(rd64(d + 16), 2);
    MP_CHECK_EQ_U64(rd64(d + 24), 24);    // "/app0/a.bin\0" + "/app0/B.bin\0"
    MP_CHECK_EQ_U64(rd64(d + 32), 128);   // align16(48 + 48 + 24)
    MP_CHECK_EQ_U64(rd32(d + 40), 16);
    MP_CHECK_EQ_U64(rd32(d + 44), 4);
    MP_CHECK_EQ_U64(n, 128 + 4 * 16);
    // Orden por clave sin mayúsculas: a.bin primero.
    MP_CHECK_EQ_U64(rd32(d + 48), 0);
    MP_CHECK_EQ_U64(rd32(d + 52), 11);
    MP_CHECK_EQ_U64(rd64(d + 56), 7);
    MP_CHECK_EQ_U64(rd64(d + 64), 200);
    MP_CHECK_EQ_U64(rd32(d + 72), 12);
    MP_CHECK(memcmp(d + 96, "/app0/a.bin\0/app0/B.bin\0", 24) == 0);
    // Cada fila está en su slot (h & 3) o en el siguiente libre.
    const char *paths[2] = {"/app0/a.bin", "/app0/B.bin"};
    for (int r = 0; r < 2; r++) {
        uint64_t h = mp_idx3_path_hash(paths[r]);
        int found = 0;
        for (int k = 0; k < 4 && !found; k++) {
            const uint8_t *s = d + 128 + 16 * (((h & 3) + (uint64_t)k) & 3);
            found = rd64(s) == h && rd32(s + 8) == (uint32_t)r + 1;
        }
        MP_CHECK(found);
    }
    free(d);
}

MP_TEST(idx3_rejects_case_dup) {
    mp_idx3_row rows[2] = {{"x/A.bin", 1, 0}, {"x/a.bin", 1, 0}};
    uint8_t *d = NULL;
    size_t n = 0;
    mp_err e = {{0}};
    MP_CHECK(mp_idx3_build(rows, 2, &d, &n, &e) == -1);
    MP_CHECK_CONTAINS(e.msg, "mayúsculas");
    mp_idx3_row bad[1] = {{"a/../b", 1, 0}};
    MP_CHECK(mp_idx3_build(bad, 1, &d, &n, &e) == -1);
}

static size_t basic_rows(mp_source **s, mp_idx3_row **rows) {
    mp_err e = {{0}};
    if (mp_source_open(MP_FIXTURES_DIR "/basic.exfat", s, &e) != 0) {
        MP_FAIL_("no abre basic.exfat: %s", e.msg);
        return 0;
    }
    size_t n = mp_source_count(*s);
    *rows = malloc(n * sizeof **rows);
    for (size_t i = 0; i < n; i++) {
        const mp_entry *en = mp_source_entry(*s, i);
        (*rows)[i].path = en->path;
        (*rows)[i].size = en->size;
        (*rows)[i].mtime = en->mtime;
    }
    return n;
}

MP_TEST(idx3_parse_roundtrip) {
    mp_source *s = NULL;
    mp_idx3_row *rows = NULL;
    size_t n = basic_rows(&s, &rows);
    if (!n) return;
    uint8_t *d = NULL;
    size_t len = 0;
    mp_err e = {{0}};
    MP_CHECK(mp_idx3_build(rows, n, &d, &len, &e) == 0);
    mp_idx3_entry *v = NULL;
    size_t m = 0;
    MP_CHECK(d && mp_idx3_parse(d, len, &v, &m, &e) == 0);
    MP_CHECK_EQ_U64(m, n);
    size_t matched = 0;
    for (size_t i = 0; i < m; i++)
        for (size_t k = 0; k < n; k++)
            if (!strncmp(v[i].app0_path, "/app0/", 6) && !strcmp(v[i].app0_path + 6, rows[k].path) &&
                v[i].size == rows[k].size && v[i].mtime == rows[k].mtime)
                matched++;
    MP_CHECK_EQ_U64(matched, n);
    mp_err e2 = {{0}};
    d[60] ^= 0xFF;   // estropear un registro
    mp_idx3_entry *v2 = NULL;
    size_t m2 = 0;
    MP_CHECK(mp_idx3_parse(d, len - 16, &v2, &m2, &e2) == -1);   // además, recortado
    mp_idx3_entries_free(v, m);
    free(d);
    free(rows);
    mp_source_close(s);
}

// Deja el índice y su lista en MP_TEST_TMP/idx3 para que check_idx3.py los compare con Python.
MP_TEST(idx3_writes_fixture) {
    mp_source *s = NULL;
    mp_idx3_row *rows = NULL;
    size_t n = basic_rows(&s, &rows);
    if (!n) return;
    uint8_t *d = NULL;
    size_t len = 0;
    mp_err e = {{0}};
    MP_CHECK(mp_idx3_build(rows, n, &d, &len, &e) == 0);
    MP_CHECK(mp_make_dirs(TMP, &e) == 0);
    MP_CHECK(d && mp_write_file(TMP "/basic.index", d, len, &e) == 0);
    size_t cap = 1 << 20, k = 0;
    char *tsv = malloc(cap);
    for (size_t i = 0; i < n; i++)
        k += (size_t)snprintf(tsv + k, cap - k, "%llu\t%lld\t%s\n", (unsigned long long)rows[i].size,
                              (long long)rows[i].mtime, rows[i].path);
    MP_CHECK(mp_write_file(TMP "/basic.tsv", tsv, k, &e) == 0);
    free(tsv);
    free(d);
    free(rows);
    mp_source_close(s);
}

MP_TEST(idx3_find_case_insensitive) {
    mp_source *s = NULL;
    mp_idx3_row *rows = NULL;
    size_t n = basic_rows(&s, &rows);
    if (!n) return;
    uint8_t *d = NULL;
    size_t len = 0;
    mp_err e = {{0}};
    MP_CHECK(mp_idx3_build(rows, n, &d, &len, &e) == 0);
    mp_idx3_entry *v = NULL;
    size_t m = 0;
    MP_CHECK(d && mp_idx3_parse(d, len, &v, &m, &e) == 0);
    size_t bad = 0;
    for (size_t i = 0; i < n; i++) {
        long k = mp_idx3_find(v, m, rows[i].path);
        bad += k < 0 || strcmp(v[k].app0_path + 6, rows[i].path) != 0;
    }
    MP_CHECK_EQ_U64(bad, 0);
    MP_CHECK(mp_idx3_find(v, m, "EBOOT.BIN") >= 0);   // sin distinguir mayúsculas
    MP_CHECK(mp_idx3_find(v, m, "no/existe") == -1);
    mp_idx3_entries_free(v, m);
    free(d);
    free(rows);
    mp_source_close(s);
}

static uint8_t *slurp_file(const char *path, size_t *len) {
    mp_file *f = NULL;
    uint64_t size = 0;
    size_t got = 0;
    *len = 0;
    if (mp_file_open_read(path, &f, NULL) != 0) return NULL;
    mp_file_size(f, &size, NULL);
    uint8_t *b = malloc((size_t)size + 1);
    if (b) mp_file_pread(f, b, (size_t)size, 0, &got, NULL);
    mp_file_close(f);
    *len = got;
    return b;
}

static int pack_folder(const char *dst, const char *runtime) {
    mp_err e = {{0}};
    mp_remove_tree(dst, &e);
    mp_pack_opts o;
    mp_pack_opts_default(&o);
    o.threads = 4;
    o.progress = 0;
    o.runtime = runtime;
    mp_pack_out out;
    if (mp_pack_out_dir(dst, &out, &e) != 0) {
        MP_FAIL_("pack_out_dir: %s", e.msg);
        return -1;
    }
    mp_pack_result r;
    int rc = mp_pack_run(MP_FIXTURES_DIR "/basic.exfat", &out, &o, &r, &e);
    out.destroy(out.ctx);
    if (rc) MP_FAIL_("pack: %s", e.msg);
    return rc;
}

MP_TEST(idx3_set_row_matches_pack_runtime) {
    mp_err e = {{0}};
    MP_CHECK(mp_make_dirs(TMP, &e) == 0);
    uint8_t rt[4096];
    for (size_t i = 0; i < sizeof rt; i++) rt[i] = (uint8_t)(i * 7);
    memcpy(rt + 100, "AMPRZPK1", 8);
    MP_CHECK(mp_write_file(TMP "/rt.sprx", rt, sizeof rt, &e) == 0);
    MP_CHECK(mp_set_mtime(TMP "/rt.sprx", 1700000000, &e) == 0);
    if (pack_folder(TMP "/sin_rt", NULL) || pack_folder(TMP "/con_rt", TMP "/rt.sprx")) return;
    size_t na = 0, nb = 0, nc = 0;
    uint8_t *a = slurp_file(TMP "/sin_rt/ampr_emu.index", &na);
    uint8_t *b = slurp_file(TMP "/con_rt/ampr_emu.index", &nb);
    uint8_t *c = NULL;
    MP_CHECK(a && b);
    MP_CHECK(a && mp_idx3_set_row(a, na, "fakelib/libSceAmpr.sprx", 4096, 1700000000, &c, &nc, &e) == 0);
    if (e.msg[0]) fprintf(stderr, "  %s\n", e.msg);
    MP_CHECK(c && nc == nb && !memcmp(c, b, nb));
    free(a);
    free(b);
    free(c);
}

MP_TEST(idx3_set_row_replaces) {
    mp_err e = {{0}};
    mp_idx3_row rows[2] = {{"a/uno.bin", 10, 100}, {"fakelib/libSceAmpr.sprx", 5, 200}};
    uint8_t *d = NULL, *x = NULL, *y = NULL;
    size_t n = 0, nx = 0, ny = 0;
    MP_CHECK(mp_idx3_build(rows, 2, &d, &n, &e) == 0);
    MP_CHECK(mp_idx3_set_row(d, n, "fakelib/libSceAmpr.sprx", 777, 300, &x, &nx, &e) == 0);
    MP_CHECK(x && mp_idx3_set_row(x, nx, "fakelib/libSceAmpr.sprx", 888, 400, &y, &ny, &e) == 0);
    mp_idx3_entry *v = NULL;
    size_t m = 0;
    MP_CHECK(y && mp_idx3_parse(y, ny, &v, &m, &e) == 0);
    MP_CHECK_EQ_U64(m, 2);
    long k = v ? mp_idx3_find(v, m, "fakelib/libSceAmpr.sprx") : -1;
    MP_CHECK(k >= 0 && v[k].size == 888 && v[k].mtime == 400);
    long u = v ? mp_idx3_find(v, m, "a/uno.bin") : -1;
    MP_CHECK(u >= 0 && v[u].size == 10 && v[u].mtime == 100);
    mp_idx3_entries_free(v, m);
    free(d);
    free(x);
    free(y);
}

MP_TEST(idx3_set_row_bad_input) {
    mp_err e = {{0}};
    mp_idx3_row rows[1] = {{"a.bin", 1, 1}};
    uint8_t *d = NULL, *x = NULL;
    size_t n = 0, nx = 0;
    MP_CHECK(mp_idx3_build(rows, 1, &d, &n, &e) == 0);
    if (!d) return;
    d[0] = 'X';
    mp_err e2 = {{0}};
    MP_CHECK(mp_idx3_set_row(d, n, "fakelib/libSceAmpr.sprx", 1, 1, &x, &nx, &e2) == -1);
    MP_CHECK(x == NULL && e2.msg[0] != 0);
    free(d);
}

// Un índice de otro generador (el propio de un paquete, p. ej. Crisis Core) puede dejar la tabla de
// slots en un offset múltiplo de 8 pero no de 16. Lo único que exige el formato es que quepa tras las
// rutas y que cada slot (16 B) esté alineado a 8.
static void put64(uint8_t *p, uint64_t v) {
    for (int i = 0; i < 8; i++) p[i] = (uint8_t)(v >> (8 * i));
}

// Copia de idx con la tabla de slots 'pad' bytes más allá (y la cabecera apuntando ahí).
static uint8_t *shift_slots(const uint8_t *idx, size_t len, size_t pad, size_t *out_len) {
    uint64_t hash_off = rd64(idx + 32);
    uint8_t *d = calloc(1, len + pad);
    memcpy(d, idx, (size_t)hash_off);
    memcpy(d + hash_off + pad, idx + hash_off, len - (size_t)hash_off);
    put64(d + 32, hash_off + pad);
    *out_len = len + pad;
    return d;
}

MP_TEST(idx3_parse_accepts_8_aligned_slots) {
    mp_idx3_row rows[3] = {{"B.bin", 5, 100}, {"a.bin", 7, 200}, {"dir/c.bin", 9, 300}};
    uint8_t *d = NULL, *a8 = NULL;
    size_t n = 0, n8 = 0;
    mp_err e = {{0}};
    MP_CHECK(mp_idx3_build(rows, 3, &d, &n, &e) == 0);
    if (!d) return;
    a8 = shift_slots(d, n, 8, &n8);
    MP_CHECK_EQ_U64(rd64(a8 + 32) % 16, 8);
    mp_idx3_entry *v = NULL;
    size_t m = 0;
    memset(&e, 0, sizeof e);
    MP_CHECK(mp_idx3_parse(a8, n8, &v, &m, &e) == 0);
    if (e.msg[0]) MP_FAIL_("%s", e.msg);
    MP_CHECK_EQ_U64(m, 3);
    if (v && m == 3) {
        MP_CHECK_STR(v[0].app0_path, "/app0/a.bin");
        MP_CHECK_STR(v[1].app0_path, "/app0/B.bin");
        MP_CHECK_STR(v[2].app0_path, "/app0/dir/c.bin");
        MP_CHECK(mp_idx3_find(v, m, "DIR/C.BIN") == 2);
    }
    mp_idx3_entries_free(v, m);
    // cambiar una fila lo reescribe en el formato propio (alineado a 16)
    uint8_t *w = NULL;
    size_t wl = 0;
    memset(&e, 0, sizeof e);
    MP_CHECK(mp_idx3_set_row(a8, n8, "a.bin", 11, 400, &w, &wl, &e) == 0);
    if (w) {
        MP_CHECK_EQ_U64(rd64(w + 32) % 16, 0);
        mp_idx3_entry *v2 = NULL;
        size_t m2 = 0;
        MP_CHECK(mp_idx3_parse(w, wl, &v2, &m2, &e) == 0);
        MP_CHECK(m2 == 3 && v2 && v2[0].size == 11 && v2[0].mtime == 400);
        mp_idx3_entries_free(v2, m2);
    }
    free(w);
    free(a8);

    // sin alinear a 8, no: los slots llevan u64
    uint8_t *a4 = shift_slots(d, n, 4, &n8);
    memset(&e, 0, sizeof e);
    MP_CHECK(mp_idx3_parse(a4, n8, &v, &m, &e) == -1);
    MP_CHECK(e.msg[0] != 0);
    free(a4);
    free(d);
}

int main(void) {
    MP_RUN(idx3_set_row_matches_pack_runtime);
    MP_RUN(idx3_set_row_replaces);
    MP_RUN(idx3_set_row_bad_input);
    MP_RUN(idx3_find_case_insensitive);
    MP_RUN(idx3_parse_accepts_8_aligned_slots);
    MP_RUN(idx3_two_rows);
    MP_RUN(idx3_rejects_case_dup);
    MP_RUN(idx3_parse_roundtrip);
    MP_RUN(idx3_writes_fixture);
    MP_TEST_MAIN_END();
}
