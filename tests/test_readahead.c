// SPDX-License-Identifier: GPL-3.0-or-later
// The read-ahead hands out exactly the bytes and the SHA-256s of reading the files one after another
// with mp_source_read, in the order asked, whatever its threads, piece size and memory; and fails,
// cancels and stops as that reading would, and starts no read while paused.
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "mp_fixture.h"
#include "mp_hash.h"
#include "mp_parallel.h"
#include "mp_readahead.h"
#include "mp_source.h"
#include "mp_source_impl.h"

#define FX MP_FIXTURES_DIR
#define TMP MP_TEST_TMP "/readahead"
#define DIRSRC TMP "/dir"
#define KIB 1024u
#define MIB (1024u * 1024u)

static uint32_t rng = 88172645u;
static uint32_t rnd(void) {
    rng ^= rng << 13;
    rng ^= rng >> 17;
    rng ^= rng << 5;
    return rng;
}

static void make_dir(void) {
    static int ready = 0;
    if (ready) return;
    ready = 1;
    mpt_clear(DIRSRC);
    // around the block and piece sizes, empty ones and many small ones
    static const size_t sizes[] = {0, 1, 262143, 262144, 262145, 1u << 20, (4u << 20) + 3, 9u << 20, 0};
    uint8_t *b = malloc(9u << 20);
    if (!b) return;
    for (size_t i = 0; i < sizeof sizes / sizeof *sizes; i++) {
        for (size_t k = 0; k < sizes[i]; k++) b[k] = (uint8_t)(rnd() >> 24);
        char p[512];
        snprintf(p, sizeof p, DIRSRC "/f%02zu.bin", i);
        mpt_put(p, b, sizes[i]);
    }
    for (int i = 0; i < 150; i++) {
        size_t n = 1 + rnd() % 5000;
        for (size_t k = 0; k < n; k++) b[k] = (uint8_t)(rnd() >> 24);
        char p[512];
        snprintf(p, sizeof p, DIRSRC "/small/s%03d.dat", i);
        mpt_put(p, b, n);
    }
    free(b);
}

// Every file of the source, in a scrambled but fixed order.
static uint32_t *scrambled(const mp_source *s, size_t *n) {
    *n = mp_source_count(s);
    uint32_t *f = malloc((*n ? *n : 1) * sizeof *f);
    for (size_t i = 0; f && i < *n; i++) f[i] = (uint32_t)i;
    uint32_t st = 12345;
    for (size_t i = *n; f && i > 1; i--) {
        st = st * 1103515245u + 12345u;
        size_t j = (st >> 8) % i;
        uint32_t t = f[i - 1];
        f[i - 1] = f[j];
        f[j] = t;
    }
    return f;
}

typedef struct {
    uint8_t all[32];       // SHA-256 of every byte in order
    uint8_t (*sha)[32];    // of each file
    uint64_t bytes;
} digest;

// Reads the files one after another with mp_source_read.
static void reference(mp_source *s, const uint32_t *f, size_t n, digest *d) {
    d->sha = calloc(n ? n : 1, 32);
    d->bytes = 0;
    uint8_t *b = malloc(MIB);
    mp_sha256 all;
    mp_sha256_init(&all);
    for (size_t k = 0; b && d->sha && k < n; k++) {
        uint64_t size = mp_source_entry(s, f[k])->size;
        mp_sha256 h;
        mp_sha256_init(&h);
        for (uint64_t off = 0; off < size; off += MIB) {
            size_t len = size - off < MIB ? (size_t)(size - off) : MIB;
            mp_err e = {{0}};
            if (mp_source_read(s, f[k], off, b, len, &e) != 0) MP_FAIL_("reference: %s", e.msg);
            mp_sha256_update(&h, b, len);
            mp_sha256_update(&all, b, len);
        }
        mp_sha256_final(&h, d->sha[k]);
        d->bytes += size;
    }
    mp_sha256_final(&all, d->all);
    free(b);
}

// Takes every piece and checks their order; 0, or the code of the last mp_readahead_next.
static int drain(mp_readahead *ra, const mp_source *s, const uint32_t *f, size_t n, digest *d, mp_err *e) {
    mp_sha256 all;
    mp_sha256_init(&all);
    d->bytes = 0;
    size_t file = 0;
    uint64_t off = 0;
    mp_readahead_piece p;
    int rc;
    while ((rc = mp_readahead_next(ra, &p, e)) == 1) {
        while (file < n && off == mp_source_entry(s, f[file])->size) {   // files done (and empty ones)
            file++;
            off = 0;
        }
        if (file >= n || p.item != file || p.off != off || p.len == 0) {
            MP_FAIL_("piece out of order: file %zu off %llu len %zu, expected file %zu off %llu", p.item,
                     (unsigned long long)p.off, p.len, file, (unsigned long long)off);
            return 2;
        }
        off += p.len;
        if (p.last != (off == mp_source_entry(s, f[file])->size)) MP_FAIL_("last flag of file %zu", file);
        mp_sha256_update(&all, p.data, p.len);
        d->bytes += p.len;
    }
    mp_sha256_final(&all, d->all);
    return rc;
}

static void same_bytes(const char *src, int with_sha) {
    mp_err e = {{0}};
    mp_source *s = NULL;
    if (mp_source_open(src, &s, &e) != 0) {
        MP_FAIL_("%s: %s", src, e.msg);
        return;
    }
    size_t n = 0;
    uint32_t *f = scrambled(s, &n);
    digest ref;
    reference(s, f, n, &ref);
    static const unsigned threads[] = {1, 2, 4, 16};
    static const size_t pieces[] = {256 * KIB, MIB, 4 * MIB};
    static const size_t budgets[] = {0, 3, 64};   // in pieces (0 = one piece, the least)
    for (size_t a = 0; a < 4; a++)
        for (size_t b = 0; b < 3; b++)
            for (size_t c = 0; c < 3; c++) {
                mp_readahead_opts o = {threads[a], pieces[b], (budgets[c] ? budgets[c] : 1) * pieces[b]};
                digest got;
                got.sha = with_sha ? calloc(n ? n : 1, 32) : NULL;
                mp_readahead *ra = NULL;
                if (mp_readahead_open(s, f, n, got.sha, &o, &ra, &e) != 0) {
                    MP_FAIL_("open: %s", e.msg);
                    free(got.sha);
                    continue;
                }
                int rc = drain(ra, s, f, n, &got, &e);
                mp_readahead_close(ra);
                if (rc != 0 || got.bytes != ref.bytes || memcmp(got.all, ref.all, 32) ||
                    (with_sha && memcmp(got.sha, ref.sha, 32 * n)))
                    MP_FAIL_("%s, %u threads, %zu KiB pieces, budget %zu: rc %d (%s), %llu of %llu bytes", src,
                             o.threads, o.piece / KIB, o.budget / KIB, rc, rc < 0 ? e.msg : "",
                             (unsigned long long)got.bytes, (unsigned long long)ref.bytes);
                free(got.sha);
            }
    free(ref.sha);
    free(f);
    mp_source_close(s);
}

MP_TEST(readahead_same_bytes_exfat) { same_bytes(FX "/basic.exfat", 1); }

MP_TEST(readahead_same_bytes_dir) {
    make_dir();
    same_bytes(DIRSRC, 1);
}

MP_TEST(readahead_without_sha) { same_bytes(FX "/basic.exfat", 0); }

// Over and over with several threads and little memory: shakes out races between them.
MP_TEST(readahead_repeated) {
    make_dir();
    mp_err e = {{0}};
    mp_source *s = NULL;
    if (mp_source_open(DIRSRC, &s, &e) != 0) {
        MP_FAIL_("%s", e.msg);
        return;
    }
    size_t n = 0;
    uint32_t *f = scrambled(s, &n);
    digest ref;
    reference(s, f, n, &ref);
    for (int i = 0; i < 200; i++) {
        mp_readahead_opts o = {1 + (unsigned)i % 8, 256 * KIB, (1 + (size_t)i % 5) * 256 * KIB};
        digest got;
        got.sha = calloc(n, 32);
        mp_readahead *ra = NULL;
        if (mp_readahead_open(s, f, n, got.sha, &o, &ra, &e) != 0) {
            MP_FAIL_("open: %s", e.msg);
            free(got.sha);
            break;
        }
        int rc = drain(ra, s, f, n, &got, &e);
        mp_readahead_close(ra);
        if (rc != 0 || memcmp(got.all, ref.all, 32) || memcmp(got.sha, ref.sha, 32 * n)) {
            MP_FAIL_("run %d: rc %d", i, rc);
            free(got.sha);
            break;
        }
        free(got.sha);
    }
    free(ref.sha);
    free(f);
    mp_source_close(s);
}

MP_TEST(readahead_nothing_to_read) {
    make_dir();
    mp_err e = {{0}};
    mp_source *s = NULL;
    if (mp_source_open(DIRSRC, &s, &e) != 0) {
        MP_FAIL_("%s", e.msg);
        return;
    }
    mp_readahead_opts o;
    mp_readahead_opts_default(&o);
    mp_readahead *ra = NULL;
    mp_readahead_piece p;
    // no files
    MP_CHECK(mp_readahead_open(s, NULL, 0, NULL, &o, &ra, &e) == 0);
    MP_CHECK(mp_readahead_next(ra, &p, &e) == 0);
    MP_CHECK(mp_readahead_next(ra, &p, &e) == 0);
    mp_readahead_close(ra);
    // only empty files: no pieces, and the SHA-256 of nothing
    uint32_t f[2];
    f[0] = (uint32_t)mp_source_find(s, "f00.bin");
    f[1] = (uint32_t)mp_source_find(s, "f08.bin");
    uint8_t sha[2][32];
    memset(sha, 0xEE, sizeof sha);
    MP_CHECK(mp_readahead_open(s, f, 2, sha, &o, &ra, &e) == 0);
    MP_CHECK(mp_readahead_next(ra, &p, &e) == 0);
    mp_readahead_close(ra);
    char hex[65];
    mp_sha256_hex(sha[1], hex);
    MP_CHECK_STR(hex, "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855");
    MP_CHECK(memcmp(sha[0], sha[1], 32) == 0);
    mp_source_close(s);
}

// Ranges of files (the windows of a sample): the bytes of each range, its own SHA-256, offsets in
// the file; a range that goes past its file's end is refused before anything is read.
MP_TEST(readahead_ranges) {
    make_dir();
    mp_err e = {{0}};
    mp_source *s = NULL;
    if (mp_source_open(FX "/basic.exfat", &s, &e) != 0) {
        MP_FAIL_("%s", e.msg);
        return;
    }
    size_t nf = mp_source_count(s), n = 0;
    mp_readahead_range *rg = malloc(3 * nf * sizeof *rg);
    for (size_t i = nf; i-- > 0;) {   // backwards, up to three ranges per file, one of them empty
        uint64_t size = mp_source_entry(s, i)->size;
        rg[n++] = (mp_readahead_range){(uint32_t)i, size / 3, size / 2};
        rg[n++] = (mp_readahead_range){(uint32_t)i, size, 0};
        rg[n++] = (mp_readahead_range){(uint32_t)i, 0, size - size / 7};
    }
    uint8_t (*want)[32] = malloc(n * 32), (*got)[32] = malloc(n * 32);
    mp_sha256 wall;
    mp_sha256_init(&wall);
    uint8_t *b = malloc(1u << 20);
    for (size_t k = 0; k < n; k++) {
        mp_sha256 h;
        mp_sha256_init(&h);
        if (rg[k].len && (rg[k].len > (1u << 20) ||
                          mp_source_read(s, rg[k].file, rg[k].off, b, (size_t)rg[k].len, &e) != 0))
            MP_FAIL_("reference: %s", e.msg);
        mp_sha256_update(&h, b, (size_t)rg[k].len);
        mp_sha256_update(&wall, b, (size_t)rg[k].len);
        mp_sha256_final(&h, want[k]);
    }
    uint8_t wd[32], gd[32];
    mp_sha256_final(&wall, wd);
    for (unsigned t = 1; t <= 8; t *= 2) {
        mp_readahead_opts o = {t, 256 * KIB, MIB};
        mp_readahead *ra = NULL;
        MP_CHECK(mp_readahead_open_ranges(s, rg, n, got, &o, &ra, &e) == 0);
        mp_readahead_piece p;
        mp_sha256 gall;
        mp_sha256_init(&gall);
        size_t k = 0;
        uint64_t done = 0;
        int rc;
        while ((rc = mp_readahead_next(ra, &p, &e)) == 1) {
            while (k < n && done == rg[k].len) {
                k++;
                done = 0;
            }
            if (k >= n || p.item != k || p.off != rg[k].off + done) {
                MP_FAIL_("range piece out of order: item %zu off %llu", p.item, (unsigned long long)p.off);
                break;
            }
            done += p.len;
            mp_sha256_update(&gall, p.data, p.len);
        }
        mp_readahead_close(ra);
        mp_sha256_final(&gall, gd);
        MP_CHECK(rc == 0);
        MP_CHECK(memcmp(gd, wd, 32) == 0);
        MP_CHECK(memcmp(got, want, 32 * n) == 0);
    }
    // past the end of its file
    mp_readahead_range bad = {0, mp_source_entry(s, 0)->size, 1};
    mp_readahead_opts o = {1, MIB, MIB};
    mp_readahead *ra = NULL;
    MP_CHECK(mp_readahead_open_ranges(s, &bad, 1, NULL, &o, &ra, &e) == -1);
    MP_CHECK(ra == NULL);
    MP_CHECK_CONTAINS(e.msg, "read outside");
    free(b);
    free(want);
    free(got);
    free(rg);
    mp_source_close(s);
}

// A file that disappears after the source was opened: every piece before it comes out, then the
// error that mp_source_read gives for it, again on every later call.
MP_TEST(readahead_read_error) {
    make_dir();
    const char *copy = TMP "/vanish";
    for (unsigned t = 1; t <= 8; t *= 2) {
        mpt_clear(copy);
        mpt_copy_file(DIRSRC "/f05.bin", TMP "/vanish/a.bin");
        mpt_copy_file(DIRSRC "/f06.bin", TMP "/vanish/b.bin");
        mpt_copy_file(DIRSRC "/f07.bin", TMP "/vanish/c.bin");
        mpt_copy_file(DIRSRC "/f04.bin", TMP "/vanish/d.bin");
        mp_err e = {{0}};
        mp_source *s = NULL;
        if (mp_source_open(copy, &s, &e) != 0) {
            MP_FAIL_("%s", e.msg);
            return;
        }
        uint32_t f[4] = {2, 0, 1, 3};   // c, a, b, d: b goes third
        MP_CHECK(mp_remove_file(TMP "/vanish/b.bin") == 0);
        mp_err want = {{0}};
        uint8_t byte;
        MP_CHECK(mp_source_read(s, 1, 0, &byte, 1, &want) == -1);
        uint64_t before = mp_source_entry(s, 2)->size + mp_source_entry(s, 0)->size;
        mp_readahead_opts o = {t, 256 * KIB, 4 * MIB};
        uint8_t sha[4][32];
        mp_readahead *ra = NULL;
        MP_CHECK(mp_readahead_open(s, f, 4, sha, &o, &ra, &e) == 0);
        digest got;
        int rc = drain(ra, s, f, 4, &got, &e);
        MP_CHECK(rc == -1);
        MP_CHECK_EQ_U64(got.bytes, before);
        MP_CHECK_STR(e.msg, want.msg);
        MP_CHECK_CONTAINS(e.msg, "b.bin");
        mp_readahead_piece p;
        mp_err again = {{0}};
        MP_CHECK(mp_readahead_next(ra, &p, &again) == -1);
        MP_CHECK_STR(again.msg, want.msg);
        mp_readahead_close(ra);
        mp_source_close(s);
    }
}

// An image cut short: the same message as mp_source_read, for the same file.
MP_TEST(readahead_truncated_image) {
    mp_err e = {{0}};
    mp_source *s = NULL;
    if (mp_source_open(FX "/truncated_tail.exfat", &s, &e) != 0) {
        MP_FAIL_("%s", e.msg);
        return;
    }
    long bad = mp_source_find(s, "random.bin");
    MP_CHECK(bad >= 0);
    if (bad < 0) return;
    uint64_t size = mp_source_entry(s, (size_t)bad)->size;
    uint8_t *b = malloc((size_t)size);
    mp_err want = {{0}};
    MP_CHECK(b && mp_source_read(s, (size_t)bad, 0, b, (size_t)size, &want) == -1);
    MP_CHECK_CONTAINS(want.msg, "truncated");
    free(b);
    size_t n = mp_source_count(s);
    uint32_t *f = malloc(n * sizeof *f);
    for (size_t i = 0; i < n; i++) f[i] = (uint32_t)i;   // in path order: random.bin is not the last
    for (unsigned t = 1; t <= 4; t++) {
        mp_readahead_opts o = {t, 256 * KIB, 64 * MIB};
        mp_readahead *ra = NULL;
        MP_CHECK(mp_readahead_open(s, f, n, NULL, &o, &ra, &e) == 0);
        digest got;
        MP_CHECK(drain(ra, s, f, n, &got, &e) == -1);
        MP_CHECK_STR(e.msg, want.msg);
        mp_readahead_close(ra);
    }
    free(f);
    mp_source_close(s);
}

MP_TEST(readahead_cancel) {
    make_dir();
    mp_err e = {{0}};
    mp_source *s = NULL;
    if (mp_source_open(DIRSRC, &s, &e) != 0) {
        MP_FAIL_("%s", e.msg);
        return;
    }
    size_t n = 0;
    uint32_t *f = scrambled(s, &n);
    for (unsigned t = 1; t <= 8; t *= 2) {
        mp_readahead_opts o = {t, 256 * KIB, 256 * KIB};   // one piece in memory at a time
        mp_readahead *ra = NULL;
        mp_readahead_piece p;
        // cancelled before it starts: nothing is read
        mp_cancel_request();
        MP_CHECK(mp_readahead_open(s, f, n, NULL, &o, &ra, &e) == 0);
        MP_CHECK(mp_readahead_next(ra, &p, &e) == -1);
        MP_CHECK_STR(e.msg, "cancelled");
        mp_readahead_close(ra);
        mp_cancel_reset();
        // cancelled after the first piece: no more come
        MP_CHECK(mp_readahead_open(s, f, n, NULL, &o, &ra, &e) == 0);
        MP_CHECK(mp_readahead_next(ra, &p, &e) == 1);
        mp_cancel_request();
        int rc;
        while ((rc = mp_readahead_next(ra, &p, &e)) == 1) {
        }
        MP_CHECK(rc == -1);
        MP_CHECK_STR(e.msg, "cancelled");
        mp_readahead_close(ra);
        mp_cancel_reset();
    }
    free(f);
    mp_source_close(s);
}

// Closed while its threads are still reading (the consumer stopped): they stop, nothing hangs.
MP_TEST(readahead_close_early) {
    make_dir();
    mp_err e = {{0}};
    mp_source *s = NULL;
    if (mp_source_open(DIRSRC, &s, &e) != 0) {
        MP_FAIL_("%s", e.msg);
        return;
    }
    size_t n = 0;
    uint32_t *f = scrambled(s, &n);
    for (int i = 0; i < 100; i++) {
        mp_readahead_opts o = {1 + (unsigned)i % 16, 256 * KIB, (1 + (size_t)i % 3) * 256 * KIB};
        mp_readahead *ra = NULL;
        mp_readahead_piece p;
        MP_CHECK(mp_readahead_open(s, f, n, NULL, &o, &ra, &e) == 0);
        for (int k = 0; k < i % 4; k++) MP_CHECK(mp_readahead_next(ra, &p, &e) == 1);
        mp_readahead_close(ra);
    }
    free(f);
    mp_source_close(s);
}

// ---- pause -----------------------------------------------------------------------------------

// The reads of the source, counted: the one numbered pause_at asks for a pause.
static int (*real_read)(mp_source *s, size_t i, uint64_t off, void *buf, size_t len, mp_err *e);
static mp_mutex *reads_mu;
static int reads, pause_at;

static int counting_read(mp_source *s, size_t i, uint64_t off, void *buf, size_t len, mp_err *e) {
    mp_mutex_lock(reads_mu);
    int n = ++reads;
    mp_mutex_unlock(reads_mu);
    if (n == pause_at) mp_pause_set(1);
    return real_read(s, i, off, buf, len, e);
}

static int reads_now(void) {
    mp_mutex_lock(reads_mu);
    int n = reads;
    mp_mutex_unlock(reads_mu);
    return n;
}

// Whether `threads` threads wait in the pause (it gives them 10 s to get there).
static int parked(unsigned threads) {
    for (int i = 0; i < 2000; i++) {
        if (mp_pause_waiting() >= threads) return 1;
        mp_sleep_ms(5);
    }
    return 0;
}

MP_TEST(readahead_pauses) {
    make_dir();
    mp_err e = {{0}};
    mp_source *s = NULL;
    if (mp_source_open(DIRSRC, &s, &e) != 0) {
        MP_FAIL_("%s", e.msg);
        return;
    }
    size_t n = 0;
    uint32_t *f = scrambled(s, &n);
    digest ref;
    reference(s, f, n, &ref);
    reads_mu = mp_mutex_new();
    real_read = s->read;
    s->read = counting_read;
    mp_readahead_opts o = {4, 256 * KIB, 256 * MIB};   // room for everything: only the pause stops them
    mp_readahead *ra = NULL;
    mp_readahead_piece p;
    digest got;

    // paused before it opens: not one read until it is resumed, then every byte in order
    reads = 0;
    pause_at = 0;
    mp_pause_set(1);
    MP_CHECK(mp_readahead_open(s, f, n, NULL, &o, &ra, &e) == 0);
    MP_CHECK(parked(4));
    mp_sleep_ms(50);
    MP_CHECK_EQ_U64(reads_now(), 0);
    mp_pause_set(0);
    MP_CHECK(drain(ra, s, f, n, &got, &e) == 0);
    MP_CHECK(!memcmp(got.all, ref.all, 32));
    MP_CHECK_EQ_U64(got.bytes, ref.bytes);
    mp_readahead_close(ra);

    // paused at the third read: each thread ends the read it has and starts no other; nothing is lost
    reads = 0;
    pause_at = 3;
    MP_CHECK(mp_readahead_open(s, f, n, NULL, &o, &ra, &e) == 0);
    MP_CHECK(parked(4));
    int at = reads_now();
    MP_CHECK(at >= 3 && at <= 6);
    mp_sleep_ms(50);
    MP_CHECK_EQ_U64(reads_now(), at);
    mp_pause_set(0);
    MP_CHECK(drain(ra, s, f, n, &got, &e) == 0);
    MP_CHECK(!memcmp(got.all, ref.all, 32));
    mp_readahead_close(ra);

    // cancelled while paused: the consumer gets "cancelled"
    pause_at = 0;
    mp_pause_set(1);
    MP_CHECK(mp_readahead_open(s, f, n, NULL, &o, &ra, &e) == 0);
    MP_CHECK(parked(4));
    mp_cancel_request();
    MP_CHECK(mp_readahead_next(ra, &p, &e) == -1);
    MP_CHECK_STR(e.msg, "cancelled");
    mp_readahead_close(ra);
    mp_cancel_reset();

    // closed while paused (the consumer stopped): its threads go, nothing hangs
    MP_CHECK(mp_readahead_open(s, f, n, NULL, &o, &ra, &e) == 0);
    MP_CHECK(parked(4));
    mp_readahead_close(ra);
    MP_CHECK_EQ_U64(mp_pause_waiting(), 0);
    mp_pause_set(0);

    s->read = real_read;
    mp_mutex_free(reads_mu);
    free(ref.sha);
    free(f);
    mp_source_close(s);
}

int main(void) {
    mpt_clear(TMP);
    MP_RUN(readahead_same_bytes_exfat);
    MP_RUN(readahead_same_bytes_dir);
    MP_RUN(readahead_without_sha);
    MP_RUN(readahead_repeated);
    MP_RUN(readahead_nothing_to_read);
    MP_RUN(readahead_ranges);
    MP_RUN(readahead_read_error);
    MP_RUN(readahead_truncated_image);
    MP_RUN(readahead_cancel);
    MP_RUN(readahead_close_early);
    MP_RUN(readahead_pauses);
    mpt_clear(TMP);
    MP_TEST_MAIN_END();
}
