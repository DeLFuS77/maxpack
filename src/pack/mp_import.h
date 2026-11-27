// SPDX-License-Identifier: GPL-3.0-or-later
// A game packed already with zstd packs (ampr_zstd.index and ampr_zstd-NNN.pak, AMPRZPK1, as Dump Forge,
// Alembic or a .zstd exFAT image make it), as a source of its original files: the loose files of the source
// but the AMPR files (the indexes, the packs, ampr_commands.bin and the runtime's logs) and the other tool's
// runtime (fakelib/libSceAmpr.sprx), and the files in its packs, decompressed as they are read, with the
// size the packs give and the date ampr_emu.index gives. The packs are read from the source itself, so a
// folder, an image or a package with packs in it all work; nothing is ever written to it.
#ifndef MP_IMPORT_H
#define MP_IMPORT_H

#include <stdint.h>

#include "mp_source.h"
#include "mp_zpk.h"

// Why a packed game is refused (mp_import_info.code), for --progress json.
#define MP_IMPORT_DAMAGED "packs_damaged"          // packs or indexes that are damaged or disagree
#define MP_IMPORT_MISSING "packed_file_missing"    // a file ampr_emu.index lists is not there
#define MP_IMPORT_LZ4 "packs_lz4"                  // LZ4 packs (ampr_assets*), which MaxPack does not read yet
#define MP_IMPORT_TWICE "packed_and_loose"        // a path both loose and in the packs (or a file and a folder)
#define MP_IMPORT_DLC "dlc_packed"                 // a DLC packed already (no eboot.bin), given as a game

typedef struct {
    int packed;                 // the source is a game packed with zstd packs
    int level;                  // the zstd level its packs say
    uint64_t packed_files, packed_bytes, volumes;
    const char *code;           // when it is refused: one of the codes above (NULL for anything else)
} mp_import_info;

// Opens path as mp_source_open_as does (what: MP_OPEN_GAME, MP_OPEN_DLC or MP_OPEN_ANY) and, when it is a
// packed game, gives the source of its original files instead (info->packed). A source that is not packed
// is given as it is. Refused, with e and info->code set: LZ4 packs, a pack volume without its index, packs
// or indexes that are damaged or disagree with each other (no ampr_emu.index, a packed file it does not list
// or lists with another size), a path both loose and packed or a file that is a folder too (ASCII case aside),
// and a file ampr_emu.index lists that is neither loose nor packed, or loose of another size (the files that
// are left out, OS junk and what dump tools leave behind aside). The names the runtime opens (the index, the
// volumes) are taken as it names them, and in another case only when there are none so.
// Asked for a game only (MP_OPEN_GAME), a packed source without eboot.bin is a DLC packed already: refused too.
int mp_import_open(const char *path, int what, mp_source **out, mp_import_info *info, mp_err *e);
// The same for a source already open, base, which it takes when it succeeds (the source it gives holds it)
// and leaves open when it fails; path only words the messages.
int mp_import_source(const char *path, mp_source *base, int what, mp_source **out, mp_import_info *info, mp_err *e);
// The packs of a source mp_import_open made of a packed game (NULL for any other source), valid while
// it is open: a file of the source with a path the packs have is read from them.
mp_zpk *mp_import_zpk(mp_source *s);
// 1 once reading a file of the packs found a block that is not what the runtime takes or does not decode: what
// stopped was the packs (a read of the source that fails is not that).
int mp_import_failed(mp_source *s);
// 1 for a file of a packed game that mp_import_open leaves out (the AMPR files and the runtime).
int mp_import_left_out(const char *path);
// 1 for what LZ4 packs are at the root (ampr_assets.index, its packs, an ampr_assets folder), ASCII case aside.
int mp_import_lz4(const char *path);

#endif
