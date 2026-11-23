// SPDX-License-Identifier: GPL-3.0-or-later
// Instalador: pausa, cortes y reanudación con estado.
#include "mp_fixture.h"
#include "mp_inst.h"

#define TMP MP_TEST_TMP "/inst_resume"

static uint8_t rt_new[4096];
static const char *no_drives[] = {TMP "/usb1"};

typedef struct {
    uint64_t stop_at;     // parar (1) al pasar de aquí; 0 = nunca
    uint64_t fail_at;     // fallar (-1) al pasar de aquí; 0 = nunca
    uint64_t last;        // último bytes_written visto
    uint64_t disk;        // si no es 0: espacio libre = disk - lo que ocupe tmpdir
    const char *tmpdir;
} hook;

static int fs_hook(void *ctx, const char *path, uint64_t *out) {
    hook *h = ctx;
    (void)path;
    uint64_t used = h->tmpdir ? mpt_dir_size(h->tmpdir) : 0;
    *out = h->disk > used ? h->disk - used : 0;
    return 0;
}

static int io_hook(void *ctx, uint64_t written, mp_err *e) {
    hook *h = ctx;
    h->last = written;
    if (h->stop_at && written > h->stop_at) return 1;
    if (h->fail_at && written > h->fail_at) {
        mp_err_set(e, "disco lleno (simulado)");
        return -1;
    }
    return 0;
}

static mp_inst_env env_for(const char *internal, hook *h, int clear) {
    mp_inst_env env;
    mp_inst_env_default(&env);
    env.internal = internal;
    env.drive_candidates = no_drives;
    env.ndrive_candidates = 1;
    env.runtime.data = rt_new;
    env.runtime.len = sizeof rt_new;
    env.runtime.mtime = 1700000000;
    env.io_check = io_hook;
    env.ctx = h;
    if (h && h->disk) env.free_space = fs_hook;
    if (clear) {
        mp_err e = {{0}};
        mpt_clear(internal);
        mp_make_dirs(internal, &e);
    }
    return env;
}

static int run(const char *internal, const char *p5z, hook *h, int clear, mp_inst_result *r, mp_err *e) {
    mp_inst_env env = env_for(internal, h, clear);
    mp_inst_req q;
    memset(&q, 0, sizeof q);
    q.p5z_path = p5z;
    q.dest_root = internal;
    memset(e, 0, sizeof *e);
    return mp_inst_install(&env, &q, r, e);
}

static uint64_t install_size(const char *p5z) {
    mp_inst_found f;
    if (mp_inst_probe(p5z, &f, NULL) != 0) return 0;
    free(f.path);
    return f.install_size;
}

static int ready = 0;

static void setup(void) {
    if (ready) return;
    ready = 1;
    mpt_clear(TMP);
    for (size_t i = 0; i < sizeof rt_new; i++) rt_new[i] = (uint8_t)(i * 13 + 1);
    memcpy(rt_new + 64, "AMPRZPK1", 8);
    mp_err e = {{0}};
    mpt_put(TMP "/R.sprx", rt_new, sizeof rt_new);
    mp_set_mtime(TMP "/R.sprx", 1700000000, &e);
    mpt_pack(MPT_BASIC, TMP "/rt.p5z", 1, TMP "/R.sprx");
    mpt_pack(MPT_BASIC, TMP "/nort.p5z", 1, NULL);
    mpt_pack(MPT_BASIC, TMP "/folder_rt", 0, TMP "/R.sprx");
    mp_p5z *p = NULL;
    if (mp_p5z_open(TMP "/rt.p5z", &p, &e) == 0) {
        mp_unpack_folder(p, TMP "/unpacked_rt", 2, 0, &e);
        mp_p5z_close(p);
    }
}

#define FINAL(d) d "/homebrew/TEST00001"

MP_TEST(stop_then_resume) {
    setup();
    uint64_t total = install_size(TMP "/rt.p5z");
    hook h = {total * 4 / 10, 0, 0, 0, NULL};
    mp_inst_result r;
    mp_err e;
    MP_CHECK(run(TMP "/d1", TMP "/rt.p5z", &h, 1, &r, &e) == 0);
    MP_CHECK(r.status == MP_INST_STOPPED);
    MP_CHECK_STR(r.code, "stopped");
    MP_CHECK(mp_path_kind(FINAL(TMP "/d1")) == 0);
    MP_CHECK(mp_path_kind(TMP "/d1/maxpack/state/TEST00001.json") == 1);
    MP_CHECK(mp_path_kind(TMP "/d1/maxpack/tmp/TEST00001") == 2);
    hook h2 = {0, 0, 0, 0, NULL};
    MP_CHECK(run(TMP "/d1", TMP "/rt.p5z", &h2, 0, &r, &e) == 0);
    if (e.msg[0]) fprintf(stderr, "  %s\n", e.msg);
    MP_CHECK(r.status == MP_INST_DONE);
    MP_CHECK(mpt_dirs_equal(TMP "/unpacked_rt", FINAL(TMP "/d1"), "*.pak"));
    MP_CHECK(h2.last < total);
    MP_CHECK(h2.last > 0);
    MP_CHECK(mp_path_kind(TMP "/d1/maxpack/state/TEST00001.json") == 0);
    mp_inst_result_free(&r);
}

// Primera ruta del registro de hechos cuyo archivo, dentro de dir, no está vacío.
static int first_done(const char *done_path, const char *dir, char *out, size_t cap) {
    size_t n = 0;
    uint8_t *b = mpt_slurp(done_path, &n);
    if (!b) return -1;
    int found = 0;
    for (size_t start = 0, k = 0; k < n && !found; k++) {
        if (b[k] != '\n') continue;
        if (k > start && k - start < cap) {
            memcpy(out, b + start, k - start);
            out[k - start] = 0;
            char path[2048];
            uint64_t size = 0;
            snprintf(path, sizeof path, "%s/%s", dir, out);
            found = mp_stat(path, &size, NULL) == 0 && size > 0;
        }
        start = k + 1;
    }
    free(b);
    return found ? 0 : -1;
}

MP_TEST(resume_rehashes_done) {
    setup();
    uint64_t total = install_size(TMP "/rt.p5z");
    hook h = {total * 4 / 10, 0, 0, 0, NULL};
    mp_inst_result r;
    mp_err e;
    MP_CHECK(run(TMP "/d2", TMP "/rt.p5z", &h, 1, &r, &e) == 0 && r.status == MP_INST_STOPPED);
    char rel[1024], path[2048];
    MP_CHECK(first_done(TMP "/d2/maxpack/state/TEST00001.done", TMP "/d2/maxpack/tmp/TEST00001", rel, sizeof rel) == 0);
    snprintf(path, sizeof path, "%s/%s", TMP "/d2/maxpack/tmp/TEST00001", rel);
    size_t n = 0;
    uint8_t *b = mpt_slurp(path, &n);
    MP_CHECK(b && n > 0);
    if (!b || n == 0) return;
    b[n - 1] ^= 0xFF;   // mismo tamaño, contenido distinto
    mpt_put(path, b, n);
    free(b);
    hook h2 = {0, 0, 0, 0, NULL};
    MP_CHECK(run(TMP "/d2", TMP "/rt.p5z", &h2, 0, &r, &e) == 0 && r.status == MP_INST_DONE);
    MP_CHECK(mpt_dirs_equal(TMP "/unpacked_rt", FINAL(TMP "/d2"), "*.pak"));
    mp_inst_result_free(&r);
}

MP_TEST(write_error_keeps_state) {
    setup();
    uint64_t total = install_size(TMP "/rt.p5z");
    hook h = {0, total / 2, 0, 0, NULL};
    mp_inst_result r;
    mp_err e;
    MP_CHECK(run(TMP "/d3", TMP "/rt.p5z", &h, 1, &r, &e) == -1);
    MP_CHECK(r.status == MP_INST_FAILED);
    MP_CHECK_STR(r.code, "io_error");
    MP_CHECK_CONTAINS(e.msg, "disco lleno");
    MP_CHECK(mp_path_kind(TMP "/d3/maxpack/state/TEST00001.json") == 1);
    MP_CHECK(mp_path_kind(FINAL(TMP "/d3")) == 0);
    mp_inst_result_free(&r);
}

MP_TEST(source_vanishes_then_resume) {
    setup();
    mpt_copy_file(TMP "/rt.p5z", TMP "/copia.p5z");
    uint64_t total = install_size(TMP "/copia.p5z");
    hook h = {total * 4 / 10, 0, 0, 0, NULL};
    mp_inst_result r;
    mp_err e;
    MP_CHECK(run(TMP "/d4", TMP "/copia.p5z", &h, 1, &r, &e) == 0 && r.status == MP_INST_STOPPED);
    size_t n = 0;
    uint8_t *b = mpt_slurp(TMP "/copia.p5z", &n);
    if (!b) return;
    mpt_put(TMP "/copia.p5z", b, n / 2);
    hook h2 = {0, 0, 0, 0, NULL};
    MP_CHECK(run(TMP "/d4", TMP "/copia.p5z", &h2, 0, &r, &e) == -1);
    MP_CHECK(r.status == MP_INST_FAILED);
    MP_CHECK(mp_path_kind(FINAL(TMP "/d4")) == 0);
    MP_CHECK(mp_path_kind(TMP "/d4/maxpack/state/TEST00001.json") == 1);
    mpt_put(TMP "/copia.p5z", b, n);
    free(b);
    hook h3 = {0, 0, 0, 0, NULL};
    MP_CHECK(run(TMP "/d4", TMP "/copia.p5z", &h3, 0, &r, &e) == 0 && r.status == MP_INST_DONE);
    MP_CHECK(h3.last < total);
    MP_CHECK(mpt_dirs_equal(TMP "/unpacked_rt", FINAL(TMP "/d4"), "*.pak"));
    mp_inst_result_free(&r);
}

MP_TEST(other_archive_restarts) {
    setup();
    uint64_t total = install_size(TMP "/rt.p5z");
    hook h = {total * 4 / 10, 0, 0, 0, NULL};
    mp_inst_result r;
    mp_err e;
    MP_CHECK(run(TMP "/d5", TMP "/rt.p5z", &h, 1, &r, &e) == 0 && r.status == MP_INST_STOPPED);
    uint64_t total2 = install_size(TMP "/nort.p5z");
    hook h2 = {0, 0, 0, 0, NULL};
    MP_CHECK(run(TMP "/d5", TMP "/nort.p5z", &h2, 0, &r, &e) == 0 && r.status == MP_INST_DONE);
    MP_CHECK(h2.last >= total2 * 9 / 10);
    MP_CHECK(mpt_dirs_equal(TMP "/folder_rt", FINAL(TMP "/d5"), "*.pak|ampr_emu.index"));
    mp_inst_result_free(&r);
}

MP_TEST(state_corrupt_restarts) {
    setup();
    uint64_t total = install_size(TMP "/rt.p5z");
    hook h = {total * 4 / 10, 0, 0, 0, NULL};
    mp_inst_result r;
    mp_err e;
    MP_CHECK(run(TMP "/d6", TMP "/rt.p5z", &h, 1, &r, &e) == 0 && r.status == MP_INST_STOPPED);
    mpt_put_text(TMP "/d6/maxpack/state/TEST00001.json", "{\"archive_id\":");
    hook h2 = {0, 0, 0, 0, NULL};
    MP_CHECK(run(TMP "/d6", TMP "/rt.p5z", &h2, 0, &r, &e) == 0 && r.status == MP_INST_DONE);
    MP_CHECK(h2.last >= total * 9 / 10);
    MP_CHECK(mpt_dirs_equal(TMP "/unpacked_rt", FINAL(TMP "/d6"), "*.pak"));
    mp_inst_result_free(&r);
}

MP_TEST(resume_with_little_space) {
    setup();
    mp_inst_env probe_env;
    mp_inst_env_default(&probe_env);
    uint64_t total = install_size(TMP "/rt.p5z");
    // Cabe justo (con 1 MiB de sobra) con la temporal vacía; lo copiado ocupa ya parte del disco.
    hook h = {total * 4 / 10, 0, 0, total + probe_env.space_margin + (1u << 20), TMP "/d7/maxpack/tmp/TEST00001"};
    mp_inst_result r;
    mp_err e;
    MP_CHECK(run(TMP "/d7", TMP "/rt.p5z", &h, 1, &r, &e) == 0 && r.status == MP_INST_STOPPED);
    hook h2 = {0, 0, 0, h.disk, h.tmpdir};
    MP_CHECK(run(TMP "/d7", TMP "/rt.p5z", &h2, 0, &r, &e) == 0);
    MP_CHECK_STR(r.code, "done");
    MP_CHECK(mpt_dirs_equal(TMP "/unpacked_rt", FINAL(TMP "/d7"), "*.pak"));
    mp_inst_result_free(&r);
}

MP_TEST(pending_and_discard) {
    setup();
    uint64_t total = install_size(TMP "/rt.p5z");
    hook h = {total * 4 / 10, 0, 0, 0, NULL};
    mp_inst_result r;
    mp_err e;
    MP_CHECK(run(TMP "/d8", TMP "/rt.p5z", &h, 1, &r, &e) == 0 && r.status == MP_INST_STOPPED);
    mp_inst_env env = env_for(TMP "/d8", NULL, 0);
    mp_inst_pending_item *v = NULL;
    size_t n = 0;
    MP_CHECK(mp_inst_pending(&env, &v, &n, &e) == 0);
    MP_CHECK_EQ_U64(n, 1);
    if (n == 1) {
        MP_CHECK_STR(v[0].title_id, "TEST00001");
        MP_CHECK_STR(v[0].source, TMP "/rt.p5z");
        MP_CHECK_STR(v[0].dest, TMP "/d8");
        MP_CHECK(v[0].bytes > 0);
    }
    free(v);
    MP_CHECK(mp_inst_discard(&env, "TEST00001", &e) == 0);
    MP_CHECK(mp_path_kind(TMP "/d8/maxpack/tmp/TEST00001") == 0);
    MP_CHECK(mp_path_kind(TMP "/d8/maxpack/state/TEST00001.json") == 0);
    MP_CHECK(mp_path_kind(TMP "/d8/maxpack/state/TEST00001.done") == 0);
    MP_CHECK(mp_inst_pending(&env, &v, &n, &e) == 0 && n == 0);
    free(v);
    MP_CHECK(mp_inst_discard(&env, "../x", &e) == -1);
}

MP_TEST(other_dest_cleans_old_temp) {
    setup();
    mp_err e0 = {{0}};
    mp_make_dirs(TMP "/usb1", &e0);   // la unidad existe en esta prueba
    uint64_t total = install_size(TMP "/rt.p5z");
    hook h = {total * 4 / 10, 0, 0, 0, NULL};
    mp_inst_env env = env_for(TMP "/d9", &h, 1);
    mp_inst_req q;
    memset(&q, 0, sizeof q);
    q.p5z_path = TMP "/rt.p5z";
    q.dest_root = TMP "/usb1";
    mp_inst_result r;
    mp_err e = {{0}};
    MP_CHECK(mp_inst_install(&env, &q, &r, &e) == 0 && r.status == MP_INST_STOPPED);
    MP_CHECK(mp_path_kind(TMP "/usb1/maxpack-tmp/TEST00001") == 2);
    hook h2 = {0, 0, 0, 0, NULL};
    MP_CHECK(run(TMP "/d9", TMP "/rt.p5z", &h2, 0, &r, &e) == 0 && r.status == MP_INST_DONE);
    MP_CHECK(mp_path_kind(TMP "/usb1/maxpack-tmp/TEST00001") == 0);
    mp_inst_result_free(&r);
    mpt_clear(TMP "/usb1");
}

MP_TEST(recover_old_after_cut) {
    setup();
    mpt_clear(TMP "/d10");
    // Un corte entre apartar el juego viejo y poner el nuevo: el viejo está en <tmp>.old.
    mpt_put_text(TMP "/d10/maxpack/tmp/TEST00001.old/eboot.bin", "juego viejo");
    mpt_put_text(TMP "/d10/maxpack/tmp/TEST00001.old.dest", TMP "/d10/homebrew/TEST00001");
    mp_inst_env env = env_for(TMP "/d10", NULL, 0);
    mp_inst_recover(&env);
    MP_CHECK(mp_path_kind(TMP "/d10/homebrew/TEST00001/eboot.bin") == 1);
    MP_CHECK(mp_path_kind(TMP "/d10/maxpack/tmp/TEST00001.old") == 0);
    MP_CHECK(mp_path_kind(TMP "/d10/maxpack/tmp/TEST00001.old.dest") == 0);
    // Si el destino ya está (el nuevo se puso bien), lo viejo se borra.
    mpt_put_text(TMP "/d10/maxpack/tmp/TEST00002.old/eboot.bin", "otro viejo");
    mpt_put_text(TMP "/d10/maxpack/tmp/TEST00002.old.dest", TMP "/d10/homebrew/TEST00002");
    mpt_put_text(TMP "/d10/homebrew/TEST00002/eboot.bin", "nuevo");
    mp_inst_recover(&env);
    MP_CHECK(mp_path_kind(TMP "/d10/maxpack/tmp/TEST00002.old") == 0);
    size_t n = 0;
    uint8_t *b = mpt_slurp(TMP "/d10/homebrew/TEST00002/eboot.bin", &n);
    MP_CHECK(b && n == 5 && !memcmp(b, "nuevo", 5));
    free(b);
}

int main(void) {
    MP_RUN(stop_then_resume);
    MP_RUN(resume_rehashes_done);
    MP_RUN(write_error_keeps_state);
    MP_RUN(source_vanishes_then_resume);
    MP_RUN(other_archive_restarts);
    MP_RUN(state_corrupt_restarts);
    MP_RUN(resume_with_little_space);
    MP_RUN(pending_and_discard);
    MP_RUN(other_dest_cleans_old_temp);
    MP_RUN(recover_old_after_cut);
    MP_TEST_MAIN_END();
}
