// SPDX-License-Identifier: GPL-3.0-or-later
// Reading sce_sys/param.json, shared by pack, describe and the installer.
#ifndef MP_PARAM_H
#define MP_PARAM_H

#include <stddef.h>

// Reads titleId, contentVersion and the name: the titleName of localizedParameters[defaultLanguage] and,
// if it is missing, the top-level one. The texts are cut at a UTF-8 character boundary. A UTF-8 BOM
// at the start and the trailing spaces and NULs do not count. Returns 0, or -1 if it is not valid
// JSON (with all three empty).
int mp_param_read(const char *json, size_t n, char id[16], char ver[16], char name[128]);

#endif
