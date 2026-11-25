// SPDX-License-Identifier: GPL-3.0-or-later
#include "mp_complete.h"

#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "mp_platform.h"
#include "mp_playgo.h"

#define RIGHT_SPRX "sce_sys/about/right.sprx"

static int lower(int c) { return c >= 'A' && c <= 'Z' ? c + 32 : c; }

// a[0..n) equals the NUL-terminated b, apart from ASCII case
static int ieq_n(const char *a, size_t n, const char *b) {
    for (size_t i = 0; i < n; i++, b++)
        if (!*b || lower((unsigned char)a[i]) != lower((unsigned char)*b)) return 0;
    return !*b;
}

static int ends_ci(const char *s, size_t n, const char *tail) {
    size_t k = strlen(tail);
    return n >= k && ieq_n(s + n - k, k, tail);
}

// The first segment of path is name, and something follows it (a folder at the root).
static int in_root_folder(const char *path, const char *name) {
    const char *slash = strchr(path, '/');
    return slash && slash[1] && ieq_n(path, (size_t)(slash - path), name);
}

int mp_complete_is_junk(const char *path) {
    static const char *const folders[] = {"__macosx", ".spotlight-v100", ".fseventsd", ".trashes", ".temporaryitems",
                                          ".appledouble", "$recycle.bin", "system volume information"};
    static const char *const files[] = {".ds_store", "thumbs.db", "desktop.ini", "ehthumbs.db", "ehthumbs_vista.db"};
    const char *seg = path;
    for (;;) {
        const char *slash = strchr(seg, '/');
        size_t len = slash ? (size_t)(slash - seg) : strlen(seg);
        if (slash) {
            for (size_t i = 0; i < sizeof folders / sizeof *folders; i++)
                if (ieq_n(seg, len, folders[i])) return 1;
            seg = slash + 1;
            continue;
        }
        if (len >= 2 && seg[0] == '.' && seg[1] == '_') return 1;   // AppleDouble
        for (size_t i = 0; i < sizeof files / sizeof *files; i++)
            if (ieq_n(seg, len, files[i])) return 1;
        return 0;
    }
}

int mp_complete_is_leftover(const char *path) {
    size_t n = strlen(path);
    if (in_root_folder(path, "decrypted")) return 1;
    if (ends_ci(path, n, ".auth_info")) return 1;
    return ieq_n(path, n, "entitlements.txt") || ieq_n(path, n, "entitlement_key.dat");
}

static long find_hash(const uint64_t *h, size_t n, uint64_t want) {
    size_t lo = 0, hi = n;
    while (lo < hi) {
        size_t mid = lo + (hi - lo) / 2;
        if (h[mid] < want)
            lo = mid + 1;
        else if (h[mid] > want)
            hi = mid;
        else
            return (long)mid;
    }
    return -1;
}

void mp_complete_free(mp_complete *c) {
    if (!c) return;
    for (size_t i = 0; i < c->npaths; i++) free(c->paths[i]);
    c->npaths = 0;
}

void mp_complete_counts_only(mp_complete *c) { mp_complete_free(c); }

int mp_complete_check(const mp_cfile *files, size_t n, const uint8_t *table, size_t table_len, mp_complete *c,
                      uint8_t *cls) {
    memset(c, 0, sizeof *c);
    uint64_t *h = NULL;
    size_t k = 0;
    const char *why = "no_table";
    if (table && mp_playgo_parse(table, table_len, &h, &k, &why) != 0) {
        if (!strcmp(why, "unreadable")) return -1;   // out of memory
        h = NULL;
    }
    c->checkable = h != NULL;
    c->reason = c->checkable ? "" : why;
    uint8_t *seen = NULL;
    long right = -1;
    if (c->checkable) {
        seen = calloc(k, 1);
        if (!seen) {
            free(h);
            return -1;
        }
        right = find_hash(h, k, mp_flt_hash(RIGHT_SPRX));
        c->listed = k - (right >= 0);
    }
    for (size_t i = 0; i < n; i++) {
        const char *p = files[i].path;
        long at = c->checkable ? find_hash(h, k, mp_flt_hash(p)) : -1;
        int cl;
        if (at >= 0) {
            cl = MP_CF_LISTED;
            seen[at] = 1;
        } else if (mp_complete_is_junk(p) || in_root_folder(p, "sce_sys")) {
            cl = MP_CF_IGNORED;
        } else if (mp_complete_is_leftover(p)) {
            cl = MP_CF_LEFTOVER;
        } else {
            cl = c->checkable ? MP_CF_EXTRA : MP_CF_IGNORED;
        }
        if (cls) cls[i] = (uint8_t)cl;
        if (cl == MP_CF_LEFTOVER) {
            c->leftover_count++;
            c->leftover_bytes += files[i].size;
        }
        if (!c->checkable || (cl != MP_CF_EXTRA && cl != MP_CF_LEFTOVER)) continue;
        c->extra_count++;
        c->extra_bytes += files[i].size;
        if (c->npaths < MP_COMPLETE_PATHS) {
            char *copy = mp_strdup(p);
            if (!copy) {
                free(seen);
                free(h);
                mp_complete_free(c);
                return -1;
            }
            c->paths[c->npaths++] = copy;
        }
    }
    for (size_t i = 0; c->checkable && i < k; i++)
        if (seen[i] && (long)i != right) c->present++;
    c->missing = c->listed - c->present;
    free(seen);
    free(h);
    return 0;
}

static long find_table(mp_source *s) {
    long i = mp_source_find(s, MP_PLAYGO_TABLE_PATH);
    if (i >= 0) return i;
    size_t n = strlen(MP_PLAYGO_TABLE_PATH);
    for (size_t k = 0; k < mp_source_count(s); k++)
        if (ieq_n(MP_PLAYGO_TABLE_PATH, n, mp_source_entry(s, k)->path)) return (long)k;
    return -1;
}

int mp_complete_source(mp_source *s, mp_complete *c, uint8_t *cls, mp_err *e) {
    size_t n = mp_source_count(s);
    mp_cfile *files = malloc((n ? n : 1) * sizeof *files);
    if (!files) {
        mp_err_set(e, "out of memory");
        return -1;
    }
    for (size_t i = 0; i < n; i++) {
        files[i].path = mp_source_entry(s, i)->path;
        files[i].size = mp_source_entry(s, i)->size;
    }
    uint8_t *table = NULL;
    size_t len = 0;
    int unreadable = 0;
    long t = find_table(s);
    if (t >= 0) {
        uint64_t size = mp_source_entry(s, (size_t)t)->size;
        table = size <= MP_PLAYGO_TABLE_MAX ? malloc((size_t)size + 1) : NULL;
        mp_err re = {{0}};
        if (!table || mp_source_read(s, (size_t)t, 0, table, (size_t)size, &re) != 0) {
            free(table);
            table = NULL;
            unreadable = 1;
        } else {
            len = (size_t)size;
        }
    }
    int rc = mp_complete_check(files, n, table, len, c, cls);
    if (rc == 0 && unreadable) c->reason = "unreadable";
    if (rc != 0) mp_err_set(e, "out of memory");
    free(table);
    free(files);
    return rc;
}

void mp_complete_json(const mp_complete *c, int with_paths, mp_jbuf *b) {
    mp_jbuf_raw(b, "{\"checkable\":");
    mp_jbuf_raw(b, c->checkable ? "true" : "false");
    mp_jbuf_raw(b, ",\"reason\":");
    mp_jbuf_str(b, c->reason ? c->reason : "");
    static const char *const names[] = {"listed", "present", "missing", "extra_count", "extra_bytes", "leftover_count",
                                        "leftover_bytes"};
    const uint64_t values[] = {c->listed, c->present, c->missing, c->extra_count, c->extra_bytes, c->leftover_count,
                               c->leftover_bytes};
    for (size_t i = 0; i < sizeof names / sizeof *names; i++) {
        mp_jbuf_raw(b, ",\"");
        mp_jbuf_raw(b, names[i]);
        mp_jbuf_raw(b, "\":");
        mp_jbuf_u64(b, values[i]);
    }
    if (with_paths) {
        mp_jbuf_raw(b, ",\"extra_paths\":[");
        for (size_t i = 0; i < c->npaths; i++) {
            if (i) mp_jbuf_raw(b, ",");
            mp_jbuf_str(b, c->paths[i]);
        }
        mp_jbuf_raw(b, "]");
    }
    mp_jbuf_raw(b, "}");
}

static const char *bytes_text(uint64_t v, char buf[32]) {
    static const char *const units[] = {"B", "KB", "MB", "GB", "TB"};
    double x = (double)v;
    int u = 0;
    while (x >= 1000.0 && u < 4) {
        x /= 1000.0;
        u++;
    }
    if (u == 0)
        snprintf(buf, 32, "%llu B", (unsigned long long)v);
    else
        snprintf(buf, 32, "%.2f %s", x, units[u]);
    return buf;
}

void mp_complete_summary(const mp_complete *c, char *buf, size_t cap) {
    if (c->checkable && !c->missing) {
        snprintf(buf, cap, "complete: all %llu files of the original package", (unsigned long long)c->listed);
    } else if (c->checkable) {
        snprintf(buf, cap, "incomplete: %llu of the %llu files of the original package %s missing",
                 (unsigned long long)c->missing, (unsigned long long)c->listed, c->missing == 1 ? "is" : "are");
    } else {
        const char *r = c->reason ? c->reason : "";
        const char *why = !strcmp(r, "unknown_format") ? "its " MP_PLAYGO_TABLE_PATH " is of a format MaxPack does not know"
                          : !strcmp(r, "damaged")      ? "its " MP_PLAYGO_TABLE_PATH " is damaged"
                          : !strcmp(r, "unreadable")   ? "its " MP_PLAYGO_TABLE_PATH " could not be read"
                                                       : "it has no list of the original package's files (" MP_PLAYGO_TABLE_PATH ")";
        snprintf(buf, cap, "not checked: %s", why);
    }
}

void mp_complete_extra_line(const mp_complete *c, char *buf, size_t cap) {
    char a[32], b[32];
    buf[0] = 0;
    if (c->checkable && c->extra_count && c->leftover_count)
        snprintf(buf, cap, "%llu files are not in the original package (%s); %s of them is what dump tools leave behind",
                 (unsigned long long)c->extra_count, bytes_text(c->extra_bytes, a), bytes_text(c->leftover_bytes, b));
    else if (c->checkable && c->extra_count)
        snprintf(buf, cap, "%llu %s not in the original package (%s)", (unsigned long long)c->extra_count,
                 c->extra_count == 1 ? "file is" : "files are", bytes_text(c->extra_bytes, a));
    else if (!c->checkable && c->leftover_count)
        snprintf(buf, cap, "%llu %s (%s) %s what dump tools leave behind, but with no list nothing proves %s not the game's",
                 (unsigned long long)c->leftover_count, c->leftover_count == 1 ? "file" : "files",
                 bytes_text(c->leftover_bytes, a), c->leftover_count == 1 ? "looks like" : "look like",
                 c->leftover_count == 1 ? "it is" : "they are");
}
