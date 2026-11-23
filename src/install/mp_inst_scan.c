// SPDX-License-Identifier: GPL-3.0-or-later
// Buscar .p5z, abrirlos y encontrar las copias de un juego que ya hay en la consola.
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "mp_inst_internal.h"
#include "mp_p5z_internal.h"
#include "mp_platform.h"

static const char *const ps5_drives[] = {"/mnt/usb0", "/mnt/usb1", "/mnt/usb2", "/mnt/usb3", "/mnt/usb4",
                                         "/mnt/usb5", "/mnt/usb6", "/mnt/usb7", "/mnt/ext0", "/mnt/ext1"};

void mp_inst_env_default(mp_inst_env *env) {
    memset(env, 0, sizeof *env);
    env->internal = "/data";
    env->drive_candidates = ps5_drives;
    env->ndrive_candidates = sizeof ps5_drives / sizeof *ps5_drives;
    env->threads = 4;
    env->space_margin = 256u << 20;
}

int mp_inst_title_ok(const char *t) {
    if (!t || strlen(t) != 9) return 0;
    for (int i = 0; i < 4; i++)
        if (t[i] < 'A' || t[i] > 'Z') return 0;
    for (int i = 4; i < 9; i++)
        if (t[i] < '0' || t[i] > '9') return 0;
    return 1;
}

size_t mp_inst_drives(const mp_inst_env *env, const char **out, size_t max) {
    size_t k = 0;
    for (size_t i = 0; i < env->ndrive_candidates && k < max; i++)
        if (mp_path_kind(env->drive_candidates[i]) == 2) out[k++] = env->drive_candidates[i];
    return k;
}

int mp_inst_probe(const char *path, mp_inst_found *out, mp_err *e) {
    memset(out, 0, sizeof *out);
    mp_p5z *p = NULL;
    if (mp_p5z_open(path, &p, e) != 0) return -1;
    const mp_pack_manifest *m = mp_p5z_manifest(p);
    if (!mp_inst_title_ok(m->title_id)) {
        mp_err_set(e, "%s no tiene un TITLE_ID válido (\"%s\"): hace falta un sce_sys/param.json con titleId "
                      "al empaquetarlo", path, m->title_id);
        mp_p5z_close(p);
        return -1;
    }
    snprintf(out->title_id, sizeof out->title_id, "%s", m->title_id);
    snprintf(out->version, sizeof out->version, "%s", m->content_version);
    snprintf(out->name, sizeof out->name, "%s", m->title_name);
    memcpy(out->archive_id, m->archive_id, 16);
    out->p5z_size = p->size;
    for (size_t i = 0; i < p->nfiles; i++)
        if (p->files[i].role != MP_ROLE_ORIGINAL) out->install_size += p->files[i].size;
    for (size_t i = 0; i < m->n; i++) out->original_size += m->v[i].size;
    mp_p5z_close(p);
    out->path = mp_strdup(path);
    if (!out->path) {
        mp_err_set(e, "sin memoria");
        return -1;
    }
    return 0;
}

void mp_inst_found_free(mp_inst_found *v, size_t n) {
    if (!v) return;
    for (size_t i = 0; i < n; i++) free(v[i].path);
    free(v);
}

typedef struct {
    char **names;
    int *is_dir;
    size_t n, cap;
} names;

static int names_cb(void *ctx, const char *name, int is_dir, uint64_t size) {
    names *l = ctx;
    (void)size;
    if (l->n == l->cap) {
        size_t cap = l->cap ? 2 * l->cap : 32;
        char **v = realloc(l->names, cap * sizeof *v);
        int *d = v ? realloc(l->is_dir, cap * sizeof *d) : NULL;
        if (v) l->names = v;
        if (!v || !d) return -1;
        l->is_dir = d;
        l->cap = cap;
    }
    l->names[l->n] = mp_strdup(name);
    if (!l->names[l->n]) return -1;
    l->is_dir[l->n++] = is_dir;
    return 0;
}

static void names_free(names *l) {
    for (size_t i = 0; i < l->n; i++) free(l->names[i]);
    free(l->names);
    free(l->is_dir);
    memset(l, 0, sizeof *l);
}

static int cmp_name(const void *a, const void *b) { return strcmp(*(char *const *)a, *(char *const *)b); }

typedef struct {
    const mp_inst_env *env;
    mp_inst_found *v;
    size_t n, cap;
    int oom;
} found_list;

static void add_found(found_list *fl, const char *path) {
    mp_inst_found f;
    mp_err e = {{0}};
    if (mp_inst_probe(path, &f, &e) != 0) {
        if (fl->env->notify) {
            char msg[700];
            snprintf(msg, sizeof msg, "Se salta %s: %s", path, e.msg);
            fl->env->notify(fl->env->ctx, msg);
        }
        return;
    }
    if (fl->n == fl->cap) {
        size_t cap = fl->cap ? 2 * fl->cap : 16;
        mp_inst_found *v = realloc(fl->v, cap * sizeof *v);
        if (!v) {
            free(f.path);
            fl->oom = 1;
            return;
        }
        fl->v = v;
        fl->cap = cap;
    }
    fl->v[fl->n++] = f;
}

// .p5z de dir y, si depth > 0, de sus subcarpetas (salvo la temporal del instalador).
static void scan_dir(found_list *fl, const char *dir, int depth) {
    names l = {0};
    if (mp_dir_list(dir, names_cb, &l, NULL) != 0) {
        names_free(&l);
        return;
    }
    for (size_t i = 0; i < l.n && !fl->oom; i++) {
        char *p = mpi_join(dir, l.names[i]);
        if (!p) {
            fl->oom = 1;
            break;
        }
        if (!l.is_dir[i] && mpi_has_ext(l.names[i], ".p5z")) add_found(fl, p);
        else if (l.is_dir[i] && depth > 0 && strcmp(l.names[i], "maxpack-tmp") != 0) scan_dir(fl, p, depth - 1);
        free(p);
    }
    names_free(&l);
}

static int cmp_found(const void *a, const void *b) {
    return strcmp(((const mp_inst_found *)a)->path, ((const mp_inst_found *)b)->path);
}

int mp_inst_scan(const mp_inst_env *env, mp_inst_found **out, size_t *n, mp_err *e) {
    *out = NULL;
    *n = 0;
    found_list fl = {env, NULL, 0, 0, 0};
    const char *drives[16];
    size_t nd = mp_inst_drives(env, drives, 16);
    for (size_t i = 0; i < nd; i++) scan_dir(&fl, drives[i], 1);
    char *mine = mpi_join(env->internal, "maxpack");
    if (mine) scan_dir(&fl, mine, 0);   // sin bajar: dentro están tmp/, state/ e installed/
    free(mine);
    if (fl.oom || !mine) {
        mp_inst_found_free(fl.v, fl.n);
        mp_err_set(e, "sin memoria");
        return -1;
    }
    if (fl.n > 1) qsort(fl.v, fl.n, sizeof *fl.v, cmp_found);
    *out = fl.v;
    *n = fl.n;
    return 0;
}

// ---- copias existentes --------------------------------------------------------------------

typedef struct {
    mp_inst_existing *v;
    size_t n, cap;
    int oom;
} ex_list;

static void add_existing(ex_list *x, const char *path) {
    int kind = mp_path_kind(path);
    if (kind == 0) return;
    for (size_t i = 0; i < x->n; i++)
        if (!strcmp(x->v[i].path, path) || mp_same_file(x->v[i].path, path)) return;
    if (x->n == x->cap) {
        size_t cap = x->cap ? 2 * x->cap : 8;
        mp_inst_existing *v = realloc(x->v, cap * sizeof *v);
        if (!v) {
            x->oom = 1;
            return;
        }
        x->v = v;
        x->cap = cap;
    }
    mp_inst_existing *it = &x->v[x->n];
    memset(it, 0, sizeof *it);
    it->path = mp_strdup(path);
    if (!it->path) {
        x->oom = 1;
        return;
    }
    it->is_file = kind == 1;
    it->size = mpi_tree_size(path);
    x->n++;
}

// Carpetas de root (un nivel) cuyo param.json es de title_id.
static void scan_root(ex_list *x, const char *root, const char *title_id) {
    names l = {0};
    if (mp_dir_list(root, names_cb, &l, NULL) != 0) {
        names_free(&l);
        return;
    }
    if (l.n > 1) {   // orden estable de resultados
        for (size_t i = 1; i < l.n; i++)
            for (size_t j = i; j > 0 && cmp_name(&l.names[j - 1], &l.names[j]) > 0; j--) {
                char *t = l.names[j];
                l.names[j] = l.names[j - 1];
                l.names[j - 1] = t;
                int d = l.is_dir[j];
                l.is_dir[j] = l.is_dir[j - 1];
                l.is_dir[j - 1] = d;
            }
    }
    for (size_t i = 0; i < l.n && !x->oom; i++) {
        if (!l.is_dir[i]) continue;
        char *p = mpi_join(root, l.names[i]);
        char id[16];
        if (!p) {
            x->oom = 1;
            break;
        }
        mpi_dir_title(p, id);
        if (!strcmp(id, title_id)) add_existing(x, p);
        free(p);
    }
    names_free(&l);
}

static void scan_base(ex_list *x, const char *base, int include_base, const char *title_id) {
    char *hb = mpi_join(base, "homebrew"), *eg = mpi_join(base, "etaHEN/games");
    char *direct = hb ? mpi_join(hb, title_id) : NULL;
    if (!hb || !eg || !direct) x->oom = 1;
    if (!x->oom && include_base) scan_root(x, base, title_id);
    if (!x->oom) scan_root(x, hb, title_id);
    if (!x->oom) scan_root(x, eg, title_id);
    if (!x->oom) add_existing(x, direct);   // ocupa el destino aunque sea de otro juego
    free(hb);
    free(eg);
    free(direct);
}

void mp_inst_existing_free(mp_inst_existing *v, size_t n) {
    if (!v) return;
    for (size_t i = 0; i < n; i++) free(v[i].path);
    free(v);
}

int mp_inst_find_existing(const mp_inst_env *env, const char *title_id, const char *const *known,
                          size_t nknown, mp_inst_existing **out, size_t *n, mp_err *e) {
    *out = NULL;
    *n = 0;
    if (!mp_inst_title_ok(title_id)) {
        mp_err_set(e, "TITLE_ID no válido: %s", title_id);
        return -1;
    }
    ex_list x = {0};
    scan_base(&x, env->internal, 0, title_id);
    const char *drives[16];
    size_t nd = mp_inst_drives(env, drives, 16);
    for (size_t i = 0; i < nd && !x.oom; i++) scan_base(&x, drives[i], 1, title_id);
    for (size_t i = 0; i < nknown && !x.oom; i++) add_existing(&x, known[i]);
    if (x.oom) {
        mp_inst_existing_free(x.v, x.n);
        mp_err_set(e, "sin memoria");
        return -1;
    }
    mp_inst_record r;
    if (mp_inst_record_load(env, title_id, &r, NULL) == 0)
        for (size_t i = 0; i < x.n; i++)
            x.v[i].from_maxpack = !strcmp(x.v[i].path, r.dir) || mp_same_file(x.v[i].path, r.dir);
    *out = x.v;
    *n = x.n;
    return 0;
}
