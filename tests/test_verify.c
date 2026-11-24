// SPDX-License-Identifier: GPL-3.0-or-later
#include <stdio.h>
#include <stdlib.h>

#include "mp_p5z.h"
#include "mp_pack.h"
#include "mp_platform.h"
#include "mp_test.h"

#define TMP MP_TEST_TMP "/verify"
#define BASIC MP_FIXTURES_DIR "/basic.exfat"

static uint8_t *slurp(const char *path, size_t *len) {
    mp_file *f = NULL;
    uint64_t size = 0;
    size_t got = 0;
    *len = 0;
    if (mp_file_open_read(path, &f, NULL) != 0) return NULL;
    mp_file_size(f, &size, NULL);
    uint8_t *b = malloc((size_t)size + 1);
    mp_file_pread(f, b, (size_t)size, 0, &got, NULL);
    mp_file_close(f);
    *len = got;
    return b;
}

// Packs basic to TMP/<name> (folder or .p5z) if it is not done yet.
static int ensure_pack(const char *path, int p5z) {
    if (mp_path_kind(path) != 0) return 0;
    mp_err e = {{0}};
    mp_make_dirs(TMP, &e);
    mp_pack_opts o;
    mp_pack_opts_default(&o);
    o.threads = 4;
    o.progress = 0;
    mp_pack_out out;
    if ((p5z ? mp_pack_out_p5z(path, 4, &out, &e) : mp_pack_out_dir(path, &out, &e)) != 0) return -1;
    mp_pack_result r;
    int rc = mp_pack_run(BASIC, &out, &o, &r, &e);
    out.destroy(out.ctx);
    if (rc) MP_FAIL_("pack: %s", e.msg);
    return rc;
}

MP_TEST(verify_ok) {
    if (ensure_pack(TMP "/ok.p5z", 1) || ensure_pack(TMP "/ok", 0)) return;
    mp_verify_result r;
    mp_err e = {{0}};
    MP_CHECK(mp_verify_p5z(TMP "/ok.p5z", 4, 0, &r, &e) == 0);
    MP_CHECK_EQ_U64(r.files_bad, 0);
    MP_CHECK(r.files_ok >= 213);
    MP_CHECK(mp_verify_folder(TMP "/ok", NULL, 4, 0, &r, &e) == 0);
    MP_CHECK_EQ_U64(r.files_bad, 0);
    MP_CHECK(mp_verify_folder(TMP "/ok", BASIC, 4, 0, &r, &e) == 0);
    MP_CHECK_EQ_U64(r.files_bad, 0);
    MP_CHECK(r.files_ok >= 213);
}

MP_TEST(verify_detects_corruption) {
    if (ensure_pack(TMP "/ok.p5z", 1)) return;
    mp_err e = {{0}};
    // A byte changed inside the zstd frame of zeros.bin's first block, in the .p5z.
    mp_p5z *p = NULL;
    MP_CHECK(mp_p5z_open(TMP "/ok.p5z", &p, &e) == 0);
    mp_zpk *z = NULL;
    MP_CHECK(p && mp_p5z_zpk(p, &z, &e) == 0);
    uint64_t at = 0;
    for (size_t i = 0; z && i < mp_zpk_count(z); i++)
        if (!strcmp(mp_zpk_path(z, i), "zeros.bin")) {
            uint64_t off = 0;
            uint32_t vol = 0, stored = 0;
            mp_zpk_chunk(z, i, 0, &vol, &off, &stored, NULL);
            // Region of volume 000 inside the .p5z: the one from its record in the index.
            const mp_pack_manifest *m = mp_p5z_manifest(p);
            (void)m;
            at = mp_p5z_volume_offset(p, vol) + off + stored / 2;
        }
    mp_zpk_close(z);
    mp_p5z_close(p);
    MP_CHECK(at > 64);
    size_t n = 0;
    uint8_t *d = slurp(TMP "/ok.p5z", &n);
    MP_CHECK(d && at < n);
    if (!d || at >= n) return;
    d[at] ^= 0x33;
    remove(TMP "/malo.p5z");
    MP_CHECK(mp_write_file(TMP "/malo.p5z", d, n, &e) == 0);
    free(d);
    mp_verify_result r;
    MP_CHECK(mp_verify_p5z(TMP "/malo.p5z", 4, 0, &r, &e) == 0);
    MP_CHECK(r.files_bad >= 1);
    // A loose file changed in a packed folder: --against finds it.
    if (ensure_pack(TMP "/mala", 0)) return;
    MP_CHECK(mp_write_file(TMP "/mala/eboot.bin", "cambiado", 8, &e) == 0);
    MP_CHECK(mp_verify_folder(TMP "/mala", BASIC, 4, 0, &r, &e) == 0);
    MP_CHECK_EQ_U64(r.files_bad, 1);
}

int main(void) {
    MP_RUN(verify_ok);
    MP_RUN(verify_detects_corruption);
    MP_TEST_MAIN_END();
}
