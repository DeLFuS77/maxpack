// SPDX-License-Identifier: GPL-3.0-or-later
// Reparto de ítems entre hilos y progreso en stderr.
#ifndef MP_PARALLEL_H
#define MP_PARALLEL_H

#include <stddef.h>
#include <stdint.h>

#include "mp_platform.h"

typedef struct mp_progress {
    mp_mutex *mu;
    uint64_t done, total;
    const char *phase;
    int print;
    uint64_t last_ns;   // última vez que se imprimió (mp_progress_maybe_print)
} mp_progress;

void mp_progress_init(mp_progress *p, const char *phase, uint64_t total, int print);
void mp_progress_add(mp_progress *p, uint64_t n);   // p puede ser NULL
void mp_progress_free(mp_progress *p);
// Para bucles que no usan mp_parallel_run para todo: imprime como mucho una vez por segundo.
void mp_progress_maybe_print(mp_progress *p);

// Procesa los ítems 0..n-1 con `threads` hilos (tid en [0, threads)). Si un work devuelve != 0,
// se dejan de repartir ítems y se devuelve el primer código. Si prog->print, el hilo principal
// escribe "\r<fase>: NN %" en stderr cada segundo y al acabar.
int mp_parallel_run(unsigned threads, size_t n, int (*work)(void *ctx, size_t item, unsigned tid),
                    void *ctx, mp_progress *prog);

#endif
