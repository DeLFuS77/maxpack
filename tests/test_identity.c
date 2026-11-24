// SPDX-License-Identifier: GPL-3.0-or-later
// The packer's output is byte-identical to 1.0.1's: the same input, level and options give the same
// bytes whatever the thread count. The golden SHA-256s below were recorded with 1.0.1 (on a failure
// the message prints the hash obtained). Also: the SHA-256 of each file, progress, a sink that fails,
// a source that cannot be read and cancelling, at 1, 2 and N threads.
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "mp_fixture.h"
#include "mp_hash.h"
#include "mp_parallel.h"
#include "mp_source.h"
#include "mp_zpk.h"

#define TMP MP_TEST_TMP "/identity"
#define MIXED TMP "/mixed"
#define LIMIT_MIN 262208u   // one uncompressed 256 KiB block per volume
#define LIMIT_1M 1000000u   // volumes cut at odd places

// ---- the synthetic source: many small files and a few large ones ---------------------------

static uint32_t rng = 2463534242u;
static uint32_t rnd(void) {
    rng ^= rng << 13;
    rng ^= rng >> 17;
    rng ^= rng << 5;
    return rng;
}

static void fill_noise(uint8_t *b, size_t n) {
    for (size_t i = 0; i < n; i++) b[i] = (uint8_t)(rnd() >> 24);
}

static void fill_text(uint8_t *b, size_t n) {
    static const char *w[] = {"alpha ", "beta ", "gamma ", "delta ", "level ", "mesh ",   "texture ", "sound ",
                              "zone ",  "\n",    "0123 ",  "vertex ", "normal ", "index ", "frame ",   "light "};
    size_t i = 0;
    while (i < n) {
        const char *s = w[rnd() % 16];
        for (; *s && i < n; s++) b[i++] = (uint8_t)*s;
    }
}

// 64 KiB pieces alternating noise and text.
static void fill_mixed(uint8_t *b, size_t n) {
    for (size_t off = 0, k = 0; off < n; off += 65536, k++) {
        size_t m = n - off < 65536 ? n - off : 65536;
        if (k % 2) fill_noise(b + off, m);
        else fill_text(b + off, m);
    }
}

// Repeats a 64 KiB pattern with a counter: long frames that compress quickly.
static void fill_repeat(uint8_t *b, size_t n) {
    uint8_t pat[65536];
    fill_noise(pat, sizeof pat);
    for (size_t off = 0, k = 0; off < n; off += sizeof pat, k++) {
        size_t m = n - off < sizeof pat ? n - off : sizeof pat;
        memcpy(b + off, pat, m);
        if (m >= 4) memcpy(b + off, &k, 4);
    }
}

static int64_t g_mtime = 1700000000;

static void put_file(const char *rel, const uint8_t *data, size_t n) {
    char p[1024];
    snprintf(p, sizeof p, MIXED "/%s", rel);
    mpt_put(p, data, n);
    mp_err e = {{0}};
    if (mp_set_mtime(p, g_mtime++, &e) != 0) MP_FAIL_("mtime %s: %s", p, e.msg);
}

static int mixed_ready = 0;

static void make_mixed(void) {
    if (mixed_ready) return;
    mixed_ready = 1;
    mpt_clear(MIXED);
    rng = 2463534242u;
    g_mtime = 1700000000;
    size_t big = (32u << 20) + 12345;
    uint8_t *b = malloc(big);
    if (!b) {
        MP_FAIL_("out of memory");
        return;
    }
    const char *pj = "{\"titleId\":\"TEST00009\",\"contentVersion\":\"01.000.000\"}";
    put_file("sce_sys/param.json", (const uint8_t *)pj, strlen(pj));
    fill_text(b, 100000);
    put_file("eboot.bin", b, 100000);
    put_file("empty.dat", b, 0);   // empty: stays loose
    // Loose and over one .p5z frame (32 MiB): two frames, and one of exactly one frame.
    fill_repeat(b, big);
    put_file("sce_module/big.prx", b, big);
    fill_repeat(b, 32u << 20);
    put_file("sce_module/exact.prx", b, 32u << 20);
    // Many small files of every kind.
    for (int i = 0; i < 400; i++) {
        char rel[64];
        size_t n = 1 + (size_t)((i * 7919) % 20000);
        snprintf(rel, sizeof rel, "data/small/d%02d/s%03d.bin", i % 13, i);
        if (i % 3 == 0) fill_noise(b, n);
        else if (i % 3 == 1) fill_text(b, n);
        else fill_mixed(b, n);
        put_file(rel, b, n);
    }
    // A few large ones: incompressible (stored raw), compressible, mixed and an exact number of blocks.
    fill_noise(b, (3u << 20) + 17);
    put_file("data/large/noise.bin", b, (3u << 20) + 17);
    fill_text(b, 2u << 20);
    put_file("data/large/text.bin", b, 2u << 20);
    fill_mixed(b, (5u << 19) + 3);
    put_file("data/large/mixed.bin", b, (5u << 19) + 3);
    fill_mixed(b, 1u << 20);
    put_file("data/large/exact.bin", b, 1u << 20);
    memset(b, 0, 512u << 10);
    put_file("data/large/zeros.bin", b, 512u << 10);
    free(b);
}

// ---- hashes of an output --------------------------------------------------------------------

static void hex(const uint8_t d[32], char out[65]) { mp_sha256_hex(d, out); }

static void le64(uint8_t *p, uint64_t v) {
    for (int i = 0; i < 8; i++) p[i] = (uint8_t)(v >> (8 * i));
}

// SHA-256 of a folder: for each file in path order, its path, a zero, its size and its bytes.
static void hash_tree(const char *dir, char out[65]) {
    mpt_list l = mpt_list_dir(dir);
    mp_sha256 s;
    mp_sha256_init(&s);
    for (size_t i = 0; i < l.n; i++) {
        char p[2048];
        snprintf(p, sizeof p, "%s/%s", dir, l.v[i]);
        size_t n = 0;
        uint8_t *b = mpt_slurp(p, &n), sz[8];
        le64(sz, n);
        mp_sha256_update(&s, l.v[i], strlen(l.v[i]) + 1);
        mp_sha256_update(&s, sz, 8);
        if (b) mp_sha256_update(&s, b, n);
        free(b);
    }
    mpt_list_free(&l);
    uint8_t d[32];
    mp_sha256_final(&s, d);
    hex(d, out);
}

static void hash_file(const char *path, char out[65]) {
    size_t n = 0;
    uint8_t *b = mpt_slurp(path, &n), d[32];
    mp_sha256 s;
    mp_sha256_init(&s);
    if (b) mp_sha256_update(&s, b, n);
    mp_sha256_final(&s, d);
    free(b);
    hex(d, out);
}

// ---- packing ----------------------------------------------------------------------------

static int pack(const char *src, const char *dst, int p5z, int level, unsigned threads, uint64_t limit,
                mp_pack_result *r, mp_err *e) {
    mpt_clear(dst);
    mpt_parent_dirs(dst);
    mp_pack_opts o;
    mp_pack_opts_default(&o);
    o.threads = threads;
    o.progress = 0;
    o.level = level;
    o.volume_limit = limit;
    mp_pack_out out;
    if ((p5z ? mp_pack_out_p5z(dst, threads, &out, e) : mp_pack_out_dir(dst, &out, e)) != 0) return -1;
    int rc = mp_pack_run(src, &out, &o, r, e);
    out.destroy(out.ctx);
    return rc;
}

typedef struct {
    const char *name;
    int mixed;   // the synthetic source (otherwise basic.exfat)
    int p5z, level;
    uint64_t limit;
    const char *golden;   // SHA-256 of the output with 1.0.1
} config;

static const config configs[] = {
    {"basic-folder-l19", 0, 0, 19, MP_ZPK_VOLUME_LIMIT, "6a673e6c6ea928b8ea70fb2a4c7fef1444a3ff0cfd00af9d275d000b7d19230f"},
    {"basic-p5z-l19", 0, 1, 19, MP_ZPK_VOLUME_LIMIT, "22c9ffe8109ebb71c563f8eb9cd8275370852cf1512ce66919288e02bed87fb1"},
    {"basic-folder-l19-minvol", 0, 0, 19, LIMIT_MIN, "86fb5349690be54dc3956e057738aa8186af16d34c086a91d0fffc5b28bbfbbe"},
    {"mixed-folder-l19-1m", 1, 0, 19, LIMIT_1M, "4c3b7b423ad2ada0408ec24dfe8bc35a3df3f960bf1b380981e02902ab32ca47"},
    {"mixed-p5z-l19-1m", 1, 1, 19, LIMIT_1M, "b427a1ef6c05533794f4b15c7a132c723db6d8d4f610c86837982d1bae81e128"},
    {"mixed-folder-l3-minvol", 1, 0, 3, LIMIT_MIN, "c2ff55d6b58d282f56c41d562f2cdb6fe9c8bea38a551a6443347f6a7a36f416"},
    {"mixed-p5z-l3", 1, 1, 3, MP_ZPK_VOLUME_LIMIT, "b8a03b7332fcbed6a73c5fbabcea5653dcefcfb5454bd2729e2c2dd03dce586e"},
};

static unsigned thread_counts[3];

static void init_threads(void) {
    unsigned n = mp_cpu_count();
    thread_counts[0] = 1;
    thread_counts[1] = 2;
    thread_counts[2] = n > 2 ? n : 5;   // N: all the CPU's (5 if it has 2 or fewer)
}

static void check_config(const config *c) {
    if (c->mixed) make_mixed();
    for (int k = 0; k < 3; k++) {
        unsigned t = thread_counts[k];
        char dst[512], got[65];
        snprintf(dst, sizeof dst, TMP "/out/%s-t%u%s", c->name, t, c->p5z ? ".p5z" : "");
        mp_pack_result r;
        mp_err e = {{0}};
        uint64_t t0 = mp_now_ns();
        if (pack(c->mixed ? MIXED : MPT_BASIC, dst, c->p5z, c->level, t, c->limit, &r, &e) != 0) {
            MP_FAIL_("%s, %u threads: %s", c->name, t, e.msg);
            continue;
        }
        if (c->p5z) hash_file(dst, got);
        else hash_tree(dst, got);
        fprintf(stderr, "  %s, %u threads: %s (%llu volumes, %.1f s)\n", c->name, t, got,
                (unsigned long long)r.volumes, (double)(mp_now_ns() - t0) / 1e9);
        if (strcmp(got, c->golden)) MP_FAIL_("%s, %u threads: %s, 1.0.1 gave %s", c->name, t, got, c->golden);
        if (c->limit == LIMIT_MIN && r.volumes < 4) MP_FAIL_("%s: only %llu volumes", c->name, (unsigned long long)r.volumes);
        mpt_clear(dst);
    }
}

MP_TEST(identity_basic) {
    for (size_t i = 0; i < sizeof configs / sizeof *configs; i++)
        if (!configs[i].mixed) check_config(&configs[i]);
}

MP_TEST(identity_mixed) {
    for (size_t i = 0; i < sizeof configs / sizeof *configs; i++)
        if (configs[i].mixed) check_config(&configs[i]);
}

// ---- mp_zpk_write with a sink in memory ---------------------------------------------------

typedef struct {
    mp_sha256 events;          // every call, in order, with its bytes
    uint64_t writes, bytes;
    int open;                  // a volume is open
    int fail_at_write;         // fails on that vol_write (1 = the first; 0 = never)
    int fail_at_begin;         // fails on that vol_begin
    int fail_header;           // fails on vol_header
    int bad_order;             // a call out of place
} memsink;

static void ev(memsink *m, char tag, uint32_t vol, const void *p, size_t n) {
    uint8_t h[13];
    h[0] = (uint8_t)tag;
    le64(h + 1, n);
    for (int i = 0; i < 4; i++) h[9 + i] = (uint8_t)(vol >> (8 * i));
    mp_sha256_update(&m->events, h, sizeof h);
    if (n) mp_sha256_update(&m->events, p, n);
}

static int ms_begin(void *ctx, uint32_t vol, mp_err *e) {
    memsink *m = ctx;
    if (m->open) m->bad_order = 1;
    if (m->fail_at_begin && (int)vol + 1 == m->fail_at_begin) {
        mp_err_set(e, "sink: cannot start volume %u", vol);
        return -1;
    }
    m->open = 1;
    ev(m, 'B', vol, NULL, 0);
    return 0;
}

static int ms_write(void *ctx, const void *p, size_t n, mp_err *e) {
    memsink *m = ctx;
    if (!m->open) m->bad_order = 1;
    m->writes++;
    if (m->fail_at_write && m->writes == (uint64_t)m->fail_at_write) {
        mp_err_set(e, "sink: disk full");
        return -1;
    }
    m->bytes += n;
    ev(m, 'W', 0, p, n);
    return 0;
}

static int ms_end(void *ctx, uint32_t vol, mp_err *e) {
    memsink *m = ctx;
    (void)e;
    if (!m->open) m->bad_order = 1;
    m->open = 0;
    ev(m, 'E', vol, NULL, 0);
    return 0;
}

static int ms_header(void *ctx, uint32_t vol, const uint8_t hdr[64], mp_err *e) {
    memsink *m = ctx;
    if (m->open) m->bad_order = 1;
    if (m->fail_header) {
        mp_err_set(e, "sink: cannot write the header of %u", vol);
        return -1;
    }
    ev(m, 'H', vol, hdr, 64);
    return 0;
}

typedef struct {
    mp_source *src;
    uint32_t *files;
    size_t n;
} srcset;

// The packable files of a source (all but the loose ones, as mp_pack_run does).
static int open_set(const char *path, srcset *s) {
    memset(s, 0, sizeof *s);
    mp_err e = {{0}};
    if (mp_source_open(path, &s->src, &e) != 0) {
        MP_FAIL_("source %s: %s", path, e.msg);
        return -1;
    }
    size_t n = mp_source_count(s->src);
    s->files = malloc((n ? n : 1) * sizeof *s->files);
    mp_rules *rules = mp_rules_new();
    for (size_t i = 0; s->files && rules && i < n; i++) {
        const mp_entry *en = mp_source_entry(s->src, i);
        if (mp_rules_classify(rules, en->path, en->size) == MP_PACKABLE) s->files[s->n++] = (uint32_t)i;
    }
    mp_rules_free(rules);
    return s->files ? 0 : -1;
}

static void close_set(srcset *s) {
    free(s->files);
    mp_source_close(s->src);
}

typedef struct {
    int rc;
    char msg[256];
    char events[65];   // hash of the sink's calls
    char index[65];    // hash of the ampr_zstd.index
    uint8_t (*sha)[32];
    mp_zpk_stats st;
    memsink m;
} zrun;

static void run_zpk(const srcset *s, int level, unsigned threads, uint64_t limit, mp_progress *prog, zrun *z) {
    memset(z, 0, sizeof *z);
    mp_sha256_init(&z->m.events);
    z->sha = calloc(s->n ? s->n : 1, 32);
    mp_zpk_sink sink = {ms_begin, ms_write, ms_end, ms_header, &z->m};
    mp_zpk_opts o = {level, threads, limit, prog};
    uint8_t *index = NULL, d[32];
    size_t len = 0;
    mp_err e = {{0}};
    z->rc = mp_zpk_write(s->src, s->files, s->n, &o, &sink, &index, &len, z->sha, &z->st, &e);
    snprintf(z->msg, sizeof z->msg, "%s", e.msg);
    mp_sha256_final(&z->m.events, d);
    hex(d, z->events);
    mp_sha256 h;
    mp_sha256_init(&h);
    if (index) mp_sha256_update(&h, index, len);
    mp_sha256_final(&h, d);
    hex(d, z->index);
    free(index);
}

// Each file's SHA-256, read again from the source.
static int sha_ok(const srcset *s, uint8_t (*sha)[32]) {
    int ok = 1;
    uint8_t *buf = malloc(1u << 20);
    for (size_t i = 0; buf && i < s->n; i++) {
        const mp_entry *en = mp_source_entry(s->src, s->files[i]);
        mp_sha256 h;
        mp_sha256_init(&h);
        for (uint64_t off = 0; off < en->size; off += 1u << 20) {
            size_t k = en->size - off < (1u << 20) ? (size_t)(en->size - off) : (1u << 20);
            mp_err e = {{0}};
            if (mp_source_read(s->src, s->files[i], off, buf, k, &e) != 0) ok = 0;
            mp_sha256_update(&h, buf, k);
        }
        uint8_t d[32];
        mp_sha256_final(&h, d);
        if (memcmp(d, sha[i], 32)) {
            MP_FAIL_("SHA-256 of %s", en->path);
            ok = 0;
        }
    }
    free(buf);
    return ok && buf;
}

MP_TEST(zpk_write_same_calls_and_sha) {
    make_mixed();
    srcset s;
    if (open_set(MIXED, &s) != 0) return;
    zrun ref;
    run_zpk(&s, 19, 1, LIMIT_1M, NULL, &ref);
    MP_CHECK(ref.rc == 0);
    MP_CHECK(!ref.m.bad_order);
    MP_CHECK(ref.st.volumes > 4);
    MP_CHECK(sha_ok(&s, ref.sha));
    for (int k = 1; k < 3; k++) {
        zrun z;
        run_zpk(&s, 19, thread_counts[k], LIMIT_1M, NULL, &z);
        MP_CHECK(z.rc == 0);
        MP_CHECK(!z.m.bad_order);
        MP_CHECK_STR(z.events, ref.events);
        MP_CHECK_STR(z.index, ref.index);
        MP_CHECK(memcmp(z.sha, ref.sha, 32 * s.n) == 0);
        MP_CHECK(memcmp(&z.st, &ref.st, sizeof z.st) == 0);
        free(z.sha);
    }
    free(ref.sha);
    close_set(&s);
}

// ---- progress ---------------------------------------------------------------------------

typedef struct {
    uint64_t calls, last_done, total, bad;
    char phase[64];
    int cancel_after;   // cancels on that call (0 = never)
} prec;

static void on_prog(void *ctx, const char *phase, uint64_t done, uint64_t total) {
    prec *p = ctx;
    if (strcmp(p->phase, phase)) {   // a new phase starts from scratch
        snprintf(p->phase, sizeof p->phase, "%s", phase);
        p->last_done = 0;
    }
    if (done < p->last_done || done > total || (p->total && strncmp(phase, "Packing", 7) == 0 && total != p->total))
        p->bad++;
    p->last_done = done;
    p->calls++;
    if (p->cancel_after && p->calls == (uint64_t)p->cancel_after) mp_cancel_request();
}

MP_TEST(zpk_write_progress_monotonic) {
    make_mixed();
    srcset s;
    if (open_set(MIXED, &s) != 0) return;
    uint64_t total = 0;
    for (size_t i = 0; i < s.n; i++) total += mp_source_entry(s.src, s.files[i])->size;
    for (int k = 0; k < 3; k++) {
        prec p = {0};
        p.total = total;
        mp_progress_hook h = {on_prog, &p};
        mp_progress_set_hook(&h);
        mp_progress prog;
        mp_progress_init(&prog, "Packing (zstd level 3)", total, 0);
        mp_progress_set_hook(NULL);
        zrun z;
        run_zpk(&s, 3, thread_counts[k], LIMIT_MIN, &prog, &z);
        MP_CHECK(z.rc == 0);
        MP_CHECK_EQ_U64(prog.done, total);
        MP_CHECK_EQ_U64(p.bad, 0);
        MP_CHECK(p.calls >= 1);
        MP_CHECK_EQ_U64(p.last_done, total);   // the last call is the end of the phase
        mp_progress_free(&prog);
        free(z.sha);
    }
    close_set(&s);
}

// ---- failures -----------------------------------------------------------------------------

MP_TEST(zpk_write_sink_failures) {
    make_mixed();
    srcset s;
    if (open_set(MIXED, &s) != 0) return;
    for (int k = 0; k < 3; k++) {
        unsigned t = thread_counts[k];
        // a full disk on the first write, in the middle and on the last; a volume that cannot start; the headers
        int at_write[] = {1, 7, 40};
        for (int j = 0; j < 3; j++) {
            zrun z;
            memset(&z, 0, sizeof z);
            mp_sha256_init(&z.m.events);
            z.m.fail_at_write = at_write[j];
            uint8_t(*sha)[32] = calloc(s.n, 32);
            mp_zpk_sink sink = {ms_begin, ms_write, ms_end, ms_header, &z.m};
            mp_zpk_opts o = {3, t, LIMIT_MIN, NULL};
            uint8_t *index = (uint8_t *)1;
            size_t len = 1;
            mp_err e = {{0}};
            int rc = mp_zpk_write(s.src, s.files, s.n, &o, &sink, &index, &len, sha, &z.st, &e);
            MP_CHECK(rc == -1);
            MP_CHECK_STR(e.msg, "sink: disk full");
            MP_CHECK(index == NULL && len == 0);
            MP_CHECK_EQ_U64(z.m.writes, (uint64_t)at_write[j]);   // nothing written after the failure
            free(sha);
        }
        int at_begin[] = {1, 3};
        for (int j = 0; j < 2; j++) {
            memsink m;
            memset(&m, 0, sizeof m);
            mp_sha256_init(&m.events);
            m.fail_at_begin = at_begin[j];
            uint8_t(*sha)[32] = calloc(s.n, 32);
            mp_zpk_sink sink = {ms_begin, ms_write, ms_end, ms_header, &m};
            mp_zpk_opts o = {3, t, LIMIT_MIN, NULL};
            uint8_t *index = NULL;
            size_t len = 0;
            mp_zpk_stats st;
            mp_err e = {{0}};
            MP_CHECK(mp_zpk_write(s.src, s.files, s.n, &o, &sink, &index, &len, sha, &st, &e) == -1);
            MP_CHECK_CONTAINS(e.msg, "sink: cannot start volume");
            MP_CHECK(index == NULL);
            free(sha);
        }
        memsink m;
        memset(&m, 0, sizeof m);
        mp_sha256_init(&m.events);
        m.fail_header = 1;
        uint8_t(*sha)[32] = calloc(s.n, 32);
        mp_zpk_sink sink = {ms_begin, ms_write, ms_end, ms_header, &m};
        mp_zpk_opts o = {3, t, LIMIT_MIN, NULL};
        uint8_t *index = NULL;
        size_t len = 0;
        mp_zpk_stats st;
        mp_err e = {{0}};
        MP_CHECK(mp_zpk_write(s.src, s.files, s.n, &o, &sink, &index, &len, sha, &st, &e) == -1);
        MP_CHECK_CONTAINS(e.msg, "sink: cannot write the header");
        MP_CHECK(index == NULL);
        free(sha);
    }
    close_set(&s);
}

// A file that disappears after opening the source: the read fails with its message.
MP_TEST(zpk_write_read_error) {
    make_mixed();
    for (int k = 0; k < 3; k++) {
        const char *copy = TMP "/vanish";
        mpt_clear(copy);
        mpt_copy_file(MIXED "/data/large/text.bin", TMP "/vanish/a/text.bin");
        mpt_copy_file(MIXED "/data/large/noise.bin", TMP "/vanish/b/noise.bin");
        mpt_copy_file(MIXED "/data/large/mixed.bin", TMP "/vanish/c/mixed.bin");
        srcset s;
        if (open_set(copy, &s) != 0) return;
        MP_CHECK_EQ_U64(s.n, 3);
        // the file that goes in the middle of the index (sorted by path hash)
        uint64_t h[3];
        for (size_t i = 0; i < 3; i++) h[i] = mp_zpk_path_hash(mp_source_entry(s.src, s.files[i])->path);
        size_t mid = 0;
        for (size_t i = 0; i < 3; i++) {
            int below = 0;
            for (size_t j = 0; j < 3; j++) below += h[j] < h[i];
            if (below == 1) mid = i;
        }
        char victim[1024];
        snprintf(victim, sizeof victim, "%s/%s", copy, mp_source_entry(s.src, s.files[mid])->path);
        MP_CHECK(mp_remove_file(victim) == 0);
        memsink m;
        memset(&m, 0, sizeof m);
        mp_sha256_init(&m.events);
        uint8_t(*sha)[32] = calloc(3, 32);
        mp_zpk_sink sink = {ms_begin, ms_write, ms_end, ms_header, &m};
        mp_zpk_opts o = {3, thread_counts[k], LIMIT_MIN, NULL};
        uint8_t *index = NULL;
        size_t len = 0;
        mp_zpk_stats st;
        mp_err e = {{0}};
        MP_CHECK(mp_zpk_write(s.src, s.files, s.n, &o, &sink, &index, &len, sha, &st, &e) == -1);
        MP_CHECK_CONTAINS(e.msg, mp_source_entry(s.src, s.files[mid])->path);
        MP_CHECK(index == NULL);
        MP_CHECK(!m.bad_order);
        free(sha);
        close_set(&s);
    }
}

MP_TEST(zpk_write_cancel) {
    make_mixed();
    srcset s;
    if (open_set(MIXED, &s) != 0) return;
    uint64_t total = 0;
    for (size_t i = 0; i < s.n; i++) total += mp_source_entry(s.src, s.files[i])->size;
    for (int k = 0; k < 3; k++) {
        // already cancelled: nothing is written
        mp_cancel_request();
        zrun z;
        run_zpk(&s, 3, thread_counts[k], LIMIT_MIN, NULL, &z);
        mp_cancel_reset();
        MP_CHECK(z.rc == -1);
        MP_CHECK_STR(z.msg, "cancelled");
        MP_CHECK_EQ_U64(z.m.writes, 0);
        free(z.sha);
        // cancelled from the progress hook, in the middle
        prec p = {0};
        p.cancel_after = 1;   // the first call comes after the first block (or batch) is written
        mp_progress_hook h = {on_prog, &p};
        mp_progress_set_hook(&h);
        mp_progress prog;
        mp_progress_init(&prog, "Packing (zstd level 19)", total, 0);
        mp_progress_set_hook(NULL);
        run_zpk(&s, 19, thread_counts[k], LIMIT_MIN, &prog, &z);
        mp_cancel_reset();
        MP_CHECK(z.rc == -1);
        MP_CHECK_STR(z.msg, "cancelled");
        MP_CHECK(prog.done < total);
        MP_CHECK(!z.m.bad_order);
        mp_progress_free(&prog);
        free(z.sha);
    }
    close_set(&s);
}

// ---- the .p5z's frames (loose files): cap, cancel ---------------------------------------------

typedef struct {
    int cancel_copying;   // cancels on the first progress of the loose files
    int saw_packing;
} lrec;

static void on_loose(void *ctx, const char *phase, uint64_t done, uint64_t total) {
    lrec *r = ctx;
    (void)done;
    (void)total;
    if (!strncmp(phase, "Packing", 7)) r->saw_packing = 1;
    if (r->cancel_copying && !strncmp(phase, "Copying", 7)) mp_cancel_request();
}

static int pack_p5z_capped(const char *dst, unsigned threads, uint64_t cap, int *too_big, mp_err *e) {
    mpt_clear(dst);
    mpt_parent_dirs(dst);
    mp_pack_opts o;
    mp_pack_opts_default(&o);
    o.threads = threads;
    o.progress = 0;
    o.level = 3;
    mp_pack_out out;
    if (mp_pack_out_p5z_ex(dst, threads, cap, &out, e) != 0) return -2;
    mp_pack_result r;
    int rc = mp_pack_run(MIXED, &out, &o, &r, e);
    *too_big = mp_pack_out_p5z_too_big(&out);
    out.destroy(out.ctx);
    return rc;
}

MP_TEST(p5z_frames_cap_and_cancel) {
    make_mixed();
    const char *dst = TMP "/out/capped.p5z";
    for (int k = 0; k < 3; k++) {
        unsigned t = thread_counts[k];
        // over the cap in the first frames, after some and in the volumes: nothing is left
        uint64_t caps[] = {100, 2000, 70000, 3u << 20};
        for (int j = 0; j < 4; j++) {
            int too_big = 0;
            mp_err e = {{0}};
            int rc = pack_p5z_capped(dst, t, caps[j], &too_big, &e);
            if (rc != -1 || !too_big) MP_FAIL_("%u threads, cap %llu: rc %d, too_big %d, %s", t,
                                              (unsigned long long)caps[j], rc, too_big, e.msg);
            MP_CHECK_CONTAINS(e.msg, "the .p5z would go over");
            MP_CHECK_EQ_U64(mp_path_kind(dst), 0);
        }
        // cancelled while the loose files are compressed: the packs never start
        lrec r = {1, 0};
        mp_progress_hook h = {on_loose, &r};
        mp_progress_set_hook(&h);
        int too_big = 0;
        mp_err e = {{0}};
        int rc = pack_p5z_capped(dst, t, 0, &too_big, &e);
        mp_progress_set_hook(NULL);
        mp_cancel_reset();
        MP_CHECK(rc == -1);
        MP_CHECK_STR(e.msg, "cancelled");
        MP_CHECK(!too_big);
        MP_CHECK(!r.saw_packing);
        MP_CHECK_EQ_U64(mp_path_kind(dst), 0);
    }
}

int main(void) {
    init_threads();
    mpt_clear(TMP "/out");
    MP_RUN(identity_basic);
    MP_RUN(identity_mixed);
    MP_RUN(zpk_write_same_calls_and_sha);
    MP_RUN(zpk_write_progress_monotonic);
    MP_RUN(zpk_write_sink_failures);
    MP_RUN(zpk_write_read_error);
    MP_RUN(zpk_write_cancel);
    MP_RUN(p5z_frames_cap_and_cancel);
    MP_TEST_MAIN_END();
}
