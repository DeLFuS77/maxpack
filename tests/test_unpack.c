// SPDX-License-Identifier: GPL-3.0-or-later
#include <stdio.h>
#include <stdlib.h>

#include "mp_hash.h"
#include "mp_p5z.h"
#include "mp_pack.h"
#include "mp_platform.h"
#include "mp_test.h"
#include "mp_fixture.h"

#define TMP MP_TEST_TMP "/unpack"
#define BASIC MP_FIXTURES_DIR "/basic.exfat"

// Utilidades comunes de las pruebas (antes estaban aquí).
#define strlist mpt_list
#define list_dir mpt_list_dir
#define free_list mpt_list_free
#define clear_dir mpt_clear
#define slurp mpt_slurp
#define pack_any mpt_pack

static mp_p5z *open_p5z(const char *path) {
    mp_err e = {{0}};
    mp_p5z *p = NULL;
    if (mp_p5z_open(path, &p, &e) != 0) MP_FAIL_("p5z_open: %s", e.msg);
    return p;
}

MP_TEST(p5z_folder_roundtrip) {
    if (pack_any(BASIC, TMP "/basic.p5z", 1, NULL) || pack_any(BASIC, TMP "/folder", 0, NULL)) return;
    mp_p5z *p = open_p5z(TMP "/basic.p5z");
    if (!p) return;
    mp_err e = {{0}};
    clear_dir(TMP "/from_p5z");
    MP_CHECK(mp_unpack_folder(p, TMP "/from_p5z", 4, 0, &e) == 0);
    if (e.msg[0]) fprintf(stderr, "  %s\n", e.msg);
    mp_p5z_close(p);
    strlist a = list_dir(TMP "/folder"), b = list_dir(TMP "/from_p5z");
    MP_CHECK_EQ_U64(a.n, b.n);
    for (size_t i = 0; i < a.n && i < b.n; i++) {
        char pa[2048], pb[2048];
        snprintf(pa, sizeof pa, "%s/%s", TMP "/folder", a.v[i]);
        snprintf(pb, sizeof pb, "%s/%s", TMP "/from_p5z", b.v[i]);
        size_t x = 0, y = 0;
        uint8_t *ba = slurp(pa, &x), *bb = slurp(pb, &y);
        int64_t ma = 0, mb = 0;
        mp_stat(pa, NULL, &ma);
        mp_stat(pb, NULL, &mb);
        int is_vol = strstr(a.v[i], ".pak") != NULL;   // los volúmenes no llevan fecha fija
        if (strcmp(a.v[i], b.v[i]) || !ba || !bb || x != y || memcmp(ba, bb, x) || (!is_vol && ma != mb))
            MP_FAIL_("distinto: %s", a.v[i]);
        free(ba);
        free(bb);
    }
    free_list(&a);
    free_list(&b);
}

MP_TEST(p5z_original_roundtrip) {
    mp_p5z *p = open_p5z(TMP "/basic.p5z");
    if (!p) return;
    mp_err e = {{0}};
    clear_dir(TMP "/original");
    MP_CHECK(mp_unpack_original(p, TMP "/original", 4, 0, &e) == 0);
    if (e.msg[0]) fprintf(stderr, "  %s\n", e.msg);
    mp_p5z_close(p);
    mp_source *s = NULL;
    MP_CHECK(mp_source_open(BASIC, &s, &e) == 0);
    strlist got = list_dir(TMP "/original");
    MP_CHECK(s && got.n == mp_source_count(s));
    for (size_t i = 0; s && i < mp_source_count(s) && i < got.n; i++) {
        const mp_entry *en = mp_source_entry(s, i);
        char pth[2048];
        snprintf(pth, sizeof pth, "%s/%s", TMP "/original", en->path);
        size_t len = 0;
        uint8_t *b = slurp(pth, &len), *a = malloc((size_t)en->size + 1);
        mp_source_read(s, i, 0, a, (size_t)en->size, &e);
        int64_t mt = 0;
        mp_stat(pth, NULL, &mt);
        if (!b || len != en->size || memcmp(a, b, len) || mt != en->mtime) MP_FAIL_("distinto: %s", en->path);
        free(a);
        free(b);
    }
    free_list(&got);
    mp_source_close(s);

    // Con originales reemplazados: vuelven el ampr_emu.index y el runtime antiguos.
    MP_CHECK(mp_make_dirs(TMP "/src_ampr/fakelib", &e) == 0);
    MP_CHECK(mp_write_file(TMP "/src_ampr/datos.bin", "datos datos datos datos datos", 29, &e) == 0);
    MP_CHECK(mp_write_file(TMP "/src_ampr/ampr_emu.index", "VIEJO", 5, &e) == 0);
    MP_CHECK(mp_write_file(TMP "/src_ampr/fakelib/libSceAmpr.sprx", "antiguo", 7, &e) == 0);
    MP_CHECK(mp_write_file(TMP "/runtime.sprx", "nuevo AMPRZPK1", 14, &e) == 0);
    if (pack_any(TMP "/src_ampr", TMP "/ampr.p5z", 1, TMP "/runtime.sprx")) return;
    p = open_p5z(TMP "/ampr.p5z");
    if (!p) return;
    clear_dir(TMP "/ampr_orig");
    MP_CHECK(mp_unpack_original(p, TMP "/ampr_orig", 2, 0, &e) == 0);
    mp_p5z_close(p);
    size_t l1 = 0, l2 = 0;
    uint8_t *i1 = slurp(TMP "/ampr_orig/ampr_emu.index", &l1), *r1 = slurp(TMP "/ampr_orig/fakelib/libSceAmpr.sprx", &l2);
    MP_CHECK(i1 && l1 == 5 && !memcmp(i1, "VIEJO", 5));
    MP_CHECK(r1 && l2 == 7 && !memcmp(r1, "antiguo", 7));
    free(i1);
    free(r1);
}

MP_TEST(p5z_truncated_fails) {
    size_t n = 0;
    uint8_t *d = slurp(TMP "/basic.p5z", &n);
    MP_CHECK(d && n > 200);
    if (!d) return;
    mp_err e = {{0}};
    remove(TMP "/corto.p5z");
    MP_CHECK(mp_write_file(TMP "/corto.p5z", d, n - 10, &e) == 0);
    mp_p5z *p = NULL;
    MP_CHECK(mp_p5z_open(TMP "/corto.p5z", &p, &e) == -1);
    MP_CHECK_CONTAINS(e.msg, "truncado");
    d[n - 40] ^= 0x10;   // dentro del índice comprimido
    remove(TMP "/malo.p5z");
    MP_CHECK(mp_write_file(TMP "/malo.p5z", d, n, &e) == 0);
    mp_err e2 = {{0}};
    MP_CHECK(mp_p5z_open(TMP "/malo.p5z", &p, &e2) == -1);
    MP_CHECK_CONTAINS(e2.msg, "CRC");
    free(d);
}

MP_TEST(p5z_info_text) {
    mp_p5z *p = open_p5z(TMP "/basic.p5z");
    if (!p) return;
    FILE *f = tmpfile();
    MP_CHECK(f && mp_p5z_info(p, f) == 0);
    char buf[4096] = {0};
    if (f) {
        rewind(f);
        size_t k = fread(buf, 1, sizeof buf - 1, f);
        buf[k] = 0;
        fclose(f);
    }
    MP_CHECK_CONTAINS(buf, "TEST00001");
    MP_CHECK_EQ_U64(mp_p5z_manifest(p)->n, 213);
    mp_p5z_close(p);
}

int main(void) {
    MP_RUN(p5z_folder_roundtrip);
    MP_RUN(p5z_original_roundtrip);
    MP_RUN(p5z_truncated_fails);
    MP_RUN(p5z_info_text);
    MP_TEST_MAIN_END();
}
