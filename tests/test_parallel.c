// SPDX-License-Identifier: GPL-3.0-or-later
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
    MP_RUN(xxh64_vector);
    MP_TEST_MAIN_END();
}
