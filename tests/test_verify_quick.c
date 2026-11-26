// SPDX-License-Identifier: GPL-3.0-or-later
// The quick verify of a .p5z (mp_verify_quick.h): a fresh pack, one that grew, one whose append stopped, and
// copies with a byte changed in a volume, in a volume's header and in a loose file, cut short or cancelled.
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "mp_fixture.h"
#include "mp_grow.h"
#include "mp_hash.h"
#include "mp_p5z_internal.h"
#include "mp_verify_quick.h"

#define TMP MP_TEST_TMP "/verify_quick"
#define GAME MP_FIXTURES_DIR "/game"
#define V1 TMP "/v1"
#define V2 TMP "/v2"
#define FRESH TMP "/fresh.p5z"
#define GROWN TMP "/grown.p5z"
#define SMALL_VOLUMES 262208u

typedef struct {
    const char *from, *to;
} tree_copy;

static int copy_cb(void *ctx, const char *rel, int is_dir, uint64_t size, int64_t mtime) {
    const tree_copy *t = ctx;
    (void)size;
    if (is_dir) return 0;
    char a[2048], b[2048];
    snprintf(a, sizeof a, "%s/%s", t->from, rel);
    snprintf(b, sizeof b, "%s/%s", t->to, rel);
    mpt_copy_file(a, b);
    mp_set_mtime(b, mtime, NULL);
    return 0;
}

static void copy_tree(const char *from, const char *to) {
    tree_copy t = {from, to};
    mpt_clear(to);
    mp_err e = {{0}};
    mp_make_dirs(to, &e);
    mp_dir_walk(from, copy_cb, &t, &e);
}

static void put_dated(const char *path, const void *data, size_t n, int64_t mtime) {
    mpt_clear(path);
    mpt_put(path, data, n);
    mp_set_mtime(path, mtime, NULL);
}

// v2: the game's next version, with a new big file (new volumes) and a changed param.json.
static void make_versions(void) {
    copy_tree(GAME, V1);
    copy_tree(GAME, V2);
    const char *pj = "{\"contentId\": \"UP0000-PPSA00000_00-MAXPACKTEST00000\", \"titleId\": \"PPSA00000\", "
                     "\"contentVersion\": \"01.001.000\", \"localizedParameters\": {\"defaultLanguage\": \"en-US\", "
                     "\"en-US\": {\"titleName\": \"MaxPack Test Game\"}}}";
    put_dated(V2 "/sce_sys/param.json", pj, strlen(pj), 1700100000);
    uint8_t *t = malloc(600000);
    uint32_t x = 12345;
    for (size_t i = 0; i < 600000; i++) {
        x = x * 1103515245u + 12345u;
        t[i] = (uint8_t)(i % 7 ? x >> 24 : 'm');
    }
    put_dated(V2 "/data/new1.bin", t, 600000, 1700100002);
    free(t);
}

static int grow_with(const char *p5z, const char *dump, mp_grow_result *r, mp_err *e) {
    mp_grow_opts o;
    mp_grow_opts_default(&o);
    o.threads = 2;
    o.progress = 0;
    o.force = 1;
    o.volume_limit = SMALL_VOLUMES;
    mp_source *s = NULL;
    if (mp_source_open(dump, &s, e) != 0) return -1;
    int rc = mp_grow_run(p5z, s, NULL, NULL, &o, r, e);
    mp_source_close(s);
    return rc;
}

// What the progress callback saw.
typedef struct {
    uint64_t calls, last_done, last_total;
    int went_back;
    uint64_t cancel_after;   // the cancel callback says stop once done passes it (0: never)
} seen;

static void on_progress(void *ctx, uint64_t done, uint64_t total) {
    seen *s = ctx;
    if (done < s->last_done) s->went_back = 1;
    s->calls++;
    s->last_done = done;
    s->last_total = total;
}

static int on_cancel(void *ctx) {
    const seen *s = ctx;
    return s->cancel_after && s->last_done >= s->cancel_after;
}

// The regions of every record of the .p5z at path, as the index gives them, and how many are volumes.
static uint64_t regions_of(const char *path, uint32_t *volumes) {
    mp_p5z *p = NULL;
    uint64_t t = 0;
    *volumes = 0;
    if (mp_p5z_open(path, &p, NULL) != 0) return 0;
    for (size_t i = 0; i < p->nfiles; i++) {
        t += p->files[i].data_len;
        *volumes += p->files[i].role == MP_ROLE_VOLUME;
    }
    mp_p5z_close(p);
    return t;
}

static uint32_t records_of(const char *path) {
    mp_p5z *p = NULL;
    if (mp_p5z_open(path, &p, NULL) != 0) return 0;
    uint32_t n = (uint32_t)p->nfiles;
    mp_p5z_close(p);
    return n;
}

static int ensure_fresh(void) {
    if (mp_path_kind(FRESH) == 1) return 0;
    mpt_clear(TMP);
    return mpt_pack_vol(GAME, FRESH, 1, NULL, SMALL_VOLUMES);
}

// A copy of FRESH (or src) as dst, with the byte at `at` changed.
static int flipped_copy(const char *src, const char *dst, uint64_t at) {
    size_t n = 0;
    uint8_t *b = mpt_slurp(src, &n);
    if (!b || at >= n) {
        free(b);
        MP_FAIL_("cannot change byte %llu of %s", (unsigned long long)at, src);
        return -1;
    }
    b[at] ^= 0x5A;
    mpt_clear(dst);
    mpt_put(dst, b, n);
    free(b);
    return 0;
}

// The first record of that role (and, for MP_ROLE_LOOSE, that storage) in the .p5z at path.
static int record_of(const char *path, int role, int storage, p5z_file *out) {
    mp_p5z *p = NULL;
    int found = 0;
    if (mp_p5z_open(path, &p, NULL) != 0) return 0;
    for (size_t i = 0; i < p->nfiles && !found; i++) {
        const p5z_file *f = &p->files[i];
        if (f->role != role || (storage >= 0 && f->storage != storage) || f->size < 200) continue;
        *out = *f;
        out->path = mp_strdup(f->path);
        found = 1;
    }
    mp_p5z_close(p);
    return found;
}

MP_TEST(quick_fresh_pack_is_ok) {
    if (ensure_fresh() != 0) return;
    uint32_t nvol = 0;
    uint64_t total = regions_of(FRESH, &nvol);
    MP_CHECK(nvol >= 3);   // the small volume limit gives several
    seen s = {0};
    mp_qv_progress pr = {on_progress, &s};
    mp_qv_report r;
    mp_err e = {{0}};
    int rc = mp_p5z_verify_quick(FRESH, &pr, NULL, &r, &e);
    if (rc != MP_QV_CHECKED) MP_FAIL_("rc %d: %s", rc, e.msg);
    MP_CHECK(r.ok == 1);
    MP_CHECK_EQ_U64(r.nbad, 0);
    MP_CHECK_EQ_U64(r.nlisted, 0);
    MP_CHECK_EQ_U64(r.total, total);
    MP_CHECK_EQ_U64(r.bytes, total);
    MP_CHECK_EQ_U64(r.version, 1);
    MP_CHECK_EQ_U64(r.volumes, nvol);
    MP_CHECK_EQ_U64(r.files + r.volumes, records_of(FRESH));
    MP_CHECK(r.files >= 15);   // the game's loose files and the two indexes
    MP_CHECK(s.calls >= 2 && !s.went_back);
    MP_CHECK_EQ_U64(s.last_done, total);
    MP_CHECK_EQ_U64(s.last_total, total);
    mp_qv_report_free(&r);
    MP_CHECK(r.bad == NULL);
}

MP_TEST(quick_grown_is_ok) {
    mpt_clear(TMP);
    make_versions();
    if (mpt_pack_vol(V1, GROWN, 1, NULL, SMALL_VOLUMES) != 0) return;
    uint32_t v1_volumes = 0;
    uint64_t v1_total = regions_of(GROWN, &v1_volumes);
    mp_grow_result g;
    mp_err e = {{0}};
    if (grow_with(GROWN, V2, &g, &e) != 0) {
        MP_FAIL_("grow: %s", e.msg);
        return;
    }
    uint32_t nvol = 0;
    uint64_t total = regions_of(GROWN, &nvol);
    MP_CHECK(nvol > v1_volumes);
    mp_qv_report r;
    int rc = mp_p5z_verify_quick(GROWN, NULL, NULL, &r, &e);
    if (rc != MP_QV_CHECKED) MP_FAIL_("rc %d: %s", rc, e.msg);
    MP_CHECK(r.ok == 1 && r.nbad == 0);
    MP_CHECK_EQ_U64(r.version, 2);
    MP_CHECK_EQ_U64(r.volumes, nvol);
    MP_CHECK_EQ_U64(r.total, total);
    MP_CHECK_EQ_U64(r.bytes, total);
    mp_qv_report_free(&r);
    (void)v1_total;
}

// An append that stops as a power cut would, its journal left: the .p5z is checked as it was before it.
static int stop_at_index(const char *step) { return !strcmp(step, "index") ? 2 : 0; }

MP_TEST(quick_follows_the_journal) {
    mpt_clear(TMP);
    make_versions();
    if (mpt_pack_vol(V1, GROWN, 1, NULL, SMALL_VOLUMES) != 0) return;
    uint32_t v1_volumes = 0;
    uint64_t v1_total = regions_of(GROWN, &v1_volumes);
    mp_grow_set_fault(stop_at_index);
    mp_grow_result g;
    mp_err e = {{0}};
    MP_CHECK(grow_with(GROWN, V2, &g, &e) != 0);
    mp_grow_set_fault(NULL);
    MP_CHECK(mp_path_kind(GROWN MP_P5Z_JOURNAL) == 1);
    uint64_t size = 0;
    mp_stat(GROWN, &size, NULL);
    mp_qv_report r;
    int rc = mp_p5z_verify_quick(GROWN, NULL, NULL, &r, &e);
    if (rc != MP_QV_CHECKED) MP_FAIL_("rc %d: %s", rc, e.msg);
    MP_CHECK(r.ok == 1);
    MP_CHECK_EQ_U64(r.version, 1);
    MP_CHECK_EQ_U64(r.volumes, v1_volumes);
    MP_CHECK_EQ_U64(r.total, v1_total);
    MP_CHECK(r.total < size);
    mp_qv_report_free(&r);
    // it never cuts the file nor touches the journal
    uint64_t after = 0;
    mp_stat(GROWN, &after, NULL);
    MP_CHECK_EQ_U64(after, size);
    MP_CHECK(mp_path_kind(GROWN MP_P5Z_JOURNAL) == 1);
}

MP_TEST(quick_finds_a_byte_in_a_volume) {
    if (ensure_fresh() != 0) return;
    mp_p5z *p = NULL;
    if (mp_p5z_open(FRESH, &p, NULL) != 0) return;
    uint8_t want[32] = {0};
    uint64_t at = 0;
    for (size_t i = 0; i < p->nfiles; i++)
        if (p->files[i].role == MP_ROLE_VOLUME && !strcmp(p->files[i].path, "ampr_zstd-001.pak")) {
            at = p->files[i].data_off + p->files[i].size / 2;
            memcpy(want, p->files[i].sha, 32);
        }
    mp_p5z_close(p);
    MP_CHECK(at > 0);
    if (!at || flipped_copy(FRESH, TMP "/bad_volume.p5z", at) != 0) return;
    mp_qv_report r;
    mp_err e = {{0}};
    int rc = mp_p5z_verify_quick(TMP "/bad_volume.p5z", NULL, NULL, &r, &e);
    MP_CHECK(rc == MP_QV_CHECKED);
    MP_CHECK(r.ok == 0);
    MP_CHECK_EQ_U64(r.nbad, 1);
    MP_CHECK_EQ_U64(r.nlisted, 1);
    if (r.nlisted == 1) {
        const mp_qv_bad *b = &r.bad[0];
        MP_CHECK(b->kind == MP_QV_VOLUME);
        MP_CHECK_EQ_U64(b->volume, 1);
        MP_CHECK_STR(b->path, "ampr_zstd-001.pak");
        MP_CHECK_STR(b->why, "sha256");
        MP_CHECK(b->has_found);
        MP_CHECK(!memcmp(b->expected, want, 32));
        MP_CHECK(memcmp(b->found, want, 32) != 0);
    }
    MP_CHECK_EQ_U64(r.bytes, r.total);   // it goes on after a bad one
    mp_qv_report_free(&r);
}

MP_TEST(quick_finds_a_byte_in_a_volume_header) {
    if (ensure_fresh() != 0) return;
    mp_p5z *p = NULL;
    if (mp_p5z_open(FRESH, &p, NULL) != 0) return;
    uint64_t at = mp_p5z_volume_offset(p, 2) + 30;   // inside the archive_id
    mp_p5z_close(p);
    if (flipped_copy(FRESH, TMP "/bad_header.p5z", at) != 0) return;
    mp_qv_report r;
    mp_err e = {{0}};
    MP_CHECK(mp_p5z_verify_quick(TMP "/bad_header.p5z", NULL, NULL, &r, &e) == MP_QV_CHECKED);
    MP_CHECK(r.ok == 0 && r.nbad == 1);
    if (r.nlisted == 1) {
        MP_CHECK(r.bad[0].kind == MP_QV_VOLUME);
        MP_CHECK_EQ_U64(r.bad[0].volume, 2);
        MP_CHECK_STR(r.bad[0].why, "header");
        MP_CHECK(r.bad[0].has_found && !memcmp(r.bad[0].found, r.bad[0].expected, 32));
    }
    mp_qv_report_free(&r);
}

MP_TEST(quick_finds_a_byte_in_a_loose_file) {
    if (ensure_fresh() != 0) return;
    // the loose files are stored compressed (only the volumes are stored as they are): decompressed and hashed
    p5z_file f;
    if (!record_of(FRESH, MP_ROLE_LOOSE, P5Z_STORE_FRAMES, &f)) {
        MP_FAIL_("no compressed loose file in the .p5z");
        return;
    }
    if (flipped_copy(FRESH, TMP "/bad_loose.p5z", f.data_off + f.data_len / 2) != 0) return;
    mp_qv_report r;
    mp_err e = {{0}};
    MP_CHECK(mp_p5z_verify_quick(TMP "/bad_loose.p5z", NULL, NULL, &r, &e) == MP_QV_CHECKED);
    MP_CHECK(r.ok == 0 && r.nbad == 1);
    if (r.nlisted == 1) {
        MP_CHECK(r.bad[0].kind == MP_QV_FILE);
        MP_CHECK_STR(r.bad[0].path, f.path);
        MP_CHECK(!strcmp(r.bad[0].why, "sha256") || !strcmp(r.bad[0].why, "data"));
        MP_CHECK(!memcmp(r.bad[0].expected, f.sha, 32));
    }
    mp_qv_report_free(&r);
    free(f.path);
}

MP_TEST(quick_refuses_a_cut_or_damaged_file) {
    if (ensure_fresh() != 0) return;
    size_t n = 0;
    uint8_t *b = mpt_slurp(FRESH, &n);
    if (!b) return;
    mpt_clear(TMP "/cut.p5z");
    mpt_put(TMP "/cut.p5z", b, n - 1000);
    mp_qv_report r;
    mp_err e = {{0}};
    MP_CHECK(mp_p5z_verify_quick(TMP "/cut.p5z", NULL, NULL, &r, &e) == MP_QV_ERROR);
    MP_CHECK_CONTAINS(e.msg, "truncated");
    MP_CHECK(r.ok == 0 && r.nbad == 0 && r.bad == NULL);
    mp_qv_report_free(&r);
    // the index's CRC
    if (flipped_copy(FRESH, TMP "/bad_index.p5z", n - 40) == 0) {
        MP_CHECK(mp_p5z_verify_quick(TMP "/bad_index.p5z", NULL, NULL, &r, &e) == MP_QV_ERROR);
        MP_CHECK_CONTAINS(e.msg, "index CRC");
        mp_qv_report_free(&r);
    }
    // the header's
    if (flipped_copy(FRESH, TMP "/bad_head.p5z", 20) == 0) {
        MP_CHECK(mp_p5z_verify_quick(TMP "/bad_head.p5z", NULL, NULL, &r, &e) == MP_QV_ERROR);
        MP_CHECK_CONTAINS(e.msg, "header");
        mp_qv_report_free(&r);
    }
    MP_CHECK(mp_p5z_verify_quick(TMP "/none.p5z", NULL, NULL, &r, &e) == MP_QV_ERROR);
    mp_qv_report_free(&r);
    free(b);
}

MP_TEST(quick_can_be_cancelled) {
    if (ensure_fresh() != 0) return;
    seen s = {0};
    s.cancel_after = 1;
    mp_qv_progress pr = {on_progress, &s};
    mp_qv_cancel ca = {on_cancel, &s};
    mp_qv_report r;
    mp_err e = {{0}};
    MP_CHECK(mp_p5z_verify_quick(FRESH, &pr, &ca, &r, &e) == MP_QV_CANCELLED);
    MP_CHECK_CONTAINS(e.msg, "cancelled");
    MP_CHECK(r.ok == 0);
    MP_CHECK(r.bytes < r.total);
    mp_qv_report_free(&r);
}

// More bad records than it lists: all counted, the first MP_QV_MAX_LISTED listed. A game with 80 more files in
// sce_sys/ (loose), each with its last stored byte changed.
MP_TEST(quick_counts_more_than_it_lists) {
    mpt_clear(TMP "/many");
    copy_tree(GAME, TMP "/many");
    for (int i = 0; i < 80; i++) {
        char path[256], text[64];
        snprintf(path, sizeof path, TMP "/many/sce_sys/extra/f%02d.txt", i);
        snprintf(text, sizeof text, "loose file number %d of a game with many\n", i);
        mpt_put_text(path, text);
    }
    if (mpt_pack_vol(TMP "/many", TMP "/many.p5z", 1, NULL, SMALL_VOLUMES) != 0) return;
    size_t n = 0;
    uint8_t *b = mpt_slurp(TMP "/many.p5z", &n);
    mp_p5z *p = NULL;
    if (!b || mp_p5z_open(TMP "/many.p5z", &p, NULL) != 0) {
        free(b);
        return;
    }
    size_t changed = 0;
    for (size_t i = 0; i < p->nfiles; i++)
        if (p->files[i].role == MP_ROLE_LOOSE && p->files[i].data_len > 0) {
            b[p->files[i].data_off + p->files[i].data_len - 1] ^= 0x5A;
            changed++;
        }
    mp_p5z_close(p);
    mpt_clear(TMP "/many_bad.p5z");
    mpt_put(TMP "/many_bad.p5z", b, n);
    free(b);
    MP_CHECK(changed > MP_QV_MAX_LISTED);
    mp_qv_report r;
    mp_err e = {{0}};
    MP_CHECK(mp_p5z_verify_quick(TMP "/many_bad.p5z", NULL, NULL, &r, &e) == MP_QV_CHECKED);
    MP_CHECK(r.ok == 0);
    MP_CHECK_EQ_U64(r.nbad, changed);
    MP_CHECK_EQ_U64(r.nlisted, MP_QV_MAX_LISTED);
    for (size_t i = 0; i < r.nlisted; i++) MP_CHECK(r.bad[i].kind == MP_QV_FILE && r.bad[i].path[0]);
    MP_CHECK_EQ_U64(r.bytes, r.total);
    mp_qv_report_free(&r);
}

int main(void) {
    MP_RUN(quick_fresh_pack_is_ok);
    MP_RUN(quick_finds_a_byte_in_a_volume);
    MP_RUN(quick_finds_a_byte_in_a_volume_header);
    MP_RUN(quick_finds_a_byte_in_a_loose_file);
    MP_RUN(quick_refuses_a_cut_or_damaged_file);
    MP_RUN(quick_can_be_cancelled);
    MP_RUN(quick_counts_more_than_it_lists);
    MP_RUN(quick_grown_is_ok);
    MP_RUN(quick_follows_the_journal);
    MP_TEST_MAIN_END();
}
