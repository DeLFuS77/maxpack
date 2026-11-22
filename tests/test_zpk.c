// SPDX-License-Identifier: GPL-3.0-or-later
#include <stdio.h>
#include <stdlib.h>

#include "mp_hash.h"
#include "mp_platform.h"
#include "mp_rules.h"
#include "mp_zpk.h"
#include "mp_test.h"

#define TMP MP_TEST_TMP "/zpk"

typedef struct {
    mp_source *src;
    uint32_t *files;
    size_t n;
} pk;

static int open_pk(pk *p) {
    mp_err e = {{0}};
    memset(p, 0, sizeof *p);
    if (mp_source_open(MP_FIXTURES_DIR "/basic.exfat", &p->src, &e) != 0) {
        MP_FAIL_("no abre basic.exfat: %s", e.msg);
        return 0;
    }
    mp_rules *r = mp_rules_new();
    p->files = malloc(mp_source_count(p->src) * sizeof *p->files);
    for (size_t i = 0; i < mp_source_count(p->src); i++) {
        const mp_entry *en = mp_source_entry(p->src, i);
        if (mp_rules_classify(r, en->path, en->size) == MP_PACKABLE) p->files[p->n++] = (uint32_t)i;
    }
    mp_rules_free(r);
    return 1;
}

static void close_pk(pk *p) {
    free(p->files);
    mp_source_close(p->src);
}

static int rm_cb(void *ctx, const char *rel, int is_dir, uint64_t size, int64_t mtime) {
    (void)size;
    (void)mtime;
    if (!is_dir) {
        char path[1024];
        snprintf(path, sizeof path, "%s/%s", (const char *)ctx, rel);
        remove(path);
    }
    return 0;
}

// Carpeta vacía (solo con archivos, sin subcarpetas) para una salida nueva.
static void fresh_dir(const char *dir) {
    mp_err e = {{0}};
    mp_make_dirs(dir, &e);
    mp_dir_walk(dir, rm_cb, (void *)dir, &e);
}

// Escribe los packs de los empaquetables de basic en dir; devuelve 0 si fue bien.
static int write_packs(pk *p, const char *dir, unsigned threads, uint64_t vol_limit, mp_zpk_stats *st,
                       uint8_t (**sha)[32]) {
    mp_err e = {{0}};
    fresh_dir(dir);
    mp_zpk_sink sink;
    if (mp_zpk_sink_dir(dir, &sink, &e) != 0) {
        MP_FAIL_("sink: %s", e.msg);
        return -1;
    }
    mp_zpk_opts o = {19, threads, vol_limit, NULL};
    uint8_t *index = NULL;
    size_t len = 0;
    *sha = malloc(p->n * sizeof **sha);
    int rc = mp_zpk_write(p->src, p->files, p->n, &o, &sink, &index, &len, *sha, st, &e);
    mp_zpk_sink_free(&sink);
    if (rc != 0) {
        MP_FAIL_("mp_zpk_write: %s", e.msg);
        return -1;
    }
    char ip[1024];
    snprintf(ip, sizeof ip, "%s/ampr_zstd.index", dir);
    rc = mp_write_file(ip, index, len, &e);
    free(index);
    return rc;
}

static uint8_t *slurp(const char *path, size_t *len) {
    mp_file *f = NULL;
    uint64_t size = 0;
    size_t got = 0;
    if (mp_file_open_read(path, &f, NULL) != 0) return NULL;
    mp_file_size(f, &size, NULL);
    uint8_t *b = malloc((size_t)size + 1);
    mp_file_pread(f, b, (size_t)size, 0, &got, NULL);
    mp_file_close(f);
    *len = got;
    return b;
}

static mp_zpk *open_dir_zpk(const char *dir, mp_zpk_volsrc *vs, uint8_t **index, mp_err *e) {
    char ip[1024];
    size_t len = 0;
    snprintf(ip, sizeof ip, "%s/ampr_zstd.index", dir);
    *index = slurp(ip, &len);
    if (!*index || mp_zpk_volsrc_dir(dir, vs, e) != 0) return NULL;
    mp_zpk *z = NULL;
    if (mp_zpk_open(*index, len, vs, &z, e) != 0) return NULL;
    return z;
}

static int same_as_source(pk *p, mp_zpk *z, mp_err *e) {
    size_t bad = 0;
    for (size_t i = 0; i < mp_zpk_count(z); i++) {
        const char *path = mp_zpk_path(z, i);
        size_t s = 0;
        while (s < mp_source_count(p->src) && strcmp(mp_source_entry(p->src, s)->path, path)) s++;
        uint64_t size = mp_zpk_size(z, i);
        uint8_t *a = malloc((size_t)size + 1), *b = malloc((size_t)size + 1);
        if (s == mp_source_count(p->src) || mp_zpk_read_file(z, i, 0, a, (size_t)size, e) != 0 ||
            mp_source_read(p->src, s, 0, b, (size_t)size, e) != 0 || memcmp(a, b, (size_t)size)) {
            MP_FAIL_("contenido distinto: %s (%s)", path, e->msg);
            bad++;
        }
        free(a);
        free(b);
    }
    return bad == 0;
}

MP_TEST(zpk_roundtrip) {
    pk p;
    if (!open_pk(&p)) return;
    mp_zpk_stats st;
    uint8_t(*sha)[32] = NULL;
    if (write_packs(&p, TMP "/roundtrip", 4, MP_ZPK_VOLUME_LIMIT, &st, &sha) != 0) return;
    MP_CHECK_EQ_U64(st.files, p.n);
    MP_CHECK_EQ_U64(st.volumes, 1);
    mp_err e = {{0}};
    mp_zpk_volsrc vs;
    uint8_t *index = NULL;
    mp_zpk *z = open_dir_zpk(TMP "/roundtrip", &vs, &index, &e);
    MP_CHECK(z != NULL);
    if (z) {
        MP_CHECK_EQ_U64(mp_zpk_count(z), p.n);
        MP_CHECK(same_as_source(&p, z, &e));
        mp_zpk_close(z);
    }
    // sha[i] es el SHA-256 de files[i].
    for (size_t i = 0; i < p.n; i++) {
        uint64_t size = mp_source_entry(p.src, p.files[i])->size;
        uint8_t *b = malloc((size_t)size + 1), d[32];
        mp_source_read(p.src, p.files[i], 0, b, (size_t)size, &e);
        mp_sha256 s;
        mp_sha256_init(&s);
        mp_sha256_update(&s, b, (size_t)size);
        mp_sha256_final(&s, d);
        if (memcmp(d, sha[i], 32)) MP_FAIL_("SHA-256 distinto: %s", mp_source_entry(p.src, p.files[i])->path);
        free(b);
    }
    mp_zpk_volsrc_free(&vs);
    free(index);
    free(sha);
    close_pk(&p);
}

MP_TEST(zpk_multi_volume) {
    pk p;
    if (!open_pk(&p)) return;
    mp_zpk_stats st;
    uint8_t(*sha)[32] = NULL;
    // 512 KiB: no caben dos bloques en bruto en un volumen, así que random.bin cruza seguro.
    if (write_packs(&p, TMP "/multi", 3, 512u << 10, &st, &sha) != 0) return;
    MP_CHECK(st.volumes > 3);
    mp_err e = {{0}};
    mp_zpk_volsrc vs;
    uint8_t *index = NULL;
    mp_zpk *z = open_dir_zpk(TMP "/multi", &vs, &index, &e);
    MP_CHECK(z != NULL);
    if (z) {
        int crosses = 0;
        for (size_t i = 0; i < mp_zpk_count(z); i++) {
            uint32_t n = (uint32_t)((mp_zpk_size(z, i) + MP_BLOCK_V1 - 1) / MP_BLOCK_V1), v0 = 0, v1 = 0;
            if (n < 2) continue;
            mp_zpk_chunk(z, i, 0, &v0, NULL, NULL, NULL);
            mp_zpk_chunk(z, i, n - 1, &v1, NULL, NULL, NULL);
            crosses |= v0 != v1;
        }
        MP_CHECK(crosses);
        MP_CHECK(same_as_source(&p, z, &e));
        mp_zpk_close(z);
    }
    mp_zpk_volsrc_free(&vs);
    free(index);
    free(sha);
    close_pk(&p);
}

MP_TEST(zpk_random_raw_zeros_small) {
    pk p;
    if (!open_pk(&p)) return;
    mp_zpk_stats st;
    uint8_t(*sha)[32] = NULL;
    if (write_packs(&p, TMP "/codec", 2, MP_ZPK_VOLUME_LIMIT, &st, &sha) != 0) return;
    mp_err e = {{0}};
    mp_zpk_volsrc vs;
    uint8_t *index = NULL;
    mp_zpk *z = open_dir_zpk(TMP "/codec", &vs, &index, &e);
    MP_CHECK(z != NULL);
    for (size_t i = 0; z && i < mp_zpk_count(z); i++) {
        uint32_t stored = 0;
        int codec = -1;
        mp_zpk_chunk(z, i, 0, NULL, NULL, &stored, &codec);
        if (!strcmp(mp_zpk_path(z, i), "random.bin")) MP_CHECK(codec == 0 && stored == MP_BLOCK_V1);
        if (!strcmp(mp_zpk_path(z, i), "zeros.bin")) MP_CHECK(codec == 1 && stored < 1024);
    }
    if (z) mp_zpk_close(z);
    mp_zpk_volsrc_free(&vs);
    free(index);
    free(sha);
    close_pk(&p);
}

static int same_dirs(const char *a, const char *b, uint32_t nvol) {
    char pa[1024], pb[1024];
    for (uint32_t v = 0; v <= nvol; v++) {
        const char *name = v == nvol ? "ampr_zstd.index" : NULL;
        char vn[32];
        if (!name) {
            snprintf(vn, sizeof vn, "ampr_zstd-%03u.pak", v);
            name = vn;
        }
        snprintf(pa, sizeof pa, "%s/%s", a, name);
        snprintf(pb, sizeof pb, "%s/%s", b, name);
        size_t la = 0, lb = 0;
        uint8_t *x = slurp(pa, &la), *y = slurp(pb, &lb);
        int same = x && y && la == lb && !memcmp(x, y, la);
        free(x);
        free(y);
        if (!same) {
            MP_FAIL_("distinto: %s", name);
            return 0;
        }
    }
    return 1;
}

MP_TEST(zpk_threads_identical) {
    pk p;
    if (!open_pk(&p)) return;
    mp_zpk_stats a, b;
    uint8_t(*sa)[32] = NULL, (*sb)[32] = NULL;
    if (write_packs(&p, TMP "/t1", 1, 1u << 20, &a, &sa) != 0 || write_packs(&p, TMP "/t4", 4, 1u << 20, &b, &sb) != 0)
        return;
    MP_CHECK_EQ_U64(a.volumes, b.volumes);
    MP_CHECK(memcmp(a.archive_id, b.archive_id, 16) == 0);
    MP_CHECK(same_dirs(TMP "/t1", TMP "/t4", (uint32_t)a.volumes));
    free(sa);
    free(sb);
    close_pk(&p);
}

// Volúmenes en memoria, para poder estropear uno sin tocar la carpeta.
typedef struct {
    uint8_t *v[64];
    size_t n[64];
} memvols;

static int mem_read(void *ctx, uint32_t vol, uint64_t off, void *buf, size_t n, mp_err *e) {
    memvols *m = ctx;
    if (vol >= 64 || !m->v[vol] || off + n > m->n[vol]) {
        mp_err_set(e, "fuera del volumen");
        return -1;
    }
    memcpy(buf, m->v[vol] + off, n);
    return 0;
}

static uint64_t mem_size(void *ctx, uint32_t vol) { return vol < 64 ? ((memvols *)ctx)->n[vol] : 0; }

MP_TEST(zpk_rejects_corruption) {
    size_t len = 0;
    uint8_t *index = slurp(TMP "/roundtrip/ampr_zstd.index", &len);
    MP_CHECK(index != NULL);
    if (!index) return;
    memvols m = {{0}, {0}};
    m.v[0] = slurp(TMP "/roundtrip/ampr_zstd-000.pak", &m.n[0]);
    mp_zpk_volsrc vs = {mem_read, mem_size, &m};
    mp_err e = {{0}};
    mp_zpk *z = NULL;
    MP_CHECK(mp_zpk_open(index, len, &vs, &z, &e) == 0);
    // Un byte cambiado dentro del frame zstd del primer bloque de zeros.bin.
    size_t zi = 0;
    while (z && zi < mp_zpk_count(z) && strcmp(mp_zpk_path(z, zi), "zeros.bin")) zi++;
    uint64_t off = 0;
    uint32_t stored = 0;
    if (z) mp_zpk_chunk(z, zi, 0, NULL, &off, &stored, NULL);
    m.v[0][off + stored / 2] ^= 0x5A;
    uint8_t *buf = malloc(MP_BLOCK_V1);
    MP_CHECK(z && mp_zpk_read_file(z, zi, 0, buf, MP_BLOCK_V1, &e) == -1);
    if (z) mp_zpk_close(z);
    // Un byte cambiado en las tablas del índice: CRC.
    index[200] ^= 0x01;
    mp_err e2 = {{0}};
    MP_CHECK(mp_zpk_open(index, len, &vs, &z, &e2) == -1);
    MP_CHECK_CONTAINS(e2.msg, "CRC");
    free(buf);
    free(m.v[0]);
    free(index);
}

int main(void) {
    MP_RUN(zpk_roundtrip);
    MP_RUN(zpk_multi_volume);
    MP_RUN(zpk_random_raw_zeros_small);
    MP_RUN(zpk_threads_identical);
    MP_RUN(zpk_rejects_corruption);
    MP_TEST_MAIN_END();
}
