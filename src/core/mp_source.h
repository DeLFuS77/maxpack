// SPDX-License-Identifier: GPL-3.0-or-later
// Source of a game: a folder or an image (exFAT, UFS2 .ffpkg, PFS .ffpfs, or an .ffpfsc: a PFS image
// that holds one of the others, usually PFSC-compressed), seen as a list of files.
#ifndef MP_SOURCE_H
#define MP_SOURCE_H

#include <stddef.h>
#include <stdint.h>

#include "mp_err.h"

typedef struct {
    char *path;      // UTF-8, relative, with '/', no leading slash
    uint64_t size;
    int64_t mtime;   // Unix seconds
} mp_entry;

typedef enum { MP_SRC_DIR = 1, MP_SRC_EXFAT = 2, MP_SRC_UFS2 = 3, MP_SRC_PFS = 4 } mp_source_kind;

typedef struct mp_source mp_source;

// Detects the type: folder, or an image by its signature (exFAT, UFS2, PFS; the extension is only a
// hint for the message when it is none), or (to reject it with a clear message) a PKG. A PFS image
// whose only file is another image is a container: the files are those of the image inside.
int mp_source_open(const char *utf8_path, mp_source **out, mp_err *e);
// The kind of the file opened (MP_SRC_PFS for a container).
mp_source_kind mp_source_kind_of(const mp_source *s);
// "folder", "exfat", "ufs2", "pfs", or "pfsc" for a PFS image that holds another image (.ffpfsc).
const char *mp_source_kind_name(const mp_source *s);
// For "pfsc": the kind of the image inside that holds the files ("exfat", "ufs2" or "pfs"; the
// innermost when they nest); "" otherwise.
const char *mp_source_inner_name(const mp_source *s);
// In words, for reports: "folder", "exFAT image", "UFS2 image", "PFS image", "PFS container (exFAT
// image inside)".
const char *mp_source_kind_text(const mp_source *s, char *buf, size_t n);
// 1 if reading the files decompresses PFSC blocks.
int mp_source_compressed(const mp_source *s);
// Reading threads the source can keep busy (a read-ahead uses up to this many): more when reading
// decompresses, since then each thread is busy inflating rather than waiting for the disk.
unsigned mp_source_readers(const mp_source *s);
size_t mp_source_count(const mp_source *s);
// Sorted by strcmp of the path's bytes.
const mp_entry *mp_source_entry(const mp_source *s, size_t i);
size_t mp_source_dir_count(const mp_source *s);
// Index of the entry with that exact path (binary search; entries are sorted), or -1.
long mp_source_find(const mp_source *s, const char *path);
// Reads exactly len bytes; off + len must be <= size. Safe across threads.
int mp_source_read(mp_source *s, size_t i, uint64_t off, void *buf, size_t len, mp_err *e);
void mp_source_close(mp_source *s);

#endif
