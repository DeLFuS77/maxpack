// SPDX-License-Identifier: GPL-3.0-or-later
// ShadowMountPlus: registro del juego y reintentos, contra un ShadowMountPlus falso.
#include "mp_fixture.h"
#include "mp_http.h"
#include "mp_inst.h"
#include "mp_smp.h"

#define TMP MP_TEST_TMP "/smp"

typedef struct {
    mp_mutex *mu;
    char log[16384];      // "ruta cuerpo\n" por llamada
    int scan_deferred;    // scan responde aplazado
    int fakelib_404;      // games/fakelib no conoce el juego
    int settings_seen;
} fake;

static void fake_handler(void *ctx, const mp_http_req *rq, mp_http_resp *rs) {
    fake *f = ctx;
    mp_mutex_lock(f->mu);
    size_t k = strlen(f->log);
    snprintf(f->log + k, sizeof f->log - k, "%s %.*s\n", rq->path, (int)rq->body_len, rq->body);
    if (strstr(rq->path, "settings")) f->settings_seen = 1;
    int deferred = f->scan_deferred, f404 = f->fakelib_404;
    mp_mutex_unlock(f->mu);
    const char *body = "{\"status\":0}";
    rs->status = 200;
    if (!strcmp(rq->path, "/api/v1/version"))
        body = "{\"status\":0,\"api_version\":1,\"shadowmount_version\":\"1.7\"}";
    else if (!strcmp(rq->path, "/api/v1/scan"))
        body = deferred ? "{\"status\":0,\"scan_queued\":false,\"scan_deferred\":true,\"scan_deferred_reason\":\"game_active\"}"
                        : "{\"status\":0,\"scan_queued\":true,\"scan_deferred\":false,\"scan_deferred_reason\":\"\"}";
    else if (!strcmp(rq->path, "/api/v1/games/fakelib") && f404) {
        rs->status = 404;
        body = "{\"status\":-1,\"error\":\"unknown title\"}";
    } else if (!strcmp(rq->path, "/api/v1/games"))
        body = "{\"status\":0,\"count\":2,\"games\":[{\"title_id\":\"PPSA00001\",\"path\":\"/data/homebrew/A\","
               "\"source_type\":\"folder\",\"mounted\":true},{\"title_id\":\"PPSA00002\",\"path\":\"/mnt/usb0/B.ffpkg\","
               "\"source_type\":\"image\"}]}";
    rs->body = mp_strdup(body);
    rs->body_len = strlen(body);
}

static fake F;
static mp_http_server *srv;
static uint16_t port;

static void start_fake(uint16_t p) {
    mp_err e = {{0}};
    if (!F.mu) F.mu = mp_mutex_new();
    srv = mp_http_start("127.0.0.1", p, fake_handler, &F, &e);
    if (!srv) MP_FAIL_("ShadowMountPlus falso: %s", e.msg);
    else port = mp_http_port(srv);
}

static void stop_fake(void) {
    mp_http_stop(srv);
    srv = NULL;
}

static mp_smp_cfg cfg(void) {
    mp_smp_cfg c;
    mp_smp_cfg_default(&c);
    c.port = port;
    c.timeout_ms = 500;
    return c;
}

static void reset_log(void) {
    mp_mutex_lock(F.mu);
    F.log[0] = 0;
    F.scan_deferred = F.fakelib_404 = 0;
    mp_mutex_unlock(F.mu);
}

MP_TEST(smp_register_order) {
    start_fake(0);
    reset_log();
    mp_smp_cfg c = cfg();
    char why[256];
    MP_CHECK(mp_smp_alive(&c, why, sizeof why) == 1);
    MP_CHECK_STR(why, "1.7");
    reset_log();
    MP_CHECK(mp_smp_register(&c, "TEST00001", "/data/homebrew/TEST00001", 1, why, sizeof why) == 0);
    MP_CHECK_STR(F.log, "/api/v1/games/fakelib {\"title_id\":\"TEST00001\",\"mode\":\"full\"}\n"
                        "/api/v1/manual/add {\"path\":\"/data/homebrew/TEST00001\"}\n"
                        "/api/v1/scan {\"reset_attempts\":false}\n");
    reset_log();
    MP_CHECK(mp_smp_register(&c, "TEST00001", "/data/homebrew/TEST00001", 0, why, sizeof why) == 0);
    MP_CHECK_STR(F.log, "/api/v1/manual/add {\"path\":\"/data/homebrew/TEST00001\"}\n"
                        "/api/v1/scan {\"reset_attempts\":false}\n");
    reset_log();
    MP_CHECK(mp_smp_forget(&c, "/data/homebrew/TEST00001", why, sizeof why) == 0);
    MP_CHECK_STR(F.log, "/api/v1/manual/remove {\"path\":\"/data/homebrew/TEST00001\"}\n"
                        "/api/v1/scan {\"reset_attempts\":false}\n");
}

MP_TEST(smp_scan_deferred) {
    reset_log();
    F.scan_deferred = 1;
    mp_smp_cfg c = cfg();
    char why[256];
    MP_CHECK(mp_smp_register(&c, "TEST00001", "/x", 1, why, sizeof why) == 1);
    MP_CHECK_CONTAINS(why, "game_active");
    reset_log();
    F.fakelib_404 = 1;
    MP_CHECK(mp_smp_register(&c, "TEST00001", "/x", 1, why, sizeof why) == 1);
    MP_CHECK_CONTAINS(F.log, "/api/v1/manual/add");   // aunque falle el fakelib, se registra
    MP_CHECK_CONTAINS(F.log, "/api/v1/scan");
    MP_CHECK_CONTAINS(why, "fakelib");
}

MP_TEST(smp_games_parse) {
    reset_log();
    mp_smp_cfg c = cfg();
    mp_smp_game *g = NULL;
    size_t n = 0;
    mp_err e = {{0}};
    MP_CHECK(mp_smp_games(&c, &g, &n, &e) == 0);
    MP_CHECK_EQ_U64(n, 2);
    if (n == 2) {
        MP_CHECK_STR(g[0].title_id, "PPSA00001");
        MP_CHECK_STR(g[0].path, "/data/homebrew/A");
        MP_CHECK_STR(g[1].source_type, "image");
        MP_CHECK(g[0].running == -1 && g[1].running == -1);
    }
    mp_smp_games_free(g, n);
}

MP_TEST(smp_sync_retries) {
    mpt_clear(TMP);
    mp_inst_env env;
    mp_inst_env_default(&env);
    env.internal = TMP "/data";
    mp_inst_record r;
    memset(&r, 0, sizeof r);
    snprintf(r.title_id, sizeof r.title_id, "TEST00001");
    snprintf(r.dir, sizeof r.dir, "/data/homebrew/TEST00001");
    r.smp_pending = 1;
    mp_err e = {{0}};
    MP_CHECK(mp_inst_record_save(&env, &r, &e) == 0);
    reset_log();
    mp_smp_cfg c = cfg();
    MP_CHECK(mp_inst_smp_sync(&env, &c, &e) == 0);
    mp_inst_record back;
    MP_CHECK(mp_inst_record_load(&env, "TEST00001", &back, &e) == 0);
    MP_CHECK(back.smp_pending == 0);
    MP_CHECK_CONTAINS(F.log, "/api/v1/manual/add");

    back.smp_pending = 1;
    MP_CHECK(mp_inst_record_save(&env, &back, &e) == 0);
    uint16_t old = port;
    stop_fake();
    port = old;
    c = cfg();
    MP_CHECK(mp_inst_smp_sync(&env, &c, &e) == 0);
    MP_CHECK(mp_inst_record_load(&env, "TEST00001", &back, &e) == 0);
    MP_CHECK(back.smp_pending == 1);
    MP_CHECK_CONTAINS(back.smp_why, "ShadowMountPlus");
    start_fake(old);
}

MP_TEST(smp_absent) {
    mp_err e = {{0}};
    mp_http_server *tmp = mp_http_start("127.0.0.1", 0, fake_handler, &F, &e);
    uint16_t free_port = tmp ? mp_http_port(tmp) : 1;
    mp_http_stop(tmp);
    mp_smp_cfg c;
    mp_smp_cfg_default(&c);
    c.port = free_port;
    c.timeout_ms = 400;
    char why[256];
    uint64_t t0 = mp_now_ns();
    MP_CHECK(mp_smp_alive(&c, why, sizeof why) == 0);
    MP_CHECK(mp_smp_register(&c, "TEST00001", "/x", 1, why, sizeof why) == -1);
    MP_CHECK(mp_now_ns() - t0 < 3000000000ull);
}

MP_TEST(smp_never_settings) { MP_CHECK(F.settings_seen == 0); }

int main(void) {
    MP_RUN(smp_register_order);
    MP_RUN(smp_scan_deferred);
    MP_RUN(smp_games_parse);
    MP_RUN(smp_sync_retries);
    MP_RUN(smp_absent);
    MP_RUN(smp_never_settings);
    if (srv) stop_fake();
    MP_TEST_MAIN_END();
}
