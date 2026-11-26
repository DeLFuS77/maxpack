// SPDX-License-Identifier: GPL-3.0-or-later
// add and update (mp_grow.h): the new dump each one makes of a .p5z, for the grow engine.
//   add:    the dump the .p5z was made of, its DLC and the new ones (addcontN/ after its last), with the
//           dlc_emu.ini written again; the game's files are taken from the .p5z itself, never read.
//   update: the new dump given, of the same game in another version, with the DLC the .p5z carries.
#include "mp_grow.h"

#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "mp_dlc.h"
#include "mp_p5z_internal.h"
#include "mp_param.h"

static int lower(int c) { return c >= 'A' && c <= 'Z' ? c + 32 : c; }

static int ieq(const char *a, const char *b) {
    for (;; a++, b++) {
        if (lower((unsigned char)*a) != lower((unsigned char)*b)) return 0;
        if (!*a) return 1;
    }
}

static long find_manifest(const mp_p5z *p, const char *path) {
    size_t lo = 0, hi = p->man.n;
    while (lo < hi) {
        size_t mid = lo + (hi - lo) / 2;
        int c = strcmp(p->man.v[mid].path, path);
        if (c == 0) return (long)mid;
        if (c < 0)
            lo = mid + 1;
        else
            hi = mid;
    }
    return -1;
}

static int is_ini(void *ctx, const char *path) {
    (void)ctx;
    return ieq(path, MP_DLC_INI);
}

static int not_ini(void *ctx, const char *path) { return !is_ini(ctx, path); }

// A file taken from the .p5z itself is the one of its manifest: its SHA-256 is known, never read.
static int known_old(void *ctx, const char *path, uint64_t size, uint8_t sha[32]) {
    const mp_p5z *p = ctx;
    long k = find_manifest(p, path);
    if (k < 0 || p->man.v[k].size != size || is_ini(NULL, path)) return 0;
    memcpy(sha, p->man.v[k].sha, 32);
    return 1;
}

static int open_recovered(const char *p5z, mp_p5z **p, mp_grow_result *r, mp_err *e) {
    int what = 0;
    if (mp_p5z_recover(p5z, &what, e) != 0) return -1;
    if (what == MP_RECOVER_BUSY) {
        r->code = "busy";
        mp_err_set(e, "another MaxPack is changing %s now", p5z);
        return -1;
    }
    return mp_p5z_open(p5z, p, e);
}

// The DLC a .p5z carries, from its dlc_emu.ini (none when it has none). -1 with *code "dlc_has_ini" when
// the ini is not exactly the one MaxPack writes for them (a game's own, written by hand), which add will
// not write again.
static int carried_dlc(mp_p5z *p, mp_dlc_list *have, const char **code, mp_err *e) {
    memset(have, 0, sizeof *have);
    mp_source *s = NULL;
    if (mp_p5z_source(p, is_ini, NULL, &s, e) != 0) return -1;
    int rc = 0, ours = 0;
    if (mp_source_count(s) == 1) {
        uint64_t size = mp_source_entry(s, 0)->size;
        char *b = NULL, *again = NULL;
        size_t len = 0;
        rc = -1;
        if (size <= MP_DLC_INI_MAX) {
            if (!(b = malloc((size_t)size + 1))) {
                mp_err_set(e, "out of memory");
            } else if (mp_source_read(s, 0, 0, b, (size_t)size, e) == 0) {
                if (mp_dlc_parse(b, (size_t)size, have) != 0) {
                    mp_err_set(e, "out of memory");
                } else {
                    again = mp_dlc_ini(have->v, have->n, &len);
                    ours = again && len == size && !memcmp(again, b, len);
                    rc = 0;
                }
            }
        } else {
            rc = 0;   // over what dlc_emu reads: not one MaxPack wrote
        }
        if (!rc && !ours) {
            rc = -1;
            *code = "dlc_has_ini";
            mp_err_set(e, "the game has a dlc_emu.ini that MaxPack did not write: MaxPack writes it again for the DLC "
                          "it adds and does not mix two lists; pack the game again with all its DLC (--dlc)");
        }
        free(again);
        free(b);
        if (rc) mp_dlc_list_free(have);
    }
    mp_source_close(s);
    return rc;
}

int mp_grow_add(const char *p5z, const char *const *dlc, size_t n, const mp_grow_opts *o, mp_grow_result *r, mp_err *e) {
    memset(r, 0, sizeof *r);
    if (!n) {
        mp_err_set(e, "no DLC to add");
        return -1;
    }
    mp_p5z *p = NULL;
    mp_source *game = NULL;
    mp_dlc_list have, added;
    memset(&have, 0, sizeof have);
    memset(&added, 0, sizeof added);
    const char *code = NULL;
    int rc = -1;
    if (open_recovered(p5z, &p, r, e) != 0) goto done;
    if (carried_dlc(p, &have, &code, e) != 0) goto done;
    if (mp_p5z_source(p, not_ini, NULL, &game, e) != 0) goto done;
    if (mp_dlc_attach_ex(&game, have.v, have.n, dlc, n, &added, &code, e) != 0) goto done;   // takes game
    mp_grow_opts g = *o;
    g.force = 1;                  // however much the DLC take, the game is still the one it was
    g.leave_out_leftovers = 0;    // what the game left out when packed is not in the .p5z either
    rc = mp_grow_run(p5z, game, known_old, p, &g, r, e);
    code = r->code;
    for (size_t k = 0; k < added.n; k++) {
        if (added.v[k].data) {
            r->dlc_data++;
            r->dlc_bytes += added.v[k].bytes;
        } else {
            r->dlc_license++;
        }
    }
done:
    if (rc != 0 && code) r->code = code;
    mp_source_close(game);
    mp_dlc_list_free(&have);
    mp_dlc_list_free(&added);
    mp_p5z_close(p);
    return rc;
}
