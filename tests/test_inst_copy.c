// SPDX-License-Identifier: GPL-3.0-or-later
// Installer: install game folders and images as is.
#include "mp_fixture.h"
#include "mp_inst.h"

#define TMP MP_TEST_TMP "/inst_copy"
#define USB TMP "/usb0"

static uint8_t rt_new[4096];
static const char *drives[] = {USB};

typedef struct {
    uint64_t stop_at, last;
} hook;

static int io_hook(void *ctx, uint64_t written, mp_err *e) {
    hook *h = ctx;
    (void)e;
    if (!h) return 0;
    h->last = written;
    return h->stop_at && written > h->stop_at ? 1 : 0;
}

static mp_inst_env env_for(const char *internal, hook *h, int clear) {
    mp_inst_env env;
    mp_inst_env_default(&env);
    env.internal = internal;
    env.drive_candidates = drives;
    env.ndrive_candidates = 1;
    env.runtime.data = rt_new;
    env.runtime.len = sizeof rt_new;
    env.runtime.mtime = 1700000000;
    env.io_check = io_hook;
    env.ctx = h;
    if (clear) {
        mp_err e = {{0}};
        mpt_clear(internal);
        mp_make_dirs(internal, &e);
    }
    return env;
}

static int install(const char *internal, const char *src, const char *const *rep, size_t nrep, hook *h, int clear,
                   mp_inst_result *r, mp_err *e) {
    mp_inst_env env = env_for(internal, h, clear);
    mp_inst_req q;
    memset(&q, 0, sizeof q);
    q.p5z_path = src;
    q.dest_root = internal;
    q.replace = rep;
    q.nreplace = nrep;
    q.delete_p5z = 1;   // ignored outside a .p5z
    memset(e, 0, sizeof *e);
    return mp_inst_install(&env, &q, r, e);
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
    // Dump in a folder.
    mpt_put_text(USB "/Juego/sce_sys/param.json", "{\"titleId\":\"TEST00003\",\"contentVersion\":\"01.000.000\"}");
    mpt_put_text(USB "/Juego/eboot.bin", "eboot del juego");
    mpt_put_text(USB "/Juego/datos/\xF0\x9F\x8E\xAE.txt", "emoji");
    static char big[3u << 20];
    for (size_t i = 0; i < sizeof big; i++) big[i] = (char)(i * 31 + (i >> 11));
    mpt_put(USB "/Juego/datos/grande.bin", big, sizeof big);
    for (int i = 0; i < 20; i++) {
        char p[256], t[64];
        snprintf(p, sizeof p, USB "/Juego/muchos/f%02d.json", i);
        snprintf(t, sizeof t, "{\"n\":%d}", i);
        mpt_put_text(p, t);
        mp_set_mtime(p, 1600000000 + i, &e);
    }
    // Folders with packs: without a runtime and with one.
    mpt_pack(MPT_BASIC, TMP "/srcs/Packed", 0, NULL);   // outside the drives: it is not "another copy"
    mpt_pack(MPT_BASIC, TMP "/packed_rt", 0, TMP "/R.sprx");
    // Images.
    mpt_copy_file(MPT_BASIC, USB "/basic.exfat");
    mpt_put(USB "/x/Starfall-PPSA10001.ffpkg", big, 1u << 20);
}

MP_TEST(copy_folder_identical) {
    setup();
    mp_inst_result r;
    mp_err e;
    MP_CHECK(install(TMP "/a", USB "/Juego", NULL, 0, NULL, 1, &r, &e) == 0);
    if (e.msg[0]) fprintf(stderr, "  %s\n", e.msg);
    MP_CHECK(r.status == MP_INST_DONE);
    MP_CHECK(mpt_dirs_equal(USB "/Juego", TMP "/a/homebrew/TEST00003", NULL));
    size_t n = 0;
    uint8_t *b = mpt_slurp(TMP "/a/maxpack/installed/TEST00003.files.json", &n);
    MP_CHECK(b != NULL);
    if (b) {
        size_t entries = 0;
        for (const char *p = (const char *)b; (p = strstr(p, "\"sha\"")) != NULL; p++) entries++;
        MP_CHECK_EQ_U64(entries, 24);
    }
    free(b);
    MP_CHECK(mp_path_kind(USB "/Juego/eboot.bin") == 1);   // the original is still there
    mp_inst_result_free(&r);
}

MP_TEST(copy_packed_gets_runtime) {
    setup();
    mp_inst_result r;
    mp_err e;
    MP_CHECK(install(TMP "/b", TMP "/srcs/Packed", NULL, 0, NULL, 1, &r, &e) == 0);
    if (e.msg[0]) fprintf(stderr, "  %s\n", e.msg);
    MP_CHECK(r.status == MP_INST_DONE && r.runtime_replaced == 1);
    MP_CHECK(mpt_dirs_equal(TMP "/packed_rt", TMP "/b/homebrew/TEST00001", "*.pak|ampr_emu.index|ampr_zstd.index"));
    mp_inst_result_free(&r);
    mpt_pack(MPT_BASIC, TMP "/srcs/PackedRt", 0, TMP "/R.sprx");
    MP_CHECK(install(TMP "/c", TMP "/srcs/PackedRt", NULL, 0, NULL, 1, &r, &e) == 0);
    MP_CHECK(r.status == MP_INST_DONE && r.runtime_replaced == 0);
    MP_CHECK(mpt_dirs_equal(TMP "/srcs/PackedRt", TMP "/c/homebrew/TEST00001", NULL));
    mp_inst_result_free(&r);
}

MP_TEST(copy_images_identical) {
    setup();
    mp_inst_result r;
    mp_err e;
    MP_CHECK(install(TMP "/d", USB "/basic.exfat", NULL, 0, NULL, 1, &r, &e) == 0 && r.status == MP_INST_DONE);
    if (e.msg[0]) fprintf(stderr, "  %s: %s\n", r.code ? r.code : "?", e.msg);
    mp_inst_result_free(&r);
    MP_CHECK(install(TMP "/d", USB "/x/Starfall-PPSA10001.ffpkg", NULL, 0, NULL, 0, &r, &e) == 0 &&
             r.status == MP_INST_DONE);
    mp_inst_result_free(&r);
    size_t a = 0, b = 0;
    uint8_t *x = mpt_slurp(USB "/basic.exfat", &a), *y = mpt_slurp(TMP "/d/homebrew/basic.exfat", &b);
    MP_CHECK(x && y && a == b && !memcmp(x, y, a));
    free(x);
    free(y);
    x = mpt_slurp(USB "/x/Starfall-PPSA10001.ffpkg", &a);
    y = mpt_slurp(TMP "/d/homebrew/Starfall-PPSA10001.ffpkg", &b);
    MP_CHECK(x && y && a == b && !memcmp(x, y, a));
    free(x);
    free(y);
    MP_CHECK(mp_path_kind(USB "/basic.exfat") == 1 && mp_path_kind(USB "/x/Starfall-PPSA10001.ffpkg") == 1);
    MP_CHECK(mp_path_kind(TMP "/d/maxpack/tmp/PPSA10001") == 0);
}

MP_TEST(copy_stop_resume) {
    setup();
    uint64_t total = mpt_dir_size(USB "/Juego");
    hook h = {total * 4 / 10, 0};
    mp_inst_result r;
    mp_err e;
    MP_CHECK(install(TMP "/f", USB "/Juego", NULL, 0, &h, 1, &r, &e) == 0 && r.status == MP_INST_STOPPED);
    // A file already marked as done, corrupted with the same size.
    size_t n = 0;
    uint8_t *b = mpt_slurp(TMP "/f/maxpack/tmp/TEST00003/eboot.bin", &n);
    if (b && n) {
        b[0] ^= 1;
        mpt_put(TMP "/f/maxpack/tmp/TEST00003/eboot.bin", b, n);
    }
    free(b);
    hook h2 = {0, 0};
    MP_CHECK(install(TMP "/f", USB "/Juego", NULL, 0, &h2, 0, &r, &e) == 0 && r.status == MP_INST_DONE);
    MP_CHECK(mpt_dirs_equal(USB "/Juego", TMP "/f/homebrew/TEST00003", NULL));
    mp_inst_result_free(&r);
}

MP_TEST(copy_verify) {
    setup();
    mp_inst_env env = env_for(TMP "/d", NULL, 0);
    mp_inst_verify_result v;
    mp_err e = {{0}};
    MP_CHECK(mp_inst_verify(&env, "PPSA10001", &v, &e) == 0);
    if (e.msg[0]) fprintf(stderr, "  %s\n", e.msg);
    MP_CHECK_EQ_U64(v.files_bad, 0);
    MP_CHECK(v.files_ok >= 1);
    size_t n = 0;
    uint8_t *b = mpt_slurp(TMP "/d/homebrew/Starfall-PPSA10001.ffpkg", &n);
    if (b && n) {
        b[n / 3] ^= 1;
        mpt_put(TMP "/d/homebrew/Starfall-PPSA10001.ffpkg", b, n);
    }
    free(b);
    MP_CHECK(mp_inst_verify(&env, "PPSA10001", &v, &e) == 0);
    MP_CHECK_EQ_U64(v.files_bad, 1);
    env = env_for(TMP "/a", NULL, 0);
    MP_CHECK(mp_inst_verify(&env, "TEST00003", &v, &e) == 0);
    MP_CHECK_EQ_U64(v.files_bad, 0);
    MP_CHECK_EQ_U64(v.files_ok, 24);
}

MP_TEST(copy_replace) {
    setup();
    mp_inst_result r;
    mp_err e;
    mpt_clear(TMP "/g");
    mpt_put_text(TMP "/g/homebrew/Starfall-PPSA10001.ffpkg", "vieja");
    MP_CHECK(install(TMP "/g", USB "/x/Starfall-PPSA10001.ffpkg", NULL, 0, NULL, 0, &r, &e) == 0);
    MP_CHECK(r.status == MP_INST_NEEDS_CONFIRM);
    mp_inst_result_free(&r);
    const char *rep[] = {TMP "/g/homebrew/Starfall-PPSA10001.ffpkg"};
    MP_CHECK(install(TMP "/g", USB "/x/Starfall-PPSA10001.ffpkg", rep, 1, NULL, 0, &r, &e) == 0);
    MP_CHECK(r.status == MP_INST_DONE);
    uint64_t size = 0;
    MP_CHECK(mp_stat(TMP "/g/homebrew/Starfall-PPSA10001.ffpkg", &size, NULL) == 0 && size == (1u << 20));
    mp_inst_result_free(&r);
}

int main(void) {
    MP_RUN(copy_folder_identical);
    MP_RUN(copy_packed_gets_runtime);
    MP_RUN(copy_images_identical);
    MP_RUN(copy_stop_resume);
    MP_RUN(copy_verify);
    MP_RUN(copy_replace);
    MP_TEST_MAIN_END();
}
