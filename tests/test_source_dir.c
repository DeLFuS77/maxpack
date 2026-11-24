// SPDX-License-Identifier: GPL-3.0-or-later
#include <stdlib.h>

#include "mp_platform.h"
#include "mp_source.h"
#include "mp_test.h"

#define TMP MP_TEST_TMP "/source_dir"

MP_TEST(dir_entries_sorted) {
    mp_err e = {{0}};
    // "c/x" and not "b/x": on NTFS and APFS the folder "b" and the file "B" would clash.
    MP_CHECK(mp_make_dirs(TMP "/sorted/c", &e) == 0);
    MP_CHECK(mp_write_file(TMP "/sorted/c/x", "12345", 5, &e) == 0);
    MP_CHECK(mp_write_file(TMP "/sorted/a", "1", 1, &e) == 0);
    MP_CHECK(mp_write_file(TMP "/sorted/B", "12", 2, &e) == 0);
    mp_source *s = NULL;
    MP_CHECK(mp_source_open(TMP "/sorted", &s, &e) == 0);
    if (!s) return;
    MP_CHECK(mp_source_kind_of(s) == MP_SRC_DIR);
    MP_CHECK_EQ_U64(mp_source_count(s), 3);
    if (mp_source_count(s) != 3) {
        mp_source_close(s);
        return;
    }
    MP_CHECK_STR(mp_source_entry(s, 0)->path, "B");
    MP_CHECK_STR(mp_source_entry(s, 1)->path, "a");
    MP_CHECK_STR(mp_source_entry(s, 2)->path, "c/x");
    MP_CHECK_EQ_U64(mp_source_entry(s, 0)->size, 2);
    MP_CHECK_EQ_U64(mp_source_entry(s, 2)->size, 5);
    MP_CHECK_EQ_U64(mp_source_dir_count(s), 1);
    mp_source_close(s);
}

MP_TEST(dir_read_exact) {
    mp_err e = {{0}};
    unsigned char data[1000], buf[50];
    for (int i = 0; i < 1000; i++) data[i] = (unsigned char)(i * 13);
    MP_CHECK(mp_make_dirs(TMP "/read", &e) == 0);
    MP_CHECK(mp_write_file(TMP "/read/data.bin", data, sizeof data, &e) == 0);
    mp_source *s = NULL;
    MP_CHECK(mp_source_open(TMP "/read", &s, &e) == 0);
    if (!s) return;
    MP_CHECK(mp_source_read(s, 0, 100, buf, 50, &e) == 0);
    MP_CHECK(memcmp(buf, data + 100, 50) == 0);
    mp_err e2 = {{0}};
    MP_CHECK(mp_source_read(s, 0, 990, buf, 20, &e2) == -1);
    MP_CHECK(e2.msg[0] != 0);
    mp_source_close(s);
}

MP_TEST(open_rejects_pkg) {
    mp_err e = {{0}};
    unsigned char pkg[64] = {0x7F, 0x46, 0x49, 0x48};
    MP_CHECK(mp_make_dirs(TMP, &e) == 0);
    MP_CHECK(mp_write_file(TMP "/fake.pkg", pkg, sizeof pkg, &e) == 0);
    mp_source *s = NULL;
    MP_CHECK(mp_source_open(TMP "/fake.pkg", &s, &e) == -1);
    MP_CHECK_CONTAINS(e.msg, "PKG");
}

MP_TEST(open_rejects_unknown) {
    mp_err e = {{0}};
    unsigned char junk[64];
    for (int i = 0; i < 64; i++) junk[i] = (unsigned char)(i * 37 + 11);
    MP_CHECK(mp_make_dirs(TMP, &e) == 0);
    MP_CHECK(mp_write_file(TMP "/junk.bin", junk, sizeof junk, &e) == 0);
    mp_source *s = NULL;
    MP_CHECK(mp_source_open(TMP "/junk.bin", &s, &e) == -1);
    MP_CHECK_CONTAINS(e.msg, ".exfat");
}

MP_TEST(open_missing) {
    mp_err e = {{0}};
    mp_source *s = NULL;
    MP_CHECK(mp_source_open(TMP "/no-existe.exfat", &s, &e) == -1);
    MP_CHECK_CONTAINS(e.msg, "does not exist");
}

int main(void) {
    MP_RUN(dir_entries_sorted);
    MP_RUN(dir_read_exact);
    MP_RUN(open_rejects_pkg);
    MP_RUN(open_rejects_unknown);
    MP_RUN(open_missing);
    MP_TEST_MAIN_END();
}
