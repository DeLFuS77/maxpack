// SPDX-License-Identifier: GPL-3.0-or-later
// A game packed with its DLC (make_fpkg.py's mini game and its DLC): the source made of a game and other
// sources.
#include <stdlib.h>

#include "mp_fixture.h"
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

int main(void) {
    MP_RUN(merge_basic);
    MP_RUN(merge_twice);
    MP_TEST_MAIN_END();
}
