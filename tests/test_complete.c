// SPDX-License-Identifier: GPL-3.0-or-later
// Whether a dump is complete: the flat path table hash, the reader of playgo-hash-table.dat on
// hostile input, the rule (missing, extra, OS junk, the package's own entries, right.sprx, what dump
// tools leave behind), and the check over the fixtures of make_playgo.py, packed and unpacked.
#include <stdio.h>
#include <stdlib.h>

#include "mp_complete.h"
#include "mp_describe.h"
#include "mp_fixture.h"
#include "mp_json.h"
#include "mp_p5z_internal.h"
#include "mp_playgo.h"

#define TMP MP_TEST_TMP "/complete"
#define PG MP_FIXTURES_DIR "/playgo"

// Known answers. The ASCII ones agree with FltPathHash of ProsperoPkgTool (MIT); pfs_image.dat and
// naps_pkg_layout.dat are the two entries of real packages' outer table that ps5upload's flt.rs
// checks; the rest come from make_playgo.py's port.
MP_TEST(hash_known_answers) {
    MP_CHECK_EQ_U64(mp_flt_hash("pfs_image.dat"), 0xa65627bdd8154701ull);
    MP_CHECK_EQ_U64(mp_flt_hash("naps_pkg_layout.dat"), 0xc683f67a1dececafull);
    MP_CHECK_EQ_U64(mp_flt_hash(""), 0x29842e87f7fa7305ull);
    MP_CHECK_EQ_U64(mp_flt_hash("a"), 0x21a42e83f7fa7305ull);
    // the word boundaries of the sponge: 7, 8, 9, 16 and 17 bytes
    MP_CHECK_EQ_U64(mp_flt_hash("abcdefg"), 0x75e4564f3b129f09ull);
    MP_CHECK_EQ_U64(mp_flt_hash("abcdefgh"), 0x756d564f3b129f09ull);
    MP_CHECK_EQ_U64(mp_flt_hash("abcdefghi"), 0x6201b886906b57edull);
    MP_CHECK_EQ_U64(mp_flt_hash("abcdefghijklmnop"), 0xe74f810b99868aa0ull);
    MP_CHECK_EQ_U64(mp_flt_hash("abcdefghijklmnopq"), 0xb3606fb90e65b775ull);
    MP_CHECK_EQ_U64(mp_flt_hash("eboot.bin"), 0xf16a60eee4f247fcull);
    MP_CHECK_EQ_U64(mp_flt_hash("sce_sys/about/right.sprx"), 0xaec9f8dac5e9f40bull);
    // deep paths
    MP_CHECK_EQ_U64(mp_flt_hash("Media/Deep/folders/one/two/three/four/five/six/seven/eight.bin"), 0xd15b4e48fe0851a8ull);
    // UTF-8: its bytes as they are; only ASCII letters change case
    MP_CHECK_EQ_U64(mp_flt_hash("data/Caf\xc3\xa9 \xc3\xb1" "and\xc3\xba.txt"), 0xa9f4c0de209c90fdull);
    MP_CHECK_EQ_U64(mp_flt_hash("DATA/CAF\xc3\xa9 \xc3\xb1" "AND\xc3\xba.TXT"), 0xa9f4c0de209c90fdull);
    MP_CHECK_EQ_U64(mp_flt_hash("DATA/CAF\xc3\x89 \xc3\x91" "AND\xc3\x9a.TXT"), 0xd134c0de508c02e9ull);
    MP_CHECK_EQ_U64(mp_flt_hash("\xc3\xa9"), 0x11e42603f7fa7980ull);
    MP_CHECK_EQ_U64(mp_flt_hash("\xc3\x89"), 0x11e42603f7fa7b84ull);
}

MP_TEST(hash_case_and_slash) {
    MP_CHECK_EQ_U64(mp_flt_hash("PFS_IMAGE.DAT"), mp_flt_hash("pfs_image.dat"));
    MP_CHECK_EQ_U64(mp_flt_hash("EBOOT.BIN"), mp_flt_hash("eboot.bin"));
    MP_CHECK_EQ_U64(mp_flt_hash("/pfs_image.dat"), 0xa65627bdd8154701ull);
    MP_CHECK_EQ_U64(mp_flt_hash("//eboot.bin"), mp_flt_hash("eboot.bin"));
    MP_CHECK_EQ_U64(mp_flt_hash("/"), mp_flt_hash(""));
    MP_CHECK_EQ_U64(mp_flt_hash_n("eboot.bin.auth_info", 9), mp_flt_hash("eboot.bin"));
    MP_CHECK(mp_flt_hash("a/b") != mp_flt_hash("a\\b"));
    MP_CHECK(mp_flt_hash("ab") != mp_flt_hash("ba"));
}

// ---- the table ----------------------------------------------------------------------------------

static void put32(uint8_t *p, uint32_t v) {
    for (int i = 0; i < 4; i++) p[i] = (uint8_t)(v >> (8 * i));
}
static void put64(uint8_t *p, uint64_t v) {
    for (int i = 0; i < 8; i++) p[i] = (uint8_t)(v >> (8 * i));
}

// A playgo-hash-table.dat with these hashes, in the order given (malloc).
static uint8_t *table_of(const uint64_t *h, size_t n, size_t *len) {
    *len = MP_PLAYGO_HEADER + 8 * n;
    uint8_t *d = calloc(1, *len);
    put32(d, 1);
    put32(d + 4, 0x08000000);
    put32(d + 8, MP_PLAYGO_HEADER);
    put32(d + 12, (uint32_t)(8 * n));
    memcpy(d + 0x18, "\x7f" "FLT", 4);
    put32(d + 0x24, (uint32_t)n);
    put64(d + 0x28, 0x92CA8AAB26A24F51ull);
    put64(d + 0x30, 0x09BBB761A41BC44Dull);
    for (size_t i = 0; i < n; i++) put64(d + MP_PLAYGO_HEADER + 8 * i, h[i]);
    return d;
}

static uint8_t *table_of_paths(const char *const *paths, size_t n, size_t *len) {
    uint64_t *h = malloc((n ? n : 1) * sizeof *h);
    for (size_t i = 0; i < n; i++) h[i] = mp_flt_hash(paths[i]);
    uint8_t *d = table_of(h, n, len);
    free(h);
    return d;
}

MP_TEST(table_reader) {
    uint64_t h[3] = {30, 10, 20}, *got = NULL;
    size_t len = 0, n = 0;
    const char *why = NULL;
    uint8_t *d = table_of(h, 3, &len);
    // sorted for the lookup, whatever order the file has
    MP_CHECK(mp_playgo_parse(d, len, &got, &n, &why) == 0);
    MP_CHECK_EQ_U64(n, 3);
    if (got && n == 3) {
        MP_CHECK_EQ_U64(got[0], 10);
        MP_CHECK_EQ_U64(got[2], 30);
    }
    free(got);
    got = NULL;
    // truncated, with bytes after the hashes, or empty: damaged
    MP_CHECK(mp_playgo_parse(d, len - 1, &got, &n, &why) == -1 && got == NULL);
    MP_CHECK_STR(why, "damaged");
    MP_CHECK(mp_playgo_parse(d, len - 8, &got, &n, &why) == -1);
    MP_CHECK_STR(why, "damaged");
    uint8_t *longer = calloc(1, len + 8);
    memcpy(longer, d, len);
    MP_CHECK(mp_playgo_parse(longer, len + 8, &got, &n, &why) == -1);
    MP_CHECK_STR(why, "damaged");
    free(longer);
    // an absurd count: no read beyond the end, no huge allocation
    put32(d + 0x24, 0xFFFFFFFFu);
    MP_CHECK(mp_playgo_parse(d, len, &got, &n, &why) == -1 && got == NULL);
    MP_CHECK_STR(why, "damaged");
    put32(d + 0x24, 0x20000000u);
    MP_CHECK(mp_playgo_parse(d, len, &got, &n, &why) == -1);
    put32(d + 0x24, 3);
    // the data size of the header disagrees with the count
    put32(d + 12, 16);
    MP_CHECK(mp_playgo_parse(d, len, &got, &n, &why) == -1);
    MP_CHECK_STR(why, "damaged");
    put32(d + 12, 24);
    // not the format: another magic, version or header size
    d[0x18] = 'X';
    MP_CHECK(mp_playgo_parse(d, len, &got, &n, &why) == -1);
    MP_CHECK_STR(why, "unknown_format");
    d[0x18] = 0x7f;
    put32(d, 2);
    MP_CHECK(mp_playgo_parse(d, len, &got, &n, &why) == -1);
    MP_CHECK_STR(why, "unknown_format");
    put32(d, 1);
    put32(d + 8, 0x40);
    MP_CHECK(mp_playgo_parse(d, len, &got, &n, &why) == -1);
    MP_CHECK_STR(why, "unknown_format");
    put32(d + 8, MP_PLAYGO_HEADER);
    MP_CHECK(mp_playgo_parse(d, len, &got, &n, &why) == 0);
    free(got);
    got = NULL;
    // shorter than the header, nothing at all, or a JSON file under that name (seen in a dump)
    MP_CHECK(mp_playgo_parse(d, 10, &got, &n, &why) == -1);
    MP_CHECK_STR(why, "unknown_format");
    MP_CHECK(mp_playgo_parse(d, 0, &got, &n, &why) == -1);
    MP_CHECK_STR(why, "unknown_format");
    const char json[] = "{\r\n  \"addcont\": {\r\n    \"serviceIdForSharing\": [\r\n      \"x\"\r\n    ]\r\n  }\r\n}\r\n"
                        "                                                                       ";
    MP_CHECK(mp_playgo_parse((const uint8_t *)json, sizeof json - 1, &got, &n, &why) == -1);
    MP_CHECK_STR(why, "unknown_format");
    // a table that lists nothing cannot say a dump is complete
    free(d);
    d = table_of(h, 0, &len);
    MP_CHECK(mp_playgo_parse(d, len, &got, &n, &why) == -1);
    MP_CHECK_STR(why, "damaged");
    free(d);
    // repeated hashes count once
    uint64_t twice[4] = {5, 5, 7, 5};
    d = table_of(twice, 4, &len);
    MP_CHECK(mp_playgo_parse(d, len, &got, &n, &why) == 0);
    MP_CHECK_EQ_U64(n, 2);
    free(got);
    free(d);
}

// ---- the rule -----------------------------------------------------------------------------------

static const char *const IMAGE[] = {"eboot.bin", "sce_module/libc.prx", "sce_sys/about/right.sprx", "sce_sys/icon0.png",
                                    "data/a.bin", "data/b.bin", "data/Caf\xc3\xa9.txt"};
#define NIMAGE (sizeof IMAGE / sizeof *IMAGE)

typedef struct {
    mp_cfile f[32];
    size_t n;
} listing;

static void add(listing *l, const char *path, uint64_t size) {
    l->f[l->n].path = path;
    l->f[l->n].size = size;
    l->n++;
}

static int cmp_cfile(const void *a, const void *b) {
    return strcmp(((const mp_cfile *)a)->path, ((const mp_cfile *)b)->path);
}

static void check(listing *l, const uint8_t *t, size_t tl, mp_complete *c, uint8_t *cls) {
    qsort(l->f, l->n, sizeof *l->f, cmp_cfile);   // as a source gives them
    MP_CHECK(mp_complete_check(l->f, l->n, t, tl, c, cls) == 0);
}

static int class_of(const listing *l, const uint8_t *cls, const char *path) {
    for (size_t i = 0; i < l->n; i++)
        if (!strcmp(l->f[i].path, path)) return cls[i];
    return -1;
}

MP_TEST(rule_complete_and_entries) {
    size_t tl = 0;
    uint8_t *t = table_of_paths(IMAGE, NIMAGE, &tl), cls[32];
    listing l = {0};
    for (size_t i = 0; i < NIMAGE; i++) add(&l, IMAGE[i], 100 + i);
    // the package's own entries in sce_sys/, outside the image: neither listed nor extra
    add(&l, "sce_sys/param.json", 300);
    add(&l, "sce_sys/playgo-hash-table.dat", tl);
    add(&l, "sce_sys/trophy2/trophy00.ucp", 50);
    mp_complete c;
    check(&l, t, tl, &c, cls);
    MP_CHECK(c.checkable);
    MP_CHECK_STR(c.reason, "");
    // right.sprx is not counted: package builders add their own
    MP_CHECK_EQ_U64(c.listed, NIMAGE - 1);
    MP_CHECK_EQ_U64(c.present, NIMAGE - 1);
    MP_CHECK_EQ_U64(c.missing, 0);
    MP_CHECK_EQ_U64(c.extra_count, 0);
    MP_CHECK_EQ_U64(c.extra_bytes, 0);
    MP_CHECK_EQ_U64(c.leftover_count, 0);
    MP_CHECK_EQ_U64(c.npaths, 0);
    MP_CHECK(class_of(&l, cls, "sce_sys/param.json") == MP_CF_IGNORED);
    MP_CHECK(class_of(&l, cls, "sce_sys/icon0.png") == MP_CF_LISTED);
    MP_CHECK(class_of(&l, cls, "data/Caf\xc3\xa9.txt") == MP_CF_LISTED);
    mp_complete_free(&c);
    // a file of the package's entries that the table lists (an image file there) counts
    listing m = {0};
    for (size_t i = 0; i < NIMAGE; i++)
        if (strcmp(IMAGE[i], "sce_sys/icon0.png")) add(&m, IMAGE[i], 1);
    check(&m, t, tl, &c, cls);
    MP_CHECK_EQ_U64(c.missing, 1);
    mp_complete_free(&c);
    free(t);
}

MP_TEST(rule_missing_extra_junk) {
    size_t tl = 0;
    uint8_t *t = table_of_paths(IMAGE, NIMAGE, &tl), cls[32];
    listing l = {0};
    // two gone, and right.sprx gone, which does not count
    add(&l, "eboot.bin", 10);
    add(&l, "sce_sys/icon0.png", 10);
    add(&l, "data/a.bin", 10);
    add(&l, "data/Caf\xc3\xa9.txt", 10);
    // in another case: the same file for the table
    add(&l, "SCE_MODULE/LIBC.PRX", 10);
    // the dump's own files, not in the original package
    add(&l, "fakelib/libScePlayGo.sprx", 1000);
    add(&l, "dlc_emu.ini", 20);
    // what dump tools leave behind
    add(&l, "decrypted/eboot.bin", 5000);
    add(&l, "Decrypted/sce_module/libc.prx", 3000);
    add(&l, "eboot.bin.auth_info", 64);
    add(&l, "sce_module/libc.prx.AUTH_INFO", 64);
    add(&l, "entitlements.txt", 7);
    // OS junk is never counted
    add(&l, "Thumbs.db", 1);
    add(&l, "data/desktop.ini", 1);
    add(&l, "data/.DS_Store", 1);
    add(&l, "._eboot.bin", 1);
    add(&l, "__MACOSX/data/._a.bin", 1);
    add(&l, "System Volume Information/IndexerVolumeGuid", 1);
    add(&l, "$RECYCLE.BIN/x", 1);
    mp_complete c;
    check(&l, t, tl, &c, cls);
    MP_CHECK(c.checkable);
    MP_CHECK_EQ_U64(c.listed, 6);
    MP_CHECK_EQ_U64(c.present, 5);
    MP_CHECK_EQ_U64(c.missing, 1);   // data/b.bin
    MP_CHECK_EQ_U64(c.extra_count, 7);
    MP_CHECK_EQ_U64(c.extra_bytes, 1000 + 20 + 5000 + 3000 + 64 + 64 + 7);
    MP_CHECK_EQ_U64(c.leftover_count, 5);
    MP_CHECK_EQ_U64(c.leftover_bytes, 5000 + 3000 + 64 + 64 + 7);
    MP_CHECK(class_of(&l, cls, "decrypted/eboot.bin") == MP_CF_LEFTOVER);
    MP_CHECK(class_of(&l, cls, "eboot.bin.auth_info") == MP_CF_LEFTOVER);
    MP_CHECK(class_of(&l, cls, "dlc_emu.ini") == MP_CF_EXTRA);
    MP_CHECK(class_of(&l, cls, "Thumbs.db") == MP_CF_IGNORED);
    MP_CHECK(class_of(&l, cls, "SCE_MODULE/LIBC.PRX") == MP_CF_LISTED);
    // the first extra paths, in path order
    MP_CHECK_EQ_U64(c.npaths, 7);
    if (c.npaths == 7) {
        MP_CHECK_STR(c.paths[0], "Decrypted/sce_module/libc.prx");
        MP_CHECK_STR(c.paths[6], "sce_module/libc.prx.AUTH_INFO");
    }
    mp_complete_free(&c);
    free(t);
}

// A file the table lists is the game's, whatever its name says: decrypted/ of a game that ships
// one is not a leftover.
MP_TEST(rule_listed_is_never_leftover) {
    static const char *const game[] = {"eboot.bin", "decrypted/notes.txt"};
    size_t tl = 0;
    uint8_t *t = table_of_paths(game, 2, &tl), cls[32];
    listing l = {0};
    add(&l, "eboot.bin", 1);
    add(&l, "decrypted/notes.txt", 2);
    add(&l, "decrypted/eboot.bin", 3);
    mp_complete c;
    check(&l, t, tl, &c, cls);
    MP_CHECK_EQ_U64(c.missing, 0);
    MP_CHECK_EQ_U64(c.leftover_count, 1);
    MP_CHECK_EQ_U64(c.leftover_bytes, 3);
    MP_CHECK(class_of(&l, cls, "decrypted/notes.txt") == MP_CF_LISTED);
    MP_CHECK(class_of(&l, cls, "decrypted/eboot.bin") == MP_CF_LEFTOVER);
    mp_complete_free(&c);
    free(t);
}

// Without a usable table nothing is counted but what dump tools are known to leave behind, which
// stays unproven (not checkable).
MP_TEST(rule_without_table) {
    listing l = {0};
    uint8_t cls[32];
    add(&l, "eboot.bin", 1);
    add(&l, "decrypted/eboot.bin", 40);
    add(&l, "data/x.bin", 2);
    mp_complete c;
    check(&l, NULL, 0, &c, cls);
    MP_CHECK(!c.checkable);
    MP_CHECK_STR(c.reason, "no_table");
    MP_CHECK_EQ_U64(c.listed + c.present + c.missing + c.extra_count + c.extra_bytes, 0);
    MP_CHECK_EQ_U64(c.leftover_count, 1);
    MP_CHECK_EQ_U64(c.leftover_bytes, 40);
    MP_CHECK(class_of(&l, cls, "decrypted/eboot.bin") == MP_CF_LEFTOVER);
    MP_CHECK(class_of(&l, cls, "eboot.bin") == MP_CF_IGNORED);
    mp_complete_free(&c);
    const uint8_t junk[8] = {1, 2, 3};
    check(&l, junk, sizeof junk, &c, cls);
    MP_CHECK(!c.checkable);
    MP_CHECK_STR(c.reason, "unknown_format");
    mp_complete_free(&c);
}

MP_TEST(rule_many_extra_paths) {
    static const char *const game[] = {"eboot.bin"};
    size_t tl = 0;
    uint8_t *t = table_of_paths(game, 1, &tl), cls[64];
    mp_cfile f[40];
    char names[40][16];
    f[0].path = "eboot.bin";
    f[0].size = 1;
    for (int i = 1; i < 40; i++) {
        snprintf(names[i], sizeof names[i], "x/%02d.bin", i);
        f[i].path = names[i];
        f[i].size = 10;
    }
    mp_complete c;
    MP_CHECK(mp_complete_check(f, 40, t, tl, &c, cls) == 0);
    MP_CHECK_EQ_U64(c.extra_count, 39);
    MP_CHECK_EQ_U64(c.extra_bytes, 390);
    MP_CHECK_EQ_U64(c.npaths, MP_COMPLETE_PATHS);
    MP_CHECK_STR(c.paths[0], "x/01.bin");
    // the JSON says the counts and, when asked, the first paths
    mp_jbuf b = {0};
    mp_complete_json(&c, 1, &b);
    MP_CHECK_CONTAINS(b.p, "\"checkable\":true");
    MP_CHECK_CONTAINS(b.p, "\"source\":\"playgo\"");
    MP_CHECK_CONTAINS(b.p, "\"listed\":1,\"present\":1,\"missing\":0,\"size_mismatch\":0,\"extra_count\":39,\"extra_bytes\":390");
    MP_CHECK_CONTAINS(b.p, "\"extra_paths\":[\"x/01.bin\"");
    mp_jbuf_free(&b);
    mp_jbuf q = {0};
    mp_complete_json(&c, 0, &q);
    MP_CHECK(q.p && !strstr(q.p, "extra_paths"));
    mp_jbuf_free(&q);
    mp_complete_free(&c);
    free(t);
}

MP_TEST(text_lines) {
    mp_complete c;
    memset(&c, 0, sizeof c);
    c.checkable = 1;
    c.reason = "";
    c.listed = c.present = 1234;
    char line[256];
    mp_complete_summary(&c, line, sizeof line);
    MP_CHECK_STR(line, "complete: all 1234 files of the original package");
    c.present = 1231;
    c.missing = 3;
    mp_complete_summary(&c, line, sizeof line);
    MP_CHECK_STR(line, "incomplete: 3 of the 1234 files of the original package are missing");
    c.missing = 1;
    mp_complete_summary(&c, line, sizeof line);
    MP_CHECK_STR(line, "incomplete: 1 of the 1234 files of the original package is missing");
    memset(&c, 0, sizeof c);
    c.reason = "no_table";
    mp_complete_summary(&c, line, sizeof line);
    MP_CHECK_CONTAINS(line, "not checked: ");
    MP_CHECK_CONTAINS(line, "sce_sys/playgo-hash-table.dat");
}

// ---- over the fixtures: a source, packed, verified, unpacked -------------------------------------

static void source_check(const char *path, mp_complete *c) {
    mp_err e = {{0}};
    mp_source *s = NULL;
    memset(c, 0, sizeof *c);
    if (mp_source_open(path, &s, &e) != 0) {
        MP_FAIL_("%s: %s", path, e.msg);
        return;
    }
    MP_CHECK(mp_complete_source(s, c, NULL, &e) == 0);
    mp_source_close(s);
}

MP_TEST(fixtures) {
    mp_complete c;
    source_check(PG "/complete", &c);
    MP_CHECK(c.checkable);
    MP_CHECK_EQ_U64(c.listed, 8);
    MP_CHECK_EQ_U64(c.present, 8);
    MP_CHECK_EQ_U64(c.missing, 0);
    MP_CHECK_EQ_U64(c.extra_count, 0);
    mp_complete_free(&c);
    source_check(PG "/missing", &c);
    MP_CHECK(c.checkable);
    MP_CHECK_EQ_U64(c.missing, 2);
    MP_CHECK_EQ_U64(c.present, 6);
    mp_complete_free(&c);
    // the same dump as a UFS2 image
    source_check(PG "/missing.ffpkg", &c);
    MP_CHECK(c.checkable);
    MP_CHECK_EQ_U64(c.missing, 2);
    mp_complete_free(&c);
    source_check(PG "/leftovers", &c);
    MP_CHECK_EQ_U64(c.missing, 0);
    MP_CHECK_EQ_U64(c.extra_count, 4);
    MP_CHECK_EQ_U64(c.leftover_count, 3);
    MP_CHECK_EQ_U64(c.leftover_bytes, 7000 + 4000 + 64);
    mp_complete_free(&c);
    source_check(PG "/notable", &c);
    MP_CHECK(!c.checkable);
    MP_CHECK_STR(c.reason, "no_table");
    MP_CHECK_EQ_U64(c.leftover_bytes, 7000);
    mp_complete_free(&c);
    source_check(PG "/badtable", &c);
    MP_CHECK_STR(c.reason, "damaged");
    mp_complete_free(&c);
    source_check(PG "/alien", &c);
    MP_CHECK_STR(c.reason, "unknown_format");
    mp_complete_free(&c);
    // a game of before, with no table
    source_check(MPT_BASIC, &c);
    MP_CHECK_STR(c.reason, "no_table");
    MP_CHECK_EQ_U64(c.leftover_count, 0);
    mp_complete_free(&c);
}

static int manifest_has(const mp_p5z *p, const char *path) {
    for (size_t i = 0; i < p->man.n; i++)
        if (!strcmp(p->man.v[i].path, path)) return 1;
    return 0;
}

static void pack_with(const char *src, const char *out, int leave_out, mp_pack_result *r) {
    mp_err e = {{0}};
    mp_pack_out po;
    mpt_clear(out);
    mpt_parent_dirs(out);
    if (mp_pack_out_p5z(out, 2, &po, &e) != 0) {
        MP_FAIL_("%s", e.msg);
        return;
    }
    mp_pack_opts o;
    mp_pack_opts_default(&o);
    o.threads = 2;
    o.progress = 0;
    o.level = 3;
    o.leave_out_leftovers = leave_out;
    if (mp_pack_run(src, &po, &o, r, &e) != 0) MP_FAIL_("pack %s: %s", src, e.msg);
    po.destroy(po.ctx);
}

MP_TEST(pack_leaves_out_proven_leftovers) {
    mp_err e = {{0}};
    mp_pack_result r;
    mpt_clear(TMP);
    // off: everything goes, as always (the identity of the dump)
    pack_with(PG "/leftovers", TMP "/all.p5z", 0, &r);
    MP_CHECK(r.complete.checkable);
    MP_CHECK_EQ_U64(r.complete.leftover_count, 3);
    MP_CHECK_EQ_U64(r.left_out_files, 0);
    mp_p5z *p = NULL;
    if (mp_p5z_open(TMP "/all.p5z", &p, &e) == 0) {
        MP_CHECK(manifest_has(p, "decrypted/eboot.bin"));
        MP_CHECK(manifest_has(p, "eboot.bin.auth_info"));
        mp_p5z_close(p);
    }
    // on: the leftovers the table proves are not the game's stay out; the rest is all there
    pack_with(PG "/leftovers", TMP "/out.p5z", 1, &r);
    MP_CHECK_EQ_U64(r.left_out_files, 3);
    MP_CHECK_EQ_U64(r.left_out_bytes, 7000 + 4000 + 64);
    p = NULL;
    if (mp_p5z_open(TMP "/out.p5z", &p, &e) == 0) {
        MP_CHECK(!manifest_has(p, "decrypted/eboot.bin"));
        MP_CHECK(!manifest_has(p, "decrypted/sce_module/libc.prx"));
        MP_CHECK(!manifest_has(p, "eboot.bin.auth_info"));
        MP_CHECK(manifest_has(p, "eboot.bin") && manifest_has(p, "dlc_emu.ini") && manifest_has(p, "Thumbs.db"));
        // the .p5z checks against the table it carries
        mp_complete c;
        MP_CHECK(mp_complete_p5z(p, &c, &e) == 0);
        MP_CHECK(c.checkable);
        MP_CHECK_EQ_U64(c.missing, 0);
        MP_CHECK_EQ_U64(c.present, 8);
        MP_CHECK_EQ_U64(c.leftover_count, 0);
        mp_complete_free(&c);
        MP_CHECK(mp_unpack_original(p, TMP "/orig", 2, 0, &e) == 0);
        mp_p5z_close(p);
        MP_CHECK(mp_path_kind(TMP "/orig/decrypted") == 0);
        MP_CHECK(mp_path_kind(TMP "/orig/eboot.bin") == 1);
    }
    mp_verify_result vr;
    MP_CHECK(mp_verify_p5z(TMP "/out.p5z", 2, 0, &vr, &e) == 0);
    MP_CHECK_EQ_U64(vr.files_bad, 0);
    // without a table nothing is left out: it cannot be proven not to be the game's
    pack_with(PG "/notable", TMP "/nt.p5z", 1, &r);
    MP_CHECK(!r.complete.checkable);
    MP_CHECK_EQ_U64(r.complete.leftover_count, 1);
    MP_CHECK_EQ_U64(r.left_out_files, 0);
    p = NULL;
    if (mp_p5z_open(TMP "/nt.p5z", &p, &e) == 0) {
        MP_CHECK(manifest_has(p, "decrypted/eboot.bin"));
        mp_p5z_close(p);
    }
    // a folder for the console leaves them out too, and verify --against does not miss them
    mpt_clear(TMP "/dir");
    mp_pack_out po;
    if (mp_pack_out_dir(TMP "/dir", &po, &e) == 0) {
        mp_pack_opts o;
        mp_pack_opts_default(&o);
        o.threads = 2;
        o.progress = 0;
        o.level = 3;
        o.leave_out_leftovers = 1;
        MP_CHECK(mp_pack_run(PG "/leftovers", &po, &o, &r, &e) == 0);
        po.destroy(po.ctx);
        MP_CHECK(mp_path_kind(TMP "/dir/decrypted") == 0);
        MP_CHECK(mp_path_kind(TMP "/dir/eboot.bin.auth_info") == 0);
        MP_CHECK(mp_verify_folder(TMP "/dir", PG "/leftovers", 2, 0, &vr, &e) == 0);
        MP_CHECK_EQ_U64(vr.files_bad, 0);
        mp_complete c;
        MP_CHECK(mp_describe_complete(TMP "/dir", &c, &e) == 0);
        MP_CHECK(c.checkable);
        MP_CHECK_EQ_U64(c.missing, 0);
        MP_CHECK_EQ_U64(c.present, 8);
        mp_complete_free(&c);
    }
}

MP_TEST(describe_says_it) {
    mp_err e = {{0}};
    mp_describe d;
    mp_complete c;
    MP_CHECK(mp_describe_path_ex(PG "/missing", &d, &c, &e) == 0);
    MP_CHECK_STR(d.kind, "folder");
    MP_CHECK(c.checkable);
    MP_CHECK_EQ_U64(c.missing, 2);
    mp_complete_free(&c);
    pack_with(PG "/missing", TMP "/m.p5z", 1, &(mp_pack_result){0});
    MP_CHECK(mp_describe_path_ex(TMP "/m.p5z", &d, &c, &e) == 0);
    MP_CHECK_STR(d.kind, "p5z");
    MP_CHECK(c.checkable);
    MP_CHECK_EQ_U64(c.missing, 2);
    MP_CHECK_EQ_U64(c.listed, 8);
    mp_complete_free(&c);
    MP_CHECK(mp_describe_complete(TMP "/m.p5z", &c, &e) == 0);
    MP_CHECK_EQ_U64(c.missing, 2);
    mp_complete_free(&c);
    // a .p5z of a game with no table says so
    mpt_pack(MPT_BASIC, TMP "/b.p5z", 1, NULL);
    MP_CHECK(mp_describe_complete(TMP "/b.p5z", &c, &e) == 0);
    MP_CHECK(!c.checkable);
    MP_CHECK_STR(c.reason, "no_table");
    mp_complete_free(&c);
}


// ---- the dump tool's list (ampr_emu.index) -------------------------------------------------------

#define DI MP_FIXTURES_DIR "/dumpindex"

typedef struct {
    const char *path;
    uint64_t size;
} irow;

// An ampr_emu.index (AMPRIDX3) with these rows, as the dump tool writes it; the hash slots are left
// empty, which the check does not read (malloc).
static uint8_t *index_of(const irow *r, size_t n, size_t *len) {
    size_t blob = 0;
    for (size_t i = 0; i < n; i++) blob += 6 + strlen(r[i].path) + 1;
    size_t hash_off = (48 + 24 * n + blob + 15) & ~(size_t)15, slots = 2;
    while (slots < 2 * n) slots <<= 1;
    *len = hash_off + 16 * slots;
    uint8_t *d = calloc(1, *len);
    memcpy(d, "AMPRIDX3", 8);
    put32(d + 8, 3);
    put32(d + 12, 24);
    put64(d + 16, n);
    put64(d + 24, blob);
    put64(d + 32, hash_off);
    put32(d + 40, 16);
    put32(d + 44, (uint32_t)slots);
    size_t at = 0;
    for (size_t i = 0; i < n; i++) {
        uint8_t *rec = d + 48 + 24 * i;
        size_t len_i = 6 + strlen(r[i].path);
        put32(rec, (uint32_t)at);
        put32(rec + 4, (uint32_t)len_i);
        put64(rec + 8, r[i].size);
        put64(rec + 16, 1714979290);
        memcpy(d + 48 + 24 * n + at, "/app0/", 6);
        memcpy(d + 48 + 24 * n + at + 6, r[i].path, len_i - 6);
        at += len_i + 1;
    }
    return d;
}

static void check_di(listing *l, const uint8_t *table, size_t tl, const uint8_t *di, size_t dl, mp_complete *c, uint8_t *cls) {
    qsort(l->f, l->n, sizeof *l->f, cmp_cfile);
    MP_CHECK(mp_complete_check_lists(l->f, l->n, table, tl, di, dl, c, cls) == 0);
}

// What the dump tool wrote is all there: listed, present, the same sizes. Its list says nothing of
// what the dump tool did not get, so it is never "complete": the source says which list it was.
MP_TEST(dump_index_rule) {
    static const irow rows[] = {{"eboot.bin", 10},          {"data/a.bin", 20},     {"Data/B.bin", 30},
                                {"sce_sys/param.json", 5},  {"decrypted/eboot.bin", 10},
                                // what a fine dump differs in: never counted
                                {"Notes/info.nfo", 4000}, {"fakelib/libScePlayGo.sprx", 900},
                                {"Media/Game/Saved/Config/PS5/Engine.ini", 20}, {"fakelib/gone.sprx", 1},
                                {"Media/Saved/Logs/game.log", 3}};
    size_t dl = 0, n = sizeof rows / sizeof *rows;
    uint8_t *di = index_of(rows, n, &dl), cls[32];
    listing l = {0};
    add(&l, "eboot.bin", 10);
    add(&l, "data/a.bin", 20);
    add(&l, "data/b.bin", 30);   // another case: the same file for the runtime
    add(&l, "sce_sys/param.json", 5);
    add(&l, "decrypted/eboot.bin", 10);
    add(&l, "fakelib/libScePlayGo.sprx", 1200);   // replaced since
    add(&l, "Media/Game/Saved/Config/PS5/Engine.ini", 33);   // written while it ran
    add(&l, "Media/Saved/Logs/game.log", 99);
    add(&l, "Notes/other.nfo", 50);   // not listed
    add(&l, "fakelib/libSceAgc.sprx", 70);   // a backport added since
    add(&l, "ampr_emu.index", dl);
    add(&l, "Thumbs.db", 1);
    mp_complete c;
    check_di(&l, NULL, 0, di, dl, &c, cls);
    MP_CHECK(c.checkable);
    MP_CHECK_STR(c.source, "dump_index");
    MP_CHECK_STR(c.reason, "");
    MP_CHECK_EQ_U64(c.listed, 5);
    MP_CHECK_EQ_U64(c.present, 5);
    MP_CHECK_EQ_U64(c.missing, 0);
    MP_CHECK_EQ_U64(c.size_mismatch, 0);
    // what the list does not have: an .nfo and the backport (informative)
    MP_CHECK_EQ_U64(c.extra_count, 2);
    MP_CHECK_EQ_U64(c.extra_bytes, 120);
    // a leftover by its name, which this list does not prove is not the game's
    MP_CHECK_EQ_U64(c.leftover_count, 1);
    MP_CHECK(class_of(&l, cls, "decrypted/eboot.bin") == MP_CF_LEFTOVER);
    MP_CHECK(class_of(&l, cls, "data/b.bin") == MP_CF_LISTED);
    MP_CHECK(class_of(&l, cls, "Thumbs.db") == MP_CF_IGNORED);
    char line[256];
    mp_complete_summary(&c, line, sizeof line);
    MP_CHECK_STR(line, "all 5 files the dump tool wrote are here (its list does not prove it got every file of the game)");
    MP_CHECK(!strstr(line, "complete"));
    mp_jbuf b = {0};
    mp_complete_json(&c, 0, &b);
    MP_CHECK_CONTAINS(b.p, "\"source\":\"dump_index\"");
    MP_CHECK_CONTAINS(b.p, "\"missing\":0,\"size_mismatch\":0");
    mp_jbuf_free(&b);
    mp_complete_free(&c);

    // a copy cut short: files gone, one shorter than the dump tool wrote it
    listing m = {0};
    add(&m, "eboot.bin", 10);
    add(&m, "data/a.bin", 7);
    add(&m, "decrypted/eboot.bin", 10);
    check_di(&m, NULL, 0, di, dl, &c, cls);
    MP_CHECK(c.checkable);
    MP_CHECK_EQ_U64(c.listed, 5);
    MP_CHECK_EQ_U64(c.present, 2);
    MP_CHECK_EQ_U64(c.missing, 2);
    MP_CHECK_EQ_U64(c.size_mismatch, 1);
    mp_complete_summary(&c, line, sizeof line);
    MP_CHECK_STR(line, "damaged copy: 2 of the 5 files the dump tool wrote are missing, 1 has another size");
    mp_complete_free(&c);
    free(di);
}

// A list that cannot be trusted is not used: one of another game (nothing of it is here), one of
// another version (many sizes differ), a damaged one, one whose files are in AMPRPAK4 packs.
MP_TEST(dump_index_not_trusted) {
    irow rows[20];
    char names[20][16];
    for (int i = 0; i < 20; i++) {
        snprintf(names[i], sizeof names[i], "d/%02d.bin", i);
        rows[i].path = names[i];
        rows[i].size = 100;
    }
    size_t dl = 0;
    uint8_t *di = index_of(rows, 20, &dl), cls[32];
    listing l = {0};
    add(&l, "eboot.bin", 1);
    add(&l, "other.bin", 1);
    mp_complete c;
    check_di(&l, NULL, 0, di, dl, &c, cls);
    MP_CHECK(!c.checkable);
    MP_CHECK_STR(c.reason, "dump_index_mismatch");
    MP_CHECK_STR(c.source, "");
    mp_complete_free(&c);
    // another version: most files there, of other sizes
    listing v = {0};
    for (int i = 0; i < 20; i++) add(&v, names[i], i < 9 ? 100 : 101);
    check_di(&v, NULL, 0, di, dl, &c, cls);
    MP_CHECK(!c.checkable);
    MP_CHECK_STR(c.reason, "dump_index_mismatch");
    mp_complete_free(&c);
    // a few cut files of a copy are still told
    listing w = {0};
    for (int i = 0; i < 20; i++) add(&w, names[i], i < 3 ? 50 : 100);
    check_di(&w, NULL, 0, di, dl, &c, cls);
    MP_CHECK(c.checkable);
    MP_CHECK_EQ_U64(c.size_mismatch, 3);
    mp_complete_free(&c);
    // its files in AMPRPAK4 packs: not there loose, so not checked
    listing pk = {0};
    for (int i = 0; i < 2; i++) add(&pk, names[i], 100);
    add(&pk, "ampr_assets.index", 64);
    check_di(&pk, NULL, 0, di, dl, &c, cls);
    MP_CHECK(!c.checkable);
    MP_CHECK_STR(c.reason, "dump_index_packed");
    mp_complete_free(&c);
    // damaged lists: cut, of another magic or version, absurd counts, paths out of the blob or not
    // under /app0/
    uint8_t *bad = malloc(dl);
    struct { size_t at; uint32_t v; int n; } muts[] = {
        {8, 4, 1},   {12, 16, 1}, {16, 0, 1}, {16, 0xFFFFFFFFu, 1}, {24, 0x7FFFFFFFu, 1},
        {48, 0x7FFFFFFFu, 1},   // a path that starts beyond the blob
        {52, 0x7FFFFFFFu, 1},   // and one that ends beyond it
    };
    for (size_t k = 0; k < sizeof muts / sizeof *muts; k++) {
        memcpy(bad, di, dl);
        put32(bad + muts[k].at, muts[k].v);
        check_di(&w, NULL, 0, bad, dl, &c, cls);
        if (c.checkable || strcmp(c.reason, "dump_index_damaged")) MP_FAIL_("mutation %zu: %s", k, c.reason);
        mp_complete_free(&c);
    }
    memcpy(bad, di, dl);
    bad[0] = 'X';
    check_di(&w, NULL, 0, bad, dl, &c, cls);
    MP_CHECK_STR(c.reason, "dump_index_damaged");
    mp_complete_free(&c);
    memcpy(bad, di, dl);
    memcpy(bad + 48 + 24 * 20, "/xxx0/", 6);
    check_di(&w, NULL, 0, bad, dl, &c, cls);
    MP_CHECK_STR(c.reason, "dump_index_damaged");
    mp_complete_free(&c);
    for (size_t cut = 0; cut < 48 + 24 * 20 + 10; cut += 7) {
        check_di(&w, NULL, 0, di, cut, &c, cls);
        if (c.checkable || strcmp(c.reason, "dump_index_damaged")) MP_FAIL_("cut at %zu: %s", cut, c.reason);
        mp_complete_free(&c);
    }
    free(bad);
    free(di);
}

// The PlayGo table wins: it is the original package's. A table of another format falls back to the
// dump tool's list.
MP_TEST(dump_index_and_table) {
    static const irow rows[] = {{"eboot.bin", 10}, {"data/a.bin", 20}, {"data/never.bin", 5}};
    static const char *const game[] = {"eboot.bin", "data/a.bin"};
    size_t dl = 0, tl = 0;
    uint8_t *di = index_of(rows, 3, &dl), *t = table_of_paths(game, 2, &tl), cls[32];
    listing l = {0};
    add(&l, "eboot.bin", 10);
    add(&l, "data/a.bin", 20);
    mp_complete c;
    check_di(&l, t, tl, di, dl, &c, cls);
    MP_CHECK_STR(c.source, "playgo");
    MP_CHECK_EQ_U64(c.missing, 0);
    mp_complete_free(&c);
    const uint8_t alien[64] = {1};
    check_di(&l, alien, sizeof alien, di, dl, &c, cls);
    MP_CHECK_STR(c.source, "dump_index");
    MP_CHECK_EQ_U64(c.missing, 1);
    mp_complete_free(&c);
    // neither: why, of the list that is there
    check_di(&l, NULL, 0, NULL, 0, &c, cls);
    MP_CHECK_STR(c.reason, "no_table");
    MP_CHECK_STR(c.source, "");
    mp_complete_free(&c);
    free(di);
    free(t);
}

MP_TEST(dump_index_fixtures) {
    mp_complete c;
    source_check(DI "/ok", &c);
    MP_CHECK(c.checkable);
    MP_CHECK_STR(c.source, "dump_index");
    MP_CHECK_EQ_U64(c.listed, 13);
    MP_CHECK_EQ_U64(c.present, 13);
    MP_CHECK_EQ_U64(c.missing + c.size_mismatch, 0);
    MP_CHECK_EQ_U64(c.extra_count, 2);
    MP_CHECK_EQ_U64(c.leftover_count, 1);
    mp_complete_free(&c);
    source_check(DI "/broken", &c);
    MP_CHECK(c.checkable);
    MP_CHECK_EQ_U64(c.missing, 2);
    MP_CHECK_EQ_U64(c.size_mismatch, 1);
    MP_CHECK_EQ_U64(c.present, 10);
    mp_complete_free(&c);
    source_check(DI "/foreign", &c);
    MP_CHECK_STR(c.reason, "dump_index_mismatch");
    mp_complete_free(&c);
    source_check(DI "/damaged", &c);
    MP_CHECK_STR(c.reason, "dump_index_damaged");
    mp_complete_free(&c);
    source_check(DI "/packed", &c);
    MP_CHECK_STR(c.reason, "dump_index_packed");
    mp_complete_free(&c);
    source_check(DI "/both", &c);
    MP_CHECK_STR(c.source, "playgo");
    MP_CHECK_EQ_U64(c.missing, 0);
    mp_complete_free(&c);
    source_check(DI "/fallback", &c);
    MP_CHECK_STR(c.source, "dump_index");
    MP_CHECK_EQ_U64(c.missing, 2);
    mp_complete_free(&c);
}

// A .p5z keeps the dump tool's list (the original ampr_emu.index): it says the same as the dump.
// The leftovers this list does not prove stay in. A compressed folder has MaxPack's own list, which
// says nothing of the dump tool's copy: it is not used.
MP_TEST(dump_index_packed_and_p5z) {
    mp_err e = {{0}};
    mp_pack_result r;
    mp_complete c;
    pack_with(DI "/broken", TMP "/di.p5z", 1, &r);
    MP_CHECK_STR(r.complete.source, "dump_index");
    MP_CHECK_EQ_U64(r.complete.missing, 2);
    MP_CHECK_EQ_U64(r.left_out_files, 0);
    MP_CHECK(mp_describe_complete(TMP "/di.p5z", &c, &e) == 0);
    MP_CHECK_STR(c.source, "dump_index");
    MP_CHECK_EQ_U64(c.missing, 2);
    MP_CHECK_EQ_U64(c.size_mismatch, 1);
    MP_CHECK_EQ_U64(c.present, 10);
    mp_complete_free(&c);
    mpt_clear(TMP "/didir");
    mp_pack_out po;
    if (mp_pack_out_dir(TMP "/didir", &po, &e) == 0) {
        mp_pack_opts o;
        mp_pack_opts_default(&o);
        o.threads = 2;
        o.progress = 0;
        o.level = 3;
        MP_CHECK(mp_pack_run(DI "/ok", &po, &o, &r, &e) == 0);
        po.destroy(po.ctx);
        MP_CHECK(mp_describe_complete(TMP "/didir", &c, &e) == 0);
        MP_CHECK(!c.checkable);
        MP_CHECK_STR(c.reason, "no_table");
        mp_complete_free(&c);
    }
}

int main(void) {
    MP_RUN(hash_known_answers);
    MP_RUN(hash_case_and_slash);
    MP_RUN(table_reader);
    MP_RUN(rule_complete_and_entries);
    MP_RUN(rule_missing_extra_junk);
    MP_RUN(rule_listed_is_never_leftover);
    MP_RUN(rule_without_table);
    MP_RUN(rule_many_extra_paths);
    MP_RUN(text_lines);
    MP_RUN(fixtures);
    MP_RUN(pack_leaves_out_proven_leftovers);
    MP_RUN(describe_says_it);
    MP_RUN(dump_index_rule);
    MP_RUN(dump_index_not_trusted);
    MP_RUN(dump_index_and_table);
    MP_RUN(dump_index_fixtures);
    MP_RUN(dump_index_packed_and_p5z);
    MP_TEST_MAIN_END();
}
