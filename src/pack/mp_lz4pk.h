// SPDX-License-Identifier: GPL-3.0-or-later
// LZ4 packs: ampr_assets.index (AMPRPAK4, the manifest: one record per file of the game, packed or loose,
// each packed one an ordered range of independent chunks), its volumes ampr_assets-NNN.pak (AMPRDAT3),
// and the optional ampr_assets.index.crc (AMPRCRC1: the CRC-32 of each chunk decoded) and
// ampr_assets.index.runtime (AMPRCFG1: the runtime's settings). The format and every check are ported from
// drakmor/ampr_emu's tools/ampr_pack_format.py and include/ampr_emu_pack_format.h and from ampr-emu-ng's
// crates/ampr-lz4 (manifest.rs, lib.rs), both GPL-3.0-or-later: the CRCs and headers, safe paths and their
// hashes, sizes and block geometry, chunks inside their pack's payload and laid out on its I/O pages as
// the runtime reads them, chunks of at most 1 MiB, raw or LZ4. Chunks shared by several files (the packer
// deduplicates blocks) are allowed. LZ4 blocks are decoded with lz4 (BSD-2-Clause).
#ifndef MP_LZ4PK_H
#define MP_LZ4PK_H

#include <stddef.h>
#include <stdint.h>

#include "mp_err.h"

#define MP_LZ4PK_INDEX "ampr_assets.index"
#define MP_LZ4PK_CRC "ampr_assets.index.crc"
#define MP_LZ4PK_RUNTIME "ampr_assets.index.runtime"
#define MP_LZ4PK_HEADER 128u
#define MP_LZ4PK_VOL_HEADER 64u
#define MP_LZ4PK_CHUNK_MAX (1u << 20)

// The volumes of the packs, by pack number (the manifest names them).
typedef struct {
    int (*read)(void *ctx, uint32_t pack, uint64_t off, void *buf, size_t n, mp_err *e);
    uint64_t (*size)(void *ctx, uint32_t pack);   // 0 when there is none
    void *ctx;
} mp_lz4pk_volsrc;

typedef struct mp_lz4pk mp_lz4pk;

// Checks the manifest (index[0..len), copied) and gives it: its header and CRCs, each pack record and file
// record and the chunks of every packed file. *older is set (and -1 returned) for a manifest of another
// version of the LZ4 packs (AMPRPAK1 to AMPRPAK3), which this reader does not take.
int mp_lz4pk_open(const uint8_t *index, size_t len, mp_lz4pk **out, int *older, mp_err *e);
// The volumes, by pack number: each one's header checked against its record and the manifest (pack number,
// flags, build ID, payload, CRC) and its size against the record. Once, before a read.
int mp_lz4pk_volumes(mp_lz4pk *p, const mp_lz4pk_volsrc *vs, mp_err *e);
// The CRC sidecar (AMPRCRC1), when there is one: of this manifest and its chunks, its CRCs right; each chunk
// decoded is then checked against its CRC (else only LZ4's own bounds tell a damaged one, and a raw chunk
// nothing).
int mp_lz4pk_crcs(mp_lz4pk *p, const uint8_t *crc, size_t len, mp_err *e);
// The runtime profile (AMPRCFG1), when there is one: the runtime refuses the packs when it is not right.
int mp_lz4pk_runtime(const mp_lz4pk *p, const uint8_t *profile, size_t len, mp_err *e);

size_t mp_lz4pk_count(const mp_lz4pk *p);
// The path of file record i relative to the game (without "/app0/"), its size, date, and whether it is packed
// (else it is a loose file of the game).
const char *mp_lz4pk_path(const mp_lz4pk *p, size_t i);
uint64_t mp_lz4pk_size(const mp_lz4pk *p, size_t i);
int64_t mp_lz4pk_mtime(const mp_lz4pk *p, size_t i);
int mp_lz4pk_packed(const mp_lz4pk *p, size_t i);
uint32_t mp_lz4pk_pack_count(const mp_lz4pk *p);
const char *mp_lz4pk_pack_name(const mp_lz4pk *p, uint32_t pack);
uint32_t mp_lz4pk_chunk_count(const mp_lz4pk *p);
// Reads [off, off+n) of packed file i, decoding its chunks; *bad (when not NULL) is 1 when it failed because
// of what the packs hold (a chunk that does not decode to its size, or not to its CRC), 0 for anything else.
int mp_lz4pk_read_file(mp_lz4pk *p, size_t i, uint64_t off, void *buf, size_t n, int *bad, mp_err *e);
void mp_lz4pk_close(mp_lz4pk *p);

// FNV-1a 64 of a path as the manifest records it ("/app0/..."), with ASCII A to Z folded to lower case and
// '\' to '/'; 0 becomes 1.
uint64_t mp_lz4pk_path_hash(const char *app0_path);

#endif
