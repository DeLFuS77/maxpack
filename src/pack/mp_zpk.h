// SPDX-License-Identifier: GPL-3.0-or-later
// ampr-emu-ng zstd packs: ampr_zstd.index (AMPRZPK1) + ampr_zstd-NNN.pak (AMPRZDT1).
// Format: ampr-emu-ng's docs/zstd-format.md and ps5-dump-forge's docs/zstd.md.
#ifndef MP_ZPK_H
#define MP_ZPK_H

#include <stddef.h>
#include <stdint.h>

#include "mp_measure.h"
#include "mp_parallel.h"
#include "mp_source.h"

#define MP_ZPK_VOLUME_LIMIT 4294901760ull   // 4 GiB - 64 KiB, header included
#define MP_ZPK_MAX_VOLUMES 4096u
#define MP_ZPK_HEADER 128u
#define MP_ZPK_VOL_HEADER 64u

// Where the volumes go: a folder or regions of a .p5z.
typedef struct {
    int (*vol_begin)(void *ctx, uint32_t vol, mp_err *e);   // starts the volume: 64 B of zeros
    int (*vol_write)(void *ctx, const void *p, size_t n, mp_err *e);
    int (*vol_end)(void *ctx, uint32_t vol, mp_err *e);
    int (*vol_header)(void *ctx, uint32_t vol, const uint8_t hdr[64], mp_err *e);   // at the end
    void *ctx;
} mp_zpk_sink;

typedef struct {
    int level;              // 1..22
    unsigned threads;
    uint64_t volume_limit;  // MP_ZPK_VOLUME_LIMIT (smaller in tests)
    mp_progress *prog;
} mp_zpk_opts;

typedef struct {
    uint64_t files, chunks, volumes, logical, stored;
    uint8_t archive_id[16];
} mp_zpk_stats;

// files: source indices of the packable files. Returns the ampr_zstd.index (malloc) and in sha[i]
// the SHA-256 of files[i]'s contents. The output does not depend on o->threads.
int mp_zpk_write(mp_source *src, const uint32_t *files, size_t n, const mp_zpk_opts *o,
                 const mp_zpk_sink *sink, uint8_t **index, size_t *index_len, uint8_t (*sha)[32],
                 mp_zpk_stats *st, mp_err *e);

// Volumes as ampr_zstd-NNN.pak inside dir (which must already exist).
int mp_zpk_sink_dir(const char *dir, mp_zpk_sink *out, mp_err *e);
void mp_zpk_sink_free(mp_zpk_sink *s);

// ---- Reading -----------------------------------------------------------------------------

typedef struct {
    int (*read)(void *ctx, uint32_t vol, uint64_t off, void *buf, size_t n, mp_err *e);
    uint64_t (*size)(void *ctx, uint32_t vol);   // 0 if it does not exist
    void *ctx;
} mp_zpk_volsrc;

typedef struct mp_zpk mp_zpk;
// Checks everything the documents require (CRCs, tables, order, consecutive blocks, volume
// headers). Does not decode the blocks: mp_zpk_read_file does that.
int mp_zpk_open(const uint8_t *index, size_t len, const mp_zpk_volsrc *vs, mp_zpk **out, mp_err *e);
size_t mp_zpk_count(const mp_zpk *z);
const char *mp_zpk_path(const mp_zpk *z, size_t i);
uint64_t mp_zpk_size(const mp_zpk *z, size_t i);
uint32_t mp_zpk_volume_count(const mp_zpk *z);
// Index of the file with that path (records are sorted by hash and path), or -1.
long mp_zpk_find(const mp_zpk *z, const char *path);
// Data of block k of file i (any pointer may be NULL). 0 if it exists.
int mp_zpk_chunk(const mp_zpk *z, size_t i, uint32_t k, uint32_t *vol, uint64_t *off, uint32_t *stored,
                 int *codec);
// Reads [off, off+n) of file i decoding its blocks; checks sizes and checksums.
int mp_zpk_read_file(mp_zpk *z, size_t i, uint64_t off, void *buf, size_t n, mp_err *e);
void mp_zpk_close(mp_zpk *z);

int mp_zpk_volsrc_dir(const char *dir, mp_zpk_volsrc *out, mp_err *e);   // opens on demand
void mp_zpk_volsrc_free(mp_zpk_volsrc *vs);

#endif
