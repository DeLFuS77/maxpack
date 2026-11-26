// SPDX-License-Identifier: GPL-3.0-or-later
// Sending a .p5z to MaxPack for the console over the network, verified when it arrives. The console's side
// (its page's server, port 8095) is documented in docs/guide.md: every request carries the pairing code
// the console shows (X-MaxPack-Code), the bytes go in PUTs from what the console already has, and finishing
// makes the console run the quick verify (mp_verify_quick.h) before it gives the file its .p5z name.
#ifndef MP_SEND_H
#define MP_SEND_H

#include <stddef.h>
#include <stdint.h>

#include "mp_err.h"

#define MP_SEND_PORT 8095
#define MP_SEND_CHUNK (32u << 20)       // bytes per PUT
#define MP_SEND_MAX_CHUNK (64u << 20)   // what the console takes in one
#define MP_SEND_MIN_CHUNK (64u << 10)

typedef struct {
    char host[64];          // a numeric IPv4 address
    uint16_t port;          // MP_SEND_PORT
    char code[8];           // the 6 digits of the console's pairing code
    const char *drive;      // its id ("internal", "usb0"...); NULL: internal when the console has it, else its first
    size_t chunk;           // MP_SEND_CHUNK
    unsigned retries;       // tries in a row after a broken connection before giving up (10)
    unsigned retry_ms;      // the first wait after one (1000); it doubles each time, up to 30 s
    unsigned timeout_ms;    // to connect and for each read and send (60000)
} mp_send_opts;

void mp_send_opts_default(mp_send_opts *o);
// "a.b.c.d" or "a.b.c.d:port" into o->host and o->port. -1 if it is not one.
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
    int (*cancelled)(void *ctx);   // non-zero: stop, and the console deletes what it received
    void *ctx;
} mp_send_hooks;

// One item of what the console's verify found wrong, as its status gives it.
typedef struct {
    char path[1024];
    int64_t volume;         // -1 when it is not a volume
    char why[32], expected[65], found[65];
} mp_send_bad;

typedef struct {
    // NULL when it arrived and the console verified it; else why, as a code: the console's refusals
    // ("receive_off", "bad_code", "locked", "exists", "busy", "no_space", "fat32", "bad_name", "no_drive"),
    // "verify_failed" (it arrived damaged: the console keeps it with its .maxpack-recv name), "console_cancelled"
    // (cancelled on the console), "unreachable" (no answer after the retries), "cancelled" (hooks.cancelled),
    // "console_error" (an answer this client does not know), "io" (reading the .p5z here)
    const char *code;
    char detail[512];       // what the console said with it, if anything
    char name[256], drive[64];
    uint64_t size, resumed_from, sent;   // the .p5z's size, what the console had already, what went this time
    unsigned retries;       // broken connections it went past
    size_t nbad;            // verify_failed: how many the console listed
    mp_send_bad *bad;       // malloc'd (mp_send_result_free)
} mp_send_result;

void mp_send_result_free(mp_send_result *r);

// Sends the file at path (its name as it is on the console) and waits for the console's verify. 0 only when
// the console says it is done; -1 with r->code (and e) otherwise.
int mp_send(const char *path, const mp_send_opts *o, const mp_send_hooks *h, mp_send_result *r, mp_err *e);

typedef struct {
    char id[64], fs[32];
    uint64_t free, total;
} mp_console_drive;

// The drives the console scans, with their free space. 0 with *v (malloc'd) and *n; -1 with r->code.
int mp_send_drives(const mp_send_opts *o, mp_console_drive **v, size_t *n, mp_send_result *r, mp_err *e);

#endif
