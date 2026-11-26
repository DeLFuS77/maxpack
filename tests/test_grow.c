// SPDX-License-Identifier: GPL-3.0-or-later
// Growing a .p5z by appending: index version 2, the journal that undoes an append that stopped, the
// grow engine (add, update), compact, and every reader of a .p5z that grew.
#define ZSTD_STATIC_LINKING_ONLY
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <zstd.h>

#include "mp_fixture.h"
#include "mp_hash.h"
#include "mp_p5z_icon.h"
#include "mp_p5z_internal.h"

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
    MP_TEST_MAIN_END();
}
