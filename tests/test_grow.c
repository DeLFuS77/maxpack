// SPDX-License-Identifier: GPL-3.0-or-later
// Growing a .p5z by appending: index version 2, the journal that undoes an append that stopped, the
// grow engine (add, update), compact, and every reader of a .p5z that grew.
#define ZSTD_STATIC_LINKING_ONLY
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <zstd.h>

#include "mp_fixture.h"
#include "mp_grow.h"
#include "mp_hash.h"
#include "mp_p5z_icon.h"
#include "mp_p5z_internal.h"
#include "mp_play.h"
#include "mp_inst.h"

#define TMP MP_TEST_TMP "/grow"
#define GAME MP_FIXTURES_DIR "/game"

static uint32_t rd32(const uint8_t *p) { return p[0] | p[1] << 8 | p[2] << 16 | (uint32_t)p[3] << 24; }
static uint64_t rd64(const uint8_t *p) { return rd32(p) | (uint64_t)rd32(p + 4) << 32; }
static void wr32(uint8_t *p, uint32_t v) {
    for (int i = 0; i < 4; i++) p[i] = (uint8_t)(v >> (8 * i));
}
static void wr64(uint8_t *p, uint64_t v) {
    for (int i = 0; i < 8; i++) p[i] = (uint8_t)(v >> (8 * i));
}

static int same_bytes(const char *a, const char *b) {
    size_t la = 0, lb = 0;
    uint8_t *x = mpt_slurp(a, &la), *y = mpt_slurp(b, &lb);
    int same = x && y && la == lb && !memcmp(x, y, la);
    free(x);
    free(y);
    return same;
}

// 1 if the file at path starts with the n bytes of want.
static int starts_with(const char *path, const uint8_t *want, size_t n) {
    size_t l = 0;
    uint8_t *x = mpt_slurp(path, &l);
    int ok = x && l >= n && !memcmp(x, want, n);
    free(x);
    return ok;
}

// The decompressed index of a .p5z (malloc), with where it starts and its compressed length.
static uint8_t *read_index(const char *path, size_t *ulen, uint64_t *ioff) {
    size_t l = 0;
    uint8_t *b = mpt_slurp(path, &l), *ix = NULL;
    if (!b || l < 96) {
        free(b);
        return NULL;
    }
    const uint8_t *ft = b + l - 32;
    uint64_t off = rd64(ft + 8), clen = rd64(ft + 16);
    unsigned long long us = ZSTD_getFrameContentSize(b + off, (size_t)clen);
    if (us != ZSTD_CONTENTSIZE_ERROR && us != ZSTD_CONTENTSIZE_UNKNOWN && (ix = malloc((size_t)us)) &&
        ZSTD_decompress(ix, (size_t)us, b + off, (size_t)clen) != us) {
        free(ix);
        ix = NULL;
    }
    *ulen = (size_t)us;
    *ioff = off;
    free(b);
    return ix;
}

// dst: src with its index replaced by ix (n bytes), compressed again, and a new footer.
static void put_index(const char *src, const char *dst, const uint8_t *ix, size_t n) {
    size_t l = 0;
    uint8_t *b = mpt_slurp(src, &l);
    uint64_t off = rd64(b + l - 32 + 8);
    size_t cap = ZSTD_compressBound(n);
    uint8_t *out = malloc((size_t)off + cap + 32);
    memcpy(out, b, (size_t)off);
    size_t c = ZSTD_compress(out + off, cap, ix, n, 3);
    uint8_t *ft = out + off + c;
    memset(ft, 0, 32);
    memcpy(ft, "P5ZFOOT1", 8);
    wr64(ft + 8, off);
    wr64(ft + 16, c);
    wr32(ft + 24, mp_crc32(out + off, c, 0));
    wr32(ft + 28, mp_crc32(ft, 28, 0));
    mpt_clear(dst);
    mpt_put(dst, out, (size_t)off + c + 32);
    free(out);
    free(b);
}

static int cmp_entry(const void *a, const void *b) {
    return strcmp(((const mp_manifest_entry *)a)->path, ((const mp_manifest_entry *)b)->path);
}

static void sha_of(const void *p, size_t n, uint8_t d[32]) {
    mp_sha256 s;
    mp_sha256_init(&s);
    mp_sha256_update(&s, p, n);
    mp_sha256_final(&s, d);
}

// ---- index version 2 -----------------------------------------------------------------------------

#define A_P5Z TMP "/a.p5z"
#define EXTRA "extra/new.txt"
static const char extra_text[] = "a file added to a .p5z already made, by appending to it\n";

// A .p5z of the game with one more loose file appended: every old record kept, index version 2.
static int make_appended(const char *dst, uint8_t **orig, size_t *orig_len) {
    mp_err e = {{0}};
    mpt_clear(TMP);
    if (mpt_pack(GAME, dst, 1, NULL) != 0) return -1;
    *orig = mpt_slurp(dst, orig_len);
    mp_p5z *p = NULL;
    mp_wfile *f = NULL;
    int busy = 0, rc = -1;
    mp_pack_out out;
    memset(&out, 0, sizeof out);
    if (mp_p5z_open(dst, &p, &e) != 0 || mp_wfile_reopen_locked(dst, &f, &busy, &e) != 0 ||
        p5z_out_append(f, 2, 0, 1, &out, &e) != 0) {
        MP_FAIL_("%s", e.msg);
        goto done;
    }
    for (size_t i = 0; i < p->nfiles; i++)
        if (p5z_out_keep(&out, p, i, p->files[i].mtime, &e) != 0) goto fail;
    uint8_t sha[32];
    size_t n = sizeof extra_text - 1;
    sha_of(extra_text, n, sha);
    if (out.file_begin(out.ctx, EXTRA, MP_ROLE_LOOSE, n, 1700000123, &e) != 0 ||
        out.file_write(out.ctx, extra_text, n, &e) != 0 || out.file_end(out.ctx, sha, &e) != 0)
        goto fail;
    mp_pack_manifest m = p->man;
    m.v = calloc(p->man.n + 1, sizeof *m.v);
    memcpy(m.v, p->man.v, p->man.n * sizeof *m.v);
    m.v[m.n].path = EXTRA;
    m.v[m.n].size = n;
    m.v[m.n].mtime = 1700000123;
    memcpy(m.v[m.n].sha, sha, 32);
    m.n++;
    qsort(m.v, m.n, sizeof *m.v, cmp_entry);
    rc = out.finish(out.ctx, &m, &e);
    free(m.v);
    if (rc != 0) goto fail;
    goto done;
fail:
    MP_FAIL_("append: %s", e.msg);
    rc = -1;
done:
    if (out.destroy) out.destroy(out.ctx);
    if (f) mp_wfile_close(f, NULL);
    mp_p5z_close(p);
    return rc;
}

MP_TEST(p5z_append_one_file) {
    uint8_t *orig = NULL;
    size_t olen = 0;
    if (make_appended(A_P5Z, &orig, &olen) != 0) return;
    MP_CHECK(starts_with(A_P5Z, orig, olen));   // nothing before the old end written again
    mp_err e = {{0}};
    mp_p5z *p = NULL;
    MP_CHECK(mp_p5z_open(A_P5Z, &p, &e) == 0);
    if (p) {
        MP_CHECK_EQ_U64(mp_p5z_version(p), 2);
        MP_CHECK_EQ_U64(mp_p5z_grown(p), 1);
        MP_CHECK_EQ_U64(p->prev_size, olen);
        MP_CHECK(p->index_off >= olen);
        MP_CHECK(mp_unpack_folder(p, TMP "/a_folder", 2, 0, &e) == 0);
        mp_p5z_close(p);
    }
    size_t n = 0;
    uint8_t *b = mpt_slurp(TMP "/a_folder/" EXTRA, &n);
    MP_CHECK(b && n == sizeof extra_text - 1 && !memcmp(b, extra_text, n));
    free(b);
    mp_verify_result r;
    MP_CHECK(mp_verify_p5z(A_P5Z, 2, 0, &r, &e) == 0);
    MP_CHECK_EQ_U64(r.files_bad, 0);
    MP_CHECK(r.files_ok > 10);
    free(orig);
}

// Every record copied, as it is stored, into a new .p5z: the same file, byte for byte.
MP_TEST(p5z_copy_all_is_identical) {
    mp_err e = {{0}};
    mpt_clear(TMP "/b.p5z");
    mpt_clear(TMP "/c.p5z");
    if (mpt_pack(GAME, TMP "/b.p5z", 1, NULL) != 0) return;
    mp_p5z *p = NULL;
    mp_pack_out out;
    MP_CHECK(mp_p5z_open(TMP "/b.p5z", &p, &e) == 0);
    MP_CHECK(mp_pack_out_p5z(TMP "/c.p5z", 2, &out, &e) == 0);
    for (size_t i = 0; p && i < p->nfiles; i++) MP_CHECK(p5z_out_copy(&out, p, i, &e) == 0);
    if (p) MP_CHECK(out.finish(out.ctx, &p->man, &e) == 0);
    out.destroy(out.ctx);
    mp_p5z_close(p);
    MP_CHECK(same_bytes(TMP "/b.p5z", TMP "/c.p5z"));
}

// A reader of version 1 (MP_P5Z_OPEN_V1, and the check 1.0.6 makes) refuses version 2 cleanly.
MP_TEST(p5z_v1_only_refuses_v2) {
    uint8_t *orig = NULL;
    size_t olen = 0;
    if (make_appended(A_P5Z, &orig, &olen) != 0) return;
    free(orig);
    mp_err e = {{0}};
    mp_p5z *p = NULL;
    MP_CHECK(mp_p5z_open_ex(A_P5Z, MP_P5Z_OPEN_V1, &p, &e) != 0 && !p);
    MP_CHECK_CONTAINS(e.msg, "index version 2");
    if (mpt_pack(GAME, TMP "/v1.p5z", 1, NULL) != 0) return;
    MP_CHECK(mp_p5z_open_ex(TMP "/v1.p5z", MP_P5Z_OPEN_V1, &p, &e) == 0);
    mp_p5z_close(p);
    size_t n = 0;
    uint64_t ioff = 0;
    uint8_t *ix = read_index(A_P5Z, &n, &ioff);
    MP_CHECK(ix && n > 72);
    // 1.0.6: memcmp(ix, "P5ZIDX01", 8) != 0 || rd32(ix + 8) != 1 -> "invalid .p5z: index"
    int old_reader_takes = ix && n >= 56 && memcmp(ix, "P5ZIDX01", 8) == 0 && rd32(ix + 8) == 1;
    MP_CHECK(!old_reader_takes);
    MP_CHECK(ix && memcmp(ix, "P5ZIDX01", 8) == 0 && rd32(ix + 8) == 2);
    free(ix);
}

MP_TEST(p5z_v2_damaged) {
    uint8_t *orig = NULL;
    size_t olen = 0;
    if (make_appended(A_P5Z, &orig, &olen) != 0) return;
    free(orig);
    size_t n = 0;
    uint64_t ioff = 0;
    uint8_t *ix = read_index(A_P5Z, &n, &ioff);
    if (!ix) return;
    mp_err e = {{0}};
    mp_p5z *p = NULL;
    // the same index written again opens
    put_index(A_P5Z, TMP "/d.p5z", ix, n);
    MP_CHECK(mp_p5z_open(TMP "/d.p5z", &p, &e) == 0);
    mp_p5z_close(p);
    // prev_size past the index, or below a .p5z's least
    uint8_t *x = malloc(n);
    memcpy(x, ix, n);
    wr64(x + 56, ioff + 1);
    put_index(A_P5Z, TMP "/d.p5z", x, n);
    MP_CHECK(mp_p5z_open(TMP "/d.p5z", &p, &e) != 0);
    wr64(x + 56, 95);
    put_index(A_P5Z, TMP "/d.p5z", x, n);
    MP_CHECK(mp_p5z_open(TMP "/d.p5z", &p, &e) != 0);
    // grown 0, the reserved field
    memcpy(x, ix, n);
    wr32(x + 64, 0);
    put_index(A_P5Z, TMP "/d.p5z", x, n);
    MP_CHECK(mp_p5z_open(TMP "/d.p5z", &p, &e) != 0);
    memcpy(x, ix, n);
    wr32(x + 68, 1);
    put_index(A_P5Z, TMP "/d.p5z", x, n);
    MP_CHECK(mp_p5z_open(TMP "/d.p5z", &p, &e) != 0);
    // a volume named out of the ids 0..n-1
    memcpy(x, ix, n);
    uint64_t slen = rd64(x + 24);
    uint8_t *strings = x + n - slen, *hit = NULL;
    for (uint64_t k = 0; k + 17 <= slen && !hit; k++)
        if (!memcmp(strings + k, "ampr_zstd-000.pak", 17)) hit = strings + k;
    MP_CHECK(hit != NULL);
    if (hit) hit[12] = '1';
    put_index(A_P5Z, TMP "/d.p5z", x, n);
    e.msg[0] = 0;
    MP_CHECK(mp_p5z_open(TMP "/d.p5z", &p, &e) != 0);
    MP_CHECK_CONTAINS(e.msg, "volume");
    free(x);
    free(ix);
}

static int mem_at(void *ctx, void *buf, size_t n, uint64_t off) {
    const uint8_t *b = ((const uint8_t **)ctx)[0];
    uint64_t size = (uint64_t)(uintptr_t)((void **)ctx)[1];
    if (off > size || n > size - off) return -1;
    memcpy(buf, b + off, n);
    return 0;
}

// The thumbnails' icon reader takes a .p5z that grew.
MP_TEST(p5z_icon_of_v2) {
    uint8_t *orig = NULL;
    size_t olen = 0;
    if (make_appended(A_P5Z, &orig, &olen) != 0) return;
    free(orig);
    size_t n = 0, want = 0;
    uint8_t *b = mpt_slurp(A_P5Z, &n), *icon = mpt_slurp(GAME "/sce_sys/icon0.png", &want);
    void *ctx[2] = {b, (void *)(uintptr_t)n};
    mp_icon_src src = {mem_at, n, ctx};
    uint8_t *png = NULL;
    size_t len = 0;
    char err[256] = "";
    MP_CHECK(mp_p5z_icon_read(&src, &png, &len, err, sizeof err) == 0);
    MP_CHECK(png && icon && len == want && !memcmp(png, icon, len));
    free(png);
    free(icon);
    free(b);
}

// ---- the journal of an append --------------------------------------------------------------------

#define J_P5Z TMP "/j.p5z"
#define J_FILE J_P5Z MP_P5Z_JOURNAL

// A .p5z with 5000 bytes appended to it (as an append that stopped would leave it), and its bytes
// before (malloc) in *orig.
static int half_appended(uint8_t **orig, size_t *olen) {
    mpt_clear(TMP);
    if (mpt_pack(GAME, J_P5Z, 1, NULL) != 0) return -1;
    *orig = mpt_slurp(J_P5Z, olen);
    mp_err e = {{0}};
    mp_wfile *f = NULL;
    uint8_t junk[5000];
    for (int i = 0; i < 5000; i++) junk[i] = (uint8_t)(i * 13 + 5);
    if (mp_wfile_reopen(J_P5Z, &f, &e) != 0 || mp_wfile_write(f, junk, sizeof junk, &e) != 0 ||
        mp_wfile_close(f, &e) != 0) {
        MP_FAIL_("%s", e.msg);
        return -1;
    }
    return 0;
}

static uint64_t size_of(const char *path) {
    uint64_t s = 0;
    return mp_stat(path, &s, NULL) == 0 ? s : 0;
}

MP_TEST(journal_view_and_restore) {
    uint8_t *orig = NULL;
    size_t olen = 0;
    if (half_appended(&orig, &olen) != 0) return;
    mp_err e = {{0}};
    mp_p5z *p = NULL;
    MP_CHECK(mp_p5z_open(J_P5Z, &p, &e) != 0);   // without the journal: no footer at its end
    MP_CHECK(p5z_journal_write(J_P5Z, olen, orig + olen - 32, &e) == 0);
    MP_CHECK(mp_path_kind(J_FILE) == 1 && mp_path_kind(J_FILE ".tmp") == 0);
    // a reader sees it as it was, and writes nothing
    MP_CHECK(mp_p5z_open(J_P5Z, &p, &e) == 0);
    if (p) MP_CHECK_EQ_U64(p->size, olen);
    mp_p5z_close(p);
    MP_CHECK_EQ_U64(size_of(J_P5Z), olen + 5000);
    int what = -1;
    MP_CHECK(mp_p5z_recover(J_P5Z, &what, &e) == 0);
    MP_CHECK_EQ_U64(what, MP_RECOVER_RESTORED);
    MP_CHECK(starts_with(J_P5Z, orig, olen) && size_of(J_P5Z) == olen);
    MP_CHECK(mp_path_kind(J_FILE) == 0);
    what = -1;
    MP_CHECK(mp_p5z_recover(J_P5Z, &what, &e) == 0 && what == MP_RECOVER_NONE);
    free(orig);
}

MP_TEST(recover_busy_leaves_everything) {
    uint8_t *orig = NULL;
    size_t olen = 0;
    if (half_appended(&orig, &olen) != 0) return;
    mp_err e = {{0}};
    MP_CHECK(p5z_journal_write(J_P5Z, olen, orig + olen - 32, &e) == 0);
    mp_wfile *f = NULL;
    int busy = 0, what = -1;
    MP_CHECK(mp_wfile_reopen_locked(J_P5Z, &f, &busy, &e) == 0);   // an append at work
    MP_CHECK(mp_p5z_recover(J_P5Z, &what, &e) == 0);
    MP_CHECK_EQ_U64(what, MP_RECOVER_BUSY);
    MP_CHECK_EQ_U64(size_of(J_P5Z), olen + 5000);
    MP_CHECK(mp_path_kind(J_FILE) == 1);
    mp_p5z *p = NULL;
    MP_CHECK(mp_p5z_open(J_P5Z, &p, &e) == 0);   // readers read it as it was meanwhile
    mp_p5z_close(p);
    mp_wfile_close(f, NULL);
    MP_CHECK(mp_p5z_recover(J_P5Z, &what, &e) == 0 && what == MP_RECOVER_RESTORED);
    MP_CHECK(starts_with(J_P5Z, orig, olen) && size_of(J_P5Z) == olen);
    free(orig);
}

MP_TEST(recover_stale_journal) {
    uint8_t *orig = NULL;
    size_t olen = 0;
    if (half_appended(&orig, &olen) != 0) return;
    size_t blen = 0;
    uint8_t *before = mpt_slurp(J_P5Z, &blen), foot[32];
    mp_err e = {{0}};
    int what = -1;
    // a footer that is not the one there: not this file's journal, the file is not touched
    memcpy(foot, orig + olen - 32, 32);
    foot[3] ^= 1;
    MP_CHECK(p5z_journal_write(J_P5Z, olen, foot, &e) == 0);
    MP_CHECK(mp_p5z_recover(J_P5Z, &what, &e) == 0 && what == MP_RECOVER_STALE);
    MP_CHECK(mp_path_kind(J_FILE) == 0);
    MP_CHECK(starts_with(J_P5Z, before, blen) && size_of(J_P5Z) == blen);
    // a size past the file's end
    MP_CHECK(p5z_journal_write(J_P5Z, blen + 1, orig + olen - 32, &e) == 0);
    MP_CHECK(mp_p5z_recover(J_P5Z, &what, &e) == 0 && what == MP_RECOVER_STALE);
    MP_CHECK(starts_with(J_P5Z, before, blen) && size_of(J_P5Z) == blen);
    // the .p5z is gone: its journal goes too
    MP_CHECK(p5z_journal_write(J_P5Z, olen, orig + olen - 32, &e) == 0);
    mpt_clear(J_P5Z);
    MP_CHECK(mp_p5z_recover(J_P5Z, &what, &e) == 0 && what == MP_RECOVER_STALE);
    MP_CHECK(mp_path_kind(J_FILE) == 0);
    free(before);
    free(orig);
}

MP_TEST(recover_damaged_or_tmp) {
    uint8_t *orig = NULL;
    size_t olen = 0;
    if (half_appended(&orig, &olen) != 0) return;
    mp_err e = {{0}};
    int what = -1;
    MP_CHECK(p5z_journal_write(J_P5Z, olen, orig + olen - 32, &e) == 0);
    size_t jl = 0;
    uint8_t *j = mpt_slurp(J_FILE, &jl);
    MP_CHECK(j && jl == 64 && !memcmp(j, "P5ZJRNL1", 8));
    if (j && jl == 64) {
        j[20] ^= 1;   // the CRC no longer matches
        mpt_clear(J_FILE);
        mpt_put(J_FILE, j, jl);
    }
    mp_p5z *p = NULL;
    MP_CHECK(mp_p5z_open(J_P5Z, &p, &e) != 0);   // a damaged journal tells a reader nothing
    MP_CHECK(mp_p5z_recover(J_P5Z, &what, &e) == 0 && what == MP_RECOVER_STALE);
    MP_CHECK(mp_path_kind(J_FILE) == 0 && size_of(J_P5Z) == olen + 5000);
    // a journal half written (only its .tmp): no append began, it goes
    mpt_put(J_FILE ".tmp", "P5ZJ", 4);
    MP_CHECK(mp_p5z_recover(J_P5Z, &what, &e) == 0 && what == MP_RECOVER_NONE);
    MP_CHECK(mp_path_kind(J_FILE ".tmp") == 0 && size_of(J_P5Z) == olen + 5000);
    free(j);
    free(orig);
}

// ---- the grow engine -----------------------------------------------------------------------------

#define V1 TMP "/v1"
#define V2 TMP "/v2"

typedef struct {
    const char *from, *to;
} tree_copy;

static int copy_cb(void *ctx, const char *rel, int is_dir, uint64_t size, int64_t mtime) {
    const tree_copy *t = ctx;
    (void)size;
    if (is_dir) return 0;
    char a[2048], b[2048];
    snprintf(a, sizeof a, "%s/%s", t->from, rel);
    snprintf(b, sizeof b, "%s/%s", t->to, rel);
    mpt_copy_file(a, b);
    mp_set_mtime(b, mtime, NULL);
    return 0;
}

// A copy of a folder, dates kept.
static void copy_tree(const char *from, const char *to) {
    tree_copy t = {from, to};
    mpt_clear(to);
    mp_err e = {{0}};
    mp_make_dirs(to, &e);
    mp_dir_walk(from, copy_cb, &t, &e);
}

static void put_dated(const char *path, const void *data, size_t n, int64_t mtime) {
    mpt_clear(path);
    mpt_put(path, data, n);
    mp_set_mtime(path, mtime, NULL);
}

// v1: the game; v2: its next version: param.json and data/big.bin changed (big.bin keeps its size),
// data/noise.bin gone, data/new1.bin and sce_sys/new.txt new, small/s00.txt only dated again.
static void make_versions(void) {
    copy_tree(GAME, V1);
    copy_tree(GAME, V2);
    const char *pj = "{\"contentId\": \"UP0000-PPSA00000_00-MAXPACKTEST00000\", \"titleId\": \"PPSA00000\", "
                     "\"contentVersion\": \"01.001.000\", \"localizedParameters\": {\"defaultLanguage\": \"en-US\", "
                     "\"en-US\": {\"titleName\": \"MaxPack Test Game\"}}}";
    put_dated(V2 "/sce_sys/param.json", pj, strlen(pj), 1700100000);
    size_t n = 0;
    uint8_t *b = mpt_slurp(V1 "/data/big.bin", &n);
    if (b && n > 1000) {
        for (size_t i = 0; i < n; i += 4096) b[i] ^= 0x5A;
        put_dated(V2 "/data/big.bin", b, n, 1700100001);
    }
    free(b);
    mpt_clear(V2 "/data/noise.bin");
    uint8_t *t = malloc(300000);
    for (size_t i = 0; i < 300000; i++) t[i] = (uint8_t)("new data for the second version "[i % 32]);
    put_dated(V2 "/data/new1.bin", t, 300000, 1700100002);
    free(t);
    put_dated(V2 "/sce_sys/new.txt", "a loose file new in version 2\n", 30, 1700100003);
    int64_t m = 0;
    mp_stat(V2 "/small/s00.txt", NULL, &m);
    mp_set_mtime(V2 "/small/s00.txt", m + 100, NULL);
}

static int grow_with(const char *p5z, const char *dump, const mp_grow_opts *o, mp_grow_result *r, mp_err *e) {
    mp_source *s = NULL;
    if (mp_source_open(dump, &s, e) != 0) return -1;
    int rc = mp_grow_run(p5z, s, NULL, NULL, o, r, e);
    mp_source_close(s);
    return rc;
}

static mp_grow_opts quiet_opts(void) {
    mp_grow_opts o;
    mp_grow_opts_default(&o);
    o.threads = 2;
    o.progress = 0;
    o.force = 1;
    return o;
}

#define G_P5Z TMP "/g.p5z"

MP_TEST(grow_unpack_original_is_v2) {
    mpt_clear(TMP);
    make_versions();
    if (mpt_pack(V1, G_P5Z, 1, NULL) != 0) return;
    size_t olen = 0;
    uint8_t *orig = mpt_slurp(G_P5Z, &olen);
    mp_grow_opts o = quiet_opts();
    mp_grow_result r;
    mp_err e = {{0}};
    MP_CHECK(grow_with(G_P5Z, V2, &o, &r, &e) == 0);
    if (e.msg[0]) fprintf(stderr, "  %s\n", e.msg);
    MP_CHECK(starts_with(G_P5Z, orig, olen));
    MP_CHECK(mp_path_kind(G_P5Z MP_P5Z_JOURNAL) == 0);
    MP_CHECK_EQ_U64(r.new_files, 2);
    MP_CHECK_EQ_U64(r.changed_files, 2);
    MP_CHECK_EQ_U64(r.removed_files, 1);
    MP_CHECK_EQ_U64(r.grown, 1);
    MP_CHECK(r.same_files > 10 && !r.nothing);
    MP_CHECK_EQ_U64(r.p5z_size, size_of(G_P5Z));
    MP_CHECK_EQ_U64(r.added_bytes, size_of(G_P5Z) - olen);
    mp_p5z *p = NULL;
    MP_CHECK(mp_p5z_open(G_P5Z, &p, &e) == 0);
    if (p) {
        MP_CHECK_EQ_U64(mp_p5z_version(p), 2);
        MP_CHECK_STR(p->man.content_version, "01.001.000");
        MP_CHECK(mp_unpack_original(p, TMP "/g_orig", 2, 0, &e) == 0);
        mp_p5z_close(p);
    }
    MP_CHECK(mpt_dirs_equal(V2, TMP "/g_orig", NULL));
    mp_verify_result vr;
    MP_CHECK(mp_verify_p5z(G_P5Z, 2, 0, &vr, &e) == 0);
    MP_CHECK_EQ_U64(vr.files_bad, 0);
    free(orig);
}

// The volumes maxpack.image lists: every one, old and new, by id 0..n-1.
MP_TEST(grow_play_lists_every_volume) {
    mpt_clear(TMP);
    make_versions();
    if (mpt_pack_vol(V1, G_P5Z, 1, NULL, 262208) != 0) return;
    mp_err e = {{0}};
    mp_p5z *p = NULL;
    uint32_t before = 0;
    MP_CHECK(mp_p5z_open(G_P5Z, &p, &e) == 0);
    for (size_t i = 0; p && i < p->nfiles; i++) before += p->files[i].role == MP_ROLE_VOLUME;
    mp_p5z_close(p);
    MP_CHECK(before > 2);
    mp_grow_opts o = quiet_opts();
    o.volume_limit = 262208;
    mp_grow_result r;
    MP_CHECK(grow_with(G_P5Z, V2, &o, &r, &e) == 0);
    MP_CHECK(r.volumes_added > 0 && r.volumes == before + r.volumes_added);
    char err[256] = "";
    mp_play *pl = mp_play_open(G_P5Z, err, sizeof err);
    MP_CHECK(pl != NULL);
    if (!pl) {
        fprintf(stderr, "  %s\n", err);
        return;
    }
    char *j = mp_play_json(pl);
    MP_CHECK(mp_p5z_open(G_P5Z, &p, &e) == 0);
    size_t nvol = 0;
    for (size_t i = 0; p && i < p->nfiles; i++) {
        const p5z_file *f = &p->files[i];
        if (f->role != MP_ROLE_VOLUME) continue;
        char want[128];
        snprintf(want, sizeof want, "{\"id\":%u,\"offset\":%llu,\"length\":%llu}", (unsigned)nvol,
                 (unsigned long long)f->data_off, (unsigned long long)f->size);
        MP_CHECK_CONTAINS(j, want);
        nvol++;
    }
    MP_CHECK_EQ_U64(nvol, r.volumes);
    char none[64];
    snprintf(none, sizeof none, "{\"id\":%u,", (unsigned)nvol);
    MP_CHECK(j && !strstr(j, none));
    mp_p5z_close(p);
    mp_play_free_str(j);
    mp_play_close(pl);
}

// The test hook: fails (mode 1) or stops as a power cut would (mode 2) at its call number `at`.
static int fault_at, fault_mode, fault_calls;
static int fault_hook(const char *step) {
    (void)step;
    return ++fault_calls == fault_at ? fault_mode : 0;
}

MP_TEST(grow_fault_every_step) {
    mpt_clear(TMP);
    make_versions();
    if (mpt_pack_vol(V1, G_P5Z, 1, NULL, 262208) != 0) return;
    size_t olen = 0;
    uint8_t *orig = mpt_slurp(G_P5Z, &olen);
    mp_grow_opts o = quiet_opts();
    o.volume_limit = 262208;
    mp_grow_set_fault(fault_hook);
    int steps = 0;
    for (int mode = 1; mode <= 2; mode++) {
        for (fault_at = 1;; fault_at++) {
            fault_mode = mode;
            fault_calls = 0;
            mp_grow_result r;
            mp_err e = {{0}};
            int rc = grow_with(G_P5Z, V2, &o, &r, &e);
            if (fault_calls < fault_at) {   // the run did not reach that step: it went to the end
                MP_CHECK(rc == 0);
                break;
            }
            MP_CHECK(rc != 0);
            if (mode == 2) {
                // as after a power cut: a reader sees it as it was, a later open undoes the append
                mp_p5z *p = NULL;
                MP_CHECK(mp_p5z_open(G_P5Z, &p, &e) == 0);
                if (p) MP_CHECK(mp_p5z_version(p) == 1 && p->size == olen);
                mp_p5z_close(p);
                int what = -1;
                MP_CHECK(mp_p5z_recover(G_P5Z, &what, &e) == 0 && what != MP_RECOVER_BUSY);
            }
            if (!starts_with(G_P5Z, orig, olen) || size_of(G_P5Z) != olen) {
                MP_FAIL_("mode %d, step %d: the .p5z is not as it was", mode, fault_at);
                mpt_clear(G_P5Z);
                mpt_put(G_P5Z, orig, olen);
            }
            MP_CHECK(mp_path_kind(G_P5Z MP_P5Z_JOURNAL) == 0);
            if (mode == 1) steps++;
            if (fault_at > 400) {
                MP_FAIL_("too many steps");
                break;
            }
        }
        // the run that went to the end grew it: start again from the original
        mpt_clear(G_P5Z);
        mpt_put(G_P5Z, orig, olen);
    }
    mp_grow_set_fault(NULL);
    fprintf(stderr, "  %d steps\n", steps);
    MP_CHECK(steps > 10);
    free(orig);
}

MP_TEST(grow_refuses) {
    mpt_clear(TMP);
    make_versions();
    if (mpt_pack_vol(V1, G_P5Z, 1, NULL, 262208) != 0) return;
    size_t olen = 0;
    uint8_t *orig = mpt_slurp(G_P5Z, &olen);
    mp_grow_opts o = quiet_opts();
    o.volume_limit = 262208;
    mp_grow_result r;
    mp_err e = {{0}};
    // a dump already packed
    copy_tree(V2, TMP "/v2packed");
    put_dated(TMP "/v2packed/ampr_zstd.index", "x", 1, 1700000000);
    MP_CHECK(grow_with(G_P5Z, TMP "/v2packed", &o, &r, &e) != 0);
    MP_CHECK_STR(r.code, "already_packed");
    // a cap the .p5z would go over
    o.max_size = olen + 1000;
    MP_CHECK(grow_with(G_P5Z, V2, &o, &r, &e) != 0);
    MP_CHECK_STR(r.code, "too_big");
    o.max_size = 0;
    // more volumes than it may have
    mp_p5z *p = NULL;
    uint32_t nvol = 0;
    MP_CHECK(mp_p5z_open(G_P5Z, &p, &e) == 0);
    for (size_t i = 0; p && i < p->nfiles; i++) nvol += p->files[i].role == MP_ROLE_VOLUME;
    mp_p5z_close(p);
    // one more volume may be added, and a new file that does not compress needs three: the third stops it
    copy_tree(V2, TMP "/v2b");
    uint8_t *noise = malloc(600000);
    uint32_t x = 2463534242u;
    for (size_t i = 0; i < 600000; i++) {
        x ^= x << 13;
        x ^= x >> 17;
        x ^= x << 5;
        noise[i] = (uint8_t)(x >> 24);
    }
    put_dated(TMP "/v2b/data/rand.bin", noise, 600000, 1700300000);
    free(noise);
    o.max_volumes = nvol + 1;
    MP_CHECK(grow_with(G_P5Z, TMP "/v2b", &o, &r, &e) != 0);
    MP_CHECK_STR(r.code, "too_many_volumes");
    MP_CHECK(starts_with(G_P5Z, orig, olen) && size_of(G_P5Z) == olen);
    o.max_volumes = nvol;   // none may be added: refused before anything is written
    MP_CHECK(grow_with(G_P5Z, V2, &o, &r, &e) != 0);
    MP_CHECK_STR(r.code, "too_many_volumes");
    o.max_volumes = 0;
    // more than half of it changed, without force
    o.force = 0;
    copy_tree(V2, TMP "/v3");
    size_t n = 0;
    uint8_t *b = mpt_slurp(V1 "/data/big.bin", &n);
    for (int k = 0; k < 3; k++) {
        char path[256];
        snprintf(path, sizeof path, TMP "/v3/data/more%d.bin", k);
        put_dated(path, b, n, 1700200000 + k);
    }
    free(b);
    MP_CHECK(grow_with(G_P5Z, TMP "/v3", &o, &r, &e) != 0);
    MP_CHECK_STR(r.code, "mostly_changed");
    MP_CHECK(starts_with(G_P5Z, orig, olen) && size_of(G_P5Z) == olen);
    MP_CHECK(mp_path_kind(G_P5Z MP_P5Z_JOURNAL) == 0);
    // another MaxPack growing it: busy
    mp_wfile *f = NULL;
    int busy = 0;
    o.force = 1;
    MP_CHECK(mp_wfile_reopen_locked(G_P5Z, &f, &busy, &e) == 0);
    MP_CHECK(grow_with(G_P5Z, V2, &o, &r, &e) != 0);
    MP_CHECK_STR(r.code, "busy");
    mp_wfile_close(f, NULL);
    MP_CHECK(starts_with(G_P5Z, orig, olen) && size_of(G_P5Z) == olen);
    free(orig);
}

MP_TEST(grow_nothing_changed) {
    mpt_clear(TMP);
    make_versions();
    if (mpt_pack(V1, G_P5Z, 1, NULL) != 0) return;
    size_t olen = 0;
    uint8_t *orig = mpt_slurp(G_P5Z, &olen);
    mp_grow_opts o = quiet_opts();
    mp_grow_result r;
    mp_err e = {{0}};
    MP_CHECK(grow_with(G_P5Z, V1, &o, &r, &e) == 0);
    MP_CHECK(r.nothing && r.new_files == 0 && r.changed_files == 0 && r.removed_files == 0);
    MP_CHECK(starts_with(G_P5Z, orig, olen) && size_of(G_P5Z) == olen);
    free(orig);
}

// ---- the readers of a .p5z that grew -------------------------------------------------------------

// v1 packed with small volumes and grown to v2 at G_P5Z.
static int grown_v2(uint64_t limit, uint8_t **orig, size_t *olen) {
    mpt_clear(TMP);
    make_versions();
    if (mpt_pack_vol(V1, G_P5Z, 1, NULL, limit) != 0) return -1;
    if (orig) *orig = mpt_slurp(G_P5Z, olen);
    mp_grow_opts o = quiet_opts();
    o.volume_limit = limit;
    mp_grow_result r;
    mp_err e = {{0}};
    if (grow_with(G_P5Z, V2, &o, &r, &e) != 0) {
        MP_FAIL_("grow: %s", e.msg);
        return -1;
    }
    return 0;
}

// unpack --folder of a .p5z that grew lays its volumes out again: the folder pack --folder makes of v2,
// which ampr-emu-ng mounts outside p5z mode too.
MP_TEST(grown_unpack_folder_equals_pack_dir) {
    if (grown_v2(262208, NULL, NULL) != 0) return;
    if (mpt_pack_vol(V2, TMP "/v2_folder", 0, NULL, 262208) != 0) return;
    mp_err e = {{0}};
    mp_p5z *p = NULL;
    MP_CHECK(mp_p5z_open(G_P5Z, &p, &e) == 0);
    if (!p) return;
    MP_CHECK(mp_unpack_folder_ex(p, TMP "/g_folder", 262208, 2, 0, &e) == 0);
    if (e.msg[0]) fprintf(stderr, "  %s\n", e.msg);
    mp_p5z_close(p);
    MP_CHECK(mpt_dirs_equal(TMP "/v2_folder", TMP "/g_folder", "*.pak"));
    mp_verify_result vr;
    MP_CHECK(mp_verify_folder(TMP "/g_folder", V2, 2, 0, &vr, &e) == 0);
    MP_CHECK_EQ_U64(vr.files_bad, 0);
    // and with the volumes of a real game (4 GiB), what unpack --folder gives: one here
    MP_CHECK(mp_p5z_open(G_P5Z, &p, &e) == 0);
    MP_CHECK(p && mp_unpack_folder(p, TMP "/g_folder4", 2, 0, &e) == 0);
    mp_p5z_close(p);
    MP_CHECK(mp_path_kind(TMP "/g_folder4/ampr_zstd-000.pak") == 1 && mp_path_kind(TMP "/g_folder4/ampr_zstd-001.pak") == 0);
    MP_CHECK(mp_verify_folder(TMP "/g_folder4", V2, 2, 0, &vr, &e) == 0);
    MP_CHECK_EQ_U64(vr.files_bad, 0);
}

MP_TEST(grown_verify_catches_damage) {
    if (grown_v2(262208, NULL, NULL) != 0) return;
    mp_err e = {{0}};
    mp_p5z *p = NULL;
    uint64_t at = 0;
    MP_CHECK(mp_p5z_open(G_P5Z, &p, &e) == 0);
    for (size_t i = 0; p && i < p->nfiles; i++)   // the last volume, a new one
        if (p->files[i].role == MP_ROLE_VOLUME) at = p->files[i].data_off + 100;
    mp_p5z_close(p);
    size_t n = 0;
    uint8_t *b = mpt_slurp(G_P5Z, &n);
    if (!b || at >= n) return;
    b[at] ^= 0x40;
    mpt_clear(TMP "/bad.p5z");
    mpt_put(TMP "/bad.p5z", b, n);
    free(b);
    mp_verify_result vr;
    MP_CHECK(mp_verify_p5z(TMP "/bad.p5z", 2, 0, &vr, &e) == 0);
    MP_CHECK(vr.files_bad > 0);
}

MP_TEST(grown_unused) {
    uint8_t *orig = NULL;
    size_t olen = 0;
    if (grown_v2(MP_ZPK_VOLUME_LIMIT, &orig, &olen) != 0) return;
    mp_err e = {{0}};
    mp_p5z *v1 = NULL, *p = NULL;
    uint64_t u1 = 1, u = 0;
    mpt_clear(TMP "/orig.p5z");
    mpt_put(TMP "/orig.p5z", orig, olen);
    MP_CHECK(mp_p5z_open(TMP "/orig.p5z", &v1, &e) == 0 && mp_p5z_unused(v1, &u1, &e) == 0);
    MP_CHECK_EQ_U64(u1, 0);
    MP_CHECK(mp_p5z_open(G_P5Z, &p, &e) == 0 && mp_p5z_unused(p, &u, &e) == 0);
    // what v2 no longer uses of v1: its index and footer, the regions of the loose files and originals it
    // changed or removed (and the old ampr_emu.index, ampr_zstd.index), the chunks of the packed ones
    uint64_t want = v1 ? v1->index_len + P5Z_FOOTER : 0;
    for (size_t i = 0; v1 && p && i < v1->nfiles; i++) {
        const p5z_file *f = &v1->files[i];
        if (f->role == MP_ROLE_VOLUME) continue;
        int kept = 0;
        for (size_t k = 0; k < p->nfiles; k++) kept |= p->files[k].data_off == f->data_off && p->files[k].role == f->role;
        if (!kept) want += f->data_len;
    }
    mp_zpk *z1 = NULL, *z2 = NULL;
    if (v1 && p && mp_p5z_zpk(v1, &z1, &e) == 0 && mp_p5z_zpk(p, &z2, &e) == 0) {
        for (size_t i = 0; i < mp_zpk_count(z1); i++) {
            uint64_t cnt = 0, cnt2 = 0;
            const uint64_t *c = mp_zpk_file_chunks(z1, i, &cnt);
            long j = mp_zpk_find(z2, mp_zpk_path(z1, i));
            const uint64_t *c2 = j >= 0 ? mp_zpk_file_chunks(z2, (size_t)j, &cnt2) : NULL;
            if (c && !(c2 && cnt2 == cnt && !memcmp(c, c2, cnt * 8)))
                for (uint64_t k = 0; k < cnt; k++) want += ((c[k] >> 44) & 0x3FFFF) + 1;
        }
    }
    mp_zpk_close(z1);
    mp_zpk_close(z2);
    MP_CHECK(u > 0);
    MP_CHECK_EQ_U64(u, want);
    mp_p5z_close(v1);
    mp_p5z_close(p);
    free(orig);
}

MP_TEST(play_json_says_index_version) {
    if (grown_v2(262208, NULL, NULL) != 0) return;
    char err[256] = "";
    mp_play *pl = mp_play_open(G_P5Z, err, sizeof err);
    char *j = pl ? mp_play_json(pl) : NULL;
    MP_CHECK(j && strstr(j, ",\"index_version\":2,\"grown\":1}"));
    mp_play_free_str(j);
    mp_play_close(pl);
    // a .p5z as packed says what it said before
    if (mpt_pack(V1, TMP "/plain.p5z", 1, NULL) != 0) return;
    pl = mp_play_open(TMP "/plain.p5z", err, sizeof err);
    j = pl ? mp_play_json(pl) : NULL;
    MP_CHECK(j && !strstr(j, "index_version") && !strstr(j, "grown") && strstr(j, "]}"));
    mp_play_free_str(j);
    mp_play_close(pl);
}

MP_TEST(p5z_info_says_grown) {
    if (grown_v2(MP_ZPK_VOLUME_LIMIT, NULL, NULL) != 0) return;
    mp_err e = {{0}};
    mp_p5z *p = NULL;
    MP_CHECK(mp_p5z_open(G_P5Z, &p, &e) == 0);
    if (!p) return;
    mpt_clear(TMP "/info.txt");
    FILE *f = fopen(TMP "/info.txt", "w");
    MP_CHECK(f && mp_p5z_info(p, f) == 0);
    if (f) fclose(f);
    mp_p5z_close(p);
    size_t n = 0;
    char *t = (char *)mpt_slurp(TMP "/info.txt", &n);
    if (t) t[n] = 0;
    MP_CHECK_CONTAINS(t, "Grown:      1 time");
    MP_CHECK_CONTAINS(t, "Unused:     ");
    MP_CHECK_CONTAINS(t, "maxpack compact");
    free(t);
}

static void inst_note(void *ctx, const char *msg) {
    (void)ctx;
    (void)msg;
}

// The installer leaves the packs of a .p5z as a folder, which a grown one's are not: it refuses it.
MP_TEST(install_refuses_grown) {
    if (grown_v2(MP_ZPK_VOLUME_LIMIT, NULL, NULL) != 0) return;
    static uint8_t rt[4096];
    memcpy(rt + 64, "AMPRZPK1", 8);
    static const char *drives[] = {TMP "/usb1"};
    mp_inst_env env;
    mp_inst_env_default(&env);
    env.internal = TMP "/internal";
    env.drive_candidates = drives;
    env.ndrive_candidates = 1;
    env.runtime.data = rt;
    env.runtime.len = sizeof rt;
    env.runtime.mtime = 1700000000;
    env.runtime.version = "test";
    env.notify = inst_note;
    mp_err e = {{0}};
    mp_make_dirs(TMP "/internal", &e);
    mp_inst_req q;
    memset(&q, 0, sizeof q);
    q.p5z_path = G_P5Z;
    q.dest_root = TMP "/internal";
    mp_inst_result r;
    memset(&r, 0, sizeof r);
    MP_CHECK(mp_inst_install(&env, &q, &r, &e) != 0);
    MP_CHECK(r.status == MP_INST_FAILED);
    MP_CHECK_STR(r.code, "grown_p5z");
    MP_CHECK_CONTAINS(e.msg, "maxpack compact");
    mp_inst_result_free(&r);
}

// ---- add: DLC into a .p5z already made ------------------------------------------------------------

#define DLC_DATA MP_FIXTURES_DIR "/dlc_data"
#define DLC_LICENSE MP_FIXTURES_DIR "/dlc_license"
#define DLC2 TMP "/dlc2"

// Packs game with the DLC dlc[0..n) into a .p5z at out (level 19, as mpt_pack).
static int pack_dlc_p5z(const char *game, const char *const *dlc, size_t n, const char *out, uint64_t limit) {
    mp_err e = {{0}};
    mpt_clear(out);
    mpt_parent_dirs(out);
    mp_pack_opts o;
    mp_pack_opts_default(&o);
    o.threads = 4;
    o.progress = 0;
    o.dlc = dlc;
    o.ndlc = n;
    o.volume_limit = limit;
    mp_pack_out po;
    mp_pack_result r;
    if (mp_pack_out_p5z(out, 4, &po, &e) != 0) return -1;
    int rc = mp_pack_run(game, &po, &o, &r, &e);
    po.destroy(po.ctx);
    if (rc) MP_FAIL_("pack --dlc: %s", e.msg);
    return rc;
}

static int unpack_original_to(const char *p5z, const char *dir) {
    mp_err e = {{0}};
    mp_p5z *p = NULL;
    mpt_clear(dir);
    int rc = mp_p5z_open(p5z, &p, &e);
    if (!rc) rc = mp_unpack_original(p, dir, 2, 0, &e);
    if (rc) MP_FAIL_("unpack --original %s: %s", p5z, e.msg);
    mp_p5z_close(p);
    return rc;
}

// A second DLC with files of its own: dlc_data with another content ID.
static void make_dlc2(void) {
    copy_tree(DLC_DATA, DLC2);
    const char *pj = "{\"contentId\": \"UP0000-PPSA00000_00-MAXPACKDLCDATA02\", \"contentVersion\": \"01.000.000\", "
                     "\"localizedParameters\": {\"defaultLanguage\": \"en-US\", \"en-US\": {\"titleName\": \"Two\"}}}";
    put_dated(DLC2 "/sce_sys/param.json", pj, strlen(pj), 1700400000);
    put_dated(DLC2 "/data/two.bin", "the second DLC's own file\n", 26, 1700400001);
}

static char *read_text(const char *path) {
    size_t n = 0;
    char *t = (char *)mpt_slurp(path, &n);
    if (t) t[n] = 0;
    return t;
}

MP_TEST(add_dlc_unpack_original) {
    mpt_clear(TMP);
    if (mpt_pack(GAME, A_P5Z, 1, NULL) != 0) return;
    size_t olen = 0;
    uint8_t *orig = mpt_slurp(A_P5Z, &olen);
    const char *dlc[] = {DLC_DATA, DLC_LICENSE};
    mp_grow_opts o = quiet_opts();
    o.force = 0;   // add never asks, however much the DLC take
    mp_grow_result r;
    mp_err e = {{0}};
    MP_CHECK(mp_grow_add(A_P5Z, dlc, 2, &o, &r, &e) == 0);
    if (e.msg[0]) fprintf(stderr, "  %s\n", e.msg);
    MP_CHECK(starts_with(A_P5Z, orig, olen));
    MP_CHECK_EQ_U64(r.dlc_data, 1);
    MP_CHECK_EQ_U64(r.dlc_license, 1);
    MP_CHECK(r.dlc_bytes > 0 && r.new_files > 3 && r.changed_files == 0 && r.removed_files == 0);
    if (pack_dlc_p5z(GAME, dlc, 2, TMP "/fresh.p5z", MP_ZPK_VOLUME_LIMIT) != 0) return;
    if (unpack_original_to(A_P5Z, TMP "/a_orig") != 0 || unpack_original_to(TMP "/fresh.p5z", TMP "/f_orig") != 0)
        return;
    MP_CHECK(mpt_dirs_equal(TMP "/f_orig", TMP "/a_orig", NULL));
    char *ini = read_text(TMP "/a_orig/dlc_emu.ini");
    MP_CHECK_STR(ini, "[PSAC]\ncontent_id=UP0000-PPSA00000_00-MAXPACKDLCDATA01\nmount_point=/app0/addcont0\n\n[PSAL]\n"
                      "content_id=UP0000-PPSA00000_00-MAXPACKLICENSE01\ndownload_status=NO_EXTRA_DATA\n");
    free(ini);
    mp_verify_result vr;
    MP_CHECK(mp_verify_p5z(A_P5Z, 2, 0, &vr, &e) == 0 && vr.files_bad == 0);
    free(orig);
}

MP_TEST(add_second_dlc_numbering) {
    mpt_clear(TMP);
    make_dlc2();
    const char *one[] = {DLC_DATA}, *both[] = {DLC_DATA, DLC2}, *two[] = {DLC2};
    if (pack_dlc_p5z(GAME, one, 1, A_P5Z, MP_ZPK_VOLUME_LIMIT) != 0) return;
    mp_grow_opts o = quiet_opts();
    mp_grow_result r;
    mp_err e = {{0}};
    MP_CHECK(mp_grow_add(A_P5Z, two, 1, &o, &r, &e) == 0);
    if (e.msg[0]) fprintf(stderr, "  %s\n", e.msg);
    MP_CHECK_EQ_U64(r.dlc_data, 1);
    if (pack_dlc_p5z(GAME, both, 2, TMP "/fresh.p5z", MP_ZPK_VOLUME_LIMIT) != 0) return;
    if (unpack_original_to(A_P5Z, TMP "/a_orig") != 0 || unpack_original_to(TMP "/fresh.p5z", TMP "/f_orig") != 0)
        return;
    MP_CHECK(mpt_dirs_equal(TMP "/f_orig", TMP "/a_orig", NULL));
    MP_CHECK(mp_path_kind(TMP "/a_orig/addcont1/data/two.bin") == 1);
    char *ini = read_text(TMP "/a_orig/dlc_emu.ini");
    MP_CHECK_STR(ini, "[PSAC]\ncontent_id=UP0000-PPSA00000_00-MAXPACKDLCDATA01\nmount_point=/app0/addcont0\n\n[PSAC]\n"
                      "content_id=UP0000-PPSA00000_00-MAXPACKDLCDATA02\nmount_point=/app0/addcont1\n");
    free(ini);
}

MP_TEST(add_refusals) {
    mpt_clear(TMP);
    const char *one[] = {DLC_DATA};
    if (pack_dlc_p5z(GAME, one, 1, A_P5Z, MP_ZPK_VOLUME_LIMIT) != 0) return;
    size_t olen = 0;
    uint8_t *orig = mpt_slurp(A_P5Z, &olen);
    mp_grow_opts o = quiet_opts();
    mp_grow_result r;
    mp_err e = {{0}};
    // the same DLC again
    MP_CHECK(mp_grow_add(A_P5Z, one, 1, &o, &r, &e) != 0);
    MP_CHECK_STR(r.code, "dlc_refused");
    MP_CHECK_CONTAINS(e.msg, "already");
    // another game's DLC
    const char *other[] = {MP_FIXTURES_DIR "/dlc_other.pkg"};
    MP_CHECK(mp_grow_add(A_P5Z, other, 1, &o, &r, &e) != 0);
    MP_CHECK_STR(r.code, "dlc_refused");
    MP_CHECK(starts_with(A_P5Z, orig, olen) && size_of(A_P5Z) == olen);
    // a game with a dlc_emu.ini of its own, written by hand
    copy_tree(GAME, TMP "/hand");
    const char *mine = "; mine\n[PSAL]\ncontent_id=UP0000-PPSA00000_00-MAXPACKLICENSE01\n";
    put_dated(TMP "/hand/dlc_emu.ini", mine, strlen(mine), 1700000000);
    if (mpt_pack(TMP "/hand", TMP "/hand.p5z", 1, NULL) != 0) return;
    const char *lic[] = {DLC_DATA};
    MP_CHECK(mp_grow_add(TMP "/hand.p5z", lic, 1, &o, &r, &e) != 0);
    MP_CHECK_STR(r.code, "dlc_has_ini");
    free(orig);
}

int main(void) {
    MP_RUN(p5z_append_one_file);
    MP_RUN(p5z_copy_all_is_identical);
    MP_RUN(p5z_v1_only_refuses_v2);
    MP_RUN(p5z_v2_damaged);
    MP_RUN(p5z_icon_of_v2);
    MP_RUN(journal_view_and_restore);
    MP_RUN(recover_busy_leaves_everything);
    MP_RUN(recover_stale_journal);
    MP_RUN(recover_damaged_or_tmp);
    MP_RUN(grow_unpack_original_is_v2);
    MP_RUN(grow_play_lists_every_volume);
    MP_RUN(grow_fault_every_step);
    MP_RUN(grow_refuses);
    MP_RUN(grow_nothing_changed);
    MP_RUN(grown_unpack_folder_equals_pack_dir);
    MP_RUN(grown_verify_catches_damage);
    MP_RUN(grown_unused);
    MP_RUN(play_json_says_index_version);
    MP_RUN(p5z_info_says_grown);
    MP_RUN(install_refuses_grown);
    MP_RUN(add_dlc_unpack_original);
    MP_RUN(add_second_dlc_numbering);
    MP_RUN(add_refusals);
    MP_TEST_MAIN_END();
}
