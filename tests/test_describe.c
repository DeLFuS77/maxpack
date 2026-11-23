// SPDX-License-Identifier: GPL-3.0-or-later
#include <stdio.h>
#include <stdlib.h>

#include "mp_describe.h"
#include "mp_fixture.h"

#define TMP MP_TEST_TMP "/describe"

// Volúmenes de los packs de una carpeta: sus ampr_zstd-NNN.pak.
static uint64_t pak_count(const char *dir) {
    mpt_list l = mpt_list_dir(dir);
    uint64_t n = 0;
    for (size_t i = 0; i < l.n; i++) {
        size_t len = strlen(l.v[i]);
        if (!strncmp(l.v[i], "ampr_zstd-", 10) && len > 14 && !strcmp(l.v[i] + len - 4, ".pak")) n++;
    }
    mpt_list_free(&l);
    return n;
}

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
    MP_CHECK_EQ_U64(d.packed_files, 0);
    MP_CHECK_EQ_U64(d.volumes, 0);

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
    MP_CHECK(f.packed == 0 && f.packed_files == 0 && f.volumes == 0);

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
    // los packs: los archivos que el manifiesto marca empaquetados, y los volúmenes que deja unpack
    p = open_p5z(TMP "/b.p5z");
    if (!p) return;
    const mp_pack_manifest *m = mp_p5z_manifest(p);
    uint64_t in_packs = 0;
    for (size_t i = 0; i < m->n; i++)
        if (m->v[i].flags & MP_MANIFEST_PACKED) in_packs++;
    mp_p5z_close(p);
    MP_CHECK(in_packs > 0 && in_packs < z.files);
    MP_CHECK_EQ_U64(z.packed_files, in_packs);
    MP_CHECK(pak_count(TMP "/fold") >= 1);
    MP_CHECK_EQ_U64(z.volumes, pak_count(TMP "/fold"));
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
    MP_CHECK_EQ_U64(g.packed_files, z.packed_files);
    MP_CHECK_EQ_U64(g.volumes, pak_count(TMP "/packed"));

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
    MP_CHECK(d.packed == 0 && d.packed_files == 0 && d.volumes == 0);
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
    mpt_put_text(TMP "/nm/sce_sys/param.json",
                 "{\"titleId\":\"NAME00001\",\"titleName\":\"A \\\"B\\\" \\\\ caf\xc3\xa9 Game\\u2122 \\ud83c\\udfae\\n\"}");
    MP_CHECK(mp_describe_path(TMP "/nm", &d, &e) == 0);
    MP_CHECK_STR(d.name, "A \"B\" \\ caf\xc3\xa9 Game\xe2\x84\xa2 \xf0\x9f\x8e\xae\n");
}

MP_TEST(describe_default_language) {
    mp_err e = {{0}};
    mp_describe d;
    mpt_clear(TMP "/lang");
    mpt_put_text(TMP "/lang/sce_sys/param.json",
                 "{\"titleId\":\"LANG00001\",\"contentVersion\":\"01.000.000\",\"titleName\":\"Top\","
                 "\"localizedParameters\":{\"en-US\":{\"titleName\":\"English\"},"
                 "\"es-ES\":{\"titleName\":\"Espa\\u00f1ol\"},\"defaultLanguage\":\"es-ES\"}}");
    MP_CHECK(mp_describe_path(TMP "/lang", &d, &e) == 0);
    MP_CHECK_STR(d.name, "Espa\xc3\xb1ol");
    MP_CHECK_STR(d.title_id, "LANG00001");
    // pack guarda el mismo nombre en el manifiesto del .p5z.
    mpt_pack(TMP "/lang", TMP "/lang.p5z", 1, NULL);
    mp_p5z *p = open_p5z(TMP "/lang.p5z");
    if (!p) return;
    MP_CHECK_STR(mp_p5z_manifest(p)->title_name, "Espa\xc3\xb1ol");
    mp_p5z_close(p);
    mp_describe z;
    MP_CHECK(mp_describe_path(TMP "/lang.p5z", &z, &e) == 0);
    MP_CHECK_STR(z.name, d.name);
    // Sin idioma por defecto: el de primer nivel.
    mpt_put_text(TMP "/lang/sce_sys/param.json",
                 "{\"titleId\":\"LANG00001\",\"titleName\":\"Top\",\"localizedParameters\":{\"en-US\":{\"titleName\":\"English\"}}}");
    MP_CHECK(mp_describe_path(TMP "/lang", &d, &e) == 0);
    MP_CHECK_STR(d.name, "Top");
}

MP_TEST(describe_name_truncated_on_boundary) {
    mp_err e = {{0}};
    mp_describe d;
    char json[1024] = "{\"titleId\":\"LONG00001\",\"titleName\":\"";
    for (int i = 0; i < 100; i++) strcat(json, "\xc3\xa9");   // 200 bytes; caben 127
    strcat(json, "\"}");
    mpt_clear(TMP "/long");
    mpt_put_text(TMP "/long/sce_sys/param.json", json);
    MP_CHECK(mp_describe_path(TMP "/long", &d, &e) == 0);
    size_t n = strlen(d.name);
    MP_CHECK_EQ_U64(n, 126);   // 63 caracteres de 2 bytes: no parte el 64.º
    MP_CHECK((unsigned char)d.name[n - 1] == 0xa9 && (unsigned char)d.name[n - 2] == 0xc3);
}

int main(void) {
    MP_RUN(describe_kinds);
    MP_RUN(describe_runtime_old);
    MP_RUN(describe_icon);
    MP_RUN(describe_name_chars);
    MP_RUN(describe_default_language);
    MP_RUN(describe_name_truncated_on_boundary);
    MP_TEST_MAIN_END();
}
