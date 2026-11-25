// SPDX-License-Identifier: GPL-3.0-or-later
// Whether a dump is complete: its files against the list of the files of the original package's
// image (sce_sys/playgo-hash-table.dat, see mp_playgo.h). The list has hashes, not names: what is
// missing is a count. A file of the dump that the list does not have is extra; of those, the ones
// dump tools are known to leave behind are leftovers.
//
// The rule, for each file of the dump:
//   - listed: the table has its hash (case does not matter);
//   - else ignored: OS junk (Thumbs.db, .DS_Store, ._*, __MACOSX/...), and anything in sce_sys/,
//     where a dump puts the package's own entries (param.json, the PlayGo tables...), which are not
//     files of the image;
//   - else extra; a leftover when dump tools leave it: a decrypted/ folder at the root (the raw ELF
//     copies of the executables), *.auth_info (written next to the executables) and
//     entitlements.txt or entitlement_key.dat at the root.
// sce_sys/about/right.sprx is not counted, listed or not: package builders add their own.
// Without a usable table nothing is checked, and the leftovers are only the files that match those
// names: nothing proves they are not the game's.
#ifndef MP_COMPLETE_H
#define MP_COMPLETE_H

#include <stddef.h>
#include <stdint.h>

#include "mp_err.h"
#include "mp_json.h"
#include "mp_source.h"

typedef struct {
    const char *path;   // relative, with '/'
    uint64_t size;
} mp_cfile;

// The class of each file (mp_complete_check's cls).
enum { MP_CF_IGNORED = 0, MP_CF_LISTED = 1, MP_CF_EXTRA = 2, MP_CF_LEFTOVER = 3 };

#define MP_COMPLETE_PATHS 20

typedef struct {
    int checkable;
    // "" when checkable; else "no_table", "unknown_format", "damaged" or "unreadable" (static)
    const char *reason;
    uint64_t listed, present, missing;
    uint64_t extra_count, extra_bytes;
    uint64_t leftover_count, leftover_bytes;
    // the first extra files, in the order given (a source's: by path), malloc
    size_t npaths;
    char *paths[MP_COMPLETE_PATHS];
} mp_complete;

// Checks files[0..n) against table (the bytes of playgo-hash-table.dat; NULL when the dump has none).
// cls, when not NULL, gets the class of each file. 0, or -1 when memory runs out (c says nothing).
int mp_complete_check(const mp_cfile *files, size_t n, const uint8_t *table, size_t table_len, mp_complete *c,
                      uint8_t *cls);
// The same over the files of a source, with the table it carries. A table that cannot be read
// leaves it unchecked ("unreadable"). cls: one per entry of the source. -1 only out of memory.
int mp_complete_source(mp_source *s, mp_complete *c, uint8_t *cls, mp_err *e);
// Frees the paths; c can be checked again.
void mp_complete_free(mp_complete *c);
// Keeps only the counts (frees the paths): for a result that is copied around.
void mp_complete_counts_only(mp_complete *c);

// 1 for what dump tools leave behind, by its path alone.
int mp_complete_is_leftover(const char *path);
// 1 for the files of the OS (Thumbs.db, .DS_Store, ._*) and what is in its folders.
int mp_complete_is_junk(const char *path);

// The object {"checkable":...,"reason":...,"listed":...,...}; with_paths adds "extra_paths".
void mp_complete_json(const mp_complete *c, int with_paths, mp_jbuf *b);
// One line: "complete: all N files of the original package", "incomplete: N of the M files of the
// original package are missing", or "not checked: <why>".
void mp_complete_summary(const mp_complete *c, char *buf, size_t cap);
// What is not in the original package, and what of it dump tools left (empty when there is none).
void mp_complete_extra_line(const mp_complete *c, char *buf, size_t cap);

#endif
