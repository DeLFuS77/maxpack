// SPDX-License-Identifier: GPL-3.0-or-later
// mp_readbench <source> [--blocks] [--threads N] [--piece KiB] [--budget MiB] [--list <file>]
// Reads a source's packable files the way the packer does (in the index's order, each file's SHA-256,
// cut into 256 KiB blocks that are copied as into the pipe's slots) and prints how fast, with no
// compression. By default through the read-ahead (its options can be changed); with --blocks the way
// 1.0.2's reading thread did it, one 256 KiB block at a time, read then hashed. With --list it writes
// "size<TAB>sha256<TAB>path" per file, sorted by path, to compare with a manifest.
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "mp_hash.h"
#include "mp_measure.h"
#include "mp_platform.h"
#include "mp_readahead.h"
#include "mp_rules.h"
#include "mp_source.h"

typedef struct {
    uint64_t hash;
    const char *path;
    uint32_t src;
} ord;

static int cmp_ord(const void *a, const void *b) {
    const ord *x = a, *y = b;
    if (x->hash != y->hash) return x->hash < y->hash ? -1 : 1;
    return strcmp(x->path, y->path);
}

static int cmp_path(const void *a, const void *b) {
    return strcmp((*(const ord *const *)a)->path, (*(const ord *const *)b)->path);
}

static void usage(void) {
    fprintf(stderr, "usage: mp_readbench <source> [--blocks] [--threads N] [--piece KiB] [--budget MiB] "
                    "[--list <file>]\n");
}

// 1.0.2's reading thread without the pipe: block by block, read then hash.
static int read_blocks(mp_source *s, const ord *o, size_t m, uint8_t (*sha)[32], uint64_t *t_read,
                       uint64_t *t_sha, mp_err *e) {
    uint8_t *buf = malloc(MP_BLOCK_V1);
    if (!buf) return -1;
    for (size_t k = 0; k < m; k++) {
        uint64_t size = mp_source_entry(s, o[k].src)->size;
        mp_sha256 h;
        mp_sha256_init(&h);
        for (uint64_t off = 0; off < size; off += MP_BLOCK_V1) {
            size_t len = size - off < MP_BLOCK_V1 ? (size_t)(size - off) : MP_BLOCK_V1;
            uint64_t a = mp_now_ns();
            if (mp_source_read(s, o[k].src, off, buf, len, e) != 0) {
                free(buf);
                return -1;
            }
            uint64_t b = mp_now_ns();
            mp_sha256_update(&h, buf, len);
            *t_read += b - a;
            *t_sha += mp_now_ns() - b;
        }
        mp_sha256_final(&h, sha[k]);
    }
    free(buf);
    return 0;
}

// Through the read-ahead; the pieces are cut into blocks and copied, as the packer's reading thread does.
static int read_ahead(mp_source *s, const ord *o, size_t m, uint8_t (*sha)[32], const mp_readahead_opts *ro,
                      uint64_t *t_wait, mp_err *e) {
    uint32_t *files = malloc((m ? m : 1) * sizeof *files);
    uint8_t *slot = malloc(MP_BLOCK_V1);
    mp_readahead *ra = NULL;
    int rc = -1;
    if (files && slot) {
        for (size_t k = 0; k < m; k++) files[k] = o[k].src;
        rc = mp_readahead_open(s, files, m, sha, ro, &ra, e);
    }
    mp_readahead_piece pc;
    uint64_t a = mp_now_ns();
    while (rc == 0 && (rc = mp_readahead_next(ra, &pc, e)) == 1) {
        *t_wait += mp_now_ns() - a;
        for (size_t at = 0; at < pc.len; at += MP_BLOCK_V1)
            memcpy(slot, pc.data + at, pc.len - at < MP_BLOCK_V1 ? pc.len - at : MP_BLOCK_V1);
        a = mp_now_ns();
        rc = 0;
    }
    mp_readahead_close(ra);
    free(files);
    free(slot);
    return rc;
}

int main(int argc, char **argv) {
    mp_args_utf8(&argc, &argv);
    const char *srcp = NULL, *list = NULL;
    int blocks = 0;
    mp_readahead_opts ro;
    mp_readahead_opts_default(&ro);
    for (int i = 1; i < argc; i++) {
        const char *a = argv[i];
        int more = i + 1 < argc;
        if (!strcmp(a, "--list") && more) list = argv[++i];
        else if (!strcmp(a, "--blocks")) blocks = 1;
        else if (!strcmp(a, "--threads") && more) ro.threads = (unsigned)strtoul(argv[++i], NULL, 10);
        else if (!strcmp(a, "--piece") && more) ro.piece = (size_t)strtoul(argv[++i], NULL, 10) << 10;
        else if (!strcmp(a, "--budget") && more) ro.budget = (size_t)strtoul(argv[++i], NULL, 10) << 20;
        else if (!srcp && a[0] != '-') srcp = a;
        else {
            usage();
            return 2;
        }
    }
    if (!srcp || ro.piece == 0 || ro.piece % MP_BLOCK_V1) {
        usage();
        return 2;
    }
    mp_err e = {{0}};
    mp_source *s = NULL;
    uint64_t t_open = mp_now_ns();
    if (mp_source_open(srcp, &s, &e) != 0) {
        fprintf(stderr, "error: %s\n", e.msg);
        return 1;
    }
    t_open = mp_now_ns() - t_open;
    size_t n = mp_source_count(s), m = 0;
    ord *o = malloc((n ? n : 1) * sizeof *o);
    uint8_t (*sha)[32] = malloc((n ? n : 1) * 32);
    mp_rules *rules = mp_rules_new();
    if (!o || !sha || !rules) return 1;
    uint64_t total = 0;
    for (size_t i = 0; i < n; i++) {
        const mp_entry *en = mp_source_entry(s, i);
        if (mp_rules_classify(rules, en->path, en->size) != MP_PACKABLE) continue;
        o[m].hash = mp_zpk_path_hash(en->path);
        o[m].path = en->path;
        o[m].src = (uint32_t)i;
        total += en->size;
        m++;
    }
    mp_rules_free(rules);
    qsort(o, m, sizeof *o, cmp_ord);

    uint64_t t0 = mp_now_ns(), t_read = 0, t_sha = 0, t_wait = 0;
    int rc = blocks ? read_blocks(s, o, m, sha, &t_read, &t_sha, &e) : read_ahead(s, o, m, sha, &ro, &t_wait, &e);
    double secs = (double)(mp_now_ns() - t0) / 1e9;
    if (rc != 0) {
        fprintf(stderr, "error: %s\n", e.msg[0] ? e.msg : "out of memory");
        return 1;
    }
    if (blocks)
        printf("blocks: %zu files, %.3f GB in %.2f s = %.0f MB/s (read %.2f s, SHA-256 %.2f s; open %.2f s)\n", m,
               (double)total / 1e9, secs, (double)total / secs / 1e6, (double)t_read / 1e9, (double)t_sha / 1e9,
               (double)t_open / 1e9);
    else
        printf("read-ahead (%u threads, %zu KiB pieces, %zu MiB): %zu files, %.3f GB in %.2f s = %.0f MB/s "
               "(waited %.2f s; open %.2f s)\n",
               ro.threads, ro.piece >> 10, ro.budget >> 20, m, (double)total / 1e9, secs,
               (double)total / secs / 1e6, (double)t_wait / 1e9, (double)t_open / 1e9);

    mp_sha256 all;
    mp_sha256_init(&all);
    for (size_t k = 0; k < m; k++) mp_sha256_update(&all, sha[k], 32);
    uint8_t d[32];
    char hex[65];
    mp_sha256_final(&all, d);
    mp_sha256_hex(d, hex);
    printf("digest of the files' SHA-256s in the index's order: %s\n", hex);
    if (list) {
        FILE *f = fopen(list, "wb");
        const ord **byp = malloc((m ? m : 1) * sizeof *byp);
        if (!f || !byp) return 1;
        for (size_t k = 0; k < m; k++) byp[k] = &o[k];
        qsort(byp, m, sizeof *byp, cmp_path);
        for (size_t k = 0; k < m; k++) {
            mp_sha256_hex(sha[byp[k] - o], hex);
            fprintf(f, "%llu\t%s\t%s\n", (unsigned long long)mp_source_entry(s, byp[k]->src)->size, hex,
                    byp[k]->path);
        }
        fclose(f);
        free(byp);
    }
    free(o);
    free(sha);
    mp_source_close(s);
    return 0;
}
