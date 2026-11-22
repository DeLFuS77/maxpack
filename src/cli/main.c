// SPDX-License-Identifier: GPL-3.0-or-later
#include <stdio.h>
#include <string.h>
#include <zlib.h>
#include <zstd.h>

#include "mp_version.h"

static void usage(FILE *out) {
    fprintf(out,
            "Uso:\n"
            "  maxpack analyze <carpeta|imagen.exfat> [opciones]\n"
            "  maxpack --version\n");
}

int main(int argc, char **argv) {
    if (argc >= 2 && strcmp(argv[1], "--version") == 0) {
        printf("maxpack %s (zstd %s, zlib %s)\n", mp_version_string(), ZSTD_versionString(),
               zlibVersion());
        return 0;
    }
    usage(stderr);
    return 2;
}
