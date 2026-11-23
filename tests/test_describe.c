// SPDX-License-Identifier: GPL-3.0-or-later
#include <stdio.h>
#include <stdlib.h>

#include "mp_describe.h"
#include "mp_fixture.h"

#define TMP MP_TEST_TMP "/describe"

static mp_p5z *open_p5z(const char *path) {
    mp_err e = {{0}};
    mp_p5z *p = NULL;
    if (mp_p5z_open(path, &p, &e) != 0) MP_FAIL_("p5z_open: %s", e.msg);
    return p;
}

MP_TEST(describe_kinds) {
    mp_err e = {{0}};
    mp_describe d;
    mpt_clear(TMP);

    // Imagen exFAT.
    MP_CHECK(mp_describe_path(MPT_BASIC, &d, &e) == 0);
    MP_CHECK_STR(d.kind, "exfat");
    MP_CHECK_STR(d.title_id, "TEST00001");
    MP_CHECK_STR(d.version, "01.000.000");
    MP_CHECK(d.files > 0);
    MP_CHECK_EQ_U64(d.p5z_size, 0);
    MP_CHECK_EQ_U64(d.install_size, d.original_size);
    MP_CHECK(d.has_runtime == 0 && d.has_zstd_runtime == 0 && d.packed == 0);

    // Carpeta de unpack --original: lo mismo.
    mpt_pack(MPT_BASIC, TMP "/a.p5z", 1, NULL);
    mp_p5z *p = open_p5z(TMP "/a.p5z");
    if (!p) return;
    MP_CHECK(mp_unpack_original(p, TMP "/orig", 2, 0, &e) == 0);
    mp_p5z_close(p);
    mp_describe f;
    MP_CHECK(mp_describe_path(TMP "/orig", &f, &e) == 0);
    MP_CHECK_STR(f.kind, "folder");
    MP_CHECK_STR(f.title_id, "TEST00001");
    MP_CHECK_EQ_U64(f.original_size, d.original_size);
    MP_CHECK_EQ_U64(f.original_size, mpt_dir_size(TMP "/orig"));
    MP_CHECK_EQ_U64(f.files, d.files);

    // .p5z con runtime que lee zstd.
    uint8_t rt[256];
    for (size_t i = 0; i < sizeof rt; i++) rt[i] = (uint8_t)(i * 7 + 3);
    memcpy(rt + 40, "AMPRZPK1", 8);
    mpt_put(TMP "/R.sprx", rt, sizeof rt);
    mpt_pack(MPT_BASIC, TMP "/b.p5z", 1, TMP "/R.sprx");
    mp_describe z;
    MP_CHECK(mp_describe_path(TMP "/b.p5z", &z, &e) == 0);
    MP_CHECK_STR(z.kind, "p5z");
    MP_CHECK_STR(z.title_id, "TEST00001");
    MP_CHECK_STR(z.version, "01.000.000");
    MP_CHECK_EQ_U64(z.original_size, d.original_size);
    MP_CHECK_EQ_U64(z.files, d.files);
    MP_CHECK(z.has_runtime == 1 && z.has_zstd_runtime == 1 && z.packed == 1);
    p = open_p5z(TMP "/b.p5z");
    if (!p) return;
    MP_CHECK(mp_unpack_folder(p, TMP "/fold", 2, 0, &e) == 0);
    mp_p5z_close(p);
    MP_CHECK_EQ_U64(z.install_size, mpt_dir_size(TMP "/fold"));
    FILE *fp = fopen(TMP "/b.p5z", "rb");
    if (fp) {
        fseek(fp, 0, SEEK_END);
        MP_CHECK_EQ_U64(z.p5z_size, ftell(fp));
        fclose(fp);
    }

    // Carpeta ya empaquetada (pack --folder): packed y runtime nuevo.
    mpt_pack(MPT_BASIC, TMP "/packed", 0, TMP "/R.sprx");
    mp_describe g;
    MP_CHECK(mp_describe_path(TMP "/packed", &g, &e) == 0);
    MP_CHECK_STR(g.kind, "folder");
    MP_CHECK(g.packed == 1 && g.has_zstd_runtime == 1);

    // No existe.
    MP_CHECK(mp_describe_path(TMP "/nada", &d, &e) != 0 && e.msg[0]);
}

MP_TEST(describe_runtime_old) {
    mp_err e = {{0}};
    mp_describe d;
    mpt_clear(TMP "/old");
    mpt_put_text(TMP "/old/sce_sys/param.json", "{\"titleId\":\"OLD000001\",\"contentVersion\":\"01.002.000\"}");
    mpt_put_text(TMP "/old/fakelib/libSceAmpr.sprx", "runtime viejo sin marca");
    mpt_put_text(TMP "/old/eboot.bin", "x");
    MP_CHECK(mp_describe_path(TMP "/old", &d, &e) == 0);
    MP_CHECK(d.has_runtime == 1);
    MP_CHECK(d.has_zstd_runtime == 0);
    MP_CHECK(d.packed == 0);
    MP_CHECK_STR(d.title_id, "OLD000001");
    MP_CHECK_STR(d.version, "01.002.000");
}

MP_TEST(describe_icon) {
    mp_err e = {{0}};
    uint8_t png[64];
    for (size_t i = 0; i < sizeof png; i++) png[i] = (uint8_t)(i * 5 + 1);
    mpt_clear(TMP "/ic");
    mpt_put_text(TMP "/ic/sce_sys/param.json", "{\"titleId\":\"ICON00001\",\"contentVersion\":\"01.000.000\"}");
    mpt_put(TMP "/ic/sce_sys/icon0.png", png, sizeof png);
    mpt_put_text(TMP "/ic/eboot.bin", "x");
    uint8_t *got = NULL;
    size_t len = 0;
    MP_CHECK(mp_describe_icon(TMP "/ic", &got, &len, &e) == 0);
    MP_CHECK_EQ_U64(len, sizeof png);
    MP_CHECK(got && len == sizeof png && memcmp(got, png, sizeof png) == 0);
    free(got);

    mpt_pack(TMP "/ic", TMP "/ic.p5z", 1, NULL);
    got = NULL;
    len = 0;
    MP_CHECK(mp_describe_icon(TMP "/ic.p5z", &got, &len, &e) == 0);
    MP_CHECK(got && len == sizeof png && memcmp(got, png, sizeof png) == 0);
    free(got);

    got = NULL;
    MP_CHECK(mp_describe_icon(MPT_BASIC, &got, &len, &e) == -1);
    MP_CHECK(got == NULL);
}

MP_TEST(describe_name_chars) {
    mp_err e = {{0}};
    mp_describe d;
    mpt_clear(TMP "/nm");
    mpt_put_text(TMP "/nm/sce_sys/param.json", "{\"titleId\":\"NAME00001\",\"titleName\":\"A \\\"B\\\" caf\xc3\xa9\"}");
    MP_CHECK(mp_describe_path(TMP "/nm", &d, &e) == 0);
    MP_CHECK_STR(d.name, "A \"B\" caf\xc3\xa9");
}

int main(void) {
    MP_RUN(describe_kinds);
    MP_RUN(describe_runtime_old);
    MP_RUN(describe_icon);
    MP_RUN(describe_name_chars);
    MP_TEST_MAIN_END();
}
