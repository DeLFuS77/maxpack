// SPDX-License-Identifier: GPL-3.0-or-later
// A game packed with its DLC (make_fpkg.py's mini game and its DLC): the source made of a game and other
// sources.
#include <stdlib.h>

#include "mp_describe.h"
#include "mp_dlc.h"
#include "mp_fixture.h"
#include "mp_hash.h"
#include "mp_p5z_internal.h"
#include "mp_platform.h"
#include "mp_source.h"
#include "mp_test.h"

#define FX MP_FIXTURES_DIR
#define TMP MP_TEST_TMP "/dlc"

// The bytes of a source's file (malloc), or NULL.
static uint8_t *whole(mp_source *s, const char *path, size_t *len) {
    long i = mp_source_find(s, path);
    *len = 0;
    if (i < 0) return NULL;
    size_t n = (size_t)mp_source_entry(s, (size_t)i)->size;
    uint8_t *b = malloc(n + 1);
    if (!b || mp_source_read(s, (size_t)i, 0, b, n, NULL) != 0) {
        free(b);
        return NULL;
    }
    *len = n;
    return b;
}

static mp_source *open_or_fail(const char *path, int what) {
    mp_err e = {{0}};
    mp_source *s = NULL;
    if (mp_source_open_as(path, what, &s, &e) != 0) MP_FAIL_("%s: %s", path, e.msg);
    return s;
}

// The game's files, a DLC's under its prefix and one more file given as bytes: sorted, each read from
// where it is; the kind and the package ID are the game's.
MP_TEST(merge_basic) {
    mp_source *game = open_or_fail(FX "/mini.pkg", MP_OPEN_GAME), *dlc = open_or_fail(FX "/dlc_data", MP_OPEN_DLC);
    mp_source *ref = open_or_fail(FX "/dlc_data", MP_OPEN_DLC);
    if (!game || !dlc || !ref) {
        mp_source_close(game);
        mp_source_close(dlc);
        mp_source_close(ref);
        return;
    }
    size_t ngame = mp_source_count(game), ndlc = mp_source_count(dlc);
    mp_source *parts[] = {dlc};
    const char *prefixes[] = {"addcont0"};
    mp_source *m = NULL;
    mp_err e = {{0}};
    static const uint8_t ini[] = "x\n";
    MP_CHECK(mp_source_merge(game, parts, prefixes, 1, NULL, "dlc_emu.ini", ini, 2, 1700000000, &m, &e) == 0);
    if (!m) {
        MP_FAIL_("%s", e.msg);
        mp_source_close(ref);
        return;
    }
    MP_CHECK_EQ_U64(mp_source_count(m), ngame + ndlc + 1);
    for (size_t i = 1; i < mp_source_count(m); i++)
        MP_CHECK(strcmp(mp_source_entry(m, i - 1)->path, mp_source_entry(m, i)->path) < 0);
    MP_CHECK_STR(mp_source_kind_name(m), "fpkg");
    MP_CHECK_STR(mp_source_package_id(m), "UP0000-PPSA00000_00-MAXPACKTEST00000");
    MP_CHECK(mp_source_readers(m) == 16);
    MP_CHECK(mp_source_find(m, "eboot.bin") >= 0);
    // each file of the DLC, with its size, date and bytes
    for (size_t i = 0; i < mp_source_count(ref); i++) {
        const mp_entry *r = mp_source_entry(ref, i);
        char path[512];
        snprintf(path, sizeof path, "addcont0/%s", r->path);
        long at = mp_source_find(m, path);
        if (at < 0) {
            MP_FAIL_("%s is not in it", path);
            continue;
        }
        MP_CHECK_EQ_U64(mp_source_entry(m, (size_t)at)->size, r->size);
        MP_CHECK_EQ_U64(mp_source_entry(m, (size_t)at)->mtime, r->mtime);
        size_t a = 0, b = 0;
        uint8_t *x = whole(m, path, &a), *y = whole(ref, r->path, &b);
        MP_CHECK(x && y && a == b && !memcmp(x, y, a));
        free(x);
        free(y);
    }
    // the file given as bytes, and a read in the middle of it
    long at = mp_source_find(m, "dlc_emu.ini");
    MP_CHECK(at >= 0);
    if (at >= 0) {
        MP_CHECK_EQ_U64(mp_source_entry(m, (size_t)at)->size, 2);
        MP_CHECK_EQ_U64(mp_source_entry(m, (size_t)at)->mtime, 1700000000);
        char c = 0;
        MP_CHECK(mp_source_read(m, (size_t)at, 1, &c, 1, &e) == 0 && c == '\n');
    }
    // a read past the end is refused, as any source's
    char four[4];
    MP_CHECK(mp_source_read(m, (size_t)at, 1, four, 2, &e) == -1);
    mp_source_close(m);
    mp_source_close(ref);
}

// A path that would be twice stops it, and every source given is closed (no leak to see here, but it
// must not crash).
MP_TEST(merge_twice) {
    mp_source *game = open_or_fail(FX "/mini", MP_OPEN_GAME);
    if (!game) return;
    mp_source *m = NULL;
    mp_err e = {{0}};
    MP_CHECK(mp_source_merge(game, NULL, NULL, 0, NULL, "eboot.bin", (const uint8_t *)"x", 1, 0, &m, &e) == -1);
    MP_CHECK(m == NULL);
    MP_CHECK_CONTAINS(e.msg, "eboot.bin is twice");
    // two parts under the same prefix
    game = open_or_fail(FX "/mini", MP_OPEN_GAME);
    mp_source *a = open_or_fail(FX "/dlc_data", MP_OPEN_DLC), *b = open_or_fail(FX "/dlc_data", MP_OPEN_DLC);
    if (!game || !a || !b) {
        mp_source_close(game);
        mp_source_close(a);
        mp_source_close(b);
        return;
    }
    mp_source *parts[] = {a, b};
    const char *prefixes[] = {"addcont0", "addcont0"};
    MP_CHECK(mp_source_merge(game, parts, prefixes, 2, NULL, NULL, NULL, 0, 0, &m, &e) == -1);
    MP_CHECK_CONTAINS(e.msg, "is twice");
    // without anything to add, the game's files as they are
    game = open_or_fail(FX "/mini", MP_OPEN_GAME);
    if (!game) return;
    size_t n = mp_source_count(game);
    MP_CHECK(mp_source_merge(game, NULL, NULL, 0, NULL, NULL, NULL, 0, 0, &m, &e) == 0);
    MP_CHECK(m && mp_source_count(m) == n && !strcmp(mp_source_kind_name(m), "folder") && !strcmp(mp_source_package_id(m), ""));
    mp_source_close(m);
}

// ---- packing a game with its DLC -----------------------------------------------------------------

#define DATA_CID "UP0000-PPSA00000_00-MAXPACKDLCDATA01"
#define LICENSE_CID "UP0000-PPSA00000_00-MAXPACKLICENSE01"
#define INI_ONE_EACH                                                                                    \
    "[PSAC]\ncontent_id=" DATA_CID "\nmount_point=/app0/addcont0\n\n[PSAL]\ncontent_id=" LICENSE_CID     \
    "\ndownload_status=NO_EXTRA_DATA\n"

// The dlc_emu.ini MaxPack writes: a [PSAC] section with the folder of each DLC with data, a [PSAL] one
// for each license, in the order given, nothing else.
MP_TEST(ini_text) {
    mp_dlc v[2];
    memset(v, 0, sizeof v);
    snprintf(v[0].content_id, sizeof v[0].content_id, "%s", DATA_CID);
    v[0].data = 1;
    snprintf(v[0].folder, sizeof v[0].folder, "addcont0");
    snprintf(v[1].content_id, sizeof v[1].content_id, "%s", LICENSE_CID);
    size_t len = 0;
    char *ini = mp_dlc_ini(v, 2, &len);
    MP_CHECK(ini != NULL);
    if (ini) {
        MP_CHECK_EQ_U64(len, strlen(INI_ONE_EACH));
        MP_CHECK_STR(ini, INI_ONE_EACH);
    }
    free(ini);
}

typedef struct {
    int rc;
    mp_pack_result r;
    mp_err e;
} packed;

// Packs game with the DLC dlc[0..n) to out (a .p5z, or a folder).
static packed pack_dlc(const char *game, const char *const *dlc, size_t n, const char *out, int p5z) {
    packed p;
    memset(&p, 0, sizeof p);
    mpt_clear(out);
    mpt_parent_dirs(out);
    mp_pack_opts o;
    mp_pack_opts_default(&o);
    o.threads = 4;
    o.progress = 0;
    o.level = 3;
    o.dlc = dlc;
    o.ndlc = n;
    mp_pack_out po;
    if ((p5z ? mp_pack_out_p5z(out, 4, &po, &p.e) : mp_pack_out_dir(out, &po, &p.e)) != 0) {
        p.rc = -2;
        return p;
    }
    p.rc = mp_pack_run(game, &po, &o, &p.r, &p.e);
    po.destroy(po.ctx);
    return p;
}

static const mp_manifest_entry *manifest_find(const mp_p5z *p, const char *path) {
    const mp_pack_manifest *m = mp_p5z_manifest(p);
    for (size_t i = 0; i < m->n; i++)
        if (!strcmp(m->v[i].path, path)) return &m->v[i];
    return NULL;
}

static void sha_of_file(const char *path, uint8_t out[32]) {
    size_t n = 0;
    uint8_t *b = mpt_slurp(path, &n);
    mp_sha256 s;
    mp_sha256_init(&s);
    mp_sha256_update(&s, b, n);
    mp_sha256_final(&s, out);
    free(b);
}

// The mini game with a DLC with data (its package) and a license (its folder): the DLC's files in
// addcont0/, with their SHA-256 in the manifest, the ini loose, and the .p5z verifies and unpacks
// to the game, addcont0/ (the DLC folder's files) and the ini.
MP_TEST(pack_with_dlc) {
    const char *dlc[] = {FX "/dlc_data.pkg", FX "/dlc_license"};
    packed k = pack_dlc(FX "/mini", dlc, 2, TMP "/g.p5z", 1);
    MP_CHECK(k.rc == 0);
    if (k.rc != 0) {
        MP_FAIL_("%s", k.e.msg);
        return;
    }
    MP_CHECK_EQ_U64(k.r.dlc_data, 1);
    MP_CHECK_EQ_U64(k.r.dlc_license, 1);
    MP_CHECK_EQ_U64(k.r.dlc_bytes, mpt_dir_size(FX "/dlc_data"));
    MP_CHECK(k.r.dlc_error == NULL);
    mp_err e = {{0}};
    mp_p5z *p = NULL;
    MP_CHECK(mp_p5z_open(TMP "/g.p5z", &p, &e) == 0);
    if (p) {
        mpt_list l = mpt_list_dir(FX "/dlc_data");
        for (size_t i = 0; i < l.n; i++) {
            char path[512], file[1024];
            snprintf(path, sizeof path, "addcont0/%s", l.v[i]);
            snprintf(file, sizeof file, FX "/dlc_data/%s", l.v[i]);
            const mp_manifest_entry *m = manifest_find(p, path);
            uint8_t want[32];
            sha_of_file(file, want);
            if (!m || m->size != l.size[i] || memcmp(m->sha, want, 32)) MP_FAIL_("%s is not in the manifest as it is", path);
        }
        mpt_list_free(&l);
        MP_CHECK(manifest_find(p, "addcont1/sce_sys/param.json") == NULL);
        MP_CHECK(manifest_find(p, "eboot.bin") != NULL);
        const mp_manifest_entry *ini = manifest_find(p, "dlc_emu.ini");
        MP_CHECK(ini && ini->size == strlen(INI_ONE_EACH) && !(ini->flags & MP_MANIFEST_PACKED));
        int loose = 0;
        for (size_t i = 0; i < p->nfiles; i++)
            if (!strcmp(p->files[i].path, "dlc_emu.ini") && p->files[i].role == MP_ROLE_LOOSE) loose = 1;
        MP_CHECK(loose);
        mpt_clear(TMP "/orig");
        MP_CHECK(mp_unpack_original(p, TMP "/orig", 2, 0, &e) == 0);
        mp_p5z_close(p);
    }
    mp_verify_result vr;
    MP_CHECK(mp_verify_p5z(TMP "/g.p5z", 2, 0, &vr, &e) == 0);
    MP_CHECK_EQ_U64(vr.files_bad, 0);
    // the original: the game, the DLC's folder as it was, and the ini
    MP_CHECK(mpt_dirs_equal(TMP "/orig/addcont0", FX "/dlc_data", NULL));
    size_t n = 0;
    uint8_t *b = mpt_slurp(TMP "/orig/dlc_emu.ini", &n);
    MP_CHECK(b && n == strlen(INI_ONE_EACH) && !memcmp(b, INI_ONE_EACH, n));
    free(b);
    MP_CHECK(mp_path_kind(TMP "/orig/eboot.bin") == 1);
    MP_CHECK(mp_path_kind(TMP "/orig/addcont1") == 0);
}

// A dump checked against its table is as complete with its DLC as without: nothing of them is a file
// of more. A folder for the console has addcont0/ and the ini too.
MP_TEST(pack_dlc_complete_and_folder) {
    const char *dlc[] = {FX "/dlc_data"};
    packed k = pack_dlc(MP_FIXTURES_DIR "/playgo/complete", dlc, 1, TMP "/pg.p5z", 1);
    MP_CHECK(k.rc == 0);
    MP_CHECK(k.r.complete.checkable);
    MP_CHECK_EQ_U64(k.r.complete.missing, 0);
    MP_CHECK_EQ_U64(k.r.complete.extra_count, 0);
    k = pack_dlc(FX "/mini", dlc, 1, TMP "/folder", 0);
    MP_CHECK(k.rc == 0);
    MP_CHECK(mp_path_kind(TMP "/folder/dlc_emu.ini") == 1);
    MP_CHECK(mp_path_kind(TMP "/folder/addcont0/module/libdlc.prx") == 1);   // a module stays loose
    // so does its sce_sys/, so addcont0/ is a real folder there, and in the console's cache of a .p5z
    MP_CHECK(mp_path_kind(TMP "/folder/addcont0/sce_sys/param.json") == 1);
    MP_CHECK(mp_path_kind(TMP "/folder/addcont0/sce_sys/icon0.png") == 1);
    MP_CHECK(mp_path_kind(TMP "/folder/addcont0/data/dlc1.bin") == 0);   // packed
    mp_verify_result vr;
    mp_err e = {{0}};
    MP_CHECK(mp_verify_folder(TMP "/folder", NULL, 2, 0, &vr, &e) == 0);
    MP_CHECK_EQ_U64(vr.files_bad, 0);
}

// A game of PPSA00000 written in TMP: eboot.bin, its param.json and the files given.
static void write_game(const char *dir, const char *const *more, size_t n) {
    char p[1024];
    mpt_clear(dir);
    snprintf(p, sizeof p, "%s/eboot.bin", dir);
    mpt_put_text(p, "\x7f" "ELF game");
    snprintf(p, sizeof p, "%s/sce_sys/param.json", dir);
    mpt_put_text(p, "{\"titleId\": \"PPSA00000\", \"contentId\": \"UP0000-PPSA00000_00-MAXPACKTEST00000\"}");
    for (size_t i = 0; i < n; i++) {
        snprintf(p, sizeof p, "%s/%s", dir, more[i]);
        mpt_put_text(p, "x");
    }
}

// count DLC folders in TMP/<name>/dNNN, each of PPSA00000 with its own content ID; data: with a file
// of its own. Their paths in paths (malloc'd strings).
static void write_dlc(const char *name, size_t count, int data, char **paths) {
    char dir[1024], p[1100];
    for (size_t i = 0; i < count; i++) {
        snprintf(dir, sizeof dir, TMP "/%s/d%03zu", name, i);
        snprintf(p, sizeof p, "%s/sce_sys/param.json", dir);
        char json[200];
        snprintf(json, sizeof json, "{\"contentId\": \"UP0000-PPSA00000_00-MAXPACKMANY%c%04zu\"}", data ? 'D' : 'L', i);
        mpt_put_text(p, json);
        if (data) {
            snprintf(p, sizeof p, "%s/data/x.bin", dir);
            mpt_put_text(p, "x");
        }
        paths[i] = mp_strdup(dir);
    }
}

static void refused(const char *what, const char *game, const char *const *dlc, size_t n, const char *code,
                    const char *words) {
    packed k = pack_dlc(game, dlc, n, TMP "/no.p5z", 1);
    if (k.rc != -1) MP_FAIL_("%s: packed (rc %d)", what, k.rc);
    if (!k.r.dlc_error || strcmp(k.r.dlc_error, code)) MP_FAIL_("%s: code %s, not %s", what, k.r.dlc_error ? k.r.dlc_error : "(none)", code);
    if (!strstr(k.e.msg, words)) MP_FAIL_("%s: \"%s\" does not say \"%s\"", what, k.e.msg, words);
    if (mp_path_kind(TMP "/no.p5z") != 0) MP_FAIL_("%s: the .p5z is left", what);
}

// What stops packing, before anything is written, with its code and its words.
MP_TEST(dlc_refusals) {
    const char *other[] = {FX "/dlc_other.pkg"};
    refused("another game's", FX "/mini", other, 1, "dlc_refused", "of another game");
    const char *twice[] = {FX "/dlc_data.pkg", FX "/dlc_data"};
    refused("the same twice", FX "/mini", twice, 2, "dlc_refused", "the same content ID");
    const char *game[] = {FX "/mini"};
    refused("a game", FX "/mini", game, 1, "dlc_refused", "it has eboot.bin");
    const char *game_pkg[] = {FX "/mini.pkg"};
    refused("a game's package", FX "/mini", game_pkg, 1, "dlc_refused", "a game's package, not additional content");
    const char *noparam[] = {FX "/dlc_noparam"};
    refused("no param.json", FX "/mini", noparam, 1, "dlc_refused", "no sce_sys/param.json");
    const char *badcid[] = {FX "/dlc_badcid"};
    refused("a bad content ID", FX "/mini", badcid, 1, "dlc_refused", "36 characters");
    const char *gone[] = {FX "/no_such_dlc"};
    refused("one that is not there", FX "/mini", gone, 1, "dlc_refused", "does not exist");
    const char *data[] = {FX "/dlc_data"};
    refused("a dump with its own ini", MP_FIXTURES_DIR "/playgo/leftovers", data, 1, "dlc_has_ini",
            "already has a dlc_emu.ini");
    const char *upper[] = {"DLC_EMU.INI"};
    write_game(TMP "/g_ini", upper, 1);
    refused("its own ini, in capitals", TMP "/g_ini", data, 1, "dlc_has_ini", "already has a dlc_emu.ini");
    const char *folder[] = {"AddCont0/x.bin"};
    write_game(TMP "/g_addcont", folder, 1);
    refused("a folder of its own where the DLC go", TMP "/g_addcont", data, 1, "dlc_refused", "already has AddCont0/");
    const char *file[] = {"addcont0"};
    write_game(TMP "/g_addcont_file", file, 1);
    refused("a file of its own where the DLC go", TMP "/g_addcont_file", data, 1, "dlc_refused", "already has addcont0");
    // names in another case are the same names: a game with EBOOT.BIN is no DLC; a DLC whose files are
    // in Sce_Sys/ is one
    mpt_clear(TMP "/dlc_caps");
    mpt_put_text(TMP "/dlc_caps/Sce_Sys/Param.json", "{\"contentId\": \"" DATA_CID "\"}");
    mpt_put_text(TMP "/dlc_caps/EBOOT.BIN", "x");
    const char *caps[] = {TMP "/dlc_caps"};
    refused("a game in capitals", FX "/mini", caps, 1, "dlc_refused", "it has eboot.bin");
    mpt_clear(TMP "/dlc_caps/EBOOT.BIN");
    packed ok = pack_dlc(FX "/mini", caps, 1, TMP "/caps.p5z", 1);
    MP_CHECK(ok.rc == 0 && ok.r.dlc_license == 1 && ok.r.dlc_data == 0);
    // a game with no title ID cannot be told its DLC
    mpt_clear(TMP "/g_noid");
    mpt_put_text(TMP "/g_noid/eboot.bin", "x");
    refused("a game with no title ID", TMP "/g_noid", data, 1, "dlc_refused", "gives no title ID");
    // a packed DLC
    char *packed_dlc[1] = {TMP "/dlc_packed"};
    mpt_clear(TMP "/dlc_packed");
    mpt_put_text(TMP "/dlc_packed/sce_sys/param.json", "{\"contentId\": \"" DATA_CID "\"}");
    mpt_put_text(TMP "/dlc_packed/ampr_zstd.index", "x");
    refused("a packed DLC", FX "/mini", (const char *const *)packed_dlc, 1, "dlc_refused", "is packed already");
    // 101 with data, and so many licenses that the ini would go over 32 KiB
    char *many[400];
    write_dlc("many_data", 101, 1, many);
    refused("101 with data", FX "/mini", (const char *const *)many, 101, "dlc_too_many", "at most 100");
    for (size_t i = 0; i < 101; i++) free(many[i]);
    write_dlc("many_license", 400, 0, many);
    refused("too many licenses", FX "/mini", (const char *const *)many, 400, "dlc_too_many", "32 KiB");
    for (size_t i = 0; i < 400; i++) free(many[i]);
}

static int describe_ok(const char *path, mp_describe *d, mp_dlc_list *l);

// A "DLC" with the game's own content ID is the game (dlc.pkg is the mini game as a package of
// additional content), not a DLC of it.
MP_TEST(dlc_is_the_game) {
    const char *self[] = {FX "/dlc.pkg"};
    refused("the game's own content ID", FX "/mini", self, 1, "dlc_refused", "is the game's own content ID");
    refused("the game's own content ID, of a package", FX "/mini.pkg", self, 1, "dlc_refused",
            "is the game's own content ID");
}

// What dump tools leave behind and the OS's files are no files of a DLC: a license dumped with them is
// still a license, and a DLC with files of its own goes in without them.
MP_TEST(dlc_leftovers_left_out) {
    mpt_clear(TMP "/lic_left");
    mpt_put_text(TMP "/lic_left/sce_sys/param.json", "{\"contentId\": \"" LICENSE_CID "\"}");
    mpt_put_text(TMP "/lic_left/entitlements.txt", "x");
    mpt_put_text(TMP "/lic_left/entitlement_key.dat", "x");
    mpt_put_text(TMP "/lic_left/decrypted/x.bin", "x");
    mpt_put_text(TMP "/lic_left/some.prx.auth_info", "x");
    mpt_put_text(TMP "/lic_left/Thumbs.db", "x");
    mpt_clear(TMP "/data_left");
    mpt_put_text(TMP "/data_left/sce_sys/param.json", "{\"contentId\": \"" DATA_CID "\"}");
    mpt_put_text(TMP "/data_left/data/x.bin", "data of its own");
    mpt_put_text(TMP "/data_left/entitlement_key.dat", "x");
    mpt_put_text(TMP "/data_left/decrypted/data/x.bin", "x");
    mpt_put_text(TMP "/data_left/data/.DS_Store", "x");
    const char *dlc[] = {TMP "/lic_left", TMP "/data_left"};
    packed k = pack_dlc(FX "/mini", dlc, 2, TMP "/left.p5z", 1);
    MP_CHECK(k.rc == 0);
    MP_CHECK_EQ_U64(k.r.dlc_license, 1);
    MP_CHECK_EQ_U64(k.r.dlc_data, 1);
    MP_CHECK_EQ_U64(k.r.dlc_bytes, strlen("{\"contentId\": \"" DATA_CID "\"}") + strlen("data of its own"));
    mp_p5z *p = NULL;
    mp_err e = {{0}};
    if (k.rc == 0 && mp_p5z_open(TMP "/left.p5z", &p, &e) == 0) {
        MP_CHECK(manifest_find(p, "addcont0/data/x.bin") != NULL);
        MP_CHECK(manifest_find(p, "addcont0/sce_sys/param.json") != NULL);
        MP_CHECK(manifest_find(p, "addcont0/entitlement_key.dat") == NULL);
        MP_CHECK(manifest_find(p, "addcont0/decrypted/data/x.bin") == NULL);
        MP_CHECK(manifest_find(p, "addcont0/data/.DS_Store") == NULL);
        MP_CHECK(manifest_find(p, "addcont1/entitlements.txt") == NULL);
        mp_p5z_close(p);
    }
    // describe says the same of each
    mp_describe d;
    mp_dlc_list l;
    if (describe_ok(TMP "/lic_left", &d, &l)) {
        MP_CHECK(d.dlc && !d.dlc_data);
        mp_dlc_list_free(&l);
    }
}

// 100 with data is the most, and goes: addcont0 to addcont99.
MP_TEST(dlc_hundred) {
    char *many[100];
    write_dlc("hundred", 100, 1, many);
    packed k = pack_dlc(FX "/mini", (const char *const *)many, 100, TMP "/h.p5z", 1);
    MP_CHECK(k.rc == 0);
    MP_CHECK_EQ_U64(k.r.dlc_data, 100);
    mp_p5z *p = NULL;
    mp_err e = {{0}};
    if (k.rc == 0 && mp_p5z_open(TMP "/h.p5z", &p, &e) == 0) {
        MP_CHECK(manifest_find(p, "addcont99/data/x.bin") != NULL);
        MP_CHECK(manifest_find(p, "addcont0/data/x.bin") != NULL);
        mp_p5z_close(p);
    }
    for (size_t i = 0; i < 100; i++) free(many[i]);
}

// ---- what describe (info --json) says ------------------------------------------------------------

static int describe_ok(const char *path, mp_describe *d, mp_dlc_list *l) {
    mp_err e = {{0}};
    mp_complete c;
    int rc = mp_describe_full(path, d, &c, l, &e);
    if (rc != 0) MP_FAIL_("%s: %s", path, e.msg);
    else mp_complete_free(&c);
    return rc == 0;
}

// A DLC says it is one, whether it has files of its own, and is of its game's title ID (its content
// ID's); a game says it is none; its icon is read as a game's.
MP_TEST(describe_dlc) {
    mp_describe d;
    mp_dlc_list l;
    if (describe_ok(FX "/dlc_data.pkg", &d, &l)) {
        MP_CHECK(d.dlc && d.dlc_data);
        MP_CHECK_STR(d.kind, "fpkg");
        MP_CHECK_STR(d.title_id, "PPSA00000");
        MP_CHECK_STR(d.content_id, DATA_CID);
        MP_CHECK_STR(d.name, "MaxPack Test Bonus");
        MP_CHECK_EQ_U64(l.n, 0);
        mp_dlc_list_free(&l);
    }
    if (describe_ok(FX "/dlc_data", &d, &l)) {
        MP_CHECK(d.dlc && d.dlc_data);
        MP_CHECK_STR(d.title_id, "PPSA00000");
        mp_dlc_list_free(&l);
    }
    if (describe_ok(FX "/dlc_license", &d, &l)) {
        MP_CHECK(d.dlc && !d.dlc_data);
        MP_CHECK_STR(d.content_id, LICENSE_CID);
        mp_dlc_list_free(&l);
    }
    if (describe_ok(FX "/dlc_license.pkg", &d, &l)) {
        MP_CHECK(d.dlc && !d.dlc_data);
        mp_dlc_list_free(&l);
    }
    if (describe_ok(FX "/mini", &d, &l)) {
        MP_CHECK(!d.dlc && !d.dlc_data && l.n == 0);
        mp_dlc_list_free(&l);
    }
    if (describe_ok(FX "/mini.pkg", &d, &l)) {
        MP_CHECK(!d.dlc);
        mp_dlc_list_free(&l);
    }
    // a package is what its content type says: a game's package with no eboot.bin (another builder's
    // test package) is still a game's
    if (describe_ok(FX "/lpp120-mixed.pkg", &d, &l)) {
        MP_CHECK(!d.dlc && !d.dlc_data);
        MP_CHECK_STR(d.title_id, "PPSA12345");
        mp_dlc_list_free(&l);
    }
    uint8_t *png = NULL;
    size_t len = 0;
    mp_err e = {{0}};
    MP_CHECK(mp_describe_icon(FX "/dlc_data.pkg", &png, &len, &e) == 0 && len > 8 && !memcmp(png, "\x89PNG", 4));
    free(png);
}

// A .p5z made with DLC lists them from its dlc_emu.ini, with what each one's folder holds; so does the
// folder for the console made of the same, whose files are partly in its packs.
MP_TEST(describe_carried) {
    const char *dlc[] = {FX "/dlc_data.pkg", FX "/dlc_license"};
    packed k = pack_dlc(FX "/mini", dlc, 2, TMP "/c.p5z", 1);
    MP_CHECK(k.rc == 0);
    packed f = pack_dlc(FX "/mini", dlc, 2, TMP "/c_folder", 0);
    MP_CHECK(f.rc == 0);
    mpt_list files = mpt_list_dir(FX "/dlc_data");
    for (int i = 0; i < 2; i++) {
        mp_describe d;
        mp_dlc_list l;
        if (!describe_ok(i ? TMP "/c_folder" : TMP "/c.p5z", &d, &l)) continue;
        MP_CHECK(!d.dlc);
        MP_CHECK_EQ_U64(l.n, 2);
        if (l.n == 2) {
            MP_CHECK_STR(l.v[0].content_id, DATA_CID);
            MP_CHECK(l.v[0].data);
            MP_CHECK_STR(l.v[0].folder, "addcont0");
            MP_CHECK_EQ_U64(l.v[0].files, files.n);
            MP_CHECK_EQ_U64(l.v[0].bytes, mpt_dir_size(FX "/dlc_data"));
            MP_CHECK_STR(l.v[1].content_id, LICENSE_CID);
            MP_CHECK(!l.v[1].data);
            MP_CHECK_STR(l.v[1].folder, "");
            MP_CHECK_EQ_U64(l.v[1].bytes, 0);
        }
        mp_dlc_list_free(&l);
    }
    mpt_list_free(&files);
}

// What dlc_emu.ini says, read as dlc_emu would take it: hostile ones end in a bounded list, never a
// crash.
MP_TEST(parse_hostile) {
    mp_dlc_list l;
    // \r\n, spaces, no final newline, a section of another kind, keys it does not know, a line with no
    // '=', a content ID of 35 characters (skipped), a mount point too long for dlc_emu (no folder)
    const char *ini = "\xef\xbb\xbf[PSAC]\r\n content_id = " DATA_CID " \r\nmount_point=/app0/addcont0\r\n\r\n"
                      "[PSCONS]\ncontent_id=UP0000-PPSA00000_00-MAXPACKCONSUME01\n"
                      "[PSAL]\nnonsense\nlabel=X\ncontent_id=UP0000-PPSA00000_00-MAXPACKSHORT001\n"
                      "[PSAC]\ncontent_id=UP0000-PPSA00000_00-MAXPACKLONGMOUNT\nmount_point=/app0/addcont0/deeper\n"
                      "[PSAL]\ncontent_id=" LICENSE_CID;
    MP_CHECK(mp_dlc_parse(ini, strlen(ini), &l) == 0);
    MP_CHECK_EQ_U64(l.n, 3);
    if (l.n == 3) {
        MP_CHECK_STR(l.v[0].content_id, DATA_CID);
        MP_CHECK(l.v[0].data);
        MP_CHECK_STR(l.v[0].folder, "addcont0");
        MP_CHECK_STR(l.v[1].content_id, "UP0000-PPSA00000_00-MAXPACKLONGMOUNT");
        MP_CHECK_STR(l.v[1].folder, "");
        MP_CHECK_STR(l.v[2].content_id, LICENSE_CID);
        MP_CHECK(!l.v[2].data);
    }
    mp_dlc_list_free(&l);
    // over 32 KiB: dlc_emu reads none of it
    size_t big = MP_DLC_INI_MAX + 1;
    char *b = malloc(big);
    memset(b, 'x', big);
    MP_CHECK(mp_dlc_parse(b, big, &l) == 0 && l.n == 0);
    mp_dlc_list_free(&l);
    // 2000 sections, NUL bytes and no newline at all: at most MP_DLC_MAX_ENTRIES
    size_t n = 0, cap = 2000 * 60;
    char *many = malloc(cap);
    for (int i = 0; i < 2000 && n + 60 < cap; i++)
        n += (size_t)snprintf(many + n, cap - n, "[PSAL]\ncontent_id=UP0000-PPSA00000_00-MAXPACKMANYL%04d\n", i);
    MP_CHECK(mp_dlc_parse(many, n, &l) == 0 && l.n == 0);   // over 32 KiB too
    mp_dlc_list_free(&l);
    MP_CHECK(mp_dlc_parse(many, 32000, &l) == 0 && l.n > 0 && l.n <= MP_DLC_MAX_ENTRIES);
    mp_dlc_list_free(&l);
    memset(many, 0, 1000);
    MP_CHECK(mp_dlc_parse(many, 1000, &l) == 0 && l.n == 0);
    mp_dlc_list_free(&l);
    free(many);
    free(b);
}

int main(void) {
    MP_RUN(merge_basic);
    MP_RUN(merge_twice);
    MP_RUN(ini_text);
    MP_RUN(pack_with_dlc);
    MP_RUN(pack_dlc_complete_and_folder);
    MP_RUN(dlc_refusals);
    MP_RUN(dlc_is_the_game);
    MP_RUN(dlc_leftovers_left_out);
    MP_RUN(dlc_hundred);
    MP_RUN(describe_dlc);
    MP_RUN(describe_carried);
    MP_RUN(parse_hostile);
    MP_TEST_MAIN_END();
}
