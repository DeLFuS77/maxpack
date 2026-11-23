// SPDX-License-Identifier: GPL-3.0-or-later
// Instalar un .p5z: carpeta temporal fuera de las rutas de los cargadores, archivos verificados
// con su SHA-256 y forzados a disco, runtime listo, y renombrado al destino al final.
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>

#include "mp_hash.h"
#include "mp_idx3.h"
#include "mp_inst_internal.h"
#include "mp_p5z_internal.h"
#include "mp_platform.h"

#define RUNTIME_PATH "fakelib/libSceAmpr.sprx"
#define RUNTIME_MAX (64u << 20)

static int ieq(const char *a, const char *b) {
    for (; *a && *b; a++, b++) {
        char x = *a, y = *b;
        if (x >= 'A' && x <= 'Z') x = (char)(x + 32);
        if (y >= 'A' && y <= 'Z') y = (char)(y + 32);
        if (x != y) return 0;
    }
    return *a == *b;
}

static int has_marker(const uint8_t *p, size_t n) {
    for (size_t i = 0; i + 8 <= n; i++)
        if (p[i] == 'A' && !memcmp(p + i, "AMPRZPK1", 8)) return 1;
    return 0;
}

typedef struct {
    uint8_t *buf;
    size_t n, cap;
} membuf;

static int to_mem(void *ctx, const void *p, size_t n, mp_err *e) {
    membuf *m = ctx;
    if (m->n + n > m->cap) {
        mp_err_set(e, "archivo más grande de lo esperado");
        return -1;
    }
    memcpy(m->buf + m->n, p, n);
    m->n += n;
    return 0;
}

static int read_to_mem(mp_p5z *p, size_t i, uint8_t **out, size_t *len, mp_err *e) {
    membuf m = {malloc(p->files[i].size ? (size_t)p->files[i].size : 1), 0, (size_t)p->files[i].size};
    if (!m.buf) {
        mp_err_set(e, "sin memoria");
        return -1;
    }
    if (p5z_read_file(p, i, to_mem, &m, e) != 0) {
        free(m.buf);
        return -1;
    }
    *out = m.buf;
    *len = m.n;
    return 0;
}

void mp_inst_result_free(mp_inst_result *r) {
    mp_inst_existing_free(r->existing, r->nexisting);
    r->existing = NULL;
    r->nexisting = 0;
}

static void set_result(mp_inst_result *r, int status, const char *code) {
    r->status = status;
    r->code = code;
}

static int dest_ok(const mp_inst_env *env, const char *root) {
    if (!root || mp_path_kind(root) != 2) return 0;
    if (!strcmp(root, env->internal)) return 1;
    const char *d[16];
    size_t n = mp_inst_drives(env, d, 16);
    for (size_t i = 0; i < n; i++)
        if (!strcmp(root, d[i])) return 1;
    return 0;
}

// ---- escritura en paralelo ----------------------------------------------------------------

typedef struct {
    mp_p5z *p;
    const mp_inst_env *env;
    const char *tmp;
    const size_t *todo;
    size_t ntodo, next;
    mp_mutex *mu;
    uint64_t written, total;
    int stop, fail, fail_io;
    mp_err err;
} job;

typedef struct {
    job *j;
    mp_wfile *w;
    int sink_failed;
} wctx;

static int sink_write(void *ctx, const void *p, size_t n, mp_err *e) {
    wctx *c = ctx;
    job *j = c->j;
    if (mp_wfile_write(c->w, p, n, e) != 0) {
        c->sink_failed = 1;
        return -1;
    }
    int rc = 0;
    mp_mutex_lock(j->mu);
    j->written += n;
    if (j->env->progress) j->env->progress(j->env->ctx, "copiando", j->written, j->total);
    if (j->stop || j->fail) rc = -1;
    else if (j->env->io_check) {
        int k = j->env->io_check(j->env->ctx, j->written, e);
        if (k == 1) {
            j->stop = 1;
            rc = -1;
        } else if (k != 0) {
            rc = -1;
        }
    }
    mp_mutex_unlock(j->mu);
    if (rc) c->sink_failed = 1;
    return rc;
}

static void fail_job(job *j, const mp_err *e, int io) {
    mp_mutex_lock(j->mu);
    if (!j->fail && !j->stop) {
        j->fail = 1;
        j->fail_io = io;
        j->err = *e;
    }
    mp_mutex_unlock(j->mu);
}

static void write_one(job *j, size_t i) {
    const p5z_file *f = &j->p->files[i];
    mp_err e = {{0}};
    char *path = mpi_join(j->tmp, f->path);
    wctx c = {j, NULL, 0};
    int rc = path ? mp_wfile_create(path, &c.w, &e) : -1;
    if (!path) mp_err_set(&e, "sin memoria");
    int io = rc != 0;
    if (rc == 0) {
        rc = p5z_read_file(j->p, i, sink_write, &c, &e);
        io = c.sink_failed;
    }
    if (rc == 0) {
        rc = mp_wfile_sync(c.w, &e);
        io = 1;
    }
    if (c.w && mp_wfile_close(c.w, rc ? NULL : &e) != 0 && rc == 0) {
        rc = -1;
        io = 1;
    }
    if (rc == 0 && f->mtime && mp_set_mtime(path, f->mtime, &e) != 0) {
        rc = -1;
        io = 1;
    }
    free(path);
    if (rc) {
        mp_mutex_lock(j->mu);
        int stopped = j->stop;
        mp_mutex_unlock(j->mu);
        if (!stopped) fail_job(j, &e, io);
    }
}

static void worker(void *arg) {
    job *j = arg;
    for (;;) {
        mp_mutex_lock(j->mu);
        size_t k = j->next;
        int quit = j->stop || j->fail || k >= j->ntodo;
        if (!quit) j->next++;
        mp_mutex_unlock(j->mu);
        if (quit) return;
        write_one(j, j->todo[k]);
    }
}

// Crea las carpetas de todos los archivos antes de repartirlos entre hilos.
static int make_parents(const char *tmp, mp_p5z *p, const size_t *todo, size_t n, mp_err *e) {
    char last[1100] = "";
    for (size_t k = 0; k < n; k++) {
        const char *rel = p->files[todo[k]].path;
        const char *s = strrchr(rel, '/');
        if (!s) continue;
        char d[1100];
        int len = snprintf(d, sizeof d, "%s/%.*s", tmp, (int)(s - rel), rel);
        if (len < 0 || (size_t)len >= sizeof d) {
            mp_err_set(e, "ruta demasiado larga: %s", rel);
            return -1;
        }
        if (!strcmp(d, last)) continue;
        if (mp_make_dirs(d, e) != 0) return -1;
        snprintf(last, sizeof last, "%s", d);
    }
    return 0;
}

static int run_job(job *j, unsigned threads) {
    if (threads < 1) threads = 1;
    if (threads > 32) threads = 32;
    mp_thread *t[32];
    unsigned started = 0;
    for (unsigned k = 1; k < threads; k++)
        if (mp_thread_start(&t[started], worker, j) == 0) started++;
    worker(j);
    for (unsigned k = 0; k < started; k++) mp_thread_join(t[k]);
    return j->fail ? -1 : 0;
}

// ---- runtime --------------------------------------------------------------------------------

typedef struct {
    int replace;          // poner el del instalador
    size_t rt_index;      // runtime del .p5z (SIZE_MAX si no hay)
    size_t idx3_index;    // ampr_emu.index del .p5z (SIZE_MAX si no hay)
    int has_packs;
} rt_plan;

static int plan_runtime(const mp_inst_env *env, mp_p5z *p, rt_plan *rp, mp_err *e) {
    rp->replace = 0;
    rp->rt_index = rp->idx3_index = SIZE_MAX;
    rp->has_packs = 0;
    for (size_t i = 0; i < p->nfiles; i++) {
        const p5z_file *f = &p->files[i];
        if (f->role == MP_ROLE_ORIGINAL) continue;
        if (f->role == MP_ROLE_ZPK_INDEX) rp->has_packs = 1;
        if (f->role == MP_ROLE_IDX3) rp->idx3_index = i;
        if (f->role == MP_ROLE_RUNTIME || ieq(f->path, RUNTIME_PATH)) rp->rt_index = i;
    }
    if (!rp->has_packs) return 0;   // sin packs, el runtime no hace falta
    int zstd = 0;
    if (rp->rt_index != SIZE_MAX && p->files[rp->rt_index].size <= RUNTIME_MAX) {
        uint8_t *b = NULL;
        size_t n = 0;
        if (read_to_mem(p, rp->rt_index, &b, &n, e) != 0) return -1;
        zstd = has_marker(b, n);
        free(b);
    }
    if (zstd) return 0;
    if (!env->runtime.data || !env->runtime.len) {
        mp_err_set(e, "el .p5z no trae un runtime que lea packs zstd (ampr-emu-ng) y este instalador no "
                      "lleva ninguno: vuelve a empaquetar con --runtime");
        return 1;
    }
    if (rp->idx3_index == SIZE_MAX) {
        mp_err_set(e, "el .p5z no trae ampr_emu.index");
        return -1;
    }
    rp->replace = 1;
    if (env->notify)
        env->notify(env->ctx, rp->rt_index == SIZE_MAX
                                  ? "El .p5z no trae runtime: se pone el ampr-emu-ng del instalador"
                                  : "El runtime del .p5z no lee packs zstd: se cambia por el ampr-emu-ng del instalador");
    return 0;
}

static int write_buffer(const char *dir, const char *rel, const uint8_t *p, size_t n, int64_t mtime, mp_err *e) {
    char *path = mpi_join(dir, rel);
    if (!path) {
        mp_err_set(e, "sin memoria");
        return -1;
    }
    char *s = strrchr(path, '/');
    *s = 0;
    int rc = mp_make_dirs(path, e);
    *s = '/';
    mp_wfile *w = NULL;
    if (rc == 0) rc = mp_wfile_create(path, &w, e);
    if (rc == 0) rc = mp_wfile_write(w, p, n, e);
    if (rc == 0) rc = mp_wfile_sync(w, e);
    if (w && mp_wfile_close(w, rc ? NULL : e) != 0) rc = -1;
    if (rc == 0 && mtime) rc = mp_set_mtime(path, mtime, e);
    free(path);
    return rc;
}

static int install_runtime(const mp_inst_env *env, mp_p5z *p, const rt_plan *rp, const char *tmp, mp_err *e) {
    uint8_t *idx = NULL, *fixed = NULL;
    size_t n = 0, fn = 0;
    if (read_to_mem(p, rp->idx3_index, &idx, &n, e) != 0) return -1;
    int rc = mp_idx3_set_row(idx, n, RUNTIME_PATH, env->runtime.len, env->runtime.mtime, &fixed, &fn, e);
    free(idx);
    if (rc == 0) rc = write_buffer(tmp, "ampr_emu.index", fixed, fn, p->files[rp->idx3_index].mtime, e);
    free(fixed);
    if (rc == 0) rc = write_buffer(tmp, RUNTIME_PATH, env->runtime.data, env->runtime.len, env->runtime.mtime, e);
    return rc;
}

// ---- instalar -------------------------------------------------------------------------------

int mp_inst_install(const mp_inst_env *env, const mp_inst_req *req, mp_inst_result *r, mp_err *e) {
    memset(r, 0, sizeof *r);
    mp_inst_found f;
    if (mp_inst_probe(req->p5z_path, &f, e) != 0) {
        set_result(r, MP_INST_FAILED, "bad_p5z");
        return -1;
    }
    snprintf(r->title_id, sizeof r->title_id, "%s", f.title_id);
    if (!dest_ok(env, req->dest_root)) {
        free(f.path);
        mp_err_set(e, "la unidad de destino no está disponible: %s", req->dest_root ? req->dest_root : "(ninguna)");
        set_result(r, MP_INST_FAILED, "bad_dest");
        return -1;
    }
    int internal = !strcmp(req->dest_root, env->internal);
    char final_dir[1024], tmp_root[1024], tmp[1100];
    snprintf(final_dir, sizeof final_dir, "%s/homebrew/%s", req->dest_root, f.title_id);
    snprintf(tmp_root, sizeof tmp_root, internal ? "%s/maxpack/tmp" : "%s/maxpack-tmp", req->dest_root);
    snprintf(tmp, sizeof tmp, "%s/%s", tmp_root, f.title_id);
    snprintf(r->final_dir, sizeof r->final_dir, "%s", final_dir);

    // Copias que ya hay: sin confirmar cada una, no se sigue.
    mp_inst_existing *ex = NULL;
    size_t nex = 0;
    if (mp_inst_find_existing(env, f.title_id, req->known, req->nknown, &ex, &nex, e) != 0) {
        free(f.path);
        set_result(r, MP_INST_FAILED, "io_error");
        return -1;
    }
    int unconfirmed = 0;
    for (size_t i = 0; i < nex; i++) {
        int ok = 0;
        for (size_t k = 0; k < req->nreplace && !ok; k++) ok = !strcmp(ex[i].path, req->replace[k]);
        unconfirmed |= !ok;
    }
    if (nex) {
        free(f.path);
        if (unconfirmed) {
            r->existing = ex;
            r->nexisting = nex;
            set_result(r, MP_INST_NEEDS_CONFIRM, "needs_confirm");
            return 0;
        }
        mp_inst_existing_free(ex, nex);   // el reemplazo confirmado llega en la Task 7
        mp_err_set(e, "reemplazar aún no está disponible");
        set_result(r, MP_INST_FAILED, "io_error");
        return -1;
    }

    // Espacio.
    uint64_t avail = 0;
    int frc = env->free_space ? env->free_space(env->ctx, req->dest_root, &avail)
                              : mp_disk_free(req->dest_root, &avail, e);
    r->need = f.install_size + env->space_margin;
    r->avail = avail;
    if (frc != 0) {
        free(f.path);
        set_result(r, MP_INST_FAILED, "io_error");
        return -1;
    }
    if (r->need > avail) {
        free(f.path);
        set_result(r, MP_INST_NO_SPACE, "no_space");
        return 0;
    }

    mp_p5z *p = NULL;
    if (mp_p5z_open(req->p5z_path, &p, e) != 0) {
        free(f.path);
        set_result(r, MP_INST_FAILED, "bad_p5z");
        return -1;
    }
    rt_plan rp;
    int prc = plan_runtime(env, p, &rp, e);
    if (prc != 0) {
        mp_p5z_close(p);
        free(f.path);
        set_result(r, MP_INST_FAILED, prc == 1 ? "no_runtime" : "bad_p5z");
        return -1;
    }

    // Carpeta temporal desde cero (la reanudación llega en la Task 6).
    size_t *todo = malloc((p->nfiles + 1) * sizeof *todo), ntodo = 0;
    int rc = todo ? 0 : -1;
    if (!todo) mp_err_set(e, "sin memoria");
    if (rc == 0) rc = mp_remove_tree(tmp, e);
    if (rc == 0) rc = mp_make_dirs(tmp, e);
    for (size_t i = 0; rc == 0 && i < p->nfiles; i++) {
        const p5z_file *pf = &p->files[i];
        if (pf->role == MP_ROLE_ORIGINAL) continue;
        if (rp.replace && (i == rp.rt_index || i == rp.idx3_index)) continue;
        todo[ntodo++] = i;
    }
    if (rc == 0) rc = make_parents(tmp, p, todo, ntodo, e);
    const char *fail_code = "io_error";
    int stopped = 0;
    if (rc == 0) {
        job j;
        memset(&j, 0, sizeof j);
        j.p = p;
        j.env = env;
        j.tmp = tmp;
        j.todo = todo;
        j.ntodo = ntodo;
        j.total = f.install_size;
        j.mu = mp_mutex_new();
        if (!j.mu) {
            mp_err_set(e, "sin memoria");
            rc = -1;
        } else {
            rc = run_job(&j, env->threads);
            stopped = j.stop;
            if (rc) {
                *e = j.err;
                if (!j.fail_io) fail_code = "bad_p5z";
            }
            mp_mutex_free(j.mu);
        }
    }
    if (rc == 0 && !stopped && rp.replace) rc = install_runtime(env, p, &rp, tmp, e);
    mp_inst_record rec;
    memset(&rec, 0, sizeof rec);
    if (rc == 0 && !stopped) {
        if (rp.replace) {
            mp_sha256 s;
            mp_sha256_init(&s);
            mp_sha256_update(&s, env->runtime.data, env->runtime.len);
            mp_sha256_final(&s, rec.runtime_sha);
        } else if (rp.rt_index != SIZE_MAX) {
            memcpy(rec.runtime_sha, p->files[rp.rt_index].sha, 32);
        }
    }
    mp_p5z_close(p);
    free(todo);
    if (rc == 0 && stopped) {
        free(f.path);
        set_result(r, MP_INST_STOPPED, "stopped");
        return 0;
    }
    if (rc != 0) {
        free(f.path);
        set_result(r, MP_INST_FAILED, fail_code);
        return -1;
    }

    // Al destino, y registro.
    if (env->progress) env->progress(env->ctx, "terminando", f.install_size, f.install_size);
    char hb[1100];
    snprintf(hb, sizeof hb, "%s/homebrew", req->dest_root);
    rc = mp_make_dirs(hb, e);
    if (rc == 0) rc = mp_rename(tmp, final_dir, e);
    if (rc == 0) {
        snprintf(rec.title_id, sizeof rec.title_id, "%s", f.title_id);
        snprintf(rec.version, sizeof rec.version, "%s", f.version);
        snprintf(rec.name, sizeof rec.name, "%s", f.name);
        snprintf(rec.dir, sizeof rec.dir, "%s", final_dir);
        snprintf(rec.p5z, sizeof rec.p5z, "%s", req->p5z_path);
        memcpy(rec.archive_id, f.archive_id, 16);
        rec.runtime_replaced = rp.replace;
        rec.smp_pending = 1;
        rec.installed_at = (int64_t)time(NULL);
        rc = mp_inst_record_save(env, &rec, e);
    }
    r->runtime_replaced = rp.replace;
    if (rc != 0) {
        free(f.path);
        set_result(r, MP_INST_FAILED, "io_error");
        return -1;
    }
    if (env->notify) {
        char msg[300];
        snprintf(msg, sizeof msg, "%s instalado", f.name[0] ? f.name : f.title_id);
        env->notify(env->ctx, msg);
    }
    free(f.path);
    set_result(r, MP_INST_DONE, "done");
    return 0;
}
