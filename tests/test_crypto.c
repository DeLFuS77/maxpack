// SPDX-License-Identifier: GPL-3.0-or-later
// The cryptography of the FPKG reader against the standards' own vectors: AES-128 (FIPS-197
// appendices B and C.1), CBC (SP 800-38A F.2.1/F.2.2), XTS (IEEE 1619 vectors 1 and 2), SHA3-256
// (FIPS-202 examples, and Python's hashlib for the block edges), HMAC-SHA256 (RFC 4231 cases 1, 2
// and 6). The CPU's AES instructions and the portable code give the same bytes, and a part of an XTS
// unit decrypts as the same part of the whole unit.
#include <stdlib.h>

#include "mp_crypto.h"
#include "mp_test.h"

static void unhex(const char *h, uint8_t *out, size_t n) {
    for (size_t i = 0; i < n; i++) {
        unsigned v = 0;
        sscanf(h + 2 * i, "%2x", &v);
        out[i] = (uint8_t)v;
    }
}

static int same_hex(const uint8_t *p, size_t n, const char *h) {
    uint8_t b[512];
    if (n > sizeof b || strlen(h) != 2 * n) return 0;
    unhex(h, b, n);
    return memcmp(b, p, n) == 0;
}

// Every test runs with the CPU's instructions (where there are any) and with the portable code.
static void both(void (*f)(void)) {
    mp_aes_use_accel(1);
    f();
    if (mp_aes_accel()) {
        mp_aes_use_accel(0);
        f();
        mp_aes_use_accel(1);
    }
}

static void aes_vectors(void) {
    uint8_t key[16], pt[16], ct[16];
    mp_aes128 a;
    unhex("000102030405060708090a0b0c0d0e0f", key, 16);
    unhex("00112233445566778899aabbccddeeff", pt, 16);
    mp_aes128_init(&a, key);
    mp_aes128_encrypt(&a, pt, ct, 1);
    MP_CHECK(same_hex(ct, 16, "69c4e0d86a7b0430d8cdb78070b4c55a"));
    mp_aes128_decrypt(&a, ct, ct, 1);
    MP_CHECK(memcmp(ct, pt, 16) == 0);
    unhex("2b7e151628aed2a6abf7158809cf4f3c", key, 16);
    unhex("3243f6a8885a308d313198a2e0370734", pt, 16);
    mp_aes128_init(&a, key);
    mp_aes128_encrypt(&a, pt, ct, 1);
    MP_CHECK(same_hex(ct, 16, "3925841d02dc09fbdc118597196a0b32"));
    mp_aes128_decrypt(&a, ct, ct, 1);
    MP_CHECK(memcmp(ct, pt, 16) == 0);
}
MP_TEST(aes_fips197) { both(aes_vectors); }

static void cbc_vectors(void) {
    uint8_t key[16], iv[16], buf[64];
    const char *pt = "6bc1bee22e409f96e93d7e117393172aae2d8a571e03ac9c9eb76fac45af8e51"
                     "30c81c46a35ce411e5fbc1191a0a52eff69f2445df4f9b17ad2b417be66c3710";
    const char *ct = "7649abac8119b246cee98e9b12e9197d5086cb9b507219ee95db113a917678b2"
                     "73bed6b8e3c1743b7116e69e222295163ff1caa1681fac09120eca307586e1a7";
    mp_aes128 a;
    unhex("2b7e151628aed2a6abf7158809cf4f3c", key, 16);
    unhex("000102030405060708090a0b0c0d0e0f", iv, 16);
    mp_aes128_init(&a, key);
    unhex(pt, buf, 64);
    mp_cbc_encrypt(&a, iv, buf, 64);
    MP_CHECK(same_hex(buf, 64, ct));
    mp_cbc_decrypt(&a, iv, buf, 64);
    MP_CHECK(same_hex(buf, 64, pt));
}
MP_TEST(cbc_sp800_38a) { both(cbc_vectors); }

static void xts_vectors(void) {
    uint8_t k1[16] = {0}, k2[16] = {0}, buf[32] = {0};
    mp_aes128 d, t;
    mp_aes128_init(&d, k1);
    mp_aes128_init(&t, k2);
    mp_xts_encrypt(&d, &t, 0, 0, buf, 32);
    MP_CHECK(same_hex(buf, 32, "917cf69ebd68b2ec9b9fe9a3eadda692cd43d2f59598ed858c02c2652fbf922e"));
    mp_xts_decrypt(&d, &t, 0, 0, buf, 32);
    uint8_t zero[32] = {0};
    MP_CHECK(memcmp(buf, zero, 32) == 0);
    memset(k1, 0x11, 16);
    memset(k2, 0x22, 16);
    memset(buf, 0x44, 32);
    mp_aes128_init(&d, k1);
    mp_aes128_init(&t, k2);
    mp_xts_encrypt(&d, &t, 0x3333333333ull, 0, buf, 32);
    MP_CHECK(same_hex(buf, 32, "c454185e6a16936e39334038acef838bfb186fff7480adc4289382ecd6d394f0"));
    // the second block alone, at its place in the unit
    uint8_t second[16];
    memcpy(second, buf + 16, 16);
    mp_xts_decrypt(&d, &t, 0x3333333333ull, 16, second, 16);
    uint8_t want[16];
    memset(want, 0x44, 16);
    MP_CHECK(memcmp(second, want, 16) == 0);
}
MP_TEST(xts_ieee1619) { both(xts_vectors); }

// A 64 KiB unit (a PFS block) in one piece and in odd parts, both directions, past the 8 blocks the
// instructions take at a time.
static uint8_t g_ref[65536];
static int g_have_ref;
static void xts_parts(void) {
    uint8_t k1[16], k2[16];
    for (int i = 0; i < 16; i++) {
        k1[i] = (uint8_t)(i * 17 + 3);
        k2[i] = (uint8_t)(250 - i * 7);
    }
    mp_aes128 d, t;
    mp_aes128_init(&d, k1);
    mp_aes128_init(&t, k2);
    uint8_t *plain = malloc(65536), *buf = malloc(65536);
    uint32_t x = 12345;
    for (size_t i = 0; i < 65536; i++) plain[i] = (uint8_t)((x = x * 1103515245u + 12345u) >> 16);
    memcpy(buf, plain, 65536);
    mp_xts_encrypt(&d, &t, 0x800000000123ull, 0, buf, 65536);
    if (!g_have_ref) {
        memcpy(g_ref, buf, 65536);
        g_have_ref = 1;
    } else {
        MP_CHECK(memcmp(g_ref, buf, 65536) == 0);   // the portable code wrote what the instructions did
    }
    static const size_t cuts[][2] = {{0, 16}, {16, 112}, {128, 4096}, {4224, 48}, {4272, 61248}, {65520, 16}};
    for (size_t c = 0; c < sizeof cuts / sizeof cuts[0]; c++) {
        uint8_t *p = buf + cuts[c][0];
        mp_xts_decrypt(&d, &t, 0x800000000123ull, cuts[c][0], p, cuts[c][1]);
    }
    MP_CHECK(memcmp(buf, plain, 65536) == 0);
    for (size_t c = 0; c < sizeof cuts / sizeof cuts[0]; c++)
        mp_xts_encrypt(&d, &t, 0x800000000123ull, cuts[c][0], buf + cuts[c][0], cuts[c][1]);
    MP_CHECK(memcmp(buf, g_ref, 65536) == 0);
    // many blocks at once against one at a time
    uint8_t *one = malloc(65536);
    mp_aes128_encrypt(&d, plain, buf, 4096);
    for (size_t i = 0; i < 4096; i++) mp_aes128_encrypt(&d, plain + 16 * i, one + 16 * i, 1);
    MP_CHECK(memcmp(buf, one, 65536) == 0);
    mp_aes128_decrypt(&d, buf, buf, 4096);
    MP_CHECK(memcmp(buf, plain, 65536) == 0);
    // CBC across many blocks
    uint8_t iv[16] = {9, 8, 7, 6, 5, 4, 3, 2, 1, 0, 1, 2, 3, 4, 5, 6};
    memcpy(buf, plain, 65536);
    mp_cbc_encrypt(&d, iv, buf, 65536);
    mp_cbc_decrypt(&d, iv, buf, 65536);
    MP_CHECK(memcmp(buf, plain, 65536) == 0);
    free(plain);
    free(buf);
    free(one);
}
MP_TEST(xts_parts_and_paths) {
    mp_aes_use_accel(1);
    int accel = mp_aes_accel();
    xts_parts();
    if (accel) {
        mp_aes_use_accel(0);
        xts_parts();
        mp_aes_use_accel(1);
    }
    fprintf(stderr, "      (AES on %s)\n", accel ? "the CPU's instructions" : "the portable code");
}

static int sha3_is(const void *p, size_t n, const char *hex) {
    uint8_t d[32];
    mp_sha3_256(p, n, d);
    return same_hex(d, 32, hex);
}

MP_TEST(sha3_fips202) {
    MP_CHECK(sha3_is("", 0, "a7ffc6f8bf1ed76651c14756a061d662f580ff4de43b49fa82d80a4b80f8434a"));
    MP_CHECK(sha3_is("abc", 3, "3a985da74fe225b2045c172d6bd390bd855f086e3e9d525b46bfe24511431532"));
    const char *m = "abcdbcdecdefdefgefghfghighijhijkijkljklmklmnlmnomnopnopq";
    MP_CHECK(sha3_is(m, strlen(m), "41c0dba2a9d6240849100376a8235e2c82e1b9998a999e21db32dd97496d3376"));
    // exactly one block of 136 bytes, and a million 'a' given in uneven pieces
    uint8_t *a = malloc(1000000);
    memset(a, 'a', 1000000);
    MP_CHECK(sha3_is(a, 136, "3fc5559f14db8e453a0a3091edbd2bc25e11528d81c66fa570a4efdcc2695ee1"));
    mp_sha3 s;
    mp_sha3_init(&s);
    size_t off = 0, step = 1;
    while (off < 1000000) {
        size_t k = 1000000 - off < step ? 1000000 - off : step;
        mp_sha3_update(&s, a + off, k);
        off += k;
        step = step * 3 + 1;
    }
    uint8_t d[32];
    mp_sha3_final(&s, d);
    MP_CHECK(same_hex(d, 32, "5c8875ae474a3634ba4fd55ec85bffd661f32aca75c6d699d0cdcb6c115891c1"));
    free(a);
}

static int hmac_is(const void *k, size_t kn, const char *msg, const char *hex) {
    uint8_t d[32];
    mp_hmac_sha256(k, kn, msg, strlen(msg), d);
    return same_hex(d, 32, hex);
}

MP_TEST(hmac_rfc4231) {
    uint8_t k[131];
    memset(k, 0x0b, 20);
    MP_CHECK(hmac_is(k, 20, "Hi There", "b0344c61d8db38535ca8afceaf0bf12b881dc200c9833da726e9376c2e32cff7"));
    MP_CHECK(hmac_is("Jefe", 4, "what do ya want for nothing?",
                     "5bdcc146bf60754e6a042426089575c75a003f089d2739839dec58b964ec3843"));
    memset(k, 0xaa, 131);
    MP_CHECK(hmac_is(k, 131, "Test Using Larger Than Block-Size Key - Hash Key First",
                     "60e431591ee0b67f0d8a26aacbf5b77f8e0bc6213728c5140546040f0ee37f54"));
}

int main(void) {
    MP_RUN(aes_fips197);
    MP_RUN(cbc_sp800_38a);
    MP_RUN(xts_ieee1619);
    MP_RUN(xts_parts_and_paths);
    MP_RUN(sha3_fips202);
    MP_RUN(hmac_rfc4231);
    MP_TEST_MAIN_END();
}
