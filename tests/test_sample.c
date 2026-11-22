// SPDX-License-Identifier: GPL-3.0-or-later
#include <stdlib.h>

#include "mp_sample.h"
#include "mp_test.h"

#define MiB (1ull << 20)
#define GiB (1ull << 30)

MP_TEST(splitmix_vector) { MP_CHECK_EQ_U64(mp_splitmix64(0), 0xE220A8397B1DCDAFULL); }

MP_TEST(full_selects_all) {
    uint64_t sizes[2] = {3 * MiB + 5, 10};
    mp_sample s = {0};
    MP_CHECK(mp_sample_select(sizes, 2, 0.02, GiB, 1, &s, NULL) == 0);
    MP_CHECK_EQ_U64(s.n, 5);
    if (s.n == 5) {
        MP_CHECK(s.w[0].file == 0 && s.w[0].off == 0 && s.w[0].len == MiB && s.w[0].ordinal == 0);
        MP_CHECK(s.w[2].file == 0 && s.w[2].off == 2 * MiB && s.w[2].len == MiB);
        MP_CHECK(s.w[3].file == 0 && s.w[3].off == 3 * MiB && s.w[3].len == 5 && s.w[3].ordinal == 3);
        MP_CHECK(s.w[4].file == 1 && s.w[4].off == 0 && s.w[4].len == 10 && s.w[4].ordinal == 4);
    }
    MP_CHECK_EQ_U64(s.sample_bytes, 3 * MiB + 15);
    MP_CHECK_EQ_U64(s.total_bytes, 3 * MiB + 15);
    mp_sample_free(&s);
}

MP_TEST(small_game_sampled_fully) {
    uint64_t sizes[1] = {500 * MiB};
    mp_sample s = {0};
    MP_CHECK(mp_sample_select(sizes, 1, 0.02, GiB, 0, &s, NULL) == 0);
    MP_CHECK(s.rate == 1.0);
    MP_CHECK_EQ_U64(s.n, 500);
    MP_CHECK_EQ_U64(s.sample_bytes, 500 * MiB);
    mp_sample_free(&s);
}

MP_TEST(rate_and_determinism) {
    uint64_t sizes[100];
    for (int i = 0; i < 100; i++) sizes[i] = GiB;
    mp_sample a = {0}, b = {0};
    MP_CHECK(mp_sample_select(sizes, 100, 0.02, GiB, 0, &a, NULL) == 0);
    MP_CHECK(mp_sample_select(sizes, 100, 0.02, GiB, 0, &b, NULL) == 0);
    MP_CHECK(a.n >= 1843 && a.n <= 2253);
    MP_CHECK(a.rate > 0.0199 && a.rate < 0.0201);
    MP_CHECK_EQ_U64(a.n, b.n);
    MP_CHECK(a.n == b.n && memcmp(a.w, b.w, a.n * sizeof *a.w) == 0);
    mp_sample_free(&a);
    mp_sample_free(&b);
}

MP_TEST(empty) {
    mp_sample s = {0};
    MP_CHECK(mp_sample_select(NULL, 0, 0.02, GiB, 0, &s, NULL) == 0);
    MP_CHECK_EQ_U64(s.n, 0);
    MP_CHECK(s.rate == 1.0);
    mp_sample_free(&s);
}

int main(void) {
    MP_RUN(splitmix_vector);
    MP_RUN(full_selects_all);
    MP_RUN(small_game_sampled_fully);
    MP_RUN(rate_and_determinism);
    MP_RUN(empty);
    MP_TEST_MAIN_END();
}
