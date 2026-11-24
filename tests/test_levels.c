// SPDX-License-Identifier: GPL-3.0-or-later
// analyze --levels: a sample of the game packed at each level with the pack's own pipeline.
#include <stdlib.h>

#include "mp_analyze.h"
#include "mp_fixture.h"
#include "mp_levels.h"

#define TMP MP_TEST_TMP "/levels"

static mp_levels_opts quiet(unsigned threads, int full) {
    mp_levels_opts o;
    mp_levels_opts_default(&o);
    o.threads = threads;
    o.full = full;
    o.progress = 0;
    return o;
}

static int run(const char *src, const mp_levels_opts *o, mp_levels_report *r) {
    mp_err e = {{0}};
    int rc = mp_levels_run(src, o, r, &e);
    if (rc != 0) MP_FAIL_("mp_levels_run: %s", e.msg);
    return rc;
}

// What the packable files of the game take at a level, block by block, without the pipeline.
static uint64_t direct(const char *src, int level) {
    mp_source *s = NULL;
    mp_err e = {{0}};
    if (mp_source_open(src, &s, &e) != 0) return 0;
    mp_rules *rules = mp_rules_new();
    ZSTD_CCtx *c = ZSTD_createCCtx();
    size_t cap = ZSTD_compressBound(MP_BLOCK_V1);
    uint8_t *buf = malloc(MP_BLOCK_V1), *out = malloc(cap);
    uint64_t total = 0;
    for (size_t i = 0; i < mp_source_count(s); i++) {
        const mp_entry *en = mp_source_entry(s, i);
        if (mp_rules_classify(rules, en->path, en->size) != MP_PACKABLE) continue;
        for (uint64_t off = 0; off < en->size; off += MP_BLOCK_V1) {
            size_t n = en->size - off < MP_BLOCK_V1 ? (size_t)(en->size - off) : MP_BLOCK_V1;
            mp_source_read(s, i, off, buf, n, &e);
            size_t f = mp_zstd_block(c, level, 18, buf, n, out, cap);
            total += f && f < n ? f : n;
        }
    }
    free(buf);
    free(out);
    ZSTD_freeCCtx(c);
    mp_rules_free(rules);
    mp_source_close(s);
    return total;
}

// The index bytes analyze adds for a pack of the packable files of src with packed bytes.
static uint64_t index_bytes(const char *src, uint64_t packed) {
    mp_source *s = NULL;
    mp_err e = {{0}};
    if (mp_source_open(src, &s, &e) != 0) return 0;
    mp_rules *rules = mp_rules_new();
    uint32_t *packable = malloc((mp_source_count(s) + 1) * sizeof *packable);
    size_t n = 0;
    for (size_t i = 0; i < mp_source_count(s); i++) {
        const mp_entry *en = mp_source_entry(s, i);
        if (mp_rules_classify(rules, en->path, en->size) == MP_PACKABLE) packable[n++] = (uint32_t)i;
    }
    uint64_t b = mp_v1_index_bytes(s, packable, n, packed);
    free(packable);
    mp_rules_free(rules);
    mp_source_close(s);
    return b;
}

MP_TEST(levels_parse) {
    mp_levels_opts o;
    mp_levels_opts_default(&o);
    MP_CHECK_EQ_U64(o.nlevels, 4);
    MP_CHECK(o.levels[0] == 3 && o.levels[1] == 9 && o.levels[2] == 15 && o.levels[3] == 19);
    mp_err e = {{0}};
    MP_CHECK(mp_levels_parse("1,6,12,19", &o, &e) == 0);
    MP_CHECK_EQ_U64(o.nlevels, 4);
    MP_CHECK(o.levels[0] == 1 && o.levels[1] == 6 && o.levels[2] == 12 && o.levels[3] == 19);
    // in increasing order, each once
    MP_CHECK(mp_levels_parse("19,3,3,22", &o, &e) == 0);
    MP_CHECK_EQ_U64(o.nlevels, 3);
    MP_CHECK(o.levels[0] == 3 && o.levels[1] == 19 && o.levels[2] == 22);
    MP_CHECK(mp_levels_parse("7", &o, &e) == 0 && o.nlevels == 1 && o.levels[0] == 7);
    static const char *const bad[] = {"", "0", "23", "a", "3,,9", "3,9,", ",3", "3;9", "-1", "3 9", "019x"};
    for (size_t i = 0; i < sizeof bad / sizeof *bad; i++) {
        mp_levels_opts_default(&o);
        e.msg[0] = 0;
        if (mp_levels_parse(bad[i], &o, &e) == 0) MP_FAIL_("\"%s\" is taken", bad[i]);
        MP_CHECK(e.msg[0] != 0);
        MP_CHECK_EQ_U64(o.nlevels, 4);   // a bad list leaves the levels as they were
    }
}

// The whole game measured: what the packs take at each level is exactly what packing it gives, and
// the .p5z and the folder add the indices and the loose files (compressed in the .p5z, as they are
// in the folder).
MP_TEST(levels_full_is_exact) {
    mp_levels_opts o = quiet(2, 1);
    mp_err e = {{0}};
    MP_CHECK(mp_levels_parse("3,19", &o, &e) == 0);
    mp_levels_report r;
    if (run(MPT_BASIC, &o, &r) != 0) return;
    MP_CHECK_EQ_U64(r.nlevels, 2);
    MP_CHECK(r.sample_rate == 1.0 && r.loose_rate == 1.0);
    MP_CHECK_EQ_U64(r.sample_bytes, r.packable_bytes);
    MP_CHECK_EQ_U64(r.loose_bytes, 10200);
    MP_CHECK_EQ_U64(r.loose_files, 3);
    MP_CHECK_EQ_U64(r.original_size, r.loose_bytes + r.packable_bytes);
    MP_CHECK_EQ_U64(r.threads, 2);
    mp_analyze_opts ao;
    mp_analyze_opts_default(&ao);
    ao.threads = 2;
    ao.full = 1;
    ao.progress = 0;
    ao.dedup = 0;
    ao.transport = 0;
    mp_report a;
    MP_CHECK(mp_analyze(MPT_BASIC, &ao, &a, &e) == 0);
    MP_CHECK_EQ_U64(r.loose_compressed, a.loose_compressed);
    mp_report_free(&a);
    for (size_t i = 0; i < r.nlevels; i++) {
        const mp_level_row *row = &r.rows[i];
        MP_CHECK_EQ_U64(row->packed, direct(MPT_BASIC, row->level));
        uint64_t idx = index_bytes(MPT_BASIC, row->packed);
        MP_CHECK_EQ_U64(row->p5z_size, row->packed + idx + r.loose_compressed);
        MP_CHECK_EQ_U64(row->folder_size, row->packed + idx + r.loose_bytes);
    }
    MP_CHECK(r.rows[0].level == 3 && r.rows[1].level == 19);
    MP_CHECK(r.rows[1].packed < r.rows[0].packed);
}

// A sample that is not the whole game: level 19 gives what analyze estimates for v1-l19 with the
// same sample (the same windows, compressed the same way, extrapolated by the same strata).
MP_TEST(levels_match_analyze_on_a_sample) {
    mp_levels_opts o = quiet(3, 0);
    o.sample_fraction = 0.3;
    o.sample_min = 256 << 10;
    mp_err e = {{0}};
    MP_CHECK(mp_levels_parse("19", &o, &e) == 0);
    mp_levels_report r;
    if (run(MPT_BASIC, &o, &r) != 0) return;
    MP_CHECK(r.sample_rate < 1.0);
    MP_CHECK(r.sample_bytes < r.packable_bytes);
    mp_analyze_opts ao;
    mp_analyze_opts_default(&ao);
    ao.threads = 3;
    ao.progress = 0;
    ao.dedup = 0;
    ao.transport = 0;
    ao.sample_fraction = 0.3;
    ao.sample_min = 256 << 10;
    mp_report a;
    MP_CHECK(mp_analyze(MPT_BASIC, &ao, &a, &e) == 0);
    MP_CHECK_EQ_U64(r.sample_bytes, a.sample_bytes);
    MP_CHECK_EQ_U64(r.rows[0].packed, a.est_packed[MP_CFG_V1_L19]);
    MP_CHECK_EQ_U64(r.rows[0].p5z_size, a.p5z_estimate);
    mp_report_free(&a);
}

MP_TEST(levels_threads_independent) {
    for (int full = 0; full <= 1; full++) {
        mp_levels_opts o1 = quiet(1, full), o4 = quiet(4, full);
        o1.sample_min = o4.sample_min = 512 << 10;
        mp_levels_report a, b;
        if (run(MPT_BASIC, &o1, &a) != 0 || run(MPT_BASIC, &o4, &b) != 0) return;
        MP_CHECK_EQ_U64(a.nlevels, b.nlevels);
        MP_CHECK_EQ_U64(a.loose_compressed, b.loose_compressed);
        for (size_t i = 0; i < a.nlevels; i++) {
            MP_CHECK_EQ_U64(a.rows[i].packed, b.rows[i].packed);
            MP_CHECK_EQ_U64(a.rows[i].p5z_size, b.rows[i].p5z_size);
            MP_CHECK_EQ_U64(a.rows[i].folder_size, b.rows[i].folder_size);
        }
    }
}

typedef struct {
    int calls, levels[MP_LEVELS_MAX];
    uint64_t done, total;
    int events, backwards, total_changed, over;
} seen;

static void on_level(void *ctx, const mp_level_row *row) {
    seen *s = ctx;
    if (s->calls < MP_LEVELS_MAX) s->levels[s->calls] = row->level;
    s->calls++;
}

static void on_progress(void *ctx, const char *phase, uint64_t done, uint64_t total) {
    (void)phase;
    seen *s = ctx;
    if (s->events && done < s->done) s->backwards = 1;
    if (s->events && total != s->total) s->total_changed = 1;
    if (done > total) s->over = 1;
    s->done = done;
    s->total = total;
    s->events++;
}

// Each level is told as it is done, in increasing order, with a speed and a time; the progress
// never goes back, keeps its total and ends at it.
MP_TEST(levels_rows_and_progress) {
    seen s;
    memset(&s, 0, sizeof s);
    mp_levels_opts o = quiet(2, 0);
    o.on_level = on_level;
    o.ctx = &s;
    mp_progress_hook h = {on_progress, &s};
    mp_progress_set_hook(&h);
    mp_levels_report r;
    int rc = run(MPT_BASIC, &o, &r);
    mp_progress_set_hook(NULL);
    if (rc != 0) return;
    MP_CHECK_EQ_U64(s.calls, 4);
    MP_CHECK(s.levels[0] == 3 && s.levels[1] == 9 && s.levels[2] == 15 && s.levels[3] == 19);
    MP_CHECK(s.events > 0 && !s.backwards && !s.total_changed && !s.over);
    MP_CHECK(s.total > 0 && s.done == s.total);
    MP_CHECK(r.read_rate > 0);
    for (size_t i = 0; i < r.nlevels; i++) {
        MP_CHECK(r.rows[i].rate > 0);
        MP_CHECK(r.rows[i].p5z_seconds >= 1 && r.rows[i].folder_seconds >= 1);
        MP_CHECK(r.rows[i].p5z_size > 0 && r.rows[i].p5z_size < r.original_size);
        if (i) MP_CHECK(r.rows[i].packed <= r.rows[i - 1].packed);
    }
}

MP_TEST(levels_cancel) {
    mp_levels_opts o = quiet(2, 1);
    mp_levels_report r;
    mp_err e = {{0}};
    mp_cancel_request();
    int rc = mp_levels_run(MPT_BASIC, &o, &r, &e);
    mp_cancel_reset();
    MP_CHECK(rc != 0);
    MP_CHECK_STR(e.msg, "cancelled");
}

// A game with only loose files: nothing to pack, every level the same.
MP_TEST(levels_only_loose) {
    mpt_clear(TMP "/onlyloose");
    mpt_put_text(TMP "/onlyloose/sce_sys/param.json", "{\"titleId\":\"LOOS00001\"}");
    uint8_t eboot[40000];
    for (size_t i = 0; i < sizeof eboot; i++) eboot[i] = (uint8_t)(i % 251 * (i % 7));
    mpt_put(TMP "/onlyloose/eboot.bin", eboot, sizeof eboot);
    mp_levels_opts o = quiet(2, 0);
    mp_levels_report r;
    if (run(TMP "/onlyloose", &o, &r) != 0) return;
    MP_CHECK_EQ_U64(r.packable_bytes, 0);
    MP_CHECK_EQ_U64(r.sample_bytes, 0);
    MP_CHECK(r.loose_compressed > 0 && r.loose_compressed < r.loose_bytes);
    for (size_t i = 0; i < r.nlevels; i++) {
        MP_CHECK_EQ_U64(r.rows[i].packed, 0);
        MP_CHECK_EQ_U64(r.rows[i].p5z_size, r.rows[0].p5z_size);
        MP_CHECK(r.rows[i].p5z_size >= r.loose_compressed);
    }
    mpt_clear(TMP "/empty");
    mp_err e = {{0}};
    MP_CHECK(mp_make_dirs(TMP "/empty", &e) == 0);
    MP_CHECK(mp_levels_run(TMP "/empty", &o, &r, &e) != 0 && e.msg[0]);
}

// Nothing is written to the game, nor next to it.
MP_TEST(levels_writes_nothing) {
    mpt_clear(TMP "/game");
    mpt_put_text(TMP "/game/sce_sys/param.json", "{\"titleId\":\"READ00001\"}");
    uint8_t data[300000];
    for (size_t i = 0; i < sizeof data; i++) data[i] = (uint8_t)((i * 31) ^ (i >> 9));
    mpt_put(TMP "/game/data/a.bin", data, sizeof data);
    mpt_put(TMP "/game/data/b.bin", data, 1000);
    mpt_list before = mpt_list_dir(TMP), inside = mpt_list_dir(TMP "/game/data");
    mp_levels_opts o = quiet(2, 0);
    mp_levels_report r;
    run(TMP "/game", &o, &r);
    mpt_list after = mpt_list_dir(TMP), inside2 = mpt_list_dir(TMP "/game/data");
    MP_CHECK_EQ_U64(before.n, after.n);
    MP_CHECK_EQ_U64(inside.n, inside2.n);
    MP_CHECK_EQ_U64(mpt_dir_size(TMP "/game"), sizeof data + 1000 + strlen("{\"titleId\":\"READ00001\"}"));
    mpt_list_free(&before);
    mpt_list_free(&after);
    mpt_list_free(&inside);
    mpt_list_free(&inside2);
}

int main(void) {
    MP_RUN(levels_parse);
    MP_RUN(levels_full_is_exact);
    MP_RUN(levels_match_analyze_on_a_sample);
    MP_RUN(levels_threads_independent);
    MP_RUN(levels_rows_and_progress);
    MP_RUN(levels_cancel);
    MP_RUN(levels_only_loose);
    MP_RUN(levels_writes_nothing);
    MP_TEST_MAIN_END();
}
