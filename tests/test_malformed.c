// SPDX-License-Identifier: GPL-3.0-or-later
// Deliberately tampered inputs, with the CRCs recalculated: an attacker can compute them too,
// so the readers have to reject impossible sizes without reading outside their buffers.
#include <stdio.h>
#include <stdlib.h>
#include <zstd.h>

#include "mp_hash.h"
#include "mp_idx3.h"
#include "mp_p5z.h"
#include "mp_pack.h"
#include "mp_platform.h"
#include "mp_rules.h"
#include "mp_test.h"
#include "mp_zpk.h"

#define TMP MP_TEST_TMP "/malformed"
#define BASIC MP_FIXTURES_DIR "/basic.exfat"

static uint32_t rd32(const uint8_t *p) { return p[0] | p[1] << 8 | p[2] << 16 | (uint32_t)p[3] << 24; }
static uint64_t rd64(const uint8_t *p) { return rd32(p) | (uint64_t)rd32(p + 4) << 32; }
static void wr32(uint8_t *p, uint32_t v) {
    for (int i = 0; i < 4; i++) p[i] = (uint8_t)(v >> (8 * i));
}
static void wr64(uint8_t *p, uint64_t v) {
    for (int i = 0; i < 8; i++) p[i] = (uint8_t)(v >> (8 * i));
}

// ---- a valid pack of basic, in memory --------------------------------------------------

typedef struct {
    uint8_t *v[16];
    size_t n[16], cap[16];
    uint32_t cur;
} memsink;

static int ms_begin(void *ctx, uint32_t vol, mp_err *e) {
    memsink *m = ctx;
    (void)e;
    m->cur = vol;
    m->cap[vol] = 8u << 20;
    m->v[vol] = calloc(1, m->cap[vol]);
    m->n[vol] = 64;
    return 0;
}
static int ms_write(void *ctx, const void *p, size_t n, mp_err *e) {
    memsink *m = ctx;
    (void)e;
    memcpy(m->v[m->cur] + m->n[m->cur], p, n);
    m->n[m->cur] += n;
    return 0;
}
static int ms_end(void *ctx, uint32_t vol, mp_err *e) {
    (void)ctx;
    (void)vol;
    (void)e;
    return 0;
}
static int ms_header(void *ctx, uint32_t vol, const uint8_t hdr[64], mp_err *e) {
    (void)e;
    memcpy(((memsink *)ctx)->v[vol], hdr, 64);
    return 0;
}
static int ms_read(void *ctx, uint32_t vol, uint64_t off, void *buf, size_t n, mp_err *e) {
    memsink *m = ctx;
    if (vol >= 16 || !m->v[vol] || off + n > m->n[vol]) {
        mp_err_set(e, "outside the volume");
        return -1;
    }
    memcpy(buf, m->v[vol] + off, n);
    return 0;
}
static uint64_t ms_size(void *ctx, uint32_t vol) { return vol < 16 ? ((memsink *)ctx)->n[vol] : 0; }

static memsink packs;
static uint8_t *zidx;
static size_t zlen;

static int build_packs(void) {
    if (zidx) return 0;
    mp_err e = {{0}};
    mp_source *s = NULL;
    if (mp_source_open(BASIC, &s, &e) != 0) return -1;
    mp_rules *r = mp_rules_new();
    uint32_t *files = malloc(mp_source_count(s) * sizeof *files);
    size_t n = 0;
    for (size_t i = 0; i < mp_source_count(s); i++)
        if (mp_rules_classify(r, mp_source_entry(s, i)->path, mp_source_entry(s, i)->size) == MP_PACKABLE)
            files[n++] = (uint32_t)i;
    mp_zpk_sink sink = {ms_begin, ms_write, ms_end, ms_header, &packs};
    mp_zpk_opts o = {1, 4, MP_ZPK_VOLUME_LIMIT, NULL};
    mp_zpk_stats st;
    uint8_t(*sha)[32] = malloc(n * sizeof *sha);
    int rc = mp_zpk_write(s, files, n, &o, &sink, &zidx, &zlen, sha, &st, &e);
    free(sha);
    free(files);
    mp_rules_free(r);
    mp_source_close(s);
    return rc;
}

static void zpk_fix_crcs(uint8_t *d, size_t n) {
    wr32(d + 112, 0);
    wr32(d + 116, 0);
    wr32(d + 112, mp_crc32(d + 128, n - 128, 0));
    uint8_t h[128];
    memcpy(h, d, 128);
    memset(h + 112, 0, 8);
    wr32(d + 116, mp_crc32(h, 128, 0));
}

static int zpk_open_copy(uint8_t *d, size_t n, mp_err *e) {
    mp_zpk_volsrc vs = {ms_read, ms_size, &packs};
    mp_zpk *z = NULL;
    int rc = mp_zpk_open(d, n, &vs, &z, e);
    mp_zpk_close(z);
    return rc;
}

MP_TEST(zpk_wrapped_strings_len) {
    if (build_packs() != 0) {
        MP_FAIL_("could not create the pack");
        return;
    }
    uint8_t *d = malloc(zlen);
    memcpy(d, zidx, zlen);
    // As many files as fit "separately" (nf <= len/32), with no blocks or volumes, and a
    // string length that wraps around so that soff + slen == len.
    uint64_t nf = zlen / 32, foff = 128, coff = foff + 32 * nf, voff = coff, soff = voff;
    wr64(d + 40, nf);
    wr64(d + 48, 0);
    wr32(d + 20, 0);
    wr64(d + 64, coff);
    wr64(d + 72, voff);
    wr64(d + 80, soff);
    wr64(d + 88, (uint64_t)zlen - soff);   // wraps around: soff > zlen
    zpk_fix_crcs(d, zlen);
    mp_err e = {{0}};
    MP_CHECK(zpk_open_copy(d, zlen, &e) == -1);
    MP_CHECK_CONTAINS(e.msg, "tables");
    free(d);
}

MP_TEST(zpk_huge_file_size) {
    if (build_packs() != 0) return;
    uint8_t *d = malloc(zlen);
    memcpy(d, zidx, zlen);
    wr64(d + 128 + 8, UINT64_MAX);   // size of the first file: 2^64 - 1
    zpk_fix_crcs(d, zlen);
    mp_err e = {{0}};
    MP_CHECK(zpk_open_copy(d, zlen, &e) == -1);
    MP_CHECK_CONTAINS(e.msg, "impossible file size");
    free(d);
}

MP_TEST(zpk_chunk_volume_out_of_range) {
    if (build_packs() != 0) return;
    uint8_t *d = malloc(zlen);
    memcpy(d, zidx, zlen);
    uint64_t nc = rd64(d + 48), coff = rd64(d + 64), voff = rd64(d + 72);
    // The last block says it is in volume 1 (only 0 exists), right where 0 ends.
    uint64_t last = rd64(d + coff + 8 * (nc - 1));
    uint64_t prev_end = (last & 0xFFFFFFFFull);
    uint64_t moved = (last & ~(0xFFFull << 32) & ~0xFFFFFFFFull) | (1ull << 32) | 64;
    wr64(d + coff + 8 * (nc - 1), moved);
    wr64(d + voff, prev_end);   // volume 0 "ends" where that block started
    zpk_fix_crcs(d, zlen);
    mp_err e = {{0}};
    MP_CHECK(zpk_open_copy(d, zlen, &e) == -1);
    MP_CHECK_CONTAINS(e.msg, "nonexistent volume");
    free(d);
}

MP_TEST(idx3_wrapped_hash_off) {
    mp_idx3_row rows[2] = {{"a.bin", 1, 0}, {"b.bin", 2, 0}};
    uint8_t *d = NULL;
    size_t n = 0;
    mp_err e = {{0}};
    MP_CHECK(mp_idx3_build(rows, 2, &d, &n, &e) == 0);
    if (!d) return;
    uint64_t slots = 1u << 20;   // 16 · slots > n
    wr32(d + 44, (uint32_t)slots);
    wr64(d + 32, (uint64_t)n - 16 * slots);   // hash_off wraps around
    mp_idx3_entry *v = NULL;
    size_t m = 0;
    MP_CHECK(mp_idx3_parse(d, n, &v, &m, &e) == -1);
    MP_CHECK_CONTAINS(e.msg, "tables");
    free(d);
}

// ---- .p5z with the index tampered with -------------------------------------------------------

static uint8_t *slurp(const char *path, size_t *len) {
    mp_file *f = NULL;
    uint64_t size = 0;
    size_t got = 0;
    *len = 0;
    if (mp_file_open_read(path, &f, NULL) != 0) return NULL;
    mp_file_size(f, &size, NULL);
    uint8_t *b = malloc((size_t)size + 1);
    mp_file_pread(f, b, (size_t)size, 0, &got, NULL);
    mp_file_close(f);
    *len = got;
    return b;
}

MP_TEST(p5z_wrapped_strings_len) {
    mp_err e = {{0}};
    const char *p = TMP "/base.p5z";
    if (mp_path_kind(p) != 1) {
        mp_make_dirs(TMP, &e);
        mp_pack_opts o;
        mp_pack_opts_default(&o);
        o.threads = 4;
        o.progress = 0;
        o.level = 1;
        mp_pack_out out;
        mp_pack_result r;
        MP_CHECK(mp_pack_out_p5z(p, 4, &out, &e) == 0);
        MP_CHECK(mp_pack_run(BASIC, &out, &o, &r, &e) == 0);
        out.destroy(out.ctx);
    }
    size_t n = 0;
    uint8_t *d = slurp(p, &n);
    MP_CHECK(d && n > 200);
    if (!d) return;
    const uint8_t *ft = d + n - 32;
    uint64_t ioff = rd64(ft + 8), ilen = rd64(ft + 16);
    unsigned long long us = ZSTD_getFrameContentSize(d + ioff, (size_t)ilen);
    uint8_t *ix = malloc((size_t)us);
    MP_CHECK(ZSTD_decompress(ix, (size_t)us, d + ioff, (size_t)ilen) == us);
    // Many more files than fit and a string length that wraps around.
    uint32_t nf = 1000000, nfr = rd32(ix + 16), nm = rd32(ix + 20);
    uint64_t fixed = 56 + 88ull * nf + 16ull * nfr + 64ull * nm;
    wr32(ix + 12, nf);
    wr64(ix + 24, (uint64_t)us - fixed);
    size_t cap = ZSTD_compressBound((size_t)us);
    uint8_t *cz = malloc(cap);
    size_t cl = ZSTD_compress(cz, cap, ix, (size_t)us, 1);
    size_t m = (size_t)ioff + cl + 32;
    uint8_t *out = malloc(m);
    memcpy(out, d, (size_t)ioff);
    memcpy(out + ioff, cz, cl);
    uint8_t *nft = out + ioff + cl;
    memset(nft, 0, 32);
    memcpy(nft, "P5ZFOOT1", 8);
    wr64(nft + 8, ioff);
    wr64(nft + 16, cl);
    wr32(nft + 24, mp_crc32(cz, cl, 0));
    wr32(nft + 28, mp_crc32(nft, 28, 0));
    remove(TMP "/wrapped.p5z");
    MP_CHECK(mp_write_file(TMP "/wrapped.p5z", out, m, &e) == 0);
    mp_p5z *pz = NULL;
    mp_err e2 = {{0}};
    MP_CHECK(mp_p5z_open(TMP "/wrapped.p5z", &pz, &e2) == -1);
    MP_CHECK_CONTAINS(e2.msg, "index size");
    mp_p5z_close(pz);
    free(out);
    free(cz);
    free(ix);
    free(d);
}

int main(void) {
    MP_RUN(zpk_wrapped_strings_len);
    MP_RUN(zpk_huge_file_size);
    MP_RUN(zpk_chunk_volume_out_of_range);
    MP_RUN(idx3_wrapped_hash_off);
    MP_RUN(p5z_wrapped_strings_len);
    MP_TEST_MAIN_END();
}
