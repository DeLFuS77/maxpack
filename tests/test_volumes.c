// SPDX-License-Identifier: GPL-3.0-or-later
// Packs de más de 32 volúmenes (un juego de ~180 GB da más de 40): empaquetar como carpeta y como
// .p5z, verify, unpack, play e instalar. Con volúmenes de 256 KiB, unos 14 MiB sin comprimir dan
// medio centenar.
#include "mp_fixture.h"
#include "mp_inst.h"
#include "mp_play.h"

#define TMP MP_TEST_TMP "/volumes"
#define SRC TMP "/src"
#define DIR TMP "/folder"
#define P5Z TMP "/g.p5z"
#define LIMIT 262208u   // el mínimo: un bloque de 256 KiB sin comprimir por volumen
#define WANT_MIN 40u

static uint8_t rt_new[4096];
static int ready = 0;

static void make_source(void) {
    mpt_put_text(SRC "/sce_sys/param.json", "{\"titleId\":\"TEST00007\",\"contentVersion\":\"01.000.000\"}");
    mpt_put_text(SRC "/eboot.bin", "eboot");
    mpt_put_text(SRC "/datos/pequeno.txt", "texto que se comprime muy bien, muy bien, muy bien, muy bien");
    // 14 MiB de ruido (xorshift): no se comprime, así que cada bloque llena un volumen.
    uint8_t *b = malloc(14u << 20);
    if (!b) return;
    uint64_t s = 0x9E3779B97F4A7C15ull;
    for (size_t i = 0; i < (14u << 20); i++) {
        s ^= s << 13;
        s ^= s >> 7;
        s ^= s << 17;
        b[i] = (uint8_t)(s >> 24);
    }
    mpt_put(SRC "/datos/a.bin", b, 6u << 20);
    mpt_put(SRC "/datos/b.bin", b + (6u << 20), 5u << 20);
    mpt_put(SRC "/datos/sub/c.bin", b + (11u << 20), 3u << 20);
    free(b);
}

static uint32_t count_paks(const char *dir) {
    uint32_t n = 0;
    for (;; n++) {
        char p[2048];
        snprintf(p, sizeof p, "%s/ampr_zstd-%03u.pak", dir, n);
        if (mp_path_kind(p) != 1) break;
    }
    return n;
}

static void setup(void) {
    if (ready) return;
    ready = 1;
    mpt_clear(TMP);
    make_source();
    for (size_t i = 0; i < sizeof rt_new; i++) rt_new[i] = (uint8_t)(i * 13 + 1);
    memcpy(rt_new + 64, "AMPRZPK1", 8);
    mpt_put(TMP "/R.sprx", rt_new, sizeof rt_new);
    mp_err e = {{0}};
    mp_set_mtime(TMP "/R.sprx", 1700000000, &e);
    mpt_pack_vol(SRC, DIR, 0, TMP "/R.sprx", LIMIT);
    mpt_pack_vol(SRC, P5Z, 1, TMP "/R.sprx", LIMIT);
}

MP_TEST(volumes_folder_has_many) {
    setup();
    uint32_t n = count_paks(DIR);
    if (n < WANT_MIN) MP_FAIL_("solo %u volúmenes", n);
    // ni uno más: la carpeta no trae huecos ni sobrantes
    mpt_list l = mpt_list_dir(DIR);
    size_t paks = 0;
    for (size_t i = 0; i < l.n; i++) {
        size_t k = strlen(l.v[i]);
        if (k > 4 && !strcmp(l.v[i] + k - 4, ".pak")) {
            paks++;
            if (l.size[i] > LIMIT) MP_FAIL_("%s mide %llu", l.v[i], (unsigned long long)l.size[i]);
        }
    }
    MP_CHECK_EQ_U64(paks, n);
    mpt_list_free(&l);
}

MP_TEST(volumes_verify_folder_and_p5z) {
    setup();
    mp_err e = {{0}};
    mp_verify_result r;
    MP_CHECK(mp_verify_folder(DIR, SRC, 4, 0, &r, &e) == 0);
    if (e.msg[0]) MP_FAIL_("verify folder: %s", e.msg);
    MP_CHECK_EQ_U64(r.files_bad, 0);
    MP_CHECK(r.files_ok >= 5);
    memset(&e, 0, sizeof e);
    MP_CHECK(mp_verify_p5z(P5Z, 4, 0, &r, &e) == 0);
    if (e.msg[0]) MP_FAIL_("verify p5z: %s", e.msg);
    MP_CHECK_EQ_U64(r.files_bad, 0);
    MP_CHECK(r.files_ok >= 5 + WANT_MIN);
}

MP_TEST(volumes_unpack_folder_equals_pack_dir) {
    setup();
    mp_err e = {{0}};
    mp_p5z *p = NULL;
    MP_CHECK(mp_p5z_open(P5Z, &p, &e) == 0);
    if (!p) return;
    mpt_clear(TMP "/unf");
    MP_CHECK(mp_unpack_folder(p, TMP "/unf", 4, 0, &e) == 0);
    if (e.msg[0]) MP_FAIL_("unpack: %s", e.msg);
    mp_p5z_close(p);
    MP_CHECK(count_paks(TMP "/unf") >= WANT_MIN);
    MP_CHECK(mpt_dirs_equal(DIR, TMP "/unf", "*.pak|ampr_emu.index"));
}

MP_TEST(volumes_unpack_original_equals_source) {
    setup();
    mp_err e = {{0}};
    mp_p5z *p = NULL;
    MP_CHECK(mp_p5z_open(P5Z, &p, &e) == 0);
    if (!p) return;
    mpt_clear(TMP "/uno");
    MP_CHECK(mp_unpack_original(p, TMP "/uno", 4, 0, &e) == 0);
    if (e.msg[0]) MP_FAIL_("unpack original: %s", e.msg);
    mp_p5z_close(p);
    MP_CHECK(mpt_dirs_equal(SRC, TMP "/uno", "*"));
}

MP_TEST(volumes_play_json_all_ids) {
    setup();
    char err[256] = {0};
    mp_play *p = mp_play_open(P5Z, err, sizeof err);
    if (!p) {
        MP_FAIL_("play_open: %s", err);
        return;
    }
    char *j = mp_play_json(p);
    MP_CHECK(j != NULL);
    mp_err e = {{0}};
    mp_p5z *z = NULL;
    MP_CHECK(mp_p5z_open(P5Z, &z, &e) == 0);
    uint32_t n = count_paks(DIR);
    size_t hl = 0;
    uint8_t *h = mpt_slurp(P5Z, &hl);
    if (j && z && h) {
        size_t cap = 64 + (size_t)n * 96;
        char *want = malloc(cap);
        size_t w = (size_t)snprintf(want, cap, "\"volumes\":[");
        for (uint32_t k = 0; k < n; k++) {
            char pak[2048];
            snprintf(pak, sizeof pak, DIR "/ampr_zstd-%03u.pak", k);
            uint64_t len = 0;
            MP_CHECK(mp_stat(pak, &len, NULL) == 0);
            uint64_t off = mp_p5z_volume_offset(z, k);
            w += (size_t)snprintf(want + w, cap - w, "%s{\"id\":%u,\"offset\":%llu,\"length\":%llu}",
                                  k ? "," : "", k, (unsigned long long)off, (unsigned long long)len);
            size_t pl = 0;
            uint8_t *pb = mpt_slurp(pak, &pl);
            // la región del .p5z es el .pak, salvo la cabecera: el archive_id y los CRC coinciden
            if (!pb || pl != len || off + len > hl || memcmp(h + off, pb, pl))
                MP_FAIL_("volumen %u: la región no es su .pak", k);
            free(pb);
        }
        snprintf(want + w, cap - w, "]}");
        MP_CHECK_CONTAINS(j, want);
        free(want);
        // no hay un id n
        char extra[32];
        snprintf(extra, sizeof extra, "{\"id\":%u,", n);
        MP_CHECK(strstr(j, extra) == NULL);
    }
    free(h);
    mp_play_free_str(j);
    mp_p5z_close(z);
    mp_play_close(p);
}

static mp_inst_env env_for(const char *internal) {
    static const char *no_drives[] = {TMP "/usb1"};
    mp_inst_env env;
    mp_inst_env_default(&env);
    env.internal = internal;
    env.drive_candidates = no_drives;
    env.ndrive_candidates = 1;
    env.runtime.data = rt_new;
    env.runtime.len = sizeof rt_new;
    env.runtime.mtime = 1700000000;
    mp_err e = {{0}};
    mpt_clear(internal);
    mp_make_dirs(internal, &e);
    return env;
}

MP_TEST(volumes_install_p5z_and_folder) {
    setup();
    const char *src[] = {P5Z, DIR};
    const char *dst[] = {TMP "/i_p5z", TMP "/i_dir"};
    for (int k = 0; k < 2; k++) {
        mp_inst_env env = env_for(dst[k]);
        mp_inst_req q;
        memset(&q, 0, sizeof q);
        q.p5z_path = src[k];
        q.dest_root = dst[k];
        mp_inst_result r;
        mp_err e = {{0}};
        MP_CHECK(mp_inst_install(&env, &q, &r, &e) == 0);
        if (e.msg[0]) MP_FAIL_("install %d: %s", k, e.msg);
        MP_CHECK(r.status == MP_INST_DONE);
        char fin[1200];
        snprintf(fin, sizeof fin, "%s/homebrew/TEST00007", dst[k]);
        MP_CHECK_STR(r.final_dir, fin);
        MP_CHECK(count_paks(fin) >= WANT_MIN);
        MP_CHECK(mpt_dirs_equal(DIR, fin, "*.pak|ampr_emu.index"));
        mp_inst_verify_result vr;
        memset(&e, 0, sizeof e);
        MP_CHECK(mp_inst_verify(&env, "TEST00007", &vr, &e) == 0);
        if (e.msg[0]) MP_FAIL_("inst_verify %d: %s", k, e.msg);
        MP_CHECK_EQ_U64(vr.files_bad, 0);
        if (k == 0) MP_CHECK(vr.p5z_checked == 1);
        if (vr.files_ok < (k == 0 ? 1u : WANT_MIN)) MP_FAIL_("k=%d files_ok=%llu", k, (unsigned long long)vr.files_ok);
        mp_inst_result_free(&r);
    }
}

int main(void) {
    MP_RUN(volumes_folder_has_many);
    MP_RUN(volumes_verify_folder_and_p5z);
    MP_RUN(volumes_unpack_folder_equals_pack_dir);
    MP_RUN(volumes_unpack_original_equals_source);
    MP_RUN(volumes_play_json_all_ids);
    MP_RUN(volumes_install_p5z_and_folder);
    MP_TEST_MAIN_END();
}
