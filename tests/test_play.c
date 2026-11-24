// SPDX-License-Identifier: GPL-3.0-or-later
// mp_play: preparar un .p5z para jugarlo sin instalarlo.
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "mp_hash.h"
#include "mp_fixture.h"
#include "mp_play.h"

#define TMP MP_TEST_TMP "/play"
#define P5Z TMP "/g.p5z"
#define REF TMP "/ref"

static int is_pak(const char *rel) {
    size_t n = strlen(rel);
    return !strncmp(rel, "ampr_zstd-", 10) && n > 14 && !strcmp(rel + n - 4, ".pak");
}

// Empaqueta basic.exfat a P5Z y saca la carpeta de referencia a REF.
static int prepare(void) {
    mp_err e = {{0}};
    if (mpt_pack(MPT_BASIC, P5Z, 1, NULL) != 0) return -1;
    mpt_clear(REF);
    mp_p5z *p = NULL;
    if (mp_p5z_open(P5Z, &p, &e) != 0) {
        MP_FAIL_("p5z_open: %s", e.msg);
        return -1;
    }
    int rc = mp_unpack_folder(p, REF, 2, 0, &e);
    if (rc) MP_FAIL_("unpack: %s", e.msg);
    mp_p5z_close(p);
    return rc;
}

static mp_play *open_play(const char *path) {
    char err[256] = {0};
    mp_play *p = mp_play_open(path, err, sizeof err);
    if (!p) MP_FAIL_("play_open: %s", err);
    return p;
}

MP_TEST(play_json_matches_folder) {
    if (prepare() != 0) return;
    mp_play *p = open_play(P5Z);
    if (!p) return;
    char *j = mp_play_json(p);
    MP_CHECK(j != NULL);
    if (!j) {
        mp_play_close(p);
        return;
    }
    MP_CHECK_CONTAINS(j, "\"title_id\":\"TEST00001\"");
    MP_CHECK_CONTAINS(j, "\"version\":\"01.000.000\"");

    // archive_id: bytes 24..40 de la cabecera, en hex minúsculas.
    size_t hl = 0;
    uint8_t *h = mpt_slurp(P5Z, &hl);
    char want[128] = "\"archive_id\":\"";
    for (int i = 0; h && i < 16; i++) sprintf(want + strlen(want), "%02x", h[24 + i]);
    strcat(want, "\"");
    MP_CHECK_CONTAINS(j, want);
    sprintf(want, "\"size\":%llu,", (unsigned long long)hl);
    MP_CHECK_CONTAINS(j, want);
    free(h);

    // un volumen: id 0, su sitio dentro del .p5z y su tamaño en la carpeta
    mp_err e = {{0}};
    mp_p5z *z = NULL;
    MP_CHECK(mp_p5z_open(P5Z, &z, &e) == 0);
    uint64_t pak = 0;
    MP_CHECK(mp_stat(REF "/ampr_zstd-000.pak", &pak, NULL) == 0);
    char vol[200];
    sprintf(vol, "\"volumes\":[{\"id\":0,\"offset\":%llu,\"length\":%llu}]",
            (unsigned long long)(z ? mp_p5z_volume_offset(z, 0) : 0), (unsigned long long)pak);
    MP_CHECK_CONTAINS(j, vol);
    mp_p5z_close(z);

    // sueltos: la carpeta menos los .pak
    mpt_list l = mpt_list_dir(REF);
    uint64_t total = 0;
    size_t nloose = 0;
    for (size_t i = 0; i < l.n; i++) {
        if (is_pak(l.v[i])) continue;
        nloose++;
        total += l.size[i];
        char want_path[1200];
        sprintf(want_path, "{\"path\":\"%s\",\"size\":%llu}", l.v[i], (unsigned long long)l.size[i]);
        MP_CHECK_CONTAINS(j, want_path);
    }
    char lb[64];
    sprintf(lb, "\"loose_bytes\":%llu,", (unsigned long long)total);
    MP_CHECK_CONTAINS(j, lb);
    size_t seen = 0;
    for (const char *s = j; (s = strstr(s, "{\"path\":")) != NULL; s++) seen++;
    MP_CHECK_EQ_U64(seen, nloose);
    mpt_list_free(&l);
    mp_play_free_str(j);
    mp_play_close(p);
}

MP_TEST(play_extract_matches_folder) {
    if (prepare() != 0) return;
    mp_play *p = open_play(P5Z);
    if (!p) return;
    mpt_clear(TMP "/x");
    mpt_list l = mpt_list_dir(REF);
    for (size_t i = 0; i < l.n; i++) {
        if (is_pak(l.v[i])) continue;
        char dest[2048], ref[2048], err[256] = {0};
        snprintf(dest, sizeof dest, TMP "/x/%s", l.v[i]);
        snprintf(ref, sizeof ref, REF "/%s", l.v[i]);
        MP_CHECK(mp_play_extract(p, l.v[i], dest, err, sizeof err) == 0);
        if (err[0]) MP_FAIL_("%s: %s", l.v[i], err);
        size_t la = 0, lb = 0;
        uint8_t *a = mpt_slurp(dest, &la), *b = mpt_slurp(ref, &lb);
        int64_t ma = 0, mb = 0;
        mp_stat(dest, NULL, &ma);
        mp_stat(ref, NULL, &mb);
        if (!a || !b || la != lb || memcmp(a, b, la)) MP_FAIL_("distinto: %s", l.v[i]);
        if (ma != mb) MP_FAIL_("mtime distinto: %s", l.v[i]);
        free(a);
        free(b);
    }
    mpt_list_free(&l);
    mp_play_close(p);
}

MP_TEST(play_extract_replaces_existing) {
    if (prepare() != 0) return;
    mp_play *p = open_play(P5Z);
    if (!p) return;
    mpt_list l = mpt_list_dir(REF);
    const char *rel = NULL;
    for (size_t i = 0; i < l.n && !rel; i++)
        if (!is_pak(l.v[i]) && l.size[i] > 0) rel = l.v[i];
    MP_CHECK(rel != NULL);
    if (rel) {
        char dest[2048], ref[2048], err[256] = {0};
        snprintf(dest, sizeof dest, TMP "/rep/%s", rel);
        snprintf(ref, sizeof ref, REF "/%s", rel);
        mpt_clear(TMP "/rep");
        mpt_put_text(dest, "otra cosa que no es el archivo, y bastante más larga que él mismo");
        MP_CHECK(mp_play_extract(p, rel, dest, err, sizeof err) == 0);
        size_t la = 0, lb = 0;
        uint8_t *a = mpt_slurp(dest, &la), *b = mpt_slurp(ref, &lb);
        MP_CHECK(a && b && la == lb && memcmp(a, b, la) == 0);
        free(a);
        free(b);
    }
    mpt_list_free(&l);
    mp_play_close(p);
}

MP_TEST(play_extract_refuses_non_loose) {
    if (prepare() != 0) return;
    mp_play *p = open_play(P5Z);
    if (!p) return;
    const char *bad[] = {"ampr_zstd-000.pak", "no/existe", "../eboot.bin"};
    for (size_t i = 0; i < 3; i++) {
        char err[256] = {0};
        mpt_clear(TMP "/no");
        MP_CHECK(mp_play_extract(p, bad[i], TMP "/no/dest.bin", err, sizeof err) == -1);
        MP_CHECK(err[0] != 0);
        MP_CHECK(mp_path_kind(TMP "/no") == 0);
        MP_CHECK(mp_path_kind(TMP "/no/dest.bin") == 0);
    }
    mp_play_close(p);
}

static void put32(uint8_t *p, uint32_t v) {
    for (int i = 0; i < 4; i++) p[i] = (uint8_t)(v >> (8 * i));
}

static void expect_refused(const char *path, const char *what) {
    char err[256] = {0};
    mp_play *p = mp_play_open(path, err, sizeof err);
    if (p) {
        MP_FAIL_("%s: se abrió", what);
        mp_play_close(p);
        return;
    }
    if (!err[0]) MP_FAIL_("%s: sin motivo", what);
}

MP_TEST(play_open_refuses_damaged) {
    if (prepare() != 0) return;
    size_t n = 0;
    uint8_t *b = mpt_slurp(P5Z, &n);
    if (!b) return;

    // cortado en 1 byte
    mpt_put(TMP "/cut.p5z", b, n - 1);
    expect_refused(TMP "/cut.p5z", "cortado");

    // un byte de la cabecera del volumen
    mp_err e = {{0}};
    mp_p5z *z = NULL;
    MP_CHECK(mp_p5z_open(P5Z, &z, &e) == 0);
    if (z) {
        uint64_t off = mp_p5z_volume_offset(z, 0);
        mp_p5z_close(z);
        uint8_t *c = malloc(n);
        memcpy(c, b, n);
        c[off + 30] ^= 0x55;
        mpt_put(TMP "/vol.p5z", c, n);
        expect_refused(TMP "/vol.p5z", "cabecera de volumen");
        free(c);
    }

    // otro archive_id en la cabecera del .p5z, con su CRC recalculado
    uint8_t *d = malloc(n);
    memcpy(d, b, n);
    d[24] ^= 0xff;
    uint8_t hz[64];
    memcpy(hz, d, 64);
    memset(hz + 60, 0, 4);
    put32(d + 60, mp_crc32(hz, 64, 0));
    mpt_put(TMP "/aid.p5z", d, n);
    expect_refused(TMP "/aid.p5z", "archive_id");
    free(d);
    free(b);
}

int main(void) {
    mpt_clear(TMP);
    MP_RUN(play_json_matches_folder);
    MP_RUN(play_extract_matches_folder);
    MP_RUN(play_extract_replaces_existing);
    MP_RUN(play_extract_refuses_non_loose);
    MP_RUN(play_open_refuses_damaged);
    MP_TEST_MAIN_END();
}
