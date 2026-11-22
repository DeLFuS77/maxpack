// SPDX-License-Identifier: GPL-3.0-or-later
#include "mp_hash.h"

#define XXH_INLINE_ALL
#include "common/xxhash.h"

uint64_t mp_xxh64(const void *p, size_t n, uint64_t seed) { return XXH64(p, n, seed); }
