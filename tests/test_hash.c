// SPDX-License-Identifier: GPL-3.0-or-later
#include <stdlib.h>

#include "mp_hash.h"
#include "mp_test.h"

static void hex_of(const void *p, size_t n, char out[65]) {
    mp_sha256 s;
    uint8_t d[32];
    mp_sha256_init(&s);
    mp_sha256_update(&s, p, n);
    mp_sha256_final(&s, d);
    mp_sha256_hex(d, out);
}

MP_TEST(sha256_vectors) {
    char h[65];
    hex_of("", 0, h);
    MP_CHECK_STR(h, "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855");
    hex_of("abc", 3, h);
    MP_CHECK_STR(h, "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad");
    // 1.000.000 'a' en trozos de 997 bytes (trozos que no casan con los bloques de 64).
    char *a = malloc(1000000);
    memset(a, 'a', 1000000);
    mp_sha256 s;
    uint8_t d[32];
    mp_sha256_init(&s);
    for (size_t off = 0; off < 1000000; off += 997)
        mp_sha256_update(&s, a + off, 1000000 - off < 997 ? 1000000 - off : 997);
    mp_sha256_final(&s, d);
    mp_sha256_hex(d, h);
    MP_CHECK_STR(h, "cdc76e5c9914fb9281a1c7e284d73e67f1809a48a497200e046d39ccc7112cd0");
    free(a);
}

MP_TEST(fnv_vectors) {
    MP_CHECK_EQ_U64(mp_fnv1a64("", 0, MP_FNV_BASIS_STD), 0xcbf29ce484222325ULL);
    MP_CHECK_EQ_U64(mp_fnv1a64("a", 1, MP_FNV_BASIS_STD), 0xaf63dc4c8601ec8cULL);
    MP_CHECK_EQ_U64(mp_zpk_path_hash("data/level1.bin"), 0xa16193e8350061c8ULL);
    MP_CHECK_EQ_U64(mp_idx3_path_hash("/app0/eboot.bin"), 0xa8641f238cf4f1aeULL);
    MP_CHECK_EQ_U64(mp_idx3_path_hash("/APP0/EBOOT.BIN"), 0xa8641f238cf4f1aeULL);
}

MP_TEST(crc32_vector) { MP_CHECK_EQ_U64(mp_crc32("123456789", 9, 0), 0xCBF43926u); }

int main(void) {
    MP_RUN(sha256_vectors);
    MP_RUN(fnv_vectors);
    MP_RUN(crc32_vector);
    MP_TEST_MAIN_END();
}
