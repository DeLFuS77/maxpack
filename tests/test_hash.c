// SPDX-License-Identifier: GPL-3.0-or-later
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "mp_hash.h"
#include "mp_test.h"

#if defined(_M_X64) || defined(__x86_64__)
#ifdef _MSC_VER
#include <intrin.h>
#else
#include <cpuid.h>
#endif
#endif

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
    // The 448-bit message of FIPS 180-2 (two blocks once padded) and the 896-bit one.
    hex_of("abcdbcdecdefdefgefghfghighijhijkijkljklmklmnlmnomnopnopq", 56, h);
    MP_CHECK_STR(h, "248d6a61d20638b8e5c026930c3e6039a33ce45964ff2167f6ecedd419db06c1");
    hex_of("abcdefghbcdefghicdefghijdefghijkefghijklfghijklmghijklmn"
           "hijklmnoijklmnopjklmnopqklmnopqrlmnopqrsmnopqrstnopqrstu",
           112, h);
    MP_CHECK_STR(h, "cf5b16a778af8380036ce59e7b0492370b249b11e8f07a51afac45037afee9d1");
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
// lengths of 0 to 3 MiB, starts that are not aligned and the data split between updates in many
// ways: pieces of one size, of sizes that cycle (empty ones too) and of random sizes.
static uint32_t xs = 2463534242u;
static uint32_t xnext(void) {
    xs ^= xs << 13;
    xs ^= xs >> 17;
    xs ^= xs << 5;
    return xs;
}

static void digest_split(const uint8_t *p, size_t n, const size_t *piece, size_t np, uint8_t d[32]) {
    mp_sha256 s;
    mp_sha256_init(&s);
    for (size_t off = 0, k = 0; off < n; k++) {
        size_t m = piece[k % np] < n - off ? piece[k % np] : n - off;
        mp_sha256_update(&s, p + off, m);
        off += m;
    }
    mp_sha256_final(&s, d);
}

// Whether the engine should find SHA instructions here, asked on its own: on 64-bit x86 CPUID says the
// CPU has SHA, SSE4.1 and SSSE3, on 64-bit ARM the compiler builds for SHA-2. So a CPU that has them is
// not left on the portable code unnoticed (as when the instructions' code failed its own check).
static int cpu_has_sha_instructions(void) {
#if defined(_M_X64) || defined(__x86_64__)
    unsigned b, c;
#ifdef _MSC_VER
    int r[4];
    __cpuid(r, 0);
    if (r[0] < 7) return 0;
    __cpuid(r, 1);
    c = (unsigned)r[2];
    __cpuidex(r, 7, 0);
    b = (unsigned)r[1];
#else
    unsigned a, d, c7;
    if (__get_cpuid_max(0, NULL) < 7) return 0;
    __cpuid(1, a, b, c, d);
    __cpuid_count(7, 0, a, b, c7, d);
#endif
    return (b >> 29 & 1) && (c >> 19 & 1) && (c >> 9 & 1);
#elif defined(__aarch64__) && (defined(__ARM_FEATURE_SHA2) || defined(__ARM_FEATURE_CRYPTO))
    return 1;
#else
    return 0;
#endif
}

MP_TEST(sha256_accel_same_as_portable) {
    int accel = mp_sha256_accel();
    fprintf(stderr, "  SHA-256: %s\n", accel ? "the CPU's SHA instructions" : "portable code");
    if (accel != cpu_has_sha_instructions())
        MP_FAIL_("the CPU %s SHA instructions, and the engine %s them", accel ? "has no" : "has",
                 accel ? "uses" : "does not use");
    size_t cap = (3u << 20) + 64;
    uint8_t *b = malloc(cap);
    if (!b) {
        MP_FAIL_("out of memory");
        return;
    }
    for (size_t i = 0; i < cap; i++) b[i] = (uint8_t)(xnext() >> 24);
    size_t lens[330], nl = 0;
    for (size_t n = 0; n <= 300; n++) lens[nl++] = n;   // every length of up to 4 blocks and a bit
    static const size_t big[] = {511, 512, 513, 1000, 4095, 4096, 4097, 65535, 65536, 65537, 262151,
                                 1u << 20, (1u << 20) + 55, (3u << 20) - 1, 3u << 20};
    for (size_t i = 0; i < sizeof big / sizeof *big; i++) lens[nl++] = big[i];
    static const size_t offs[] = {0, 1, 3, 13, 63};
    static const size_t whole[] = {SIZE_MAX}, one[] = {1}, seven[] = {7}, block[] = {64}, page[] = {4096},
                        cycle[] = {100, 3, 61, 0, 1000, 65}, empties[] = {0, 33};
    size_t rnd[64];
    for (size_t i = 0; i < 64; i++) rnd[i] = 1 + xnext() % 9000;
    const struct {
        const size_t *v;
        size_t n;
    } splits[] = {{whole, 1}, {one, 1}, {seven, 1}, {block, 1}, {page, 1}, {cycle, 6}, {empties, 2}, {rnd, 64}};
    size_t cases = 0;
    int bad = 0;
    for (size_t i = 0; i < nl; i++)
        for (size_t o = 0; o < sizeof offs / sizeof *offs; o++)
            for (size_t j = 0; j < sizeof splits / sizeof *splits; j++) {
                size_t n = lens[i];
                const uint8_t *p = b + offs[o];
                // long ones: enough with the larger pieces and two of the starts
                if (n > 70000 && (splits[j].v == one || splits[j].v == seven || (offs[o] != 0 && offs[o] != 13)))
                    continue;
                uint8_t ref[32], sw[32], hw[32];
                mp_sha256_use_accel(0);
                MP_CHECK(mp_sha256_accel() == 0);
                digest_split(p, n, whole, 1, ref);
                digest_split(p, n, splits[j].v, splits[j].n, sw);
                mp_sha256_use_accel(1);
                digest_split(p, n, splits[j].v, splits[j].n, hw);
                cases++;
                int sw_bad = memcmp(ref, sw, 32) != 0, hw_bad = memcmp(ref, hw, 32) != 0;
                if ((sw_bad || hw_bad) && bad++ < 10)
                    MP_FAIL_("length %zu at offset %zu, split %zu: portable %s, accelerated %s", n, offs[o], j,
                             sw_bad ? "differs" : "same", hw_bad ? "differs" : "same");
            }
    fprintf(stderr, "  %zu cases compared\n", cases);
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
