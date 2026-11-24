// SPDX-License-Identifier: GPL-3.0-or-later
#include <stdio.h>
#include <stdlib.h>

#include "mp_describe.h"
#include "mp_fixture.h"
#include "mp_p5z_internal.h"

#define TMP MP_TEST_TMP "/describe"

// Volumes of a folder's packs: its ampr_zstd-NNN.pak files.
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

    // exFAT image.
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

    // unpack --original folder: the same.
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

    // .p5z with a runtime that reads zstd.
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
    // the packs: the files the manifest marks as packed, and the volumes that unpack leaves
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

    // Already packed folder (pack --folder): packed and new runtime.
    mpt_pack(MPT_BASIC, TMP "/packed", 0, TMP "/R.sprx");
    mp_describe g;
    MP_CHECK(mp_describe_path(TMP "/packed", &g, &e) == 0);
    MP_CHECK_STR(g.kind, "folder");
    MP_CHECK(g.packed == 1 && g.has_zstd_runtime == 1);
    MP_CHECK_EQ_U64(g.packed_files, z.packed_files);
    MP_CHECK_EQ_U64(g.volumes, pak_count(TMP "/packed"));

    // Does not exist.
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
    // pack stores the same name in the .p5z's manifest.
    mpt_pack(TMP "/lang", TMP "/lang.p5z", 1, NULL);
    mp_p5z *p = open_p5z(TMP "/lang.p5z");
    if (!p) return;
    MP_CHECK_STR(mp_p5z_manifest(p)->title_name, "Espa\xc3\xb1ol");
    mp_p5z_close(p);
    mp_describe z;
    MP_CHECK(mp_describe_path(TMP "/lang.p5z", &z, &e) == 0);
    MP_CHECK_STR(z.name, d.name);
    // Without a default language: the top-level one.
    mpt_put_text(TMP "/lang/sce_sys/param.json",
                 "{\"titleId\":\"LANG00001\",\"titleName\":\"Top\",\"localizedParameters\":{\"en-US\":{\"titleName\":\"English\"}}}");
    MP_CHECK(mp_describe_path(TMP "/lang", &d, &e) == 0);
    MP_CHECK_STR(d.name, "Top");
}

MP_TEST(describe_name_truncated_on_boundary) {
    mp_err e = {{0}};
    mp_describe d;
    char json[1024] = "{\"titleId\":\"LONG00001\",\"titleName\":\"";
    for (int i = 0; i < 100; i++) strcat(json, "\xc3\xa9");   // 200 bytes; 127 fit
    strcat(json, "\"}");
    mpt_clear(TMP "/long");
    mpt_put_text(TMP "/long/sce_sys/param.json", json);
    MP_CHECK(mp_describe_path(TMP "/long", &d, &e) == 0);
    size_t n = strlen(d.name);
    MP_CHECK_EQ_U64(n, 126);   // 63 characters of 2 bytes: it does not split the 64th
    MP_CHECK((unsigned char)d.name[n - 1] == 0xa9 && (unsigned char)d.name[n - 2] == 0xc3);
}

// A param.json with a UTF-8 BOM, or with trailing spaces and NULs (as some
// tools produce), reads the same: describe gives its TITLE_ID and pack stores it in the .p5z, which without it
// the installer rejects.
MP_TEST(describe_param_bom_and_padding) {
    static const char body[] = "{\"titleId\":\"BOMS00001\",\"contentVersion\":\"01.000.000\",\"titleName\":\"Con BOM\"}";
    struct {
        const char *name;
        char text[256];
        size_t len;
    } cases[3];
    size_t n = sizeof body - 1;
    cases[0].name = "bom";
    memcpy(cases[0].text, "\xef\xbb\xbf", 3);
    memcpy(cases[0].text + 3, body, n);
    cases[0].len = n + 3;
    cases[1].name = "nul";
    memcpy(cases[1].text, body, n);
    memset(cases[1].text + n, 0, 16);
    cases[1].len = n + 16;
    cases[2].name = "todo";
    memcpy(cases[2].text, "\xef\xbb\xbf", 3);
    memcpy(cases[2].text + 3, body, n);
    memcpy(cases[2].text + 3 + n, "\r\n \t\0\0\0", 7);
    cases[2].len = n + 10;
    for (size_t i = 0; i < 3; i++) {
        char dir[256], file[300], p5z[300];
        snprintf(dir, sizeof dir, TMP "/pad_%s", cases[i].name);
        snprintf(file, sizeof file, "%s/sce_sys/param.json", dir);
        snprintf(p5z, sizeof p5z, "%s.p5z", dir);
        mpt_clear(dir);
        mpt_put(file, cases[i].text, cases[i].len);
        mp_err e = {{0}};
        mp_describe d;
        MP_CHECK(mp_describe_path(dir, &d, &e) == 0);
        if (strcmp(d.title_id, "BOMS00001") || strcmp(d.name, "Con BOM"))
            MP_FAIL_("%s: describe da \"%s\" \"%s\"", cases[i].name, d.title_id, d.name);
        mpt_pack(dir, p5z, 1, NULL);
        mp_p5z *p = open_p5z(p5z);
        if (!p) continue;
        if (strcmp(mp_p5z_manifest(p)->title_id, "BOMS00001") || strcmp(mp_p5z_manifest(p)->content_version, "01.000.000"))
            MP_FAIL_("%s: the .p5z stores \"%s\"", cases[i].name, mp_p5z_manifest(p)->title_id);
        mp_p5z_close(p);
    }
}

// The content ID of param.json, whose first letters are the region, for every kind of game: a
// folder, an .exfat image and a .p5z (from the param.json it carries, so a .p5z of before says it
// too). A game whose param.json gives none, or has none, says "".
MP_TEST(describe_content_id) {
    mp_err e = {{0}};
    mp_describe d;
    MP_CHECK(mp_describe_path(MP_FIXTURES_DIR "/cid.exfat", &d, &e) == 0);
    MP_CHECK_STR(d.kind, "exfat");
    MP_CHECK_STR(d.title_id, "TEST00003");
    MP_CHECK_STR(d.content_id, "EP9000-TEST00003_00-MAXPACKCONTENTID");
    mpt_pack(MP_FIXTURES_DIR "/cid.exfat", TMP "/cid.p5z", 1, NULL);
    MP_CHECK(mp_describe_path(TMP "/cid.p5z", &d, &e) == 0);
    MP_CHECK_STR(d.kind, "p5z");
    MP_CHECK_STR(d.content_id, "EP9000-TEST00003_00-MAXPACKCONTENTID");
    mp_p5z *p = open_p5z(TMP "/cid.p5z");
    if (!p) return;
    MP_CHECK(mp_unpack_original(p, TMP "/cidf", 2, 0, &e) == 0);
    mp_p5z_close(p);
    MP_CHECK(mp_describe_path(TMP "/cidf", &d, &e) == 0);
    MP_CHECK_STR(d.kind, "folder");
    MP_CHECK_STR(d.content_id, "EP9000-TEST00003_00-MAXPACKCONTENTID");
    // with a BOM and the padding of some tools, and an ID longer than the field: cut, never split
    mpt_clear(TMP "/cidb");
    char json[512];
    snprintf(json, sizeof json, "\xef\xbb\xbf{\"titleId\":\"TEST00004\",\"contentId\":\"UP9000-TEST00004_00-%s\"}  \r\n",
             "0123456789ABCDEF0123456789ABCDEF");
    mpt_put_text(TMP "/cidb/sce_sys/param.json", json);
    mpt_put_text(TMP "/cidb/eboot.bin", "x");
    MP_CHECK(mp_describe_path(TMP "/cidb", &d, &e) == 0);
    MP_CHECK_EQ_U64(strlen(d.content_id), sizeof d.content_id - 1);
    MP_CHECK(!strncmp(d.content_id, "UP9000-TEST00004_00-0123456789ABCDEF", 36));
    // none
    MP_CHECK(mp_describe_path(MPT_BASIC, &d, &e) == 0);
    MP_CHECK_STR(d.content_id, "");
    mpt_pack(MPT_BASIC, TMP "/nocid.p5z", 1, NULL);
    MP_CHECK(mp_describe_path(TMP "/nocid.p5z", &d, &e) == 0);
    MP_CHECK_STR(d.content_id, "");
    mpt_clear(TMP "/noparam");
    mpt_put_text(TMP "/noparam/eboot.bin", "x");
    MP_CHECK(mp_describe_path(TMP "/noparam", &d, &e) == 0);
    MP_CHECK_STR(d.content_id, "");
}

MP_TEST(describe_loose_bytes) {
    mp_err e = {{0}};
    mp_describe d;
    mpt_pack(MPT_BASIC, TMP "/lb.p5z", 1, NULL);
    mp_p5z *p = open_p5z(TMP "/lb.p5z");
    if (!p) return;
    uint64_t want = 0;
    for (size_t i = 0; i < p->nfiles; i++)
        if (p->files[i].role == MP_ROLE_LOOSE || p->files[i].role == MP_ROLE_ZPK_INDEX || p->files[i].role == MP_ROLE_IDX3)
            want += p->files[i].size;
    mp_p5z_close(p);
    MP_CHECK(want > 0);
    MP_CHECK(mp_describe_path(TMP "/lb.p5z", &d, &e) == 0);
    MP_CHECK_EQ_U64(d.loose_bytes, want);
    MP_CHECK(mp_describe_path(MPT_BASIC, &d, &e) == 0);
    MP_CHECK_EQ_U64(d.loose_bytes, 0);
    mpt_clear(TMP "/lbf");
    p = open_p5z(TMP "/lb.p5z");
    if (!p) return;
    MP_CHECK(mp_unpack_original(p, TMP "/lbf", 2, 0, &e) == 0);
    mp_p5z_close(p);
    MP_CHECK(mp_describe_path(TMP "/lbf", &d, &e) == 0);
    MP_CHECK_EQ_U64(d.loose_bytes, 0);
}

int main(void) {
    MP_RUN(describe_param_bom_and_padding);
    MP_RUN(describe_kinds);
    MP_RUN(describe_runtime_old);
    MP_RUN(describe_icon);
    MP_RUN(describe_name_chars);
    MP_RUN(describe_default_language);
    MP_RUN(describe_name_truncated_on_boundary);
    MP_RUN(describe_loose_bytes);
    MP_RUN(describe_content_id);
    MP_TEST_MAIN_END();
}
