// SPDX-License-Identifier: GPL-3.0-or-later
// Salida a carpeta: lo que se copia a la consola (los originales reemplazados no se escriben).
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "mp_pack.h"
#include "mp_platform.h"

typedef struct {
    char *dir;
    mp_wfile *cur;
    char *cur_path;
    int64_t cur_mtime;
    int skip;
    mp_zpk_sink zs;   // copia del sink de volúmenes, para liberarlo en destroy
} dirout;

static char *join(const char *dir, const char *rel) {
    size_t a = strlen(dir), b = strlen(rel);
    char *p = malloc(a + b + 2);
    if (!p) return NULL;
    memcpy(p, dir, a);
    p[a] = '/';
    memcpy(p + a + 1, rel, b + 1);
    return p;
}

static int d_begin(void *ctx, const char *rel, int role, uint64_t size, int64_t mtime, mp_err *e) {
    dirout *d = ctx;
    (void)size;
    if (role == MP_ROLE_ORIGINAL) {
        d->skip = 1;
        return 0;
    }
    d->cur_path = join(d->dir, rel);
    if (!d->cur_path) {
        mp_err_set(e, "sin memoria");
        return -1;
    }
    char *slash = strrchr(d->cur_path, '/');
    if (slash && slash > d->cur_path + strlen(d->dir)) {
        *slash = 0;
        int rc = mp_make_dirs(d->cur_path, e);
        *slash = '/';
        if (rc != 0) return -1;
    }
    d->cur_mtime = mtime;
    return mp_wfile_create(d->cur_path, &d->cur, e);
}

static int d_write(void *ctx, const void *p, size_t n, mp_err *e) {
    dirout *d = ctx;
    return d->skip ? 0 : mp_wfile_write(d->cur, p, n, e);
}

static int d_end(void *ctx, const uint8_t sha[32], mp_err *e) {
    dirout *d = ctx;
    (void)sha;
    if (d->skip) {
        d->skip = 0;
        return 0;
    }
    int rc = mp_wfile_close(d->cur, e);
    d->cur = NULL;
    if (rc == 0) rc = mp_set_mtime(d->cur_path, d->cur_mtime, e);
    free(d->cur_path);
    d->cur_path = NULL;
    return rc;
}

static int d_finish(void *ctx, const mp_pack_manifest *m, mp_err *e) {
    (void)ctx;
    (void)m;
    (void)e;
    return 0;
}

static void d_destroy(void *ctx) {
    dirout *d = ctx;
    if (!d) return;
    if (d->cur) mp_wfile_close(d->cur, NULL);
    mp_zpk_sink_free(&d->zs);
    free(d->cur_path);
    free(d->dir);
    free(d);
}

int mp_pack_out_dir(const char *out_dir, mp_pack_out *out, mp_err *e) {
    memset(out, 0, sizeof *out);
    int kind = mp_path_kind(out_dir);
    if (kind == 1) {
        mp_err_set(e, "la salida es un archivo, no una carpeta: %s", out_dir);
        return -1;
    }
    if (kind == 2 && mp_dir_is_empty(out_dir) != 1) {
        mp_err_set(e, "la carpeta de salida no está vacía: %s", out_dir);
        return -1;
    }
    if (kind == 0 && mp_make_dirs(out_dir, e) != 0) return -1;
    dirout *d = calloc(1, sizeof *d);
    if (!d || !(d->dir = mp_strdup(out_dir))) {
        free(d);
        mp_err_set(e, "sin memoria");
        return -1;
    }
    if (mp_zpk_sink_dir(out_dir, &out->zpk, e) != 0) {
        d_destroy(d);
        return -1;
    }
    d->zs = out->zpk;
    out->file_begin = d_begin;
    out->file_write = d_write;
    out->file_end = d_end;
    out->finish = d_finish;
    out->destroy = d_destroy;
    out->ctx = d;
    return 0;
}
