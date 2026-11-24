// SPDX-License-Identifier: GPL-3.0-or-later
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

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
    // 1,000,000 'a' in chunks of 997 bytes (chunks that do not line up with the 64-byte blocks).
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

// The CPU's SHA instructions (where there are any) give the same digests as the portable code, for
// every length and however the data is split between updates.
static void digest_split(const uint8_t *p, size_t n, size_t piece, uint8_t d[32]) {
    mp_sha256 s;
    mp_sha256_init(&s);
    for (size_t off = 0; off < n; off += piece) mp_sha256_update(&s, p + off, n - off < piece ? n - off : piece);
    mp_sha256_final(&s, d);
}

MP_TEST(sha256_accel_same_as_portable) {
    int accel = mp_sha256_accel();
    fprintf(stderr, "  SHA-256: %s\n", accel ? "the CPU's SHA instructions" : "portable code");
    size_t n = 300000;
    uint8_t *b = malloc(n);
    uint32_t x = 2463534242u;
    for (size_t i = 0; i < n; i++) {
        x ^= x << 13;
        x ^= x >> 17;
        x ^= x << 5;
        b[i] = (uint8_t)(x >> 24);
    }
    size_t lens[] = {0, 1, 3, 55, 56, 63, 64, 65, 119, 120, 127, 128, 129, 1000, 4095, 65536, 262144, 299999};
    size_t pieces[] = {1, 7, 64, 100, 4096, 300000};
    int bad = 0;
    for (size_t i = 0; i < sizeof lens / sizeof *lens; i++)
        for (size_t j = 0; j < sizeof pieces / sizeof *pieces; j++) {
            if (lens[i] > 70000 && pieces[j] < 64) continue;   // long enough with the others
            uint8_t a[32], c[32];
            mp_sha256_use_accel(0);
            MP_CHECK(mp_sha256_accel() == 0);
            digest_split(b, lens[i], pieces[j], a);
            mp_sha256_use_accel(1);
            digest_split(b, lens[i], pieces[j], c);
            if (memcmp(a, c, 32)) {
                MP_FAIL_("length %zu in pieces of %zu", lens[i], pieces[j]);
                bad++;
            }
        }
    MP_CHECK_EQ_U64(bad, 0);
    MP_CHECK(mp_sha256_accel() == accel);   // back as it was
    free(b);
}

MP_TEST(sha256_vectors_portable) {
    mp_sha256_use_accel(0);
    sha256_vectors();
    mp_sha256_use_accel(1);
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
    MP_RUN(sha256_vectors_portable);
    MP_RUN(sha256_accel_same_as_portable);
    MP_RUN(fnv_vectors);
    MP_RUN(crc32_vector);
    MP_TEST_MAIN_END();
}
