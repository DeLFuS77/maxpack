// SPDX-License-Identifier: GPL-3.0-or-later
// mp_api_call: la API del instalador llamada sin servidor HTTP, y POST /api/quit.
#include "mp_api.h"
#include "mp_api_call.h"
#include "mp_fixture.h"
#include "mp_http.h"
#include "mp_inst.h"
#include "mp_json.h"
#include "mp_smp.h"

#define TMP MP_TEST_TMP "/apicall"
#define DATA TMP "/data"
#define USB TMP "/usb0"

static const char *drives[] = {USB};

// ---- ShadowMountPlus falso --------------------------------------------------------------------

static mp_http_server *smp_srv;
static mp_mutex *smp_mu;
static char smp_log[65536];

static void smp_handler(void *ctx, const mp_http_req *rq, mp_http_resp *rs) {
    (void)ctx;
    mp_mutex_lock(smp_mu);
    size_t k = strlen(smp_log);
    if (k + rq->body_len + 300 < sizeof smp_log)
        snprintf(smp_log + k, sizeof smp_log - k, "%s %.*s\n", rq->path, (int)rq->body_len, rq->body);
    mp_mutex_unlock(smp_mu);
    const char *body = "{\"status\":0}";
    if (!strcmp(rq->path, "/api/v1/version")) body = "{\"status\":0,\"shadowmount_version\":\"1.7\"}";
    else if (!strcmp(rq->path, "/api/v1/scan")) body = "{\"status\":0,\"scan_queued\":true,\"scan_deferred\":false}";
    else if (!strcmp(rq->path, "/api/v1/games")) body = "{\"status\":0,\"count\":0,\"games\":[]}";
    rs->status = 200;
    rs->body = mp_strdup(body);
    rs->body_len = strlen(body);
}

// ---- API ----------------------------------------------------------------------------------------

static mp_api *api;
static int ready = 0;
static int quit_calls = 0;

static void on_quit(void *ctx) {
    (void)ctx;
    quit_calls++;
}

static void setup(void) {
    if (ready) return;
    ready = 1;
    mpt_clear(TMP);
    mp_err e = {{0}};
    mp_make_dirs(DATA, &e);
    mp_make_dirs(USB, &e);
    static uint8_t rt_new[4096];
    for (size_t i = 0; i < sizeof rt_new; i++) rt_new[i] = (uint8_t)(i * 13 + 1);
    memcpy(rt_new + 64, "AMPRZPK1", 8);
    mpt_put(TMP "/R.sprx", rt_new, sizeof rt_new);
    mpt_pack(MPT_BASIC, USB "/basic.p5z", 1, TMP "/R.sprx");
    smp_mu = mp_mutex_new();
    smp_srv = mp_http_start("127.0.0.1", 0, smp_handler, NULL, &e);
    if (!smp_srv) {
        MP_FAIL_("ShadowMountPlus falso: %s", e.msg);
        return;
    }
    mp_inst_env env;
    mp_inst_env_default(&env);
    env.internal = DATA;
    env.drive_candidates = drives;
    env.ndrive_candidates = 1;
    mp_smp_cfg c;
    mp_smp_cfg_default(&c);
    c.port = mp_http_port(smp_srv);
    c.timeout_ms = 500;
    api = mp_api_new(&env, &c, 50, &e);
    if (!api) MP_FAIL_("mp_api_new: %s", e.msg);
}

static int do_call(const char *method, const char *path, const char *host, const char *ctype, const char *body,
                   int loopback, mp_api_out *out) {
    return mp_api_call(api, method, path, host, ctype, (const uint8_t *)body, body ? strlen(body) : 0, loopback, out);
}

MP_TEST(bridge_state) {
    setup();
    mp_api_out o;
    MP_CHECK(do_call("GET", "/api/state", "127.0.0.1:8095", NULL, NULL, 1, &o) == 0);
    MP_CHECK_EQ_U64(o.status, 200);
    MP_CHECK(strncmp(o.ctype, "application/json", 16) == 0);
    mp_json *j = mp_json_parse((const char *)o.body, o.len, NULL);
    MP_CHECK(j != NULL);
    MP_CHECK(mp_json_get(j, "drives") != NULL);
    mp_json_free(j);
    mp_api_out_free(&o);
}

MP_TEST(bridge_rejects_foreign_host) {
    setup();
    mp_api_out o;
    MP_CHECK(do_call("GET", "/api/state", "evil.example", NULL, NULL, 1, &o) == 0);
    MP_CHECK_EQ_U64(o.status, 403);
    mp_api_out_free(&o);
    MP_CHECK(do_call("POST", "/api/scan", "127.0.0.1", "text/plain", "{}", 1, &o) == 0);
    MP_CHECK_EQ_U64(o.status, 403);
    mp_api_out_free(&o);
}

MP_TEST(bridge_install_flow_uses_smp) {
    setup();
    char body[1200];
    snprintf(body, sizeof body, "{\"p5z\":\"%s\",\"dest\":\"%s\"}", USB "/basic.p5z", DATA);
    mp_api_out o;
    MP_CHECK(do_call("POST", "/api/install", "127.0.0.1:8095", "application/json", body, 1, &o) == 0);
    MP_CHECK_EQ_U64(o.status, 200);
    mp_api_out_free(&o);
    MP_CHECK(mp_api_wait_idle(api, 20000) == 0);
    MP_CHECK(do_call("GET", "/api/state", "127.0.0.1:8095", NULL, NULL, 1, &o) == 0);
    mp_json *s = mp_json_parse((const char *)o.body, o.len, NULL);
    MP_CHECK(s != NULL);
    MP_CHECK_STR(mp_json_str(mp_json_get(mp_json_get(s, "last"), "code")), "done");
    mp_json_free(s);
    mp_api_out_free(&o);
    mp_mutex_lock(smp_mu);
    MP_CHECK(strstr(smp_log, "/api/v1/manual/add {\"path\":\"" DATA "/homebrew/TEST00001\"}") != NULL);
    mp_mutex_unlock(smp_mu);
}

MP_TEST(bridge_quit_loopback_only) {
    setup();
    mp_api_out o;
    // Sin callback puesto, nadie atiende la petición.
    MP_CHECK(do_call("POST", "/api/quit", "127.0.0.1", "application/json", "{}", 1, &o) == 0);
    MP_CHECK_EQ_U64(o.status, 403);
    mp_api_out_free(&o);
    mp_api_set_quit(api, on_quit, NULL);
    MP_CHECK(do_call("POST", "/api/quit", "127.0.0.1", "application/json", "{}", 0, &o) == 0);
    MP_CHECK_EQ_U64(o.status, 403);
    mp_api_out_free(&o);
    MP_CHECK_EQ_U64(quit_calls, 0);
    MP_CHECK(do_call("POST", "/api/quit", "127.0.0.1", "application/json", "{}", 1, &o) == 0);
    MP_CHECK_EQ_U64(o.status, 200);
    mp_api_out_free(&o);
    MP_CHECK_EQ_U64(quit_calls, 1);
}

int main(void) {
    MP_RUN(bridge_state);
    MP_RUN(bridge_rejects_foreign_host);
    MP_RUN(bridge_install_flow_uses_smp);
    MP_RUN(bridge_quit_loopback_only);
    mp_api_free(api);
    if (smp_srv) mp_http_stop(smp_srv);
    MP_TEST_MAIN_END();
}
