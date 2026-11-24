// SPDX-License-Identifier: GPL-3.0-or-later
// Progress hook and cancellation of the engine.
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "mp_fixture.h"
#include "mp_parallel.h"
#include "mp_test.h"

#define TMP MP_TEST_TMP "/cancel"

typedef struct {
    int calls, saw_pack, cancel_at_30;
    int cancel_mid;        // cancels on the first call with done < total
    int cancelled_mid;     // that call happened
    int reached_total;     // after cancelling there was a call with done == total
    uint64_t last_done, last_total, cancel_done, cancel_total;
} rec;

static void on_progress(void *ctx, const char *phase, uint64_t done, uint64_t total) {
    rec *r = ctx;
    r->calls++;
    r->last_done = done;
    r->last_total = total;
    if (!strncmp(phase, "Packing", 7)) r->saw_pack = 1;
    if (r->cancelled_mid && total && done >= total) r->reached_total = 1;
    if (r->cancel_mid && !r->cancelled_mid && total && done < total) {
        r->cancelled_mid = 1;
        r->cancel_done = done;
        r->cancel_total = total;
        mp_cancel_request();
    }
    if (r->cancel_at_30 && total && done * 10 >= total * 3) mp_cancel_request();
}

static void set_hook(rec *r) {
    mp_progress_hook h = {on_progress, r};
    mp_progress_set_hook(&h);
}

static void clean(void) {
    mp_progress_set_hook(NULL);
    mp_cancel_reset();
}

MP_TEST(hook_reports_progress) {
    clean();
    rec r = {0};
    set_hook(&r);
    int rc = mpt_pack(MPT_BASIC, TMP "/hook.p5z", 1, NULL);
    clean();
    MP_CHECK(rc == 0);
    MP_CHECK(r.calls >= 2);
    MP_CHECK(r.last_total > 0);
    MP_CHECK_EQ_U64(r.last_done, r.last_total);
    MP_CHECK(r.saw_pack);
}

MP_TEST(cancel_pack_p5z) {
    clean();
    const char *dst = TMP "/cancel_pack.p5z";
    mpt_clear(dst);
    mpt_parent_dirs(dst);
    rec r = {0};
    r.cancel_at_30 = 1;
    set_hook(&r);
    mp_err e = {{0}};
    mp_pack_opts o;
    mp_pack_opts_default(&o);
    o.threads = 4;
    o.progress = 0;
    mp_pack_out out;
    MP_CHECK(mp_pack_out_p5z(dst, 4, &out, &e) == 0);
    mp_pack_result res;
    int rc = mp_pack_run(MPT_BASIC, &out, &o, &res, &e);
    out.destroy(out.ctx);
    clean();
    MP_CHECK(rc == -1);
    MP_CHECK_CONTAINS(e.msg, "cancelled");
    MP_CHECK_EQ_U64(mp_path_kind(dst), 0);
}

MP_TEST(cancel_unpack_and_verify) {
    clean();
    const char *p5z = TMP "/for_cancel.p5z";
    if (mpt_pack(MPT_BASIC, p5z, 1, NULL)) return;
    mp_p5z *p = NULL;
    mp_err e = {{0}};
    MP_CHECK(mp_p5z_open(p5z, &p, &e) == 0);
    if (!p) return;
    mpt_clear(TMP "/cancel_out");
    rec r = {0};
    r.cancel_at_30 = 1;
    set_hook(&r);
    MP_CHECK(mp_unpack_original(p, TMP "/cancel_out", 4, 0, &e) == -1);
    MP_CHECK_CONTAINS(e.msg, "cancelled");
    mp_p5z_close(p);
    clean();

    memset(&r, 0, sizeof r);
    r.cancel_at_30 = 1;
    set_hook(&r);
    memset(&e, 0, sizeof e);
    mp_verify_result vr;
    MP_CHECK(mp_verify_p5z(p5z, 4, 0, &vr, &e) == -1);
    MP_CHECK_CONTAINS(e.msg, "cancelled");
    clean();

    // And without cancelling, verification is still clean.
    memset(&e, 0, sizeof e);
    MP_CHECK(mp_verify_p5z(p5z, 4, 0, &vr, &e) == 0);
    MP_CHECK_EQ_U64(vr.files_bad, 0);
}

MP_TEST(no_hook_unchanged) {
    clean();
    if (mpt_pack(MPT_BASIC, TMP "/plain_a.p5z", 1, NULL)) return;
    rec r = {0};
    set_hook(&r);
    mp_progress_set_hook(NULL);   // it was set and removed: it must leave no trace
    mp_cancel_request();
    mp_cancel_reset();
    if (mpt_pack(MPT_BASIC, TMP "/plain_b.p5z", 1, NULL)) return;
    MP_CHECK_EQ_U64(r.calls, 0);
    size_t na = 0, nb = 0;
    uint8_t *a = mpt_slurp(TMP "/plain_a.p5z", &na), *b = mpt_slurp(TMP "/plain_b.p5z", &nb);
    MP_CHECK(a && b && na == nb && na > 0 && memcmp(a, b, na) == 0);
    free(a);
    free(b);
}

// Input of several MiB: cancellation lands in the middle of a loop, not at the end of a phase.
#define BIG TMP "/big_src"
static int make_big(void) {
    if (mp_path_kind(BIG "/f5.bin") == 1) return 0;
    mpt_clear(BIG);
    uint32_t x = 12345;
    size_t n = 2u << 20;
    uint8_t *b = malloc(n);
    if (!b) return -1;
    for (int f = 0; f < 6; f++) {
        for (size_t i = 0; i < n; i++) {
            x ^= x << 13;
            x ^= x >> 17;
            x ^= x << 5;
            b[i] = (uint8_t)(x >> 24);
        }
        char path[256];
        snprintf(path, sizeof path, BIG "/data/f%d.bin", f);
        mpt_put(path, b, n);
    }
    free(b);
    mpt_put_text(BIG "/sce_sys/param.json", "{\"titleId\":\"TEST00002\"}");
    return 0;
}

static void check_mid(const rec *r) {
    MP_CHECK(r->cancelled_mid);
    MP_CHECK(r->cancel_done < r->cancel_total);
    MP_CHECK(!r->reached_total);   // it stopped before the phase finished
}

MP_TEST(cancel_mid_pack_p5z) {
    clean();
    if (make_big()) return;
    const char *dst = TMP "/mid_pack.p5z";
    mpt_clear(dst);
    rec r = {0};
    r.cancel_mid = 1;
    set_hook(&r);
    mp_err e = {{0}};
    mp_pack_opts o;
    mp_pack_opts_default(&o);
    o.threads = 4;
    o.progress = 0;
    mp_pack_out out;
    MP_CHECK(mp_pack_out_p5z(dst, 4, &out, &e) == 0);
    mp_pack_result res;
    int rc = mp_pack_run(BIG, &out, &o, &res, &e);
    out.destroy(out.ctx);
    clean();
    MP_CHECK(rc == -1);
    MP_CHECK_CONTAINS(e.msg, "cancelled");
    MP_CHECK_EQ_U64(mp_path_kind(dst), 0);
    check_mid(&r);
}

MP_TEST(cancel_mid_unpack_and_verify) {
    clean();
    if (make_big()) return;
    const char *p5z = TMP "/mid_src.p5z";
    if (mpt_pack(BIG, p5z, 1, NULL)) return;
    mp_p5z *p = NULL;
    mp_err e = {{0}};
    MP_CHECK(mp_p5z_open(p5z, &p, &e) == 0);
    if (!p) return;
    mpt_clear(TMP "/mid_out");
    rec r = {0};
    r.cancel_mid = 1;
    set_hook(&r);
    MP_CHECK(mp_unpack_original(p, TMP "/mid_out", 4, 0, &e) == -1);
    MP_CHECK_CONTAINS(e.msg, "cancelled");
    mp_p5z_close(p);
    clean();
    check_mid(&r);

    memset(&r, 0, sizeof r);
    r.cancel_mid = 1;
    set_hook(&r);
    memset(&e, 0, sizeof e);
    mp_verify_result vr;
    MP_CHECK(mp_verify_p5z(p5z, 4, 0, &vr, &e) == -1);
    MP_CHECK_CONTAINS(e.msg, "cancelled");
    clean();
    check_mid(&r);
}

int main(void) {
    MP_RUN(hook_reports_progress);
    MP_RUN(cancel_pack_p5z);
    MP_RUN(cancel_unpack_and_verify);
    MP_RUN(cancel_mid_pack_p5z);
    MP_RUN(cancel_mid_unpack_and_verify);
    MP_RUN(no_hook_unchanged);
    MP_TEST_MAIN_END();
}
