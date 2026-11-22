// SPDX-License-Identifier: GPL-3.0-or-later
// Provisional: el lector exFAT llega en la tarea 5.
#include "mp_source_impl.h"

int mp_exfat_open(const char *path, mp_source **out, mp_err *e) {
    *out = NULL;
    mp_err_set(e, "%s: el lector exFAT todavía no está implementado", path);
    return -1;
}
