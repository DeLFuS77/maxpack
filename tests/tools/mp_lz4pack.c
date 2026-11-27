// SPDX-License-Identifier: GPL-3.0-or-later
// Test tool for test_import.py: mp_lz4pack <source> <folder> [packs]: the files of a game (a folder or an
// image) made into LZ4 packs in a new folder, as tests/mp_lz4pack.h lays them out (another tool's split of
// loose and packed files, 4 KiB pages, the CRC sidecar and a runtime profile).
#include <stdio.h>
#include <stdlib.h>

#include "mp_lz4pack.h"

int main(int argc, char **argv) {
    if (argc < 3 || argc > 4) {
        fprintf(stderr, "usage: mp_lz4pack <source> <folder> [packs]\n");
        return 2;
    }
    mpt_lz4_opts o = {argc == 4 ? atoi(argv[3]) : 3, 4096, mpt_lz4_odd, 1, 1, 0, 0, 0};
    if (o.npacks < 1 || o.npacks > 64) {
        fprintf(stderr, "packs: 1 to 64\n");
        return 2;
    }
    if (mpt_lz4_pack(argv[1], argv[2], &o) != 0) {
        fprintf(stderr, "error: could not make the LZ4 packs of %s\n", argv[1]);
        return 1;
    }
    printf("%llu LZ4 chunks, %llu raw, %llu shared\n", (unsigned long long)o.lz4_chunks,
           (unsigned long long)o.raw_chunks, (unsigned long long)o.shared_chunks);
    return 0;
}
