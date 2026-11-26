// SPDX-License-Identifier: GPL-3.0-or-later
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "mp_hash.h"
#include "mp_parallel.h"
#include "mp_test.h"

static int mark(void *ctx, size_t item, unsigned tid) {
    (void)tid;
    ((int *)ctx)[item]++;
    return 0;
}

static int fail_at_50(void *ctx, size_t item, unsigned tid) {
    (void)ctx;
    (void)tid;
    return item == 50 ? 3 : 0;
}

MP_TEST(parallel_covers_all_items) {
    int *seen = calloc(10000, sizeof *seen);
    MP_CHECK(mp_parallel_run(7, 10000, mark, seen, NULL) == 0);
    int bad = 0;
    for (int i = 0; i < 10000; i++) bad += seen[i] != 1;
    MP_CHECK_EQ_U64(bad, 0);
    free(seen);
    MP_CHECK(mp_parallel_run(4, 0, mark, NULL, NULL) == 0);
}

MP_TEST(parallel_stops_on_error) { MP_CHECK(mp_parallel_run(4, 1000, fail_at_50, NULL, NULL) == 3); }

// ---- mp_pipe ---------------------------------------------------------------------------------

#define NSLOTS 16

typedef struct {
    uint64_t in[NSLOTS], out[NSLOTS], junk[NSLOTS];
    unsigned max_tid;
    int calls;
    mp_mutex *mu;
    uint64_t fail_value;   // work returns 7 for that input (0 = never)
    int pause_at;          // the call of work that asks for a pause (0 = none)
    mp_pipe *pipe;
    uint64_t n;            // the producer thread submits 1..n
} pctx;

// Uneven work: some slots finish long after later ones.
static int square_work(void *ctx, size_t slot, unsigned tid) {
    pctx *c = ctx;
    uint64_t v = c->in[slot], x = v;
    unsigned spin = (unsigned)(v * 2654435761u) % 20000;
    for (unsigned i = 0; i < spin; i++) x = x * 6364136223846793005ull + 1442695040888963407ull;
    c->junk[slot] = x;
    c->out[slot] = v * v;
    mp_mutex_lock(c->mu);
    c->calls++;
    if (tid > c->max_tid) c->max_tid = tid;
    if (c->pause_at && c->calls == c->pause_at) mp_pause_set(1);
    mp_mutex_unlock(c->mu);
    return c->fail_value && v == c->fail_value ? 7 : 0;
}

static void producer(void *arg) {
    pctx *c = arg;
    for (uint64_t v = 1; v <= c->n; v++) {
        long s = mp_pipe_acquire(c->pipe);
        if (s < 0) return;
        c->in[s] = v;
        mp_pipe_submit(c->pipe, (size_t)s, v % 5 != 0);   // every fifth needs no work
    }
    mp_pipe_close(c->pipe);
}

MP_TEST(pipe_keeps_order_two_threads) {
    unsigned threads[] = {1, 3, 8, 40};
    for (int k = 0; k < 4; k++) {
        pctx c;
        memset(&c, 0, sizeof c);
        c.mu = mp_mutex_new();
        c.n = 20000;
        c.pipe = mp_pipe_new(threads[k], NSLOTS, square_work, &c);
        MP_CHECK(c.pipe != NULL);
        if (!c.pipe) return;
        mp_thread *t = NULL;
        MP_CHECK(mp_thread_start(&t, producer, &c) == 0);
        uint64_t want = 1, bad = 0;
        int rc = 0;
        long s;
        while ((s = mp_pipe_next(c.pipe, 1, &rc)) >= 0) {
            uint64_t v = c.in[s];
            if (v != want || rc != 0 || (v % 5 != 0 && c.out[s] != v * v)) bad++;
            want++;
            mp_pipe_release(c.pipe, (size_t)s);
        }
        mp_thread_join(t);
        MP_CHECK_EQ_U64(bad, 0);
        MP_CHECK_EQ_U64(want, c.n + 1);
        MP_CHECK_EQ_U64(c.calls, c.n - c.n / 5);
        MP_CHECK(c.max_tid < threads[k]);
        MP_CHECK_EQ_U64(mp_pipe_pending(c.pipe), 0);
        mp_pipe_free(c.pipe);
        mp_mutex_free(c.mu);
    }
}

// The same thread fills and empties it (as the .p5z frames do): it never waits for a free slot.
MP_TEST(pipe_same_thread) {
    pctx c;
    memset(&c, 0, sizeof c);
    c.mu = mp_mutex_new();
    c.fail_value = 777;
    mp_pipe *p = mp_pipe_new(4, NSLOTS, square_work, &c);
    MP_CHECK(p != NULL);
    if (!p) return;
    uint64_t want = 1, bad = 0, failed_at = 0;
    for (uint64_t v = 1; v <= 5000; v++) {
        if (mp_pipe_pending(p) == NSLOTS) {   // all in use: the oldest first
            int rc = 0;
            long s = mp_pipe_next(p, 1, &rc);
            if (s < 0 || c.in[s] != want++) bad++;
            if (s >= 0 && rc == 7) failed_at = c.in[s];
            if (s >= 0) mp_pipe_release(p, (size_t)s);
        }
        long s = mp_pipe_acquire(p);
        if (s < 0) {
            bad++;
            break;
        }
        c.in[s] = v;
        mp_pipe_submit(p, (size_t)s, 1);
        int rc = 0;
        while ((s = mp_pipe_next(p, 0, &rc)) >= 0) {   // and whatever is already done
            if (c.in[s] != want++ || c.out[s] != c.in[s] * c.in[s]) bad++;
            if (rc == 7) failed_at = c.in[s];
            mp_pipe_release(p, (size_t)s);
        }
    }
    while (mp_pipe_pending(p)) {
        int rc = 0;
        long s = mp_pipe_next(p, 1, &rc);
        if (s < 0 || c.in[s] != want++) {
            bad++;
            break;
        }
        if (rc == 7) failed_at = c.in[s];
        mp_pipe_release(p, (size_t)s);
    }
    MP_CHECK_EQ_U64(bad, 0);
    MP_CHECK_EQ_U64(want, 5001);
    MP_CHECK_EQ_U64(failed_at, 777);   // the work's code reaches the consumer with its slot
    int rc = 0;
    MP_CHECK(mp_pipe_next(p, 0, &rc) == -1);   // nothing pending: does not wait
    mp_pipe_free(p);
    mp_mutex_free(c.mu);
}

// The producer waits for a free slot; abort wakes it and the consumer.
MP_TEST(pipe_abort_wakes_everyone) {
    pctx c;
    memset(&c, 0, sizeof c);
    c.mu = mp_mutex_new();
    c.n = 1000;
    c.pipe = mp_pipe_new(2, NSLOTS, square_work, &c);
    if (!c.pipe) {
        MP_FAIL_("mp_pipe_new");
        return;
    }
    mp_thread *t = NULL;
    MP_CHECK(mp_thread_start(&t, producer, &c) == 0);
    while (mp_pipe_pending(c.pipe) < NSLOTS) mp_sleep_ms(1);   // full: the producer is waiting
    mp_sleep_ms(20);
    mp_pipe_abort(c.pipe);
    mp_thread_join(t);   // its mp_pipe_acquire returned -1
    int rc = 0;
    MP_CHECK(mp_pipe_next(c.pipe, 1, &rc) == -1);
    MP_CHECK(mp_pipe_acquire(c.pipe) == -1);
    mp_pipe_free(c.pipe);   // slots never taken back: it still joins
    mp_mutex_free(c.mu);
}

// After mp_cancel_request the slots are no longer passed to work: code -1.
MP_TEST(pipe_cancelled) {
    pctx c;
    memset(&c, 0, sizeof c);
    c.mu = mp_mutex_new();
    mp_pipe *p = mp_pipe_new(3, NSLOTS, square_work, &c);
    if (!p) {
        MP_FAIL_("mp_pipe_new");
        return;
    }
    mp_cancel_request();
    for (int i = 0; i < 10; i++) {
        long s = mp_pipe_acquire(p);
        c.in[s] = (uint64_t)i + 1;
        mp_pipe_submit(p, (size_t)s, 1);
    }
    mp_pipe_close(p);
    int n = 0, minus = 0, rc = 0;
    long s;
    while ((s = mp_pipe_next(p, 1, &rc)) >= 0) {
        n++;
        minus += rc == -1;
        mp_pipe_release(p, (size_t)s);
    }
    mp_cancel_reset();
    MP_CHECK_EQ_U64(n, 10);
    MP_CHECK_EQ_U64(minus, 10);
    MP_CHECK_EQ_U64(c.calls, 0);
    mp_pipe_free(p);
    mp_pipe_free(NULL);
    mp_mutex_free(c.mu);
    MP_CHECK(mp_pipe_new(2, 0, square_work, &c) == NULL);
}

// ---- pause -----------------------------------------------------------------------------------

// Whether `threads` threads wait in the pause (it gives them 10 s to get there).
static int parked(unsigned threads) {
    for (int i = 0; i < 2000; i++) {
        if (mp_pause_waiting() >= threads) return 1;
        mp_sleep_ms(5);
    }
    return 0;
}

typedef struct {
    mp_mutex *mu;
    int calls, pause_at;   // the call that asks for a pause (0 = none)
    int *seen;
    size_t n;
    unsigned threads;
    int rc;
} pause_ctx;

static int pausing_work(void *ctx, size_t item, unsigned tid) {
    (void)tid;
    pause_ctx *c = ctx;
    mp_mutex_lock(c->mu);
    int n = ++c->calls;
    c->seen[item]++;
    mp_mutex_unlock(c->mu);
    if (n == c->pause_at) mp_pause_set(1);
    return 0;
}

static int calls_of(mp_mutex *mu, const int *calls) {
    mp_mutex_lock(mu);
    int n = *calls;
    mp_mutex_unlock(mu);
    return n;
}

static void run_pausing(void *arg) {
    pause_ctx *c = arg;
    c->rc = mp_parallel_run(c->threads, c->n, pausing_work, c, NULL);
}

// A pause asked for during the 10th item: each thread ends the item it has and takes no other until
// it is resumed; then every item is done once.
MP_TEST(parallel_pauses_between_items) {
    static int seen[1000];
    memset(seen, 0, sizeof seen);
    pause_ctx c = {mp_mutex_new(), 0, 10, seen, 1000, 3, -5};
    mp_thread *t = NULL;
    MP_CHECK(mp_thread_start(&t, run_pausing, &c) == 0);
    MP_CHECK(parked(3));
    int at = calls_of(c.mu, &c.calls);
    MP_CHECK(at >= 10 && at <= 12);   // the 10th, and at most the one each other thread had
    mp_sleep_ms(100);
    MP_CHECK_EQ_U64(calls_of(c.mu, &c.calls), at);
    MP_CHECK(mp_paused());
    mp_pause_set(0);
    mp_thread_join(t);
    MP_CHECK(!mp_paused());
    MP_CHECK(c.rc == 0);
    MP_CHECK_EQ_U64(c.calls, 1000);
    int bad = 0;
    for (int i = 0; i < 1000; i++) bad += seen[i] != 1;
    MP_CHECK_EQ_U64(bad, 0);
    MP_CHECK_EQ_U64(mp_pause_waiting(), 0);
    mp_mutex_free(c.mu);
}

// A cancel while paused ends the wait: the run stops as a cancel does.
MP_TEST(parallel_cancel_while_paused) {
    static int seen[1000];
    memset(seen, 0, sizeof seen);
    pause_ctx c = {mp_mutex_new(), 0, 5, seen, 1000, 2, -5};
    mp_thread *t = NULL;
    MP_CHECK(mp_thread_start(&t, run_pausing, &c) == 0);
    MP_CHECK(parked(2));
    mp_cancel_request();
    mp_thread_join(t);
    MP_CHECK(c.rc == -1);
    MP_CHECK(c.calls < 1000);
    MP_CHECK_EQ_U64(mp_pause_waiting(), 0);
    mp_cancel_reset();
    mp_pause_set(0);
    mp_mutex_free(c.mu);
}

// The pipe's threads end the slot they have and take no other while paused; resumed, every slot
// comes out in order with its result.
MP_TEST(pipe_pauses_between_slots) {
    pctx c;
    memset(&c, 0, sizeof c);
    c.mu = mp_mutex_new();
    c.pause_at = 3;
    mp_pipe *p = mp_pipe_new(2, NSLOTS, square_work, &c);
    MP_CHECK(p != NULL);
    if (!p) return;
    for (int i = 0; i < NSLOTS; i++) {
        long s = mp_pipe_acquire(p);
        c.in[s] = (uint64_t)i + 1;
        mp_pipe_submit(p, (size_t)s, 1);
    }
    MP_CHECK(parked(2));
    int at = calls_of(c.mu, &c.calls);
    MP_CHECK(at >= 3 && at <= 4);
    mp_sleep_ms(100);
    MP_CHECK_EQ_U64(calls_of(c.mu, &c.calls), at);
    mp_pause_set(0);
    uint64_t bad = 0;
    for (int i = 0; i < NSLOTS; i++) {
        int rc = -5;
        long s = mp_pipe_next(p, 1, &rc);
        if (s < 0 || rc != 0 || c.in[s] != (uint64_t)i + 1 || c.out[s] != c.in[s] * c.in[s]) bad++;
        if (s >= 0) mp_pipe_release(p, (size_t)s);
    }
    MP_CHECK_EQ_U64(bad, 0);
    MP_CHECK_EQ_U64(c.calls, NSLOTS);
    mp_pipe_free(p);
    mp_mutex_free(c.mu);
}

// Stopped while its threads wait in the pause (the writer failed): they go, nothing hangs.
MP_TEST(pipe_freed_while_paused) {
    pctx c;
    memset(&c, 0, sizeof c);
    c.mu = mp_mutex_new();
    c.pause_at = 1;
    mp_pipe *p = mp_pipe_new(3, NSLOTS, square_work, &c);
    MP_CHECK(p != NULL);
    if (!p) return;
    for (int i = 0; i < NSLOTS; i++) {
        long s = mp_pipe_acquire(p);
        c.in[s] = (uint64_t)i + 1;
        mp_pipe_submit(p, (size_t)s, 1);
    }
    MP_CHECK(parked(3));
    mp_pipe_abort(p);
    mp_pipe_free(p);
    MP_CHECK_EQ_U64(mp_pause_waiting(), 0);
    MP_CHECK(c.calls < NSLOTS);
    mp_pause_set(0);
    mp_mutex_free(c.mu);
}

typedef struct {
    mp_mutex *mu;
    int n;
    char msg[64];
} check_ctx;

static void check_loop(void *arg) {
    check_ctx *x = arg;
    mp_err e = {{0}};
    while (mp_cancel_check(&e) == 0) {
        mp_mutex_lock(x->mu);
        if (++x->n == 5) mp_pause_set(1);
        mp_mutex_unlock(x->mu);
    }
    snprintf(x->msg, sizeof x->msg, "%s", e.msg);
}

// The loops of pack, unpack and verify stop at their next mp_cancel_check while paused, go on once
// resumed, and a cancel while paused ends them as a cancel does.
MP_TEST(cancel_check_waits_while_paused) {
    check_ctx x = {mp_mutex_new(), 0, ""};
    mp_thread *t = NULL;
    MP_CHECK(mp_thread_start(&t, check_loop, &x) == 0);
    MP_CHECK(parked(1));
    MP_CHECK_EQ_U64(calls_of(x.mu, &x.n), 5);
    mp_sleep_ms(50);
    MP_CHECK_EQ_U64(calls_of(x.mu, &x.n), 5);
    mp_pause_set(0);
    for (int i = 0; i < 2000 && calls_of(x.mu, &x.n) < 100; i++) mp_sleep_ms(5);
    MP_CHECK(calls_of(x.mu, &x.n) >= 100);
    mp_pause_set(1);
    MP_CHECK(parked(1));
    mp_cancel_request();
    mp_thread_join(t);
    MP_CHECK_STR(x.msg, "cancelled");
    mp_cancel_reset();
    mp_pause_set(0);
    mp_mutex_free(x.mu);
}

MP_TEST(xxh64_vector) {
    MP_CHECK_EQ_U64(mp_xxh64("", 0, 0), 0xEF46DB3751D8E999ULL);
    MP_CHECK(mp_xxh64("a", 1, 0) != mp_xxh64("b", 1, 0));
}

int main(void) {
    MP_RUN(parallel_covers_all_items);
    MP_RUN(parallel_stops_on_error);
    MP_RUN(pipe_keeps_order_two_threads);
    MP_RUN(pipe_same_thread);
    MP_RUN(pipe_abort_wakes_everyone);
    MP_RUN(pipe_cancelled);
    MP_RUN(parallel_pauses_between_items);
    MP_RUN(parallel_cancel_while_paused);
    MP_RUN(pipe_pauses_between_slots);
    MP_RUN(pipe_freed_while_paused);
    MP_RUN(cancel_check_waits_while_paused);
    MP_RUN(xxh64_vector);
    MP_TEST_MAIN_END();
}
