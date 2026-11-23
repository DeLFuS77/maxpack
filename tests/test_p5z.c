// SPDX-License-Identifier: GPL-3.0-or-later
#include <stdio.h>
#include <stdlib.h>
#include <zstd.h>

#include "mp_hash.h"
#include "mp_p5z.h"
#include "mp_pack.h"
#include "mp_platform.h"
#include "mp_test.h"

#define TMP MP_TEST_TMP "/p5z"
#define BASIC MP_FIXTURES_DIR "/basic.exfat"

static uint32_t rd32(const uint8_t *p) { return p[0] | p[1] << 8 | p[2] << 16 | (uint32_t)p[3] << 24; }
static uint64_t rd64(const uint8_t *p) { return rd32(p) | (uint64_t)rd32(p + 4) << 32; }

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

static int pack_p5z(const char *path, unsigned threads, mp_pack_result *r) {
    mp_err e = {{0}};
    remove(path);
    mp_make_dirs(TMP, &e);
    mp_pack_opts o;
    mp_pack_opts_default(&o);
    o.threads = threads;
    o.progress = 0;
    mp_pack_out out;
    if (mp_pack_out_p5z(path, threads, &out, &e) != 0) {
        MP_FAIL_("p5z: %s", e.msg);
        return -1;
    }
    int rc = mp_pack_run(BASIC, &out, &o, r, &e);
    out.destroy(out.ctx);
    if (rc) MP_FAIL_("pack: %s", e.msg);
    return rc;
}

MP_TEST(p5z_structure) {
    mp_pack_result r;
    if (pack_p5z(TMP "/basic.p5z", 4, &r) != 0) return;
    size_t n = 0;
    uint8_t *d = slurp(TMP "/basic.p5z", &n);
    MP_CHECK(d && n > 96);
    if (!d) return;
    // Cabecera.
    MP_CHECK(memcmp(d, "MAXPKP5Z", 8) == 0 && rd32(d + 8) == 1 && rd32(d + 12) == 64);
    uint8_t h[64];
    memcpy(h, d, 64);
    memset(h + 60, 0, 4);
    MP_CHECK_EQ_U64(mp_crc32(h, 64, 0), rd32(d + 60));
    MP_CHECK(d[20] == 19);
    // Pie e índice.
    const uint8_t *f = d + n - 32;
    MP_CHECK(memcmp(f, "P5ZFOOT1", 8) == 0);
    MP_CHECK_EQ_U64(mp_crc32(f, 28, 0), rd32(f + 28));
    uint64_t ioff = rd64(f + 8), ilen = rd64(f + 16);
    MP_CHECK(ioff + ilen + 32 == n);
    MP_CHECK_EQ_U64(mp_crc32(d + ioff, (size_t)ilen, 0), rd32(f + 24));
    unsigned long long usize = ZSTD_getFrameContentSize(d + ioff, (size_t)ilen);
    MP_CHECK(usize != ZSTD_CONTENTSIZE_ERROR && usize != ZSTD_CONTENTSIZE_UNKNOWN);
    uint8_t *ix = malloc((size_t)usize);
    MP_CHECK(ZSTD_decompress(ix, (size_t)usize, d + ioff, (size_t)ilen) == usize);
    MP_CHECK(memcmp(ix, "P5ZIDX01", 8) == 0);
    uint32_t nfiles = rd32(ix + 12), nframes = rd32(ix + 16), nman = rd32(ix + 20);
    MP_CHECK_EQ_U64(nman, 213);
    // Cada SHA-256 del manifiesto es el del archivo original.
    mp_source *s = NULL;
    mp_err e = {{0}};
    MP_CHECK(mp_source_open(BASIC, &s, &e) == 0);
    const uint8_t *man = ix + P5Z_INDEX_HEADER + (size_t)P5Z_FILE_REC * nfiles + (size_t)P5Z_FRAME_REC * nframes;
    size_t ok = 0;
    for (uint32_t i = 0; s && i < nman && i < mp_source_count(s); i++) {
        const mp_entry *en = mp_source_entry(s, i);
        uint8_t *b = malloc((size_t)en->size + 1), dg[32];
        mp_source_read(s, i, 0, b, (size_t)en->size, &e);
        mp_sha256 sh;
        mp_sha256_init(&sh);
        mp_sha256_update(&sh, b, (size_t)en->size);
        mp_sha256_final(&sh, dg);
        const uint8_t *m = man + (size_t)P5Z_MANIFEST_REC * i;
        ok += !memcmp(dg, m + 32, 32) && rd64(m + 16) == en->size;
        free(b);
    }
    MP_CHECK_EQ_U64(ok, 213);
    mp_source_close(s);
    free(ix);
    free(d);
}

MP_TEST(p5z_threads_identical) {
    mp_pack_result r;
    if (pack_p5z(TMP "/t1.p5z", 1, &r) != 0 || pack_p5z(TMP "/t4.p5z", 4, &r) != 0) return;
    size_t a = 0, b = 0;
    uint8_t *x = slurp(TMP "/t1.p5z", &a), *y = slurp(TMP "/t4.p5z", &b);
    MP_CHECK(x && y && a == b && !memcmp(x, y, a));
    free(x);
    free(y);
}

MP_TEST(p5z_packs_stored_raw) {
    // La región de cada volumen en el .p5z es el volumen tal cual (la carpeta da el mismo).
    mp_err e = {{0}};
    const char *dir = TMP "/folder";
    mp_pack_opts o;
    mp_pack_opts_default(&o);
    o.threads = 4;
    o.progress = 0;
    mp_pack_out out;
    if (mp_path_kind(TMP "/folder/ampr_zstd-000.pak") == 1) {
        // De una ejecución anterior: el volumen ya está; basta con compararlo.
    } else {
        MP_CHECK(mp_pack_out_dir(dir, &out, &e) == 0);
        mp_pack_result r;
        MP_CHECK(mp_pack_run(BASIC, &out, &o, &r, &e) == 0);
        out.destroy(out.ctx);
    }
    size_t vl = 0, pl = 0;
    uint8_t *vol = slurp(TMP "/folder/ampr_zstd-000.pak", &vl), *p = slurp(TMP "/basic.p5z", &pl);
    MP_CHECK(vol && p && vl > 64 && pl > 96);
    if (!vol || !p || pl <= 96) return;
    // Busca en el índice la región del volumen 000 (los sueltos van antes que los packs).
    const uint8_t *f = p + pl - 32;
    uint64_t ioff = rd64(f + 8), ilen = rd64(f + 16);
    unsigned long long usize = ZSTD_getFrameContentSize(p + ioff, (size_t)ilen);
    uint8_t *ix = malloc((size_t)usize);
    MP_CHECK(ZSTD_decompress(ix, (size_t)usize, p + ioff, (size_t)ilen) == usize);
    uint32_t nfiles = rd32(ix + 12);
    uint64_t soff = usize - rd64(ix + 24);
    int found = 0;
    for (uint32_t i = 0; i < nfiles && !found; i++) {
        const uint8_t *r = ix + P5Z_INDEX_HEADER + (size_t)P5Z_FILE_REC * i;
        uint32_t po = rd32(r), plen = rd32(r + 4);
        if (r[8] != MP_ROLE_VOLUME || plen != 17 || memcmp(ix + soff + po, "ampr_zstd-000.pak", 17)) continue;
        found = 1;
        uint64_t doff = rd64(r + 28), dlen = rd64(r + 36);
        MP_CHECK(r[9] == P5Z_STORE_RAW);
        MP_CHECK_EQ_U64(dlen, vl);
        MP_CHECK(doff + dlen <= pl && !memcmp(p + doff, vol, vl));
    }
    MP_CHECK(found);
    free(ix);
    free(vol);
    free(p);
}

// Un origen que se rechaza no deja ni rastro: el .p5z no llega a crearse.
MP_TEST(p5z_not_created_on_refusal) {
    mp_err e = {{0}};
    const char *path = TMP "/rechazado.p5z";
    remove(path);
    mp_make_dirs(TMP, &e);
    mp_pack_opts o;
    mp_pack_opts_default(&o);
    o.progress = 0;
    mp_pack_out out;
    mp_pack_result r;
    MP_CHECK(mp_pack_out_p5z(path, 2, &out, &e) == 0);
    MP_CHECK(mp_pack_run(MP_FIXTURES_DIR "/casedup.exfat", &out, &o, &r, &e) == -1);
    out.destroy(out.ctx);
    MP_CHECK(mp_path_kind(path) == 0);
}

// Un fallo a mitad (imagen truncada) borra el .p5z incompleto, que no serviría para nada.
MP_TEST(p5z_partial_removed_on_failure) {
    mp_err e = {{0}};
    const char *path = TMP "/incompleto.p5z";
    remove(path);
    mp_pack_opts o;
    mp_pack_opts_default(&o);
    o.progress = 0;
    mp_pack_out out;
    mp_pack_result r;
    MP_CHECK(mp_pack_out_p5z(path, 2, &out, &e) == 0);
    MP_CHECK(mp_pack_run(MP_FIXTURES_DIR "/truncated_tail.exfat", &out, &o, &r, &e) == -1);
    out.destroy(out.ctx);
    MP_CHECK(mp_path_kind(path) == 0);
}

// Empaqueta basic.exfat con tope; size es lo que mide el .p5z justo antes de destroy, que es lo
// más que llega a medir (el escritor nunca lo acorta), y too_big si el escritor paró por el tope.
static int pack_capped(const char *path, uint64_t cap, uint64_t *size, int *too_big) {
    mp_err e = {{0}};
    remove(path);
    mp_make_dirs(TMP, &e);
    mp_pack_opts o;
    mp_pack_opts_default(&o);
    o.threads = 2;
    o.progress = 0;
    mp_pack_out out;
    if (mp_pack_out_p5z_ex(path, 2, cap, &out, &e) != 0) {
        MP_FAIL_("p5z: %s", e.msg);
        return -2;
    }
    mp_pack_result r;
    int rc = mp_pack_run(BASIC, &out, &o, &r, &e);
    *size = 0;
    mp_file *f = NULL;
    if (mp_file_open_read(path, &f, NULL) == 0) {
        mp_file_size(f, size, NULL);
        mp_file_close(f);
    }
    *too_big = mp_pack_out_p5z_too_big(&out);
    out.destroy(out.ctx);
    return rc;
}

// Con tope, el .p5z no pasa de él en ningún momento: la escritura que lo pasaría no se hace (en
// FAT32 fallaría antes en el sistema, como "io"). Un tope del tamaño justo del .p5z se acepta.
MP_TEST(p5z_max_size_never_exceeded) {
    const char *path = TMP "/tope.p5z";
    uint64_t full = 0, size = 0;
    int too_big = 1;
    if (pack_capped(path, 0, &full, &too_big) != 0) return;
    MP_CHECK(full > 1000 && !too_big);
    // más pequeño que la cabecera, que la cabecera y un frame, y que el .p5z por un byte
    const uint64_t caps[] = {10, P5Z_HEADER, P5Z_HEADER + 200, full / 2, full - P5Z_FOOTER, full - 1};
    for (size_t i = 0; i < sizeof caps / sizeof caps[0]; i++) {
        int rc = pack_capped(path, caps[i], &size, &too_big);
        if (rc != -1 || !too_big || size > caps[i])
            MP_FAIL_("tope %llu: rc %d, too_big %d, llegó a %llu bytes", (unsigned long long)caps[i], rc, too_big,
                     (unsigned long long)size);
        MP_CHECK(mp_path_kind(path) == 0);
    }
    // justo del tamaño: cabe
    MP_CHECK(pack_capped(path, full, &size, &too_big) == 0);
    MP_CHECK_EQ_U64(size, full);
    MP_CHECK(!too_big && mp_path_kind(path) == 1);
    // otro fallo no es del tope
    mp_err e = {{0}};
    mp_pack_opts o;
    mp_pack_opts_default(&o);
    o.progress = 0;
    mp_pack_out out;
    mp_pack_result r;
    remove(TMP "/otro.p5z");
    MP_CHECK(mp_pack_out_p5z_ex(TMP "/otro.p5z", 2, full, &out, &e) == 0);
    MP_CHECK(mp_pack_run(MP_FIXTURES_DIR "/truncated_tail.exfat", &out, &o, &r, &e) == -1);
    MP_CHECK(!mp_pack_out_p5z_too_big(&out));
    out.destroy(out.ctx);
}

int main(void) {
    MP_RUN(p5z_max_size_never_exceeded);
    MP_RUN(p5z_not_created_on_refusal);
    MP_RUN(p5z_partial_removed_on_failure);
    MP_RUN(p5z_structure);
    MP_RUN(p5z_threads_identical);
    MP_RUN(p5z_packs_stored_raw);
    MP_TEST_MAIN_END();
}
