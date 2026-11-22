// SPDX-License-Identifier: GPL-3.0-or-later
#include <stdio.h>
#include <stdlib.h>

#include "mp_hash.h"
#include "mp_idx3.h"
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

int main(void) {
    MP_RUN(idx3_find_case_insensitive);
    MP_RUN(idx3_two_rows);
    MP_RUN(idx3_rejects_case_dup);
    MP_RUN(idx3_parse_roundtrip);
    MP_RUN(idx3_writes_fixture);
    MP_TEST_MAIN_END();
}
