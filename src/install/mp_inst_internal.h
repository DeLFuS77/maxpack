// SPDX-License-Identifier: GPL-3.0-or-later
// Utilidades internas del motor del instalador.
#ifndef MP_INST_INTERNAL_H
#define MP_INST_INTERNAL_H

#include <stddef.h>
#include <stdint.h>

#include "mp_inst.h"

char *mpi_join(const char *a, const char *b);   // "a/b" (malloc)
// Lee un archivo entero de como mucho max bytes (malloc, acabado en NUL).
int mpi_read_small(const char *path, size_t max, char **out, size_t *len, mp_err *e);
// Escribe path.tmp y lo pone en lugar de path.
int mpi_write_replace(const char *path, const void *p, size_t n, mp_err *e);
void mpi_hex(const uint8_t *b, size_t n, char *out);       // out: 2n+1
int mpi_unhex(const char *s, uint8_t *b, size_t n);        // 0 si s son exactamente 2n hex
// titleId, contentVersion y titleName (el del idioma por defecto si lo hay) de un param.json.
int mpi_param_parse(const char *json, size_t n, char id[16], char ver[16], char name[128]);
// titleId de <dir>/sce_sys/param.json, o "" si no lo hay.
void mpi_dir_title(const char *dir, char id[16]);
uint64_t mpi_tree_size(const char *path);                 // archivo o carpeta entera
int mpi_has_ext(const char *name, const char *ext);       // ext con punto, sin distinguir mayúsculas
int mpi_record_path(const mp_inst_env *env, const char *title_id, char *out, size_t cap);

#endif
