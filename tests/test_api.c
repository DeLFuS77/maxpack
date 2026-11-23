// SPDX-License-Identifier: GPL-3.0-or-later
// API web del instalador y su cola de trabajos.
#include "mp_api.h"
#include "mp_fixture.h"
#include "mp_http.h"
#include "mp_inst.h"
#include "mp_json.h"
#include "mp_smp.h"

#define TMP MP_TEST_TMP "/api"
#define DATA TMP "/data"
#define USB TMP "/usb0"

static uint8_t rt_new[4096];
static const char *drives[] = {USB};

// ---- ShadowMountPlus falso --------------------------------------------------------------------

static mp_http_server *smp_srv;
static uint16_t smp_port;

static void smp_handler(void *ctx, const mp_http_req *rq, mp_http_resp *rs) {
    (void)ctx;
    const char *body = "{\"status\":0}";
    if (!strcmp(rq->path, "/api/v1/version")) body = "{\"status\":0,\"shadowmount_version\":\"1.7\"}";
    else if (!strcmp(rq->path, "/api/v1/scan")) body = "{\"status\":0,\"scan_queued\":true,\"scan_deferred\":false}";
    else if (!strcmp(rq->path, "/api/v1/games")) body = "{\"status\":0,\"count\":0,\"games\":[]}";
    rs->status = 200;
    rs->body = mp_strdup(body);
    rs->body_len = strlen(body);
}

static void smp_up(uint16_t p) {
    mp_err e = {{0}};
    smp_srv = mp_http_start("127.0.0.1", p, smp_handler, NULL, &e);
    if (!smp_srv) MP_FAIL_("ShadowMountPlus falso: %s", e.msg);
    else smp_port = mp_http_port(smp_srv);
}

static void smp_down(void) {
    mp_http_stop(smp_srv);
    smp_srv = NULL;
}

// ---- un io_check que se puede bloquear para tener un trabajo "en marcha" ----------------------

static volatile int hold = 0, holding = 0;

static int hold_io(void *ctx, uint64_t written, mp_err *e) {
    (void)ctx;
    (void)written;
    (void)e;
    while (hold) {
        holding = 1;
        mp_sleep_ms(5);
    }
    holding = 0;
    return 0;
}

// ---- API ----------------------------------------------------------------------------------------

static mp_api *api;
static mp_http_server *web;
static uint16_t web_port;

static int call(const char *method, const char *path, const char *body, mp_json **out) {
    int st = 0;
    char *resp = NULL;
    size_t rl = 0;
    mp_err e = {{0}};
    if (out) *out = NULL;
    if (mp_http_request("127.0.0.1", web_port, method, path, body, body ? strlen(body) : 0, 10000, &st, &resp, &rl,
                        &e) != 0) {
        MP_FAIL_("%s %s: %s", method, path, e.msg);
        return -1;
    }
    if (out) *out = mp_json_parse(resp, rl, NULL);
    free(resp);
    return st;
}

static char *state_text(void) {
    int st = 0;
    char *resp = NULL;
    size_t rl = 0;
    mp_http_request("127.0.0.1", web_port, "GET", "/api/state", NULL, 0, 10000, &st, &resp, &rl, NULL);
    return resp;
}

static const char *jcode(const mp_json *j) { return mp_json_str(mp_json_get(j, "code")); }

static int jok(const mp_json *j) {
    int b = 0;
    return mp_json_bool(mp_json_get(j, "ok"), &b) == 0 && b;
}

static int wait_job_running(void) {
    for (int i = 0; i < 400; i++) {
        if (holding) return 1;
        mp_sleep_ms(10);
    }
    return 0;
}

static int ready = 0;

static void setup(void) {
    if (ready) return;
    ready = 1;
    mpt_clear(TMP);
    for (size_t i = 0; i < sizeof rt_new; i++) rt_new[i] = (uint8_t)(i * 13 + 1);
    memcpy(rt_new + 64, "AMPRZPK1", 8);
    mp_err e = {{0}};
    mp_make_dirs(DATA, &e);
    mp_make_dirs(USB, &e);
    mpt_put(TMP "/R.sprx", rt_new, sizeof rt_new);
    mpt_pack(MPT_BASIC, USB "/basic.p5z", 1, TMP "/R.sprx");
    mpt_copy_file(USB "/basic.p5z", TMP "/fuera/basic.p5z");
    mpt_put_text(USB "/nota.txt", "hola");
    smp_up(0);
    mp_inst_env env;
    mp_inst_env_default(&env);
    env.internal = DATA;
    env.drive_candidates = drives;
    env.ndrive_candidates = 1;
    env.io_check = hold_io;
    mp_smp_cfg c;
    mp_smp_cfg_default(&c);
    c.port = smp_port;
    c.timeout_ms = 500;
    api = mp_api_new(&env, &c, 50, &e);
    if (!api) {
        MP_FAIL_("mp_api_new: %s", e.msg);
        return;
    }
    web = mp_http_start("127.0.0.1", 0, mp_api_handle, api, &e);
    if (web) web_port = mp_http_port(web);
}

static char install_body[2048];

static const char *install_json(const char *p5z, int with_replace) {
    if (with_replace)
        snprintf(install_body, sizeof install_body,
                 "{\"p5z\":\"%s\",\"dest\":\"%s\",\"replace\":[\"%s/homebrew/TEST00001\"],\"delete_p5z\":false}", p5z,
                 DATA, DATA);
    else
        snprintf(install_body, sizeof install_body, "{\"p5z\":\"%s\",\"dest\":\"%s\"}", p5z, DATA);
    return install_body;
}

MP_TEST(api_state_and_scan) {
    setup();
    MP_CHECK(call("POST", "/api/scan", "{}", NULL) == 200);
    mp_json *s = NULL;
    MP_CHECK(call("GET", "/api/state", NULL, &s) == 200);
    const mp_json *found = mp_json_get(s, "found");
    MP_CHECK_EQ_U64(mp_json_len(found), 1);
    MP_CHECK_STR(mp_json_str(mp_json_get(mp_json_at(found, 0), "path")), USB "/basic.p5z");
    MP_CHECK_STR(mp_json_str(mp_json_get(mp_json_at(found, 0), "title_id")), "TEST00001");
    uint64_t isz = 0;
    MP_CHECK(mp_json_u64(mp_json_get(mp_json_at(found, 0), "install_size"), &isz) == 0 && isz > 0);
    const mp_json *d = mp_json_get(s, "drives");
    MP_CHECK_EQ_U64(mp_json_len(d), 2);
    int internal = 0;
    MP_CHECK(mp_json_bool(mp_json_get(mp_json_at(d, 0), "internal"), &internal) == 0 && internal == 1);
    MP_CHECK_STR(mp_json_str(mp_json_get(mp_json_at(d, 1), "root")), USB);
    mp_json_free(s);
    MP_CHECK(call("GET", "/no/existe", NULL, NULL) == 404);
    MP_CHECK(call("POST", "/api/install", "{malo", NULL) == 400);
}

MP_TEST(api_install_flow) {
    setup();
    mp_json *r = NULL;
    MP_CHECK(call("POST", "/api/install", install_json(USB "/basic.p5z", 0), &r) == 200);
    MP_CHECK(jok(r));
    mp_json_free(r);
    MP_CHECK(mp_api_wait_idle(api, 20000) == 0);
    mp_json *s = NULL;
    call("GET", "/api/state", NULL, &s);
    const mp_json *inst = mp_json_get(s, "installed");
    MP_CHECK_EQ_U64(mp_json_len(inst), 1);
    MP_CHECK_STR(mp_json_str(mp_json_get(mp_json_at(inst, 0), "title_id")), "TEST00001");
    MP_CHECK_STR(mp_json_str(mp_json_get(mp_json_get(s, "last"), "code")), "done");
    mp_json_free(s);
}

MP_TEST(api_needs_confirm) {
    setup();
    mp_json *r = NULL;
    MP_CHECK(call("POST", "/api/install", install_json(USB "/basic.p5z", 0), &r) == 200);
    MP_CHECK(!jok(r));
    MP_CHECK_STR(jcode(r), "needs_confirm");
    const mp_json *ex = mp_json_get(r, "existing");
    MP_CHECK_EQ_U64(mp_json_len(ex), 1);
    MP_CHECK_STR(mp_json_str(mp_json_get(mp_json_at(ex, 0), "path")), DATA "/homebrew/TEST00001");
    mp_json_free(r);
    MP_CHECK(call("POST", "/api/install", install_json(USB "/basic.p5z", 1), &r) == 200);
    MP_CHECK(jok(r));
    mp_json_free(r);
    MP_CHECK(mp_api_wait_idle(api, 20000) == 0);
    mp_json *s = NULL;
    call("GET", "/api/state", NULL, &s);
    MP_CHECK_STR(mp_json_str(mp_json_get(mp_json_get(s, "last"), "code")), "done");
    mp_json_free(s);
}

MP_TEST(api_bad_paths) {
    setup();
    const char *bad[] = {TMP "/fuera/basic.p5z", USB "/../fuera/basic.p5z", USB "/nota.txt"};
    for (int i = 0; i < 3; i++) {
        char body[1200], path[1400];
        mp_json *r = NULL;
        snprintf(body, sizeof body, "{\"path\":\"%s\"}", bad[i]);
        MP_CHECK(call("POST", "/api/probe", body, &r) == 200);
        MP_CHECK_STR(jcode(r), "bad_path");
        mp_json_free(r);
        MP_CHECK(call("POST", "/api/install", install_json(bad[i], 0), &r) == 200);
        MP_CHECK_STR(jcode(r), "bad_path");
        mp_json_free(r);
        char enc[1300];
        size_t k = 0;
        for (const char *p = bad[i]; *p && k + 4 < sizeof enc; p++) {
            if (*p == '/' || *p == ' ' || *p == ':') k += (size_t)snprintf(enc + k, sizeof enc - k, "%%%02X", (unsigned char)*p);
            else enc[k++] = *p;
        }
        enc[k] = 0;
        snprintf(path, sizeof path, "/api/icon?p5z=%s", enc);
        MP_CHECK(call("GET", path, NULL, NULL) == 404);
    }
    mp_json *r = NULL;
    MP_CHECK(call("POST", "/api/uninstall", "{\"title_id\":\"PPSA99999\",\"confirm\":true}", &r) == 200);
    MP_CHECK_STR(jcode(r), "not_found");
    mp_json_free(r);
    MP_CHECK(call("POST", "/api/probe", "{\"path\":\"" USB "/basic.p5z\"}", &r) == 200);
    MP_CHECK(jok(r));
    mp_json_free(r);
}

MP_TEST(api_busy_title) {
    setup();
    hold = 1;
    mp_json *r = NULL;
    MP_CHECK(call("POST", "/api/install", install_json(USB "/basic.p5z", 1), &r) == 200 && jok(r));
    mp_json_free(r);
    MP_CHECK(wait_job_running());
    MP_CHECK(call("POST", "/api/install", install_json(USB "/basic.p5z", 1), &r) == 200);
    MP_CHECK_STR(jcode(r), "busy");
    mp_json_free(r);
    MP_CHECK(call("POST", "/api/uninstall", "{\"title_id\":\"TEST00001\",\"confirm\":true}", &r) == 200);
    MP_CHECK_STR(jcode(r), "busy");
    mp_json_free(r);
    mp_json *s = NULL;
    call("GET", "/api/state", NULL, &s);
    MP_CHECK(mp_json_get(s, "job") && mp_json_str(mp_json_get(mp_json_get(s, "job"), "title_id")));
    mp_json_free(s);
    hold = 0;
    MP_CHECK(mp_api_wait_idle(api, 20000) == 0);
}

MP_TEST(api_pause) {
    setup();
    hold = 1;
    mp_json *r = NULL;
    MP_CHECK(call("POST", "/api/install", install_json(USB "/basic.p5z", 1), &r) == 200 && jok(r));
    mp_json_free(r);
    MP_CHECK(wait_job_running());
    MP_CHECK(call("POST", "/api/pause", "{}", NULL) == 200);
    hold = 0;
    MP_CHECK(mp_api_wait_idle(api, 20000) == 0);
    mp_json *s = NULL;
    call("GET", "/api/state", NULL, &s);
    MP_CHECK_STR(mp_json_str(mp_json_get(mp_json_get(s, "last"), "code")), "stopped");
    mp_json_free(s);
    MP_CHECK(call("POST", "/api/install", install_json(USB "/basic.p5z", 1), &r) == 200 && jok(r));
    mp_json_free(r);
    MP_CHECK(mp_api_wait_idle(api, 20000) == 0);
    call("GET", "/api/state", NULL, &s);
    MP_CHECK_STR(mp_json_str(mp_json_get(mp_json_get(s, "last"), "code")), "done");
    mp_json_free(s);
}

MP_TEST(api_escapes) {
    setup();
    mpt_put_text(TMP "/xss/sce_sys/param.json",
                 "{\"titleId\":\"TEST00007\",\"contentVersion\":\"01.000.000\",\"titleName\":\"<script>x</script>\"}");
    mpt_put_text(TMP "/xss/datos.bin", "datos datos datos datos datos datos datos");
    mpt_pack(TMP "/xss", USB "/xss.p5z", 1, TMP "/R.sprx");
    call("POST", "/api/scan", "{}", NULL);
    char *s = state_text();
    MP_CHECK(s && !strstr(s, "<script>"));
    MP_CHECK_CONTAINS(s, "\\u003cscript");
    free(s);
}

MP_TEST(api_smp_retry) {
    setup();
    uint16_t p = smp_port;
    smp_down();
    mp_json *r = NULL;
    MP_CHECK(call("POST", "/api/install", install_json(USB "/basic.p5z", 1), &r) == 200 && jok(r));
    mp_json_free(r);
    MP_CHECK(mp_api_wait_idle(api, 20000) == 0);
    mp_json *s = NULL;
    call("GET", "/api/state", NULL, &s);
    int pending = 0;
    mp_json_bool(mp_json_get(mp_json_at(mp_json_get(s, "installed"), 0), "smp_pending"), &pending);
    MP_CHECK(pending == 1);
    mp_json_free(s);
    smp_up(p);
    int cleared = 0;
    for (int i = 0; i < 200 && !cleared; i++) {
        mp_sleep_ms(10);
        call("GET", "/api/state", NULL, &s);
        pending = 1;
        mp_json_bool(mp_json_get(mp_json_at(mp_json_get(s, "installed"), 0), "smp_pending"), &pending);
        cleared = pending == 0;
        mp_json_free(s);
    }
    MP_CHECK(cleared);
}

MP_TEST(api_refuses_running) {
    // ShadowMountPlus 1.7 no dice qué juego está en marcha (running = -1): se sigue adelante.
    setup();
    mp_json *r = NULL;
    MP_CHECK(call("POST", "/api/install", install_json(USB "/basic.p5z", 1), &r) == 200 && jok(r));
    mp_json_free(r);
    MP_CHECK(mp_api_wait_idle(api, 20000) == 0);
    MP_CHECK(call("POST", "/api/uninstall", "{\"title_id\":\"TEST00001\"}", &r) == 200);
    MP_CHECK_STR(jcode(r), "needs_confirm");
    mp_json_free(r);
    MP_CHECK(call("POST", "/api/uninstall", "{\"title_id\":\"TEST00001\",\"confirm\":true}", &r) == 200 && jok(r));
    mp_json_free(r);
    MP_CHECK(mp_api_wait_idle(api, 20000) == 0);
    MP_CHECK(mp_path_kind(DATA "/homebrew/TEST00001") == 0);
}

int main(void) {
    MP_RUN(api_state_and_scan);
    MP_RUN(api_install_flow);
    MP_RUN(api_needs_confirm);
    MP_RUN(api_bad_paths);
    MP_RUN(api_busy_title);
    MP_RUN(api_pause);
    MP_RUN(api_escapes);
    MP_RUN(api_smp_retry);
    MP_RUN(api_refuses_running);
    mp_http_stop(web);
    mp_api_free(api);
    if (smp_srv) smp_down();
    MP_TEST_MAIN_END();
}
