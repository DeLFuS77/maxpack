// SPDX-License-Identifier: GPL-3.0-or-later
// The installer's page (web/index.html), embedded at build time (cmake/embed.cmake).
#ifndef MP_PAGE_H
#define MP_PAGE_H

#include <stddef.h>

extern const unsigned char mp_page_html[];   // ends in NUL (not counted in the length)
extern const size_t mp_page_html_len;

#endif
