// SPDX-License-Identifier: GPL-3.0-or-later
#include <stdlib.h>

#include "mp_analyze.h"
#include "mp_platform.h"
#include "mp_rules.h"
#include "mp_sample.h"
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
        (void)raw;
        total += stored;   // sin regla del 1 %: cada bloque, comprimido solo si mejora (como Forge)
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

// El camino que usan todos los juegos reales: muestra < 100 %, escalado por estratos y, para
// estratos sin ninguna ventana muestreada, la proporción global.
MP_TEST(sampled_path_estimates) {
    mp_analyze_opts o = quiet(1, 0), o4 = quiet(4, 0), of = quiet(4, 1);
    o.sample_min = o4.sample_min = 0;
    o.sample_fraction = o4.sample_fraction = 0.3;
    o.dedup = o4.dedup = of.dedup = 0;
    mp_report a, b, f;
    if (run(&o, &a) != 0 || run(&o4, &b) != 0 || run(&of, &f) != 0) return;
    MP_CHECK(a.sample_rate < 1.0);
    MP_CHECK(a.sample_bytes < a.packable_bytes);
    for (int c = 0; c < MP_CFG_COUNT; c++) MP_CHECK_EQ_U64(a.est_packed[c], b.est_packed[c]);
    double est = (double)a.est_packed[MP_CFG_V1_L19], exact = (double)f.est_packed[MP_CFG_V1_L19];
    fprintf(stderr, "  muestra %.1f %%: estimado %.0f, exacto %.0f (%+.1f %%)\n",
            100.0 * a.sample_rate, est, exact, 100.0 * (est - exact) / exact);
    // Con un fixture tan pequeño y variado el error es grande; en juegos reales hay miles de
    // ventanas por estrato. Lo que se fija aquí es que el camino funciona y no se desboca.
    MP_CHECK(est > 0.75 * exact && est < 1.25 * exact);

    // El estrato ".dat" (un solo archivo de 1 byte) debe quedar sin muestrear, para que se use la
    // proporción global. La selección es determinista, así que esto no cambia entre ejecuciones.
    mp_source *s = NULL;
    mp_err e = {{0}};
    MP_CHECK(mp_source_open(BASIC, &s, &e) == 0);
    if (!s) return;
    mp_rules *rules = mp_rules_new();
    uint64_t sizes[512];
    size_t np = 0, dat = (size_t)-1;
    for (size_t i = 0; i < mp_source_count(s); i++) {
        const mp_entry *en = mp_source_entry(s, i);
        if (mp_rules_classify(rules, en->path, en->size) != MP_PACKABLE) continue;
        if (strstr(en->path, ".dat")) dat = np;
        sizes[np++] = en->size;
    }
    mp_sample smp = {0};
    MP_CHECK(mp_sample_select(sizes, np, 0.3, 0, 0, &smp, NULL) == 0);
    int dat_sampled = 0;
    for (size_t k = 0; k < smp.n; k++) dat_sampled |= smp.w[k].file == dat;
    MP_CHECK(dat != (size_t)-1 && !dat_sampled);
    mp_sample_free(&smp);
    mp_rules_free(rules);
    mp_source_close(s);
    mp_report_free(&a);
    mp_report_free(&b);
    mp_report_free(&f);
}

// Un archivo que ahorra menos del 1 % ya no va entero en bruto: su bloque comprimible se comprime.
MP_TEST(no_store_only_rule) {
    mp_err e = {{0}};
    const char *dir = MP_TEST_TMP "/analyze/casi";
    MP_CHECK(mp_make_dirs(dir, &e) == 0);
    size_t n = 121u * MP_BLOCK_V1;
    uint8_t *d = malloc(n);
    for (size_t i = 0; i < 120u * MP_BLOCK_V1; i += 8) {
        uint64_t v = mp_splitmix64(i);
        memcpy(d + i, &v, 8);
    }
    memset(d + 120u * MP_BLOCK_V1, 0, MP_BLOCK_V1);
    MP_CHECK(mp_write_file(MP_TEST_TMP "/analyze/casi/casi.bin", d, n, &e) == 0);
    free(d);
    mp_analyze_opts o = quiet(8, 1);
    o.dedup = 0;
    o.transport = 0;
    mp_report r;
    memset(&r, 0, sizeof r);
    MP_CHECK(mp_analyze(dir, &o, &r, &e) == 0);
    MP_CHECK(r.est_packed[MP_CFG_V1_L19] < n - MP_BLOCK_V1 / 2);
    mp_report_free(&r);
}

// Transporte calculado aquí: cada archivo empaquetable en frames de 32 MiB con zstd 19 y
// ventana larga; un frame que no mejora va en bruto.
static uint64_t direct_transport(void) {
    mp_source *s = NULL;
    mp_err e = {{0}};
    if (mp_source_open(BASIC, &s, &e) != 0) return 0;
    mp_rules *rules = mp_rules_new();
    ZSTD_CCtx *c = ZSTD_createCCtx();
    mp_zstd_set_long(c);
    size_t cap = ZSTD_compressBound(MP_LONG_FRAME);
    uint8_t *buf = malloc(MP_LONG_FRAME), *out = malloc(cap);
    uint64_t total = 0;
    for (size_t i = 0; i < mp_source_count(s); i++) {
        const mp_entry *en = mp_source_entry(s, i);
        if (mp_rules_classify(rules, en->path, en->size) != MP_PACKABLE) continue;
        for (uint64_t off = 0; off < en->size; off += MP_LONG_FRAME) {
            size_t n = en->size - off < MP_LONG_FRAME ? (size_t)(en->size - off) : MP_LONG_FRAME;
            mp_source_read(s, i, off, buf, n, &e);
            size_t f = ZSTD_compress2(c, out, cap, buf, n);
            total += !ZSTD_isError(f) && f < n ? f : n;
        }
    }
    free(buf);
    free(out);
    ZSTD_freeCCtx(c);
    mp_rules_free(rules);
    mp_source_close(s);
    return total;
}

MP_TEST(transport_in_report) {
    mp_analyze_opts o = quiet(2, 1);
    o.dedup = 0;
    mp_report r;
    if (run(&o, &r) != 0) return;
    MP_CHECK(r.xport_done);
    MP_CHECK_EQ_U64(r.xport.est_packed, direct_transport());
    // dupblocks.bin repite 3 veces el mismo bloque aleatorio: v1 no lo ve, la ventana larga sí.
    MP_CHECK(r.xport.est_packed < r.est_packed[MP_CFG_V1_L19]);
    MP_CHECK_EQ_U64(r.p5z_transport, r.xport.est_packed + r.loose_compressed);
    mp_report_free(&r);
    o.transport = 0;
    if (run(&o, &r) != 0) return;
    MP_CHECK(!r.xport_done);
    MP_CHECK_EQ_U64(r.p5z_transport, 0);
    mp_report_free(&r);
}

MP_TEST(transport_threads_independent) {
    mp_analyze_opts o1 = quiet(1, 0), o4 = quiet(4, 0);
    o1.sample_min = o4.sample_min = 0;
    o1.sample_fraction = o4.sample_fraction = 0.3;
    o1.dedup = o4.dedup = 0;
    mp_report a, b;
    if (run(&o1, &a) != 0 || run(&o4, &b) != 0) return;
    MP_CHECK(a.xport.rate < 1.0);
    MP_CHECK_EQ_U64(a.xport.est_packed, b.xport.est_packed);
    MP_CHECK_EQ_U64(a.xport.sample_stored, b.xport.sample_stored);
    mp_report_free(&a);
    mp_report_free(&b);
}

int main(void) {
    MP_RUN(full_is_exact);
    MP_RUN(threads_independent);
    MP_RUN(only_loose_game);
    MP_RUN(keep_loose_moves_files);
    MP_RUN(small_game_full_sample);
    MP_RUN(sampled_path_estimates);
    MP_RUN(no_store_only_rule);
    MP_RUN(transport_in_report);
    MP_RUN(transport_threads_independent);
    MP_TEST_MAIN_END();
}
