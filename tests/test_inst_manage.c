// SPDX-License-Identifier: GPL-3.0-or-later
// Installer: list, Verify and Uninstall.
#include "mp_fixture.h"
#include "mp_inst.h"

#define TMP MP_TEST_TMP "/inst_manage"
#define DATA TMP "/data"

static uint8_t rt_new[4096];
static const char *no_drives[] = {TMP "/usb1"};
static char notes[4096];

static void note(void *ctx, const char *msg) {
    (void)ctx;
    size_t k = strlen(notes);
    snprintf(notes + k, sizeof notes - k, "%s\n", msg);
}

static mp_inst_env env_get(void) {
    mp_inst_env env;
    mp_inst_env_default(&env);
    env.internal = DATA;
    env.drive_candidates = no_drives;
    env.ndrive_candidates = 1;
    env.runtime.data = rt_new;
    env.runtime.len = sizeof rt_new;
    env.runtime.mtime = 1700000000;
    env.notify = note;
    return env;
}

static int install(const char *p5z) {
    mp_inst_env env = env_get();
    mp_inst_req q;
    memset(&q, 0, sizeof q);
    q.p5z_path = p5z;
    q.dest_root = DATA;
    mp_inst_result r;
    mp_err e = {{0}};
    int rc = mp_inst_install(&env, &q, &r, &e);
    if (rc || r.status != MP_INST_DONE) MP_FAIL_("instalar %s: %s", p5z, e.msg);
    mp_inst_result_free(&r);
    return rc;
}

static int ready = 0;

static void setup(void) {
    if (ready) return;
    ready = 1;
    mpt_clear(TMP);
    for (size_t i = 0; i < sizeof rt_new; i++) rt_new[i] = (uint8_t)(i * 13 + 1);
    memcpy(rt_new + 64, "AMPRZPK1", 8);
    mp_err e = {{0}};
    mp_make_dirs(DATA, &e);
    mpt_put(TMP "/R.sprx", rt_new, sizeof rt_new);
    mp_set_mtime(TMP "/R.sprx", 1700000000, &e);
    mpt_pack(MPT_BASIC, TMP "/src/a.p5z", 1, TMP "/R.sprx");
    char big[300000];
    for (size_t i = 0; i < sizeof big; i++) big[i] = (char)('a' + i % 23);
    mpt_put_text(TMP "/two/sce_sys/param.json", "{\"titleId\":\"TEST00002\",\"contentVersion\":\"01.000.000\"}");
    mpt_put(TMP "/two/datos/grande.bin", big, sizeof big);
    mpt_pack(TMP "/two", TMP "/src/b.p5z", 1, NULL);   // without a runtime: the installer puts it in
    install(TMP "/src/a.p5z");
    install(TMP "/src/b.p5z");
}

MP_TEST(list_installed) {
    setup();
    mp_inst_env env = env_get();
    mp_inst_record *v = NULL;
    size_t n = 0;
    mp_err e = {{0}};
    MP_CHECK(mp_inst_list(&env, &v, &n, &e) == 0);
    MP_CHECK_EQ_U64(n, 2);
    if (n == 2) {
        MP_CHECK_STR(v[0].title_id, "TEST00001");
        MP_CHECK_STR(v[1].title_id, "TEST00002");
        MP_CHECK_STR(v[1].dir, DATA "/homebrew/TEST00002");
        MP_CHECK(v[1].runtime_replaced == 1);
    }
    free(v);
}

MP_TEST(registry_corrupt_skipped) {
    setup();
    mpt_put_text(DATA "/maxpack/installed/TEST00003.json", "{\"title_id\":\"TEST0");
    notes[0] = 0;
    mp_inst_env env = env_get();
    mp_inst_record *v = NULL;
    size_t n = 0;
    mp_err e = {{0}};
    MP_CHECK(mp_inst_list(&env, &v, &n, &e) == 0);
    MP_CHECK_EQ_U64(n, 2);
    MP_CHECK_CONTAINS(notes, "TEST00003.json");
    free(v);
    mp_remove_file(DATA "/maxpack/installed/TEST00003.json");
}

MP_TEST(verify_ok_and_bad) {
    setup();
    mp_inst_env env = env_get();
    mp_inst_verify_result r;
    mp_err e = {{0}};
    MP_CHECK(mp_inst_verify(&env, "TEST00001", &r, &e) == 0);
    if (e.msg[0]) fprintf(stderr, "  %s\n", e.msg);
    MP_CHECK_EQ_U64(r.files_bad, 0);
    MP_CHECK(r.files_ok > 0 && r.p5z_checked == 1);
    MP_CHECK(mp_inst_verify(&env, "TEST00002", &r, &e) == 0);
    MP_CHECK_EQ_U64(r.files_bad, 0);   // with the runtime changed by the installer
    size_t n = 0;
    uint8_t *b = mpt_slurp(DATA "/homebrew/TEST00001/eboot.bin", &n);
    if (b && n) {
        b[0] ^= 1;
        mpt_put(DATA "/homebrew/TEST00001/eboot.bin", b, n);
    }
    MP_CHECK(mp_inst_verify(&env, "TEST00001", &r, &e) == 0);
    MP_CHECK_EQ_U64(r.files_bad, 1);
    mp_rename(TMP "/src/a.p5z", TMP "/src/a.apart", &e);
    MP_CHECK(mp_inst_verify(&env, "TEST00001", &r, &e) == 0);
    MP_CHECK(r.p5z_checked == 0);
    MP_CHECK_EQ_U64(r.files_bad, 0);   // without the .p5z, the changed loose file cannot be checked
    mp_rename(TMP "/src/a.apart", TMP "/src/a.p5z", &e);
    if (b && n) {
        b[0] ^= 1;
        mpt_put(DATA "/homebrew/TEST00001/eboot.bin", b, n);
    }
    free(b);
}

MP_TEST(uninstall_only_registered) {
    setup();
    mp_inst_env env = env_get();
    mp_err e = {{0}};
    MP_CHECK(mp_inst_uninstall(&env, "TEST00001", &e) == 0);
    MP_CHECK(mp_path_kind(DATA "/homebrew/TEST00001") == 0);
    MP_CHECK(mp_path_kind(DATA "/maxpack/installed/TEST00001.json") == 0);
    mpt_put_text(DATA "/homebrew/TEST00005/eboot.bin", "x");
    mp_err e2 = {{0}};
    MP_CHECK(mp_inst_uninstall(&env, "TEST00005", &e2) == -1);
    MP_CHECK(e2.msg[0] != 0);
    MP_CHECK(mp_path_kind(DATA "/homebrew/TEST00005/eboot.bin") == 1);
    MP_CHECK(mp_inst_uninstall(&env, "../x", &e2) == -1);
}

int main(void) {
    MP_RUN(list_installed);
    MP_RUN(registry_corrupt_skipped);
    MP_RUN(verify_ok_and_bad);
    MP_RUN(uninstall_only_registered);
    MP_TEST_MAIN_END();
}
