// SPDX-License-Identifier: GPL-3.0-or-later
#include <stdlib.h>

#include "mp_hash.h"
#include "mp_parallel.h"
#include "mp_test.h"

static int mark(void *ctx, size_t item, unsigned tid) {
    (void)tid;
    ((int *)ctx)[item]++;
    return 0;
}

static int fail_at_50(void *ctx, size_t item, unsigned tid) {
    (void)ctx;
    (void)tid;
    return item == 50 ? 3 : 0;
}

MP_TEST(parallel_covers_all_items) {
    int *seen = calloc(10000, sizeof *seen);
    MP_CHECK(mp_parallel_run(7, 10000, mark, seen, NULL) == 0);
    int bad = 0;
    for (int i = 0; i < 10000; i++) bad += seen[i] != 1;
    MP_CHECK_EQ_U64(bad, 0);
    free(seen);
    MP_CHECK(mp_parallel_run(4, 0, mark, NULL, NULL) == 0);
}

MP_TEST(parallel_stops_on_error) { MP_CHECK(mp_parallel_run(4, 1000, fail_at_50, NULL, NULL) == 3); }

MP_TEST(xxh64_vector) {
    MP_CHECK_EQ_U64(mp_xxh64("", 0, 0), 0xEF46DB3751D8E999ULL);
    MP_CHECK(mp_xxh64("a", 1, 0) != mp_xxh64("b", 1, 0));
}

int main(void) {
    MP_RUN(parallel_covers_all_items);
    MP_RUN(parallel_stops_on_error);
    MP_RUN(xxh64_vector);
    MP_TEST_MAIN_END();
}
