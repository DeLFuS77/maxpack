// SPDX-License-Identifier: GPL-3.0-or-later
// Utilities for the installer tests (and test_unpack): pack, clean, compare.
#ifndef MP_FIXTURE_H
#define MP_FIXTURE_H

#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "mp_pack.h"
#include "mp_p5z.h"
#include "mp_platform.h"
#include "mp_rules.h"
#include "mp_test.h"

#define MPT_BASIC MP_FIXTURES_DIR "/basic.exfat"

// Deletes a folder or file, but only inside MP_TEST_TMP.
static inline void mpt_clear(const char *path) {
    if (strncmp(path, MP_TEST_TMP, strlen(MP_TEST_TMP)) != 0) return;
    mp_err e = {{0}};
    if (mp_remove_tree(path, &e) != 0) MP_FAIL_("could not clean %s: %s", path, e.msg);
}

static inline void mpt_parent_dirs(const char *path) {
    char d[2048];
    snprintf(d, sizeof d, "%s", path);
    char *s = strrchr(d, '/');
    if (s && s > d) {
        *s = 0;
        mp_err e = {{0}};
        mp_make_dirs(d, &e);
    }
}

static inline uint8_t *mpt_slurp(const char *path, size_t *len) {
    mp_file *f = NULL;
    uint64_t size = 0;
    size_t got = 0;
    *len = 0;
    if (mp_file_open_read(path, &f, NULL) != 0) return NULL;
    mp_file_size(f, &size, NULL);
    uint8_t *b = malloc((size_t)size + 1);
    if (b) mp_file_pread(f, b, (size_t)size, 0, &got, NULL);
    mp_file_close(f);
    *len = got;
    return b;
}

static inline void mpt_put(const char *path, const void *data, size_t n) {
    mpt_parent_dirs(path);
    mp_err e = {{0}};
    if (mp_write_file(path, data, n, &e) != 0) MP_FAIL_("%s", e.msg);
}

static inline void mpt_put_text(const char *path, const char *text) { mpt_put(path, text, strlen(text)); }

static inline void mpt_copy_file(const char *src, const char *dst) {
    size_t n = 0;
    uint8_t *b = mpt_slurp(src, &n);
    if (!b) {
        MP_FAIL_("cannot read %s", src);
        return;
    }
    mpt_put(dst, b, n);
    free(b);
}

// Packs src (folder or .exfat) to dst: .p5z if p5z, otherwise a folder. runtime may be NULL.
// volume_limit: maximum size of each volume (MP_ZPK_VOLUME_LIMIT, or less to get several).
static inline int mpt_pack_vol(const char *src, const char *dst, int p5z, const char *runtime,
                               uint64_t volume_limit) {
    mp_err e = {{0}};
    mpt_clear(dst);
    mpt_parent_dirs(dst);
    mp_pack_opts o;
    mp_pack_opts_default(&o);
    o.threads = 4;
    o.progress = 0;
    o.runtime = runtime;
    o.volume_limit = volume_limit;
    mp_pack_out out;
    if ((p5z ? mp_pack_out_p5z(dst, 4, &out, &e) : mp_pack_out_dir(dst, &out, &e)) != 0) {
        MP_FAIL_("pack output: %s", e.msg);
        return -1;
    }
    mp_pack_result r;
    int rc = mp_pack_run(src, &out, &o, &r, &e);
    out.destroy(out.ctx);
    if (rc) MP_FAIL_("pack: %s", e.msg);
    return rc;
}

static inline int mpt_pack(const char *src, const char *dst, int p5z, const char *runtime) {
    return mpt_pack_vol(src, dst, p5z, runtime, MP_ZPK_VOLUME_LIMIT);
}

typedef struct {
    char **v;
    uint64_t *size;
    size_t n, cap;
} mpt_list;

static inline int mpt_list_cb(void *ctx, const char *rel, int is_dir, uint64_t size, int64_t mtime) {
    mpt_list *l = ctx;
    (void)mtime;
    if (is_dir) return 0;
    if (l->n == l->cap) {
        l->cap = l->cap ? 2 * l->cap : 256;
        l->v = realloc(l->v, l->cap * sizeof *l->v);
        l->size = realloc(l->size, l->cap * sizeof *l->size);
    }
    l->size[l->n] = size;
    l->v[l->n++] = mp_strdup(rel);
    return 0;
}

static inline int mpt_cmp_str(const void *a, const void *b) {
    return strcmp(*(char *const *)a, *(char *const *)b);
}

// Files of the folder (sorted relative paths).
static inline mpt_list mpt_list_dir(const char *dir) {
    mpt_list l = {0};
    mp_dir_walk(dir, mpt_list_cb, &l, NULL);
    if (l.n > 1) {   // sorts paths and sizes together
        for (size_t i = 1; i < l.n; i++)
            for (size_t j = i; j > 0 && strcmp(l.v[j - 1], l.v[j]) > 0; j--) {
                char *t = l.v[j];
                l.v[j] = l.v[j - 1];
                l.v[j - 1] = t;
                uint64_t s = l.size[j];
                l.size[j] = l.size[j - 1];
                l.size[j - 1] = s;
            }
    }
    return l;
}

static inline void mpt_list_free(mpt_list *l) {
    for (size_t i = 0; i < l->n; i++) free(l->v[i]);
    free(l->v);
    free(l->size);
    memset(l, 0, sizeof *l);
}

static inline uint64_t mpt_dir_size(const char *dir) {
    mpt_list l = mpt_list_dir(dir);
    uint64_t t = 0;
    for (size_t i = 0; i < l.n; i++) t += l.size[i];
    mpt_list_free(&l);
    return t;
}

static inline int mpt_skip(const char *rel, const char *globs) {
    if (!globs) return 0;
    char buf[512];
    snprintf(buf, sizeof buf, "%s", globs);
    for (char *t = strtok(buf, "|"); t; t = strtok(NULL, "|"))
        if (mp_glob_match(t, rel)) return 1;
    return 0;
}

// 1 if a and b have the same files with the same bytes and dates (except the date of those that
// match skip_mtime: patterns separated by '|', e.g. "*.pak|ampr_emu.index").
static inline int mpt_dirs_equal(const char *a, const char *b, const char *skip_mtime) {
    mpt_list x = mpt_list_dir(a), y = mpt_list_dir(b);
    int ok = x.n == y.n && x.n > 0;
    if (x.n != y.n) MP_FAIL_("%s has %zu files and %s has %zu", a, x.n, b, y.n);
    for (size_t i = 0; i < x.n && i < y.n; i++) {
        char pa[2048], pb[2048];
        snprintf(pa, sizeof pa, "%s/%s", a, x.v[i]);
        snprintf(pb, sizeof pb, "%s/%s", b, y.v[i]);
        size_t la = 0, lb = 0;
        uint8_t *ba = mpt_slurp(pa, &la), *bb = mpt_slurp(pb, &lb);
        int64_t ma = 0, mb = 0;
        mp_stat(pa, NULL, &ma);
        mp_stat(pb, NULL, &mb);
        if (strcmp(x.v[i], y.v[i]) || !ba || !bb || la != lb || memcmp(ba, bb, la) ||
            (!mpt_skip(x.v[i], skip_mtime) && ma != mb)) {
            MP_FAIL_("distinto: %s / %s", x.v[i], y.v[i]);
            ok = 0;
        }
        free(ba);
        free(bb);
    }
    mpt_list_free(&x);
    mpt_list_free(&y);
    return ok;
}

#endif
