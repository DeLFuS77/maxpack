// SPDX-License-Identifier: GPL-3.0-or-later
// Sending a .p5z to MaxPack for the console (see mp_send.h). One request at a time, each on a connection of
// its own; a connection that breaks is tried again from what the console says it has.
#include "mp_send.h"

#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "mp_http.h"
#include "mp_json.h"
#include "mp_platform.h"

#define MAX_WAIT_MS 30000u
#define POLL_MS 500u

void mp_send_opts_default(mp_send_opts *o) {
    memset(o, 0, sizeof *o);
    o->port = MP_SEND_PORT;
    o->chunk = MP_SEND_CHUNK;
    o->retries = 10;
    o->retry_ms = 1000;
    o->timeout_ms = 60000;
}

int mp_send_parse_address(const char *s, mp_send_opts *o) {
    const char *colon = strchr(s, ':');
    size_t n = colon ? (size_t)(colon - s) : strlen(s);
    if (n == 0 || n >= sizeof o->host) return -1;
    // four numbers of 0 to 255, separated by dots
    unsigned parts = 0, v = 0, digits = 0;
    for (size_t i = 0; i <= n; i++) {
        char c = i < n ? s[i] : '.';
        if (c >= '0' && c <= '9') {
            v = v * 10 + (unsigned)(c - '0');
            if (++digits > 3 || v > 255) return -1;
        } else if (c == '.' && digits) {
            parts++;
            v = digits = 0;
        } else {
            return -1;
        }
    }
    if (parts != 4) return -1;
    uint16_t port = MP_SEND_PORT;
    if (colon) {
        char *end = NULL;
        unsigned long p = strtoul(colon + 1, &end, 10);
        if (end == colon + 1 || *end || p == 0 || p > 65535) return -1;
        port = (uint16_t)p;
    }
    memcpy(o->host, s, n);
    o->host[n] = 0;
    o->port = port;
    return 0;
}

int mp_send_set_code(mp_send_opts *o, const char *code) {
    if (!code || strlen(code) != 6) return -1;
    for (int i = 0; i < 6; i++)
        if (code[i] < '0' || code[i] > '9') return -1;
    memcpy(o->code, code, 7);
    return 0;
}

int mp_send_name_ok(const char *name) {
    size_t n = strlen(name);
    if (n < 5 || n > 200 || strstr(name, "..")) return 0;
    for (size_t i = 0; i < n; i++) {
        unsigned char c = (unsigned char)name[i];
        if (c == '/' || c == '\\' || c < 0x20 || c == 0x7f) return 0;
    }
    const char *x = name + n - 4;
    return x[0] == '.' && (x[1] | 32) == 'p' && x[2] == '5' && (x[3] | 32) == 'z';
}

void mp_send_result_free(mp_send_result *r) {
    if (!r) return;
    free(r->bad);
    r->bad = NULL;
    r->nbad = 0;
}

// ---- one request ---------------------------------------------------------------------------------

typedef struct {
    const mp_send_opts *o;
    const mp_send_hooks *h;
    mp_send_result *r;
    mp_err *e;
    unsigned attempt;   // broken connections in a row
} sender;

static int cancelled(const sender *s) { return s->h && s->h->cancelled && s->h->cancelled(s->h->ctx); }

// 0 with the status and the body parsed (*j NULL when it is not JSON), -1 when there was no answer.
static int call(sender *s, const char *method, const char *path, const char *ctype, const void *body, size_t n,
                int *status, mp_json **j) {
    char headers[64];
    snprintf(headers, sizeof headers, "X-MaxPack-Code: %s\r\n", s->o->code);
    mp_http_opts ho = {ctype, headers, s->o->timeout_ms};
    char *resp = NULL;
    size_t rl = 0;
    *j = NULL;
    if (mp_http_request_ex(s->o->host, s->o->port, method, path, body, n, &ho, status, &resp, &rl, NULL) != 0) return -1;
    if (rl) *j = mp_json_parse(resp, rl, NULL);
    free(resp);
    return 0;
}

static const char *str_of(const mp_json *j, const char *key) {
    const char *v = mp_json_str(mp_json_get(j, key));
    return v ? v : "";
}

static int u64_of(const mp_json *j, const char *key, uint64_t *v) { return mp_json_u64(mp_json_get(j, key), v); }

static int fail(sender *s, const char *code, const char *fmt, const char *arg) {
    s->r->code = code;
    mp_err_set(s->e, fmt, arg ? arg : "");
    return -1;
}

// An answer that is not what was asked for: the console's refusal, as a code and a message.
static int refused(sender *s, int status, const mp_json *j) {
    static const char *const known[] = {"receive_off", "bad_code", "locked", "exists", "busy",
                                        "no_space", "fat32", "bad_name", "no_drive"};
    const char *code = str_of(j, "error"), *detail = str_of(j, "detail");
    snprintf(s->r->detail, sizeof s->r->detail, "%s", detail);
    if (!*code) code = status == 401 ? "bad_code" : status == 403 ? "receive_off" : status == 429 ? "locked"
                     : status == 507 ? "no_space" : "";
    const char *c = NULL;
    for (size_t i = 0; i < sizeof known / sizeof *known; i++)
        if (!strcmp(code, known[i])) c = known[i];
    if (!c) {
        char what[600];
        snprintf(what, sizeof what, "%d %s%s%s", status, code, *detail ? ": " : "", detail);
        return fail(s, "console_error", "the console gave an answer MaxPack does not know: %s", what);
    }
    if (!strcmp(c, "receive_off"))
        return fail(s, c, "the console does not take games from the PC: turn on Settings > \"Receive games from the "
                          "PC\" in MaxPack for the console%s", NULL);
    if (!strcmp(c, "bad_code"))
        return fail(s, c, "the console refused the pairing code: give the 6 digits its page shows in Settings > "
                          "\"Receive games from the PC\"%s", NULL);
    if (!strcmp(c, "locked"))
        return fail(s, c, "after 5 wrong codes in a row the console takes none from this PC for a minute (too many "
                          "wrong codes): wait, and give the right one%s", NULL);
    if (!strcmp(c, "exists"))
        return fail(s, c, "%s is already on that drive of the console", s->r->name);
    if (!strcmp(c, "busy"))
        return fail(s, c, "the console is busy with another receive, an install, a verify or a test compression: "
                          "send it when that ends%s", NULL);
    if (!strcmp(c, "no_space"))
        return fail(s, c, "that drive of the console has not enough free space (the .p5z needs its size and 256 MiB "
                          "more)%s", NULL);
    if (!strcmp(c, "fat32"))
        return fail(s, c, "that drive of the console is FAT32, which holds no file of 4 GiB or more: send it to an "
                          "exFAT drive%s", NULL);
    if (!strcmp(c, "bad_name"))
        return fail(s, c, "the console does not take this file name (a plain .p5z name of at most 200 bytes): %s",
                    s->r->name);
    return fail(s, c, "the console has no drive %s", s->r->drive);
}

// After a broken connection: waits (retry_ms, doubling, up to 30 s) and says so. 0 to try again; -1 when
// there are no tries left or it was cancelled meanwhile.
static int wait_retry(sender *s, uint64_t have) {
    if (s->attempt >= s->o->retries) {
        char where[96];
        snprintf(where, sizeof where, "%s:%u", s->o->host, (unsigned)s->o->port);
        return fail(s, "unreachable", "no answer from the console at %s (is MaxPack for the console open there, on "
                                      "the same network?)", where);
    }
    s->attempt++;
    s->r->retries++;
    unsigned wait = s->o->retry_ms;
    for (unsigned i = 1; i < s->attempt && wait < MAX_WAIT_MS; i++) wait *= 2;
    if (wait > MAX_WAIT_MS) wait = MAX_WAIT_MS;
    if (s->h && s->h->retry) s->h->retry(s->h->ctx, s->attempt, wait, have);
    for (unsigned waited = 0; waited < wait; waited += 50) {
        if (cancelled(s)) return fail(s, "cancelled", "cancelled%s", NULL);
        mp_sleep_ms(wait - waited < 50 ? wait - waited : 50);
    }
    return 0;
}

// The request again and again while the connection breaks, as long as there are tries left.
static int call_retry(sender *s, const char *method, const char *path, const char *ctype, const void *body, size_t n,
                      uint64_t have, int *status, mp_json **j) {
    while (call(s, method, path, ctype, body, n, status, j) != 0)
        if (wait_retry(s, have) != 0) return -1;
    return 0;
}

static void query_id(char *out, size_t cap, const char *route, const char *id) {
    size_t k = (size_t)snprintf(out, cap, "%s?id=", route);
    for (const unsigned char *c = (const unsigned char *)id; *c && k + 4 < cap; c++) {
        if ((*c >= 'a' && *c <= 'z') || (*c >= 'A' && *c <= 'Z') || (*c >= '0' && *c <= '9') || *c == '-' ||
            *c == '_' || *c == '.' || *c == '~')
            out[k++] = (char)*c;
        else
            k += (size_t)snprintf(out + k, cap - k, "%%%02X", *c);
    }
    out[k] = 0;
}

// ---- the routes ----------------------------------------------------------------------------------

static int get_drives(sender *s, mp_console_drive **v, size_t *n) {
    *v = NULL;
    *n = 0;
    int st = 0;
    mp_json *j = NULL;
    if (call_retry(s, "GET", "/api/receive/drives", NULL, NULL, 0, 0, &st, &j) != 0) return -1;
    const mp_json *list = mp_json_get(j, "drives");
    int rc = 0;
    if (st != 200) {
        rc = refused(s, st, j);
    } else if (!list || !(*v = calloc(mp_json_len(list) + 1, sizeof **v))) {
        rc = fail(s, "console_error", "the console's list of drives cannot be read%s", NULL);
    } else {
        for (size_t i = 0; i < mp_json_len(list); i++) {
            const mp_json *d = mp_json_at(list, i);
            mp_console_drive *x = &(*v)[*n];
            if (!*str_of(d, "id")) continue;
            snprintf(x->id, sizeof x->id, "%s", str_of(d, "id"));
            snprintf(x->fs, sizeof x->fs, "%s", str_of(d, "fs"));
            u64_of(d, "free", &x->free);
            u64_of(d, "total", &x->total);
            (*n)++;
        }
    }
    mp_json_free(j);
    if (rc) {
        free(*v);
        *v = NULL;
        *n = 0;
    }
    return rc;
}

int mp_send_drives(const mp_send_opts *o, mp_console_drive **v, size_t *n, mp_send_result *r, mp_err *e) {
    memset(r, 0, sizeof *r);
    sender s = {o, NULL, r, e, 0};
    return get_drives(&s, v, n);
}

// POST start: the token and what the console already has of it.
static int start(sender *s, char *id, size_t cap, uint64_t *have) {
    mp_jbuf b = {0};
    mp_jbuf_raw(&b, "{\"name\":");
    mp_jbuf_str(&b, s->r->name);
    mp_jbuf_raw(&b, ",\"size\":");
    mp_jbuf_u64(&b, s->r->size);
    mp_jbuf_raw(&b, ",\"drive\":");
    mp_jbuf_str(&b, s->r->drive);
    mp_jbuf_raw(&b, "}");
    if (b.oom) {
        mp_jbuf_free(&b);
        return fail(s, "io", "out of memory%s", NULL);
    }
    int st = 0;
    mp_json *j = NULL;
    int rc = call_retry(s, "POST", "/api/receive/start", NULL, b.p, b.n, 0, &st, &j);
    mp_jbuf_free(&b);
    if (rc) return -1;
    if (st != 200) {
        rc = refused(s, st, j);
    } else if (!*str_of(j, "id") || u64_of(j, "have", have) != 0 || *have > s->r->size) {
        rc = fail(s, "console_error", "the console's answer to a new receive cannot be read%s", NULL);
    } else {
        snprintf(id, cap, "%s", str_of(j, "id"));
    }
    mp_json_free(j);
    return rc;
}

enum { AT_RECEIVING, AT_VERIFYING, AT_DONE, AT_FAILED, AT_CANCELLED, AT_GONE };

// GET status: the state and what the console has; AT_GONE when it does not know the token any more (its
// payload started again). -1 on a refusal or with no tries left.
static int status(sender *s, const char *id, uint64_t have_here, uint64_t *have, uint64_t *vdone, uint64_t *vtotal,
                  mp_json **keep) {
    char path[256];
    query_id(path, sizeof path, "/api/receive/status", id);
    int st = 0;
    mp_json *j = NULL;
    if (call_retry(s, "GET", path, NULL, NULL, 0, have_here, &st, &j) != 0) return -1;
    int at = -1;
    if (st == 404) {
        at = AT_GONE;
    } else if (st != 200) {
        refused(s, st, j);
    } else {
        const char *state = str_of(j, "state");
        const mp_json *v = mp_json_get(j, "verify");
        *vdone = *vtotal = 0;
        u64_of(v, "bytes", vdone);
        u64_of(v, "total", vtotal);
        at = !strcmp(state, "receiving") ? AT_RECEIVING : !strcmp(state, "verifying") ? AT_VERIFYING
           : !strcmp(state, "done") ? AT_DONE : !strcmp(state, "failed") ? AT_FAILED
           : !strcmp(state, "cancelled") ? AT_CANCELLED : -1;
        if (u64_of(j, "have", have) != 0 || *have > s->r->size) at = -1;
        if (at < 0) fail(s, "console_error", "the console's status of the receive cannot be read%s", NULL);
    }
    if (keep && at >= 0) *keep = j;
    else mp_json_free(j);
    return at;
}

static void copy_hex(char out[65], const mp_json *j) {
    const char *v = mp_json_str(j);
    snprintf(out, 65, "%s", v ? v : "");
}

// What the console's verify found wrong, from its status.
static void take_bad(sender *s, const mp_json *j) {
    const mp_json *list = mp_json_get(mp_json_get(j, "verify"), "bad");
    size_t n = mp_json_len(list);
    if (!n || !(s->r->bad = calloc(n, sizeof *s->r->bad))) return;
    for (size_t i = 0; i < n; i++) {
        const mp_json *x = mp_json_at(list, i);
        mp_send_bad *b = &s->r->bad[s->r->nbad++];
        uint64_t vol = 0;
        b->volume = mp_json_u64(mp_json_get(x, "volume"), &vol) == 0 ? (int64_t)vol : -1;
        snprintf(b->path, sizeof b->path, "%s", mp_json_str(x) ? mp_json_str(x) : str_of(x, "path"));
        snprintf(b->why, sizeof b->why, "%s", str_of(x, "why"));
        copy_hex(b->expected, mp_json_get(x, "expected"));
        copy_hex(b->found, mp_json_get(x, "found"));
    }
}

static void post_cancel(sender *s, const char *id) {
    char path[256];
    query_id(path, sizeof path, "/api/receive/cancel", id);
    int st = 0;
    mp_json *j = NULL;
    if (call(s, "POST", path, NULL, NULL, 0, &st, &j) == 0) mp_json_free(j);   // one try: it is going away
}

int mp_send(const char *path, const mp_send_opts *o, const mp_send_hooks *h, mp_send_result *r, mp_err *e) {
    memset(r, 0, sizeof *r);
    sender s = {o, h, r, e, 0};
    const char *base = path + strlen(path);
    while (base > path && base[-1] != '/' && base[-1] != '\\') base--;
    snprintf(r->name, sizeof r->name, "%s", base);
    if (strlen(base) >= sizeof r->name || !mp_send_name_ok(r->name))
        return fail(&s, "bad_name", "the console takes only a plain .p5z name of at most 200 bytes: %s", base);
    mp_file *f = NULL;
    uint8_t *buf = NULL;
    mp_console_drive *dv = NULL;
    size_t nd = 0;
    int rc = -1;
    if (mp_file_open_read(path, &f, e) != 0 || mp_file_size(f, &r->size, e) != 0) {
        r->code = "io";
        goto done;
    }
    // the drive: the one asked for, or internal, or the first the console lists
    if (get_drives(&s, &dv, &nd) != 0) goto done;
    const char *want = o->drive ? o->drive : "internal";
    for (size_t i = 0; i < nd && !r->drive[0]; i++)
        if (!strcmp(dv[i].id, want)) snprintf(r->drive, sizeof r->drive, "%s", dv[i].id);
    if (!r->drive[0] && !o->drive && nd) snprintf(r->drive, sizeof r->drive, "%s", dv[0].id);
    if (!r->drive[0]) {
        snprintf(r->drive, sizeof r->drive, "%s", want);
        char list[512] = "";
        for (size_t i = 0; i < nd; i++)
            snprintf(list + strlen(list), sizeof list - strlen(list), "%s%s", i ? ", " : "", dv[i].id);
        char what[600];
        snprintf(what, sizeof what, "%s (it has %s)", want, nd ? list : "none");
        fail(&s, "no_drive", "the console has no drive %s", what);
        goto done;
    }
    char id[128];
    uint64_t have = 0, vdone = 0, vtotal = 0;
    if (start(&s, id, sizeof id, &have) != 0) goto done;
    r->resumed_from = have;
    if (h && h->started) h->started(h->ctx, r->name, r->drive, have, r->size);
    size_t chunk = o->chunk < MP_SEND_MIN_CHUNK ? MP_SEND_MIN_CHUNK : o->chunk > MP_SEND_MAX_CHUNK ? MP_SEND_MAX_CHUNK
                                                                                                    : o->chunk;
    if (!(buf = malloc(chunk))) {
        fail(&s, "io", "out of memory%s", NULL);
        goto done;
    }
    for (unsigned finishes = 0;; finishes++) {
        if (finishes > 5) {   // it keeps saying it has not all of it, or starting again
            fail(&s, "console_error", "the console does not finish the receive%s", NULL);
            goto done;
        }
        // the bytes, from what the console has
        while (have < r->size) {
            if (cancelled(&s)) {
                post_cancel(&s, id);
                fail(&s, "cancelled", "cancelled%s", NULL);
                goto done;
            }
            size_t n = r->size - have < chunk ? (size_t)(r->size - have) : chunk, got = 0;
            if (mp_file_pread(f, buf, n, have, &got, e) != 0 || got != n) {
                if (got != n) mp_err_set(e, "%s changed while it was being sent", path);
                r->code = "io";
                goto done;
            }
            char put[256];
            query_id(put, sizeof put, "/api/receive/data", id);
            snprintf(put + strlen(put), sizeof put - strlen(put), "&offset=%llu", (unsigned long long)have);
            int st = 0;
            mp_json *j = NULL;
            if (call(&s, "PUT", put, "application/octet-stream", buf, n, &st, &j) != 0) {
                // the connection broke: where the console is now (it may have the bytes, or start again)
                if (wait_retry(&s, have) != 0) goto done;
                int at = status(&s, id, have, &have, &vdone, &vtotal, NULL);
                if (at == AT_GONE) {
                    if (start(&s, id, sizeof id, &have) != 0) goto done;
                } else if (at != AT_RECEIVING) {
                    if (at >= 0) fail(&s, "console_error", "the console stopped receiving it%s", NULL);
                    goto done;
                }
                continue;
            }
            uint64_t now = 0;
            int ok = st == 200 && u64_of(j, "have", &now) == 0 && now >= have && now <= r->size;
            int offset = st == 409 && !strcmp(str_of(j, "error"), "offset") && u64_of(j, "have", &now) == 0 &&
                         now <= r->size;
            if (!ok && !offset) {
                if (st == 200 || st == 409) fail(&s, "console_error", "the console's answer to the bytes cannot be read%s", NULL);
                else refused(&s, st, j);
                mp_json_free(j);
                goto done;
            }
            mp_json_free(j);
            if (ok) {
                s.attempt = 0;
                r->sent += now - have;
            }
            have = now;   // 409 offset: from what it has
            if (ok && h && h->sending) h->sending(h->ctx, have, r->size, r->sent);
        }
        // all of it there: the console's verify
        char fin[256];
        query_id(fin, sizeof fin, "/api/receive/finish", id);
        int st = 0, at = -1;
        mp_json *j = NULL;
        if (call(&s, "POST", fin, NULL, NULL, 0, &st, &j) != 0) {
            if (wait_retry(&s, have) != 0) goto done;
            at = status(&s, id, have, &have, &vdone, &vtotal, NULL);   // did it begin?
        } else if (st == 200 || st == 202) {
            at = AT_VERIFYING;
        } else if (st == 409) {
            at = status(&s, id, have, &have, &vdone, &vtotal, NULL);   // it has less than it said
        } else {
            refused(&s, st, j);
        }
        mp_json_free(j);
        if (at == AT_RECEIVING) continue;
        // and its answer
        while (at == AT_VERIFYING) {
            if (cancelled(&s)) {
                post_cancel(&s, id);
                fail(&s, "cancelled", "cancelled%s", NULL);
                goto done;
            }
            mp_sleep_ms(POLL_MS);
            mp_json *keep = NULL;
            at = status(&s, id, have, &have, &vdone, &vtotal, &keep);
            if (at == AT_VERIFYING && h && h->verifying) h->verifying(h->ctx, vdone, vtotal);
            if (at == AT_FAILED) take_bad(&s, keep);
            mp_json_free(keep);
        }
        if (at == AT_GONE) {
            // its payload started again: the file is still there whole, or already verified and named .p5z
            if (start(&s, id, sizeof id, &have) != 0) {
                if (r->code && !strcmp(r->code, "exists")) at = AT_DONE;
                else goto done;
            } else {
                continue;
            }
        }
        if (at == AT_DONE) {
            if (h && h->verifying) h->verifying(h->ctx, r->size, r->size);
            r->code = NULL;
            rc = 0;
        } else if (at == AT_FAILED) {
            char what[400];
            snprintf(what, sizeof what, "%zu of its volumes and files are not what was sent; the console keeps it as "
                     "%.200s.maxpack-recv", r->nbad, r->name);
            fail(&s, "verify_failed", "it arrived damaged: %s", what);
        } else if (at == AT_CANCELLED) {
            fail(&s, "console_cancelled", "the receive was cancelled on the console%s", NULL);
        } else if (at == AT_RECEIVING || at == AT_GONE) {
            continue;
        }
        break;
    }
done:
    free(buf);
    free(dv);
    mp_file_close(f);
    return rc;
}
