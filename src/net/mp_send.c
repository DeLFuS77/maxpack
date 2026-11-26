// SPDX-License-Identifier: GPL-3.0-or-later
// Sending a .p5z to MaxPack for the console (see mp_send.h). One request at a time, each on a connection of
// its own; a connection that breaks is tried again from what the console says it has, and a receive the
// console paused is started again (but one stopped on the console is not). Nothing the console says is
// trusted further than it has to be: every string it sends loses its control characters and is cut at
// MP_SEND_MAX_TEXT, and its list of what failed is cut at MP_SEND_MAX_BAD.
#include "mp_send.h"

#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "mp_http.h"
#include "mp_json.h"
#include "mp_platform.h"

#define POLL_MS 500u
#define MAX_SAME 20u     // answers in a row that take nothing before the console counts as broken
#define MAX_ROUNDS 50u   // starts again (a paused or restarted receive) before it gives up

void mp_send_opts_default(mp_send_opts *o) {
    memset(o, 0, sizeof *o);
    o->port = MP_SEND_PORT;
    o->chunk = MP_SEND_CHUNK;
    o->first_retries = 1;
    o->retries = 10;
    o->retry_ms = 1000;
    o->connect_ms = 5000;
    o->timeout_ms = 60000;
    o->stall_ms = 300000;
}

int mp_send_parse_address(const char *s, mp_send_opts *o) {
    const char *colon = strchr(s, ':');
    size_t n = colon ? (size_t)(colon - s) : strlen(s);
    if (n == 0 || n >= sizeof o->host) return -1;
    // four numbers of 0 to 255, separated by dots, none with a leading zero (inet_pton refuses them on POSIX)
    unsigned parts = 0, v = 0, digits = 0;
    char first = 0;
    for (size_t i = 0; i <= n; i++) {
        char c = i < n ? s[i] : '.';
        if (c >= '0' && c <= '9') {
            if (!digits) first = c;
            v = v * 10 + (unsigned)(c - '0');
            if (++digits > 3 || v > 255 || (digits > 1 && first == '0')) return -1;
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
        if (end == colon + 1 || *end || p == 0 || p > 65535 || colon[1] == '0') return -1;
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

// A string of the console, for printing or showing: control characters become '?', and it is cut at
// MP_SEND_MAX_TEXT bytes (and at cap).
static void clean(char *dst, size_t cap, const char *src) {
    size_t n = 0, most = cap < MP_SEND_MAX_TEXT ? cap : MP_SEND_MAX_TEXT;
    for (const unsigned char *c = (const unsigned char *)(src ? src : ""); *c && n + 1 < most; c++)
        dst[n++] = (char)(*c < 0x20 || *c == 0x7f ? '?' : *c);
    dst[n] = 0;
}

// ---- one request ---------------------------------------------------------------------------------

typedef struct {
    const mp_send_opts *o;
    const mp_send_hooks *h;
    mp_send_result *r;
    mp_err *e;
    unsigned attempt;       // broken connections in a row
    int answered;           // the console answered something this run
    unsigned write_errors, pauses;
    int watching;           // the receive began: stalls count
    uint64_t moved_ns, best_have, best_verify;
    uint64_t look_ns;       // the last look for a stall
    const char *token;      // the receive's token, once it began (to say the PC left)
    uint64_t drive_total;   // the size of the drive it goes to, as the console lists it (0: not known)
} sender;

static int fail(sender *s, const char *code, const char *fmt, const char *arg) {
    s->r->code = code;
    mp_err_set(s->e, fmt, arg ? arg : "");
    return -1;
}

// 0 with the status and the body parsed (*j NULL when it is not JSON), -1 when there was no answer. Any answer
// begins the count of tries again.
static int call(sender *s, const char *method, const char *path, const char *ctype, const void *body, size_t n,
                int *status, mp_json **j) {
    char headers[64];
    snprintf(headers, sizeof headers, "X-MaxPack-Code: %s\r\n", s->o->code);
    mp_http_opts ho = {ctype, headers, s->o->timeout_ms, s->o->connect_ms};
    char *resp = NULL;
    size_t rl = 0;
    *j = NULL;
    if (mp_http_request_ex(s->o->host, s->o->port, method, path, body, n, &ho, status, &resp, &rl, NULL) != 0) return -1;
    s->attempt = 0;
    s->answered = 1;
    if (rl) *j = mp_json_parse(resp, rl, NULL);
    free(resp);
    return 0;
}

static const char *str_of(const mp_json *j, const char *key) {
    const char *v = mp_json_str(mp_json_get(j, key));
    return v ? v : "";
}

static int u64_of(const mp_json *j, const char *key, uint64_t *v) { return mp_json_u64(mp_json_get(j, key), v); }

// Something moved (what the console has, or its verify): the stall begins again.
static void moved(sender *s, uint64_t have, uint64_t verify) {
    if (have > s->best_have || verify > s->best_verify || !s->moved_ns) s->moved_ns = mp_now_with_sleep_ns();
    if (have > s->best_have) s->best_have = have;
    if (verify > s->best_verify) s->best_verify = verify;
}

static void have_text(sender *s, char *out, size_t cap) {
    snprintf(out, cap, "%llu of %llu bytes", (unsigned long long)s->best_have, (unsigned long long)s->r->size);
}

// -1 (and "stalled") once nothing moved for stall_ms since the receive began. When the clock jumped past
// stall_ms since the last look (one before each request, and in each short wait), it is this PC that slept (or
// this program was stopped), not the console that did nothing.
static void post_cancel(sender *s, const char *id);
static void query_id(char *out, size_t cap, const char *route, const char *id);

// The PC stops and keeps what arrived (contract v3.1): one try to tell the console, which pauses the receive
// ("left") so that it can be resumed or deleted at once. 2 s at most; whatever it answers, or not, is not read.
static void post_leave(sender *s) {
    if (!s->token || !*s->token) return;
    char path[256], headers[64];
    query_id(path, sizeof path, "/api/receive/leave", s->token);
    snprintf(headers, sizeof headers, "X-MaxPack-Code: %s\r\n", s->o->code);
    mp_http_opts ho = {NULL, headers, 2000, 2000};
    char *resp = NULL;
    size_t rl = 0;
    int st = 0;
    mp_http_request_ex(s->o->host, s->o->port, "POST", path, NULL, 0, &ho, &st, &resp, &rl, NULL);
    free(resp);
}

// How a stop keeps what arrived: kept only when something did; the console is told the PC left.
static void keep_on_stop(sender *s, char *had, size_t cap) {
    have_text(s, had, cap);
    s->r->kept = s->best_have > 0;
    post_leave(s);
}

static int stalled(sender *s) {
    uint64_t now = mp_now_with_sleep_ns(), stall = (uint64_t)s->o->stall_ms * 1000000ull;
    uint64_t gap = s->look_ns && now > s->look_ns ? now - s->look_ns : 0;
    s->look_ns = now;
    if (!s->watching || !s->o->stall_ms || now - s->moved_ns < stall) return 0;
    char what[600], had[96];
    keep_on_stop(s, had, sizeof had);
    char keeps[200];
    if (s->r->kept) snprintf(keeps, sizeof keeps, "the console keeps the %s that arrived; send it again to go on from there", had);
    else snprintf(keeps, sizeof keeps, "nothing of it had arrived; send it again");
    if (gap >= stall) {
        s->r->slept = 1;
        snprintf(what, sizeof what, "%llu s (%s)", (unsigned long long)(gap / 1000000000ull), keeps);
        return fail(s, "stalled", "this PC slept while it was sending, for %s", what);
    }
    snprintf(what, sizeof what, "%u s (%s)", s->o->stall_ms / 1000, keeps);
    return fail(s, "stalled", "nothing moved on the console for %s", what);
}

// -1 once hooks.cancelled asks to stop: what arrived stays on the console (which is told the PC left), or (2,
// with delete_on_cancel and a token) the console deletes it.
static int stopped_here(sender *s, const char *id) {
    int how = s->h && s->h->cancelled ? s->h->cancelled(s->h->ctx) : 0;
    if (!how) return 0;
    if (how == 2 && s->o->delete_on_cancel && id && *id) {
        post_cancel(s, id);
        s->r->kept = 0;
        return fail(s, "cancelled", "cancelled: the console deletes what it received%s", NULL);
    }
    if (!s->watching) {   // no receive began: nothing arrived to keep
        s->r->kept = 0;
        return fail(s, "cancelled", "stopped before anything was sent%s", NULL);
    }
    char had[96];
    keep_on_stop(s, had, sizeof had);
    if (!s->r->kept) return fail(s, "cancelled", "stopped before any of it arrived on the console%s", NULL);
    return fail(s, "cancelled", "stopped: the console keeps the %s that arrived; send it again to go on from there", had);
}

// An answer that is not what was asked for: the console's refusal, as a code and a message.
static int refused(sender *s, int status, const mp_json *j) {
    static const char *const known[] = {"receive_off", "bad_code", "locked", "exists", "busy",
                                        "no_space", "fat32", "bad_name", "no_drive", "delete_failed"};
    char code[64];
    clean(code, sizeof code, str_of(j, "error"));
    clean(s->r->detail, sizeof s->r->detail, str_of(j, "detail"));
    if (!*code)
        snprintf(code, sizeof code, "%s", status == 401 ? "bad_code" : status == 403 ? "receive_off" : status == 429 ? "locked"
                                          : status == 507 ? "no_space" : "");
    const char *c = NULL;
    for (size_t i = 0; i < sizeof known / sizeof *known; i++)
        if (!strcmp(code, known[i])) c = known[i];
    if (!c) {
        char what[700];
        snprintf(what, sizeof what, "%d %s%s%s", status, code, *s->r->detail ? ": " : "", s->r->detail);
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
    if (!strcmp(c, "no_space")) {
        // what of it is on that drive already (contract v3: "have"; a console that does not say it has what arrived
        // this run) takes room there: it is kept, and said, to be deleted
        uint64_t had = 0, need = 0;
        if (u64_of(j, "have", &had) != 0) had = s->watching ? s->best_have : 0;
        if (had > (s->drive_total ? s->drive_total : UINT64_MAX / 2)) had = 0;   // not a file that drive can hold
        if (u64_of(j, "need", &need) != 0) need = 0;
        s->r->need = need;
        char what[400] = "";
        if (need) snprintf(what, sizeof what, "; the drive lacks %llu bytes", (unsigned long long)need);
        if (had > s->r->size) {
            // an earlier copy of it, bigger than this one (the game was made smaller since): it takes the room
            if (had > s->best_have) s->best_have = had;
            s->r->kept = 1;
            snprintf(what + strlen(what), sizeof what - strlen(what), "): part of an earlier copy of it, %llu bytes, "
                     "is there, taking room; delete it (send --discard) to free it", (unsigned long long)had);
        } else if (had) {
            if (had > s->best_have) s->best_have = had;
            s->r->kept = 1;
            snprintf(what + strlen(what), sizeof what - strlen(what), "): the %llu of %llu bytes of it that arrived "
                     "before are there, taking room; delete them (send --discard) to free it",
                     (unsigned long long)had, (unsigned long long)s->r->size);
        } else {
            snprintf(what + strlen(what), sizeof what - strlen(what), ")");
        }
        return fail(s, c, "that drive of the console has not enough free space (the .p5z needs its size and 256 MiB "
                          "more%s", what);
    }
    if (!strcmp(c, "delete_failed"))
        return fail(s, c, "the console could not delete it: %s", *s->r->detail ? s->r->detail : "it gave no reason");
    if (!strcmp(c, "fat32"))
        return fail(s, c, "that drive of the console is FAT32, which holds no file of 4 GiB or more: send it to an "
                          "exFAT drive%s", NULL);
    if (!strcmp(c, "bad_name"))
        return fail(s, c, "the console does not take this file name (a plain .p5z name of at most 200 bytes): %s",
                    s->r->name);
    return fail(s, c, "the console has no drive %s", s->r->drive);
}

// After a broken connection: waits (retry_ms, doubling, up to 30 s) and says so. 0 to try again; -1 when there
// are no tries left (first_retries while the console never answered, else retries), or it was stopped meanwhile.
static int wait_retry(sender *s, const char *id, uint64_t have) {
    unsigned limit = s->answered ? s->o->retries : (s->o->first_retries < s->o->retries ? s->o->first_retries : s->o->retries);
    if (s->attempt >= limit) {
        char where[96];
        snprintf(where, sizeof where, "%s:%u", s->o->host, (unsigned)s->o->port);
        s->r->kept = s->watching && s->best_have > 0;
        return fail(s, "unreachable", "no answer from the console at %s (is MaxPack for the console open there, on "
                                      "the same network?)", where);
    }
    s->attempt++;
    s->r->retries++;
    unsigned wait = s->o->retry_ms;
    for (unsigned i = 1; i < s->attempt && wait < MP_SEND_MAX_WAIT_MS; i++) wait *= 2;
    if (wait > MP_SEND_MAX_WAIT_MS) wait = MP_SEND_MAX_WAIT_MS;
    if (s->h && s->h->retry) s->h->retry(s->h->ctx, s->attempt, wait, have);
    for (unsigned waited = 0; waited < wait; waited += 50) {
        if (stopped_here(s, id) || stalled(s)) return -1;
        mp_sleep_ms(wait - waited < 50 ? wait - waited : 50);
    }
    return 0;
}

// The request again and again while the connection breaks, as long as there are tries left.
static int call_retry(sender *s, const char *id, const char *method, const char *path, const char *ctype, const void *body,
                      size_t n, uint64_t have, int *status, mp_json **j) {
    while (call(s, method, path, ctype, body, n, status, j) != 0)
        if (wait_retry(s, id, have) != 0) return -1;
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
    if (call_retry(s, NULL, "GET", "/api/receive/drives", NULL, NULL, 0, 0, &st, &j) != 0) return -1;
    const mp_json *list = mp_json_get(j, "drives");
    size_t len = mp_json_len(list) < 256 ? mp_json_len(list) : 256;
    int rc = 0;
    if (st != 200) {
        rc = refused(s, st, j);
    } else if (!list || !(*v = calloc(len + 1, sizeof **v))) {
        rc = fail(s, "console_error", "the console's list of drives cannot be read%s", NULL);
    } else {
        for (size_t i = 0; i < len; i++) {
            const mp_json *d = mp_json_at(list, i);
            mp_console_drive *x = &(*v)[*n];
            if (!*str_of(d, "id")) continue;
            clean(x->id, sizeof x->id, str_of(d, "id"));
            clean(x->fs, sizeof x->fs, str_of(d, "fs"));
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
    sender s;
    memset(&s, 0, sizeof s);
    s.o = o;
    s.r = r;
    s.e = e;
    return get_drives(&s, v, n);
}

// POST start: the token and what the console already has of it. A new one also after a pause or a restart.
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
    int rc = call_retry(s, id, "POST", "/api/receive/start", NULL, b.p, b.n, *have, &st, &j);
    mp_jbuf_free(&b);
    if (rc) return -1;
    if (st != 200) {
        rc = refused(s, st, j);
    } else if (!*str_of(j, "id") || strlen(str_of(j, "id")) >= cap || u64_of(j, "have", have) != 0 || *have > s->r->size) {
        rc = fail(s, "console_error", "the console's answer to a new receive cannot be read%s", NULL);
    } else {
        snprintf(id, cap, "%s", str_of(j, "id"));
        moved(s, *have, 0);
    }
    mp_json_free(j);
    return rc;
}

enum { AT_RECEIVING, AT_VERIFYING, AT_DONE, AT_FAILED, AT_CANCELLED, AT_GONE, AT_PAUSED };

static void copy_hex(char out[65], const mp_json *j) {
    const char *v = mp_json_str(j);
    clean(out, 65, v ? v : "");
}

// What the console's verify found wrong, from its status: at most MP_SEND_MAX_BAD of them.
static void take_bad(sender *s, const mp_json *j) {
    const mp_json *list = mp_json_get(mp_json_get(j, "verify"), "bad");
    size_t n = mp_json_len(list);
    if (n > MP_SEND_MAX_BAD) n = MP_SEND_MAX_BAD;
    free(s->r->bad);
    s->r->bad = NULL;
    s->r->nbad = 0;
    if (!n || !(s->r->bad = calloc(n, sizeof *s->r->bad))) return;
    for (size_t i = 0; i < n; i++) {
        const mp_json *x = mp_json_at(list, i);
        mp_send_bad *b = &s->r->bad[s->r->nbad++];
        uint64_t vol = 0;
        b->volume = mp_json_u64(mp_json_get(x, "volume"), &vol) == 0 && vol < (1ull << 32) ? (int64_t)vol : -1;
        clean(b->path, sizeof b->path, mp_json_str(x) ? mp_json_str(x) : str_of(x, "path"));
        clean(b->why, sizeof b->why, str_of(x, "why"));
        copy_hex(b->expected, mp_json_get(x, "expected"));
        copy_hex(b->found, mp_json_get(x, "found"));
    }
}

// GET status: the state and what the console has (its verify too); AT_GONE when it does not know the token
// (its payload started again, or the receive ended), AT_PAUSED with why in reason. -1 on a refusal or with no
// tries left. keep, when given, gets the answer of a state it gives.
static int status(sender *s, const char *id, uint64_t have_here, uint64_t *have, uint64_t *vdone, uint64_t *vtotal,
                  char reason[32], mp_json **keep) {
    char path[256];
    query_id(path, sizeof path, "/api/receive/status", id);
    int st = 0;
    mp_json *j = NULL;
    if (call_retry(s, id, "GET", path, NULL, NULL, 0, have_here, &st, &j) != 0) return -1;
    int at = -1;
    reason[0] = 0;
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
        clean(reason, 32, str_of(j, "reason"));
        at = !strcmp(state, "receiving") ? AT_RECEIVING : !strcmp(state, "verifying") ? AT_VERIFYING
           : !strcmp(state, "done") ? AT_DONE : !strcmp(state, "failed") ? AT_FAILED
           : !strcmp(state, "cancelled") ? AT_CANCELLED : !strcmp(state, "paused") ? AT_PAUSED : -1;
        if (u64_of(j, "have", have) != 0 || *have > s->r->size) at = -1;
        if (at < 0) fail(s, "console_error", "the console's status of the receive cannot be read%s", NULL);
        else moved(s, *have, at == AT_VERIFYING ? *vdone : 0);
    }
    if (keep && at >= 0) *keep = j;
    else mp_json_free(j);
    return at;
}

static void post_cancel(sender *s, const char *id) {
    char path[256];
    query_id(path, sizeof path, "/api/receive/cancel", id);
    int st = 0;
    mp_json *j = NULL;
    if (call(s, "POST", path, NULL, NULL, 0, &st, &j) == 0) mp_json_free(j);   // one try: it is going away
}

// A receive the console paused. Stopped on the console: it stays as it is (-1, "console_stopped"). Idle or a
// write error (which counts against the tries): after a wait, it is started again from what the console has (0).
static int on_pause(sender *s, const char *reason, char *id, size_t cap, uint64_t *have) {
    char had[96];
    have_text(s, had, sizeof had);
    if (!strcmp(reason, "stopped")) {
        s->r->kept = s->best_have > 0;
        return fail(s, "console_stopped", "it was stopped on the console, which keeps the %s that arrived; send it "
                                          "again to go on from there", had);
    }
    if (!strcmp(reason, "write_error") && ++s->write_errors > s->o->retries) {
        s->r->kept = s->best_have > 0;
        return fail(s, "write_error", "the console could not write it to that drive, again and again (it keeps the %s "
                                      "that arrived): is the drive full, read-only or failing?", had);
    }
    if (s->h && s->h->paused) s->h->paused(s->h->ctx, !strcmp(reason, "write_error") ? "write_error" : "idle", *have);
    unsigned wait = s->o->retry_ms;
    for (unsigned i = 0; i < s->pauses && wait < MP_SEND_MAX_WAIT_MS; i++) wait *= 2;
    if (wait > MP_SEND_MAX_WAIT_MS) wait = MP_SEND_MAX_WAIT_MS;
    s->pauses++;
    for (unsigned waited = 0; waited < wait; waited += 50) {
        if (stopped_here(s, id) || stalled(s)) return -1;
        mp_sleep_ms(wait - waited < 50 ? wait - waited : 50);
    }
    return start(s, id, cap, have);
}

// After a broken connection while sending: where the console is now. 0 to go on from *have, -1 to stop.
static int resync(sender *s, char *id, size_t cap, uint64_t *have) {
    uint64_t vd = 0, vt = 0;
    char reason[32];
    int at = status(s, id, *have, have, &vd, &vt, reason, NULL);
    if (at == AT_GONE) return start(s, id, cap, have);
    if (at == AT_PAUSED) return on_pause(s, reason, id, cap, have);
    if (at == AT_RECEIVING) return 0;
    if (at >= 0) fail(s, "console_error", "the console stopped receiving it%s", NULL);
    return -1;
}

// The bytes, from what the console has to the end. 0 once it has them all, -1 to stop.
static int send_bytes(sender *s, mp_file *f, uint8_t *buf, size_t chunk, char *id, size_t cap, uint64_t *have) {
    unsigned same = 0;
    while (*have < s->r->size) {
        if (stopped_here(s, id) || stalled(s)) return -1;
        if (same > MAX_SAME)
            return fail(s, "console_error", "the console answers without taking the bytes%s", NULL);
        size_t n = s->r->size - *have < chunk ? (size_t)(s->r->size - *have) : chunk, got = 0;
        if (mp_file_pread(f, buf, n, *have, &got, s->e) != 0 || got != n) {
            if (got != n) mp_err_set(s->e, "the .p5z changed while it was being sent");
            s->r->code = "io";
            return -1;
        }
        char put[256];
        query_id(put, sizeof put, "/api/receive/data", id);
        snprintf(put + strlen(put), sizeof put - strlen(put), "&offset=%llu", (unsigned long long)*have);
        int st = 0;
        mp_json *j = NULL;
        if (call(s, "PUT", put, "application/octet-stream", buf, n, &st, &j) != 0) {
            // the connection broke: where the console is now (it may have the bytes, have paused, or started again)
            if (wait_retry(s, id, *have) != 0 || resync(s, id, cap, have) != 0) return -1;
            continue;
        }
        char err[32], reason[32];
        clean(err, sizeof err, str_of(j, "error"));
        clean(reason, sizeof reason, str_of(j, "reason"));
        uint64_t now = 0;
        int rc = 0;
        if (st == 200 && u64_of(j, "have", &now) == 0 && now >= *have && now <= s->r->size) {
            int took = now > *have;
            if (took) {
                s->r->sent += now - *have;
                same = 0;
            } else {
                same++;
            }
            *have = now;
            moved(s, now, 0);
            if (took && s->h && s->h->sending) s->h->sending(s->h->ctx, *have, s->r->size, s->r->sent);
        } else if (st == 409 && !strcmp(err, "offset") && u64_of(j, "have", &now) == 0 && now <= s->r->size) {
            same += now == *have;   // from what it has (it may have less than it said)
            *have = now;
        } else if (st == 409 && !strcmp(err, "paused")) {
            rc = on_pause(s, reason, id, cap, have);
        } else if (st == 404) {
            rc = start(s, id, cap, have);   // it does not know the token: it started again
        } else if (st == 200 || st == 409) {
            rc = fail(s, "console_error", "the console's answer to the bytes cannot be read%s", NULL);
        } else {
            rc = refused(s, st, j);
        }
        mp_json_free(j);
        if (rc) return -1;
    }
    return 0;
}

// All of it is there: the console's verify, and its answer. AT_DONE, AT_FAILED, AT_CANCELLED; AT_RECEIVING when
// the bytes have to go again from *have (a pause, a restart, less than it said); -1 to stop.
static int finish_and_wait(sender *s, char *id, size_t cap, uint64_t *have) {
    char fin[256], reason[32];
    uint64_t vd = 0, vt = 0;
    query_id(fin, sizeof fin, "/api/receive/finish", id);
    int st = 0, at = -1;
    mp_json *j = NULL;
    if (call(s, "POST", fin, NULL, NULL, 0, &st, &j) != 0) {
        if (wait_retry(s, id, *have) != 0) return -1;
        at = status(s, id, *have, have, &vd, &vt, reason, NULL);   // did it begin?
    } else if (st == 200 || st == 202) {
        at = AT_VERIFYING;
    } else if (st == 409 && !strcmp(str_of(j, "error"), "paused")) {
        clean(reason, sizeof reason, str_of(j, "reason"));
        at = AT_PAUSED;
    } else if (st == 409) {
        at = status(s, id, *have, have, &vd, &vt, reason, NULL);   // it has less than it said
    } else if (st == 404) {
        at = AT_GONE;
    } else {
        refused(s, st, j);
    }
    mp_json_free(j);
    while (at == AT_VERIFYING) {
        if (stopped_here(s, id) || stalled(s)) return -1;
        mp_sleep_ms(POLL_MS);
        mp_json *keep = NULL;
        at = status(s, id, *have, have, &vd, &vt, reason, &keep);
        if (at == AT_VERIFYING && s->h && s->h->verifying) s->h->verifying(s->h->ctx, vd, vt);
        if (at == AT_FAILED) take_bad(s, keep);
        mp_json_free(keep);
    }
    if (at == AT_PAUSED) return on_pause(s, reason, id, cap, have) == 0 ? AT_RECEIVING : -1;
    if (at == AT_GONE) {
        // its payload started again: the file is still there whole, or verified and named .p5z already
        if (start(s, id, cap, have) == 0) return AT_RECEIVING;
        if (s->r->code && !strcmp(s->r->code, "exists")) return AT_DONE;
        return -1;
    }
    return at;
}

// The name, the file (f NULL: only its name, for a discard) and the drive of a send. 0, or -1 with the code.
static int prepare(sender *s, const char *path, mp_file **f) {
    mp_send_result *r = s->r;
    const char *base = path + strlen(path);
    while (base > path && base[-1] != '/' && base[-1] != '\\') base--;
    snprintf(r->name, sizeof r->name, "%s", base);
    if (strlen(base) >= sizeof r->name || !mp_send_name_ok(r->name))
        return fail(s, "bad_name", "the console takes only a plain .p5z name of at most 200 bytes: %s", base);
    if (f && (mp_file_open_read(path, f, s->e) != 0 || mp_file_size(*f, &r->size, s->e) != 0)) {
        r->code = "io";
        return -1;
    }
    // the drive: the one asked for, or internal, or the first the console lists
    mp_console_drive *dv = NULL;
    size_t nd = 0;
    if (get_drives(s, &dv, &nd) != 0) return -1;
    const char *want = s->o->drive ? s->o->drive : "internal";
    for (size_t i = 0; i < nd && !r->drive[0]; i++)
        if (!strcmp(dv[i].id, want)) {
            snprintf(r->drive, sizeof r->drive, "%s", dv[i].id);
            s->drive_total = dv[i].total;
        }
    if (!r->drive[0] && !s->o->drive && nd) {
        snprintf(r->drive, sizeof r->drive, "%s", dv[0].id);
        s->drive_total = dv[0].total;
    }
    int rc = 0;
    if (!r->drive[0]) {
        snprintf(r->drive, sizeof r->drive, "%s", want);
        char list[512] = "";
        for (size_t i = 0; i < nd; i++)
            snprintf(list + strlen(list), sizeof list - strlen(list), "%s%s", i ? ", " : "", dv[i].id);
        char what[600];
        snprintf(what, sizeof what, "%s (it has %s)", want, nd ? list : "none");
        rc = fail(s, "no_drive", "the console has no drive %s", what);
    }
    free(dv);
    return rc;
}

int mp_send(const char *path, const mp_send_opts *o, const mp_send_hooks *h, mp_send_result *r, mp_err *e) {
    memset(r, 0, sizeof *r);
    sender s;
    memset(&s, 0, sizeof s);
    s.o = o;
    s.h = h;
    s.r = r;
    s.e = e;
    mp_file *f = NULL;
    uint8_t *buf = NULL;
    int rc = -1;
    char id[128] = "";
    uint64_t have = 0;
    if (prepare(&s, path, &f) != 0 || start(&s, id, sizeof id, &have) != 0) goto done;
    s.token = id;
    r->resumed_from = have;
    s.watching = 1;
    s.moved_ns = mp_now_with_sleep_ns();
    if (h && h->started) h->started(h->ctx, r->name, r->drive, have, r->size);
    size_t chunk = o->chunk < MP_SEND_MIN_CHUNK ? MP_SEND_MIN_CHUNK : o->chunk > MP_SEND_MAX_CHUNK ? MP_SEND_MAX_CHUNK
                                                                                                    : o->chunk;
    if (!(buf = malloc(chunk))) {
        fail(&s, "io", "out of memory%s", NULL);
        goto done;
    }
    for (unsigned rounds = 0;; rounds++) {
        if (rounds > MAX_ROUNDS) {   // it keeps pausing, starting again or saying it has not all of it
            fail(&s, "console_error", "the console does not finish the receive%s", NULL);
            break;
        }
        if (send_bytes(&s, f, buf, chunk, id, sizeof id, &have) != 0) break;
        int at = finish_and_wait(&s, id, sizeof id, &have);
        if (at == AT_RECEIVING) continue;
        if (at == AT_DONE) {
            if (h && h->verifying) h->verifying(h->ctx, r->size, r->size);
            r->code = NULL;
            have = r->size;
            rc = 0;
        } else if (at == AT_FAILED) {
            char what[400];
            snprintf(what, sizeof what, "%zu of its volumes and files are not what was sent; the console keeps it as "
                     "%.200s.maxpack-recv", r->nbad, r->name);
            fail(&s, "verify_failed", "it arrived damaged: %s", what);
        } else if (at == AT_CANCELLED) {
            fail(&s, "console_cancelled", "the receive was cancelled on the console%s", NULL);
        } else if (at >= 0) {
            fail(&s, "console_error", "the console stopped receiving it%s", NULL);
        }
        break;
    }
done:
    r->have = have > s.best_have ? have : s.best_have;
    free(buf);
    mp_file_close(f);
    return rc;
}

int mp_send_discard(const char *path, const mp_send_opts *o, const mp_send_hooks *h, mp_send_result *r, mp_err *e) {
    memset(r, 0, sizeof *r);
    sender s;
    memset(&s, 0, sizeof s);
    s.o = o;
    s.h = h;
    s.r = r;
    s.e = e;
    if (prepare(&s, path, NULL) != 0) return -1;
    mp_jbuf b = {0};
    mp_jbuf_raw(&b, "{\"name\":");
    mp_jbuf_str(&b, r->name);
    mp_jbuf_raw(&b, ",\"drive\":");
    mp_jbuf_str(&b, r->drive);
    mp_jbuf_raw(&b, "}");
    if (b.oom) {
        mp_jbuf_free(&b);
        return fail(&s, "io", "out of memory%s", NULL);
    }
    int st = 0, rc = -1, deleted = 0;
    mp_json *j = NULL;
    char err[32];
    // a receive of it that took a request a moment ago (a PC that could not say it left): the console says when it
    // lets go of it (retry_after), and it is asked again, for MP_SEND_DISCARD_WAIT_S in all
    for (unsigned waited = 0;;) {
        int sent = call_retry(&s, NULL, "POST", "/api/receive/discard", NULL, b.p, b.n, 0, &st, &j);
        if (sent != 0) {
            mp_jbuf_free(&b);
            return -1;
        }
        clean(err, sizeof err, str_of(j, "error"));
        uint64_t after = 0;
        if (st != 409 || strcmp(err, "busy") != 0 || u64_of(j, "retry_after", &after) != 0 || waited >= MP_SEND_DISCARD_WAIT_S)
            break;
        unsigned wait = after < 1 ? 1 : after > MP_SEND_DISCARD_WAIT_S ? MP_SEND_DISCARD_WAIT_S : (unsigned)after;
        if (wait > MP_SEND_DISCARD_WAIT_S - waited) wait = MP_SEND_DISCARD_WAIT_S - waited;
        if (h && h->waiting) h->waiting(h->ctx, wait);
        mp_json_free(j);
        j = NULL;
        mp_sleep_ms(wait * 1000u);
        waited += wait;
    }
    mp_jbuf_free(&b);
    if (st == 200 && mp_json_bool(mp_json_get(j, "deleted"), &deleted) == 0) {
        r->deleted = deleted != 0;
        if (!r->deleted || u64_of(j, "bytes", &r->have) != 0) r->have = 0;
        rc = 0;
    } else if (st == 200) {
        fail(&s, "console_error", "the console's answer to deleting it cannot be read%s", NULL);
    } else if (st == 409 && !strcmp(err, "busy")) {
        clean(r->detail, sizeof r->detail, str_of(j, "detail"));
        fail(&s, "busy", "the console is still receiving or verifying %s (it lets go of a receive a minute after its "
                         "last data): delete it once that ends", r->name);
    } else if (st == 404 && strcmp(err, "no_drive") != 0) {
        fail(&s, "discard_unsupported", "this MaxPack for the console cannot delete what a send left there: update it "
                                        "(or delete <name>.maxpack-recv in its homebrew folder by FTP)%s", NULL);
    } else {
        refused(&s, st, j);
    }
    mp_json_free(j);
    return rc;
}
