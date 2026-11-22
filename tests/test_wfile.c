// SPDX-License-Identifier: GPL-3.0-or-later
#include <stdlib.h>

#include "mp_platform.h"
#include "mp_test.h"

#define TMP MP_TEST_TMP "/wfile"

static void remove_if_exists(const char *p) { remove(p); }

MP_TEST(wfile_write_patch_read) {
    mp_err e = {{0}};
    MP_CHECK(mp_make_dirs(TMP, &e) == 0);
    const char *p = TMP "/patch.bin";
    remove_if_exists(p);
    size_t n = 3u << 20;
    uint8_t *data = malloc(n), *back = malloc(n);
    for (size_t i = 0; i < n; i++) data[i] = (uint8_t)(i * 31 + (i >> 12));
    mp_wfile *f = NULL;
    MP_CHECK(mp_wfile_create(p, &f, &e) == 0);
    if (!f) return;
    MP_CHECK(mp_wfile_write(f, data, n / 2, &e) == 0);
    MP_CHECK(mp_wfile_write(f, data + n / 2, n - n / 2, &e) == 0);
    uint8_t patch[64];
    memset(patch, 0xAB, sizeof patch);
    MP_CHECK(mp_wfile_pwrite(f, patch, sizeof patch, 100, &e) == 0);
    memcpy(data + 100, patch, sizeof patch);
    MP_CHECK_EQ_U64(mp_wfile_size(f), n);
    MP_CHECK(mp_wfile_close(f, &e) == 0);
    mp_file *r = NULL;
    size_t got = 0;
    MP_CHECK(mp_file_open_read(p, &r, &e) == 0);
    MP_CHECK(r && mp_file_pread(r, back, n, 0, &got, &e) == 0);
    MP_CHECK_EQ_U64(got, n);
    MP_CHECK(memcmp(back, data, n) == 0);
    mp_file_close(r);
    free(data);
    free(back);
}

MP_TEST(wfile_create_refuses_existing) {
    mp_err e = {{0}};
    MP_CHECK(mp_make_dirs(TMP, &e) == 0);
    const char *p = TMP "/once.bin";
    remove_if_exists(p);
    mp_wfile *f = NULL;
    MP_CHECK(mp_wfile_create(p, &f, &e) == 0);
    if (f) MP_CHECK(mp_wfile_close(f, &e) == 0);
    mp_err e2 = {{0}};
    mp_wfile *g = NULL;
    MP_CHECK(mp_wfile_create(p, &g, &e2) == -1);
    MP_CHECK(g == NULL && e2.msg[0] != 0);
}

typedef struct {
    int64_t mtime;
    int found;
} mt_ctx;

static int mt_cb(void *ctx, const char *rel, int is_dir, uint64_t size, int64_t mtime) {
    mt_ctx *c = ctx;
    (void)size;
    if (!is_dir && strcmp(rel, "fecha.bin") == 0) {
        c->mtime = mtime;
        c->found = 1;
    }
    return 0;
}

MP_TEST(set_mtime_roundtrip) {
    mp_err e = {{0}};
    MP_CHECK(mp_make_dirs(TMP "/mt", &e) == 0);
    MP_CHECK(mp_write_file(TMP "/mt/fecha.bin", "x", 1, &e) == 0);
    MP_CHECK(mp_set_mtime(TMP "/mt/fecha.bin", 1714979290, &e) == 0);
    mt_ctx c = {0, 0};
    MP_CHECK(mp_dir_walk(TMP "/mt", mt_cb, &c, &e) == 0);
    MP_CHECK(c.found);
    MP_CHECK_EQ_U64(c.mtime, 1714979290);
}

MP_TEST(dir_is_empty) {
    mp_err e = {{0}};
    MP_CHECK(mp_make_dirs(TMP "/vacia", &e) == 0);
    remove_if_exists(TMP "/vacia/a.bin");
    MP_CHECK(mp_dir_is_empty(TMP "/vacia") == 1);
    MP_CHECK(mp_write_file(TMP "/vacia/a.bin", "x", 1, &e) == 0);
    MP_CHECK(mp_dir_is_empty(TMP "/vacia") == 0);
    remove_if_exists(TMP "/vacia/a.bin");
    MP_CHECK(mp_dir_is_empty(TMP "/no-existe") == -1);
}

MP_TEST(wfile_reopen_patches) {
    mp_err e = {{0}};
    MP_CHECK(mp_make_dirs(TMP, &e) == 0);
    const char *p = TMP "/reopen.bin";
    remove_if_exists(p);
    MP_CHECK(mp_write_file(p, "0123456789", 10, &e) == 0);
    mp_wfile *f = NULL;
    MP_CHECK(mp_wfile_reopen(p, &f, &e) == 0);
    if (!f) return;
    MP_CHECK_EQ_U64(mp_wfile_size(f), 10);
    MP_CHECK(mp_wfile_pwrite(f, "AB", 2, 3, &e) == 0);
    MP_CHECK(mp_wfile_close(f, &e) == 0);
    mp_file *r = NULL;
    char back[11] = {0};
    size_t got = 0;
    MP_CHECK(mp_file_open_read(p, &r, &e) == 0);
    MP_CHECK(r && mp_file_pread(r, back, 10, 0, &got, &e) == 0);
    mp_file_close(r);
    MP_CHECK_STR(back, "012AB56789");
    MP_CHECK(mp_wfile_reopen(TMP "/no-existe.bin", &f, &e) == -1);
}

int main(void) {
    MP_RUN(wfile_reopen_patches);
    MP_RUN(wfile_write_patch_read);
    MP_RUN(wfile_create_refuses_existing);
    MP_RUN(set_mtime_roundtrip);
    MP_RUN(dir_is_empty);
    MP_TEST_MAIN_END();
}
