// SPDX-License-Identifier: GPL-3.0-or-later
// A game packed with its DLC (make_fpkg.py's mini game and its DLC): the source made of a game and other
// sources.
#include <stdlib.h>

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
    MP_CHECK(mp_source_merge(game, parts, prefixes, 1, "dlc_emu.ini", ini, 2, 1700000000, &m, &e) == 0);
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
    MP_CHECK(mp_source_merge(game, NULL, NULL, 0, "eboot.bin", (const uint8_t *)"x", 1, 0, &m, &e) == -1);
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
    MP_CHECK(mp_source_merge(game, parts, prefixes, 2, NULL, NULL, 0, 0, &m, &e) == -1);
    MP_CHECK_CONTAINS(e.msg, "is twice");
    // without anything to add, the game's files as they are
    game = open_or_fail(FX "/mini", MP_OPEN_GAME);
    if (!game) return;
    size_t n = mp_source_count(game);
    MP_CHECK(mp_source_merge(game, NULL, NULL, 0, NULL, NULL, 0, 0, &m, &e) == 0);
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

int main(void) {
    MP_RUN(merge_basic);
    MP_RUN(merge_twice);
    MP_RUN(ini_text);
    MP_RUN(pack_with_dlc);
    MP_RUN(pack_dlc_complete_and_folder);
    MP_RUN(dlc_refusals);
    MP_RUN(dlc_hundred);
    MP_TEST_MAIN_END();
}
