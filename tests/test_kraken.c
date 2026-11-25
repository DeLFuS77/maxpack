// SPDX-License-Identifier: GPL-3.0-or-later
// The Kraken decoder on the blocks of make_fpkg.py's encoder (kraken.vec): what the package writers
// make (one and two sub-chunks, Huffman and raw arrays, long lengths) and the parts of the format
// they leave out (repeated offsets, offsets without scaling, two-split arrays, delta literals, bare
// arrays, a stored second sub-chunk) decode to their bytes; every block cut short, with bytes
// flipped or with the wrong flags ends in an error or some bytes, never a crash; a block decodes the
// same in a reused context.
#include <stdlib.h>

#include "mp_fixture.h"
#include "mp_kraken.h"
#include "mp_test.h"

#define FX MP_FIXTURES_DIR

typedef struct {
    uint32_t raw_len, enc_len, flags, first;
    const uint8_t *raw, *enc;
} vec;

static uint8_t *g_buf;
static vec *g_v;
static size_t g_nv;

static uint32_t rd32(const uint8_t *p) { return p[0] | p[1] << 8 | p[2] << 16 | (uint32_t)p[3] << 24; }

static int load(void) {
    if (g_v) return 1;
    size_t n = 0;
    g_buf = mpt_slurp(FX "/kraken.vec", &n);
    if (!g_buf || n < 8 || memcmp(g_buf, "KVEC", 4)) return 0;
    uint32_t count = rd32(g_buf + 4);
    g_v = calloc(count, sizeof *g_v);
    size_t at = 8;
    for (uint32_t i = 0; i < count && at + 16 <= n; i++) {
        vec v = {rd32(g_buf + at), rd32(g_buf + at + 4), rd32(g_buf + at + 8), rd32(g_buf + at + 12), NULL, NULL};
        at += 16;
        if (at + v.raw_len + v.enc_len > n) return 0;
        v.raw = g_buf + at;
        v.enc = g_buf + at + v.raw_len;
        at += v.raw_len + v.enc_len;
        g_v[g_nv++] = v;
    }
    return g_nv == count;
}

MP_TEST(decodes_the_vectors) {
    if (!load()) {
        MP_FAIL_("cannot read " FX "/kraken.vec");
        return;
    }
    mp_kraken *k = mp_kraken_new();
    uint8_t *out = malloc(MP_KRAKEN_BLOCK);
    size_t bad = 0, two = 0;
    for (size_t i = 0; i < g_nv; i++) {
        const vec *v = &g_v[i];
        memset(out, 0xAA, MP_KRAKEN_BLOCK);
        int rc = mp_kraken_decode(k, v->enc, v->enc_len, (int)v->flags, v->first, out, v->raw_len);
        if (rc != MP_KRAKEN_OK || memcmp(out, v->raw, v->raw_len) != 0) {
            if (bad++ < 5)
                MP_FAIL_("vector %zu (%u -> %u bytes, flags 0x%x): rc %d", i, v->enc_len, v->raw_len, v->flags, rc);
        }
        two += v->raw_len > MP_KRAKEN_CHUNK;
    }
    MP_CHECK_EQ_U64(bad, 0);
    MP_CHECK(g_nv >= 35);
    MP_CHECK(two >= 8);
    fprintf(stderr, "      (%zu blocks, %zu of two sub-chunks)\n", g_nv, two);
    free(out);
    mp_kraken_free(k);
}

// Deterministic damage: every block cut at several places, bytes flipped at many places, and the
// flags of the other kind; the decoder only has to come back.
MP_TEST(damaged_blocks_end_cleanly) {
    if (!load()) return;
    mp_kraken *k = mp_kraken_new();
    uint8_t *out = malloc(MP_KRAKEN_BLOCK), *mut = malloc(MP_KRAKEN_BLOCK * 2);
    uint32_t seed = 1;
    size_t runs = 0, ok = 0;
    for (size_t i = 0; i < g_nv; i++) {
        const vec *v = &g_v[i];
        for (int cut = 1; cut <= 4; cut++) {
            size_t n = (size_t)v->enc_len * cut / 5;
            ok += mp_kraken_decode(k, v->enc, n, (int)v->flags, v->first, out, v->raw_len) == MP_KRAKEN_OK;
            runs++;
        }
        for (int m = 0; m < 24; m++) {
            memcpy(mut, v->enc, v->enc_len);
            for (int f = 0; f < 1 + m % 3; f++) {
                seed = seed * 1103515245u + 12345u;
                size_t at = (seed >> 8) % (v->enc_len ? v->enc_len : 1);
                mut[at] ^= (uint8_t)(1u << ((seed >> 4) & 7));
            }
            ok += mp_kraken_decode(k, mut, v->enc_len, (int)v->flags, v->first, out, v->raw_len) == MP_KRAKEN_OK;
            runs++;
        }
        static const int other[] = {0, MP_KRAKEN_SUB0, MP_KRAKEN_NEWLZ0 | MP_KRAKEN_NEWLZ1 | MP_KRAKEN_RESTART1,
                                    MP_KRAKEN_NEWLZ0 | MP_KRAKEN_SUB1};
        for (size_t f = 0; f < sizeof other / sizeof other[0]; f++) {
            mp_kraken_decode(k, v->enc, v->enc_len, other[f], v->first, out, v->raw_len);
            mp_kraken_decode(k, v->enc, v->enc_len, (int)v->flags, v->first / 2 + 1, out, v->raw_len);
            runs += 2;
        }
        // the wrong decoded size
        mp_kraken_decode(k, v->enc, v->enc_len, (int)v->flags, v->first, out, v->raw_len / 2 + 1);
        mp_kraken_decode(k, v->enc, v->enc_len, (int)v->flags, v->first, out, MP_KRAKEN_BLOCK);
        runs += 2;
    }
    // a block of nothing, and limits
    MP_CHECK(mp_kraken_decode(k, NULL, 0, 0, 0, out, 0) == MP_KRAKEN_OK);
    MP_CHECK(mp_kraken_decode(k, out, 1, 0, 0, out, 0) == MP_KRAKEN_MALFORMED);
    MP_CHECK(mp_kraken_decode(k, out, 10, MP_KRAKEN_NEWLZ0, 0, out, MP_KRAKEN_BLOCK + 1) == MP_KRAKEN_MALFORMED);
    MP_CHECK(mp_kraken_decode(k, out, 100, MP_KRAKEN_NEWLZ0 | MP_KRAKEN_NEWLZ1, 0, out, MP_KRAKEN_BLOCK) ==
             MP_KRAKEN_MALFORMED);
    MP_CHECK(mp_kraken_decode(NULL, out, 1, 0, 0, out, 1) == MP_KRAKEN_NOMEM);
    fprintf(stderr, "      (%zu damaged decodings, %zu of them gave bytes)\n", runs, ok);
    // and a good block still decodes after all that in the same context
    const vec *v = &g_v[0];
    MP_CHECK(mp_kraken_decode(k, v->enc, v->enc_len, (int)v->flags, v->first, out, v->raw_len) == MP_KRAKEN_OK &&
             memcmp(out, v->raw, v->raw_len) == 0);
    free(out);
    free(mut);
    mp_kraken_free(k);
}

int main(void) {
    MP_RUN(decodes_the_vectors);
    MP_RUN(damaged_blocks_end_cleanly);
    free(g_v);
    free(g_buf);
    MP_TEST_MAIN_END();
}
