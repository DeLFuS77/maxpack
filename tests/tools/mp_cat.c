// SPDX-License-Identifier: GPL-3.0-or-later
// mp_cat <origen> <ruta> [off] [len]: vuelca bytes de un archivo de un origen a stdout.
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#ifdef _WIN32
#include <fcntl.h>
#include <io.h>
#endif

#include "mp_source.h"

int main(int argc, char **argv) {
    if (argc < 3) {
        fprintf(stderr, "uso: mp_cat <origen> <ruta> [off] [len]\n");
        return 2;
    }
#ifdef _WIN32
    _setmode(_fileno(stdout), _O_BINARY);
#endif
    mp_err e = {{0}};
    mp_source *s = NULL;
    if (mp_source_open(argv[1], &s, &e) != 0) {
        fprintf(stderr, "error: %s\n", e.msg);
        return 1;
    }
    size_t idx = (size_t)-1;
    for (size_t i = 0; i < mp_source_count(s); i++)
        if (strcmp(mp_source_entry(s, i)->path, argv[2]) == 0) idx = i;
    if (idx == (size_t)-1) {
        fprintf(stderr, "error: no está en el origen: %s\n", argv[2]);
        mp_source_close(s);
        return 1;
    }
    uint64_t size = mp_source_entry(s, idx)->size;
    uint64_t off = argc > 3 ? strtoull(argv[3], NULL, 10) : 0;
    uint64_t len = argc > 4 ? strtoull(argv[4], NULL, 10) : size;
    if (off > size) off = size;
    if (len > size - off) len = size - off;
    size_t cap = 1 << 20;
    unsigned char *buf = malloc(cap);
    int rc = 0;
    while (len && buf) {
        size_t k = len < cap ? (size_t)len : cap;
        if (mp_source_read(s, idx, off, buf, k, &e) != 0) {
            fprintf(stderr, "error: %s\n", e.msg);
            rc = 1;
            break;
        }
        fwrite(buf, 1, k, stdout);
        off += k;
        len -= k;
    }
    free(buf);
    mp_source_close(s);
    return rc;
}
