// SPDX-License-Identifier: GPL-3.0-or-later
#include <stdlib.h>
#include <zdict.h>

#include "mp_rules.h"
#include "mp_test.h"
#include "mp_v2exp.h"

typedef struct {
    mp_source *s;
    uint32_t *packable, *small;
    size_t npack, nsmall;
    uint64_t small_raw;
} fixture;

static int open_fixture(fixture *f) {
    mp_err e = {{0}};
    memset(f, 0, sizeof *f);
    if (mp_source_open(MP_FIXTURES_DIR "/basic.exfat", &f->s, &e) != 0) {
        MP_FAIL_("no abre basic.exfat: %s", e.msg);
        return 0;
    }
    mp_rules *r = mp_rules_new();
    size_t n = mp_source_count(f->s);
    f->packable = malloc(n * sizeof *f->packable);
    f->small = malloc(n * sizeof *f->small);
    for (size_t i = 0; i < n; i++) {
        const mp_entry *en = mp_source_entry(f->s, i);
        if (mp_rules_classify(r, en->path, en->size) != MP_PACKABLE) continue;
        f->packable[f->npack++] = (uint32_t)i;
        if (en->size <= MP_SMALL_MAX) {
            f->small[f->nsmall++] = (uint32_t)i;
            f->small_raw += en->size;
        }
    }
    mp_rules_free(r);
    return 1;
}

static void close_fixture(fixture *f) {
    free(f->packable);
    free(f->small);
    mp_source_close(f->s);
}

MP_TEST(small_counts_and_grouping) {
    fixture f;
    if (!open_fixture(&f)) return;
    mp_small_result r = {0};
    mp_err e = {{0}};
    MP_CHECK(mp_small_run(f.s, f.small, f.nsmall, NULL, 2, NULL, &r, &e) == 0);
    MP_CHECK_EQ_U64(f.nsmall, 204);
    MP_CHECK_EQ_U64(r.files, f.nsmall);
    MP_CHECK_EQ_U64(r.raw, f.small_raw);
    MP_CHECK(r.grouped < r.individual);
    MP_CHECK(r.individual <= r.raw);
    MP_CHECK_EQ_U64(r.individual_dict, r.individual);   // sin diccionarios
    MP_CHECK(r.dicts == 0);
    close_fixture(&f);
}

MP_TEST(dict_helps_small) {
    fixture f;
    if (!open_fixture(&f)) return;
    mp_dict_pair dp = {{0}};
    mp_err e = {{0}};
    MP_CHECK(mp_dict_train_small(f.s, f.small, f.nsmall, &dp, &e) == 0);
    MP_CHECK(dp.dict[0] != NULL && dp.dict[1] != NULL);
    mp_small_result r = {0};
    MP_CHECK(mp_small_run(f.s, f.small, f.nsmall, &dp, 2, NULL, &r, &e) == 0);
    MP_CHECK(r.dicts == 2);
    MP_CHECK(r.individual_dict < r.individual);
    mp_dict_pair_free(&dp);
    close_fixture(&f);
}

MP_TEST(small_threads_independent) {
    fixture f;
    if (!open_fixture(&f)) return;
    mp_dict_pair dp = {{0}};
    mp_err e = {{0}};
    MP_CHECK(mp_dict_train_small(f.s, f.small, f.nsmall, &dp, &e) == 0);
    mp_small_result a = {0}, b = {0};
    MP_CHECK(mp_small_run(f.s, f.small, f.nsmall, &dp, 1, NULL, &a, &e) == 0);
    MP_CHECK(mp_small_run(f.s, f.small, f.nsmall, &dp, 3, NULL, &b, &e) == 0);
    MP_CHECK(memcmp(&a, &b, sizeof a) == 0);
    mp_dict_pair_free(&dp);
    close_fixture(&f);
}

MP_TEST(block_dict_never_errors) {
    fixture f;
    if (!open_fixture(&f)) return;
    uint64_t *sizes = malloc(f.npack * sizeof *sizes);
    for (size_t i = 0; i < f.npack; i++) sizes[i] = mp_source_entry(f.s, f.packable[i])->size;
    mp_sample smp = {0};
    MP_CHECK(mp_sample_select(sizes, f.npack, 0.02, 1ull << 30, 1, &smp, NULL) == 0);
    mp_dict_pair dp = {{0}};
    mp_err e = {{0}};
    MP_CHECK(mp_dict_train_blocks(f.s, f.packable, &smp, &dp, &e) == 0);
    for (int p = 0; p < 2; p++)
        if (dp.dict[p]) MP_CHECK(ZDICT_getDictID(dp.dict[p], dp.size[p]) != 0);
    mp_dict_pair_free(&dp);
    mp_sample_free(&smp);
    free(sizes);
    close_fixture(&f);
}

int main(void) {
    MP_RUN(small_counts_and_grouping);
    MP_RUN(dict_helps_small);
    MP_RUN(small_threads_independent);
    MP_RUN(block_dict_never_errors);
    MP_TEST_MAIN_END();
}
