// SPDX-License-Identifier: GPL-3.0-or-later
// Test tool for make_fpkg.py: encrypts the outer PFS of a package it writes with the engine's AES
// (fast), instead of its pure-Python one.
//
//   mp_fpkgtool xts <file> <offset> <blocks> <data key hex> <tweak key hex> <signed from> <plain>
//
// encrypts, in place, the <blocks> blocks of 64 KiB at <offset> of <file>: block i with the sector i
// below <signed from>, 0x800000000000 | i from there on, and block <plain> (the superblock) left as is.
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "mp_crypto.h"
#include "mp_platform.h"

static int unhex(const char *h, uint8_t out[16]) {
    if (strlen(h) != 32) return -1;
    for (int i = 0; i < 16; i++) {
        unsigned v = 0;
        if (sscanf(h + 2 * i, "%2x", &v) != 1) return -1;
        out[i] = (uint8_t)v;
    }
    return 0;
}

int main(int argc, char **argv) {
    mp_args_utf8(&argc, &argv);
    if (argc != 9 || strcmp(argv[1], "xts")) {
        fprintf(stderr, "usage: mp_fpkgtool xts <file> <offset> <blocks> <data key> <tweak key> <signed from> <plain>\n");
        return 2;
    }
    uint8_t dk[16], tk[16];
    if (unhex(argv[5], dk) || unhex(argv[6], tk)) {
        fprintf(stderr, "error: the keys are 32 hex digits\n");
        return 2;
    }
    unsigned long long off = strtoull(argv[3], NULL, 10), n = strtoull(argv[4], NULL, 10),
                       signed_from = strtoull(argv[7], NULL, 10), plain = strtoull(argv[8], NULL, 10);
    mp_aes128 d, t;
    mp_aes128_init(&d, dk);
    mp_aes128_init(&t, tk);
    mp_err e = {{0}};
    mp_file *f = NULL;
    mp_wfile *wf = NULL;
    if (mp_file_open_read(argv[2], &f, &e) != 0 || mp_wfile_reopen(argv[2], &wf, &e) != 0) {
        fprintf(stderr, "error: %s\n", e.msg);
        mp_file_close(f);
        return 1;
    }
    enum { BS = 0x10000, BATCH = 64 };
    uint8_t *buf = malloc((size_t)BS * BATCH);
    int rc = buf ? 0 : 1;
    for (unsigned long long i = 0; rc == 0 && i < n; i += BATCH) {
        size_t k = n - i < BATCH ? (size_t)(n - i) : BATCH, got = 0;
        if (mp_file_pread(f, buf, k * BS, off + i * BS, &got, &e) != 0 || got != k * BS) {
            rc = 1;
            break;
        }
        for (size_t j = 0; j < k; j++) {
            unsigned long long b = i + j;
            if (b == plain) continue;
            uint64_t sector = b < signed_from ? b : (0x800000000000ull | b);
            mp_xts_encrypt(&d, &t, sector, 0, buf + j * BS, BS);
        }
        if (mp_wfile_pwrite(wf, buf, k * BS, off + i * BS, &e) != 0) rc = 1;
    }
    free(buf);
    mp_file_close(f);
    if (mp_wfile_close(wf, &e) != 0) rc = 1;
    if (rc) fprintf(stderr, "error: %s\n", e.msg[0] ? e.msg : "cannot read the blocks");
    return rc;
}
