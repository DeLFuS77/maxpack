// SPDX-License-Identifier: GPL-3.0-or-later
#include <stdlib.h>
#include <zlib.h>
#include <zstd.h>

#include "mp_test.h"
#include "mp_version.h"

static unsigned char *make_text(size_t n) {
    unsigned char *p = malloc(n);
    for (size_t i = 0; i < n; i++) p[i] = (unsigned char)("maxpack-prueba "[i % 15] + (i / 4096) % 3);
    return p;
}

MP_TEST(zstd_roundtrip) {
    MP_CHECK_EQ_U64(ZSTD_versionNumber(), 10506);
    size_t n = 262144;
    unsigned char *src = make_text(n);
    size_t cap = ZSTD_compressBound(n);
    unsigned char *dst = malloc(cap), *back = malloc(n);
    size_t c = ZSTD_compress(dst, cap, src, n, 19);
    MP_CHECK(!ZSTD_isError(c));
    MP_CHECK(c < n);
    size_t d = ZSTD_decompress(back, n, dst, c);
    MP_CHECK_EQ_U64(d, n);
    MP_CHECK(memcmp(src, back, n) == 0);
    free(src); free(dst); free(back);
}

MP_TEST(zlib_roundtrip) {
    MP_CHECK_STR(zlibVersion(), "1.3.1");
    uLong n = 100000;
    unsigned char *src = make_text(n);
    uLongf cap = compressBound(n), back_n = n;
    unsigned char *dst = malloc(cap), *back = malloc(n);
    MP_CHECK(compress2(dst, &cap, src, n, 9) == Z_OK);
    MP_CHECK(cap < n);
    MP_CHECK(uncompress(back, &back_n, dst, cap) == Z_OK);
    MP_CHECK_EQ_U64(back_n, n);
    MP_CHECK(memcmp(src, back, n) == 0);
    free(src); free(dst); free(back);
}

MP_TEST(version_string) { MP_CHECK_STR(mp_version_string(), "1.0.6"); }

int main(void) {
    MP_RUN(zstd_roundtrip);
    MP_RUN(zlib_roundtrip);
    MP_RUN(version_string);
    MP_TEST_MAIN_END();
}
