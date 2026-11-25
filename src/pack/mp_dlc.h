// SPDX-License-Identifier: GPL-3.0-or-later
// The DLC of a game, packed with it: each DLC with files of its own goes to addcont0/, addcont1/...
// inside the game, and MaxPack writes the dlc_emu.ini that tells dlc_emu (drakmor/dlc_emu, which
// MaxPack for the console adds) what the game owns: a [PSAC] section with the content ID and the
// mount point /app0/addcontN of each DLC with files, a [PSAL] one with download_status=NO_EXTRA_DATA
// for each license. The game then sees its DLC installed, from inside the .p5z, as a game in a folder
// does with a dlc_emu.ini written by hand.
//
// A DLC is a package of additional content (CNT content type 0x21), or a folder or image with
// sce_sys/param.json and no eboot.bin at its root. Its content ID is the package's, or its
// param.json's contentId: 36 characters (XXYYYY-TITLEID_00-LABEL, as dlc_emu wants it), of the game's
// title ID. One with no file outside sce_sys/ (OS junk aside) is a license only and adds no folder.
#ifndef MP_DLC_H
#define MP_DLC_H

#include <stddef.h>
#include <stdint.h>

#include "mp_complete.h"
#include "mp_err.h"
#include "mp_source.h"

#define MP_DLC_MAX_DATA 100          // addcont0 to addcont99: dlc_emu takes mount points of at most 15 characters
#define MP_DLC_INI "dlc_emu.ini"
#define MP_DLC_INI_MAX (32u << 10)   // the most of it dlc_emu reads
#define MP_DLC_MAX_ENTRIES 1024      // the most entries dlc_emu takes

typedef struct {
    char content_id[37];
    int data;            // it has files of its own (in folder); else a license only
    char folder[16];     // "addcont0" ("" for a license)
    uint64_t files, bytes;   // what is in folder
} mp_dlc;

typedef struct {
    mp_dlc *v;
    size_t n;
} mp_dlc_list;

void mp_dlc_list_free(mp_dlc_list *l);

// Opens the DLC at paths[0..n) (packages with mp_source_set_passcode's passcode), checks them against
// the game and gives the game with them (mp_source_merge): *game is taken; on success it is the game
// with its DLC, on failure it is closed and NULL, with the reason in e and *code "dlc_has_ini" (the
// game has its own dlc_emu.ini), "dlc_too_many" (more than MP_DLC_MAX_DATA with files, or an ini of
// more than MP_DLC_INI_MAX) or "dlc_refused" (any other). out, when not NULL, gets what each DLC is,
// in the order given.
int mp_dlc_attach(mp_source **game, const char *const *paths, size_t n, mp_dlc_list *out, const char **code,
                  mp_err *e);

// The dlc_emu.ini of v[0..n) (malloc, NUL-terminated; *len its bytes); NULL out of memory.
char *mp_dlc_ini(const mp_dlc *v, size_t n, size_t *len);

// 1 if cid is a content ID dlc_emu takes: 36 characters, '-' at 6 and 19, '_' at 16, the rest ASCII
// letters and digits.
int mp_dlc_content_id_ok(const char *cid);

#endif
