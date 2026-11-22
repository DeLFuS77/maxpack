// SPDX-License-Identifier: GPL-3.0-or-later
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <zlib.h>
#include <zstd.h>
#ifdef _WIN32
#define WIN32_LEAN_AND_MEAN
#include <windows.h>
#endif

#include "mp_analyze.h"
#include "mp_platform.h"
#include "mp_report_out.h"
#include "mp_rules.h"
#include "mp_version.h"

static void usage(FILE *out) {
    fprintf(out,
            "Uso:\n"
            "  maxpack analyze <carpeta|imagen.exfat> [opciones]\n"
            "  maxpack --version\n"
            "\n"
            "Opciones de analyze:\n"
            "  --sample N%%        porcentaje de la muestra (por defecto 2%%, mínimo 1 GiB)\n"
            "  --full             medir todo el juego, sin muestra\n"
            "  --json ARCHIVO     guardar el informe en JSON\n"
            "  --threads N        hilos (por defecto, todos los de la CPU)\n"
            "  --keep-loose GLOB  dejar suelto también lo que case (se puede repetir)\n"
            "  --no-dedup         no hacer el pase de deduplicación (lee todo el juego)\n"
            "  --list             solo listar los archivos y su clase, sin medir\n");
}

static int bad_usage(const char *msg, const char *arg) {
    fprintf(stderr, "error: %s%s%s\n\n", msg, arg ? ": " : "", arg ? arg : "");
    usage(stderr);
    return 2;
}

static int is_sep(char c) { return c == '/' || c == '\\'; }

// Deja p en su carpeta padre (C:\a\b -> C:\a, C:\a -> C:\, /a -> /).
// Devuelve 0 si p ya es una raíz.
static int to_parent(char *p) {
    size_t n = strlen(p);
    while (n > 0 && is_sep(p[n - 1])) n--;
    size_t end = n;
    while (n > 0 && !is_sep(p[n - 1])) n--;
    if (n == 0 || n == end) return 0;
    size_t sep = n - 1;
    if (sep == 0 || (sep == 2 && p[1] == ':'))
        p[n] = 0;   // raíz: se queda con su separador
    else
        p[sep] = 0;
    return 1;
}

// Comprueba --json antes de analizar: su carpeta existe y no pisa el juego ni cae dentro de él.
static int check_json_target(const char *src, const char *json) {
    char *dir = mp_full_path(json);
    if (!dir) return bad_usage("ruta de --json no válida", json);
    int rc = 0;
    if (mp_path_kind(json) == 2) {
        rc = bad_usage("--json apunta a una carpeta", json);
    } else if (mp_same_file(json, src)) {
        rc = bad_usage("--json no puede sobrescribir el juego que se analiza", json);
    } else if (!to_parent(dir) || mp_path_kind(dir) != 2) {
        rc = bad_usage("no existe la carpeta donde guardar --json", json);
    } else if (mp_path_kind(src) == 2) {
        do {
            if (mp_same_file(dir, src)) {
                rc = bad_usage("--json no puede escribir dentro de la carpeta que se analiza", json);
                break;
            }
        } while (to_parent(dir));
    }
    free(dir);
    return rc;
}

static int list_files(const char *src_path, const char *const *keep, size_t nkeep) {
    mp_err e = {{0}};
    mp_source *s = NULL;
    if (mp_source_open(src_path, &s, &e) != 0) {
        fprintf(stderr, "error: %s\n", e.msg);
        return 1;
    }
    mp_rules *r = mp_rules_new();
    for (size_t i = 0; i < nkeep; i++)
        if (mp_rules_add_keep_loose(r, keep[i], &e) != 0) {
            fprintf(stderr, "error: %s\n", e.msg);
            mp_rules_free(r);
            mp_source_close(s);
            return 2;
        }
    for (size_t i = 0; i < mp_source_count(s); i++) {
        const mp_entry *en = mp_source_entry(s, i);
        printf("%llu\t%s\t%s\n", (unsigned long long)en->size,
               mp_rules_classify(r, en->path, en->size) == MP_LOOSE ? "suelto" : "empaquetable",
               en->path);
    }
    mp_rules_free(r);
    mp_source_close(s);
    return 0;
}

static int cmd_analyze(int argc, char **argv) {
    mp_analyze_opts o;
    mp_analyze_opts_default(&o);
    const char *src = NULL, *json = NULL;
    const char **keep = calloc((size_t)argc, sizeof *keep);
    int list = 0;
    if (!keep) return 1;
    for (int i = 0; i < argc; i++) {
        const char *a = argv[i];
        int has_value = i + 1 < argc;
        if (!strcmp(a, "--full")) {
            o.full = 1;
        } else if (!strcmp(a, "--no-dedup")) {
            o.dedup = 0;
        } else if (!strcmp(a, "--list")) {
            list = 1;
        } else if (!strcmp(a, "--sample") || !strcmp(a, "--json") || !strcmp(a, "--threads") ||
                   !strcmp(a, "--keep-loose")) {
            if (!has_value) return bad_usage("falta el valor de", a);
            const char *v = argv[++i];
            if (!strcmp(a, "--json")) {
                json = v;
            } else if (!strcmp(a, "--keep-loose")) {
                keep[o.nkeep++] = v;
            } else if (!strcmp(a, "--sample")) {
                char *end = NULL;
                double p = strtod(v, &end);
                if (end && *end == '%') end++;
                if (end == v || *end || !(p > 0.0 && p <= 100.0))
                    return bad_usage("--sample debe estar entre 0 y 100 %", v);
                o.sample_fraction = p / 100.0;
            } else {
                char *end = NULL;
                long t = strtol(v, &end, 10);
                if (end == v || *end || t < 1 || t > 1024)
                    return bad_usage("--threads debe estar entre 1 y 1024", v);
                o.threads = (unsigned)t;
            }
        } else if (a[0] == '-' && a[1] == '-') {
            return bad_usage("opción desconocida", a);
        } else if (!src) {
            src = a;
        } else {
            return bad_usage("sobra un argumento", a);
        }
    }
    if (!src) return bad_usage("falta la carpeta o la imagen .exfat", NULL);
    o.keep_loose = keep;
    if (list) return list_files(src, keep, o.nkeep);
    if (json) {
        int jrc = check_json_target(src, json);
        if (jrc) return jrc;
    }

    mp_report r;
    mp_err e = {{0}};
    if (mp_analyze(src, &o, &r, &e) != 0) {
        fprintf(stderr, "\nerror: %s\n", e.msg);
        return 1;
    }
    mp_report_write_text(&r, stdout);
    int rc = 0;
    if (json && mp_report_write_json(&r, json, &e) != 0) {
        fprintf(stderr, "error: %s\n", e.msg);
        rc = 1;
    }
    mp_report_free(&r);
    free(keep);
    return rc;
}

int main(int argc, char **argv) {
    mp_args_utf8(&argc, &argv);
#ifdef _WIN32
    SetConsoleOutputCP(CP_UTF8);
#endif
    if (argc >= 2 && strcmp(argv[1], "--version") == 0) {
        printf("maxpack %s (zstd %s, zlib %s)\n", mp_version_string(), ZSTD_versionString(),
               zlibVersion());
        return 0;
    }
    if (argc >= 2 && (!strcmp(argv[1], "--help") || !strcmp(argv[1], "-h") || !strcmp(argv[1], "help"))) {
        usage(stdout);
        return 0;
    }
    if (argc >= 2 && strcmp(argv[1], "analyze") == 0) return cmd_analyze(argc - 2, argv + 2);
    if (argc >= 2) return bad_usage("orden desconocida", argv[1]);
    usage(stderr);
    return 2;
}
