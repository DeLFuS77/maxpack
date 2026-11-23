// SPDX-License-Identifier: GPL-3.0-or-later
// Enganche de progreso y cancelación del motor.
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "mp_fixture.h"
#include "mp_parallel.h"
#include "mp_test.h"

#define TMP MP_TEST_TMP "/cancel"

typedef struct {
    int calls, saw_pack, cancel_at_30;
    uint64_t last_done, last_total;
} rec;

static void on_progress(void *ctx, const char *phase, uint64_t done, uint64_t total) {
    rec *r = ctx;
    r->calls++;
    r->last_done = done;
    r->last_total = total;
    if (!strncmp(phase, "Empaquetando", 12)) r->saw_pack = 1;
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
    MP_CHECK_CONTAINS(e.msg, "cancelado");
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
    MP_CHECK_CONTAINS(e.msg, "cancelado");
    mp_p5z_close(p);
    clean();

    memset(&r, 0, sizeof r);
    r.cancel_at_30 = 1;
    set_hook(&r);
    memset(&e, 0, sizeof e);
    mp_verify_result vr;
    MP_CHECK(mp_verify_p5z(p5z, 4, 0, &vr, &e) == -1);
    MP_CHECK_CONTAINS(e.msg, "cancelado");
    clean();

    // Y sin cancelar, la verificación sigue siendo limpia.
    memset(&e, 0, sizeof e);
    MP_CHECK(mp_verify_p5z(p5z, 4, 0, &vr, &e) == 0);
    MP_CHECK_EQ_U64(vr.files_bad, 0);
}

MP_TEST(no_hook_unchanged) {
    clean();
    if (mpt_pack(MPT_BASIC, TMP "/plain_a.p5z", 1, NULL)) return;
    rec r = {0};
    set_hook(&r);
    mp_progress_set_hook(NULL);   // se puso y se quitó: no debe dejar rastro
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

int main(void) {
    MP_RUN(hook_reports_progress);
    MP_RUN(cancel_pack_p5z);
    MP_RUN(cancel_unpack_and_verify);
    MP_RUN(no_hook_unchanged);
    MP_TEST_MAIN_END();
}
