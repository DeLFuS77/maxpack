// SPDX-License-Identifier: GPL-3.0-or-later
// Test helper: a game's files made into LZ4 packs as drakmor/ampr_emu's tools/ampr_pack.py lays them out
// (docs/ASSET_PACKS.md there; GPL-3.0-or-later): ampr_assets.index (AMPRPAK4) with a record for every file in
// folded path order, loose or packed, its volumes ampr_assets-NNN.pak (AMPRDAT3), the CRC sidecar
// (AMPRCRC1), a runtime profile (AMPRCFG1), ampr_emu.index, the loose files with their dates and the tool's
// runtime. Packed files go round-robin into the packs in blocks of 16 KiB, 64 KiB or 1 MiB by their size;
// each block is LZ4 when that is smaller, else raw, and raw always for the files stored ("noise" or "random"
// in their path); a block already in that pack is not written again: its chunk record points at it
// (shared). Every chunk sits on its pack's I/O pages as the runtime reads them.
#ifndef MP_LZ4PACK_H
#define MP_LZ4PACK_H

#include <lz4.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "mp_hash.h"
#include "mp_idx3.h"
#include "mp_lz4pk.h"
#include "mp_platform.h"
#include "mp_rules.h"
#include "mp_source.h"

typedef int (*mpt_lz4_fn)(const char *path, uint64_t size);   // whether a file goes into the packs

// What another tool may pack: what MaxPack packs and eboot.bin and param.json, which it leaves loose, but not
// random.bin and data/big.bin, which it packs; an empty file too.
static inline int mpt_lz4_odd(const char *path, uint64_t size) {
    mp_rules *r = mp_rules_new();
    int packable = mp_rules_classify(r, path, size) == MP_PACKABLE;
    mp_rules_free(r);
    if (!strcmp(path, "eboot.bin") || !strcmp(path, "sce_sys/param.json") || !strcmp(path, "empty.txt")) return 1;
    if (!strcmp(path, "random.bin") || !strcmp(path, "data/big.bin")) return 0;
    return packable;
}

typedef struct {
    int npacks;          // packs, 1 or more
    uint32_t page;       // the I/O page of each pack: 4096 to 1 MiB
    mpt_lz4_fn in_packs;
    int crc, runtime;    // write the CRC sidecar, a runtime profile
    // what it made, to check the packs hold what the tests want
    uint64_t lz4_chunks, raw_chunks, shared_chunks;
} mpt_lz4_opts;

typedef struct {
    uint8_t *p;
    size_t n, cap;
} mpt_buf;

static inline int mpt_buf_put(mpt_buf *b, const void *p, size_t n) {
    if (b->n + n > b->cap) {
        size_t cap = b->cap ? b->cap : 65536;
        while (cap < b->n + n) cap *= 2;
        uint8_t *q = realloc(b->p, cap);
        if (!q) return -1;
        b->p = q;
        b->cap = cap;
    }
    if (n) memcpy(b->p + b->n, p, n);
    b->n += n;
    return 0;
}

static inline int mpt_buf_zeros(mpt_buf *b, size_t n) {
    static const uint8_t z[4096] = {0};
    while (n) {
        size_t k = n < sizeof z ? n : sizeof z;
        if (mpt_buf_put(b, z, k) != 0) return -1;
        n -= k;
    }
    return 0;
}

static inline void mpt_w32(uint8_t *p, uint32_t v) {
    for (int i = 0; i < 4; i++) p[i] = (uint8_t)(v >> (8 * i));
}
static inline void mpt_w64(uint8_t *p, uint64_t v) {
    for (int i = 0; i < 8; i++) p[i] = (uint8_t)(v >> (8 * i));
}
static inline uint32_t mpt_r32(const uint8_t *p) { return p[0] | p[1] << 8 | p[2] << 16 | (uint32_t)p[3] << 24; }
static inline uint64_t mpt_r64(const uint8_t *p) { return mpt_r32(p) | (uint64_t)mpt_r32(p + 4) << 32; }

// CRC-32 of 4 bytes zeroed at zero_at, the rest as it is.
static inline uint32_t mpt_crc_without(const uint8_t *p, size_t n, size_t zero_at) {
    static const uint8_t z[4] = {0};
    uint32_t c = mp_crc32(p, zero_at, 0);
    c = mp_crc32(z, 4, c);
    return mp_crc32(p + zero_at + 4, n - zero_at - 4, c);
}

// The manifest's file record hashes made again from their paths, and its two CRCs: after a test changed it.
static inline int mpt_lz4_reseal(const char *index_path) {
    mp_file *f = NULL;
    uint64_t size = 0;
    size_t got = 0;
    if (mp_file_open_read(index_path, &f, NULL) != 0) return -1;
    mp_file_size(f, &size, NULL);
    uint8_t *d = malloc((size_t)size);
    if (d) mp_file_pread(f, d, (size_t)size, 0, &got, NULL);
    mp_file_close(f);
    if (!d || got != size || size < MP_LZ4PK_HEADER) {
        free(d);
        return -1;
    }
    uint64_t nf = mpt_r64(d + 40), strings = mpt_r64(d + 96);
    for (uint64_t i = 0; i < nf; i++) {
        uint8_t *r = d + MP_LZ4PK_HEADER + 48 * i;
        char path[1100];
        uint32_t off = mpt_r32(r + 32), len = mpt_r32(r + 36);
        if (len >= sizeof path || strings + off + len > size) break;
        memcpy(path, d + strings + off, len);
        path[len] = 0;
        mpt_w64(r, mp_lz4pk_path_hash(path));
    }
    mpt_w32(d + 112, mp_crc32(d + MP_LZ4PK_HEADER, (size_t)size - MP_LZ4PK_HEADER, 0));
    mpt_w32(d + 116, mpt_crc_without(d, MP_LZ4PK_HEADER, 116));
    mp_err e = {{0}};
    int rc = mp_write_file(index_path, d, (size_t)size, &e);
    free(d);
    return rc;
}

static inline char mpt_fold(char c) { return c >= 'A' && c <= 'Z' ? (char)(c + 32) : c; }

static inline int mpt_fold_cmp(const char *a, const char *b) {
    for (;; a++, b++) {
        int x = (unsigned char)mpt_fold(*a), y = (unsigned char)mpt_fold(*b);
        if (x != y || !x) return x - y;
    }
}

typedef struct {
    const mp_entry *en;
    size_t at;   // in the source
} mpt_lz4_rec;

static int mpt_lz4_cmp(const void *a, const void *b) {
    return mpt_fold_cmp(((const mpt_lz4_rec *)a)->en->path, ((const mpt_lz4_rec *)b)->en->path);
}

// A block already written to a pack: its raw bytes' SHA-256 and whether it was stored raw by policy.
typedef struct {
    uint8_t sha[32];
    int store;
    uint32_t pack;
    uint64_t loc;    // its chunk record: where it is
    uint32_t desc;   // and what it is
} mpt_lz4_seen;

static inline int mpt_lz4_put_file(const char *dir, const char *rel, const void *p, size_t n, int64_t mtime) {
    char path[2048];
    snprintf(path, sizeof path, "%s/%s", dir, rel);
    char *slash = strrchr(path, '/');
    mp_err e = {{0}};
    if (slash) {
        *slash = 0;
        mp_make_dirs(path, &e);
        *slash = '/';
    }
    if (mp_write_file(path, p, n, &e) != 0) return -1;
    return mtime ? mp_set_mtime(path, mtime, &e) : 0;
}

static const char mpt_lz4_rt[] = "another tool's libSceAmpr.sprx, with LZ4 packs";

static inline int mpt_lz4_pack(const char *src_path, const char *dir, mpt_lz4_opts *o) {
    mp_err e = {{0}};
    mp_source *src = NULL;
    if (mp_source_open(src_path, &src, &e) != 0 || mp_make_dirs(dir, &e) != 0) {
        mp_source_close(src);
        return -1;
    }
    size_t n = mp_source_count(src), nrec = n + 1;   // and the tool's runtime, loose
    mpt_lz4_rec *rec = calloc(nrec, sizeof *rec);
    mp_entry rt = {(char *)"fakelib/libSceAmpr.sprx", sizeof mpt_lz4_rt - 1, 1600000000};
    uint8_t *recs = calloc(nrec, 48);
    mpt_buf chunks = {0}, strings = {0}, crcs = {0};
    mpt_buf *data = calloc((size_t)o->npacks, sizeof *data);
    mpt_lz4_seen *seen = NULL;
    size_t nseen = 0, cap = 0;
    uint8_t *block = malloc(MP_LZ4PK_CHUNK_MAX), *lz = malloc((size_t)LZ4_compressBound(MP_LZ4PK_CHUNK_MAX));
    mp_idx3_row *rows = calloc(nrec, sizeof *rows);
    int rc = -1, ordinal = 0;
    uint64_t nchunks = 0;
    o->lz4_chunks = o->raw_chunks = o->shared_chunks = 0;
    if (!rec || !recs || !data || !block || !lz || !rows) goto done;
    for (size_t i = 0; i < n; i++) {
        rec[i].en = mp_source_entry(src, i);
        rec[i].at = i;
    }
    rec[n].en = &rt;
    rec[n].at = SIZE_MAX;
    qsort(rec, nrec, sizeof *rec, mpt_lz4_cmp);
    for (int k = 0; k < o->npacks; k++)   // the header, then the payload from the next page
        if (mpt_buf_zeros(&data[k], o->page) != 0) goto done;
    for (size_t i = 0; i < nrec; i++) {
        const mp_entry *en = rec[i].en;
        uint8_t *r = recs + 48 * i;
        char app0[1100];
        snprintf(app0, sizeof app0, "/app0/%s", en->path);
        mpt_w64(r, mp_lz4pk_path_hash(app0));
        mpt_w64(r + 8, en->size);
        mpt_w64(r + 16, (uint64_t)en->mtime);
        mpt_w32(r + 32, (uint32_t)strings.n);
        mpt_w32(r + 36, (uint32_t)strlen(app0));
        if (mpt_buf_put(&strings, app0, strlen(app0) + 1) != 0) goto done;
        rows[i].path = en->path;
        rows[i].size = en->size;
        rows[i].mtime = en->mtime;
        if (rec[i].at == SIZE_MAX || !o->in_packs(en->path, en->size)) {   // loose
            uint8_t *b = malloc((size_t)en->size + 1);
            if (!b) goto done;
            if (rec[i].at == SIZE_MAX) memcpy(b, mpt_lz4_rt, (size_t)en->size);
            else if (mp_source_read(src, rec[i].at, 0, b, (size_t)en->size, &e) != 0) {
                free(b);
                goto done;
            }
            int w = mpt_lz4_put_file(dir, en->path, b, (size_t)en->size, en->mtime);
            free(b);
            if (w != 0) goto done;
            continue;
        }
        int store = strstr(en->path, "noise") || strstr(en->path, "random");
        uint8_t shift = en->size < (64u << 10) ? 14 : en->size >= (2u << 20) ? 20 : 16;
        uint64_t bs = 1ull << shift, count = (en->size + bs - 1) / bs;
        uint32_t pack = (uint32_t)(ordinal++ % o->npacks);
        mpt_w32(r + 24, (uint32_t)nchunks);
        mpt_w32(r + 28, (uint32_t)count);
        mpt_w32(r + 40, store ? 3u : 1u);   // packed, and stored raw
        r[44] = shift;
        for (uint64_t k = 0; k < count; k++) {
            size_t raw = (size_t)(en->size - k * bs < bs ? en->size - k * bs : bs);
            if (mp_source_read(src, rec[i].at, k * bs, block, raw, &e) != 0) goto done;
            uint8_t crc[4];
            mpt_w32(crc, mp_crc32(block, raw, 0));
            if (mpt_buf_put(&crcs, crc, 4) != 0) goto done;
            mp_sha256 h;
            uint8_t sha[32];
            mp_sha256_init(&h);
            mp_sha256_update(&h, block, raw);
            mp_sha256_final(&h, sha);
            uint64_t loc = 0;
            uint32_t desc = 0;
            int found = 0;
            for (size_t s = 0; s < nseen && !found; s++)
                if (seen[s].pack == pack && seen[s].store == store && !memcmp(seen[s].sha, sha, 32)) {
                    loc = seen[s].loc;
                    desc = seen[s].desc | 1u << 22;   // shared
                    found = 1;
                }
            if (found) {
                o->shared_chunks++;
            } else {
                int l = store ? 0 : LZ4_compress_default((const char *)block, (char *)lz, (int)raw, LZ4_compressBound((int)raw));
                int codec = l > 0 && (size_t)l < raw;
                size_t stored = codec ? (size_t)l : raw;
                const uint8_t *bytes = codec ? lz : block;
                mpt_buf *v = &data[pack];
                size_t off = (v->n + 63) & ~(size_t)63;
                // on the I/O pages as the runtime reads them: a small chunk inside one page, a large one from a
                // page's start
                if (stored > o->page || off / o->page != (off + stored - 1) / o->page) off = (off + o->page - 1) / o->page * o->page;
                uint32_t flags = stored <= o->page ? 4u | (off % o->page == 0 ? 8u : 0u) : 8u;
                if (mpt_buf_zeros(v, off - v->n) != 0 || mpt_buf_put(v, bytes, stored) != 0) goto done;
                loc = (uint64_t)off | (uint64_t)pack << 48;
                desc = (uint32_t)(stored - 1) | (uint32_t)codec << 20 | flags << 22;
                if (nseen == cap) {
                    cap = cap ? 2 * cap : 256;
                    mpt_lz4_seen *q = realloc(seen, cap * sizeof *q);
                    if (!q) goto done;
                    seen = q;
                }
                memcpy(seen[nseen].sha, sha, 32);
                seen[nseen].store = store;
                seen[nseen].pack = pack;
                seen[nseen].loc = loc;
                seen[nseen].desc = desc;
                nseen++;
                if (codec) o->lz4_chunks++;
                else o->raw_chunks++;
            }
            uint8_t cr[12];
            mpt_w64(cr, loc);
            mpt_w32(cr + 8, desc);
            if (mpt_buf_put(&chunks, cr, 12) != 0) goto done;
            nchunks++;
        }
    }
    // the pack records' names, then the layout and the volumes
    uint64_t name_at[64];
    for (int k = 0; k < o->npacks && k < 64; k++) {
        char name[32];
        snprintf(name, sizeof name, "ampr_assets-%03d.pak", k);
        name_at[k] = strings.n;
        if (mpt_buf_put(&strings, name, strlen(name) + 1) != 0) goto done;
        size_t end = (data[k].n + o->page - 1) / o->page * o->page;
        if (mpt_buf_zeros(&data[k], end - data[k].n) != 0) goto done;
    }
    uint64_t files_at = MP_LZ4PK_HEADER, chunks_at = files_at + 48 * nrec, packs_at = chunks_at + chunks.n,
             strings_at = packs_at + 32ull * (uint64_t)o->npacks, total = strings_at + strings.n;
    uint8_t *idx = calloc(1, (size_t)total);
    if (!idx) goto done;
    memcpy(idx + files_at, recs, 48 * nrec);
    memcpy(idx + chunks_at, chunks.p, chunks.n);
    for (int k = 0; k < o->npacks; k++) {
        uint8_t *p = idx + packs_at + 32 * (uint64_t)k;
        mpt_w64(p, data[k].n - o->page);
        mpt_w64(p + 8, data[k].n);
        mpt_w32(p + 16, (uint32_t)name_at[k]);
        mpt_w32(p + 20, (uint32_t)strlen("ampr_assets-000.pak"));
        mpt_w32(p + 24, 2);   // laid out on I/O pages
        mpt_w32(p + 28, o->page);
    }
    memcpy(idx + strings_at, strings.p, strings.n);
    uint8_t build[32];
    mp_sha256 bh;
    mp_sha256_init(&bh);
    mp_sha256_update(&bh, "MaxPack test LZ4 packs", 22);
    mp_sha256_update(&bh, idx + MP_LZ4PK_HEADER, (size_t)(total - MP_LZ4PK_HEADER));
    mp_sha256_final(&bh, build);
    memcpy(idx, "AMPRPAK4", 8);
    mpt_w32(idx + 8, 4);
    mpt_w32(idx + 12, MP_LZ4PK_HEADER);
    mpt_w32(idx + 20, 0x01020304u);
    memcpy(idx + 24, build, 16);
    mpt_w64(idx + 40, nrec);
    mpt_w64(idx + 48, nchunks);
    mpt_w32(idx + 56, (uint32_t)o->npacks);
    mpt_w32(idx + 60, 48);
    mpt_w32(idx + 64, 12);
    mpt_w32(idx + 68, 32);
    mpt_w64(idx + 72, files_at);
    mpt_w64(idx + 80, chunks_at);
    mpt_w64(idx + 88, packs_at);
    mpt_w64(idx + 96, strings_at);
    mpt_w64(idx + 104, strings.n);
    mpt_w32(idx + 112, mp_crc32(idx + MP_LZ4PK_HEADER, (size_t)(total - MP_LZ4PK_HEADER), 0));
    mpt_w32(idx + 116, mpt_crc_without(idx, MP_LZ4PK_HEADER, 116));
    char p[2048];
    snprintf(p, sizeof p, "%s/%s", dir, MP_LZ4PK_INDEX);
    int bad = mp_write_file(p, idx, (size_t)total, &e) != 0;
    for (int k = 0; k < o->npacks && !bad; k++) {
        uint8_t *h = data[k].p;
        memcpy(h, "AMPRDAT3", 8);
        mpt_w32(h + 8, 3);
        mpt_w32(h + 12, MP_LZ4PK_VOL_HEADER);
        mpt_w32(h + 16, (uint32_t)k);
        mpt_w32(h + 20, 2);
        memcpy(h + 24, build, 16);
        mpt_w64(h + 40, o->page);
        mpt_w64(h + 48, data[k].n - o->page);
        mpt_w32(h + 56, mpt_crc_without(h, MP_LZ4PK_VOL_HEADER, 56));
        snprintf(p, sizeof p, "%s/ampr_assets-%03d.pak", dir, k);
        bad = mp_write_file(p, data[k].p, data[k].n, &e) != 0;
    }
    if (o->crc && !bad) {
        uint8_t h[48] = "AMPRCRC1";
        mpt_w32(h + 8, 1);
        mpt_w32(h + 12, 48);
        memcpy(h + 16, build, 16);
        mpt_w64(h + 32, nchunks);
        mpt_w32(h + 40, mp_crc32(crcs.p, crcs.n, 0));
        mpt_w32(h + 44, mpt_crc_without(h, 48, 44));
        mpt_buf all = {0};
        bad = mpt_buf_put(&all, h, 48) != 0 || mpt_buf_put(&all, crcs.p, crcs.n) != 0;
        snprintf(p, sizeof p, "%s/%s", dir, MP_LZ4PK_CRC);
        if (!bad) bad = mp_write_file(p, all.p, all.n, &e) != 0;
        free(all.p);
    }
    if (o->runtime && !bad) {
        uint8_t h[64] = "AMPRCFG1";
        mpt_w32(h + 8, 1);
        mpt_w32(h + 12, 64);
        memcpy(h + 16, build, 16);
        mpt_w64(h + 32, 64u << 20);
        mpt_w64(h + 40, 16u << 20);
        mpt_w32(h + 48, 2);
        mpt_w32(h + 52, 1);
        mpt_w32(h + 56, mpt_crc_without(h, 64, 56));
        snprintf(p, sizeof p, "%s/%s", dir, MP_LZ4PK_RUNTIME);
        bad = mp_write_file(p, h, 64, &e) != 0;
    }
    uint8_t *emu = NULL;
    size_t emu_len = 0;
    if (!bad) bad = mp_idx3_build(rows, nrec, &emu, &emu_len, &e) != 0;
    snprintf(p, sizeof p, "%s/ampr_emu.index", dir);
    if (!bad) bad = mp_write_file(p, emu, emu_len, &e) != 0;
    free(emu);
    free(idx);
    rc = bad ? -1 : 0;
done:
    for (int k = 0; data && k < o->npacks; k++) free(data[k].p);
    free(data);
    free(chunks.p);
    free(strings.p);
    free(crcs.p);
    free(seen);
    free(block);
    free(lz);
    free(rows);
    free(recs);
    free(rec);
    mp_source_close(src);
    return rc;
}

#endif
