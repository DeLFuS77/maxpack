// SPDX-License-Identifier: GPL-3.0-or-later
// .p5z: el juego empaquetado en un solo archivo para guardarlo en el PC (diseño 6.4).
// Formato completo en docs/formato-p5z.md.
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

// Salida .p5z (el archivo no debe existir). threads: hilos para comprimir los sueltos.
int mp_pack_out_p5z(const char *path, unsigned threads, mp_pack_out *out, mp_err *e);

// ---- Lectura (Task 7) -------------------------------------------------------------------

typedef struct mp_p5z mp_p5z;
int mp_p5z_open(const char *path, mp_p5z **out, mp_err *e);
const mp_pack_manifest *mp_p5z_manifest(const mp_p5z *p);
int mp_p5z_info(const mp_p5z *p, FILE *out);
int mp_p5z_zpk(mp_p5z *p, mp_zpk **out, mp_err *e);
int mp_unpack_folder(mp_p5z *p, const char *out_dir, unsigned threads, int progress, mp_err *e);
int mp_unpack_original(mp_p5z *p, const char *out_dir, unsigned threads, int progress, mp_err *e);
void mp_p5z_close(mp_p5z *p);

#endif
