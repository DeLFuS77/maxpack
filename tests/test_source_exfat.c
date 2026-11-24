// SPDX-License-Identifier: GPL-3.0-or-later
#include <stdlib.h>
#include <zlib.h>

#include "mp_platform.h"
#include "mp_source.h"
#include "mp_test.h"

#define FX MP_FIXTURES_DIR

typedef struct {
    uint64_t size;
    unsigned long crc;
    int64_t mtime;
    char *path;
} man_line;

static char *slurp(const char *path, size_t *len) {
    mp_file *f = NULL;
    uint64_t size = 0;
    size_t got = 0;
    if (mp_file_open_read(path, &f, NULL) != 0) return NULL;
    mp_file_size(f, &size, NULL);
    char *buf = malloc((size_t)size + 1);
    mp_file_pread(f, buf, (size_t)size, 0, &got, NULL);
    mp_file_close(f);
    buf[got] = 0;
    *len = got;
    return buf;
}

static size_t load_manifest(man_line **out) {
    size_t len = 0, n = 0;
    char *buf = slurp(FX "/basic.manifest.tsv", &len);
    man_line *v = calloc(1024, sizeof *v);
    for (char *line = buf; line && *line;) {
        char *nl = strchr(line, '\n');
        if (nl) *nl = 0;
        char *t1 = strchr(line, '\t'), *t2 = t1 ? strchr(t1 + 1, '\t') : NULL,
             *t3 = t2 ? strchr(t2 + 1, '\t') : NULL;
        if (t3) {
            v[n].size = strtoull(line, NULL, 10);
            v[n].crc = strtoul(t1 + 1, NULL, 16);
            v[n].mtime = strtoll(t2 + 1, NULL, 10);
            v[n].path = t3 + 1;
            n++;
        }
        line = nl ? nl + 1 : NULL;
    }
    *out = v;
    return n;   // the buffer stays alive: the paths point into it
}

static size_t count_lines(const char *path) {
    size_t len = 0, n = 0;
    char *buf = slurp(path, &len);
    for (size_t i = 0; buf && i < len; i++) n += buf[i] == '\n';
    free(buf);
    return n;
}

static long find_entry(const mp_source *s, const char *path) {
    for (size_t i = 0; i < mp_source_count(s); i++)
        if (strcmp(mp_source_entry(s, i)->path, path) == 0) return (long)i;
    return -1;
}

static mp_source *open_basic(void) {
    mp_err e = {{0}};
    mp_source *s = NULL;
    if (mp_source_open(FX "/basic.exfat", &s, &e) != 0) MP_FAIL_("no abre basic.exfat: %s", e.msg);
    return s;
}

MP_TEST(exfat_lists_like_manifest) {
    man_line *m = NULL;
    size_t n = load_manifest(&m);
    mp_source *s = open_basic();
    MP_CHECK(s != NULL);
    if (!s) return;
    MP_CHECK(mp_source_kind_of(s) == MP_SRC_EXFAT);
    MP_CHECK_EQ_U64(mp_source_count(s), n);
    for (size_t i = 0; i < n && i < mp_source_count(s); i++) {
        MP_CHECK_STR(mp_source_entry(s, i)->path, m[i].path);
        MP_CHECK_EQ_U64(mp_source_entry(s, i)->size, m[i].size);
    }
    MP_CHECK_EQ_U64(mp_source_dir_count(s), count_lines(FX "/basic.dirs.tsv"));
    mp_source_close(s);
}

MP_TEST(exfat_contents_match) {
    man_line *m = NULL;
    size_t n = load_manifest(&m);
    mp_source *s = open_basic();
    if (!s) return;
    unsigned char *buf = malloc(65536);
    for (size_t i = 0; i < n && i < mp_source_count(s); i++) {
        uint64_t size = mp_source_entry(s, i)->size, off = 0;
        uLong crc = crc32(0, Z_NULL, 0);
        mp_err e = {{0}};
        int ok = 1;
        while (off < size) {
            size_t k = size - off > 65536 ? 65536 : (size_t)(size - off);
            if (mp_source_read(s, i, off, buf, k, &e) != 0) {
                ok = 0;
                break;
            }
            crc = crc32(crc, buf, (uInt)k);
            off += k;
        }
        if (!ok || crc != m[i].crc) MP_FAIL_("different content in %s (%s)", m[i].path, e.msg);
    }
    free(buf);
    mp_source_close(s);
}

MP_TEST(exfat_vdl_zero_fill) {
    mp_source *s = open_basic();
    if (!s) return;
    long i = find_entry(s, "data/vdl.bin");
    MP_CHECK(i >= 0);
    unsigned char buf[15000];
    memset(buf, 0x55, sizeof buf);
    mp_err e = {{0}};
    MP_CHECK(i >= 0 && mp_source_read(s, (size_t)i, 5000, buf, sizeof buf, &e) == 0);
    int zero = 1;
    for (size_t k = 0; k < sizeof buf; k++) zero &= buf[k] == 0;
    MP_CHECK(zero);
    mp_source_close(s);
}

MP_TEST(exfat_mtime) {
    mp_source *s = open_basic();
    if (!s) return;
    long i = find_entry(s, "eboot.bin");
    MP_CHECK(i >= 0);
    if (i >= 0) MP_CHECK_EQ_U64(mp_source_entry(s, (size_t)i)->mtime, 1714979290);
    mp_source_close(s);
}

MP_TEST(exfat_truncated_head_fails) {
    mp_err e = {{0}};
    mp_source *s = NULL;
    MP_CHECK(mp_source_open(FX "/truncated_head.exfat", &s, &e) == -1);
    MP_CHECK_CONTAINS(e.msg, "truncated");
}

MP_TEST(exfat_truncated_read_fails) {
    mp_err e = {{0}};
    mp_source *s = NULL;
    MP_CHECK(mp_source_open(FX "/truncated_tail.exfat", &s, &e) == 0);
    if (!s) return;
    long i = find_entry(s, "random.bin");
    MP_CHECK(i >= 0);
    unsigned char buf[1000];
    MP_CHECK(i >= 0 && mp_source_read(s, (size_t)i, 599000, buf, sizeof buf, &e) == -1);
    MP_CHECK_CONTAINS(e.msg, "truncated");
    mp_source_close(s);
}

MP_TEST(exfat_fatloop_fails) {
    mp_err e = {{0}};
    mp_source *s = NULL;
    MP_CHECK(mp_source_open(FX "/fatloop.exfat", &s, &e) == -1);
    MP_CHECK_CONTAINS(e.msg, "FAT");
}

MP_TEST(exfat_huge_length_rejected) {
    mp_err e = {{0}};
    mp_source *s = NULL;
    MP_CHECK(mp_source_open(FX "/huge_len.exfat", &s, &e) == -1);
    MP_CHECK_CONTAINS(e.msg, "larger than the volume");
    mp_source_close(s);
}

MP_TEST(exfat_huge_dir_rejected) {
    mp_err e = {{0}};
    mp_source *s = NULL;
    MP_CHECK(mp_source_open(FX "/bigdir.exfat", &s, &e) == -1);
    MP_CHECK_CONTAINS(e.msg, "too large");
    mp_source_close(s);
}

MP_TEST(source_find_all) {
    mp_source *s = open_basic();
    if (!s) return;
    size_t bad = 0;
    for (size_t i = 0; i < mp_source_count(s); i++) bad += mp_source_find(s, mp_source_entry(s, i)->path) != (long)i;
    MP_CHECK_EQ_U64(bad, 0);
    MP_CHECK(mp_source_find(s, "no/existe") == -1);
    mp_source_close(s);
}

int main(void) {
    MP_RUN(source_find_all);
    MP_RUN(exfat_lists_like_manifest);
    MP_RUN(exfat_contents_match);
    MP_RUN(exfat_vdl_zero_fill);
    MP_RUN(exfat_mtime);
    MP_RUN(exfat_truncated_head_fails);
    MP_RUN(exfat_truncated_read_fails);
    MP_RUN(exfat_fatloop_fails);
    MP_RUN(exfat_huge_length_rejected);
    MP_RUN(exfat_huge_dir_rejected);
    MP_TEST_MAIN_END();
}
