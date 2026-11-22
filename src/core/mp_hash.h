// SPDX-License-Identifier: GPL-3.0-or-later
#ifndef MP_HASH_H
#define MP_HASH_H

#include <stddef.h>
#include <stdint.h>

// XXH64 (la copia de xxhash que trae zstd, en línea).
uint64_t mp_xxh64(const void *p, size_t n, uint64_t seed);

#endif
