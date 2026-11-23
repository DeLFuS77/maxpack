// SPDX-License-Identifier: GPL-3.0-or-later
// Estado de una instalación a medias, para reanudarla:
//   <internal>/maxpack/state/<TITLE_ID>.json  cabecera (archive_id, .p5z, destino)
//   <internal>/maxpack/state/<TITLE_ID>.done  una ruta por línea, cada una tras el fsync de su archivo
// Si se pierde una línea por un apagón, ese archivo se vuelve a escribir; al reanudar, todo lo
// apuntado se relee y se compara con su SHA-256, así que el registro solo ahorra trabajo.
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "mp_inst_internal.h"
#include "mp_json.h"
#include "mp_platform.h"

struct mpi_state {
    mp_wfile *done;
};

static void state_paths(const mp_inst_env *env, const char *id, char *json, char *done, size_t cap) {
    snprintf(json, cap, "%s/maxpack/state/%s.json", env->internal, id);
    snprintf(done, cap, "%s/maxpack/state/%s.done", env->internal, id);
}

void mpi_state_remove(const mp_inst_env *env, const char *title_id) {
    char j[1200], d[1200];
    state_paths(env, title_id, j, d, sizeof j);
    mp_remove_file(j);
    mp_remove_file(d);
}

static int header_matches(const char *path, const char *aid_hex, const char *dest_root) {
    char *buf = NULL;
    size_t n = 0;
    if (mp_path_kind(path) != 1 || mpi_read_small(path, 1u << 20, &buf, &n, NULL) != 0) return 0;
    mp_json *j = mp_json_parse(buf, n, NULL);
    free(buf);
    const char *a = mp_json_str(mp_json_get(j, "archive_id")), *d = mp_json_str(mp_json_get(j, "dest_root"));
    int ok = a && d && !strcmp(a, aid_hex) && !strcmp(d, dest_root);
    mp_json_free(j);
    return ok;
}

// Líneas completas del registro de hechos (las que acaban en '\n').
static int read_done(const char *path, char ***out, size_t *n, size_t *good_len, mp_err *e) {
    *out = NULL;
    *n = 0;
    *good_len = 0;
    char *buf = NULL;
    size_t len = 0;
    if (mp_path_kind(path) != 1) return 0;
    if (mpi_read_small(path, (size_t)1 << 30, &buf, &len, e) != 0) return -1;
    size_t cap = 0;
    char **v = NULL;
    size_t start = 0;
    for (size_t i = 0; i < len; i++) {
        if (buf[i] != '\n') continue;
        if (i > start) {
            if (*n == cap) {
                cap = cap ? 2 * cap : 256;
                char **nv = realloc(v, cap * sizeof *nv);
                if (!nv) break;
                v = nv;
            }
            buf[i] = 0;
            v[*n] = mp_strdup(buf + start);
            if (!v[*n]) break;
            (*n)++;
        }
        start = i + 1;
        *good_len = start;
    }
    free(buf);
    *out = v;
    return 0;
}

void mpi_done_free(char **v, size_t n) {
    for (size_t i = 0; i < n; i++) free(v[i]);
    free(v);
}

int mpi_state_begin(const mp_inst_env *env, const char *title_id, const uint8_t aid[16], const char *p5z,
                    const char *dest_root, const char *tmp, int *resumed, char ***done, size_t *ndone,
                    mpi_state **st, mp_err *e) {
    *resumed = 0;
    *done = NULL;
    *ndone = 0;
    *st = NULL;
    char jp[1200], dp[1200], dir[1200], aid_hex[33];
    state_paths(env, title_id, jp, dp, sizeof jp);
    snprintf(dir, sizeof dir, "%s/maxpack/state", env->internal);
    mpi_hex(aid, 16, aid_hex);
    if (mp_make_dirs(dir, e) != 0) return -1;
    if (mp_path_kind(tmp) == 2 && header_matches(jp, aid_hex, dest_root)) {
        size_t good = 0;
        if (read_done(dp, done, ndone, &good, e) != 0) return -1;
        // Sin la línea a medias del final (si un corte la dejó así).
        uint64_t size = 0;
        if (mp_stat(dp, &size, NULL) == 0 && size != good) {
            char *buf = NULL;
            size_t len = 0;
            if (mpi_read_small(dp, (size_t)1 << 30, &buf, &len, e) != 0) return -1;
            int rc = mpi_write_replace(dp, buf, good, e);
            free(buf);
            if (rc != 0) return -1;
        }
        *resumed = 1;
    } else {
        mpi_state_remove(env, title_id);
        if (mp_remove_tree(tmp, e) != 0) return -1;
        mp_jbuf b = {0};
        mp_jbuf_raw(&b, "{\"archive_id\":");
        mp_jbuf_str(&b, aid_hex);
        mp_jbuf_raw(&b, ",\"p5z\":");
        mp_jbuf_str(&b, p5z);
        mp_jbuf_raw(&b, ",\"dest_root\":");
        mp_jbuf_str(&b, dest_root);
        mp_jbuf_raw(&b, "}\n");
        int rc = b.oom ? -1 : mpi_write_replace(jp, b.p, b.n, e);
        if (b.oom) mp_err_set(e, "sin memoria");
        mp_jbuf_free(&b);
        if (rc == 0) rc = mp_write_file(dp, "", 0, e);
        if (rc != 0) return -1;
    }
    mpi_state *s = calloc(1, sizeof *s);
    if (!s) {
        mp_err_set(e, "sin memoria");
        return -1;
    }
    if (mp_wfile_reopen(dp, &s->done, e) != 0) {
        free(s);
        return -1;
    }
    *st = s;
    return 0;
}

int mpi_state_add(mpi_state *st, const char *rel, mp_err *e) {
    size_t n = strlen(rel);
    if (mp_wfile_write(st->done, rel, n, e) != 0) return -1;
    return mp_wfile_write(st->done, "\n", 1, e);
}

void mpi_state_close(mpi_state *st) {
    if (!st) return;
    if (st->done) mp_wfile_close(st->done, NULL);
    free(st);
}
