// SPDX-License-Identifier: GPL-3.0-or-later
// Arnés mínimo de pruebas: cada archivo de prueba es un ejecutable registrado en CTest.
#ifndef MP_TEST_H
#define MP_TEST_H

#include <inttypes.h>
#include <stdint.h>
#include <stdio.h>
#include <string.h>

static int mp_test_failures_ = 0;
static const char *mp_test_current_ = "";

#define MP_FAIL_(...)                                                              \
    do {                                                                           \
        fprintf(stderr, "%s:%d: [%s] ", __FILE__, __LINE__, mp_test_current_);    \
        fprintf(stderr, __VA_ARGS__);                                              \
        fputc('\n', stderr);                                                       \
        mp_test_failures_++;                                                       \
    } while (0)

#define MP_CHECK(c)                                                                \
    do {                                                                           \
        if (!(c)) MP_FAIL_("falla: %s", #c);                                       \
    } while (0)

#define MP_CHECK_EQ_U64(a, b)                                                      \
    do {                                                                           \
        uint64_t a_ = (uint64_t)(a), b_ = (uint64_t)(b);                           \
        if (a_ != b_)                                                              \
            MP_FAIL_("%s == %s: %" PRIu64 " != %" PRIu64, #a, #b, a_, b_);         \
    } while (0)

#define MP_CHECK_STR(a, b)                                                         \
    do {                                                                           \
        const char *a_ = (a), *b_ = (b);                                           \
        if (!a_ || !b_ || strcmp(a_, b_) != 0)                                     \
            MP_FAIL_("%s == %s: \"%s\" != \"%s\"", #a, #b, a_ ? a_ : "(null)",     \
                     b_ ? b_ : "(null)");                                          \
    } while (0)

#define MP_CHECK_CONTAINS(h, n)                                                    \
    do {                                                                           \
        const char *h_ = (h), *n_ = (n);                                           \
        if (!h_ || !strstr(h_, n_))                                                \
            MP_FAIL_("\"%s\" no contiene \"%s\"", h_ ? h_ : "(null)", n_);         \
    } while (0)

#define MP_TEST(name) static void name(void)

#define MP_RUN(name)                                                               \
    do {                                                                           \
        int before_ = mp_test_failures_;                                           \
        mp_test_current_ = #name;                                                  \
        name();                                                                    \
        fprintf(stderr, "%s %s\n", mp_test_failures_ == before_ ? "ok   " : "FALLO", \
                #name);                                                            \
    } while (0)

#define MP_TEST_MAIN_END() return mp_test_failures_ ? 1 : 0

#endif
