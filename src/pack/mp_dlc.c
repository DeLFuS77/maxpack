// SPDX-License-Identifier: GPL-3.0-or-later
// A game's DLC packed with it (mp_dlc.h).
#include "mp_dlc.h"

#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "mp_param.h"
#include "mp_platform.h"
#include "mp_rules.h"

#define PARAM_PATH "sce_sys/param.json"
#define PARAM_MAX (1u << 20)

static int lower(int c) { return c >= 'A' && c <= 'Z' ? c + 32 : c; }

static int ieq(const char *a, const char *b) {
    for (;; a++, b++) {
        if (lower((unsigned char)*a) != lower((unsigned char)*b)) return 0;
        if (!*a) return 1;
    }
}

// The first segment of path is name (ASCII case aside), with something after it.
static int in_folder(const char *path, const char *name) {
    size_t k = strlen(name);
    for (size_t i = 0; i < k; i++)
        if (lower((unsigned char)path[i]) != lower((unsigned char)name[i])) return 0;
    return path[k] == '/' && path[k + 1];
}

static long find_ci(mp_source *s, const char *path) {
    long i = mp_source_find(s, path);
    if (i >= 0) return i;
    for (size_t k = 0; k < mp_source_count(s); k++)
        if (ieq(mp_source_entry(s, k)->path, path)) return (long)k;
    return -1;
}

void mp_dlc_list_free(mp_dlc_list *l) {
    if (!l) return;
    free(l->v);
    l->v = NULL;
    l->n = 0;
}

int mp_dlc_content_id_ok(const char *cid) {
    if (strlen(cid) != 36 || cid[6] != '-' || cid[16] != '_' || cid[19] != '-') return 0;
    for (int i = 0; i < 36; i++) {
        unsigned char c = (unsigned char)cid[i];
        if (i == 6 || i == 16 || i == 19) continue;
        if (!((c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9'))) return 0;
    }
    return 1;
}

char *mp_dlc_ini(const mp_dlc *v, size_t n, size_t *len) {
    size_t cap = 1;
    for (size_t i = 0; i < n; i++) cap += 128;
    char *b = malloc(cap);
    if (!b) return NULL;
    size_t at = 0;
    b[0] = 0;
    for (size_t i = 0; i < n; i++) {
        int w = v[i].data ? snprintf(b + at, cap - at, "%s[PSAC]\ncontent_id=%s\nmount_point=/app0/%s\n", i ? "\n" : "",
                                     v[i].content_id, v[i].folder)
                          : snprintf(b + at, cap - at, "%s[PSAL]\ncontent_id=%s\ndownload_status=NO_EXTRA_DATA\n",
                                     i ? "\n" : "", v[i].content_id);
        if (w < 0 || (size_t)w >= cap - at) {
            free(b);
            return NULL;
        }
        at += (size_t)w;
    }
    *len = at;
    return b;
}

// The bytes of a source's param.json (malloc, NUL-terminated), or NULL when it has none or it cannot
// be read.
static char *read_param(mp_source *s, size_t *len) {
    long i = find_ci(s, PARAM_PATH);
    *len = 0;
    if (i < 0) return NULL;
    uint64_t size = mp_source_entry(s, (size_t)i)->size;
    char *b = size <= PARAM_MAX ? malloc((size_t)size + 1) : NULL;
    if (!b || mp_source_read(s, (size_t)i, 0, b, (size_t)size, NULL) != 0) {
        free(b);
        return NULL;
    }
    b[size] = 0;
    *len = (size_t)size;
    return b;
}

// The title part of a content ID ("PPSA00000" of "UP0000-PPSA00000_00-..."), "" when it has none.
static void title_of(const char *cid, char out[16]) {
    out[0] = 0;
    if (strlen(cid) >= 16 && cid[6] == '-') {
        memcpy(out, cid + 7, 9);
        out[9] = 0;
    }
}

// The game's title ID: its param.json's titleId, else the title part of its content ID.
static void game_title(mp_source *game, char out[16]) {
    size_t len = 0;
    char *pj = read_param(game, &len), id[16] = "", ver[16], name[128], cid[48] = "";
    if (pj) {
        mp_param_read(pj, len, id, ver, name);
        mp_param_content_id(pj, len, cid);
        free(pj);
    }
    if (!cid[0]) snprintf(cid, sizeof cid, "%s", mp_source_package_id(game));
    if (id[0])
        snprintf(out, 16, "%s", id);
    else
        title_of(cid, out);
}

// Whether a DLC has files of its own: one outside sce_sys/ that is not the OS's.
static int has_data(mp_source *s) {
    for (size_t i = 0; i < mp_source_count(s); i++) {
        const char *p = mp_source_entry(s, i)->path;
        if (!in_folder(p, "sce_sys") && !mp_complete_is_junk(p)) return 1;
    }
    return 0;
}

// Checks one DLC and gives its content ID (cid) and whether it has data: 0, or -1 with e set.
static int check_dlc(mp_source *s, const char *path, const char *title, char cid[37], int *data, mp_err *e) {
    if (!mp_source_is_dlc_package(s)) {
        if (find_ci(s, PARAM_PATH) < 0) {
            mp_err_set(e, "%s: not a DLC: it has no sce_sys/param.json", path);
            return -1;
        }
        if (find_ci(s, "eboot.bin") >= 0) {
            mp_err_set(e, "%s: a game, not a DLC: it has eboot.bin", path);
            return -1;
        }
    }
    for (size_t i = 0; i < mp_source_count(s); i++) {
        const char *p = mp_source_entry(s, i)->path;
        if (ieq(p, "ampr_zstd.index") || mp_glob_match("ampr_zstd-*.pak", p)) {
            mp_err_set(e, "%s: this DLC is packed already (it has %s): give it as it was dumped", path, p);
            return -1;
        }
    }
    char found[48] = "";
    if (mp_source_is_dlc_package(s)) {
        snprintf(found, sizeof found, "%s", mp_source_package_id(s));
    } else {
        size_t len = 0;
        char *pj = read_param(s, &len);
        if (pj) mp_param_content_id(pj, len, found);
        free(pj);
        if (!found[0]) {
            mp_err_set(e, "%s: its sce_sys/param.json gives no contentId", path);
            return -1;
        }
    }
    if (!mp_dlc_content_id_ok(found)) {
        mp_err_set(e, "%s: its content ID \"%s\" is not one dlc_emu takes: 36 characters, as "
                      "UP0000-PPSA00000_00-ABCDEFGHIJKLMNOP",
                   path, found);
        return -1;
    }
    char dt[16];
    title_of(found, dt);
    if (strcmp(dt, title)) {
        mp_err_set(e, "%s: a DLC of another game: its content ID %s is of %s, and the game is %s", path, found, dt, title);
        return -1;
    }
    memcpy(cid, found, 37);
    *data = has_data(s);
    return 0;
}

int mp_dlc_attach(mp_source **game, const char *const *paths, size_t n, mp_dlc_list *out, const char **code,
                  mp_err *e) {
    *code = "dlc_refused";
    if (out) memset(out, 0, sizeof *out);
    mp_source *g = *game;
    *game = NULL;
    mp_source **src = calloc(n ? n : 1, sizeof *src);
    mp_dlc *v = calloc(n ? n : 1, sizeof *v);
    mp_source **parts = calloc(n ? n : 1, sizeof *parts);
    char (*folders)[16] = calloc(n ? n : 1, sizeof *folders);
    const char **prefixes = calloc(n ? n : 1, sizeof *prefixes);
    char *ini = NULL;
    size_t ini_len = 0, nparts = 0;
    int rc = -1;
    if (!src || !v || !parts || !folders || !prefixes) {
        mp_err_set(e, "out of memory");
        goto done;
    }
    // the game: a list of its own is not mixed with MaxPack's (dlc_emu shows only the DLC of its file)
    for (size_t i = 0; i < mp_source_count(g); i++)
        if (ieq(mp_source_entry(g, i)->path, MP_DLC_INI)) {
            *code = "dlc_has_ini";
            mp_err_set(e, "the game already has a dlc_emu.ini: MaxPack writes it for the DLC it packs with the game "
                          "and does not mix two lists (dlc_emu shows only the DLC of its file); take it out of the "
                          "dump, or pack it without --dlc");
            goto done;
        }
    char title[16];
    game_title(g, title);
    if (!title[0]) {
        mp_err_set(e, "the game gives no title ID (its sce_sys/param.json has neither titleId nor contentId), so "
                      "its DLC cannot be told");
        goto done;
    }
    size_t ndata = 0;
    for (size_t k = 0; k < n; k++) {
        if (mp_source_open_as(paths[k], MP_OPEN_DLC, &src[k], e) != 0) goto done;
        if (check_dlc(src[k], paths[k], title, v[k].content_id, &v[k].data, e) != 0) goto done;
        for (size_t j = 0; j < k; j++) {
            if (!strcmp(v[j].content_id, v[k].content_id)) {
                mp_err_set(e, "%s and %s: the same content ID (%s): give each DLC once", paths[j], paths[k], v[k].content_id);
                goto done;
            }
            if (!strcmp(v[j].content_id + 20, v[k].content_id + 20)) {
                mp_err_set(e, "%s and %s: the same label %s (the end of their content IDs), which dlc_emu takes once",
                           paths[j], paths[k], v[k].content_id + 20);
                goto done;
            }
        }
        if (v[k].data) {
            if (ndata == MP_DLC_MAX_DATA) {
                *code = "dlc_too_many";
                mp_err_set(e, "more than %d DLC with files of their own: dlc_emu takes at most %d (addcont0 to addcont%d)",
                           MP_DLC_MAX_DATA, MP_DLC_MAX_DATA, MP_DLC_MAX_DATA - 1);
                goto done;
            }
            snprintf(v[k].folder, sizeof v[k].folder, "addcont%u", (unsigned)ndata);
            for (size_t i = 0; i < mp_source_count(src[k]); i++) {
                v[k].files++;
                v[k].bytes += mp_source_entry(src[k], i)->size;
            }
            ndata++;
        }
    }
    if (n > MP_DLC_MAX_ENTRIES || !(ini = mp_dlc_ini(v, n, &ini_len)) || ini_len > MP_DLC_INI_MAX) {
        if (n <= MP_DLC_MAX_ENTRIES && !ini) {
            mp_err_set(e, "out of memory");
        } else {
            *code = "dlc_too_many";
            mp_err_set(e, "too many DLC: their dlc_emu.ini would take %zu bytes, and dlc_emu reads at most 32 KiB",
                       n > MP_DLC_MAX_ENTRIES ? n * 80 : ini_len);
        }
        goto done;
    }
    // where their folders go must be free in the game
    for (size_t i = 0; i < mp_source_count(g); i++) {
        const char *p = mp_source_entry(g, i)->path;
        for (size_t k = 0; k < n; k++) {
            if (!v[k].data || !in_folder(p, v[k].folder)) continue;
            mp_err_set(e, "the game already has %.*s/, where its DLC %s would go", (int)strlen(v[k].folder), p,
                       v[k].content_id);
            goto done;
        }
    }
    int64_t mtime = 0;
    for (size_t i = 0; i < mp_source_count(g); i++)
        if (mp_source_entry(g, i)->mtime > mtime) mtime = mp_source_entry(g, i)->mtime;
    for (size_t k = 0; k < n; k++)
        for (size_t i = 0; i < mp_source_count(src[k]); i++)
            if (mp_source_entry(src[k], i)->mtime > mtime) mtime = mp_source_entry(src[k], i)->mtime;
    for (size_t k = 0; k < n; k++) {
        if (v[k].data) {
            memcpy(folders[nparts], v[k].folder, sizeof folders[nparts]);
            prefixes[nparts] = folders[nparts];
            parts[nparts++] = src[k];
        } else {
            mp_source_close(src[k]);
        }
        src[k] = NULL;
    }
    mp_source *m = NULL;
    rc = mp_source_merge(g, parts, prefixes, nparts, MP_DLC_INI, (const uint8_t *)ini, ini_len, mtime, &m, e);
    g = NULL;   // taken, with the parts
    nparts = 0;
    if (rc == 0) {
        *game = m;
        *code = NULL;
        if (out) {
            out->v = v;
            out->n = n;
            v = NULL;
        }
    }
done:
    mp_source_close(g);
    for (size_t k = 0; src && k < n; k++) mp_source_close(src[k]);
    free(src);
    free(v);
    free(parts);
    free(folders);
    free(prefixes);
    free(ini);
    return rc;
}
