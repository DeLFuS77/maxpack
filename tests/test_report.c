// SPDX-License-Identifier: GPL-3.0-or-later
#include <stdlib.h>

#include "mp_report_out.h"
#include "mp_test.h"

MP_TEST(json_escapes) {
    mp_report r;
    memset(&r, 0, sizeof r);
    char name[] = "a\"b\\c\x01";
    r.source_name = name;
    char *j = mp_report_json(&r);
    MP_CHECK_CONTAINS(j, "\"name\": \"a\\\"b\\\\c\\u0001\"");
    free(j);
}

MP_TEST(json_values_finite) {
    mp_report r;
    memset(&r, 0, sizeof r);
    char name[] = "vacio";
    r.source_name = name;
    r.sample_rate = 1.0;
    char *j = mp_report_json(&r);
    MP_CHECK(j != NULL);
    MP_CHECK(j && !strstr(j, "nan") && !strstr(j, "inf"));
    MP_CHECK_CONTAINS(j, "\"dedup\": null");
    MP_CHECK_CONTAINS(j, "\"transport\": null");
    MP_CHECK_CONTAINS(j, "\"v1-l5\"");
    MP_CHECK_CONTAINS(j, "\"zlib-64k\"");
    free(j);
}

MP_TEST(text_mentions_forge) {
    mp_report r;
    memset(&r, 0, sizeof r);
    char name[] = "juego";
    r.source_name = name;
    r.total_bytes = 1000;
    FILE *f = tmpfile();
    MP_CHECK(f != NULL);
    if (!f) return;
    MP_CHECK(mp_report_write_text(&r, f) == 0);
    rewind(f);
    char buf[8192] = {0};
    size_t n = fread(buf, 1, sizeof buf - 1, f);
    fclose(f);
    buf[n] = 0;
    MP_CHECK_CONTAINS(buf, "(Forge)");
    MP_CHECK_CONTAINS(buf, ".ffpfsc");
    MP_CHECK_CONTAINS(buf, "juego");
}

int main(void) {
    MP_RUN(json_escapes);
    MP_RUN(json_values_finite);
    MP_RUN(text_mentions_forge);
    MP_TEST_MAIN_END();
}
