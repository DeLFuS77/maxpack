// SPDX-License-Identifier: GPL-3.0-or-later
#define ZSTD_STATIC_LINKING_ONLY
#include <stdio.h>
#include <stdlib.h>
#include <zdict.h>
#include <zstd.h>

#include "mp_measure.h"
#include "mp_sample.h"
#include "mp_test.h"

#define MiB (1u << 20)

static void fill_random(uint8_t *p, size_t n, uint64_t seed) {
    for (size_t i = 0; i < n; i += 8) {
        uint64_t v = mp_splitmix64(seed + i);
        for (size_t k = 0; k < 8 && i + k < n; k++) p[i + k] = (uint8_t)(v >> (8 * k));
    }
}

static size_t make_records(uint8_t *p, size_t cap, unsigned first) {
    static const char *tipos[] = {"arma", "armadura", "pocion", "llave", "mapa"};
    size_t n = 0;
    for (unsigned i = first; n + 200 < cap; i++) {
        uint64_t r = mp_splitmix64(i);
        n += (size_t)snprintf((char *)p + n, cap - n,
                              "{\"id\": %u, \"nombre\": \"objeto_%05u\", \"tipo\": \"%s\", "
                              "\"valores\": [%u, %u, %u], \"activo\": %s}\n",
                              i, i, tipos[r % 5], (unsigned)(r >> 8) % 10000,
                              (unsigned)(r >> 24) % 10000, (unsigned)(r >> 40) % 10000,
                              (r >> 63) ? "true" : "false");
    }
    return n;
}

MP_TEST(random_is_raw) {
    uint8_t *d = malloc(MiB);
    fill_random(d, MiB, 1);
    mp_measurer *m = mp_measurer_new(NULL);
    mp_cfg_stat st[MP_CFG_COUNT] = {{0}};
    mp_measure_window(m, d, MiB, 0, st);
    for (int c = 0; c < MP_CFG_COUNT; c++) {
        MP_CHECK_EQ_U64(st[c].raw, MiB);
        MP_CHECK_EQ_U64(st[c].stored, MiB);
    }
    mp_measurer_free(m);
    free(d);
}

MP_TEST(zeros_compress) {
    uint8_t *d = calloc(1, MiB);
    mp_measurer *m = mp_measurer_new(NULL);
    mp_cfg_stat st[MP_CFG_COUNT] = {{0}};
    mp_measure_window(m, d, MiB, 0, st);
    MP_CHECK(st[MP_CFG_V1_L19].stored < 1024);
    MP_CHECK(st[MP_CFG_ZLIB64K].stored < 8192);
    MP_CHECK(st[MP_CFG_ZLIB64K].ns > 0);
    mp_measurer_free(m);
    free(d);
}

MP_TEST(partial_window) {
    uint8_t *d = malloc(300000);
    fill_random(d, 300000, 7);
    for (size_t i = 0; i < 300000; i += 3) d[i] = 'a';   // something compressible
    mp_measurer *m = mp_measurer_new(NULL);
    mp_cfg_stat st[MP_CFG_COUNT] = {{0}};
    mp_measure_window(m, d, 300000, 1, st);
    for (int c = 0; c < MP_CFG_COUNT; c++) MP_CHECK_EQ_U64(st[c].raw, 300000);
    MP_CHECK(st[MP_CFG_V1_L19].stored < 300000);
    mp_measurer_free(m);
    free(d);
}

MP_TEST(frame_params) {
    size_t n = MP_BLOCK_V1;
    uint8_t *src = malloc(n), *back = malloc(n);
    make_records(src, n + 1, 0);
    for (size_t i = make_records(src, n, 0); i < n; i++) src[i] = ' ';
    size_t cap = ZSTD_compressBound(n);
    uint8_t *dst = malloc(cap);
    ZSTD_CCtx *c = ZSTD_createCCtx();
    size_t f = mp_zstd_block(c, 22, 18, src, n, dst, cap);
    MP_CHECK(f > 0 && f < n);
    ZSTD_frameHeader h;
    MP_CHECK(ZSTD_getFrameHeader(&h, dst, f) == 0);
    MP_CHECK_EQ_U64(h.frameContentSize, n);
    MP_CHECK(h.windowSize <= 262144);
    MP_CHECK(h.checksumFlag == 1);
    MP_CHECK_EQ_U64(ZSTD_decompress(back, n, dst, f), n);
    MP_CHECK(memcmp(src, back, n) == 0);
    ZSTD_freeCCtx(c);
    free(src);
    free(back);
    free(dst);
}

MP_TEST(dict_used_by_parity) {
    // 2000 training records, one per sample.
    size_t cap = 2000 * 400;   // each sample takes up to 400 bytes
    uint8_t *train = malloc(cap);
    size_t sizes[2000], total = 0;
    for (unsigned i = 0; i < 2000; i++) {
        size_t k = make_records(train + total, 400, i * 1000);
        sizes[i] = k;
        total += k;
    }
    void *dict = malloc(110 * 1024);
    size_t ds = ZDICT_trainFromBuffer(dict, 110 * 1024, train, sizes, 2000);
    MP_CHECK(!ZDICT_isError(ds));
    mp_dict_pair dp = {{NULL, dict}, {0, ds}};   // only dict[1]: evaluates parity 0
    uint8_t *w = malloc(65536);
    size_t n = make_records(w, 65536, 5000000);
    mp_measurer *m = mp_measurer_new(&dp);
    mp_cfg_stat st0[MP_CFG_COUNT] = {{0}}, st1[MP_CFG_COUNT] = {{0}};
    mp_measure_window(m, w, n, 0, st0);
    mp_measure_window(m, w, n, 1, st1);
    MP_CHECK(st0[MP_CFG_DICT_L19].stored < st0[MP_CFG_V1_L19].stored);
    MP_CHECK_EQ_U64(st1[MP_CFG_DICT_L19].stored, st1[MP_CFG_V1_L19].stored);   // without dict[0]
    mp_measurer_free(m);
    free(w);
    free(dict);
    free(train);
}

// Characterization: with a useless dictionary of realistic size (110 KiB random), dict-l19 can only
// lose what zstd does not avoid: with any dictionary, btultra2 does not do its double pass
// over the first block (measured: +0.88 % here). If the difference ever grows, the
// search parameters of the dictionary context will have weakened. Margin: 1.5 %.
MP_TEST(useless_dict_costs_only_two_pass) {
    size_t ds = 110 * 1024;
    uint8_t *raw_dict = malloc(ds);
    fill_random(raw_dict, ds, 77);
    raw_dict[0] ^= 0x55;   // so that it does not start with the zstd dictionary signature
    mp_dict_pair dp = {{NULL, raw_dict}, {0, ds}};
    uint8_t *w = malloc(MiB);
    size_t n = make_records(w, MiB, 9000000);
    mp_measurer *m = mp_measurer_new(&dp);
    mp_cfg_stat st[MP_CFG_COUNT] = {{0}};
    mp_measure_window(m, w, n, 0, st);
    uint64_t a = st[MP_CFG_DICT_L19].stored, b = st[MP_CFG_V1_L19].stored;
    if (a * 1000 > b * 1015)
        MP_FAIL_("dict-l19 %llu frente a v1-l19 %llu (+%.2f %%)", (unsigned long long)a,
                 (unsigned long long)b, 100.0 * ((double)a - (double)b) / (double)b);
    mp_measurer_free(m);
    free(w);
    free(raw_dict);
}

int main(void) {
    MP_RUN(random_is_raw);
    MP_RUN(zeros_compress);
    MP_RUN(partial_window);
    MP_RUN(frame_params);
    MP_RUN(dict_used_by_parity);
    MP_RUN(useless_dict_costs_only_two_pass);
    MP_TEST_MAIN_END();
}
