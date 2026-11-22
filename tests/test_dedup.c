// SPDX-License-Identifier: GPL-3.0-or-later
#include <stdlib.h>

#include "mp_dedup.h"
#include "mp_rules.h"
#include "mp_test.h"

static mp_source *open_packable(uint32_t **files, size_t *n) {
    mp_err e = {{0}};
    mp_source *s = NULL;
    if (mp_source_open(MP_FIXTURES_DIR "/basic.exfat", &s, &e) != 0) {
        MP_FAIL_("no abre basic.exfat: %s", e.msg);
        return NULL;
    }
    mp_rules *r = mp_rules_new();
    *files = malloc(mp_source_count(s) * sizeof **files);
    *n = 0;
    for (size_t i = 0; i < mp_source_count(s); i++)
        if (mp_rules_classify(r, mp_source_entry(s, i)->path, mp_source_entry(s, i)->size) ==
            MP_PACKABLE)
            (*files)[(*n)++] = (uint32_t)i;
    mp_rules_free(r);
    return s;
}

MP_TEST(dedup_basic) {
    uint32_t *files = NULL;
    size_t n = 0;
    mp_source *s = open_packable(&files, &n);
    if (!s) return;
    mp_dedup_result d = {0};
    mp_err e = {{0}};
    MP_CHECK(mp_dedup_run(s, files, n, 1, NULL, &d, &e) == 0);
    MP_CHECK_EQ_U64(d.dup_blocks, 4);
    MP_CHECK_EQ_U64(d.dup_block_bytes, 4ull * 262144);
    MP_CHECK_EQ_U64(d.dup_files, 1);
    MP_CHECK_EQ_U64(d.dup_file_bytes, 262144);
    MP_CHECK(d.blocks > 200);
    MP_CHECK(d.bytes_read > 0);
    free(files);
    mp_source_close(s);
}

MP_TEST(dedup_threads_independent) {
    uint32_t *files = NULL;
    size_t n = 0;
    mp_source *s = open_packable(&files, &n);
    if (!s) return;
    mp_dedup_result a = {0}, b = {0};
    mp_err e = {{0}};
    MP_CHECK(mp_dedup_run(s, files, n, 1, NULL, &a, &e) == 0);
    MP_CHECK(mp_dedup_run(s, files, n, 4, NULL, &b, &e) == 0);
    MP_CHECK_EQ_U64(a.blocks, b.blocks);
    MP_CHECK_EQ_U64(a.dup_blocks, b.dup_blocks);
    MP_CHECK_EQ_U64(a.dup_block_bytes, b.dup_block_bytes);
    MP_CHECK_EQ_U64(a.dup_files, b.dup_files);
    MP_CHECK_EQ_U64(a.dup_file_bytes, b.dup_file_bytes);
    MP_CHECK_EQ_U64(a.bytes_read, b.bytes_read);
    free(files);
    mp_source_close(s);
}

int main(void) {
    MP_RUN(dedup_basic);
    MP_RUN(dedup_threads_independent);
    MP_TEST_MAIN_END();
}
