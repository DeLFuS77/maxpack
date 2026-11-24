// SPDX-License-Identifier: GPL-3.0-or-later
// Distribution of items among threads and progress on stderr.
#ifndef MP_PARALLEL_H
#define MP_PARALLEL_H

#include <stddef.h>
#include <stdint.h>

#include "mp_err.h"
#include "mp_platform.h"

// Progress hook: mp_progress_add calls it at most every 100 ms and always when the
// phase ends (done == total). It is called with the phase's lock held, so no two calls happen at once.
typedef struct {
    void (*cb)(void *ctx, const char *phase, uint64_t done, uint64_t total);
    void *ctx;
} mp_progress_hook;

// One hook per process (NULL removes it). mp_progress_init copies it: set it before starting.
void mp_progress_set_hook(const mp_progress_hook *h);

// Cancellation: atomic flag, can be requested from any thread. mp_parallel_run stops handing out
// and pack, unpack and verify exit with -1 and the message "cancelled".
void mp_cancel_request(void);
int mp_cancelled(void);
void mp_cancel_reset(void);
// If a cancellation is pending, puts "cancelled" in e and returns -1; otherwise 0.
int mp_cancel_check(mp_err *e);

typedef struct mp_progress {
    mp_mutex *mu;
    uint64_t done, total;
    const char *phase;
    int print;
    uint64_t last_ns;   // last time it was printed (mp_progress_maybe_print)
    mp_progress_hook hook;
    uint64_t hook_ns;   // last call to the hook
    int hook_final;     // done == total has already been reported
} mp_progress;

void mp_progress_init(mp_progress *p, const char *phase, uint64_t total, int print);
void mp_progress_add(mp_progress *p, uint64_t n);   // p may be NULL
void mp_progress_free(mp_progress *p);
// For loops that do not use mp_parallel_run for everything: prints at most once per second.
void mp_progress_maybe_print(mp_progress *p);

// Processes items 0..n-1 with `threads` threads (tid in [0, threads)). If a work returns != 0,
// items stop being handed out and the first code is returned. With mp_cancelled() it also stops
// handing out and returns -1 if items were left undistributed. If prog->print, the main thread
// writes "\r<phase>: NN %" on stderr every second and at the end.
int mp_parallel_run(unsigned threads, size_t n, int (*work)(void *ctx, size_t item, unsigned tid),
                    void *ctx, mp_progress *prog);

#endif
