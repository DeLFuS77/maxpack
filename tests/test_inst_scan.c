// SPDX-License-Identifier: GPL-3.0-or-later
// Installer: open and find .p5z files, find copies of the game and the registry.
#include "mp_fixture.h"
#include "mp_inst.h"

#define TMP MP_TEST_TMP "/inst_scan"

static const char *cands[] = {TMP "/usb0", TMP "/usb1"};

static mp_inst_env test_env(void) {
    mp_inst_env env;
    mp_inst_env_default(&env);
    env.internal = TMP "/data";
    env.drive_candidates = cands;
    env.ndrive_candidates = 2;
    return env;
}

static int setup_done = 0;

static void setup(void) {
    if (setup_done) return;
    setup_done = 1;
    mpt_clear(TMP);
    mp_err e = {{0}};
    mp_make_dirs(TMP "/usb0", &e);
    mp_make_dirs(TMP "/data", &e);
    mpt_pack(MPT_BASIC, TMP "/basic.p5z", 1, NULL);
}

MP_TEST(scan_depth) {
    setup();
    mpt_copy_file(TMP "/basic.p5z", TMP "/usb0/basic.p5z");
    mpt_copy_file(TMP "/basic.p5z", TMP "/usb0/juegos/b.P5Z");
    mpt_copy_file(TMP "/basic.p5z", TMP "/usb0/a/b/c.p5z");
    mpt_copy_file(TMP "/basic.p5z", TMP "/data/maxpack/d.p5z");
    mp_inst_env env = test_env();
    mp_inst_found *v = NULL;
    size_t n = 0;
    mp_err e = {{0}};
    MP_CHECK(mp_inst_scan(&env, &v, &n, &e) == 0);
    MP_CHECK_EQ_U64(n, 3);
    // install_size = what unpack --folder leaves
    mp_p5z *p = NULL;
    MP_CHECK(mp_p5z_open(TMP "/basic.p5z", &p, &e) == 0);
    mpt_clear(TMP "/unp");
    MP_CHECK(p && mp_unpack_folder(p, TMP "/unp", 2, 0, &e) == 0);
    mp_p5z_close(p);
    uint64_t want = mpt_dir_size(TMP "/unp");
    mpt_clear(TMP "/orig");
    MP_CHECK(mp_p5z_open(TMP "/basic.p5z", &p, &e) == 0);
    MP_CHECK(p && mp_unpack_original(p, TMP "/orig", 2, 0, &e) == 0);
    mp_p5z_close(p);
    uint64_t orig = mpt_dir_size(TMP "/orig");
    int seen_deep = 0;
    for (size_t i = 0; i < n; i++) {
        MP_CHECK_STR(v[i].title_id, "TEST00001");
        MP_CHECK_EQ_U64(v[i].install_size, want);
        MP_CHECK_EQ_U64(v[i].original_size, orig);
        MP_CHECK(v[i].p5z_size > 0);
        if (strstr(v[i].path, "/a/b/")) seen_deep = 1;
    }
    MP_CHECK(!seen_deep);
    mp_inst_found_free(v, n);
}

MP_TEST(drives_only_existing) {
    setup();
    mp_inst_env env = test_env();
    const char *d[4];
    MP_CHECK_EQ_U64(mp_inst_drives(&env, d, 4), 1);
    MP_CHECK_STR(d[0], TMP "/usb0");
}

MP_TEST(probe_rejects) {
    setup();
    mpt_put_text(TMP "/falso.p5z", "esto no es un .p5z");
    mp_inst_found f;
    mp_err e = {{0}};
    MP_CHECK(mp_inst_probe(TMP "/falso.p5z", &f, &e) == -1);
    MP_CHECK(e.msg[0] != 0);
    size_t n = 0;
    uint8_t *b = mpt_slurp(TMP "/basic.p5z", &n);
    MP_CHECK(b != NULL);
    if (!b) return;
    mpt_put(TMP "/mitad.p5z", b, n / 2);
    free(b);
    mp_err e2 = {{0}};
    MP_CHECK(mp_inst_probe(TMP "/mitad.p5z", &f, &e2) == -1);
    MP_CHECK(e2.msg[0] != 0);
}

MP_TEST(probe_rejects_bad_title) {
    setup();
    mpt_put_text(TMP "/evil_src/sce_sys/param.json", "{\"titleId\":\"../EVIL\",\"contentVersion\":\"01.000.000\"}");
    mpt_put_text(TMP "/evil_src/datos.bin", "datos datos datos datos datos datos");
    mpt_put_text(TMP "/noid_src/datos.bin", "datos datos datos datos datos datos");
    if (mpt_pack(TMP "/evil_src", TMP "/evil.p5z", 1, NULL) || mpt_pack(TMP "/noid_src", TMP "/noid.p5z", 1, NULL))
        return;
    mp_inst_found f;
    mp_err e = {{0}};
    MP_CHECK(mp_inst_probe(TMP "/evil.p5z", &f, &e) == -1);
    MP_CHECK_CONTAINS(e.msg, "TITLE_ID");
    mp_err e2 = {{0}};
    MP_CHECK(mp_inst_probe(TMP "/noid.p5z", &f, &e2) == -1);
    MP_CHECK_CONTAINS(e2.msg, "TITLE_ID");
    MP_CHECK(mp_inst_title_ok("PPSA12544") == 1);
    MP_CHECK(mp_inst_title_ok("ppsa12544") == 0);
    MP_CHECK(mp_inst_title_ok("PPSA1254") == 0);
    MP_CHECK(mp_inst_title_ok("PPSA12544/") == 0);
    MP_CHECK(mp_inst_title_ok("../EVIL") == 0);
    MP_CHECK(mp_inst_title_ok("") == 0);
}

static const mp_inst_existing *find_path(const mp_inst_existing *v, size_t n, const char *path) {
    for (size_t i = 0; i < n; i++)
        if (!strcmp(v[i].path, path)) return &v[i];
    return NULL;
}

MP_TEST(existing_by_param) {
    setup();
    mpt_put_text(TMP "/data/homebrew/Silksong/sce_sys/param.json", "{\"titleId\":\"TEST00001\"}");
    mpt_put_text(TMP "/data/homebrew/Silksong/eboot.bin", "eboot");
    mpt_put_text(TMP "/usb0/homebrew/TEST00001/sce_sys/param.json", "{\"titleId\":\"OTRO00002\"}");
    mpt_put_text(TMP "/usb0/raro/x.bin", "123");
    mpt_put_text(TMP "/usb0/homebrew/Otro/sce_sys/param.json", "{\"titleId\":\"OTRO00003\"}");
    mp_inst_env env = test_env();
    const char *known[] = {TMP "/usb0/raro"};
    mp_inst_existing *v = NULL;
    size_t n = 0;
    mp_err e = {{0}};
    MP_CHECK(mp_inst_find_existing(&env, "TEST00001", known, 1, &v, &n, &e) == 0);
    MP_CHECK_EQ_U64(n, 3);
    const mp_inst_existing *a = find_path(v, n, TMP "/data/homebrew/Silksong");
    const mp_inst_existing *b = find_path(v, n, TMP "/usb0/homebrew/TEST00001");
    const mp_inst_existing *c = find_path(v, n, TMP "/usb0/raro");
    MP_CHECK(a && b && c);
    if (a) MP_CHECK_EQ_U64(a->size, mpt_dir_size(TMP "/data/homebrew/Silksong"));
    if (c) MP_CHECK_EQ_U64(c->size, 3);
    if (a) MP_CHECK(a->from_maxpack == 0 && a->is_file == 0);
    mp_inst_existing_free(v, n);

    mp_inst_record r;
    memset(&r, 0, sizeof r);
    snprintf(r.title_id, sizeof r.title_id, "TEST00001");
    snprintf(r.dir, sizeof r.dir, "%s", TMP "/data/homebrew/Silksong");
    MP_CHECK(mp_inst_record_save(&env, &r, &e) == 0);
    MP_CHECK(mp_inst_find_existing(&env, "TEST00001", NULL, 0, &v, &n, &e) == 0);
    a = find_path(v, n, TMP "/data/homebrew/Silksong");
    b = find_path(v, n, TMP "/usb0/homebrew/TEST00001");
    MP_CHECK(a && a->from_maxpack == 1);
    MP_CHECK(b && b->from_maxpack == 0);
    mp_inst_existing_free(v, n);
    mpt_clear(TMP "/data/homebrew");
    mpt_clear(TMP "/data/maxpack/installed");
}

MP_TEST(record_roundtrip) {
    setup();
    mp_inst_env env = test_env();
    mp_inst_record r, back;
    memset(&r, 0, sizeof r);
    snprintf(r.title_id, sizeof r.title_id, "TEST00009");
    snprintf(r.version, sizeof r.version, "01.000.000");
    snprintf(r.name, sizeof r.name, "\xC3\x91" "and\xC3\xBA <b>");
    snprintf(r.dir, sizeof r.dir, "/x/y");
    snprintf(r.p5z, sizeof r.p5z, "/mnt/usb0/y.p5z");
    for (int i = 0; i < 16; i++) r.archive_id[i] = (uint8_t)(i * 17);
    for (int i = 0; i < 32; i++) r.runtime_sha[i] = (uint8_t)(255 - i);
    r.runtime_replaced = 1;
    r.smp_pending = 1;
    snprintf(r.smp_why, sizeof r.smp_why, "scan postponed: game_active");
    r.installed_at = 1700000000;
    mp_err e = {{0}};
    MP_CHECK(mp_inst_record_save(&env, &r, &e) == 0);
    memset(&back, 0x55, sizeof back);
    MP_CHECK(mp_inst_record_load(&env, "TEST00009", &back, &e) == 0);
    MP_CHECK_STR(back.title_id, r.title_id);
    MP_CHECK_STR(back.version, r.version);
    MP_CHECK_STR(back.name, r.name);
    MP_CHECK_STR(back.dir, r.dir);
    MP_CHECK_STR(back.p5z, r.p5z);
    MP_CHECK_STR(back.smp_why, r.smp_why);
    MP_CHECK(!memcmp(back.archive_id, r.archive_id, 16) && !memcmp(back.runtime_sha, r.runtime_sha, 32));
    MP_CHECK(back.runtime_replaced == 1 && back.smp_pending == 1 && back.installed_at == 1700000000);
    mp_err e2 = {{0}};
    MP_CHECK(mp_inst_record_load(&env, "TEST00008", &back, &e2) == -1);
    MP_CHECK(mp_inst_record_load(&env, "../x", &back, &e2) == -1);
}

static const char *kcands[] = {TMP "/usbk"};

static mp_inst_env kinds_env(void) {
    mp_inst_env env;
    mp_inst_env_default(&env);
    env.internal = TMP "/datak";
    env.drive_candidates = kcands;
    env.ndrive_candidates = 1;
    return env;
}

static const mp_inst_found *find_found(const mp_inst_found *v, size_t n, const char *path) {
    for (size_t i = 0; i < n; i++)
        if (!strcmp(v[i].path, path)) return &v[i];
    return NULL;
}

MP_TEST(scan_kinds) {
    setup();
    mpt_clear(TMP "/usbk");
    mpt_clear(TMP "/datak");
    mp_err e = {{0}};
    mp_make_dirs(TMP "/datak", &e);
    mpt_put_text(TMP "/usbk/Juego/sce_sys/param.json", "{\"titleId\":\"TEST00003\",\"contentVersion\":\"01.000.000\"}");
    mpt_put_text(TMP "/usbk/Juego/eboot.bin", "eboot del juego");
    mpt_copy_file(MPT_BASIC, TMP "/usbk/basic.exfat");
    static char junk[1 << 20];
    for (size_t i = 0; i < sizeof junk; i++) junk[i] = (char)(i * 7);
    mpt_put(TMP "/usbk/x/Silksong-PPSA12544.ffpkg", junk, sizeof junk);
    mpt_put(TMP "/usbk/sinid.ffpfsc", junk, 1000);
    mp_inst_env env = kinds_env();
    mp_inst_found *v = NULL;
    size_t n = 0;
    MP_CHECK(mp_inst_scan(&env, &v, &n, &e) == 0);
    MP_CHECK_EQ_U64(n, 3);
    const mp_inst_found *a = find_found(v, n, TMP "/usbk/Juego");
    const mp_inst_found *b = find_found(v, n, TMP "/usbk/basic.exfat");
    const mp_inst_found *c = find_found(v, n, TMP "/usbk/x/Silksong-PPSA12544.ffpkg");
    MP_CHECK(a && b && c);
    if (a) {
        MP_CHECK(a->kind == MP_SRC_FOLDER);
        MP_CHECK_STR(a->title_id, "TEST00003");
        MP_CHECK_EQ_U64(a->install_size, mpt_dir_size(TMP "/usbk/Juego"));
    }
    if (b) {
        MP_CHECK(b->kind == MP_SRC_IMAGE);
        MP_CHECK_STR(b->title_id, "TEST00001");
        MP_CHECK_STR(b->version, "01.000.000");
    }
    if (c) {
        MP_CHECK(c->kind == MP_SRC_IMAGE);
        MP_CHECK_STR(c->title_id, "PPSA12544");
        MP_CHECK_EQ_U64(c->install_size, sizeof junk);
    }
    mp_inst_found_free(v, n);
    mp_inst_found f;
    mp_err e2 = {{0}};
    MP_CHECK(mp_inst_probe_any(TMP "/usbk/sinid.ffpfsc", &f, &e2) == -1);
    MP_CHECK_CONTAINS(e2.msg, "name");
    MP_CHECK(mp_inst_probe_any(TMP "/basic.p5z", &f, &e2) == 0 && f.kind == MP_SRC_P5Z);
    free(f.path);
}

MP_TEST(probe_any_packed) {
    setup();
    mpt_pack(MPT_BASIC, TMP "/packed_dir", 0, NULL);
    mp_inst_found f;
    mp_err e = {{0}};
    MP_CHECK(mp_inst_probe_any(TMP "/packed_dir", &f, &e) == 0);
    MP_CHECK(f.kind == MP_SRC_FOLDER && f.packed == 1);
    free(f.path);
    MP_CHECK(mp_inst_probe_any(TMP "/usbk/Juego", &f, &e) == 0);
    MP_CHECK(f.kind == MP_SRC_FOLDER && f.packed == 0);
    free(f.path);
}

MP_TEST(existing_images) {
    setup();
    char junk[4096] = {1};
    mpt_put(TMP "/datak/homebrew/Silksong-PPSA12544.ffpkg", junk, sizeof junk);
    mp_inst_env env = kinds_env();
    mp_inst_existing *v = NULL;
    size_t n = 0;
    mp_err e = {{0}};
    MP_CHECK(mp_inst_find_existing(&env, "PPSA12544", NULL, 0, &v, &n, &e) == 0);
    int seen = 0;
    for (size_t i = 0; i < n; i++)
        if (!strcmp(v[i].path, TMP "/datak/homebrew/Silksong-PPSA12544.ffpkg")) seen = v[i].is_file == 1 && v[i].size == 4096;
    MP_CHECK(seen);
    mp_inst_existing_free(v, n);
}

int main(void) {
    MP_RUN(scan_kinds);
    MP_RUN(probe_any_packed);
    MP_RUN(existing_images);
    MP_RUN(scan_depth);
    MP_RUN(drives_only_existing);
    MP_RUN(probe_rejects);
    MP_RUN(probe_rejects_bad_title);
    MP_RUN(existing_by_param);
    MP_RUN(record_roundtrip);
    MP_TEST_MAIN_END();
}
