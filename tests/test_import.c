// SPDX-License-Identifier: GPL-3.0-or-later
// A game packed already with zstd packs (as Dump Forge, Alembic or pack --folder make it) packed into a
// .p5z: compressed again it gives the .p5z of the dump it was made of, byte for byte, whatever the tool
// left loose or packed and whatever the thread count; and what is damaged, missing or not zstd is refused
// with its code, nothing left of the .p5z and nothing written to the source.
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <zstd.h>

#include "mp_fixture.h"
#include "mp_hash.h"
#include "mp_idx3.h"
#include "mp_import.h"
#include "mp_lz4pack.h"
#include "mp_measure.h"
#include "mp_source.h"
#include "mp_zpk.h"

#define TMP MP_TEST_TMP "/import"
#define LIMIT_MIN 262208u   // one uncompressed 256 KiB block per volume: several volumes

static unsigned all_threads(void) {
    unsigned n = mp_cpu_count();
    return n > 2 ? n : 5;
}

static int pack_ex(const char *src, const char *dst, int p5z, int level, unsigned threads, uint64_t limit, int keep,
                   mp_pack_result *r, mp_err *e) {
    mpt_clear(dst);
    mpt_parent_dirs(dst);
    mp_pack_opts o;
    mp_pack_opts_default(&o);
    o.threads = threads;
    o.progress = 0;
    o.level = level;
    o.volume_limit = limit;
    o.keep_packs = keep;
    mp_pack_out out;
    if ((p5z ? mp_pack_out_p5z(dst, threads, &out, e) : mp_pack_out_dir(dst, &out, e)) != 0) return -1;
    int rc = mp_pack_run(src, &out, &o, r, e);
    out.destroy(out.ctx);
    return rc;
}

static int same_bytes(const char *a, const char *b) {
    size_t la = 0, lb = 0;
    uint8_t *x = mpt_slurp(a, &la), *y = mpt_slurp(b, &lb);
    int same = x && y && la == lb && memcmp(x, y, la) == 0;
    free(x);
    free(y);
    return same;
}

// SHA-256 of a folder: each file's path, size, date and bytes, in path order.
static void hash_tree(const char *dir, uint8_t out[32]) {
    mpt_list l = mpt_list_dir(dir);
    mp_sha256 s;
    mp_sha256_init(&s);
    for (size_t i = 0; i < l.n; i++) {
        char p[2048];
        snprintf(p, sizeof p, "%s/%s", dir, l.v[i]);
        size_t n = 0;
        int64_t mtime = 0;
        mp_stat(p, NULL, &mtime);
        uint8_t *b = mpt_slurp(p, &n);
        mp_sha256_update(&s, l.v[i], strlen(l.v[i]) + 1);
        mp_sha256_update(&s, &n, sizeof n);
        mp_sha256_update(&s, &mtime, sizeof mtime);
        if (b) mp_sha256_update(&s, b, n);
        free(b);
    }
    mpt_list_free(&l);
    mp_sha256_final(&s, out);
}

static int copy_cb(void *ctx, const char *rel, int is_dir, uint64_t size, int64_t mtime) {
    const char *const *dirs = ctx;
    (void)size;
    if (is_dir) return 0;
    char a[2048], b[2048];
    snprintf(a, sizeof a, "%s/%s", dirs[0], rel);
    snprintf(b, sizeof b, "%s/%s", dirs[1], rel);
    mpt_copy_file(a, b);
    mp_set_mtime(b, mtime, NULL);
    return 0;
}

// A copy of a folder, dates kept.
static void copy_tree(const char *from, const char *to) {
    mpt_clear(to);
    const char *dirs[2] = {from, to};
    mp_err e = {{0}};
    if (mp_dir_walk(from, copy_cb, (void *)dirs, &e) != 0) MP_FAIL_("copy %s: %s", from, e.msg);
}

static void flip_byte(const char *path, uint64_t off) {
    size_t n = 0;
    uint8_t *b = mpt_slurp(path, &n);
    if (!b || off >= n) {
        MP_FAIL_("cannot change byte %llu of %s", (unsigned long long)off, path);
        free(b);
        return;
    }
    b[off] ^= 0x5A;
    mpt_put(path, b, n);
    free(b);
}

// ---- the packed games ------------------------------------------------------------------------

#define A19 TMP "/a19.p5z"        // the dump packed at level 19
#define FOLDER TMP "/folder"      // the dump packed to a folder at level 19, in several volumes

static int ready = 0;

static void make_reference(void) {
    if (ready) return;
    ready = 1;
    mp_pack_result r;
    mp_err e = {{0}};
    if (pack_ex(MPT_BASIC, A19, 1, 19, 4, MP_ZPK_VOLUME_LIMIT, 0, &r, &e) != 0) MP_FAIL_("pack: %s", e.msg);
    if (pack_ex(MPT_BASIC, FOLDER, 0, 19, 4, LIMIT_MIN, 0, &r, &e) != 0) MP_FAIL_("pack --folder: %s", e.msg);
    if (r.volumes < 4) MP_FAIL_("only %llu volumes", (unsigned long long)r.volumes);
}

// Whether a file goes into the packs of the folder another tool makes.
typedef int (*packs_fn)(const char *path, uint64_t size);

static const char foreign_rt[] = "another tool's libSceAmpr.sprx, AMPRZPK1";

// A packed folder as another tool may make it, of the files of src: the ones in_packs takes go into the packs
// at that level, the others stay loose, and ampr_emu.index lists them all (but unlisted, when not NULL) with
// their sizes and dates; the tool's own runtime (in the packs with rt_packed) and the runtime's files are
// there too, as the console leaves them.
static int foreign_ex(const char *src_path, const char *dir, int level, packs_fn in_packs, const char *unlisted,
                      int rt_packed) {
    mp_err e = {{0}};
    mp_source *src = NULL;
    mpt_clear(dir);
    if (mp_source_open(src_path, &src, &e) != 0 || mp_make_dirs(dir, &e) != 0) {
        MP_FAIL_("%s", e.msg);
        mp_source_close(src);
        return -1;
    }
    if (rt_packed) {   // the runtime as a file of the source, to pack it
        mp_source *m = NULL;
        if (mp_source_merge(src, NULL, NULL, 0, NULL, "fakelib/libSceAmpr.sprx", (const uint8_t *)foreign_rt,
                            sizeof foreign_rt - 1, 1600000000, &m, &e) != 0) {
            MP_FAIL_("%s", e.msg);
            return -1;
        }
        src = m;
    }
    size_t n = mp_source_count(src), np = 0, nrows = 0;
    uint32_t *files = malloc(n * sizeof *files);
    mp_idx3_row *rows = calloc(n + 1, sizeof *rows);
    uint8_t (*sha)[32] = malloc(n * sizeof *sha);
    int rc = -1;
    for (size_t i = 0; i < n; i++) {
        const mp_entry *en = mp_source_entry(src, i);
        if (!strcmp(en->path, "fakelib/libSceAmpr.sprx")) {
            files[np++] = (uint32_t)i;
            continue;   // its row is below
        }
        if (en->size && in_packs(en->path, en->size)) {
            files[np++] = (uint32_t)i;
        } else {
            uint8_t *b = malloc((size_t)en->size + 1);
            char p[2048];
            snprintf(p, sizeof p, "%s/%s", dir, en->path);
            if (mp_source_read(src, i, 0, b, (size_t)en->size, &e) != 0) MP_FAIL_("%s", e.msg);
            mpt_put(p, b, (size_t)en->size);
            mp_set_mtime(p, en->mtime, NULL);
            free(b);
        }
        if (unlisted && !strcmp(en->path, unlisted)) continue;
        rows[nrows].path = en->path;
        rows[nrows].size = en->size;
        rows[nrows].mtime = en->mtime;
        nrows++;
    }
    rows[nrows].path = "fakelib/libSceAmpr.sprx";
    rows[nrows].size = sizeof foreign_rt - 1;
    rows[nrows].mtime = 1600000000;
    nrows++;
    char p[2048];
    snprintf(p, sizeof p, "%s/fakelib/libSceAmpr.sprx", dir);
    if (!rt_packed) mpt_put(p, foreign_rt, sizeof foreign_rt - 1);
    snprintf(p, sizeof p, "%s/ampr_commands.bin", dir);
    mpt_put_text(p, "commands the runtime logged");
    snprintf(p, sizeof p, "%s/apr_emu.log", dir);
    mpt_put_text(p, "the runtime's log");
    mp_zpk_sink sink;
    mp_zpk_opts zo = {level, 2, 1000000, NULL, 0, NULL, NULL};
    uint8_t *zidx = NULL, *idx3 = NULL;
    size_t zlen = 0, ilen = 0;
    mp_zpk_stats st;
    if (mp_zpk_sink_dir(dir, &sink, &e) != 0 ||
        mp_zpk_write(src, files, np, &zo, &sink, &zidx, &zlen, sha, &st, &e) != 0 ||
        mp_idx3_build(rows, nrows, &idx3, &ilen, &e) != 0) {
        MP_FAIL_("%s", e.msg);
    } else {
        snprintf(p, sizeof p, "%s/ampr_zstd.index", dir);
        mpt_put(p, zidx, zlen);
        snprintf(p, sizeof p, "%s/ampr_emu.index", dir);
        mpt_put(p, idx3, ilen);
        rc = 0;
    }
    mp_zpk_sink_free(&sink);
    free(zidx);
    free(idx3);
    free(files);
    free(rows);
    free(sha);
    mp_source_close(src);
    return rc;
}

static int foreign(const char *src_path, const char *dir, int level, packs_fn in_packs, const char *unlisted) {
    return foreign_ex(src_path, dir, level, in_packs, unlisted, 0);
}

// eboot.bin and param.json, which MaxPack leaves loose, in the packs; random.bin and data/big.bin, which it
// packs, loose.
static int odd_split(const char *path, uint64_t size) {
    mp_rules *r = mp_rules_new();
    int packable = mp_rules_classify(r, path, size) == MP_PACKABLE;
    mp_rules_free(r);
    if (!strcmp(path, "eboot.bin") || !strcmp(path, "sce_sys/param.json")) return 1;
    if (!strcmp(path, "random.bin") || !strcmp(path, "data/big.bin")) return 0;
    return packable;
}

// ---- compressed again -------------------------------------------------------------------------

// The dump packed to a folder and that folder packed again at the same level give the dump's .p5z.
MP_TEST(recompress_is_identical) {
    make_reference();
    unsigned threads[3] = {1, 2, all_threads()};
    for (int k = 0; k < 3; k++) {
        char out[512];
        snprintf(out, sizeof out, TMP "/b19-t%u.p5z", threads[k]);
        mp_pack_result r;
        mp_err e = {{0}};
        if (pack_ex(FOLDER, out, 1, 19, threads[k], MP_ZPK_VOLUME_LIMIT, 0, &r, &e) != 0) {
            MP_FAIL_("%u threads: %s", threads[k], e.msg);
            continue;
        }
        if (!same_bytes(A19, out)) MP_FAIL_("%u threads: the .p5z is not the dump's", threads[k]);
        MP_CHECK(r.from_packed == 1);
        MP_CHECK(r.packed_level == 19);
        MP_CHECK(r.kept_packs == 0);
        MP_CHECK(r.code == NULL);
        mpt_clear(out);
    }
}

// At another level and as a folder too: what the dump gives with the same options.
MP_TEST(recompress_other_level_and_folder) {
    make_reference();
    mp_pack_result r;
    mp_err e = {{0}};
    MP_CHECK(pack_ex(MPT_BASIC, TMP "/a3.p5z", 1, 3, 4, MP_ZPK_VOLUME_LIMIT, 0, &r, &e) == 0);
    MP_CHECK(r.from_packed == 0);
    MP_CHECK(pack_ex(FOLDER, TMP "/b3.p5z", 1, 3, 4, MP_ZPK_VOLUME_LIMIT, 0, &r, &e) == 0);
    MP_CHECK(same_bytes(TMP "/a3.p5z", TMP "/b3.p5z"));
    MP_CHECK(pack_ex(MPT_BASIC, TMP "/fa", 0, 9, 4, 1000000, 0, &r, &e) == 0);
    MP_CHECK(pack_ex(FOLDER, TMP "/fb", 0, 9, 4, 1000000, 0, &r, &e) == 0);
    // the volumes get the date they are written on: their bytes must be the same
    MP_CHECK(mpt_dirs_equal(TMP "/fa", TMP "/fb", "*.pak"));
    mpt_clear(TMP "/a3.p5z");
    mpt_clear(TMP "/b3.p5z");
    mpt_clear(TMP "/fa");
    mpt_clear(TMP "/fb");
}

// What another tool packed or left loose against MaxPack's rules, its runtime and the runtime's files do not
// change what comes out: the dump's .p5z.
MP_TEST(recompress_other_split) {
    make_reference();
    if (foreign(MPT_BASIC, TMP "/other", 7, odd_split, NULL) != 0) return;
    mp_pack_result r;
    mp_err e = {{0}};
    uint8_t before[32], after[32];
    hash_tree(TMP "/other", before);
    if (pack_ex(TMP "/other", TMP "/other.p5z", 1, 19, 3, MP_ZPK_VOLUME_LIMIT, 0, &r, &e) != 0) {
        MP_FAIL_("%s", e.msg);
        return;
    }
    MP_CHECK(same_bytes(A19, TMP "/other.p5z"));
    MP_CHECK(r.from_packed == 1);
    MP_CHECK(r.packed_level == 7);
    hash_tree(TMP "/other", after);
    MP_CHECK(memcmp(before, after, 32) == 0);   // nothing written to the source
    mpt_clear(TMP "/other.p5z");
}

// The source as the packer sees it: no AMPR file, no runtime, the packed files with the dates of
// ampr_emu.index, read decompressed.
MP_TEST(source_has_the_original_files) {
    make_reference();
    if (foreign(MPT_BASIC, TMP "/other", 7, odd_split, NULL) != 0) return;
    mp_source *a = NULL, *b = NULL;
    mp_import_info info;
    mp_err e = {{0}};
    MP_CHECK(mp_source_open(MPT_BASIC, &a, &e) == 0);
    MP_CHECK(mp_import_open(TMP "/other", MP_OPEN_GAME, &b, &info, &e) == 0);
    if (!a || !b) {
        MP_FAIL_("%s", e.msg);
        mp_source_close(a);
        mp_source_close(b);
        return;
    }
    MP_CHECK(info.packed == 1);
    MP_CHECK(info.level == 7);
    MP_CHECK(info.code == NULL);
    MP_CHECK(mp_import_zpk(b) != NULL);
    MP_CHECK(mp_import_zpk(a) == NULL);
    MP_CHECK_EQ_U64(mp_source_count(a), mp_source_count(b));
    uint8_t *x = malloc(1u << 20), *y = malloc(1u << 20);
    for (size_t i = 0; i < mp_source_count(a) && i < mp_source_count(b); i++) {
        const mp_entry *ea = mp_source_entry(a, i), *eb = mp_source_entry(b, i);
        MP_CHECK_STR(ea->path, eb->path);
        MP_CHECK_EQ_U64(ea->size, eb->size);
        MP_CHECK(ea->mtime == eb->mtime);
        if (ea->size > (1u << 20)) continue;
        MP_CHECK(mp_source_read(a, i, 0, x, (size_t)ea->size, &e) == 0);
        MP_CHECK(mp_source_read(b, i, 0, y, (size_t)eb->size, &e) == 0);
        if (memcmp(x, y, (size_t)ea->size)) MP_FAIL_("%s reads otherwise", ea->path);
    }
    free(x);
    free(y);
    mp_source_close(a);
    mp_source_close(b);
    // a dump is given as it is
    MP_CHECK(mp_import_open(MPT_BASIC, MP_OPEN_GAME, &a, &info, &e) == 0);
    MP_CHECK(info.packed == 0);
    MP_CHECK(mp_import_zpk(a) == NULL);
    mp_source_close(a);
    MP_CHECK(mp_import_left_out("ampr_zstd.index") && mp_import_left_out("AMPR_ZSTD-012.pak") &&
             mp_import_left_out("ampr_emu.index") && mp_import_left_out("ampr_commands.bin") &&
             mp_import_left_out("apr_emu.log") && mp_import_left_out("fakelib/libSceAmpr.sprx"));
    MP_CHECK(!mp_import_left_out("eboot.bin") && !mp_import_left_out("data/ampr_zstd.index") &&
             !mp_import_left_out("fakelib/other.sprx") && !mp_import_left_out("sub/ampr_emu.index"));
}

// ---- keeping the packs ------------------------------------------------------------------------

#define A5 TMP "/a5.p5z"          // the dump packed at level 5
#define F5 TMP "/folder5"         // and to a folder at level 5
#define A_ORIG TMP "/a-orig"      // the dump, from its .p5z

static int ready5 = 0;

static void make_level5(void) {
    make_reference();
    if (ready5) return;
    ready5 = 1;
    mp_pack_result r;
    mp_err e = {{0}};
    if (pack_ex(MPT_BASIC, A5, 1, 5, 4, MP_ZPK_VOLUME_LIMIT, 0, &r, &e) != 0) MP_FAIL_("pack: %s", e.msg);
    if (pack_ex(MPT_BASIC, F5, 0, 5, 4, 1000000, 0, &r, &e) != 0) MP_FAIL_("pack --folder: %s", e.msg);
    mp_p5z *p = NULL;
    mpt_clear(A_ORIG);
    if (mp_p5z_open(A19, &p, &e) != 0 || mp_unpack_original(p, A_ORIG, 4, 0, &e) != 0) MP_FAIL_("unpack: %s", e.msg);
    mp_p5z_close(p);
}

// The packs of a .p5z or of a folder.
typedef struct {
    mp_p5z *p;
    mp_zpk *z;
    mp_zpk_volsrc vs;
    uint8_t *idx;
} packs;

static int open_packs(const char *path, packs *k) {
    memset(k, 0, sizeof *k);
    mp_err e = {{0}};
    if (mp_path_kind(path) == 1) {
        if (mp_p5z_open(path, &k->p, &e) == 0 && mp_p5z_zpk(k->p, &k->z, &e) == 0) return 0;
    } else {
        char p[2048];
        size_t len = 0;
        snprintf(p, sizeof p, "%s/ampr_zstd.index", path);
        k->idx = mpt_slurp(p, &len);
        if (k->idx && mp_zpk_volsrc_dir(path, &k->vs, &e) == 0 && mp_zpk_open(k->idx, len, &k->vs, &k->z, &e) == 0) return 0;
    }
    MP_FAIL_("the packs of %s: %s", path, e.msg);
    return -1;
}

static void close_packs(packs *k) {
    mp_zpk_close(k->z);
    mp_p5z_close(k->p);
    if (k->vs.ctx) mp_zpk_volsrc_free(&k->vs);
    free(k->idx);
}

// Each chunk of path in a has the codec and the stored bytes of its chunk in b.
static int same_chunks(packs *a, packs *b, const char *path) {
    long i = mp_zpk_find(a->z, path), j = mp_zpk_find(b->z, path);
    if (i < 0 || j < 0) return 0;
    uint64_t na = 0, nb = 0;
    const uint64_t *ca = mp_zpk_file_chunks(a->z, (size_t)i, &na), *cb = mp_zpk_file_chunks(b->z, (size_t)j, &nb);
    uint8_t *x = malloc(1u << 18), *y = malloc(1u << 18);
    int same = x && y && na == nb;
    for (uint64_t k = 0; same && k < na; k++) {
        uint32_t sa = 0, sb = 0;
        int ka = 0, kb = 0;
        mp_err e = {{0}};
        mp_zpk_chunk(a->z, (size_t)i, (uint32_t)k, NULL, NULL, NULL, &ka);
        mp_zpk_chunk(b->z, (size_t)j, (uint32_t)k, NULL, NULL, NULL, &kb);
        same = ka == kb && mp_zpk_read_stored(a->z, ca[k], x, 1u << 18, &sa, &e) == 0 &&
               mp_zpk_read_stored(b->z, cb[k], y, 1u << 18, &sb, &e) == 0 && sa == sb && !memcmp(x, y, sa);
    }
    free(x);
    free(y);
    return same;
}

// What the .p5z holds: verify finds every file whole, and its original is the dump, dates too.
static void holds_the_dump(const char *p5z, int level) {
    mp_verify_result vr;
    mp_err e = {{0}};
    MP_CHECK(mp_verify_p5z(p5z, 4, 0, &vr, &e) == 0);
    MP_CHECK_EQ_U64(vr.files_bad, 0);
    MP_CHECK(vr.files_ok > 200);
    mp_p5z *p = NULL;
    if (mp_p5z_open(p5z, &p, &e) != 0) {
        MP_FAIL_("%s", e.msg);
        return;
    }
    MP_CHECK(mp_p5z_manifest(p)->level == level);
    mpt_clear(TMP "/k-orig");
    MP_CHECK(mp_unpack_original(p, TMP "/k-orig", 4, 0, &e) == 0);
    mp_p5z_close(p);
    MP_CHECK(mpt_dirs_equal(A_ORIG, TMP "/k-orig", NULL));
    mpt_clear(TMP "/k-orig");
}

// MaxPack's own packs at level 5 kept: the .p5z the dump gives at level 5, whatever level was asked.
MP_TEST(keep_packs_round_trip) {
    make_level5();
    mp_pack_result r;
    mp_err e = {{0}};
    if (pack_ex(F5, TMP "/k5.p5z", 1, 19, 3, MP_ZPK_VOLUME_LIMIT, 1, &r, &e) != 0) {
        MP_FAIL_("%s", e.msg);
        return;
    }
    MP_CHECK(r.from_packed == 1 && r.kept_packs == 1 && r.packed_level == 5);
    MP_CHECK(same_bytes(A5, TMP "/k5.p5z"));
    holds_the_dump(TMP "/k5.p5z", 5);
    packs a, b;
    if (open_packs(TMP "/k5.p5z", &a) == 0 && open_packs(F5, &b) == 0) {
        MP_CHECK_EQ_U64(mp_zpk_count(a.z), mp_zpk_count(b.z));
        for (size_t i = 0; i < mp_zpk_count(a.z); i++)
            if (!same_chunks(&a, &b, mp_zpk_path(a.z, i))) MP_FAIL_("the chunks of %s", mp_zpk_path(a.z, i));
    }
    close_packs(&a);
    close_packs(&b);
    // a dump has no packs to keep: it is compressed at the level asked
    MP_CHECK(pack_ex(MPT_BASIC, TMP "/k-dump.p5z", 1, 19, 3, MP_ZPK_VOLUME_LIMIT, 1, &r, &e) == 0);
    MP_CHECK(r.from_packed == 0 && r.kept_packs == 0);
    MP_CHECK(same_bytes(A19, TMP "/k-dump.p5z"));
    mpt_clear(TMP "/k5.p5z");
    mpt_clear(TMP "/k-dump.p5z");
}

// A sink that takes the volumes and keeps nothing.
static int null_vol(void *ctx, uint32_t vol, mp_err *e) {
    (void)ctx;
    (void)vol;
    (void)e;
    return 0;
}
static int null_write(void *ctx, const void *p, size_t n, mp_err *e) {
    (void)p;
    (void)e;
    *(uint64_t *)ctx += n;
    return 0;
}
static int null_header(void *ctx, uint32_t vol, const uint8_t hdr[64], mp_err *e) {
    (void)ctx;
    (void)vol;
    (void)hdr;
    (void)e;
    return 0;
}

// Its packs kept, each chunk is read from them once: the bytes copied are the very ones decoded and hashed
// (a second read could give others, and doubles what a slow disk reads).
MP_TEST(keep_packs_reads_each_chunk_once) {
    make_level5();
    mp_source *s = NULL;
    mp_import_info info;
    mp_err e = {{0}};
    if (mp_import_open(F5, MP_OPEN_GAME, &s, &info, &e) != 0) {
        MP_FAIL_("%s", e.msg);
        return;
    }
    mp_zpk *z = mp_import_zpk(s);
    size_t n = mp_source_count(s), np = 0;
    uint32_t *files = malloc(n * sizeof *files);
    long *keep = malloc(n * sizeof *keep);
    uint8_t (*sha)[32] = malloc(n * sizeof *sha);
    mp_rules *rules = mp_rules_new();
    for (size_t i = 0; i < n; i++) {
        const mp_entry *en = mp_source_entry(s, i);
        if (mp_rules_classify(rules, en->path, en->size) != MP_PACKABLE) continue;
        files[np] = (uint32_t)i;
        keep[np++] = mp_zpk_find(z, en->path);
    }
    mp_rules_free(rules);
    uint64_t before = mp_import_volume_bytes(s), written = 0;
    mp_zpk_sink sink = {null_vol, null_write, null_vol, null_header, &written};
    mp_zpk_opts zo = {5, 4, MP_ZPK_VOLUME_LIMIT, NULL, 0, z, keep, mp_import_take, s};
    uint8_t *index = NULL;
    size_t len = 0;
    mp_zpk_stats st;
    mp_import_keep(s, 1);
    MP_CHECK(mp_zpk_write(s, files, np, &zo, &sink, &index, &len, sha, &st, &e) == 0);
    mp_import_keep(s, 0);
    MP_CHECK_EQ_U64(mp_import_volume_bytes(s) - before, mp_zpk_stored_bytes(z));
    MP_CHECK_EQ_U64(written, mp_zpk_stored_bytes(z));
    free(index);
    free(files);
    free(keep);
    free(sha);
    mp_source_close(s);
}

// The level an index says (byte 17), with its header CRC made again.
static void relabel(const char *index, int level) {
    size_t n = 0;
    uint8_t *d = mpt_slurp(index, &n);
    if (!d || n < 128) {
        MP_FAIL_("cannot read %s", index);
        free(d);
        return;
    }
    d[17] = (uint8_t)level;
    uint8_t h[128];
    memcpy(h, d, 128);
    memset(h + 112, 0, 8);
    uint32_t crc = mp_crc32(h, 128, 0);
    for (int i = 0; i < 4; i++) d[116 + i] = (uint8_t)(crc >> (8 * i));
    mpt_put(index, d, n);
    free(d);
}

// Another tool's packs (chunks MaxPack would not write: level 1 under an index that says 5), and what it put
// in them or left loose against MaxPack's rules: its chunks are kept, eboot.bin and param.json come out of the
// packs to go loose, what it left loose that MaxPack packs is compressed at the packs' level, and the
// thread count changes nothing.
MP_TEST(keep_packs_of_another_tool) {
    make_level5();
    if (foreign(MPT_BASIC, TMP "/other1", 1, odd_split, NULL) != 0) return;
    relabel(TMP "/other1/ampr_zstd.index", 5);
    unsigned threads[2] = {1, all_threads()};
    for (int t = 0; t < 2; t++) {
        char out[512];
        snprintf(out, sizeof out, TMP "/k-other-t%u.p5z", threads[t]);
        mp_pack_result r;
        mp_err e = {{0}};
        if (pack_ex(TMP "/other1", out, 1, 19, threads[t], MP_ZPK_VOLUME_LIMIT, 1, &r, &e) != 0) {
            MP_FAIL_("%u threads: %s", threads[t], e.msg);
            return;
        }
        MP_CHECK(r.kept_packs == 1 && r.packed_level == 5);
    }
    char k1[512], kn[512];
    snprintf(k1, sizeof k1, TMP "/k-other-t%u.p5z", threads[0]);
    snprintf(kn, sizeof kn, TMP "/k-other-t%u.p5z", threads[1]);
    MP_CHECK(same_bytes(k1, kn));
    MP_CHECK(!same_bytes(k1, A5));   // the chunks are the other tool's
    holds_the_dump(k1, 5);
    packs k, o, a;
    if (open_packs(k1, &k) == 0 && open_packs(TMP "/other1", &o) == 0 && open_packs(A5, &a) == 0) {
        for (size_t i = 0; i < mp_zpk_count(o.z); i++) {
            const char *p = mp_zpk_path(o.z, i);
            if (!strcmp(p, "eboot.bin") || !strcmp(p, "sce_sys/param.json")) {
                MP_CHECK(mp_zpk_find(k.z, p) < 0);
            } else if (!same_chunks(&k, &o, p)) {
                MP_FAIL_("the chunks of %s are not the other tool's", p);
            }
        }
        MP_CHECK(same_chunks(&k, &a, "random.bin"));
        MP_CHECK(same_chunks(&k, &a, "data/big.bin"));
        MP_CHECK_EQ_U64(mp_zpk_count(k.z), mp_zpk_count(a.z));
        const mp_pack_manifest *m = mp_p5z_manifest(k.p);
        for (size_t i = 0; i < m->n; i++)
            if (!strcmp(m->v[i].path, "eboot.bin")) MP_CHECK(!(m->v[i].flags & MP_MANIFEST_PACKED));
    }
    close_packs(&k);
    close_packs(&o);
    close_packs(&a);
    mpt_clear(k1);
    mpt_clear(kn);
}

// ---- refused ----------------------------------------------------------------------------------

// Packs the broken copy and checks it is refused with that code, without a .p5z and without a write to it.
static void refused(const char *src, const char *code, const char *words) {
    uint8_t before[32], after[32];
    hash_tree(src, before);
    const char *out = TMP "/refused.p5z";
    for (int keep = 0; keep < 2; keep++) {
        mp_pack_result r;
        mp_err e = {{0}};
        MP_CHECK(pack_ex(src, out, 1, 19, 3, MP_ZPK_VOLUME_LIMIT, keep, &r, &e) == -1);
        MP_CHECK_STR(r.code, code);
        if (words) MP_CHECK_CONTAINS(e.msg, words);
        MP_CHECK(mp_path_kind(out) == 0);
    }
    hash_tree(src, after);
    MP_CHECK(memcmp(before, after, 32) == 0);
}

// The volume and place of a compressed block of the folder's packs.
static int compressed_block(const char *dir, uint32_t *vol, uint64_t *off, uint32_t *stored) {
    char p[2048];
    snprintf(p, sizeof p, "%s/ampr_zstd.index", dir);
    size_t len = 0;
    uint8_t *idx = mpt_slurp(p, &len);
    mp_zpk_volsrc vs;
    mp_zpk *z = NULL;
    mp_err e = {{0}};
    int found = 0;
    if (idx && mp_zpk_volsrc_dir(dir, &vs, &e) == 0) {
        if (mp_zpk_open(idx, len, &vs, &z, &e) == 0) {
            for (size_t i = 0; i < mp_zpk_count(z) && !found; i++) {
                int codec = 0;
                for (uint32_t k = 0; mp_zpk_chunk(z, i, k, vol, off, stored, &codec) == 0 && !found; k++) found = codec == 1;
            }
            mp_zpk_close(z);
        }
        mp_zpk_volsrc_free(&vs);
    }
    free(idx);
    if (!found) MP_FAIL_("no compressed block in %s: %s", dir, e.msg);
    return found ? 0 : -1;
}

MP_TEST(refuses_a_damaged_block) {
    make_reference();
    copy_tree(FOLDER, TMP "/damaged");
    uint32_t vol = 0, stored = 0;
    uint64_t off = 0;
    if (compressed_block(TMP "/damaged", &vol, &off, &stored) != 0) return;
    char p[512];
    snprintf(p, sizeof p, TMP "/damaged/ampr_zstd-%03u.pak", vol);
    flip_byte(p, off + stored / 2);
    refused(TMP "/damaged", MP_IMPORT_DAMAGED, NULL);
}

MP_TEST(refuses_a_damaged_index_or_volume) {
    make_reference();
    copy_tree(FOLDER, TMP "/damaged");
    flip_byte(TMP "/damaged/ampr_zstd.index", 200);
    refused(TMP "/damaged", MP_IMPORT_DAMAGED, "ampr_zstd.index");
    copy_tree(FOLDER, TMP "/damaged");
    flip_byte(TMP "/damaged/ampr_zstd-001.pak", 30);   // its header
    refused(TMP "/damaged", MP_IMPORT_DAMAGED, "ampr_zstd-001.pak");
    copy_tree(FOLDER, TMP "/damaged");
    mpt_clear(TMP "/damaged/ampr_zstd-002.pak");
    refused(TMP "/damaged", MP_IMPORT_DAMAGED, "ampr_zstd-002.pak");
    // volumes without their index
    copy_tree(FOLDER, TMP "/damaged");
    mpt_clear(TMP "/damaged/ampr_zstd.index");
    refused(TMP "/damaged", MP_IMPORT_DAMAGED, "ampr_zstd.index");
    // no ampr_emu.index: the dates of the packed files are not known
    copy_tree(FOLDER, TMP "/damaged");
    mpt_clear(TMP "/damaged/ampr_emu.index");
    refused(TMP "/damaged", MP_IMPORT_DAMAGED, "ampr_emu.index");
    mpt_clear(TMP "/damaged");
}

MP_TEST(refuses_a_missing_file) {
    make_reference();
    copy_tree(FOLDER, TMP "/missing");
    mpt_clear(TMP "/missing/eboot.bin");
    refused(TMP "/missing", MP_IMPORT_MISSING, "eboot.bin");
    mpt_clear(TMP "/missing");
}

// A packed file ampr_emu.index does not list: the console would not serve it, and its date is not known.
MP_TEST(refuses_an_unlisted_packed_file) {
    if (foreign(MPT_BASIC, TMP "/unlisted", 7, odd_split, "data/frag.bin") != 0) return;
    refused(TMP "/unlisted", MP_IMPORT_DAMAGED, "data/frag.bin");
    mpt_clear(TMP "/unlisted");
}

// An LZ4 manifest that is none (a header and nothing else) is damaged packs.
MP_TEST(refuses_lz4_packs) {
    mpt_clear(TMP "/lz4");
    mpt_put_text(TMP "/lz4/eboot.bin", "EBOOT");
    mpt_put_text(TMP "/lz4/sce_sys/param.json", "{\"titleId\":\"PPSA01234\"}");
    uint8_t head[64] = "AMPRPAK4";
    mpt_put(TMP "/lz4/ampr_assets.index", head, sizeof head);
    mpt_put(TMP "/lz4/ampr_assets-000.pak", head, sizeof head);
    refused(TMP "/lz4", MP_IMPORT_DAMAGED, "ampr_assets.index");
    mpt_clear(TMP "/lz4");
}

// A loose copy of a packed file (someone patching it) is no damage: it says what it is, with its own code.
MP_TEST(refuses_a_file_loose_and_packed) {
    make_reference();
    copy_tree(FOLDER, TMP "/twice");
    mpt_put_text(TMP "/twice/data/big.bin", "a loose copy of a packed file");
    refused(TMP "/twice", MP_IMPORT_TWICE, "data/big.bin");
    mpt_clear(TMP "/twice");
}

// A packed file whose path is a folder of loose files too would make a .p5z that verifies and cannot be
// unpacked (nor played: the indexes would hold it as a file and as a folder).
MP_TEST(refuses_a_file_that_is_a_folder) {
    make_reference();
    copy_tree(FOLDER, TMP "/filedir");
    mpt_put_text(TMP "/filedir/data/big.bin/child.txt", "a loose file under a packed one");
    refused(TMP "/filedir", MP_IMPORT_TWICE, "data/big.bin");
    mpt_clear(TMP "/filedir");
}

// A loose file of another size than its ampr_emu.index says: a copy cut short (the console would serve the
// size the index says).
MP_TEST(refuses_a_loose_file_cut_short) {
    make_reference();
    copy_tree(FOLDER, TMP "/short");
    size_t n = 0;
    uint8_t *b = mpt_slurp(TMP "/short/eboot.bin", &n);
    if (b && n > 100) mpt_put(TMP "/short/eboot.bin", b, n - 100);
    free(b);
    refused(TMP "/short", MP_IMPORT_MISSING, "eboot.bin");
    mpt_clear(TMP "/short");
}

// The other tool's runtime and the runtime's files inside the packs are left out as when they are loose.
MP_TEST(leaves_out_ampr_files_in_the_packs) {
    make_reference();
    if (foreign_ex(MPT_BASIC, TMP "/rtpacked", 7, odd_split, NULL, 1) != 0) return;
    for (int keep = 0; keep < 2; keep++) {
        mp_pack_result r;
        mp_err e = {{0}};
        if (pack_ex(TMP "/rtpacked", TMP "/rtpacked.p5z", 1, 19, 3, MP_ZPK_VOLUME_LIMIT, keep, &r, &e) != 0) {
            MP_FAIL_("keep %d: %s", keep, e.msg);
            continue;
        }
        if (!keep) MP_CHECK(same_bytes(A19, TMP "/rtpacked.p5z"));
        mp_p5z *p = NULL;
        if (mp_p5z_open(TMP "/rtpacked.p5z", &p, &e) == 0) {
            const mp_pack_manifest *m = mp_p5z_manifest(p);
            for (size_t i = 0; i < m->n; i++)
                if (mp_import_left_out(m->v[i].path)) MP_FAIL_("keep %d: %s went in", keep, m->v[i].path);
            mp_p5z_close(p);
        }
        mpt_clear(TMP "/rtpacked.p5z");
    }
    mpt_clear(TMP "/rtpacked");
}

// A chunk is one zstd frame: one with a skippable frame after it decodes, but is not what the runtime takes.
MP_TEST(refuses_a_chunk_of_two_frames) {
    if (foreign(MPT_BASIC, TMP "/frames", 1, odd_split, NULL) != 0) return;
    uint32_t vol = 0, stored = 0;
    uint64_t off = 0;
    if (compressed_block(TMP "/frames", &vol, &off, &stored) != 0) return;
    // the chunk's block again, at level 19 (smaller than level 1's), and a skippable frame for the room left
    char p[512];
    snprintf(p, sizeof p, TMP "/frames/ampr_zstd-%03u.pak", vol);
    size_t n = 0;
    uint8_t *v = mpt_slurp(p, &n), *plain = malloc(MP_BLOCK_V1), *again = malloc(ZSTD_compressBound(MP_BLOCK_V1));
    size_t got = v ? ZSTD_decompress(plain, MP_BLOCK_V1, v + off, stored) : 0;
    ZSTD_CCtx *c = ZSTD_createCCtx();
    size_t f = !ZSTD_isError(got) && c ? mp_zstd_block(c, 19, 18, plain, got, again, ZSTD_compressBound(MP_BLOCK_V1)) : 0;
    ZSTD_freeCCtx(c);
    if (!f || f + 8 > stored) {
        MP_FAIL_("no room for a second frame (%zu of %u)", f, stored);
    } else {
        memcpy(v + off, again, f);
        uint32_t room = stored - (uint32_t)f - 8;
        uint8_t skip[8] = {0x50, 0x2A, 0x4D, 0x18, (uint8_t)room, (uint8_t)(room >> 8), (uint8_t)(room >> 16), 0};
        memcpy(v + off + f, skip, 8);
        memset(v + off + f + 8, 0, room);
        mpt_put(p, v, n);
        refused(TMP "/frames", MP_IMPORT_DAMAGED, "zstd frame");
    }
    free(v);
    free(plain);
    free(again);
    mpt_clear(TMP "/frames");
}

// A loose copy beside the packs that is the packed file itself (a game packed in place with its files kept) is
// taken once, from the packs; one that differs, in a byte or in its size, is refused: keep only one.
MP_TEST(takes_a_loose_copy_the_same_as_the_packed_one) {
    make_reference();
    const char *dir = TMP "/zinplace";
    copy_tree(FOLDER, dir);
    mp_source *src = NULL;
    mp_err e = {{0}};
    long at = mp_source_open(MPT_BASIC, &src, &e) == 0 ? mp_source_find(src, "data/big.bin") : -1;
    const mp_entry *en = at >= 0 ? mp_source_entry(src, (size_t)at) : NULL;
    uint8_t *b = en ? malloc((size_t)en->size) : NULL;
    if (!b || mp_source_read(src, (size_t)at, 0, b, (size_t)en->size, &e) != 0) {
        MP_FAIL_("data/big.bin of the dump: %s", e.msg);
        free(b);
        mp_source_close(src);
        return;
    }
    mpt_put(TMP "/zinplace/data/big.bin", b, (size_t)en->size);
    mp_set_mtime(TMP "/zinplace/data/big.bin", en->mtime, NULL);
    for (int keep = 0; keep < 2; keep++) {
        mp_pack_result res;
        if (pack_ex(dir, TMP "/zinplace.p5z", 1, 19, 3, MP_ZPK_VOLUME_LIMIT, keep, &res, &e) != 0) {
            MP_FAIL_("keep %d: %s", keep, e.msg);
            continue;
        }
        if (!keep) MP_CHECK(same_bytes(A19, TMP "/zinplace.p5z"));
        mpt_clear(TMP "/zinplace.p5z");
    }
    b[1000] ^= 0x5A;   // a byte of it
    mpt_put(TMP "/zinplace/data/big.bin", b, (size_t)en->size);
    refused(dir, MP_IMPORT_TWICE, "differ");
    b[1000] ^= 0x5A;   // the same bytes, one fewer
    mpt_put(TMP "/zinplace/data/big.bin", b, (size_t)en->size - 1);
    refused(dir, MP_IMPORT_TWICE, "differ");
    free(b);
    mp_source_close(src);
    mpt_clear(dir);
}

// Cancelled in the middle of the packs: it says it was cancelled, not that the packs are damaged, and the .p5z
// goes.
static int cancel_calls;
static void cancel_soon(void *ctx, const char *phase, uint64_t done, uint64_t total) {
    (void)ctx;
    (void)done;
    if (!strncmp(phase, "Packing", 7) && total && ++cancel_calls == 1) mp_cancel_request();   // after its first block
}

MP_TEST(cancel_while_reading_the_packs) {
    make_reference();
    for (int keep = 0; keep < 2; keep++) {
        cancel_calls = 0;
        mp_progress_hook h = {cancel_soon, NULL};
        mp_progress_set_hook(&h);
        mp_pack_result r;
        mp_err e = {{0}};
        int rc = pack_ex(FOLDER, TMP "/cancelled.p5z", 1, 19, 2, MP_ZPK_VOLUME_LIMIT, keep, &r, &e);
        mp_progress_set_hook(NULL);
        int was = mp_cancelled();
        mp_cancel_reset();
        MP_CHECK(rc == -1 && was);
        MP_CHECK_CONTAINS(e.msg, "cancelled");
        MP_CHECK(r.code == NULL);
        MP_CHECK(mp_path_kind(TMP "/cancelled.p5z") == 0);
    }
}

// ---- LZ4 packs -------------------------------------------------------------------------------

#define LZ4F TMP "/lz4game"   // the dump in LZ4 packs: 3 packs of 4 KiB pages, LZ4, raw and shared chunks

static int lz4_ready = 0;

static void make_lz4(void) {
    make_reference();
    if (lz4_ready) return;
    lz4_ready = 1;
    mpt_clear(LZ4F);
    mpt_lz4_opts o = {3, 4096, mpt_lz4_odd, 1, 1, 0, 0, 0};
    if (mpt_lz4_pack(MPT_BASIC, LZ4F, &o) != 0) {
        MP_FAIL_("could not make the LZ4 packs");
        return;
    }
    // what the tests want of them
    if (!o.lz4_chunks || !o.raw_chunks || !o.shared_chunks)
        MP_FAIL_("%llu LZ4, %llu raw, %llu shared chunks", (unsigned long long)o.lz4_chunks,
                 (unsigned long long)o.raw_chunks, (unsigned long long)o.shared_chunks);
}

// The game in LZ4 packs, packed again: the dump's .p5z byte for byte, whatever the thread count, also asked
// to keep the packs (LZ4 chunks cannot go into a .p5z: it is compressed again and says so).
MP_TEST(lz4_recompress_is_identical) {
    make_lz4();
    unsigned threads[2] = {1, all_threads()};
    for (int k = 0; k < 2; k++)
        for (int keep = 0; keep < 2; keep++) {
            mp_pack_result r;
            mp_err e = {{0}};
            if (pack_ex(LZ4F, TMP "/lz4.p5z", 1, 19, threads[k], MP_ZPK_VOLUME_LIMIT, keep, &r, &e) != 0) {
                MP_FAIL_("%u threads, keep %d: %s", threads[k], keep, e.msg);
                continue;
            }
            if (!same_bytes(A19, TMP "/lz4.p5z")) MP_FAIL_("%u threads, keep %d: not the dump's .p5z", threads[k], keep);
            MP_CHECK(r.from_packed == 1 && r.kept_packs == 0 && r.packed_level == 0);
            MP_CHECK_STR(r.packs, "lz4");
            mpt_clear(TMP "/lz4.p5z");
        }
}

// As the packer sees it: the dump's files, sizes, dates and bytes; no AMPR file, no runtime.
MP_TEST(lz4_source_has_the_original_files) {
    make_lz4();
    mp_source *a = NULL, *b = NULL;
    mp_import_info info;
    mp_err e = {{0}};
    MP_CHECK(mp_source_open(MPT_BASIC, &a, &e) == 0);
    MP_CHECK(mp_import_open(LZ4F, MP_OPEN_GAME, &b, &info, &e) == 0);
    if (!a || !b) {
        MP_FAIL_("%s", e.msg);
        mp_source_close(a);
        mp_source_close(b);
        return;
    }
    MP_CHECK(info.packed == 1 && info.level == 0 && info.volumes == 3);
    MP_CHECK_STR(info.packs, "lz4");
    MP_CHECK(mp_import_zpk(b) == NULL);   // nothing to keep
    MP_CHECK_EQ_U64(mp_source_count(a), mp_source_count(b));
    uint8_t *x = malloc(1u << 20), *y = malloc(1u << 20);
    for (size_t i = 0; i < mp_source_count(a) && i < mp_source_count(b); i++) {
        const mp_entry *ea = mp_source_entry(a, i), *eb = mp_source_entry(b, i);
        MP_CHECK_STR(ea->path, eb->path);
        MP_CHECK_EQ_U64(ea->size, eb->size);
        MP_CHECK(ea->mtime == eb->mtime);
        // a read across a chunk's end too
        size_t n = ea->size > (1u << 20) ? (1u << 20) : (size_t)ea->size, from = n > 70000 ? 70000 : 0;
        MP_CHECK(mp_source_read(a, i, from, x, n - from, &e) == 0);
        MP_CHECK(mp_source_read(b, i, from, y, n - from, &e) == 0);
        if (memcmp(x, y, n - from)) MP_FAIL_("%s reads otherwise", ea->path);
    }
    free(x);
    free(y);
    mp_source_close(a);
    mp_source_close(b);
    MP_CHECK(mp_import_left_out("ampr_assets.index") && mp_import_left_out("ampr_assets.index.crc") &&
             mp_import_left_out("AMPR_ASSETS.INDEX.RUNTIME") && mp_import_left_out("ampr_assets-002.pak"));
}

static uint8_t *memmem_simple(uint8_t *h, size_t n, const char *needle) {
    size_t k = strlen(needle);
    for (size_t i = 0; k && i + k <= n; i++)
        if (!memcmp(h + i, needle, k)) return h + i;
    return NULL;
}

// The manifest of a copy, its record of path changed in place (to what of the same length) and sealed again.
static void lz4_rename(const char *dir, const char *from, const char *to) {
    char p[512];
    snprintf(p, sizeof p, "%s/ampr_assets.index", dir);
    size_t n = 0;
    uint8_t *d = mpt_slurp(p, &n);
    uint8_t *at = d ? memmem_simple(d, n, from) : NULL;
    if (!at || strlen(from) != strlen(to)) {
        MP_FAIL_("no %s in the manifest", from);
    } else {
        memcpy(at, to, strlen(to));
        mpt_put(p, d, n);
        if (mpt_lz4_reseal(p) != 0) MP_FAIL_("reseal");
    }
    free(d);
}

// A chunk record of the manifest of a copy pointed past its pack, and sealed again.
static void lz4_chunk_out(const char *dir) {
    char p[512];
    snprintf(p, sizeof p, "%s/ampr_assets.index", dir);
    size_t n = 0;
    uint8_t *d = mpt_slurp(p, &n);
    if (!d || n < MP_LZ4PK_HEADER) {
        MP_FAIL_("no manifest");
        free(d);
        return;
    }
    uint64_t chunks = mpt_r64(d + 80);
    uint64_t loc = mpt_r64(d + chunks);
    mpt_w64(d + chunks, (loc & 0xFFFF000000000000ull) | (64ull << 20));   // 64 MiB into its pack
    mpt_put(p, d, n);
    if (mpt_lz4_reseal(p) != 0) MP_FAIL_("reseal");
    free(d);
}

// The first chunk of that codec: its pack and place, from the manifest.
static int lz4_chunk_of(const char *dir, int codec, uint32_t *pack, uint64_t *off, uint32_t *stored) {
    char p[512];
    snprintf(p, sizeof p, "%s/ampr_assets.index", dir);
    size_t n = 0;
    uint8_t *d = mpt_slurp(p, &n);
    int found = 0;
    for (uint64_t k = 0; d && k < mpt_r64(d + 48) && !found; k++) {
        const uint8_t *c = d + mpt_r64(d + 80) + 12 * k;
        uint32_t desc = mpt_r32(c + 8);
        if ((int)((desc >> 20) & 3) != codec || desc & 1u << 22) continue;
        *off = mpt_r64(c) & ((1ull << 48) - 1);
        *pack = (uint32_t)(mpt_r64(c) >> 48);
        *stored = (desc & 0xFFFFF) + 1;
        found = 1;
    }
    free(d);
    if (!found) MP_FAIL_("no chunk of codec %d", codec);
    return found ? 0 : -1;
}

MP_TEST(lz4_refuses_damage) {
    make_lz4();
    const char *bad = TMP "/lz4bad";
    uint32_t pack = 0, stored = 0;
    uint64_t off = 0;
    char p[512];
    // a byte of an LZ4 chunk, and of a raw one (the CRC sidecar tells it)
    for (int codec = 1; codec >= 0; codec--) {
        copy_tree(LZ4F, bad);
        if (lz4_chunk_of(bad, codec, &pack, &off, &stored) != 0) return;
        snprintf(p, sizeof p, "%s/ampr_assets-%03u.pak", bad, pack);
        flip_byte(p, off + stored / 2);
        refused(bad, MP_IMPORT_DAMAGED, codec ? "chunk" : "CRC");
    }
    // the manifest's CRC, a volume's header, the CRC sidecar's, the runtime profile's
    const char *what[] = {"ampr_assets.index", "ampr_assets-001.pak", "ampr_assets.index.crc", "ampr_assets.index.runtime"};
    uint64_t at[] = {300, 30, 50, 40};
    for (int i = 0; i < 4; i++) {
        copy_tree(LZ4F, bad);
        snprintf(p, sizeof p, "%s/%s", bad, what[i]);
        flip_byte(p, at[i]);
        refused(bad, MP_IMPORT_DAMAGED, what[i]);
    }
    // a chunk past the end of its pack, with the manifest's CRCs right
    copy_tree(LZ4F, bad);
    lz4_chunk_out(bad);
    refused(bad, MP_IMPORT_DAMAGED, "chunk");
    // a volume missing
    copy_tree(LZ4F, bad);
    mpt_clear(TMP "/lz4bad/ampr_assets-002.pak");
    refused(bad, MP_IMPORT_DAMAGED, "ampr_assets-002.pak");
    mpt_clear(bad);
}

MP_TEST(lz4_refuses_tricky_paths) {
    make_lz4();
    const char *bad = TMP "/lz4path";
    // a path out of the game, and one that is not under /app0/
    const char *from[] = {"/app0/data/frag.bin", "/app0/data/frag.bin"};
    const char *to[] = {"/app0/../a/frag.bin", "/app9/data/frag.bin"};
    for (int i = 0; i < 2; i++) {
        copy_tree(LZ4F, bad);
        lz4_rename(bad, from[i], to[i]);
        refused(bad, MP_IMPORT_DAMAGED, "path");
    }
    // two records of one path, ASCII case aside
    copy_tree(LZ4F, bad);
    lz4_rename(bad, "/app0/many/f001.json", "/app0/MANY/F000.json");
    refused(bad, MP_IMPORT_DAMAGED, "twice");
    mpt_clear(bad);
}

MP_TEST(lz4_refuses_missing_and_twice) {
    make_lz4();
    const char *bad = TMP "/lz4miss";
    copy_tree(LZ4F, bad);
    mpt_clear(TMP "/lz4miss/random.bin");   // loose in the packs' game
    refused(bad, MP_IMPORT_MISSING, "random.bin");
    copy_tree(LZ4F, bad);
    mpt_put_text(TMP "/lz4miss/data/frag.bin", "a loose copy of a packed file");
    refused(bad, MP_IMPORT_TWICE, "data/frag.bin");
    copy_tree(LZ4F, bad);
    mpt_put_text(TMP "/lz4miss/data/frag.bin/child.txt", "a loose file under a packed one");
    refused(bad, MP_IMPORT_TWICE, "data/frag.bin");
    mpt_clear(bad);
}

// LZ4 packs of an older manifest (AMPRPAK3) are not read; nor a game with zstd and LZ4 packs at once.
MP_TEST(lz4_refuses_other_kinds) {
    make_lz4();
    const char *bad = TMP "/lz4old";
    copy_tree(LZ4F, bad);
    char p[512];
    snprintf(p, sizeof p, "%s/ampr_assets.index", bad);
    size_t n = 0;
    uint8_t *d = mpt_slurp(p, &n);
    if (d) {
        d[7] = '3';
        mpt_put(p, d, n);
    }
    free(d);
    refused(bad, MP_IMPORT_LZ4, "AMPRPAK3");
    copy_tree(LZ4F, bad);
    mpt_copy_file(FOLDER "/ampr_zstd.index", TMP "/lz4old/ampr_zstd.index");
    refused(bad, MP_IMPORT_LZ4, "zstd");
    mpt_clear(bad);
}

// The same for LZ4 packs, as `ampr_pack.py pack --root app0 --output app0` leaves a game: its packed files
// still loose beside the packs (64 KiB pages, streaming, random-access and hot files too).
MP_TEST(lz4_takes_a_game_packed_in_place) {
    make_reference();
    const char *dir = TMP "/lz4inplace", *bad = TMP "/lz4twin";
    mpt_clear(dir);
    mpt_lz4_opts o = {2, 65536, mpt_lz4_odd, 1, 0, 0, 0, 0, 1, 1, 0, 0};
    if (mpt_lz4_pack(MPT_BASIC, dir, &o) != 0) {
        MP_FAIL_("could not make the LZ4 packs");
        return;
    }
    for (int keep = 0; keep < 2; keep++) {
        mp_pack_result res;
        mp_err e = {{0}};
        if (pack_ex(dir, TMP "/lz4inplace.p5z", 1, 19, 3, MP_ZPK_VOLUME_LIMIT, keep, &res, &e) != 0) {
            MP_FAIL_("keep %d: %s", keep, e.msg);
            continue;
        }
        MP_CHECK(same_bytes(A19, TMP "/lz4inplace.p5z"));
        mpt_clear(TMP "/lz4inplace.p5z");
    }
    copy_tree(dir, bad);
    flip_byte(TMP "/lz4twin/data/frag.bin", 30000);
    refused(bad, MP_IMPORT_TWICE, "differ");
    copy_tree(dir, bad);
    mpt_put_text(TMP "/lz4twin/data/frag.bin", "a loose copy of another size");
    refused(bad, MP_IMPORT_TWICE, "differ");
    mpt_clear(bad);
    mpt_clear(dir);
}

#define LZ4G TMP "/lz4bare"   // without the CRC file and the runtime profile, as most are deployed

// LZ4 packs with no sidecar, 64 KiB pages, streaming, random-access and hot files and the tool's runtime in the
// packs: the dump's .p5z all the same, the runtime left out of what it counts; a damaged LZ4 chunk is still
// told, by its decoding.
MP_TEST(lz4_without_sidecars) {
    make_reference();
    mpt_clear(LZ4G);
    mpt_lz4_opts o = {2, 65536, mpt_lz4_odd, 0, 0, 0, 0, 0, 1, 0, 1, 0};
    if (mpt_lz4_pack(MPT_BASIC, LZ4G, &o) != 0) {
        MP_FAIL_("could not make the LZ4 packs");
        return;
    }
    if (!o.lz4_chunks || !o.raw_chunks || !o.shared_chunks || !o.streaming_chunks)
        MP_FAIL_("%llu LZ4, %llu raw, %llu shared, %llu streaming chunks", (unsigned long long)o.lz4_chunks,
                 (unsigned long long)o.raw_chunks, (unsigned long long)o.shared_chunks,
                 (unsigned long long)o.streaming_chunks);
    MP_CHECK(mp_path_kind(LZ4G "/" MP_LZ4PK_CRC) == 0 && mp_path_kind(LZ4G "/" MP_LZ4PK_RUNTIME) == 0);
    unsigned threads[2] = {1, all_threads()};
    for (int k = 0; k < 2; k++) {
        mp_pack_result res;
        mp_err e = {{0}};
        if (pack_ex(LZ4G, TMP "/lz4bare.p5z", 1, 19, threads[k], MP_ZPK_VOLUME_LIMIT, 0, &res, &e) != 0) {
            MP_FAIL_("%u threads: %s", threads[k], e.msg);
            continue;
        }
        MP_CHECK(same_bytes(A19, TMP "/lz4bare.p5z"));
        mpt_clear(TMP "/lz4bare.p5z");
    }
    // what it says it packs: the files of the game in the packs, the runtime that is there too not counted
    size_t n = 0;
    uint8_t *d = mpt_slurp(LZ4G "/" MP_LZ4PK_INDEX, &n);
    mp_lz4pk *l = NULL;
    mp_err e = {{0}};
    uint64_t files = 0, bytes = 0;
    if (d && mp_lz4pk_open(d, n, &l, NULL, &e) == 0) {
        for (size_t i = 0; i < mp_lz4pk_count(l); i++)
            if (mp_lz4pk_packed(l, i) && !mp_import_left_out(mp_lz4pk_path(l, i))) {
                files++;
                bytes += mp_lz4pk_size(l, i);
            }
        mp_lz4pk_close(l);
    } else {
        MP_FAIL_("%s", e.msg);
    }
    free(d);
    mp_source *g = NULL;
    mp_import_info info;
    if (mp_import_open(LZ4G, MP_OPEN_GAME, &g, &info, &e) == 0) {
        MP_CHECK_EQ_U64(info.packed_files, files);
        MP_CHECK_EQ_U64(info.packed_bytes, bytes);
        mp_source_close(g);
    } else {
        MP_FAIL_("%s", e.msg);
    }
    // an LZ4 chunk of zeros does not decode
    const char *bad = TMP "/lz4bare_bad";
    copy_tree(LZ4G, bad);
    uint32_t pack = 0, stored = 0;
    uint64_t off = 0;
    if (lz4_chunk_of(bad, 1, &pack, &off, &stored) == 0) {
        char p[512];
        snprintf(p, sizeof p, "%s/ampr_assets-%03u.pak", bad, pack);
        size_t vn = 0;
        uint8_t *v = mpt_slurp(p, &vn);
        if (v && off + stored <= vn) {
            memset(v + off, 0, stored);
            mpt_put(p, v, vn);
            refused(bad, MP_IMPORT_DAMAGED, "does not decode");
        } else {
            MP_FAIL_("no chunk at %llu", (unsigned long long)off);
        }
        free(v);
    }
    mpt_clear(bad);
}

MP_TEST(lz4_cancel_while_reading_the_packs) {
    make_lz4();
    cancel_calls = 0;
    mp_progress_hook h = {cancel_soon, NULL};
    mp_progress_set_hook(&h);
    mp_pack_result res;
    mp_err e = {{0}};
    int rc = pack_ex(LZ4F, TMP "/lz4cancelled.p5z", 1, 19, 2, MP_ZPK_VOLUME_LIMIT, 0, &res, &e);
    mp_progress_set_hook(NULL);
    int was = mp_cancelled();
    mp_cancel_reset();
    MP_CHECK(rc == -1 && was);
    MP_CHECK_CONTAINS(e.msg, "cancelled");
    MP_CHECK(res.code == NULL);
    MP_CHECK(mp_path_kind(TMP "/lz4cancelled.p5z") == 0);
}

// ---- LZ4 packs: the reader alone ------------------------------------------------------------

// A changed manifest is refused by the reader with those words in its reason (sealed again first, its file
// hashes made again from the paths when hashes is 1).
static void lz4_open_refused(uint8_t *d, size_t n, int hashes, const char *words) {
    mpt_lz4_seal(d, n, hashes);
    mp_lz4pk *l = NULL;
    mp_err e = {{0}};
    if (mp_lz4pk_open(d, n, &l, NULL, &e) == 0) {
        MP_FAIL_("taken, not refused for %s", words);
        mp_lz4pk_close(l);
        return;
    }
    MP_CHECK_CONTAINS(e.msg, words);
}

// The first file record that is packed with at least min chunks, the first of them of that codec (any when -1),
// or loose (min 0, codec -2).
static uint8_t *lz4_record_of(uint8_t *d, uint32_t min, int codec) {
    uint64_t nf = mpt_r64(d + 40), chunks = mpt_r64(d + 80);
    for (uint64_t i = 0; i < nf; i++) {
        uint8_t *rec = d + MP_LZ4PK_HEADER + 48 * i;
        uint32_t flags = mpt_r32(rec + 40), first = mpt_r32(rec + 24), count = mpt_r32(rec + 28);
        if (codec == -2) {
            if (!(flags & 1)) return rec;
            continue;
        }
        if (!(flags & 1) || count < min || (flags & 6)) continue;   // packed, neither stored nor streaming
        if (codec >= 0 && (int)((mpt_r32(d + chunks + 12 * first + 8) >> 20) & 3) != codec) continue;
        return rec;
    }
    MP_FAIL_("no such record");
    return NULL;
}

// Each rule of the manifest, broken in a copy of a right one: refused, with its reason.
MP_TEST(lz4_reader_refusals) {
    make_lz4();
    size_t n = 0;
    uint8_t *orig = mpt_slurp(LZ4F "/" MP_LZ4PK_INDEX, &n), *d = orig ? malloc(n) : NULL, *rec = NULL;
    if (!d) {
        MP_FAIL_("no manifest");
        free(orig);
        return;
    }
    uint64_t chunks = mpt_r64(orig + 80), packs = 0, strings = 0;
    // a path hash that is not its path's
    memcpy(d, orig, n);
    if ((rec = lz4_record_of(d, 1, -1))) {
        rec[0] ^= 1;
        lz4_open_refused(d, n, 0, "path hash");
    }
    // a backslash, a NUL and an empty part (an absolute path under /app0/) in a path
    const char *to[] = {"/app0/data\\frag.bin", "/app0/data\0frag.bin", "/app0//ata/frag.bin"};
    const char *why[] = {"not a safe one", "path", "not a safe one"};
    for (int k = 0; k < 3; k++) {
        memcpy(d, orig, n);
        strings = mpt_r64(d + 96);
        uint8_t *at = memmem_simple(d + strings, n - strings, "/app0/data/frag.bin");
        if (!at) {
            MP_FAIL_("no data/frag.bin");
            break;
        }
        memcpy(at, to[k], 19);
        lz4_open_refused(d, n, 1, why[k]);
    }
    // a chunk count that is not its size's
    memcpy(d, orig, n);
    if ((rec = lz4_record_of(d, 2, -1))) {
        mpt_w32(rec + 28, mpt_r32(rec + 28) - 1);
        lz4_open_refused(d, n, 0, "chunk count");
    }
    // a file stored as it is with an LZ4 chunk, and a file said to stream whose chunks do not
    for (uint32_t flag = 2; flag <= 4; flag += 2) {
        memcpy(d, orig, n);
        if ((rec = lz4_record_of(d, 1, 1))) {
            mpt_w32(rec + 40, mpt_r32(rec + 40) | flag);
            lz4_open_refused(d, n, 0, "chunk out of its pack or of its place");
        }
    }
    // a chunk neither inside one I/O page nor at a page's start
    memcpy(d, orig, n);
    if ((rec = lz4_record_of(d, 1, -1))) {
        uint8_t *c = d + chunks + 12 * (uint64_t)mpt_r32(rec + 24);
        mpt_w32(c + 8, mpt_r32(c + 8) & ~((4u | 8u) << 22));
        lz4_open_refused(d, n, 0, "chunk out of its pack or of its place");
    }
    // a pack record: a page that is no power of two, and no I/O page layout
    for (int k = 0; k < 2; k++) {
        memcpy(d, orig, n);
        packs = mpt_r64(d + 88);
        mpt_w32(d + packs + (k ? 24 : 28), k ? 0 : 3000);
        lz4_open_refused(d, n, 0, "pack record");
    }
    // a loose file with chunks
    memcpy(d, orig, n);
    if ((rec = lz4_record_of(d, 0, -2))) {
        mpt_w32(rec + 28, 1);
        lz4_open_refused(d, n, 0, "loose file with chunks");
    }
    free(d);
    free(orig);
}

// A manifest of files that all claim the same chunks, which no packer writes (each file has a range of its
// own): refused at once, not after checking every chunk of every file.
MP_TEST(lz4_refuses_shared_chunk_ranges) {
    enum { NF = 4000, NC = 4000 };
    uint64_t files_at = MP_LZ4PK_HEADER, chunks_at = files_at + 48ull * NF, packs_at = chunks_at + 12ull * NC,
             strings_at = packs_at + 32, slen = 17ull * NF + 20, n = strings_at + slen;
    uint8_t *d = calloc(1, (size_t)n);
    if (!d) return;
    for (uint32_t i = 0; i < NF; i++) {
        char path[32];
        snprintf(path, sizeof path, "/app0/f%05u.bin", i);
        memcpy(d + strings_at + 17ull * i, path, 17);
        uint8_t *rec = d + files_at + 48ull * i;
        mpt_w64(rec, mp_lz4pk_path_hash(path));
        mpt_w64(rec + 8, (uint64_t)NC << 14);
        mpt_w32(rec + 24, 0);
        mpt_w32(rec + 28, NC);
        mpt_w32(rec + 32, 17 * i);
        mpt_w32(rec + 36, 16);
        mpt_w32(rec + 40, 1);
        rec[44] = 14;
    }
    for (uint32_t k = 0; k < NC; k++) {   // raw 16 KiB chunks, each from a page's start
        mpt_w64(d + chunks_at + 12ull * k, 4096 + ((uint64_t)k << 14));
        mpt_w32(d + chunks_at + 12ull * k + 8, (16384u - 1) | 8u << 22);
    }
    memcpy(d + strings_at + 17ull * NF, "ampr_assets-000.pak", 20);
    mpt_w64(d + packs_at, (uint64_t)NC << 14);
    mpt_w64(d + packs_at + 8, 4096 + ((uint64_t)NC << 14));
    mpt_w32(d + packs_at + 16, 17 * NF);
    mpt_w32(d + packs_at + 20, 19);
    mpt_w32(d + packs_at + 24, 2);
    mpt_w32(d + packs_at + 28, 4096);
    memcpy(d, "AMPRPAK4", 8);
    mpt_w32(d + 8, 4);
    mpt_w32(d + 12, MP_LZ4PK_HEADER);
    mpt_w32(d + 20, 0x01020304u);
    mpt_w64(d + 40, NF);
    mpt_w64(d + 48, NC);
    mpt_w32(d + 56, 1);
    mpt_w32(d + 60, 48);
    mpt_w32(d + 64, 12);
    mpt_w32(d + 68, 32);
    mpt_w64(d + 72, files_at);
    mpt_w64(d + 80, chunks_at);
    mpt_w64(d + 88, packs_at);
    mpt_w64(d + 96, strings_at);
    mpt_w64(d + 104, slen);
    lz4_open_refused(d, (size_t)n, 0, "overlap");
    free(d);
}

// More files, chunks or packs than ampr-emu-ng's runtime takes (AMPR_EMU_PACK_MAX_FILES, MAX_CHUNKS and
// MAX_PACKS): refused by their counts, before anything is made of them.
MP_TEST(lz4_refuses_counts_over_the_runtime_caps) {
    const uint64_t nf = 2000001, nc = 16000001, np = 1025;
    const uint64_t lens[3] = {MP_LZ4PK_HEADER + 48 * nf, MP_LZ4PK_HEADER + 12 * nc, MP_LZ4PK_HEADER + 32 * np + 20 * np};
    uint8_t *d = calloc(1, (size_t)lens[1]);
    if (!d) {
        MP_FAIL_("no memory for the test");
        return;
    }
    for (int k = 0; k < 3; k++) {
        size_t n = (size_t)lens[k];
        memset(d, 0, n);
        uint64_t f = k == 0 ? nf : 0, c = k == 1 ? nc : 0, p = k == 2 ? np : 0;
        uint64_t at = MP_LZ4PK_HEADER + 48 * f + 12 * c + 32 * p;
        for (uint64_t i = 0; i < p; i++) {   // right pack records, each one with its name
            uint8_t *q = d + MP_LZ4PK_HEADER + 32 * i;
            snprintf((char *)d + at + 20 * i, 20, "ampr_assets-%04llu.p", (unsigned long long)i);
            mpt_w64(q, 4096);
            mpt_w64(q + 8, 8192);
            mpt_w32(q + 16, (uint32_t)(20 * i));
            mpt_w32(q + 20, 18);
            mpt_w32(q + 24, 2);
            mpt_w32(q + 28, 4096);
        }
        memcpy(d, "AMPRPAK4", 8);
        mpt_w32(d + 8, 4);
        mpt_w32(d + 12, MP_LZ4PK_HEADER);
        mpt_w32(d + 20, 0x01020304u);
        mpt_w64(d + 40, f);
        mpt_w64(d + 48, c);
        mpt_w32(d + 56, (uint32_t)p);
        mpt_w32(d + 60, 48);
        mpt_w32(d + 64, 12);
        mpt_w32(d + 68, 32);
        mpt_w64(d + 72, MP_LZ4PK_HEADER);
        mpt_w64(d + 80, MP_LZ4PK_HEADER + 48 * f);
        mpt_w64(d + 88, MP_LZ4PK_HEADER + 48 * f + 12 * c);
        mpt_w64(d + 96, at);
        mpt_w64(d + 104, n - at);
        lz4_open_refused(d, n, 0, "counts");
    }
    free(d);
}

// A manifest of another version says what to do with it: only AMPRPAK3 has a converter (ampr_emu's
// convert_ampr_pack_v3.py).
MP_TEST(lz4_older_manifests_say_what_to_do) {
    uint8_t d[MP_LZ4PK_HEADER] = "AMPRPAK1";
    for (char v = '1'; v <= '3'; v++) {
        d[7] = (uint8_t)v;
        mp_lz4pk *l = NULL;
        mp_err e = {{0}};
        int older = 0;
        MP_CHECK(mp_lz4pk_open(d, sizeof d, &l, &older, &e) == -1 && older == 1);
        if (v == '3') MP_CHECK_CONTAINS(e.msg, "convert_ampr_pack_v3.py");
        else MP_CHECK(strstr(e.msg, "convert_ampr_pack_v3") == NULL && strstr(e.msg, "again") != NULL);
    }
}

// A manifest written by drakmor/ampr_emu's own Python tools (tools/build_ampr_index.py and
// tools/ampr_pack_format.py): ampr-emu-ng's crates/ampr-lz4/tests/fixtures/python_manifest.bin
// (GPL-3.0-or-later), byte for byte. A packed file of one raw block at a page's start, and a loose one.
static const uint8_t python_manifest[325] = {
    0x41, 0x4d, 0x50, 0x52, 0x50, 0x41, 0x4b, 0x34, 0x04, 0x00, 0x00, 0x00, 0x80, 0x00, 0x00, 0x00,
    0x00, 0x00, 0x00, 0x00, 0x04, 0x03, 0x02, 0x01, 0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07,
    0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x02, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
    0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x30, 0x00, 0x00, 0x00,
    0x0c, 0x00, 0x00, 0x00, 0x20, 0x00, 0x00, 0x00, 0x80, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
    0xe0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xec, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
    0x0c, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x39, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
    0xf5, 0x1e, 0x18, 0xbc, 0xc4, 0xf1, 0x7d, 0x5d, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
    0xba, 0x5f, 0x10, 0x23, 0x31, 0x72, 0xde, 0xfd, 0x00, 0x40, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
    0x7b, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00,
    0x00, 0x00, 0x00, 0x00, 0x13, 0x00, 0x00, 0x00, 0x03, 0x00, 0x00, 0x00, 0x0e, 0x00, 0x00, 0x00,
    0xcc, 0xa7, 0x80, 0x68, 0x80, 0x97, 0x98, 0xa6, 0x07, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
    0xc8, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
    0x14, 0x00, 0x00, 0x00, 0x10, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
    0x00, 0x10, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xff, 0x3f, 0x00, 0x02, 0x00, 0x40, 0x00, 0x00,
    0x00, 0x00, 0x00, 0x00, 0x00, 0x50, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x25, 0x00, 0x00, 0x00,
    0x13, 0x00, 0x00, 0x00, 0x02, 0x00, 0x00, 0x00, 0x00, 0x10, 0x00, 0x00, 0x2f, 0x61, 0x70, 0x70,
    0x30, 0x2f, 0x44, 0x61, 0x74, 0x61, 0x2f, 0x54, 0x65, 0x73, 0x74, 0x2e, 0x62, 0x69, 0x6e, 0x00,
    0x2f, 0x61, 0x70, 0x70, 0x30, 0x2f, 0x72, 0x65, 0x61, 0x64, 0x6d, 0x65, 0x2e, 0x74, 0x78, 0x74,
    0x00, 0x61, 0x6d, 0x70, 0x72, 0x5f, 0x61, 0x73, 0x73, 0x65, 0x74, 0x73, 0x2d, 0x30, 0x30, 0x30,
    0x2e, 0x70, 0x61, 0x6b, 0x00,
};

MP_TEST(lz4_reads_a_manifest_of_the_python_tools) {
    mp_lz4pk *l = NULL;
    mp_err e = {{0}};
    if (mp_lz4pk_open(python_manifest, sizeof python_manifest, &l, NULL, &e) != 0) {
        MP_FAIL_("%s", e.msg);
        return;
    }
    MP_CHECK_EQ_U64(mp_lz4pk_count(l), 2);
    MP_CHECK_EQ_U64(mp_lz4pk_chunk_count(l), 1);
    MP_CHECK_EQ_U64(mp_lz4pk_pack_count(l), 1);
    MP_CHECK_STR(mp_lz4pk_pack_name(l, 0), "ampr_assets-000.pak");
    MP_CHECK_STR(mp_lz4pk_path(l, 0), "Data/Test.bin");
    MP_CHECK_EQ_U64(mp_lz4pk_size(l, 0), 16384);
    MP_CHECK(mp_lz4pk_mtime(l, 0) == 123 && mp_lz4pk_packed(l, 0));
    MP_CHECK_STR(mp_lz4pk_path(l, 1), "readme.txt");
    MP_CHECK_EQ_U64(mp_lz4pk_size(l, 1), 7);
    MP_CHECK(mp_lz4pk_mtime(l, 1) == 456 && !mp_lz4pk_packed(l, 1));
    mp_lz4pk_close(l);
}

int main(void) {
    mp_err e = {{0}};
    mpt_clear(TMP);
    mp_make_dirs(TMP, &e);
    MP_RUN(recompress_is_identical);
    MP_RUN(recompress_other_level_and_folder);
    MP_RUN(recompress_other_split);
    MP_RUN(source_has_the_original_files);
    MP_RUN(keep_packs_round_trip);
    MP_RUN(keep_packs_of_another_tool);
    MP_RUN(keep_packs_reads_each_chunk_once);
    MP_RUN(refuses_a_damaged_block);
    MP_RUN(refuses_a_damaged_index_or_volume);
    MP_RUN(refuses_a_missing_file);
    MP_RUN(refuses_an_unlisted_packed_file);
    MP_RUN(refuses_lz4_packs);
    MP_RUN(refuses_a_file_loose_and_packed);
    MP_RUN(refuses_a_file_that_is_a_folder);
    MP_RUN(refuses_a_loose_file_cut_short);
    MP_RUN(leaves_out_ampr_files_in_the_packs);
    MP_RUN(refuses_a_chunk_of_two_frames);
    MP_RUN(takes_a_loose_copy_the_same_as_the_packed_one);
    MP_RUN(cancel_while_reading_the_packs);
    MP_RUN(lz4_recompress_is_identical);
    MP_RUN(lz4_source_has_the_original_files);
    MP_RUN(lz4_refuses_damage);
    MP_RUN(lz4_refuses_tricky_paths);
    MP_RUN(lz4_refuses_missing_and_twice);
    MP_RUN(lz4_refuses_other_kinds);
    MP_RUN(lz4_takes_a_game_packed_in_place);
    MP_RUN(lz4_without_sidecars);
    MP_RUN(lz4_cancel_while_reading_the_packs);
    MP_RUN(lz4_reader_refusals);
    MP_RUN(lz4_refuses_shared_chunk_ranges);
    MP_RUN(lz4_refuses_counts_over_the_runtime_caps);
    MP_RUN(lz4_older_manifests_say_what_to_do);
    MP_RUN(lz4_reads_a_manifest_of_the_python_tools);
    MP_TEST_MAIN_END();
}
