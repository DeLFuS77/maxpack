// SPDX-License-Identifier: GPL-3.0-or-later
// La página del instalador: reglas para el navegador de la PS5 y que la API la sirve.
#include <stdlib.h>

#include "mp_api.h"
#include "mp_page.h"
#include "mp_platform.h"
#include "mp_test.h"

#define TMP MP_TEST_TMP "/page"

MP_TEST(page_rules) {
    const char *p = (const char *)mp_page_html;
    MP_CHECK(mp_page_html_len > 1000 && strlen(p) == mp_page_html_len);
    const char *banned[] = {"innerHTML", "=>", "fetch(", "let ", "const ", "`", "outerHTML", "insertAdjacentHTML"};
    for (size_t i = 0; i < sizeof banned / sizeof *banned; i++)
        if (strstr(p, banned[i])) MP_FAIL_("la página usa \"%s\"", banned[i]);
    const char *needed[] = {"XMLHttpRequest", "textContent", "navigator.language", "/api/state", "Instalar",
                            "Install",        "Desinstalar", "Uninstall",          "/api/icon", "original_size"};
    for (size_t i = 0; i < sizeof needed / sizeof *needed; i++)
        if (!strstr(p, needed[i])) MP_FAIL_("a la página le falta \"%s\"", needed[i]);
}

MP_TEST(page_served) {
    mp_err e = {{0}};
    mp_make_dirs(TMP, &e);
    static const char *no_drives[] = {TMP "/usb0"};
    mp_inst_env env;
    mp_inst_env_default(&env);
    env.internal = TMP;
    env.drive_candidates = no_drives;
    env.ndrive_candidates = 1;
    mp_smp_cfg c;
    mp_smp_cfg_default(&c);
    c.port = 1;   // nadie escucha
    c.timeout_ms = 100;
    mp_api *a = mp_api_new(&env, &c, 1000, &e);
    MP_CHECK(a != NULL);
    if (!a) return;
    mp_http_req rq = {"GET", "/", "", "", 0};
    mp_http_resp rs = {200, NULL, NULL, 0};
    mp_api_handle(a, &rq, &rs);
    MP_CHECK(rs.status == 404);   // sin página puesta
    free(rs.body);
    mp_api_set_page(a, mp_page_html, mp_page_html_len);
    mp_http_resp rs2 = {200, NULL, NULL, 0};
    mp_api_handle(a, &rq, &rs2);
    MP_CHECK(rs2.status == 200);
    MP_CHECK_STR(rs2.content_type, "text/html; charset=utf-8");
    MP_CHECK_EQ_U64(rs2.body_len, mp_page_html_len);
    MP_CHECK(rs2.body && !memcmp(rs2.body, mp_page_html, mp_page_html_len));
    free(rs2.body);
    mp_api_free(a);
}

int main(void) {
    MP_RUN(page_rules);
    MP_RUN(page_served);
    MP_TEST_MAIN_END();
}
