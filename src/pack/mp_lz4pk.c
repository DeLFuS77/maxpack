// SPDX-License-Identifier: GPL-3.0-or-later
// LZ4 packs reader (mp_lz4pk.h): the checks of drakmor/ampr_emu's tools/ampr_pack_format.py
// (validate_manifest, validate_data_header, load_chunk_crcs, read_runtime_settings) and of ampr-emu-ng's
// crates/ampr-lz4 (Manifest::open, check_volume_header, decode_chunk), ported to C.
#include "mp_lz4pk.h"

#include <lz4.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "mp_hash.h"
#include "mp_idx3.h"
#include "mp_platform.h"

#define FILE_REC 48u
#define CHUNK_REC 12u
#define PACK_REC 32u
#define PATH_MAX_BYTES 1024u   // the PS5's path limit, as the runtime takes a path
#define ENDIAN 0x01020304u
// what ampr-emu-ng's runtime takes (AMPR_EMU_PACK_MAX_FILES, _MAX_CHUNKS and _MAX_PACKS)
#define MAX_FILES 2000000u
#define MAX_CHUNKS 16000000u
#define MAX_PACKS 1024u

#define F_PACKED 1u
#define F_STORE_ONLY 2u
#define F_STREAMING 4u
#define F_RANDOM 16u
#define F_KNOWN 0x1Fu
#define C_STREAMING 2u
#define C_CONTAINED 4u
#define C_ALIGNED 8u
#define C_KNOWN 0x0Fu   // shared (1), streaming, page-contained, page-aligned
#define P_STRIPED 1u
#define P_PAGES 2u

typedef struct {
    char *path;      // relative to the game
    uint64_t size;
    int64_t mtime;
    uint32_t first, count, flags;
    uint8_t shift;
} lfile;

typedef struct {
    uint64_t off;
    uint32_t pack, stored;
    uint8_t codec, flags;
} lchunk;

typedef struct {
    char *name;
    uint64_t payload, size;
    uint32_t flags, page;
} lpack;

struct mp_lz4pk {
    lfile *f;
    size_t n;
    lchunk *c;
    uint32_t nc;
    lpack *p;
    uint32_t np;
    uint8_t build[16];
    uint32_t *crc;   // of each chunk decoded, when the sidecar is there
    mp_lz4pk_volsrc vs;
    int has_vs;
};

static uint32_t rd32(const uint8_t *p) { return p[0] | p[1] << 8 | p[2] << 16 | (uint32_t)p[3] << 24; }
static uint64_t rd64(const uint8_t *p) { return rd32(p) | (uint64_t)rd32(p + 4) << 32; }

static int bad(mp_err *e, const char *what, const char *why) {
    mp_err_set(e, "invalid %s: %s", what, why);
    return -1;
}

// CRC-32 of n bytes with the 4 at zero_at taken as zeros.
static uint32_t crc_without(const uint8_t *p, size_t n, size_t zero_at) {
    static const uint8_t z[4] = {0};
    uint32_t c = mp_crc32(p, zero_at, 0);
    c = mp_crc32(z, 4, c);
    return mp_crc32(p + zero_at + 4, n - zero_at - 4, c);
}

static unsigned char fold(unsigned char c) { return c == '\\' ? '/' : c >= 'A' && c <= 'Z' ? (unsigned char)(c + 32) : c; }

uint64_t mp_lz4pk_path_hash(const char *path) {
    uint64_t h = 14695981039346656037ull;
    for (const unsigned char *p = (const unsigned char *)path; *p; p++) {
        h ^= fold(*p);
        h *= 1099511628211ull;
    }
    return h ? h : 1;
}

// Parts that are neither empty nor "." or "..", without NUL or '\' (safe_components).
static int safe_parts(const char *s, size_t len) {
    if (!len) return 0;
    size_t start = 0;
    for (size_t i = 0; i <= len; i++) {
        if (i < len && s[i] != '/') {
            if (s[i] == 0 || s[i] == '\\') return 0;
            continue;
        }
        size_t k = i - start;
        if (!k || (k == 1 && s[start] == '.') || (k == 2 && s[start] == '.' && s[start + 1] == '.')) return 0;
        start = i + 1;
    }
    return 1;
}

// String off of len bytes: shorter than the path limit, inside the strings, ended by a NUL, without one inside.
static char *string_at(const uint8_t *strings, uint64_t slen, uint32_t off, uint32_t len) {
    uint64_t end = (uint64_t)off + len;
    if (len >= PATH_MAX_BYTES || end >= slen || strings[end] != 0 || memchr(strings + off, 0, len)) return NULL;
    char *s = malloc(len + 1u);
    if (!s) return NULL;
    memcpy(s, strings + off, len);
    s[len] = 0;
    return s;
}

void mp_lz4pk_close(mp_lz4pk *p) {
    if (!p) return;
    for (size_t i = 0; i < p->n; i++) free(p->f[i].path);
    for (uint32_t k = 0; k < p->np; k++) free(p->p[k].name);
    free(p->f);
    free(p->c);
    free(p->p);
    free(p->crc);
    free(p);
}

// The placement checks of a chunk of a packed file (validate_manifest): inside its pack's payload, at 64 bytes,
// and on its I/O pages as the runtime plans its reads.
static int chunk_ok(const lchunk *c, const lpack *k, const lfile *f, uint64_t raw, uint64_t block) {
    uint64_t start = k->size - k->payload, page = k->page, end = c->off + c->stored;
    int contained = (c->flags & C_CONTAINED) != 0, aligned = (c->flags & C_ALIGNED) != 0;
    int streaming = (f->flags & F_STREAMING) != 0;
    if (c->stored > block || c->stored > MP_LZ4PK_CHUNK_MAX || (c->codec == 0 && c->stored != raw)) return 0;
    if ((f->flags & F_STORE_ONLY) && c->codec != 0) return 0;
    if (streaming != ((c->flags & C_STREAMING) != 0)) return 0;
    if (c->off < start || end > k->size || c->off % 64) return 0;
    if (contained && (c->stored > page || c->off / page != (end - 1) / page)) return 0;
    if (aligned && c->off % page) return 0;
    if (c->stored <= page && aligned && !contained) return 0;
    if (((f->flags & F_RANDOM) || !streaming) && !contained && !aligned) return 0;
    if (c->off / page * page < start || (end + page - 1) / page * page > k->size) return 0;
    return 1;
}

int mp_lz4pk_open(const uint8_t *d, size_t len, mp_lz4pk **out, int *older, mp_err *e) {
    const char *what = "ampr_assets.index";
    *out = NULL;
    if (older) *older = 0;
    if (len >= 8 && !memcmp(d, "AMPRPAK", 7) && d[7] >= '1' && d[7] <= '3') {
        if (older) *older = 1;
        // only the third has a converter
        mp_err_set(e, "its ampr_assets.index is of an older version of the LZ4 packs (AMPRPAK%c), which MaxPack does "
                      "not read: %s",
                   d[7],
                   d[7] == '3' ? "ampr_emu's convert_ampr_pack_v3.py makes it the current one (AMPRPAK4)"
                               : "pack the game again from its dump with a current ampr_emu (AMPRPAK4)");
        return -1;
    }
    if (len < MP_LZ4PK_HEADER || memcmp(d, "AMPRPAK4", 8)) return bad(e, what, "header");
    if (rd32(d + 8) != 4 || rd32(d + 12) != MP_LZ4PK_HEADER || rd32(d + 16) != 0 || rd32(d + 20) != ENDIAN ||
        rd32(d + 60) != FILE_REC || rd32(d + 64) != CHUNK_REC || rd32(d + 68) != PACK_REC || rd64(d + 120) != 0)
        return bad(e, what, "version, flags, endianness, record sizes or reserved fields");
    if (crc_without(d, MP_LZ4PK_HEADER, 116) != rd32(d + 116)) return bad(e, what, "header CRC");
    if (mp_crc32(d + MP_LZ4PK_HEADER, len - MP_LZ4PK_HEADER, 0) != rd32(d + 112)) return bad(e, what, "payload CRC");
    uint64_t nf = rd64(d + 40), nc = rd64(d + 48), np = rd32(d + 56), slen = rd64(d + 104);
    // each count within what the runtime takes and against what is left of the file: no sum can wrap
    if (nf > MAX_FILES || nc > MAX_CHUNKS || np > MAX_PACKS || nf > len / FILE_REC || nc > len / CHUNK_REC ||
        np > len / PACK_REC)
        return bad(e, what, "counts");
    uint64_t files_at = MP_LZ4PK_HEADER, chunks_at = files_at + FILE_REC * nf, packs_at = chunks_at + CHUNK_REC * nc,
             strings_at = packs_at + PACK_REC * np;
    if (rd64(d + 72) != files_at || rd64(d + 80) != chunks_at || rd64(d + 88) != packs_at || rd64(d + 96) != strings_at ||
        strings_at > len || slen != len - strings_at)
        return bad(e, what, "position or size of the tables");
    const uint8_t *strings = d + strings_at;
    mp_lz4pk *p = calloc(1, sizeof *p);
    if (!p) goto oom;
    memcpy(p->build, d + 24, 16);
    p->n = (size_t)nf;
    p->nc = (uint32_t)nc;
    p->np = (uint32_t)np;
    p->f = calloc(p->n ? p->n : 1, sizeof *p->f);
    p->c = calloc(p->nc ? p->nc : 1, sizeof *p->c);
    p->p = calloc(p->np ? p->np : 1, sizeof *p->p);
    if (!p->f || !p->c || !p->p) goto oom;
    for (uint32_t k = 0; k < p->np; k++) {
        const uint8_t *r = d + packs_at + PACK_REC * k;
        lpack *q = &p->p[k];
        q->payload = rd64(r);
        q->size = rd64(r + 8);
        q->flags = rd32(r + 24);
        q->page = rd32(r + 28);
        q->name = string_at(strings, slen, rd32(r + 16), rd32(r + 20));
        uint64_t start = q->size - q->payload;
        if (!q->name || q->name[0] == '/' || !safe_parts(q->name, strlen(q->name)) || q->page < 4096 ||
            q->page > (1u << 20) || (q->page & (q->page - 1)) || (q->flags & ~(P_STRIPED | P_PAGES)) || !(q->flags & P_PAGES) ||
            q->size < MP_LZ4PK_VOL_HEADER || q->payload > q->size || start < MP_LZ4PK_VOL_HEADER || start % q->page ||
            q->size % q->page) {
            mp_lz4pk_close(p);
            return bad(e, what, "pack record");
        }
    }
    for (uint32_t k = 0; k < p->nc; k++) {
        const uint8_t *r = d + chunks_at + CHUNK_REC * k;
        uint64_t loc = rd64(r);
        uint32_t desc = rd32(r + 8);
        lchunk *c = &p->c[k];
        c->off = loc & ((1ull << 48) - 1);
        c->pack = (uint32_t)(loc >> 48);
        c->stored = (desc & 0xFFFFFu) + 1;
        c->codec = (uint8_t)((desc >> 20) & 3);
        c->flags = (uint8_t)((desc >> 22) & 0xFF);
        if ((desc & 0xC0000000u) || c->codec > 1 || (c->flags & ~C_KNOWN) || c->pack >= p->np) {
            mp_lz4pk_close(p);
            return bad(e, what, "chunk record");
        }
    }
    // the chunks the packed files claim: each file has a range of its own (a shared block has a record in each),
    // so no more than there are, and each chunk is checked for one file only
    uint64_t claimed = 0;
    for (size_t i = 0; i < p->n; i++) {
        const uint8_t *r = d + files_at + FILE_REC * i;
        lfile *f = &p->f[i];
        f->size = rd64(r + 8);
        f->mtime = (int64_t)rd64(r + 16);
        f->first = rd32(r + 24);
        f->count = rd32(r + 28);
        f->flags = rd32(r + 40);
        f->shift = r[44];
        uint8_t cls = r[45];
        char *app0 = string_at(strings, slen, rd32(r + 32), rd32(r + 36));
        const char *why = NULL;
        // its path: "/app0/" and safe parts, canonical, with its hash; within MaxPack's own limits
        if (!app0) why = "a path out of its table";
        else if (strlen(app0) <= 6 || app0[0] != '/' || app0[5] != '/' || fold((unsigned char)app0[1]) != 'a' ||
                 fold((unsigned char)app0[2]) != 'p' || fold((unsigned char)app0[3]) != 'p' || app0[4] != '0' ||
                 !safe_parts(app0 + 6, strlen(app0 + 6)) || mp_pack_path_check(app0 + 6, NULL) != 0)
            why = "a path that is not a safe one of the game";
        else if (mp_lz4pk_path_hash(app0) != rd64(r))
            why = "a path hash";
        else if ((r[46] | r[47] << 8) != 0 || (f->flags & ~F_KNOWN))
            why = "file flags or reserved fields";
        if (!why && !(f->flags & F_PACKED)) {
            if (f->flags || f->first || f->count || f->shift || cls) why = "a loose file with chunks";
        } else if (!why) {
            uint64_t block = 1ull << (f->shift < 64 ? f->shift : 0);
            if (f->shift < 14 || f->shift > 20) why = "a block size";
            else if ((f->flags & F_STREAMING) && (f->flags & F_RANDOM)) why = "a file both streaming and random-access";
            else if ((uint64_t)f->first + f->count > p->nc) why = "a chunk range";
            else if (f->count != f->size / block + (f->size % block != 0)) why = "a chunk count";
            else if ((claimed += f->count) > p->nc) why = "chunk ranges that overlap: more chunks claimed than there are";
            for (uint32_t k = 0; !why && k < f->count; k++) {
                const lchunk *c = &p->c[f->first + k];
                uint64_t raw = f->size - (uint64_t)k * block < block ? f->size - (uint64_t)k * block : block;
                if (!chunk_ok(c, &p->p[c->pack], f, raw, block)) why = "a chunk out of its pack or of its place";
            }
        }
        if (why) {
            char m[300];
            snprintf(m, sizeof m, "%s (file %zu%s%s)", why, i + 1, app0 ? ", " : "", app0 ? app0 : "");
            free(app0);
            mp_lz4pk_close(p);
            return bad(e, what, m);
        }
        f->path = mp_strdup(app0 + 6);
        free(app0);
        if (!f->path) goto oom;
    }
    *out = p;
    return 0;
oom:
    mp_lz4pk_close(p);
    mp_err_set(e, "out of memory");
    return -1;
}

int mp_lz4pk_volumes(mp_lz4pk *p, const mp_lz4pk_volsrc *vs, mp_err *e) {
    for (uint32_t k = 0; k < p->np; k++) {
        const lpack *q = &p->p[k];
        uint8_t h[MP_LZ4PK_VOL_HEADER];
        if (vs->size(vs->ctx, k) != q->size) {
            mp_err_set(e, "volume %s does not exist or is not the size its manifest says", q->name);
            return -1;
        }
        if (vs->read(vs->ctx, k, 0, h, sizeof h, e) != 0) return -1;
        uint64_t start = q->size - q->payload;
        if (memcmp(h, "AMPRDAT3", 8) || rd32(h + 8) != 3 || rd32(h + 12) != MP_LZ4PK_VOL_HEADER || rd32(h + 16) != k ||
            rd32(h + 20) != q->flags || memcmp(h + 24, p->build, 16) || rd32(h + 60) != 0 || rd64(h + 40) != start ||
            rd64(h + 48) != q->payload) {
            mp_err_set(e, "invalid header of volume %s (or of another set of packs)", q->name);
            return -1;
        }
        if (crc_without(h, sizeof h, 56) != rd32(h + 56)) {
            mp_err_set(e, "header CRC of volume %s", q->name);
            return -1;
        }
    }
    p->vs = *vs;
    p->has_vs = 1;
    return 0;
}

int mp_lz4pk_crcs(mp_lz4pk *p, const uint8_t *d, size_t len, mp_err *e) {
    const char *what = "ampr_assets.index.crc";
    if (len < 48 || memcmp(d, "AMPRCRC1", 8) || rd32(d + 8) != 1 || rd32(d + 12) != 48) return bad(e, what, "header");
    if (memcmp(d + 16, p->build, 16) || rd64(d + 32) != p->nc) return bad(e, what, "not of this ampr_assets.index");
    if (crc_without(d, 48, 44) != rd32(d + 44)) return bad(e, what, "header CRC");
    if (len != 48 + 4ull * p->nc || mp_crc32(d + 48, len - 48, 0) != rd32(d + 40)) return bad(e, what, "payload CRC");
    uint32_t *crc = malloc((p->nc ? p->nc : 1) * sizeof *crc);
    if (!crc) {
        mp_err_set(e, "out of memory");
        return -1;
    }
    for (uint32_t k = 0; k < p->nc; k++) crc[k] = rd32(d + 48 + 4 * k);
    free(p->crc);
    p->crc = crc;
    return 0;
}

int mp_lz4pk_runtime(const mp_lz4pk *p, const uint8_t *d, size_t len, mp_err *e) {
    const char *what = "ampr_assets.index.runtime";
    if (len != 64 || memcmp(d, "AMPRCFG1", 8) || rd32(d + 8) != 1 || rd32(d + 12) != 64 || rd32(d + 60) != 0)
        return bad(e, what, "header");
    if (memcmp(d + 16, p->build, 16)) return bad(e, what, "not of this ampr_assets.index");
    if (crc_without(d, 64, 56) != rd32(d + 56)) return bad(e, what, "CRC");
    uint32_t workers = rd32(d + 48), reserve = rd32(d + 52);
    if (workers < 1 || workers > 16 || reserve >= workers || rd64(d + 32) % 16384 || rd64(d + 40) % 16384)
        return bad(e, what, "settings the runtime refuses");
    return 0;
}

size_t mp_lz4pk_count(const mp_lz4pk *p) { return p->n; }
const char *mp_lz4pk_path(const mp_lz4pk *p, size_t i) { return i < p->n ? p->f[i].path : NULL; }
uint64_t mp_lz4pk_size(const mp_lz4pk *p, size_t i) { return i < p->n ? p->f[i].size : 0; }
int64_t mp_lz4pk_mtime(const mp_lz4pk *p, size_t i) { return i < p->n ? p->f[i].mtime : 0; }
int mp_lz4pk_packed(const mp_lz4pk *p, size_t i) { return i < p->n && (p->f[i].flags & F_PACKED); }
uint32_t mp_lz4pk_pack_count(const mp_lz4pk *p) { return p->np; }
const char *mp_lz4pk_pack_name(const mp_lz4pk *p, uint32_t pack) { return pack < p->np ? p->p[pack].name : NULL; }
uint32_t mp_lz4pk_chunk_count(const mp_lz4pk *p) { return p->nc; }

int mp_lz4pk_read_file(mp_lz4pk *p, size_t i, uint64_t off, void *buf, size_t n, int *bad_out, mp_err *e) {
    if (bad_out) *bad_out = 0;
    if (i >= p->n || !(p->f[i].flags & F_PACKED) || off > p->f[i].size || n > p->f[i].size - off) {
        mp_err_set(e, "read outside the packed file");
        return -1;
    }
    if (!p->has_vs) {
        mp_err_set(e, "the volumes of the LZ4 packs are not open");
        return -1;
    }
    if (!n) return 0;
    const lfile *f = &p->f[i];
    uint64_t block = 1ull << f->shift;   // what a chunk of this file stores and decodes to at most
    size_t room = (size_t)(f->size < block ? f->size : block);
    uint8_t *src = malloc((size_t)block), *dec = malloc(room ? room : 1), *o = buf;
    int rc = 0;
    if (!src || !dec) {
        mp_err_set(e, "out of memory");
        rc = -1;
    }
    for (uint64_t k = off / block; !rc && n; k++) {
        uint32_t id = f->first + (uint32_t)k;
        const lchunk *c = &p->c[id];
        uint64_t base = k * block;
        size_t raw = (size_t)(f->size - base < block ? f->size - base : block);
        if (p->vs.read(p->vs.ctx, c->pack, c->off, src, c->stored, e) != 0) {
            rc = -1;
            break;
        }
        const uint8_t *plain = src;
        if (c->codec == 1) {
            int got = LZ4_decompress_safe((const char *)src, (char *)dec, (int)c->stored, (int)raw);
            if (got < 0 || (size_t)got != raw) {
                mp_err_set(e, "%s: chunk %llu does not decode to its size: damaged", f->path, (unsigned long long)k);
                if (bad_out) *bad_out = 1;
                rc = -1;
                break;
            }
            plain = dec;
        }
        if (p->crc && mp_crc32(plain, raw, 0) != p->crc[id]) {
            mp_err_set(e, "%s: chunk %llu is not the one of its CRC in ampr_assets.index.crc: damaged", f->path,
                       (unsigned long long)k);
            if (bad_out) *bad_out = 1;
            rc = -1;
            break;
        }
        size_t from = (size_t)(off > base ? off - base : 0), take = raw - from < n ? raw - from : n;
        memcpy(o, plain + from, take);
        o += take;
        n -= take;
        off += take;
    }
    free(src);
    free(dec);
    return rc;
}
