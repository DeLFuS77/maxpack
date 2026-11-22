// SPDX-License-Identifier: GPL-3.0-or-later
#include "mp_extent.h"
#include "mp_test.h"

MP_TEST(extent_beyond_4g) {
    const mp_extent ex[2] = {
        {0, 0x100000000ULL, 0x80000000ULL},
        {0x80000000ULL, 0x1000000000ULL, 0x200000000ULL},
    };
    uint64_t img = 0, run = 0;
    MP_CHECK(mp_extent_find(ex, 2, 0x7FFFFFFFULL, &img, &run) == 0);
    MP_CHECK_EQ_U64(img, 0x17FFFFFFFULL);
    MP_CHECK_EQ_U64(run, 1);
    MP_CHECK(mp_extent_find(ex, 2, 0x80000000ULL, &img, &run) == 1);
    MP_CHECK_EQ_U64(img, 0x1000000000ULL);
    MP_CHECK_EQ_U64(run, 0x200000000ULL);
    MP_CHECK(mp_extent_find(ex, 2, 0x27FFFFFFFULL, &img, &run) == 1);
    MP_CHECK_EQ_U64(img, 0x1000000000ULL + 0x1FFFFFFFFULL);
    MP_CHECK_EQ_U64(run, 1);
    MP_CHECK(mp_extent_find(ex, 2, 0x280000000ULL, &img, &run) == -1);
    MP_CHECK(mp_extent_find(ex, 0, 0, &img, &run) == -1);
}

int main(void) {
    MP_RUN(extent_beyond_4g);
    MP_TEST_MAIN_END();
}
