// SPDX-License-Identifier: GPL-3.0-or-later
// Instalador: reemplazar copias con confirmación y borrar el .p5z verificado.
#include "mp_fixture.h"
#include "mp_inst.h"

#define TMP MP_TEST_TMP "/inst_replace"

static uint8_t rt_new[4096];
static const char *drives[] = {TMP "/usb0"};

typedef struct {
    const char *corrupt;   // archivo que se estropea al empezar "comprobando"
    uint64_t fail_at;
    int done_corrupt;
} hook;

static void on_progress(void *ctx, const char *stage, uint64_t done, uint64_t total) {
    hook *h = ctx;
    (void)done;
    (void)total;
    if (h && h->corrupt && !h->done_corrupt && !strcmp(stage, "comprobando")) {
        h->done_corrupt = 1;
        size_t n = 0;
        uint8_t *b = mpt_slurp(h->corrupt, &n);
        if (b && n) {
            b[n / 2] ^= 0x33;
            mpt_put(h->corrupt, b, n);
        }
        free(b);
    }
}

static int on_io(void *ctx, uint64_t written, mp_err *e) {
    hook *h = ctx;
    if (h && h->fail_at && written > h->fail_at) {
        mp_err_set(e, "fallo inyectado");
        return -1;
    }
    return 0;
}

static mp_inst_env env_for(const char *internal, hook *h) {
    mp_inst_env env;
    mp_inst_env_default(&env);
    env.internal = internal;
    env.drive_candidates = drives;
    env.ndrive_candidates = 1;
    env.runtime.data = rt_new;
    env.runtime.len = sizeof rt_new;
    env.runtime.mtime = 1700000000;
    env.progress = on_progress;
    env.io_check = on_io;
    env.ctx = h;
    return env;
}

static int ready = 0;

static void setup(void) {
    if (ready) return;
    ready = 1;
    mpt_clear(TMP);
    for (size_t i = 0; i < sizeof rt_new; i++) rt_new[i] = (uint8_t)(i * 13 + 1);
    memcpy(rt_new + 64, "AMPRZPK1", 8);
    mp_err e = {{0}};
    mpt_put(TMP "/R.sprx", rt_new, sizeof rt_new);
    mp_set_mtime(TMP "/R.sprx", 1700000000, &e);
    mpt_pack(MPT_BASIC, TMP "/rt.p5z", 1, TMP "/R.sprx");
    mp_p5z *p = NULL;
    if (mp_p5z_open(TMP "/rt.p5z", &p, &e) == 0) {
        mp_unpack_folder(p, TMP "/unpacked_rt", 2, 0, &e);
        mp_unpack_original(p, TMP "/dump", 2, 0, &e);   // el dump sin comprimir
        mp_p5z_close(p);
    }
}

// Copia el dump sin comprimir a dir.
static void place_dump(const char *dir) {
    mpt_list l = mpt_list_dir(TMP "/dump");
    for (size_t i = 0; i < l.n; i++) {
        char a[2048], b[2048];
        snprintf(a, sizeof a, "%s/%s", TMP "/dump", l.v[i]);
        snprintf(b, sizeof b, "%s/%s", dir, l.v[i]);
        mpt_copy_file(a, b);
        int64_t mt = 0;
        mp_err e = {{0}};
        if (mp_stat(a, NULL, &mt) == 0) mp_set_mtime(b, mt, &e);
    }
    mpt_list_free(&l);
}

static void fresh(const char *internal) {
    mp_err e = {{0}};
    mpt_clear(internal);
    mpt_clear(TMP "/usb0");
    mp_make_dirs(internal, &e);
    mp_make_dirs(TMP "/usb0", &e);
}

static int install(const char *internal, const char *p5z, const char *const *rep, size_t nrep, int del,
                   hook *h, mp_inst_result *r, mp_err *e) {
    mp_inst_env env = env_for(internal, h);
    mp_inst_req q;
    memset(&q, 0, sizeof q);
    q.p5z_path = p5z;
    q.dest_root = internal;
    q.replace = rep;
    q.nreplace = nrep;
    q.delete_p5z = del;
    memset(e, 0, sizeof *e);
    return mp_inst_install(&env, &q, r, e);
}

MP_TEST(replace_needs_confirm) {
    setup();
    fresh(TMP "/a");
    place_dump(TMP "/a/homebrew/TEST00001");
    mp_inst_result r;
    mp_err e;
    MP_CHECK(install(TMP "/a", TMP "/rt.p5z", NULL, 0, 0, NULL, &r, &e) == 0);
    MP_CHECK(r.status == MP_INST_NEEDS_CONFIRM);
    MP_CHECK_STR(r.code, "needs_confirm");
    MP_CHECK_EQ_U64(r.nexisting, 1);
    if (r.nexisting) {
        MP_CHECK_STR(r.existing[0].path, TMP "/a/homebrew/TEST00001");
        MP_CHECK_EQ_U64(r.existing[0].size, mpt_dir_size(TMP "/dump"));
    }
    MP_CHECK(mpt_dirs_equal(TMP "/dump", TMP "/a/homebrew/TEST00001", NULL));
    mp_inst_result_free(&r);
}

MP_TEST(replace_same_place) {
    setup();
    fresh(TMP "/b");
    place_dump(TMP "/b/homebrew/TEST00001");
    const char *rep[] = {TMP "/b/homebrew/TEST00001"};
    mp_inst_result r;
    mp_err e;
    MP_CHECK(install(TMP "/b", TMP "/rt.p5z", rep, 1, 0, NULL, &r, &e) == 0);
    if (e.msg[0]) fprintf(stderr, "  %s\n", e.msg);
    MP_CHECK(r.status == MP_INST_DONE);
    MP_CHECK(mpt_dirs_equal(TMP "/unpacked_rt", TMP "/b/homebrew/TEST00001", "*.pak"));
    MP_CHECK(mp_path_kind(TMP "/b/maxpack/tmp/TEST00001") == 0);
    MP_CHECK(mp_path_kind(TMP "/b/maxpack/tmp/TEST00001.old") == 0);
    mp_inst_result_free(&r);
}

MP_TEST(replace_other_drive) {
    setup();
    fresh(TMP "/c");
    place_dump(TMP "/usb0/homebrew/Silksong");
    const char *rep[] = {TMP "/usb0/homebrew/Silksong"};
    mp_inst_result r;
    mp_err e;
    MP_CHECK(install(TMP "/c", TMP "/rt.p5z", rep, 1, 0, NULL, &r, &e) == 0);
    MP_CHECK(r.status == MP_INST_DONE);
    MP_CHECK(mp_path_kind(TMP "/usb0/homebrew/Silksong") == 0);
    MP_CHECK(mpt_dirs_equal(TMP "/unpacked_rt", TMP "/c/homebrew/TEST00001", "*.pak"));
    mp_inst_result_free(&r);
}

MP_TEST(replace_old_kept_on_failure) {
    setup();
    fresh(TMP "/d");
    place_dump(TMP "/d/homebrew/TEST00001");
    const char *rep[] = {TMP "/d/homebrew/TEST00001"};
    mp_inst_found f;
    uint64_t total = 0;
    if (mp_inst_probe(TMP "/rt.p5z", &f, NULL) == 0) {
        total = f.install_size;
        free(f.path);
    }
    hook h = {NULL, total / 2, 0};
    mp_inst_result r;
    mp_err e;
    MP_CHECK(install(TMP "/d", TMP "/rt.p5z", rep, 1, 0, &h, &r, &e) == -1);
    MP_CHECK(r.status == MP_INST_FAILED);
    MP_CHECK(mpt_dirs_equal(TMP "/dump", TMP "/d/homebrew/TEST00001", NULL));
    mp_inst_result_free(&r);
}

MP_TEST(replace_list_changed) {
    setup();
    fresh(TMP "/e");
    place_dump(TMP "/e/homebrew/TEST00001");
    place_dump(TMP "/usb0/otra");
    const char *rep[] = {TMP "/e/homebrew/TEST00001"};
    mp_inst_result r;
    mp_err e;
    MP_CHECK(install(TMP "/e", TMP "/rt.p5z", rep, 1, 0, NULL, &r, &e) == 0);
    MP_CHECK(r.status == MP_INST_NEEDS_CONFIRM);
    int seen = 0;
    for (size_t i = 0; i < r.nexisting; i++) seen |= !strcmp(r.existing[i].path, TMP "/usb0/otra");
    MP_CHECK(seen);
    MP_CHECK(mpt_dirs_equal(TMP "/dump", TMP "/e/homebrew/TEST00001", NULL));
    mp_inst_result_free(&r);
}

MP_TEST(delete_p5z_after_check) {
    setup();
    fresh(TMP "/f");
    mpt_copy_file(TMP "/rt.p5z", TMP "/f_src/del.p5z");
    mp_inst_result r;
    mp_err e;
    MP_CHECK(install(TMP "/f", TMP "/f_src/del.p5z", NULL, 0, 1, NULL, &r, &e) == 0);
    MP_CHECK(r.status == MP_INST_DONE);
    MP_CHECK(mp_path_kind(TMP "/f_src/del.p5z") == 0);
    mp_inst_result_free(&r);

    fresh(TMP "/g");
    mpt_copy_file(TMP "/rt.p5z", TMP "/f_src/keep.p5z");
    hook h = {TMP "/g/maxpack/tmp/TEST00001/eboot.bin", 0, 0};
    MP_CHECK(install(TMP "/g", TMP "/f_src/keep.p5z", NULL, 0, 1, &h, &r, &e) == -1);
    MP_CHECK(h.done_corrupt == 1);
    MP_CHECK(r.status == MP_INST_FAILED);
    MP_CHECK(mp_path_kind(TMP "/f_src/keep.p5z") == 1);
    MP_CHECK(mp_path_kind(TMP "/g/homebrew/TEST00001") == 0);
    mp_inst_result_free(&r);
}

MP_TEST(check_dir_counts) {
    setup();
    mp_p5z *p = NULL;
    mp_err e = {{0}};
    MP_CHECK(mp_p5z_open(TMP "/rt.p5z", &p, &e) == 0);
    if (!p) return;
    mpt_clear(TMP "/chk");
    MP_CHECK(mp_unpack_folder(p, TMP "/chk", 2, 0, &e) == 0);
    MP_CHECK(mp_inst_check_dir(p, TMP "/chk", 0, 2, &e) == 0);
    size_t n = 0;
    uint8_t *b = mpt_slurp(TMP "/chk/ampr_zstd-000.pak", &n);
    if (b && n > 200) {
        b[n - 100] ^= 1;
        mpt_put(TMP "/chk/ampr_zstd-000.pak", b, n);
    }
    free(b);
    MP_CHECK(mp_inst_check_dir(p, TMP "/chk", 0, 2, &e) == 1);
    mp_p5z_close(p);
}

int main(void) {
    MP_RUN(replace_needs_confirm);
    MP_RUN(replace_same_place);
    MP_RUN(replace_other_drive);
    MP_RUN(replace_old_kept_on_failure);
    MP_RUN(replace_list_changed);
    MP_RUN(delete_p5z_after_check);
    MP_RUN(check_dir_counts);
    MP_TEST_MAIN_END();
}
