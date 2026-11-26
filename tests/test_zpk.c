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

// Empty folder (with files only, no subfolders) for a new output.
static void fresh_dir(const char *dir) {
    mp_err e = {{0}};
    mp_make_dirs(dir, &e);
    mp_dir_walk(dir, rm_cb, (void *)dir, &e);
}

// Writes the packs of basic's packable files into dir; returns 0 if it went well.
static int write_packs(pk *p, const char *dir, unsigned threads, uint64_t vol_limit, mp_zpk_stats *st,
                       uint8_t (**sha)[32]) {
    mp_err e = {{0}};
    fresh_dir(dir);
    mp_zpk_sink sink;
    if (mp_zpk_sink_dir(dir, &sink, &e) != 0) {
        MP_FAIL_("sink: %s", e.msg);
        return -1;
    }
    mp_zpk_opts o = {19, threads, vol_limit, NULL, 0};
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
            MP_FAIL_("different content: %s (%s)", path, e->msg);
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
    // sha[i] is the SHA-256 of files[i].
    for (size_t i = 0; i < p.n; i++) {
        uint64_t size = mp_source_entry(p.src, p.files[i])->size;
        uint8_t *b = malloc((size_t)size + 1), d[32];
        mp_source_read(p.src, p.files[i], 0, b, (size_t)size, &e);
        mp_sha256 s;
        mp_sha256_init(&s);
        mp_sha256_update(&s, b, (size_t)size);
        mp_sha256_final(&s, d);
        if (memcmp(d, sha[i], 32)) MP_FAIL_("different SHA-256: %s", mp_source_entry(p.src, p.files[i])->path);
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
    // 512 KiB: two raw blocks do not fit in a volume, so random.bin surely crosses.
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
            MP_FAIL_("different: %s", name);
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

// Volumes in memory, so one can be corrupted without touching the folder.
typedef struct {
    uint8_t *v[64];
    size_t n[64];
} memvols;

static int mem_read(void *ctx, uint32_t vol, uint64_t off, void *buf, size_t n, mp_err *e) {
    memvols *m = ctx;
    if (vol >= 64 || !m->v[vol] || off + n > m->n[vol]) {
        mp_err_set(e, "outside the volume");
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
    // A byte changed inside the zstd frame of zeros.bin's first block.
    size_t zi = 0;
    while (z && zi < mp_zpk_count(z) && strcmp(mp_zpk_path(z, zi), "zeros.bin")) zi++;
    uint64_t off = 0;
    uint32_t stored = 0;
    if (z) mp_zpk_chunk(z, zi, 0, NULL, &off, &stored, NULL);
    m.v[0][off + stored / 2] ^= 0x5A;
    uint8_t *buf = malloc(MP_BLOCK_V1);
    MP_CHECK(z && mp_zpk_read_file(z, zi, 0, buf, MP_BLOCK_V1, &e) == -1);
    if (z) mp_zpk_close(z);
    // A byte changed in the index tables: CRC.
    index[200] ^= 0x01;
    mp_err e2 = {{0}};
    MP_CHECK(mp_zpk_open(index, len, &vs, &z, &e2) == -1);
    MP_CHECK_CONTAINS(e2.msg, "CRC");
    free(buf);
    free(m.v[0]);
    free(index);
}

MP_TEST(zpk_find_all) {
    mp_err e = {{0}};
    mp_zpk_volsrc vs;
    uint8_t *index = NULL;
    mp_zpk *z = open_dir_zpk(TMP "/roundtrip", &vs, &index, &e);
    MP_CHECK(z != NULL);
    size_t bad = 0;
    for (size_t i = 0; z && i < mp_zpk_count(z); i++) bad += mp_zpk_find(z, mp_zpk_path(z, i)) != (long)i;
    MP_CHECK_EQ_U64(bad, 0);
    MP_CHECK(z && mp_zpk_find(z, "no/existe.bin") == -1);
    if (z) mp_zpk_close(z);
    mp_zpk_volsrc_free(&vs);
    free(index);
}


// ---- chunks, index and layout apart; the rules of a .p5z that grew ------------------------------

static int write_index(const char *dir, const uint8_t *index, size_t len) {
    char ip[1024];
    mp_err e = {{0}};
    snprintf(ip, sizeof ip, "%s/ampr_zstd.index", dir);
    if (mp_write_file(ip, index, len, &e) != 0) {
        MP_FAIL_("%s", e.msg);
        return -1;
    }
    return 0;
}

// mp_zpk_write_chunks, mp_zpk_build_index and mp_zpk_volume_header give what mp_zpk_write gives.
MP_TEST(zpk_chunks_then_index_is_write) {
    pk p;
    if (!open_pk(&p)) return;
    mp_zpk_stats st;
    uint8_t(*sha)[32] = NULL, (*sha2)[32] = malloc(p.n * sizeof *sha2);
    if (write_packs(&p, TMP "/cw_ref", 2, 1u << 20, &st, &sha) != 0) return;
    mp_err e = {{0}};
    fresh_dir(TMP "/cw");
    mp_zpk_sink sink;
    MP_CHECK(mp_zpk_sink_dir(TMP "/cw", &sink, &e) == 0);
    mp_zpk_opts o = {19, 3, 1u << 20, NULL, 0};
    mp_zpk_chunks c;
    MP_CHECK(mp_zpk_write_chunks(p.src, p.files, p.n, &o, &sink, &c, sha2, &e) == 0);
    MP_CHECK_EQ_U64(c.nvol, st.volumes);
    MP_CHECK(memcmp(sha, sha2, p.n * sizeof *sha2) == 0);
    mp_zpk_entry *v = calloc(p.n, sizeof *v);
    for (size_t k = 0; k < p.n; k++) {
        const mp_entry *en = mp_source_entry(p.src, p.files[k]);
        v[k].path = en->path;
        v[k].size = en->size;
        v[k].chunks = c.chunks + c.first[k];
    }
    uint8_t *index = NULL, aid[16];
    size_t len = 0;
    MP_CHECK(mp_zpk_build_index(v, p.n, c.vol_size, c.nvol, 19, NULL, &index, &len, aid, &e) == 0);
    MP_CHECK(memcmp(aid, st.archive_id, 16) == 0);
    for (uint32_t k = 0; k < c.nvol; k++) {
        uint8_t h[64];
        mp_zpk_volume_header(k, c.nvol, aid, c.vol_size[k], h);
        MP_CHECK(sink.vol_header(sink.ctx, k, h, &e) == 0);
    }
    mp_zpk_sink_free(&sink);
    if (index && write_index(TMP "/cw", index, len) == 0) MP_CHECK(same_dirs(TMP "/cw_ref", TMP "/cw", c.nvol));
    mp_zpk_chunks_free(&c);
    free(index);
    free(v);
    free(sha);
    free(sha2);
    close_pk(&p);
}

// Volumes numbered from first_volume: the chunks name them, the sink gets them.
MP_TEST(zpk_first_volume) {
    pk p;
    if (!open_pk(&p)) return;
    mp_err e = {{0}};
    fresh_dir(TMP "/fv");
    mp_zpk_sink sink;
    MP_CHECK(mp_zpk_sink_dir(TMP "/fv", &sink, &e) == 0);
    mp_zpk_opts o = {19, 2, 512u << 10, NULL, 3};
    mp_zpk_chunks c;
    uint8_t(*sha)[32] = malloc(p.n * sizeof *sha);
    MP_CHECK(mp_zpk_write_chunks(p.src, p.files, p.n, &o, &sink, &c, sha, &e) == 0);
    mp_zpk_sink_free(&sink);
    MP_CHECK(c.nvol > 1);
    uint32_t lo = 0xFFFFFFFFu, hi = 0;
    for (uint64_t k = 0; k < c.nchunks; k++) {
        uint32_t vol = (uint32_t)((c.chunks[k] >> 32) & 0xFFF);
        if (vol < lo) lo = vol;
        if (vol > hi) hi = vol;
    }
    MP_CHECK_EQ_U64(lo, 3);
    MP_CHECK_EQ_U64(hi, 3 + c.nvol - 1);
    char vp[1024];
    uint64_t size = 0;
    snprintf(vp, sizeof vp, "%s/ampr_zstd-003.pak", TMP "/fv");
    MP_CHECK(mp_stat(vp, &size, NULL) == 0 && size == c.vol_size[0]);
    snprintf(vp, sizeof vp, "%s/ampr_zstd-000.pak", TMP "/fv");
    MP_CHECK(mp_path_kind(vp) == 0);
    mp_zpk_chunks_free(&c);
    free(sha);
    close_pk(&p);
}

// Laid out again with the same volume limit, a pack is the same, index and volumes.
MP_TEST(zpk_relayout_identity) {
    mp_err e = {{0}};
    mp_zpk_volsrc vs;
    uint8_t *index = NULL;
    mp_zpk *z = open_dir_zpk(TMP "/multi", &vs, &index, &e);
    MP_CHECK(z != NULL);
    if (!z) return;
    fresh_dir(TMP "/relay");
    mp_zpk_sink sink;
    MP_CHECK(mp_zpk_sink_dir(TMP "/relay", &sink, &e) == 0);
    uint8_t *ni = NULL, aid[16];
    size_t nl = 0;
    MP_CHECK(mp_zpk_relayout(z, 512u << 10, &sink, NULL, &ni, &nl, aid, &e) == 0);
    mp_zpk_sink_free(&sink);
    MP_CHECK(memcmp(aid, mp_zpk_archive_id(z), 16) == 0);
    if (ni && write_index(TMP "/relay", ni, nl) == 0) MP_CHECK(same_dirs(TMP "/multi", TMP "/relay", mp_zpk_volume_count(z)));
    free(ni);
    mp_zpk_close(z);
    mp_zpk_volsrc_free(&vs);
    free(index);
}

#define C_OFF(c) ((c) & 0xFFFFFFFFull)
#define C_VOL(c) ((uint32_t)(((c) >> 32) & 0xFFF))
#define C_STORED(c) ((uint32_t)(((c) >> 44) & 0x3FFFF) + 1)
#define C_WITH_OFF(c, o) (((c) & ~0xFFFFFFFFull) | (uint64_t)(o))

// A pack in memory whose chunks can be moved: its files' records, chunks and volumes.
typedef struct {
    size_t n;
    char **path;
    uint64_t *size, *first;   // first: of each file in chunks
    uint64_t *chunks, nchunks;
    uint32_t nvol;
    uint8_t aid[16];
    memvols m;
} mpack;

static int load_mpack(const char *dir, mpack *q) {
    memset(q, 0, sizeof *q);
    mp_err e = {{0}};
    mp_zpk_volsrc vs;
    uint8_t *index = NULL;
    mp_zpk *z = open_dir_zpk(dir, &vs, &index, &e);
    if (!z) {
        MP_FAIL_("%s: %s", dir, e.msg);
        return 0;
    }
    q->n = mp_zpk_count(z);
    q->nvol = mp_zpk_volume_count(z);
    memcpy(q->aid, mp_zpk_archive_id(z), 16);
    q->path = calloc(q->n, sizeof *q->path);
    q->size = calloc(q->n, sizeof *q->size);
    q->first = calloc(q->n, sizeof *q->first);
    for (size_t i = 0; i < q->n; i++) q->nchunks += (mp_zpk_size(z, i) + MP_BLOCK_V1 - 1) / MP_BLOCK_V1;
    q->chunks = calloc(q->nchunks + 1, sizeof *q->chunks);
    uint64_t at = 0;
    for (size_t i = 0; i < q->n; i++) {
        uint64_t cnt = 0;
        const uint64_t *c = mp_zpk_file_chunks(z, i, &cnt);
        q->path[i] = mp_strdup(mp_zpk_path(z, i));
        q->size[i] = mp_zpk_size(z, i);
        q->first[i] = at;
        if (cnt) memcpy(q->chunks + at, c, cnt * sizeof *c);
        at += cnt;
    }
    for (uint32_t v = 0; v < q->nvol; v++) {
        char vp[1024];
        snprintf(vp, sizeof vp, "%s/ampr_zstd-%03u.pak", dir, v);
        q->m.v[v] = slurp(vp, &q->m.n[v]);
    }
    mp_zpk_close(z);
    mp_zpk_volsrc_free(&vs);
    free(index);
    return 1;
}

static void free_mpack(mpack *q) {
    for (size_t i = 0; i < q->n; i++) free(q->path[i]);
    free(q->path);
    free(q->size);
    free(q->first);
    free(q->chunks);
    for (int v = 0; v < 64; v++) free(q->m.v[v]);
}

static void reheader(mpack *q, uint32_t v, uint32_t count) {
    mp_zpk_volume_header(v, count, q->aid, q->m.n[v], q->m.v[v]);
}

static int build_mpack(mpack *q, uint8_t **index, size_t *len, mp_err *e) {
    mp_zpk_entry *v = calloc(q->n, sizeof *v);
    uint64_t vs_size[64];
    for (uint32_t k = 0; k < q->nvol; k++) vs_size[k] = q->m.n[k];
    for (size_t i = 0; i < q->n; i++) {
        v[i].path = q->path[i];
        v[i].size = q->size[i];
        v[i].chunks = q->chunks + q->first[i];
    }
    uint8_t aid[16];
    int rc = mp_zpk_build_index(v, q->n, vs_size, q->nvol, 19, q->aid, index, len, aid, e);
    free(v);
    return rc;
}

// The index of q as it is now, opened with the rules given: 0 if it opens and (with src) reads every
// file as the source has it.
static int open_mpack(mpack *q, int rules, pk *src, mp_err *e) {
    uint8_t *index = NULL;
    size_t len = 0;
    if (build_mpack(q, &index, &len, e) != 0) return -1;
    mp_zpk_volsrc vs = {mem_read, mem_size, &q->m};
    mp_zpk *z = NULL;
    int rc = mp_zpk_open_ex(index, len, &vs, rules, &z, e);
    if (rc == 0 && src && !same_as_source(src, z, e)) rc = -2;
    mp_zpk_close(z);
    free(index);
    return rc;
}

// Each volume's chunks written again in the reverse of their order, back to back from 64.
static void reverse_layout(mpack *q) {
    for (uint32_t v = 0; v < q->nvol; v++) {
        uint8_t *nb = malloc(q->m.n[v]);
        memcpy(nb, q->m.v[v], 64);
        uint64_t at = 64;
        for (uint64_t k = q->nchunks; k-- > 0;) {
            uint64_t c = q->chunks[k];
            if (C_VOL(c) != v) continue;
            memcpy(nb + at, q->m.v[v] + C_OFF(c), C_STORED(c));
            q->chunks[k] = C_WITH_OFF(c, at);
            at += C_STORED(c);
        }
        free(q->m.v[v]);
        q->m.v[v] = nb;
    }
}

static uint64_t first_chunk_of(const mpack *q, uint32_t v, uint64_t from) {
    for (uint64_t k = from; k < q->nchunks; k++)
        if (C_VOL(q->chunks[k]) == v) return k;
    return q->nchunks;
}

MP_TEST(zpk_p5z_rules) {
    pk p;
    if (!open_pk(&p)) return;
    mpack q;
    if (!load_mpack(TMP "/multi", &q)) return;
    mp_err e = {{0}};
    MP_CHECK(q.nvol >= 3 && q.nvol < 63);
    MP_CHECK(open_mpack(&q, MP_ZPK_RULES_PACK, &p, &e) == 0);
    MP_CHECK(open_mpack(&q, MP_ZPK_RULES_P5Z, &p, &e) == 0);
    // out of record order: only p5z rules take it, and every file reads the same
    reverse_layout(&q);
    MP_CHECK(open_mpack(&q, MP_ZPK_RULES_PACK, NULL, &e) != 0);
    e.msg[0] = 0;
    MP_CHECK(open_mpack(&q, MP_ZPK_RULES_P5Z, &p, &e) == 0);
    // laid out again, it is the pack it was
    {
        uint8_t *index = NULL, aid[16], *ni = NULL;
        size_t len = 0, nl = 0;
        MP_CHECK(build_mpack(&q, &index, &len, &e) == 0);
        mp_zpk_volsrc vs = {mem_read, mem_size, &q.m};
        mp_zpk *z = NULL;
        MP_CHECK(index && mp_zpk_open_ex(index, len, &vs, MP_ZPK_RULES_P5Z, &z, &e) == 0);
        fresh_dir(TMP "/relay2");
        mp_zpk_sink sink;
        MP_CHECK(mp_zpk_sink_dir(TMP "/relay2", &sink, &e) == 0);
        MP_CHECK(z && mp_zpk_relayout(z, 512u << 10, &sink, NULL, &ni, &nl, aid, &e) == 0);
        mp_zpk_sink_free(&sink);
        if (ni && write_index(TMP "/relay2", ni, nl) == 0) MP_CHECK(same_dirs(TMP "/multi", TMP "/relay2", q.nvol));
        mp_zpk_close(z);
        free(ni);
        free(index);
    }
    free_mpack(&q);

    // a hole: 1000 bytes after volume 0's first chunk
    if (!load_mpack(TMP "/multi", &q)) return;
    {
        uint64_t k0 = first_chunk_of(&q, 0, 0), cut = C_OFF(q.chunks[k0]) + C_STORED(q.chunks[k0]);
        uint8_t *nb = calloc(1, q.m.n[0] + 1000);
        memcpy(nb, q.m.v[0], (size_t)cut);
        memcpy(nb + cut + 1000, q.m.v[0] + cut, q.m.n[0] - (size_t)cut);
        for (uint64_t k = 0; k < q.nchunks; k++)
            if (C_VOL(q.chunks[k]) == 0 && C_OFF(q.chunks[k]) >= cut)
                q.chunks[k] = C_WITH_OFF(q.chunks[k], C_OFF(q.chunks[k]) + 1000);
        free(q.m.v[0]);
        q.m.v[0] = nb;
        q.m.n[0] += 1000;
        reheader(&q, 0, q.nvol);
        MP_CHECK(open_mpack(&q, MP_ZPK_RULES_PACK, NULL, &e) != 0);
        MP_CHECK(open_mpack(&q, MP_ZPK_RULES_P5Z, &p, &e) == 0);
    }
    // a volume with no chunk (only its header) at the end, every header counting it
    {
        q.m.v[q.nvol] = calloc(1, 64);
        q.m.n[q.nvol] = 64;
        q.nvol++;
        for (uint32_t v = 0; v < q.nvol; v++) reheader(&q, v, q.nvol);
        MP_CHECK(open_mpack(&q, MP_ZPK_RULES_PACK, NULL, &e) != 0);
        MP_CHECK(open_mpack(&q, MP_ZPK_RULES_P5Z, &p, &e) == 0);
        // headers written before it grew count fewer volumes: p5z rules only; more, neither
        for (uint32_t v = 0; v + 1 < q.nvol; v++) reheader(&q, v, q.nvol - 1);
        MP_CHECK(open_mpack(&q, MP_ZPK_RULES_PACK, NULL, &e) != 0);
        MP_CHECK(open_mpack(&q, MP_ZPK_RULES_P5Z, &p, &e) == 0);
        reheader(&q, 0, q.nvol + 1);
        e.msg[0] = 0;
        MP_CHECK(open_mpack(&q, MP_ZPK_RULES_P5Z, NULL, &e) != 0);
        MP_CHECK_CONTAINS(e.msg, "header of volume ampr_zstd-000.pak");
        reheader(&q, 0, q.nvol);
        MP_CHECK(open_mpack(&q, MP_ZPK_RULES_P5Z, &p, &e) == 0);
    }
    free_mpack(&q);

    // an overlap, a chunk past its volume's end, a chunk in the header, in a volume not there: refused
    if (!load_mpack(TMP "/multi", &q)) return;
    {
        // a volume with two chunks at least
        uint32_t v = 0;
        uint64_t k0 = 0, k1 = q.nchunks;
        for (; v < q.nvol && k1 == q.nchunks; v++) {
            k0 = first_chunk_of(&q, v, 0);
            k1 = first_chunk_of(&q, v, k0 + 1);
        }
        v--;
        MP_CHECK(k1 < q.nchunks);
        char want[64];
        uint64_t keep = q.chunks[k1];
        q.chunks[k1] = C_WITH_OFF(keep, C_OFF(q.chunks[k0]) + 1);
        MP_CHECK(open_mpack(&q, MP_ZPK_RULES_PACK, NULL, &e) != 0);
        e.msg[0] = 0;
        MP_CHECK(open_mpack(&q, MP_ZPK_RULES_P5Z, NULL, &e) != 0);
        snprintf(want, sizeof want, "overlap in volume %u", v);
        MP_CHECK_CONTAINS(e.msg, want);
        q.chunks[k1] = keep;
        // the volume's last chunk one byte later: past its end
        uint64_t kl = k1;
        for (uint64_t k = k1; k < q.nchunks; k++)
            if (C_VOL(q.chunks[k]) == v) kl = k;
        uint64_t keepl = q.chunks[kl];
        q.chunks[kl] = C_WITH_OFF(keepl, C_OFF(keepl) + 1);
        MP_CHECK(open_mpack(&q, MP_ZPK_RULES_PACK, NULL, &e) != 0);
        e.msg[0] = 0;
        MP_CHECK(open_mpack(&q, MP_ZPK_RULES_P5Z, NULL, &e) != 0);
        snprintf(want, sizeof want, "past the end of volume %u", v);
        MP_CHECK_CONTAINS(e.msg, want);
        q.chunks[kl] = keepl;
        MP_CHECK(open_mpack(&q, MP_ZPK_RULES_P5Z, &p, &e) == 0);
        uint64_t keep0 = q.chunks[k0];
        q.chunks[k0] = C_WITH_OFF(keep0, 32);
        MP_CHECK(open_mpack(&q, MP_ZPK_RULES_PACK, NULL, &e) != 0);
        MP_CHECK(open_mpack(&q, MP_ZPK_RULES_P5Z, NULL, &e) != 0);
        q.chunks[k0] = (keep0 & ~(0xFFFull << 32)) | ((uint64_t)q.nvol << 32);
        MP_CHECK(open_mpack(&q, MP_ZPK_RULES_P5Z, NULL, &e) != 0);
        q.chunks[k0] = keep0;
    }
    free_mpack(&q);
    close_pk(&p);
}

int main(void) {
    MP_RUN(zpk_roundtrip);
    MP_RUN(zpk_find_all);
    MP_RUN(zpk_multi_volume);
    MP_RUN(zpk_random_raw_zeros_small);
    MP_RUN(zpk_threads_identical);
    MP_RUN(zpk_rejects_corruption);
    MP_RUN(zpk_chunks_then_index_is_write);
    MP_RUN(zpk_first_volume);
    MP_RUN(zpk_relayout_identity);
    MP_RUN(zpk_p5z_rules);
    MP_TEST_MAIN_END();
}
