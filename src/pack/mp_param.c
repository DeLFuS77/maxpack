// SPDX-License-Identifier: GPL-3.0-or-later
#include <string.h>

#include "mp_param.h"

void mp_param_str(const char *j, size_t n, const char *key, char *out, size_t cap) {
    out[0] = 0;
    char pat[64];
    size_t kl = strlen(key);
    if (kl + 3 > sizeof pat) return;
    pat[0] = '"';
    memcpy(pat + 1, key, kl);
    pat[kl + 1] = '"';
    pat[kl + 2] = 0;
    for (size_t i = 0; i + kl + 2 <= n; i++) {
        if (memcmp(j + i, pat, kl + 2) != 0) continue;
        size_t p = i + kl + 2;
        while (p < n && (j[p] == ' ' || j[p] == '\t' || j[p] == '\r' || j[p] == '\n')) p++;
        if (p >= n || j[p] != ':') continue;
        p++;
        while (p < n && (j[p] == ' ' || j[p] == '\t' || j[p] == '\r' || j[p] == '\n')) p++;
        if (p >= n || j[p] != '"') continue;
        p++;
        size_t k = 0;
        while (p < n && j[p] != '"' && k + 1 < cap) {
            if (j[p] == '\\' && p + 1 < n) p++;
            out[k++] = j[p++];
        }
        out[k] = 0;
        return;
    }
}
