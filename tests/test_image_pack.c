// SPDX-License-Identifier: GPL-3.0-or-later
// Packing an image or a package gives what packing the same game as a folder gives: the same .p5z
// byte for byte, the same folder for the console, the same analysis but for the source's kind, and
// unpack --original of the .p5z gives back the game's files. The folder is the basic game as unpack
// --original leaves it (its files with their dates); the images are make_images.py's, the packages
// make_fpkg.py's (stored, Kraken, and Kraken with what the reference encoder leaves out). A package a
// builder made of a game folder packs to that folder's files with their SHA-256s, plus or changed
// exactly what the builder added (game.differ.tsv), every date the builder's.
#include <stdio.h>
#include <stdlib.h>

#include "mp_analyze.h"
#include "mp_fixture.h"
#include "mp_hash.h"
#include "mp_source.h"
#include "mp_p5z.h"
#include "mp_test.h"

#define FX MP_FIXTURES_DIR
#define TMP MP_TEST_TMP "/image_pack"
#define GAME TMP "/game"

static const char *images[] = {"basic.ffpkg",        "profile.ffpkg",          "basic.ffpfs",
                               "basic_c.ffpfs",      "basic.exfat.ffpfsc",     "basic.ffpkg.ffpfsc",
                               "basic.ffpfs.ffpfsc", "basic_raw.exfat.ffpfsc", "nest3.ffpfsc",
                               "basic.pkg",          "basic_k.pkg",            "basic_x.pkg"};
#define NIMAGES (sizeof images / sizeof images[0])

static int unpack_original(const char *p5z, const char *dir) {
    mp_err e = {{0}};
    mp_p5z *p = NULL;
    mpt_clear(dir);
    if (mp_p5z_open(p5z, &p, &e) != 0) {
        MP_FAIL_("%s: %s", p5z, e.msg);
        return -1;
    }
    int rc = mp_unpack_original(p, dir, 4, 0, &e);
    if (rc) MP_FAIL_("unpack --original: %s", e.msg);
    mp_p5z_close(p);
    return rc;
}

static int same_file(const char *a, const char *b) {
    size_t x = 0, y = 0;
    uint8_t *ba = mpt_slurp(a, &x), *bb = mpt_slurp(b, &y);
    int same = ba && bb && x == y && x > 0 && !memcmp(ba, bb, x);
    free(ba);
    free(bb);
    return same;
}

// The game as a folder, from the exFAT image's .p5z.
MP_TEST(make_game_folder) {
    if (mpt_pack(FX "/basic.exfat", TMP "/exfat.p5z", 1, NULL) == 0) unpack_original(TMP "/exfat.p5z", GAME);
}

MP_TEST(images_pack_like_folder_p5z) {
    if (mpt_pack(GAME, TMP "/game.p5z", 1, NULL) != 0) return;
    MP_CHECK(same_file(TMP "/game.p5z", TMP "/exfat.p5z"));
    for (size_t i = 0; i < NIMAGES; i++) {
        char src[512], dst[512];
        snprintf(src, sizeof src, FX "/%s", images[i]);
        snprintf(dst, sizeof dst, TMP "/%s.p5z", images[i]);
        if (mpt_pack(src, dst, 1, NULL) != 0) continue;
        if (!same_file(dst, TMP "/game.p5z")) MP_FAIL_("the .p5z of %s differs from the folder's", images[i]);
    }
}

MP_TEST(images_pack_like_folder_dir) {
    if (mpt_pack(GAME, TMP "/game_out", 0, NULL) != 0) return;
    for (size_t i = 0; i < NIMAGES; i++) {
        char src[512], dst[512];
        snprintf(src, sizeof src, FX "/%s", images[i]);
        snprintf(dst, sizeof dst, TMP "/out_%zu", i);
        if (mpt_pack(src, dst, 0, NULL) != 0) continue;
        if (!mpt_dirs_equal(TMP "/game_out", dst, "*.pak")) MP_FAIL_("the folder of %s differs", images[i]);
        mpt_clear(dst);
    }
}

// unpack --original of the .p5z of an image: the game's files, with their dates.
MP_TEST(images_unpack_original) {
    const char *which[] = {"basic.exfat.ffpfsc", "basic.ffpkg", "basic_c.ffpfs", "basic_k.pkg"};
    for (size_t i = 0; i < 4; i++) {
        char p5z[512];
        snprintf(p5z, sizeof p5z, TMP "/%s.p5z", which[i]);
        if (unpack_original(p5z, TMP "/orig") == 0 && !mpt_dirs_equal(GAME, TMP "/orig", NULL))
            MP_FAIL_("unpack --original of %s differs", which[i]);
    }
    mpt_clear(TMP "/orig");
}

// analyze: the same numbers; only the kind of the source differs.
MP_TEST(images_analyze_like_folder) {
    mp_analyze_opts o;
    mp_analyze_opts_default(&o);
    o.threads = 4;
    o.full = 1;
    mp_report a, b;
    mp_err e = {{0}};
    memset(&a, 0, sizeof a);
    if (mp_analyze(GAME, &o, &a, &e) != 0) {
        MP_FAIL_("analyze: %s", e.msg);
        return;
    }
    MP_CHECK_STR(a.kind_name, "folder");
    for (size_t i = 0; i < NIMAGES; i++) {
        char src[512];
        snprintf(src, sizeof src, FX "/%s", images[i]);
        memset(&b, 0, sizeof b);
        if (mp_analyze(src, &o, &b, &e) != 0) {
            MP_FAIL_("analyze %s: %s", images[i], e.msg);
            continue;
        }
        int same = a.files == b.files && a.dirs == b.dirs && a.total_bytes == b.total_bytes &&
                   a.loose_files == b.loose_files && a.loose_bytes == b.loose_bytes &&
                   a.packable_files == b.packable_files && a.packable_bytes == b.packable_bytes &&
                   a.sample_bytes == b.sample_bytes && a.v1_index_bytes == b.v1_index_bytes &&
                   a.loose_compressed == b.loose_compressed && !memcmp(a.est_packed, b.est_packed, sizeof a.est_packed);
        if (!same) MP_FAIL_("the analysis of %s differs from the folder's", images[i]);
        if (!strcmp(b.kind_name, "folder")) MP_FAIL_("%s says it is a folder", images[i]);
        mp_report_free(&b);
    }
    mp_report_free(&a);
}

// The images of tests/data (MkPFS): each has exactly the game's files, and packs to the .p5z of the
// game as a folder whose files carry the dates the image gives them.
MP_TEST(tool_images) {
    static const struct {
        const char *file, *kind, *inner;
    } t[] = {{"mkpfs_plain.ffpfs", "pfs", ""},
             {"mkpfs_raw.ffpfs", "pfs", ""},
             {"mkpfs_exfat.ffpfsc", "pfsc", "exfat"},
             {"mkpfs_ufs2.ffpfsc", "pfsc", "ufs2"},
             {"mkpfs_pfs.ffpfsc", "pfsc", "pfs"}};
    mp_err e = {{0}};
    mp_source *folder = NULL;
    if (mp_source_open(FX "/tool_game", &folder, &e) != 0) {
        MP_FAIL_("tool_game: %s", e.msg);
        return;
    }
    for (size_t k = 0; k < sizeof t / sizeof t[0]; k++) {
        char src[512], dir[512], p5z_a[512], p5z_b[512];
        snprintf(src, sizeof src, FX "/%s", t[k].file);
        mp_source *s = NULL;
        if (mp_source_open(src, &s, &e) != 0) {
            MP_FAIL_("%s: %s", t[k].file, e.msg);
            continue;
        }
        MP_CHECK_STR(mp_source_kind_name(s), t[k].kind);
        MP_CHECK_STR(mp_source_inner_name(s), t[k].inner);
        MP_CHECK_EQ_U64(mp_source_count(s), mp_source_count(folder));
        snprintf(dir, sizeof dir, TMP "/tool_%zu", k);
        mpt_clear(dir);
        size_t bad = 0;
        for (size_t i = 0; i < mp_source_count(s) && i < mp_source_count(folder); i++) {
            const mp_entry *en = mp_source_entry(s, i), *fe = mp_source_entry(folder, i);
            uint8_t *a = malloc(en->size + 1), *b = malloc(fe->size + 1);
            int same = a && b && !strcmp(en->path, fe->path) && en->size == fe->size &&
                       mp_source_read(s, i, 0, a, (size_t)en->size, &e) == 0 &&
                       mp_source_read(folder, i, 0, b, (size_t)fe->size, &e) == 0 && !memcmp(a, b, (size_t)en->size);
            if (!same && bad++ < 3) MP_FAIL_("%s: %s differs (%s)", t[k].file, en->path, e.msg);
            char out[1024];
            snprintf(out, sizeof out, "%s/%s", dir, en->path);
            if (same) {
                mpt_put(out, a, (size_t)en->size);
                mp_set_mtime(out, en->mtime, &e);
            }
            free(a);
            free(b);
        }
        MP_CHECK_EQ_U64(bad, 0);
        mp_source_close(s);
        snprintf(p5z_a, sizeof p5z_a, TMP "/tool_%zu_image.p5z", k);
        snprintf(p5z_b, sizeof p5z_b, TMP "/tool_%zu_folder.p5z", k);
        if (mpt_pack(src, p5z_a, 1, NULL) == 0 && mpt_pack(dir, p5z_b, 1, NULL) == 0 && !same_file(p5z_a, p5z_b))
            MP_FAIL_("the .p5z of %s differs from the folder's", t[k].file);
        mpt_clear(dir);
    }
    mp_source_close(folder);
}

// SHA-256 of a file of a source, as hex; "" if it cannot be read.
static void sha_hex(mp_source *s, size_t i, char hex[65]) {
    const mp_entry *en = mp_source_entry(s, i);
    uint8_t *b = malloc(en->size + 1), d[32];
    mp_err e = {{0}};
    hex[0] = 0;
    if (b && mp_source_read(s, i, 0, b, (size_t)en->size, &e) == 0) {
        mp_sha256 h;
        mp_sha256_init(&h);
        mp_sha256_update(&h, b, (size_t)en->size);
        mp_sha256_final(&h, d);
        mp_sha256_hex(d, hex);
    }
    free(b);
}

// game.pkg (what a builder made of game/) and game/ itself, each packed and unpacked --original: the
// folder's files with the same SHA-256, and only the builder's additions and changes besides.
MP_TEST(fpkg_game_like_folder) {
    if (mpt_pack(FX "/game", TMP "/gdir.p5z", 1, NULL) != 0 || mpt_pack(FX "/game.pkg", TMP "/gpkg.p5z", 1, NULL) != 0)
        return;
    if (unpack_original(TMP "/gdir.p5z", TMP "/gdir") != 0 || unpack_original(TMP "/gpkg.p5z", TMP "/gpkg") != 0)
        return;
    mp_err e = {{0}};
    mp_source *a = NULL, *b = NULL;
    size_t len = 0;
    uint8_t *tsv = mpt_slurp(FX "/game.differ.tsv", &len);
    if (mp_source_open(TMP "/gdir", &a, &e) != 0 || mp_source_open(TMP "/gpkg", &b, &e) != 0 || !tsv) {
        MP_FAIL_("%s", e.msg);
        mp_source_close(a);
        free(tsv);
        return;
    }
    tsv[len] = 0;
    char seen[4096] = "";
    for (size_t i = 0; i < mp_source_count(b); i++) {
        const mp_entry *en = mp_source_entry(b, i);
        MP_CHECK_EQ_U64(en->mtime, 1730000000);   // the builder's time
        long j = mp_source_find(a, en->path);
        char x[65], y[65];
        sha_hex(b, i, x);
        if (j >= 0) sha_hex(a, (size_t)j, y);
        if (j < 0 || strcmp(x, y))
            snprintf(seen + strlen(seen), sizeof seen - strlen(seen), "%s\t%s\n", j < 0 ? "added" : "changed",
                     en->path);
    }
    for (size_t j = 0; j < mp_source_count(a); j++)
        if (mp_source_find(b, mp_source_entry(a, j)->path) < 0)
            MP_FAIL_("%s is missing from the package's", mp_source_entry(a, j)->path);
    // the differences are exactly the listed ones (the list is sorted by path, as the sources are)
    char want[4096] = "";
    for (char *line = (char *)tsv; line && *line;) {
        char *nl = strchr(line, '\n');
        if (nl) *nl = 0;
        if (line[0] != '#') snprintf(want + strlen(want), sizeof want - strlen(want), "%s\n", line);
        line = nl ? nl + 1 : NULL;
    }
    MP_CHECK_STR(seen, want);
    mp_source_close(a);
    mp_source_close(b);
    free(tsv);
    mpt_clear(TMP "/gdir");
    mpt_clear(TMP "/gpkg");
}

int main(void) {
    mpt_clear(TMP);
    MP_RUN(make_game_folder);
    MP_RUN(images_pack_like_folder_p5z);
    MP_RUN(images_pack_like_folder_dir);
    MP_RUN(images_unpack_original);
    MP_RUN(images_analyze_like_folder);
    MP_RUN(tool_images);
    MP_RUN(fpkg_game_like_folder);
    MP_TEST_MAIN_END();
}
