// SPDX-License-Identifier: GPL-3.0-or-later
// The installer's file operations: rename, delete folders, free space, fsync, list.
#include <stdlib.h>

#include "mp_platform.h"
#include "mp_test.h"

#define TMP MP_TEST_TMP "/fsops"

static void put(const char *path, const char *text) {
    mp_err e = {{0}};
    if (mp_write_file(path, text, strlen(text), &e) != 0) MP_FAIL_("%s", e.msg);
}

MP_TEST(rename_moves_dir) {
    mp_err e = {{0}};
    mp_remove_tree(TMP "/ren", &e);
    MP_CHECK(mp_make_dirs(TMP "/ren/a/sub", &e) == 0);
    put(TMP "/ren/a/x.txt", "x");
    put(TMP "/ren/a/sub/y.txt", "yy");
    MP_CHECK(mp_rename(TMP "/ren/a", TMP "/ren/b", &e) == 0);
    MP_CHECK(mp_path_kind(TMP "/ren/a") == 0);
    MP_CHECK(mp_path_kind(TMP "/ren/b/x.txt") == 1);
    MP_CHECK(mp_path_kind(TMP "/ren/b/sub/y.txt") == 1);
    MP_CHECK(mp_make_dirs(TMP "/ren/c", &e) == 0);
    mp_err e2 = {{0}};
    MP_CHECK(mp_rename(TMP "/ren/b", TMP "/ren/c", &e2) == -1);
    MP_CHECK(e2.msg[0] != 0);
    MP_CHECK(mp_path_kind(TMP "/ren/b/x.txt") == 1);
}

MP_TEST(remove_tree_all) {
    mp_err e = {{0}};
    MP_CHECK(mp_make_dirs(TMP "/rt/n1/n2/n3", &e) == 0);
    put(TMP "/rt/n1/a.bin", "a");
    put(TMP "/rt/n1/n2/\xF0\x9F\x8E\xAE.bin", "emoji");
    put(TMP "/rt/n1/n2/n3/c.bin", "c");
    MP_CHECK(mp_remove_tree(TMP "/rt", &e) == 0);
    MP_CHECK(mp_path_kind(TMP "/rt") == 0);
    MP_CHECK(mp_remove_tree(TMP "/no-existe", &e) == 0);
    put(TMP "/suelto.bin", "s");
    MP_CHECK(mp_remove_tree(TMP "/suelto.bin", &e) == 0);
    MP_CHECK(mp_path_kind(TMP "/suelto.bin") == 0);
}

MP_TEST(disk_free_positive) {
    mp_err e = {{0}};
    uint64_t f = 0;
    MP_CHECK(mp_make_dirs(TMP, &e) == 0);
    MP_CHECK(mp_disk_free(TMP, &f, &e) == 0);
    MP_CHECK(f > 0);
    mp_err e2 = {{0}};
    MP_CHECK(mp_disk_free(TMP "/no-existe/x", &f, &e2) == -1);
    MP_CHECK(e2.msg[0] != 0);
}

typedef struct {
    int saw_a, saw_sub, saw_b;
    uint64_t a_size;
} list_ctx;

static int list_cb(void *ctx, const char *name, int is_dir, uint64_t size) {
    list_ctx *c = ctx;
    if (!strcmp(name, "a.txt") && !is_dir) {
        c->saw_a = 1;
        c->a_size = size;
    }
    if (!strcmp(name, "sub") && is_dir) c->saw_sub = 1;
    if (strstr(name, "b.txt")) c->saw_b = 1;
    return 0;
}

MP_TEST(dir_list_one_level) {
    mp_err e = {{0}};
    mp_remove_tree(TMP "/ls", &e);
    MP_CHECK(mp_make_dirs(TMP "/ls/sub", &e) == 0);
    put(TMP "/ls/a.txt", "abc");
    put(TMP "/ls/sub/b.txt", "b");
    list_ctx c = {0};
    MP_CHECK(mp_dir_list(TMP "/ls", list_cb, &c, &e) == 0);
    MP_CHECK(c.saw_a && c.saw_sub && !c.saw_b);
    MP_CHECK_EQ_U64(c.a_size, 3);
    MP_CHECK(mp_dir_list(TMP "/no-existe", list_cb, &c, &e) == -1);
}

MP_TEST(wfile_sync_ok) {
    mp_err e = {{0}};
    MP_CHECK(mp_make_dirs(TMP, &e) == 0);
    mp_remove_tree(TMP "/sync.bin", &e);
    mp_wfile *f = NULL;
    MP_CHECK(mp_wfile_create(TMP "/sync.bin", &f, &e) == 0);
    if (!f) return;
    MP_CHECK(mp_wfile_write(f, "datos", 5, &e) == 0);
    MP_CHECK(mp_wfile_sync(f, &e) == 0);
    MP_CHECK(mp_wfile_close(f, &e) == 0);
}

int main(void) {
    MP_RUN(rename_moves_dir);
    MP_RUN(remove_tree_all);
    MP_RUN(disk_free_positive);
    MP_RUN(dir_list_one_level);
    MP_RUN(wfile_sync_ok);
    MP_TEST_MAIN_END();
}
