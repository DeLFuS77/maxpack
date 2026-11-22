// SPDX-License-Identifier: GPL-3.0-or-later
#include <stdlib.h>

#include "mp_platform.h"
#include "mp_test.h"

#define TMP MP_TEST_TMP "/platform"

typedef struct {
    int found_file, found_empty, found_dir;
    uint64_t file_size;
} walk_ctx;

static int walk_cb(void *ctx, const char *rel, int is_dir, uint64_t size, int64_t mtime) {
    walk_ctx *w = ctx;
    (void)mtime;
    if (!is_dir && strcmp(rel, "ñandú/日本語.bin") == 0) {
        w->found_file = 1;
        w->file_size = size;
    }
    if (!is_dir && strcmp(rel, "a.txt") == 0 && size == 0) w->found_empty = 1;
    if (is_dir && strcmp(rel, "ñandú") == 0) w->found_dir = 1;
    return 0;
}

MP_TEST(walk_lists_unicode_names) {
    mp_err e = {{0}};
    MP_CHECK(mp_make_dirs(TMP "/walk/ñandú", &e) == 0);
    MP_CHECK(mp_write_file(TMP "/walk/ñandú/日本語.bin", "abc", 3, &e) == 0);
    MP_CHECK(mp_write_file(TMP "/walk/a.txt", "", 0, &e) == 0);
    walk_ctx w = {0};
    MP_CHECK(mp_dir_walk(TMP "/walk", walk_cb, &w, &e) == 0);
    MP_CHECK(w.found_file);
    MP_CHECK_EQ_U64(w.file_size, 3);
    MP_CHECK(w.found_empty);
    MP_CHECK(w.found_dir);
}

MP_TEST(pread_at_offsets) {
    mp_err e = {{0}};
    size_t n = 1000000;
    unsigned char *buf = malloc(n);
    for (size_t i = 0; i < n; i++) buf[i] = (unsigned char)((i * 7) % 251);
    MP_CHECK(mp_make_dirs(TMP, &e) == 0);
    MP_CHECK(mp_write_file(TMP "/pread.bin", buf, n, &e) == 0);
    mp_file *f = NULL;
    MP_CHECK(mp_file_open_read(TMP "/pread.bin", &f, &e) == 0);
    uint64_t size = 0;
    MP_CHECK(mp_file_size(f, &size, &e) == 0);
    MP_CHECK_EQ_U64(size, n);
    unsigned char got_buf[100];
    size_t got = 0;
    MP_CHECK(mp_file_pread(f, got_buf, 100, 999950, &got, &e) == 0);
    MP_CHECK_EQ_U64(got, 50);
    MP_CHECK(memcmp(got_buf, buf + 999950, 50) == 0);
    MP_CHECK(mp_file_pread(f, got_buf, 100, 1000000, &got, &e) == 0);
    MP_CHECK_EQ_U64(got, 0);
    mp_file_close(f);
    free(buf);
}

typedef struct {
    mp_mutex *mu;
    uint64_t counter;
} count_ctx;

static void count_worker(void *arg) {
    count_ctx *c = arg;
    for (int i = 0; i < 100000; i++) {
        mp_mutex_lock(c->mu);
        c->counter++;
        mp_mutex_unlock(c->mu);
    }
}

MP_TEST(threads_and_mutex) {
    count_ctx c = {mp_mutex_new(), 0};
    mp_thread *t[8];
    for (int i = 0; i < 8; i++) MP_CHECK(mp_thread_start(&t[i], count_worker, &c) == 0);
    for (int i = 0; i < 8; i++) mp_thread_join(t[i]);
    MP_CHECK_EQ_U64(c.counter, 800000);
    mp_mutex_free(c.mu);
    MP_CHECK(mp_cpu_count() >= 1);
}

MP_TEST(clock_and_sleep) {
    uint64_t a = mp_now_ns();
    mp_sleep_ms(20);
    uint64_t b = mp_now_ns();
    MP_CHECK(b >= a);
    MP_CHECK(b - a >= 15000000u);
}

MP_TEST(path_kind) {
    mp_err e = {{0}};
    MP_CHECK(mp_make_dirs(TMP "/kind", &e) == 0);
    MP_CHECK(mp_write_file(TMP "/kind/f.bin", "x", 1, &e) == 0);
    MP_CHECK(mp_path_kind(TMP "/kind") == 2);
    MP_CHECK(mp_path_kind(TMP "/kind/f.bin") == 1);
    MP_CHECK(mp_path_kind(TMP "/kind/no-existe") == 0);
}

int main(void) {
    MP_RUN(walk_lists_unicode_names);
    MP_RUN(pread_at_offsets);
    MP_RUN(threads_and_mutex);
    MP_RUN(clock_and_sleep);
    MP_RUN(path_kind);
    MP_TEST_MAIN_END();
}
