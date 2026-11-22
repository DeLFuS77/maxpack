// SPDX-License-Identifier: GPL-3.0-or-later
#include <stdlib.h>

#include "mp_analyze.h"
#include "mp_platform.h"
#include "mp_rules.h"
#include "mp_test.h"

#define BASIC MP_FIXTURES_DIR "/basic.exfat"

static int run(const mp_analyze_opts *o, mp_report *r) {
    mp_err e = {{0}};
    memset(r, 0, sizeof *r);
    int rc = mp_analyze(BASIC, o, r, &e);
    if (rc != 0) MP_FAIL_("mp_analyze: %s", e.msg);
    return rc;
}

static mp_analyze_opts quiet(unsigned threads, int full) {
    mp_analyze_opts o;
    mp_analyze_opts_default(&o);
    o.threads = threads;
    o.full = full;
    o.progress = 0;
    return o;
}

// v1-l19 calculado aquí, bloque a bloque, sin pasar por el analizador.
static uint64_t direct_v1_l19(void) {
    mp_source *s = NULL;
    mp_err e = {{0}};
    if (mp_source_open(BASIC, &s, &e) != 0) return 0;
    mp_rules *rules = mp_rules_new();
    ZSTD_CCtx *c = ZSTD_createCCtx();
    size_t cap = ZSTD_compressBound(MP_BLOCK_V1);
    uint8_t *buf = malloc(MP_BLOCK_V1), *out = malloc(cap);
    uint64_t total = 0;
    for (size_t i = 0; i < mp_source_count(s); i++) {
        const mp_entry *en = mp_source_entry(s, i);
        if (mp_rules_classify(rules, en->path, en->size) != MP_PACKABLE) continue;
        uint64_t raw = 0, stored = 0;
        for (uint64_t off = 0; off < en->size; off += MP_BLOCK_V1) {
            size_t n = en->size - off < MP_BLOCK_V1 ? (size_t)(en->size - off) : MP_BLOCK_V1;
            mp_source_read(s, i, off, buf, n, &e);
            size_t f = mp_zstd_block(c, 19, 18, buf, n, out, cap);
            stored += f && f < n ? f : n;
            raw += n;
        }
        total += stored * 100 > raw * 99 ? raw : stored;   // ahorro < 1 % -> todo en bruto
    }
    free(buf);
    free(out);
    ZSTD_freeCCtx(c);
    mp_rules_free(rules);
    mp_source_close(s);
    return total;
}

MP_TEST(full_is_exact) {
    mp_analyze_opts o = quiet(2, 1);
    mp_report r;
    if (run(&o, &r) != 0) return;
    MP_CHECK_EQ_U64(r.est_packed[MP_CFG_V1_L19], direct_v1_l19());
    MP_CHECK_EQ_U64(r.loose_bytes, 10200);
    MP_CHECK_EQ_U64(r.loose_files, 3);
    MP_CHECK_EQ_U64(r.packable_files, r.files - 3);
    MP_CHECK(r.est_packed[MP_CFG_V1_L5] >= r.est_packed[MP_CFG_V1_L19]);
    MP_CHECK(r.dedup_done);
    MP_CHECK(r.loose_compressed > 0 && r.loose_compressed <= r.loose_bytes);
    MP_CHECK(r.p5z_estimate > r.est_packed[MP_CFG_V1_L19]);
    mp_report_free(&r);
}

MP_TEST(threads_independent) {
    for (int full = 0; full <= 1; full++) {
        mp_analyze_opts o1 = quiet(1, full), o4 = quiet(4, full);
        mp_report a, b;
        if (run(&o1, &a) != 0 || run(&o4, &b) != 0) return;
        for (int c = 0; c < MP_CFG_COUNT; c++) {
            MP_CHECK_EQ_U64(a.est_packed[c], b.est_packed[c]);
            MP_CHECK_EQ_U64(a.sampled[c].raw, b.sampled[c].raw);
            MP_CHECK_EQ_U64(a.sampled[c].stored, b.sampled[c].stored);
        }
        MP_CHECK_EQ_U64(a.dedup.dup_blocks, b.dedup.dup_blocks);
        MP_CHECK_EQ_U64(a.dedup.dup_files, b.dedup.dup_files);
        MP_CHECK(memcmp(&a.small, &b.small, sizeof a.small) == 0);
        MP_CHECK_EQ_U64(a.loose_compressed, b.loose_compressed);
        MP_CHECK_EQ_U64(a.v1_index_bytes, b.v1_index_bytes);
        mp_report_free(&a);
        mp_report_free(&b);
    }
}

MP_TEST(only_loose_game) {
    mp_err e = {{0}};
    const char *dir = MP_TEST_TMP "/analyze/onlyloose";
    MP_CHECK(mp_make_dirs(MP_TEST_TMP "/analyze/onlyloose/sce_sys", &e) == 0);
    MP_CHECK(mp_write_file(MP_TEST_TMP "/analyze/onlyloose/eboot.bin", "0123456789", 10, &e) == 0);
    MP_CHECK(mp_write_file(MP_TEST_TMP "/analyze/onlyloose/sce_sys/param.json", "{}", 2, &e) == 0);
    mp_analyze_opts o = quiet(2, 0);
    mp_report r;
    memset(&r, 0, sizeof r);
    MP_CHECK(mp_analyze(dir, &o, &r, &e) == 0);
    MP_CHECK_EQ_U64(r.packable_bytes, 0);
    for (int c = 0; c < MP_CFG_COUNT; c++) MP_CHECK_EQ_U64(r.est_packed[c], 0);
    MP_CHECK(r.sample_rate == 1.0);
    MP_CHECK_EQ_U64(r.loose_files, 2);
    MP_CHECK_STR(r.source_name, "onlyloose");
    mp_report_free(&r);
}

MP_TEST(keep_loose_moves_files) {
    mp_analyze_opts o = quiet(2, 0);
    mp_report a, b;
    if (run(&o, &a) != 0) return;
    static const char *const keep[] = {"many/**"};
    o.keep_loose = keep;
    o.nkeep = 1;
    if (run(&o, &b) != 0) return;
    MP_CHECK_EQ_U64(a.packable_files - b.packable_files, 200);
    MP_CHECK_EQ_U64(b.loose_files - a.loose_files, 200);
    mp_report_free(&a);
    mp_report_free(&b);
}

MP_TEST(small_game_full_sample) {
    mp_analyze_opts o = quiet(2, 0);
    mp_report r;
    if (run(&o, &r) != 0) return;
    MP_CHECK(r.sample_rate == 1.0);
    MP_CHECK_EQ_U64(r.sample_bytes, r.packable_bytes);
    MP_CHECK_STR(r.source_name, "basic.exfat");
    MP_CHECK(r.kind == MP_SRC_EXFAT);
    mp_report_free(&r);
}

int main(void) {
    MP_RUN(full_is_exact);
    MP_RUN(threads_independent);
    MP_RUN(only_loose_game);
    MP_RUN(keep_loose_moves_files);
    MP_RUN(small_game_full_sample);
    MP_TEST_MAIN_END();
}
