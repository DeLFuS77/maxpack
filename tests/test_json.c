// SPDX-License-Identifier: GPL-3.0-or-later
#include <stdlib.h>

#include "mp_json.h"
#include "mp_test.h"

static mp_json *parse(const char *s) {
    mp_err e = {{0}};
    return mp_json_parse(s, strlen(s), &e);
}

MP_TEST(json_parse_object) {
    mp_json *j = parse("{\"a\":\"x\\\"y\",\"n\":18446744073709551615,\"t\":true,\"l\":[1,2,{\"k\":null}],"
                       "\"neg\":-42, \"sp\" : \"\\/\\\\\\b\\f\\n\\r\\t\" }");
    MP_CHECK(j != NULL);
    if (!j) return;
    MP_CHECK_STR(mp_json_str(mp_json_get(j, "a")), "x\"y");
    uint64_t n = 0;
    MP_CHECK(mp_json_u64(mp_json_get(j, "n"), &n) == 0);
    MP_CHECK(n == UINT64_MAX);
    int t = 0;
    MP_CHECK(mp_json_bool(mp_json_get(j, "t"), &t) == 0 && t == 1);
    const mp_json *l = mp_json_get(j, "l");
    MP_CHECK_EQ_U64(mp_json_len(l), 3);
    MP_CHECK(mp_json_get(mp_json_at(l, 2), "k") != NULL);
    MP_CHECK(mp_json_at(l, 3) == NULL);
    int64_t neg = 0;
    MP_CHECK(mp_json_i64(mp_json_get(j, "neg"), &neg) == 0 && neg == -42);
    MP_CHECK(mp_json_u64(mp_json_get(j, "neg"), &n) != 0);
    MP_CHECK_STR(mp_json_str(mp_json_get(j, "sp")), "/\\\b\f\n\r\t");
    MP_CHECK(mp_json_get(j, "falta") == NULL);
    MP_CHECK(mp_json_str(mp_json_get(j, "n")) == NULL);
    MP_CHECK(mp_json_get(l, "a") == NULL);
    mp_json_free(j);
}

MP_TEST(json_unicode) {
    mp_json *j = parse("\"\\u00e9\\ud83c\\udfae\"");
    MP_CHECK(j != NULL);
    if (!j) return;
    MP_CHECK_STR(mp_json_str(j), "\xC3\xA9\xF0\x9F\x8E\xAE");
    mp_json_free(j);
}

MP_TEST(json_rejects) {
    const char *bad[] = {"{\"a\":1} x", "{\"a\":", "\"\\ud800\"", "\"a\tb\"", "{\"a\" 1}", "[1,]", "01", "tru", ""};
    for (size_t i = 0; i < sizeof bad / sizeof *bad; i++) {
        mp_err e = {{0}};
        mp_json *j = mp_json_parse(bad[i], strlen(bad[i]), &e);
        if (j) MP_FAIL_("aceptado: %s", bad[i]);
        else if (!e.msg[0]) MP_FAIL_("no message: %s", bad[i]);
        mp_json_free(j);
    }
    char deep[80];
    memset(deep, '[', 33);
    memset(deep + 33, ']', 33);
    deep[66] = 0;
    mp_err e = {{0}};
    MP_CHECK(mp_json_parse(deep, strlen(deep), &e) == NULL);
    deep[0] = ' ';   // 32 levels: valid
    deep[65] = ' ';
    mp_json *ok = mp_json_parse(deep, strlen(deep), &e);
    MP_CHECK(ok != NULL);
    mp_json_free(ok);
}

MP_TEST(jbuf_escapes) {
    mp_jbuf b = {0};
    mp_jbuf_str(&b, "<a href=\"x\">\n</a>&");
    MP_CHECK(!b.oom);
    MP_CHECK_STR(b.p, "\"\\u003ca href=\\\"x\\\"\\u003e\\n\\u003c/a\\u003e\\u0026\"");
    mp_json *j = mp_json_parse(b.p, b.n, NULL);
    MP_CHECK(j && !strcmp(mp_json_str(j), "<a href=\"x\">\n</a>&"));
    mp_json_free(j);
    mp_jbuf_free(&b);

    mp_jbuf c = {0};
    mp_jbuf_str(&c, "a\xff" "b\x01");
    MP_CHECK_STR(c.p, "\"a\\ufffdb\\u0001\"");
    mp_jbuf_free(&c);

    mp_jbuf d = {0};
    mp_jbuf_raw(&d, "{\"n\":");
    mp_jbuf_u64(&d, UINT64_MAX);
    mp_jbuf_raw(&d, ",\"m\":");
    mp_jbuf_i64(&d, INT64_MIN);
    mp_jbuf_raw(&d, ",\"u\":");
    mp_jbuf_str(&d, "\xC3\xA9");
    mp_jbuf_raw(&d, "}");
    MP_CHECK_STR(d.p, "{\"n\":18446744073709551615,\"m\":-9223372036854775808,\"u\":\"\xC3\xA9\"}");
    mp_json *k = mp_json_parse(d.p, d.n, NULL);
    int64_t m = 0;
    MP_CHECK(k && mp_json_i64(mp_json_get(k, "m"), &m) == 0 && m == INT64_MIN);
    mp_json_free(k);
    mp_jbuf_free(&d);
}

int main(void) {
    MP_RUN(json_parse_object);
    MP_RUN(json_unicode);
    MP_RUN(json_rejects);
    MP_RUN(jbuf_escapes);
    MP_TEST_MAIN_END();
}
