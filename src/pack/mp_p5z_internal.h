// SPDX-License-Identifier: GPL-3.0-or-later
// Internals of the .p5z reader, shared by mp_p5z_read.c, mp_unpack.c and mp_verify.c.
#ifndef MP_P5Z_INTERNAL_H
#define MP_P5Z_INTERNAL_H

#include "mp_p5z.h"
#include "mp_platform.h"

typedef struct {
    char *path;
    int role, storage;
    uint64_t size, data_off, data_len;
    int64_t mtime;
    uint32_t first_frame, nframes;
    uint8_t sha[32];
} p5z_file;

typedef struct {
    uint64_t off;
    uint32_t csize, usize;
} p5z_frame;

struct mp_p5z {
    mp_file *f;
    uint64_t size, index_off;
    p5z_file *files;
    size_t nfiles;
    p5z_frame *frames;
    size_t nframes;
    mp_pack_manifest man;
    uint32_t *man_file;   // index in files[] of each manifest record
};

// Reads the contents of files[i] in chunks (decompressing if needed) and checks its SHA-256.
typedef int (*p5z_sink)(void *ctx, const void *p, size_t n, mp_err *e);
int p5z_read_file(mp_p5z *p, size_t i, p5z_sink sink, void *ctx, mp_err *e);

#endif
