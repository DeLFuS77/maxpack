// SPDX-License-Identifier: GPL-3.0-or-later
// The .p5z icon reader of the file manager thumbnails: the icon it gives, and every file it refuses
// (half copied, not a .p5z, damaged, or declaring sizes over its caps) without reading or allocating them.
#include <stdio.h>
#include <stdlib.h>
#include <zstd.h>

#include "mp_describe.h"
#include "mp_fixture.h"
#include "mp_hash.h"
#include "mp_p5z_icon.h"

#define TMP MP_TEST_TMP "/p5z_icon"
#define ICON "sce_sys/icon0.png"
#define FRAME_REC_T 16u   // a frame record of the index

static uint32_t rd32(const uint8_t *p) { return p[0] | p[1] << 8 | p[2] << 16 | (uint32_t)p[3] << 24; }
static uint64_t rd64(const uint8_t *p) { return rd32(p) | (uint64_t)rd32(p + 4) << 32; }
static void wr32(uint8_t *p, uint32_t v) {
    for (int i = 0; i < 4; i++) p[i] = (uint8_t)(v >> (8 * i));
}
static void wr64(uint8_t *p, uint64_t v) {
    for (int i = 0; i < 8; i++) p[i] = (uint8_t)(v >> (8 * i));
}

// ---- a .p5z in memory as the reader's source -----------------------------------------------------

typedef struct {
    const uint8_t *d;
    size_t n;          // bytes held; the source may say it is larger (virtual zeros past them)
    size_t max_read;   // the largest single read asked for
    uint64_t total;    // bytes read in all
    uint64_t fail_from;   // reads at or past this offset fail (UINT64_MAX: none)
} memsrc;

static int mem_read_at(void *ctx, void *buf, size_t n, uint64_t off) {
    memsrc *m = ctx;
    if (n > m->max_read) m->max_read = n;
    m->total += n;
    if (off + n > m->fail_from) return -1;
    if (off > m->n || n > m->n - off) return -1;
    memcpy(buf, m->d + off, n);
    return 0;
}

static int read_icon(memsrc *m, uint64_t size, uint8_t **png, size_t *len, char *err, size_t errlen) {
    mp_icon_src s = {mem_read_at, size, m};
    *png = NULL;
    *len = 0;
    err[0] = 0;
    return mp_p5z_icon_read(&s, png, len, err, errlen);
}

static memsrc src_of(const uint8_t *d, size_t n) {
    memsrc m = {d, n, 0, 0, UINT64_MAX};
    return m;
}

// ---- games and .p5z files ----------------------------------------------------------------------

static uint8_t *pattern(size_t n, uint32_t seed) {
    uint8_t *b = malloc(n ? n : 1);
    uint32_t x = seed * 2654435761u + 1;
    for (size_t i = 0; i < n; i++) {
        x ^= x << 13;
        x ^= x >> 17;
        x ^= x << 5;
        b[i] = (uint8_t)x;
    }
    return b;
}

// A game folder with param.json, eboot.bin, a file that goes into the packs and, if icon_path, an
// icon there; packed to dir.p5z, whose bytes it returns.
static uint8_t *game_p5z(const char *name, const char *icon_path, const uint8_t *icon, size_t icon_n,
                         size_t big, size_t *n) {
    char dir[512], f[600], p5z[600];
    snprintf(dir, sizeof dir, TMP "/%s", name);
    snprintf(p5z, sizeof p5z, TMP "/%s.p5z", name);
    mpt_clear(dir);
    snprintf(f, sizeof f, "%s/sce_sys/param.json", dir);
    mpt_put_text(f, "{\"titleId\":\"ICON00002\",\"contentVersion\":\"01.000.000\"}");
    snprintf(f, sizeof f, "%s/eboot.bin", dir);
    mpt_put_text(f, "eboot");
    uint8_t *b = pattern(big, 7);
    snprintf(f, sizeof f, "%s/data/big.bin", dir);
    mpt_put(f, b, big);
    free(b);
    if (icon_path) {
        snprintf(f, sizeof f, "%s/%s", dir, icon_path);
        mpt_put(f, icon, icon_n);
    }
    *n = 0;
    if (mpt_pack(dir, p5z, 1, NULL) != 0) return NULL;
    return mpt_slurp(p5z, n);
}

static uint8_t *icon_p5z(const uint8_t *icon, size_t icon_n, size_t *n) {
    return game_p5z("game", ICON, icon, icon_n, 64u << 10, n);
}

// The .p5z's index, decompressed.
static uint8_t *index_of(const uint8_t *d, size_t n, size_t *ixn) {
    const uint8_t *ft = d + n - 32;
    uint64_t ioff = rd64(ft + 8), ilen = rd64(ft + 16);
    unsigned long long us = ZSTD_getFrameContentSize(d + ioff, (size_t)ilen);
    uint8_t *ix = malloc((size_t)us);
    if (ZSTD_decompress(ix, (size_t)us, d + ioff, (size_t)ilen) != us) MP_FAIL_("could not decompress the index");
    *ixn = (size_t)us;
    return ix;
}

// The index's file record with that path.
static uint8_t *record(uint8_t *ix, size_t ixn, const char *path) {
    uint32_t nf = rd32(ix + 12);
    uint64_t slen = rd64(ix + 24);
    const uint8_t *strings = ix + ixn - slen;
    for (uint32_t i = 0; i < nf; i++) {
        uint8_t *r = ix + 56 + 88ull * i;
        if (rd32(r + 4) == strlen(path) && !memcmp(strings + rd32(r), path, strlen(path))) return r;
    }
    MP_FAIL_("no record %s", path);
    return NULL;
}

// The same .p5z with extra bytes where its index was and the index after them, as ix says, with
// the footer and its CRCs recalculated (an attacker can compute them too).
static uint8_t *with_index(const uint8_t *d, size_t n, const uint8_t *extra, size_t extra_n, const uint8_t *ix,
                           size_t ixn, size_t *out_n) {
    uint64_t ioff = rd64(d + n - 32 + 8);
    size_t cap = ZSTD_compressBound(ixn);
    uint8_t *cz = malloc(cap);
    size_t cl = ZSTD_compress(cz, cap, ix, ixn, 3);
    size_t m = (size_t)ioff + extra_n + cl + 32;
    uint8_t *out = malloc(m);
    memcpy(out, d, (size_t)ioff);
    if (extra_n) memcpy(out + ioff, extra, extra_n);
    memcpy(out + ioff + extra_n, cz, cl);
    uint8_t *ft = out + m - 32;
    memset(ft, 0, 32);
    memcpy(ft, "P5ZFOOT1", 8);
    wr64(ft + 8, ioff + extra_n);
    wr64(ft + 16, cl);
    wr32(ft + 24, mp_crc32(cz, cl, 0));
    wr32(ft + 28, mp_crc32(ft, 28, 0));
    free(cz);
    *out_n = m;
    return out;
}

static void fix_footer_crc(uint8_t *ft) { wr32(ft + 28, mp_crc32(ft, 28, 0)); }

static void put_file(const char *path, const uint8_t *d, size_t n) {
    remove(path);
    mpt_put(path, d, n);
}

// ---- the icon ----------------------------------------------------------------------------------

// The same bytes that `maxpack icon` writes for the same .p5z, and those of the game's icon; it
// reads the header, the footer, the index and the icon, not the packs (1 MiB here).
MP_TEST(thumb_reads_icon) {
    uint8_t *icon = pattern(200000, 3);
    size_t n = 0;
    uint8_t *d = game_p5z("reads", ICON, icon, 200000, 1u << 20, &n);
    MP_CHECK(d && n > (1u << 20));
    if (!d) {
        free(icon);
        return;
    }
    memsrc m = src_of(d, n);
    uint8_t *png = NULL;
    size_t len = 0;
    char err[256];
    MP_CHECK(read_icon(&m, n, &png, &len, err, sizeof err) == 0);
    MP_CHECK_EQ_U64(len, 200000);
    MP_CHECK(png && len == 200000 && !memcmp(png, icon, len));
    MP_CHECK(m.total < 300000);

    mp_err e = {{0}};
    uint8_t *cli = NULL;
    size_t cli_n = 0;
    MP_CHECK(mp_describe_icon(TMP "/reads.p5z", &cli, &cli_n, &e) == 0);
    MP_CHECK(cli && png && cli_n == len && !memcmp(cli, png, len));
    free(cli);
    free(png);
    free(d);
    free(icon);
}

// An icon whose name differs in case is found too.
MP_TEST(thumb_icon_any_case) {
    uint8_t *icon = pattern(5000, 4);
    size_t n = 0;
    uint8_t *d = game_p5z("case", "sce_sys/Icon0.PNG", icon, 5000, 1000, &n);
    MP_CHECK(d != NULL);
    if (d) {
        memsrc m = src_of(d, n);
        uint8_t *png = NULL;
        size_t len = 0;
        char err[256];
        MP_CHECK(read_icon(&m, n, &png, &len, err, sizeof err) == 0);
        MP_CHECK(png && len == 5000 && !memcmp(png, icon, len));
        free(png);
    }
    free(d);
    free(icon);
}

// An icon stored as is (raw), which the format allows for any file, reads the same.
MP_TEST(thumb_raw_icon) {
    uint8_t *icon = pattern(30000, 5);
    size_t n = 0, ixn = 0, rn = 0;
    uint8_t *d = icon_p5z(icon, 30000, &n);
    if (!d) {
        free(icon);
        return;
    }
    uint64_t ioff = rd64(d + n - 32 + 8);
    uint8_t *ix = index_of(d, n, &ixn);
    uint8_t *r = record(ix, ixn, ICON);
    if (r) {
        r[9] = 0;   // raw
        wr64(r + 28, ioff);
        wr64(r + 36, 30000);
        wr32(r + 44, 0);
        wr32(r + 48, 0);
    }
    uint8_t *raw = with_index(d, n, icon, 30000, ix, ixn, &rn);
    put_file(TMP "/raw.p5z", raw, rn);
    mp_err e = {{0}};
    uint8_t *cli = NULL;
    size_t cli_n = 0;
    MP_CHECK(mp_describe_icon(TMP "/raw.p5z", &cli, &cli_n, &e) == 0);   // the engine reads it too
    memsrc m = src_of(raw, rn);
    uint8_t *png = NULL;
    size_t len = 0;
    char err[256];
    MP_CHECK(read_icon(&m, rn, &png, &len, err, sizeof err) == 0);
    MP_CHECK(png && len == 30000 && !memcmp(png, icon, len));
    MP_CHECK(cli && png && cli_n == len && !memcmp(cli, png, len));
    free(cli);
    free(png);
    free(raw);
    free(ix);
    free(d);
    free(icon);
}

MP_TEST(thumb_no_icon) {
    size_t n = 0;
    uint8_t *d = game_p5z("noicon", NULL, NULL, 0, 1000, &n);
    MP_CHECK(d != NULL);
    if (!d) return;
    memsrc m = src_of(d, n);
    uint8_t *png = NULL;
    size_t len = 0;
    char err[256];
    MP_CHECK(read_icon(&m, n, &png, &len, err, sizeof err) == 1);
    MP_CHECK(png == NULL && len == 0);
    MP_CHECK_CONTAINS(err, "no " ICON);
    free(d);
}

// ---- what it refuses ---------------------------------------------------------------------------

// Still being copied: one byte short, or half of it (the footer is not there yet).
MP_TEST(thumb_truncated) {
    uint8_t *icon = pattern(1000, 6);
    size_t n = 0;
    uint8_t *d = icon_p5z(icon, 1000, &n);
    free(icon);
    if (!d) return;
    uint64_t sizes[] = {n - 1, n / 2, 96, 95, 0};
    for (size_t i = 0; i < sizeof sizes / sizeof *sizes; i++) {
        memsrc m = src_of(d, (size_t)sizes[i]);
        uint8_t *png = NULL;
        size_t len = 0;
        char err[256];
        if (read_icon(&m, sizes[i], &png, &len, err, sizeof err) != -1 || png || !err[0])
            MP_FAIL_("size %llu: not refused (%s)", (unsigned long long)sizes[i], err);
    }
    memsrc m = src_of(d, (size_t)n - 1);
    uint8_t *png = NULL;
    size_t len = 0;
    char err[256];
    MP_CHECK(read_icon(&m, n - 1, &png, &len, err, sizeof err) == -1);
    MP_CHECK_CONTAINS(err, "footer");
    // the same file whose reads fail past the header (a copy that went away)
    memsrc g = src_of(d, n);
    g.fail_from = 64;
    MP_CHECK(read_icon(&g, n, &png, &len, err, sizeof err) == -1);
    MP_CHECK(png == NULL && err[0]);
    free(d);
}

// Anything else renamed to .p5z.
MP_TEST(thumb_not_p5z) {
    uint8_t *junk = pattern(4096, 9);
    memsrc m = src_of(junk, 4096);
    uint8_t *png = NULL;
    size_t len = 0;
    char err[256];
    MP_CHECK(read_icon(&m, 4096, &png, &len, err, sizeof err) == -1);
    MP_CHECK(png == NULL);
    MP_CHECK_CONTAINS(err, "not a .p5z");
    // a .p5z header from a newer version
    size_t n = 0;
    uint8_t *icon = pattern(1000, 6);
    uint8_t *d = icon_p5z(icon, 1000, &n);
    free(icon);
    if (d) {
        wr32(d + 8, 2);
        uint8_t h[64];
        memcpy(h, d, 64);
        memset(h + 60, 0, 4);
        wr32(d + 60, mp_crc32(h, 64, 0));
        memsrc v = src_of(d, n);
        MP_CHECK(read_icon(&v, n, &png, &len, err, sizeof err) == -1);
        MP_CHECK_CONTAINS(err, "newer version");
    }
    free(d);
    free(junk);
    // err may be NULL
    mp_icon_src s = {mem_read_at, 4096, &m};
    MP_CHECK(mp_p5z_icon_read(&s, &png, &len, NULL, 0) == -1);
}

MP_TEST(thumb_bad_footer_crc) {
    uint8_t *icon = pattern(1000, 6);
    size_t n = 0;
    uint8_t *d = icon_p5z(icon, 1000, &n);
    free(icon);
    if (!d) return;
    uint8_t *ft = d + n - 32;
    ft[8] ^= 1;   // the index offset, with the CRC left as it was
    memsrc m = src_of(d, n);
    uint8_t *png = NULL;
    size_t len = 0;
    char err[256];
    MP_CHECK(read_icon(&m, n, &png, &len, err, sizeof err) == -1);
    MP_CHECK_CONTAINS(err, "footer CRC");
    ft[8] ^= 1;
    d[n - 40] ^= 1;   // inside the index: its CRC
    MP_CHECK(read_icon(&m, n, &png, &len, err, sizeof err) == -1);
    MP_CHECK_CONTAINS(err, "index CRC");
    d[n - 40] ^= 1;
    d[30] ^= 1;   // the header: its CRC
    MP_CHECK(read_icon(&m, n, &png, &len, err, sizeof err) == -1);
    MP_CHECK_CONTAINS(err, "header CRC");
    MP_CHECK(png == NULL);
    free(d);
}

// The 2 GiB file of thumb_index_too_big: a 64-byte header, zeros, and a 32-byte footer at the end
// (m->d holds the header and the footer, one after the other).
#define FAR_SIZE (64 + (2ull << 30) + 32)
static int far_read_at(void *ctx, void *buf, size_t n, uint64_t off) {
    memsrc *m = ctx;
    if (n > m->max_read) m->max_read = n;
    if (off > FAR_SIZE || n > FAR_SIZE - off) return -1;
    uint8_t *b = buf;
    for (size_t i = 0; i < n; i++) {
        uint64_t p = off + i;
        b[i] = p < 64 ? m->d[p] : p >= FAR_SIZE - 32 ? m->d[64 + (p - (FAR_SIZE - 32))] : 0;
    }
    return 0;
}

// A footer that declares a 2 GiB index (in a file that is that large) and an index that declares
// it unpacks to 2 GiB: refused, without asking to read either.
MP_TEST(thumb_index_too_big) {
    uint8_t *icon = pattern(1000, 6);
    size_t n = 0;
    uint8_t *d = icon_p5z(icon, 1000, &n);
    free(icon);
    if (!d) return;
    uint64_t big = 2ull << 30;
    uint8_t far[96] = {0};
    memcpy(far, d, 64);
    uint8_t *ft = far + 64;
    memcpy(ft, "P5ZFOOT1", 8);
    wr64(ft + 8, 64);
    wr64(ft + 16, big);
    fix_footer_crc(ft);
    memsrc m = src_of(far, sizeof far);
    mp_icon_src s = {far_read_at, FAR_SIZE, &m};
    uint8_t *png = NULL;
    size_t len = 0;
    char err[256] = "";
    MP_CHECK(mp_p5z_icon_read(&s, &png, &len, err, sizeof err) == -1);
    MP_CHECK_CONTAINS(err, "index too large");
    MP_CHECK(m.max_read <= 64);
    MP_CHECK(png == NULL);

    // A compressed index whose frame says it unpacks to 2 GiB.
    uint8_t fr[] = {0x28, 0xB5, 0x2F, 0xFD, 0xC0, 0x00, 0, 0, 0, 0x80, 0, 0, 0, 0, 0x01, 0x00, 0x00};
    MP_CHECK_EQ_U64(ZSTD_getFrameContentSize(fr, sizeof fr), big);
    uint64_t ioff = rd64(d + n - 32 + 8);
    size_t hn = (size_t)ioff + sizeof fr + 32;
    uint8_t *h = malloc(hn);
    memcpy(h, d, (size_t)ioff);
    memcpy(h + ioff, fr, sizeof fr);
    uint8_t *hf = h + hn - 32;
    memset(hf, 0, 32);
    memcpy(hf, "P5ZFOOT1", 8);
    wr64(hf + 8, ioff);
    wr64(hf + 16, sizeof fr);
    wr32(hf + 24, mp_crc32(fr, sizeof fr, 0));
    fix_footer_crc(hf);
    memsrc hm = src_of(h, hn);
    MP_CHECK(read_icon(&hm, hn, &png, &len, err, sizeof err) == -1);
    MP_CHECK_CONTAINS(err, "index too large");
    MP_CHECK(hm.max_read <= 64);
    MP_CHECK(png == NULL);
    free(h);
    free(d);
}

// The icon's record pointing outside the data, or declaring an icon over the cap: refused before
// reading or allocating the icon (and the engine refuses the first ones too).
MP_TEST(thumb_region_outside) {
    uint8_t *icon = pattern(1000, 6);
    size_t n = 0, ixn = 0;
    uint8_t *d = icon_p5z(icon, 1000, &n);
    free(icon);
    if (!d) return;
    uint64_t ioff = rd64(d + n - 32 + 8);
    uint8_t *ix0 = index_of(d, n, &ixn);
    enum { PAST_INDEX, WRAPS, FRAMES_OUTSIDE, TOO_BIG, NCASES };
    const char *want[NCASES] = {"region", "region", "frames", "icon too large"};
    for (int c = 0; c < NCASES; c++) {
        uint8_t *ix = malloc(ixn);
        memcpy(ix, ix0, ixn);
        uint8_t *r = record(ix, ixn, ICON);
        if (!r) {
            free(ix);
            break;
        }
        if (c == PAST_INDEX) wr64(r + 28, ioff + 10);
        if (c == WRAPS) wr64(r + 36, UINT64_MAX - 10);
        if (c == FRAMES_OUTSIDE) wr32(r + 44, 1000000);
        if (c == TOO_BIG) wr64(r + 12, 2ull << 30);
        size_t m = 0;
        uint8_t *bad = with_index(d, n, NULL, 0, ix, ixn, &m);
        memsrc s = src_of(bad, m);
        uint8_t *png = NULL;
        size_t len = 0;
        char err[256];
        int rc = read_icon(&s, m, &png, &len, err, sizeof err);
        if (rc != -1 || png || !strstr(err, want[c]))
            MP_FAIL_("case %d: rc %d, \"%s\" (wants \"%s\")", c, rc, err, want[c]);
        if (s.max_read > ixn + 64) MP_FAIL_("case %d: read %zu bytes at once", c, s.max_read);
        if (c != TOO_BIG) {
            put_file(TMP "/outside.p5z", bad, m);
            mp_p5z *p = NULL;
            mp_err e = {{0}};
            MP_CHECK(mp_p5z_open(TMP "/outside.p5z", &p, &e) == -1);
            mp_p5z_close(p);
        }
        free(bad);
        free(ix);
    }
    free(ix0);
    free(d);
}

// A damaged icon frame: no icon, no crash.
MP_TEST(thumb_damaged_frame) {
    uint8_t *icon = pattern(20000, 8);
    size_t n = 0, ixn = 0;
    uint8_t *d = icon_p5z(icon, 20000, &n);
    free(icon);
    if (!d) return;
    uint8_t *ix = index_of(d, n, &ixn);
    uint8_t *r = record(ix, ixn, ICON);
    if (r) {
        uint64_t off = rd64(r + 28), dl = rd64(r + 36);
        memset(d + off + 4, 0xA5, (size_t)(dl - 8));   // inside the frame, past its magic number
        memsrc m = src_of(d, n);
        uint8_t *png = NULL;
        size_t len = 0;
        char err[256];
        MP_CHECK(read_icon(&m, n, &png, &len, err, sizeof err) == -1);
        MP_CHECK(png == NULL);
        MP_CHECK_CONTAINS(err, "damaged");
    }
    free(ix);
    free(d);
}

// The icon's frames, crafted so that every check of before holds and the read is still the icon: one
// frame padded with a skippable frame of 1 MiB (more than any zstd frame of its size takes), or the icon
// in 65 frames (the packer gives an icon of up to 8 MiB one). Each is refused before its frames are read,
// so a hostile .p5z cannot make the file manager read far more than an icon.
MP_TEST(thumb_frames_bounded) {
    const size_t isz = 20000;
    uint8_t *icon = pattern(isz, 9);
    size_t n = 0, ixn = 0;
    uint8_t *d = icon_p5z(icon, isz, &n);
    if (!d) {
        free(icon);
        return;
    }
    uint64_t ioff = rd64(d + n - 32 + 8);
    uint8_t *ix0 = index_of(d, n, &ixn);
    for (int c = 0; c < 2; c++) {
        const uint32_t nframes = c == 0 ? 1 : 65;
        uint32_t nf = rd32(ix0 + 12), nfr = rd32(ix0 + 16);
        // the index, with room for nframes more frame records at the end of its frames (case 1)
        size_t add = c == 0 ? 0 : (size_t)FRAME_REC_T * nframes, at = 56 + 88ull * nf + FRAME_REC_T * (size_t)nfr;
        size_t ixn2 = ixn + add;
        uint8_t *ix = malloc(ixn2);
        memcpy(ix, ix0, at);
        memcpy(ix + at + add, ix0 + at, ixn - at);
        wr32(ix + 16, nfr + (c == 0 ? 0 : nframes));
        uint8_t *r = record(ix, ixn2, ICON);
        uint32_t first = c == 0 ? rd32(r + 44) : nfr;
        // the region: the icon's frames, one after the other
        size_t cap = ZSTD_compressBound(isz) + nframes * 64 + (1u << 20) + 8, en = 0, done = 0;
        uint8_t *extra = calloc(cap, 1);
        for (uint32_t k = 0; k < nframes; k++) {
            size_t piece = k + 1 < nframes ? isz / nframes : isz - done;
            size_t cz = ZSTD_compress(extra + en, cap - en, icon + done, piece, 3);
            if (c == 0) {   // a skippable frame after it, inside the same frame record
                wr32(extra + en + cz, 0x184D2A50);
                wr32(extra + en + cz + 4, 1u << 20);
                cz += 8 + (1u << 20);
            }
            uint8_t *fr = ix + 56 + 88ull * nf + FRAME_REC_T * (size_t)(first + k);
            wr64(fr, ioff + en);
            wr32(fr + 8, (uint32_t)cz);
            wr32(fr + 12, (uint32_t)piece);
            en += cz;
            done += piece;
        }
        r[9] = 1;   // stored as frames
        wr64(r + 28, ioff);
        wr64(r + 36, en);
        wr32(r + 44, first);
        wr32(r + 48, nframes);
        size_t m = 0;
        uint8_t *bad = with_index(d, n, extra, en, ix, ixn2, &m);
        memsrc s = src_of(bad, m);
        uint8_t *png = NULL;
        size_t len = 0;
        char err[256];
        int rc = read_icon(&s, m, &png, &len, err, sizeof err);
        if (rc != -1 || png || !strstr(err, "frames"))
            MP_FAIL_("case %d: rc %d, \"%s\", %llu bytes read", c, rc, err, (unsigned long long)s.total);
        if (s.total > ixn2 + (64u << 10)) MP_FAIL_("case %d: read %llu bytes", c, (unsigned long long)s.total);
        free(png);
        free(bad);
        free(extra);
        free(ix);
    }
    free(ix0);
    free(icon);
    free(d);
}

int main(void) {
    mpt_clear(TMP);
    MP_RUN(thumb_reads_icon);
    MP_RUN(thumb_icon_any_case);
    MP_RUN(thumb_raw_icon);
    MP_RUN(thumb_no_icon);
    MP_RUN(thumb_truncated);
    MP_RUN(thumb_not_p5z);
    MP_RUN(thumb_bad_footer_crc);
    MP_RUN(thumb_index_too_big);
    MP_RUN(thumb_region_outside);
    MP_RUN(thumb_damaged_frame);
    MP_RUN(thumb_frames_bounded);
    MP_TEST_MAIN_END();
}
