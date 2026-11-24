// SPDX-License-Identifier: GPL-3.0-or-later
// .p5z: the game packed into a single file to keep on the PC (design 6.4).
// Full format in docs/p5z-format.md.
#ifndef MP_P5Z_H
#define MP_P5Z_H

#include <stdint.h>
#include <stdio.h>

#include "mp_pack.h"

#define P5Z_HEADER 64u
#define P5Z_FOOTER 32u
#define P5Z_INDEX_HEADER 56u
#define P5Z_FILE_REC 88u
#define P5Z_FRAME_REC 16u
#define P5Z_MANIFEST_REC 64u
#define P5Z_STORE_RAW 0
#define P5Z_STORE_FRAMES 1
#define P5Z_NO_FILE 0xFFFFFFFFu

// .p5z output (the file must not exist). threads: threads to compress the loose files.
int mp_pack_out_p5z(const char *path, unsigned threads, mp_pack_out *out, mp_err *e);
// Same, with a cap: if the .p5z is going to go over max_size bytes, the write fails with the message
// "the .p5z would go over N bytes" and what was written is deleted (0 = no cap).
int mp_pack_out_p5z_ex(const char *path, unsigned threads, uint64_t max_size, mp_pack_out *out, mp_err *e);
// 1 if the .p5z output failed because of the cap (and not something else), 0 if not; ask before destroy.
int mp_pack_out_p5z_too_big(const mp_pack_out *out);

// ---- Reading (Task 7) -------------------------------------------------------------------

typedef struct mp_p5z mp_p5z;
int mp_p5z_open(const char *path, mp_p5z **out, mp_err *e);
const mp_pack_manifest *mp_p5z_manifest(const mp_p5z *p);
int mp_p5z_info(const mp_p5z *p, FILE *out);
int mp_p5z_zpk(mp_p5z *p, mp_zpk **out, mp_err *e);
uint64_t mp_p5z_volume_offset(const mp_p5z *p, uint32_t vol);   // where the volume starts in the .p5z
int mp_unpack_folder(mp_p5z *p, const char *out_dir, unsigned threads, int progress, mp_err *e);
int mp_unpack_original(mp_p5z *p, const char *out_dir, unsigned threads, int progress, mp_err *e);
void mp_p5z_close(mp_p5z *p);

#endif
