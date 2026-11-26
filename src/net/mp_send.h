// SPDX-License-Identifier: GPL-3.0-or-later
// Sending a .p5z to MaxPack for the console over the network, verified when it arrives. The console's side
// (its page's server, port 8095) is documented in docs/guide.md: every request carries the pairing code
// the console shows (X-MaxPack-Code), the bytes go in PUTs from what the console already has, and finishing
// makes the console run the quick verify (mp_verify_quick.h) before it gives the file its .p5z name. A receive
// the console pauses (idle, a write error, or stopped on its page) keeps what arrived.
#ifndef MP_SEND_H
#define MP_SEND_H

#include <stddef.h>
#include <stdint.h>

#include "mp_err.h"

#define MP_SEND_PORT 8095
#define MP_SEND_CHUNK (32u << 20)       // bytes per PUT
#define MP_SEND_MAX_CHUNK (64u << 20)   // what the console takes in one
#define MP_SEND_MIN_CHUNK (64u << 10)
#define MP_SEND_MAX_WAIT_MS 30000u      // the longest wait between two tries
#define MP_SEND_MAX_BAD 64u             // what the console found wrong: at most this many are kept
#define MP_SEND_MAX_TEXT 1024u          // and at most this many bytes of any string it sends
#define MP_SEND_DISCARD_WAIT_S 75u      // a discard waits this long at most for the console to let go of it

typedef struct {
    char host[64];          // a numeric IPv4 address
    uint16_t port;          // MP_SEND_PORT
    char code[8];           // the 6 digits of the console's pairing code
    const char *drive;      // its id ("internal", "usb0"...); NULL: internal when the console has it, else its first
    size_t chunk;           // MP_SEND_CHUNK
    // tries after a broken connection: first_retries while the console has never answered this run (1: "no
    // answer" in about 11 s), then retries (10); any answer begins the count again
    unsigned first_retries, retries;
    unsigned retry_ms;      // the first wait after one (1000); it doubles each time, up to 30 s
    unsigned connect_ms;    // to connect (5000)
    unsigned timeout_ms;    // for each read and send (60000)
    unsigned stall_ms;      // what the console has (and its verify) not moving for this long: stopped, kept (300000)
    int delete_on_cancel;   // a cancel (hooks.cancelled() == 2) makes the console delete what it received
} mp_send_opts;

void mp_send_opts_default(mp_send_opts *o);
// "a.b.c.d" or "a.b.c.d:port" into o->host and o->port, each number without a leading zero. -1 if it is not one.
int mp_send_parse_address(const char *s, mp_send_opts *o);
// 0 if code is 6 digits (and goes into o->code).
int mp_send_set_code(mp_send_opts *o, const char *code);
// 0 if name is what the console takes as a file name: a plain *.p5z name (no '/', '\', "..", control
// characters), at most 200 bytes.
int mp_send_name_ok(const char *name);

// What happens while it sends; any of them may be NULL.
typedef struct {
    void (*started)(void *ctx, const char *name, const char *drive, uint64_t have, uint64_t size);
    // after each PUT the console took: what it has, of size; sent, what went this time
    void (*sending)(void *ctx, uint64_t have, uint64_t size, uint64_t sent);
    void (*verifying)(void *ctx, uint64_t done, uint64_t total);      // the console's quick verify
    void (*retry)(void *ctx, unsigned attempt, unsigned wait_ms, uint64_t have);   // a broken connection
    // a receive the console paused (idle or a write error) that is started again from what it has
    void (*paused)(void *ctx, const char *reason, uint64_t have);
    // 0 go on; 1 stop and keep what the console has (the next send goes on from there); 2 stop, and the console
    // deletes what it received (only with opts.delete_on_cancel; else as 1)
    int (*cancelled)(void *ctx);
    void *ctx;
    // mp_send_discard: the console is letting go of the receive, and asks to wait that long before it is asked again
    void (*waiting)(void *ctx, unsigned seconds);
} mp_send_hooks;

// One item of what the console's verify found wrong, as its status gives it (control characters replaced).
typedef struct {
    char path[MP_SEND_MAX_TEXT];
    int64_t volume;         // -1 when it is not a volume
    char why[32], expected[65], found[65];
} mp_send_bad;

typedef struct {
    // NULL when it arrived and the console verified it; else why, as a code: the console's refusals
    // ("receive_off", "bad_code", "locked", "exists", "busy", "no_space", "fat32", "bad_name", "no_drive"),
    // "verify_failed" (it arrived damaged: the console keeps it with its .maxpack-recv name), "console_cancelled"
    // (cancelled on the console), "console_stopped" (stopped on the console's page: it keeps what arrived),
    // "write_error" (the console could not write it, again and again), "stalled" (nothing moved for
    // opts.stall_ms, or this PC slept that long: kept), "unreachable" (no answer after the tries), "cancelled"
    // (hooks.cancelled: kept says whether the console keeps what arrived), "delete_failed" (send_discard: the
    // console could not delete it; detail says why), "discard_unsupported" (send_discard: a console that has no
    // such request), "console_error" (an answer this client does not know, or one that does not move forward),
    // "io" (reading the .p5z here)
    const char *code;
    char detail[512];       // what the console said with it, if anything (control characters replaced)
    char name[256], drive[64];
    uint64_t size, resumed_from, sent;   // the .p5z's size, what the console had already, what went this time
    uint64_t have;          // what the console has of it at the end
    // "cancelled", "console_stopped", "stalled", "write_error", "unreachable", and "no_space" when part of it (or of
    // an earlier copy) is there already, taking room: 1 when the console keeps what arrived (something did)
    int kept;
    int slept;              // "stalled": this PC slept (its clock jumped past opts.stall_ms between two looks)
    uint64_t need;          // "no_space": how many more bytes the drive would need, when the console says it
    int deleted;            // mp_send_discard: 1 when there was something to delete (have says how much)
    unsigned retries;       // broken connections it went past
    size_t nbad;            // verify_failed: how many the console listed (at most MP_SEND_MAX_BAD)
    mp_send_bad *bad;       // malloc'd (mp_send_result_free)
} mp_send_result;

void mp_send_result_free(mp_send_result *r);

// Sends the file at path (its name as it is on the console) and waits for the console's verify. 0 only when
// the console says it is done; -1 with r->code (and e) otherwise.
int mp_send(const char *path, const mp_send_opts *o, const mp_send_hooks *h, mp_send_result *r, mp_err *e);

// Deletes what the console has received of the .p5z at path on that drive (its .maxpack-recv), with the console's
// own request for it: never a start, so a drive that is full does not refuse it. Only the name of path is used.
// While the console lets go of a receive of it at work (409 busy with retry_after) it waits and asks again, up to
// MP_SEND_DISCARD_WAIT_S (h->waiting is told; h may be NULL). 0 when nothing of it is there any more: r->deleted
// says whether there was something (r->have, its bytes); the .p5z whole on the console is never touched. -1 with
// r->code ("busy": a receive of it is at work; "discard_unsupported").
int mp_send_discard(const char *path, const mp_send_opts *o, const mp_send_hooks *h, mp_send_result *r, mp_err *e);

typedef struct {
    char id[64], fs[32];
    uint64_t free, total;
} mp_console_drive;

// The drives the console scans, with their free space. 0 with *v (malloc'd) and *n; -1 with r->code.
int mp_send_drives(const mp_send_opts *o, mp_console_drive **v, size_t *n, mp_send_result *r, mp_err *e);

#endif
