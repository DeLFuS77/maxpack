// SPDX-License-Identifier: GPL-3.0-or-later
// Installer: install a .p5z as a folder ready for the console.
#include "mp_fixture.h"
#include "mp_inst.h"
#include "mp_p5z_internal.h"

#define TMP MP_TEST_TMP "/inst_install"

static uint8_t rt_new[4096], rt_old[2000];
static const char *no_drives[] = {TMP "/usb1"};
static char notes[4096];

static void note(void *ctx, const char *msg) {
    (void)ctx;
    size_t k = strlen(notes);
    snprintf(notes + k, sizeof notes - k, "%s\n", msg);
}

static mp_inst_env env_for(const char *internal) {
    mp_inst_env env;
    mp_inst_env_default(&env);
    env.internal = internal;
    env.drive_candidates = no_drives;
    env.ndrive_candidates = 1;
    env.runtime.data = rt_new;
    env.runtime.len = sizeof rt_new;
    env.runtime.mtime = 1700000000;
    env.runtime.version = "prueba";
    env.notify = note;
    mp_err e = {{0}};
    mpt_clear(internal);
    mp_make_dirs(internal, &e);
    return env;
}

static mp_inst_req req_for(const char *p5z, const char *dest) {
    mp_inst_req q;
    memset(&q, 0, sizeof q);
    q.p5z_path = p5z;
    q.dest_root = dest;
    return q;
}

static int ready = 0;

static void setup(void) {
    if (ready) return;
    ready = 1;
    mpt_clear(TMP);
    for (size_t i = 0; i < sizeof rt_new; i++) rt_new[i] = (uint8_t)(i * 13 + 1);
    memcpy(rt_new + 64, "AMPRZPK1", 8);
    for (size_t i = 0; i < sizeof rt_old; i++) rt_old[i] = (uint8_t)(i * 3);
    mp_err e = {{0}};
    mpt_put(TMP "/R.sprx", rt_new, sizeof rt_new);
    mp_set_mtime(TMP "/R.sprx", 1700000000, &e);
    mpt_put(TMP "/old.sprx", rt_old, sizeof rt_old);
    mpt_pack(MPT_BASIC, TMP "/rt.p5z", 1, TMP "/R.sprx");
    mpt_pack(MPT_BASIC, TMP "/nort.p5z", 1, NULL);
    mpt_pack(MPT_BASIC, TMP "/old.p5z", 1, TMP "/old.sprx");
    mpt_pack(MPT_BASIC, TMP "/folder_rt", 0, TMP "/R.sprx");
    mp_p5z *p = NULL;
    if (mp_p5z_open(TMP "/rt.p5z", &p, &e) == 0) {
        mp_unpack_folder(p, TMP "/unpacked_rt", 2, 0, &e);
        mp_p5z_close(p);
    }
}

MP_TEST(install_equals_unpack) {
    setup();
    mp_inst_env env = env_for(TMP "/d_eq");
    mp_inst_req q = req_for(TMP "/rt.p5z", TMP "/d_eq");
    mp_inst_result r;
    mp_err e = {{0}};
    MP_CHECK(mp_inst_install(&env, &q, &r, &e) == 0);
    if (e.msg[0]) fprintf(stderr, "  %s\n", e.msg);
    MP_CHECK(r.status == MP_INST_DONE);
    MP_CHECK_STR(r.code, "done");
    MP_CHECK_STR(r.final_dir, TMP "/d_eq/homebrew/TEST00001");
    MP_CHECK(mpt_dirs_equal(TMP "/unpacked_rt", TMP "/d_eq/homebrew/TEST00001", "*.pak"));
    MP_CHECK(r.runtime_replaced == 0);
    MP_CHECK(mp_path_kind(TMP "/d_eq/maxpack/installed/TEST00001.json") == 1);
    MP_CHECK(mp_path_kind(TMP "/d_eq/maxpack/tmp/TEST00001") == 0);
    mp_inst_record rec;
    MP_CHECK(mp_inst_record_load(&env, "TEST00001", &rec, &e) == 0);
    MP_CHECK_STR(rec.dir, TMP "/d_eq/homebrew/TEST00001");
    MP_CHECK_STR(rec.p5z, TMP "/rt.p5z");
    mp_inst_result_free(&r);
}

MP_TEST(install_threads_identical) {
    setup();
    mp_inst_env a = env_for(TMP "/d_t1"), b = env_for(TMP "/d_t4");
    a.threads = 1;
    b.threads = 4;
    mp_inst_req qa = req_for(TMP "/rt.p5z", TMP "/d_t1"), qb = req_for(TMP "/rt.p5z", TMP "/d_t4");
    mp_inst_result ra, rb;
    mp_err e = {{0}};
    MP_CHECK(mp_inst_install(&a, &qa, &ra, &e) == 0 && ra.status == MP_INST_DONE);
    MP_CHECK(mp_inst_install(&b, &qb, &rb, &e) == 0 && rb.status == MP_INST_DONE);
    MP_CHECK(mpt_dirs_equal(TMP "/d_t1/homebrew/TEST00001", TMP "/d_t4/homebrew/TEST00001", "*.pak"));
    mp_inst_result_free(&ra);
    mp_inst_result_free(&rb);
}

MP_TEST(install_replaces_runtime) {
    setup();
    const char *src[] = {TMP "/nort.p5z", TMP "/old.p5z"};
    const char *dst[] = {TMP "/d_nort", TMP "/d_old"};
    for (int k = 0; k < 2; k++) {
        notes[0] = 0;
        mp_inst_env env = env_for(dst[k]);
        mp_inst_req q = req_for(src[k], dst[k]);
        mp_inst_result r;
        mp_err e = {{0}};
        MP_CHECK(mp_inst_install(&env, &q, &r, &e) == 0);
        if (e.msg[0]) fprintf(stderr, "  %s\n", e.msg);
        MP_CHECK(r.status == MP_INST_DONE && r.runtime_replaced == 1);
        char fin[1024];
        snprintf(fin, sizeof fin, "%s/homebrew/TEST00001", dst[k]);
        MP_CHECK(mpt_dirs_equal(TMP "/folder_rt", fin, "*.pak|ampr_emu.index"));
        MP_CHECK_CONTAINS(notes, "runtime");
        mp_inst_result_free(&r);
    }
}

MP_TEST(install_no_runtime) {
    setup();
    mp_inst_env env = env_for(TMP "/d_nr");
    env.runtime.data = NULL;
    env.runtime.len = 0;
    mp_inst_req q = req_for(TMP "/nort.p5z", TMP "/d_nr");
    mp_inst_result r;
    mp_err e = {{0}};
    MP_CHECK(mp_inst_install(&env, &q, &r, &e) == -1);
    MP_CHECK(r.status == MP_INST_FAILED);
    MP_CHECK_STR(r.code, "no_runtime");
    MP_CHECK(e.msg[0] != 0);
    MP_CHECK(mp_path_kind(TMP "/d_nr/homebrew") == 0);
    MP_CHECK(mp_path_kind(TMP "/d_nr/maxpack/tmp/TEST00001") == 0);
    mp_inst_result_free(&r);
}

static int tiny_space(void *ctx, const char *path, uint64_t *out) {
    (void)ctx;
    (void)path;
    *out = 1u << 20;
    return 0;
}

MP_TEST(install_no_space) {
    setup();
    mp_inst_env env = env_for(TMP "/d_ns");
    env.free_space = tiny_space;
    mp_inst_req q = req_for(TMP "/rt.p5z", TMP "/d_ns");
    mp_inst_result r;
    mp_err e = {{0}};
    MP_CHECK(mp_inst_install(&env, &q, &r, &e) == 0);
    MP_CHECK(r.status == MP_INST_NO_SPACE);
    MP_CHECK_STR(r.code, "no_space");
    MP_CHECK(r.need > r.avail && r.avail == (1u << 20));
    MP_CHECK(mp_path_kind(TMP "/d_ns/homebrew") == 0);
    MP_CHECK(mp_path_kind(TMP "/d_ns/maxpack/tmp") == 0);
    mp_inst_result_free(&r);
}

MP_TEST(dest_missing) {
    setup();
    mp_inst_env env = env_for(TMP "/d_dm");
    mp_inst_req q = req_for(TMP "/rt.p5z", TMP "/usb1");
    mp_inst_result r;
    mp_err e = {{0}};
    MP_CHECK(mp_inst_install(&env, &q, &r, &e) == -1);
    MP_CHECK_STR(r.code, "bad_dest");
    MP_CHECK(mp_path_kind(TMP "/usb1") == 0);
    MP_CHECK(mp_path_kind(TMP "/d_dm/maxpack") == 0);
    mp_inst_result_free(&r);
}

MP_TEST(install_damaged_p5z) {
    setup();
    mpt_copy_file(TMP "/rt.p5z", TMP "/damaged.p5z");
    mp_p5z *p = NULL;
    mp_err e = {{0}};
    uint64_t at = 0;
    if (mp_p5z_open(TMP "/damaged.p5z", &p, &e) == 0) {
        for (size_t i = 0; i < p->nfiles && !at; i++)
            if (p->files[i].role == MP_ROLE_LOOSE && p->files[i].storage == P5Z_STORE_FRAMES && p->files[i].nframes) {
                const p5z_frame *fr = &p->frames[p->files[i].first_frame];
                at = fr->off + fr->csize / 2;
            }
        mp_p5z_close(p);
    }
    MP_CHECK(at != 0);
    size_t n = 0;
    uint8_t *b = mpt_slurp(TMP "/damaged.p5z", &n);
    if (!b || !at) return;
    b[at] ^= 0x5A;
    mpt_put(TMP "/damaged.p5z", b, n);
    free(b);
    mp_inst_env env = env_for(TMP "/d_dmg");
    mp_inst_req q = req_for(TMP "/damaged.p5z", TMP "/d_dmg");
    mp_inst_result r;
    MP_CHECK(mp_inst_install(&env, &q, &r, &e) == -1);
    MP_CHECK(r.status == MP_INST_FAILED);
    MP_CHECK_STR(r.code, "bad_p5z");
    MP_CHECK(mp_path_kind(TMP "/d_dmg/homebrew/TEST00001") == 0);
    mp_inst_result_free(&r);
}

int main(void) {
    MP_RUN(install_equals_unpack);
    MP_RUN(install_threads_identical);
    MP_RUN(install_replaces_runtime);
    MP_RUN(install_no_runtime);
    MP_RUN(install_no_space);
    MP_RUN(dest_missing);
    MP_RUN(install_damaged_p5z);
    MP_TEST_MAIN_END();
}
