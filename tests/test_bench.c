// SPDX-License-Identifier: GPL-3.0-or-later
// maxpack measure: generated data packed at a few thread counts, and the disk of a folder.
#include <stdlib.h>

#include "mp_bench.h"
#include "mp_fixture.h"

#define TMP MP_TEST_TMP "/bench"

// Short trials: what is checked is what they report, not how fast this PC is.
static mp_bench_opts quick(const char *dir) {
    mp_bench_opts o;
    mp_bench_opts_default(&o);
    o.dir = dir;
    o.cpu_threads = 2;
    o.trial_seconds = 0.05;
    o.disk_seconds = 0.2;
    o.disk_max = 32u << 20;
    o.progress = 0;
    return o;
}

MP_TEST(bench_counts) {
    unsigned c[MP_BENCH_MAX_TRIALS];
    size_t n = mp_bench_counts(32, c);
    MP_CHECK(n == 4 && c[0] == 8 && c[1] == 16 && c[2] == 24 && c[3] == 32);
    n = mp_bench_counts(6, c);
    MP_CHECK(n == 4 && c[0] == 2 && c[1] == 3 && c[2] == 5 && c[3] == 6);
    n = mp_bench_counts(4, c);
    MP_CHECK(n == 4 && c[0] == 1 && c[1] == 2 && c[2] == 3 && c[3] == 4);
    n = mp_bench_counts(2, c);
    MP_CHECK(n == 2 && c[0] == 1 && c[1] == 2);
    n = mp_bench_counts(1, c);
    MP_CHECK(n == 1 && c[0] == 1);
    n = mp_bench_counts(0, c);
    MP_CHECK(n == 1 && c[0] == 1);
}

// The fastest, but a count with fewer threads less than 5 % slower is as good: the fewest such.
MP_TEST(bench_choose) {
    mp_bench_trial t[4] = {{8, 0, 0, 100}, {16, 0, 0, 189}, {24, 0, 0, 200}, {32, 0, 0, 199}};
    MP_CHECK_EQ_U64(mp_bench_choose(t, 4), 24);
    t[1].rate = 190;   // exactly 5 % slower: a tie
    MP_CHECK_EQ_U64(mp_bench_choose(t, 4), 16);
    mp_bench_trial same[3] = {{4, 0, 0, 50}, {8, 0, 0, 50}, {12, 0, 0, 50}};
    MP_CHECK_EQ_U64(mp_bench_choose(same, 3), 4);
    mp_bench_trial up[3] = {{4, 0, 0, 50}, {8, 0, 0, 90}, {12, 0, 0, 130}};
    MP_CHECK_EQ_U64(mp_bench_choose(up, 3), 12);
    MP_CHECK_EQ_U64(mp_bench_choose(up, 1), 4);
    MP_CHECK_EQ_U64(mp_bench_choose(up, 0), 0);
}

// The generated data: the same for a seed, another for another seed, and like a game's: some of it
// compresses well and some not at all.
MP_TEST(bench_fill) {
    size_t n = 8u << 20;
    uint8_t *a = malloc(n), *b = malloc(n), *c = malloc(n), *z = malloc(ZSTD_compressBound(n));
    if (!a || !b || !c || !z) return;
    mp_bench_fill(a, n, 1);
    mp_bench_fill(b, n, 1);
    mp_bench_fill(c, n, 2);
    MP_CHECK(memcmp(a, b, n) == 0);
    MP_CHECK(memcmp(a, c, n) != 0);
    size_t got = ZSTD_compress(z, ZSTD_compressBound(n), a, n, 3);
    MP_CHECK(!ZSTD_isError(got));
    double ratio = (double)got / (double)n;
    if (!(ratio > 0.3 && ratio < 0.9)) MP_FAIL_("the generated data compresses to %.3f", ratio);
    free(a);
    free(b);
    free(c);
    free(z);
}

typedef struct {
    int trials, events, backwards, total_changed, over;
    uint64_t done, total;
} seen;

static void on_trial(void *ctx, const mp_bench_trial *t) {
    seen *s = ctx;
    if (t->rate > 0 && t->threads > 0) s->trials++;
}

static void on_progress(void *ctx, const char *phase, uint64_t done, uint64_t total) {
    (void)phase;
    seen *s = ctx;
    if (s->events && done < s->done) s->backwards = 1;
    if (s->events && total != s->total) s->total_changed = 1;
    if (done > total) s->over = 1;
    s->done = done;
    s->total = total;
    s->events++;
}

// Each count of threads is tried and told, the disk of the folder is written and read, and nothing
// is left in it.
MP_TEST(bench_run) {
    mpt_clear(TMP);
    mp_err e = {{0}};
    MP_CHECK(mp_make_dirs(TMP "/dest", &e) == 0);
    seen s;
    memset(&s, 0, sizeof s);
    mp_bench_opts o = quick(TMP "/dest");
    o.on_trial = on_trial;
    o.ctx = &s;
    mp_progress_hook h = {on_progress, &s};
    mp_progress_set_hook(&h);
    mp_bench_report r;
    int rc = mp_bench_run(&o, &r, &e);
    mp_progress_set_hook(NULL);
    if (rc != 0) MP_FAIL_("mp_bench_run: %s", e.msg);
    if (rc != 0) return;
    MP_CHECK_EQ_U64(r.cpu_threads, 2);
    MP_CHECK_EQ_U64(r.level, 19);
    MP_CHECK_EQ_U64(r.ntrials, 2);
    MP_CHECK(r.trials[0].threads == 1 && r.trials[1].threads == 2);
    for (size_t i = 0; i < r.ntrials; i++) MP_CHECK(r.trials[i].bytes > 0 && r.trials[i].ns > 0 && r.trials[i].rate > 0);
    MP_CHECK(r.best == 1 || r.best == 2);
    MP_CHECK(r.fastest == 1 || r.fastest == 2);
    MP_CHECK_EQ_U64(r.best, mp_bench_choose(r.trials, r.ntrials));
    MP_CHECK_EQ_U64(s.trials, 2);
    MP_CHECK(s.events > 0 && !s.backwards && !s.total_changed && !s.over && s.done == s.total);
    MP_CHECK(r.disk == 1);
    if (r.disk != 1) MP_FAIL_("disk: %s", r.disk_error);
    MP_CHECK(r.written > 0 && r.written <= o.disk_max && r.write_rate > 0);
    MP_CHECK(r.read > 0 && r.read <= r.written && r.read_rate > 0);
    MP_CHECK(mp_dir_is_empty(TMP "/dest") == 1);
}

// Without a folder only the CPU is measured; a folder that is not there is told, and the CPU is
// measured all the same.
MP_TEST(bench_no_folder) {
    mp_err e = {{0}};
    mp_bench_opts o = quick(NULL);
    unsigned one[1] = {1};
    memcpy(o.counts, one, sizeof one);
    o.ncounts = 1;
    mp_bench_report r;
    MP_CHECK(mp_bench_run(&o, &r, &e) == 0);
    MP_CHECK(r.disk == 0 && r.ntrials == 1 && r.best == 1);
    o.dir = TMP "/not-there";
    MP_CHECK(mp_bench_run(&o, &r, &e) == 0);
    MP_CHECK(r.disk == -1 && r.disk_error[0]);
    MP_CHECK(r.ntrials == 1 && r.trials[0].rate > 0);
    MP_CHECK(mp_path_kind(TMP "/not-there") == 0);
}

// A file of the user with the name it would take stays as it is: another name is used, and goes.
MP_TEST(bench_keeps_what_is_there) {
    mpt_clear(TMP);
    mp_err e = {{0}};
    mpt_put_text(TMP "/dest2/.maxpack-measure.tmp", "mine");
    mp_bench_opts o = quick(TMP "/dest2");
    mp_bench_report r;
    MP_CHECK(mp_bench_run(&o, &r, &e) == 0);
    MP_CHECK(r.disk == 1);
    size_t len = 0;
    uint8_t *got = mpt_slurp(TMP "/dest2/.maxpack-measure.tmp", &len);
    MP_CHECK(got && len == 4 && !memcmp(got, "mine", 4));
    free(got);
    mpt_list l = mpt_list_dir(TMP "/dest2");
    MP_CHECK_EQ_U64(l.n, 1);
    mpt_list_free(&l);
}

// Cancelled, it stops with "cancelled" and leaves nothing.
MP_TEST(bench_cancel) {
    mpt_clear(TMP);
    mp_err e = {{0}};
    MP_CHECK(mp_make_dirs(TMP "/dest3", &e) == 0);
    mp_bench_opts o = quick(TMP "/dest3");
    mp_bench_report r;
    mp_cancel_request();
    int rc = mp_bench_run(&o, &r, &e);
    mp_cancel_reset();
    MP_CHECK(rc != 0);
    MP_CHECK_STR(e.msg, "cancelled");
    MP_CHECK(mp_dir_is_empty(TMP "/dest3") == 1);
}

// Cancelled while the disk is written: the temporary file goes all the same.
static int cancel_armed;
static void arm_on_last_trial(void *ctx, const mp_bench_trial *t) {
    (void)ctx;
    if (t->threads == 2) cancel_armed = 1;
}
static void cancel_when_armed(void *ctx, const char *phase, uint64_t done, uint64_t total) {
    (void)ctx;
    (void)phase;
    (void)done;
    (void)total;
    if (cancel_armed) mp_cancel_request();
}

MP_TEST(bench_cancel_while_writing) {
    mpt_clear(TMP);
    mp_err e = {{0}};
    MP_CHECK(mp_make_dirs(TMP "/dest4", &e) == 0);
    mp_bench_opts o = quick(TMP "/dest4");
    o.disk_seconds = 5;
    o.disk_max = 256u << 20;
    o.on_trial = arm_on_last_trial;
    cancel_armed = 0;
    mp_progress_hook h = {cancel_when_armed, NULL};
    mp_progress_set_hook(&h);
    mp_bench_report r;
    int rc = mp_bench_run(&o, &r, &e);
    mp_progress_set_hook(NULL);
    mp_cancel_reset();
    MP_CHECK(cancel_armed);
    MP_CHECK(rc != 0);
    MP_CHECK_STR(e.msg, "cancelled");
    MP_CHECK(mp_dir_is_empty(TMP "/dest4") == 1);
}

int main(void) {
    MP_RUN(bench_counts);
    MP_RUN(bench_choose);
    MP_RUN(bench_fill);
    MP_RUN(bench_run);
    MP_RUN(bench_no_folder);
    MP_RUN(bench_keeps_what_is_there);
    MP_RUN(bench_cancel);
    MP_RUN(bench_cancel_while_writing);
    MP_TEST_MAIN_END();
}
