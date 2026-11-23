// SPDX-License-Identifier: GPL-3.0-or-later
// Lectura mínima de sce_sys/param.json, compartida por pack y describe.
#ifndef MP_PARAM_H
#define MP_PARAM_H

#include <stddef.h>

// Valor de una clave de texto en un JSON pequeño: la primera aparición de "clave". Si no está, out
// queda vacío. Se corta a cap-1 bytes.
void mp_param_str(const char *json, size_t n, const char *key, char *out, size_t cap);

#endif
