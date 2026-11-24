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

// Two threads take turns through one condition variable (signal wakes the other one).
typedef struct {
    mp_mutex *mu;
    mp_cond *cv;
    int turn;
    unsigned waiting, woken;
    int go;
} cond_ctx;

static void pong(void *arg) {
    cond_ctx *c = arg;
    mp_mutex_lock(c->mu);
    for (int i = 0; i < 2000; i++) {
        while (c->turn != 1) mp_cond_wait(c->cv, c->mu);
        c->turn = 0;
        mp_cond_signal(c->cv);
    }
    mp_mutex_unlock(c->mu);
}

MP_TEST(cond_signal_ping_pong) {
    cond_ctx c = {mp_mutex_new(), mp_cond_new(), 0, 0, 0, 0};
    MP_CHECK(c.mu && c.cv);
    if (!c.mu || !c.cv) return;
    mp_thread *t = NULL;
    MP_CHECK(mp_thread_start(&t, pong, &c) == 0);
    mp_mutex_lock(c.mu);
    for (int i = 0; i < 2000; i++) {
        c.turn = 1;
        mp_cond_signal(c.cv);
        while (c.turn != 0) mp_cond_wait(c.cv, c.mu);
    }
    mp_mutex_unlock(c.mu);
    mp_thread_join(t);
    MP_CHECK(c.turn == 0);
    mp_cond_free(c.cv);
    mp_mutex_free(c.mu);
}

static void wait_go(void *arg) {
    cond_ctx *c = arg;
    mp_mutex_lock(c->mu);
    c->waiting++;
    while (!c->go) mp_cond_wait(c->cv, c->mu);
    c->woken++;
    mp_mutex_unlock(c->mu);
}

// broadcast wakes all of them at once (with only a signal the join would never return).
MP_TEST(cond_broadcast_wakes_all) {
    cond_ctx c = {mp_mutex_new(), mp_cond_new(), 0, 0, 0, 0};
    MP_CHECK(c.mu && c.cv);
    if (!c.mu || !c.cv) return;
    mp_thread *t[8];
    for (int i = 0; i < 8; i++) MP_CHECK(mp_thread_start(&t[i], wait_go, &c) == 0);
    for (;;) {   // all of them are waiting
        mp_mutex_lock(c.mu);
        unsigned w = c.waiting;
        mp_mutex_unlock(c.mu);
        if (w == 8) break;
        mp_sleep_ms(1);
    }
    mp_sleep_ms(20);   // and inside mp_cond_wait
    mp_mutex_lock(c.mu);
    MP_CHECK_EQ_U64(c.woken, 0);
    c.go = 1;
    mp_cond_broadcast(c.cv);
    mp_mutex_unlock(c.mu);
    for (int i = 0; i < 8; i++) mp_thread_join(t[i]);
    MP_CHECK_EQ_U64(c.woken, 8);
    mp_cond_free(c.cv);
    mp_mutex_free(c.mu);
    mp_cond_free(NULL);
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
    MP_RUN(cond_signal_ping_pong);
    MP_RUN(cond_broadcast_wakes_all);
    MP_RUN(clock_and_sleep);
    MP_RUN(path_kind);
    MP_TEST_MAIN_END();
}
