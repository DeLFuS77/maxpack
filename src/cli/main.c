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
#include "mp_p5z.h"
#include "mp_pack.h"
#include "mp_platform.h"
#include "mp_report_out.h"
#include "mp_rules.h"
#include "mp_version.h"

static void usage(FILE *out) {
    fprintf(out,
            "Uso:\n"
            "  maxpack analyze <carpeta|imagen.exfat> [opciones]\n"
            "  maxpack pack <carpeta|imagen.exfat> -o <salida> [opciones]\n"
            "  maxpack unpack <juego.p5z> --folder|--original -o <carpeta> [--threads N]\n"
            "  maxpack info <juego.p5z>\n"
            "  maxpack --version\n"
            "\n"
            "Opciones de pack:\n"
            "  -o SALIDA          carpeta nueva o vacía (--folder) para copiar a la consola\n"
            "  --folder           salida en carpeta (por defecto)\n"
            "  --p5z              salida en un solo archivo .p5z para guardar en el PC\n"
            "  --level N          nivel de zstd, 1 a 22 (por defecto 19)\n"
            "  --threads N        hilos (por defecto, todos los de la CPU)\n"
            "  --runtime ARCHIVO  libSceAmpr.sprx de ampr-emu-ng para fakelib/\n"
            "  --keep-loose GLOB  dejar suelto también lo que case (se puede repetir)\n"
            "\n"
            "Opciones de analyze:\n"
            "  --sample N%%        porcentaje de la muestra (por defecto 2%%, mínimo 1 GiB)\n"
            "  --full             medir todo el juego, sin muestra\n"
            "  --json ARCHIVO     guardar el informe en JSON\n"
            "  --threads N        hilos (por defecto, todos los de la CPU)\n"
            "  --keep-loose GLOB  dejar suelto también lo que case (se puede repetir)\n"
            "  --no-dedup         no hacer el pase de deduplicación (lee todo el juego)\n"
            "  --no-transport     no medir el .p5z de transporte (zstd 19, ventana larga)\n"
            "  --list            solo listar los archivos y su clase, sin medir\n");
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
        } else if (!strcmp(a, "--no-transport")) {
            o.transport = 0;
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

static void print_size(FILE *out, uint64_t b) {
    double v = (double)b;
    const char *u = "B";
    if (v >= 1e9) {
        v /= 1e9;
        u = "GB";
    } else if (v >= 1e6) {
        v /= 1e6;
        u = "MB";
    } else if (v >= 1e3) {
        v /= 1e3;
        u = "KB";
    }
    char s[32];
    snprintf(s, sizeof s, "%.2f %s", v, u);
    for (char *p = s; *p; p++)
        if (*p == '.') *p = ',';
    fputs(s, out);
}

static int cmd_pack(int argc, char **argv) {
    mp_pack_opts o;
    mp_pack_opts_default(&o);
    const char *src = NULL, *dst = NULL;
    int p5z = 0;
    const char **keep = calloc((size_t)argc + 1, sizeof *keep);
    if (!keep) return 1;
    for (int i = 0; i < argc; i++) {
        const char *a = argv[i];
        int has_value = i + 1 < argc;
        if (!strcmp(a, "--folder")) {
            p5z = 0;
        } else if (!strcmp(a, "--p5z")) {
            p5z = 1;
        } else if (!strcmp(a, "-o") || !strcmp(a, "--level") || !strcmp(a, "--threads") ||
                   !strcmp(a, "--runtime") || !strcmp(a, "--keep-loose")) {
            if (!has_value) return bad_usage("falta el valor de", a);
            const char *v = argv[++i];
            char *end = NULL;
            if (!strcmp(a, "-o")) {
                dst = v;
            } else if (!strcmp(a, "--runtime")) {
                o.runtime = v;
            } else if (!strcmp(a, "--keep-loose")) {
                keep[o.nkeep++] = v;
            } else if (!strcmp(a, "--level")) {
                long l = strtol(v, &end, 10);
                if (end == v || *end || l < 1 || l > 22) return bad_usage("--level debe estar entre 1 y 22", v);
                o.level = (int)l;
            } else {
                long t = strtol(v, &end, 10);
                if (end == v || *end || t < 1 || t > 1024) return bad_usage("--threads debe estar entre 1 y 1024", v);
                o.threads = (unsigned)t;
            }
        } else if (a[0] == '-') {
            return bad_usage("opción desconocida", a);
        } else if (!src) {
            src = a;
        } else {
            return bad_usage("sobra un argumento", a);
        }
    }
    if (!src) return bad_usage("falta la carpeta o la imagen .exfat del juego", NULL);
    if (!dst) return bad_usage("falta la salida (-o)", NULL);
    o.keep_loose = keep;
    if (mp_path_kind(src) == 0) {
        fprintf(stderr, "error: no existe: %s\n", src);
        return 1;
    }
    if (mp_same_file(dst, src) || (mp_path_kind(src) == 2 && mp_path_is_inside(dst, src)))
        return bad_usage("la salida no puede ser el juego ni estar dentro de él", dst);
    if (p5z) {
        if (mp_path_kind(dst) != 0) return bad_usage("el .p5z de salida ya existe", dst);
        char *dir = mp_full_path(dst);
        int parent_ok = dir && to_parent(dir) && mp_path_kind(dir) == 2;
        free(dir);
        if (!parent_ok) return bad_usage("no existe la carpeta donde guardar el .p5z", dst);
    } else {
        if (mp_path_kind(dst) == 1) return bad_usage("la salida ya existe y es un archivo", dst);
        if (mp_path_kind(dst) == 2 && mp_dir_is_empty(dst) != 1)
            return bad_usage("la carpeta de salida no está vacía", dst);
    }

    mp_err e = {{0}};
    mp_pack_out out;
    if ((p5z ? mp_pack_out_p5z(dst, o.threads, &out, &e) : mp_pack_out_dir(dst, &out, &e)) != 0) {
        fprintf(stderr, "error: %s\n", e.msg);
        return 1;
    }
    mp_pack_result r;
    uint64_t t0 = mp_now_ns();
    int rc = mp_pack_run(src, &out, &o, &r, &e);
    out.destroy(out.ctx);
    free(keep);
    if (rc != 0) {
        fprintf(stderr, "\nerror: %s\n(%s queda incompleto: bórralo antes de repetir)\n", e.msg, dst);
        return 1;
    }
    printf("Empaquetado en %s (%.0f s)\n", dst, (double)(mp_now_ns() - t0) / 1e9);
    printf("  sueltos:      %llu archivos, ", (unsigned long long)r.loose_files);
    print_size(stdout, r.loose_bytes);
    printf("\n  empaquetados: %llu archivos, ", (unsigned long long)r.packed_files);
    print_size(stdout, r.packed_bytes);
    printf(" -> ");
    print_size(stdout, r.stored_bytes);
    printf(" en %llu volúmenes", (unsigned long long)r.volumes);
    if (r.packed_bytes) {
        char pct[32];
        snprintf(pct, sizeof pct, "%.1f", 100.0 * (1.0 - (double)r.stored_bytes / (double)r.packed_bytes));
        for (char *p = pct; *p; p++)
            if (*p == '.') *p = ',';
        printf(" (%s %% menos)", pct);
    }
    printf("\n");
    uint64_t psize = 0;
    if (p5z && mp_stat(dst, &psize, NULL) == 0) {
        printf("  .p5z: ");
        print_size(stdout, psize);
        printf("\n");
    }
    if (r.runtime_without_zstd)
        fprintf(stderr, "aviso: el runtime de fakelib/libSceAmpr.sprx no parece leer packs zstd: usa el "
                        "libSceAmpr.sprx de ampr-emu-ng (--runtime)\n");
    return 0;
}

static int cmd_info(int argc, char **argv) {
    if (argc != 1) return bad_usage("info necesita un archivo .p5z", NULL);
    mp_err e = {{0}};
    mp_p5z *p = NULL;
    if (mp_p5z_open(argv[0], &p, &e) != 0) {
        fprintf(stderr, "error: %s\n", e.msg);
        return 1;
    }
    int rc = mp_p5z_info(p, stdout) == 0 ? 0 : 1;
    mp_p5z_close(p);
    return rc;
}

static int cmd_unpack(int argc, char **argv) {
    const char *src = NULL, *dst = NULL;
    int mode = 0;   // 1 carpeta, 2 original
    unsigned threads = mp_cpu_count();
    for (int i = 0; i < argc; i++) {
        const char *a = argv[i];
        if (!strcmp(a, "--folder")) {
            mode = 1;
        } else if (!strcmp(a, "--original")) {
            mode = 2;
        } else if (!strcmp(a, "-o") || !strcmp(a, "--threads")) {
            if (i + 1 >= argc) return bad_usage("falta el valor de", a);
            const char *v = argv[++i];
            if (!strcmp(a, "-o")) {
                dst = v;
            } else {
                char *end = NULL;
                long t = strtol(v, &end, 10);
                if (end == v || *end || t < 1 || t > 1024) return bad_usage("--threads debe estar entre 1 y 1024", v);
                threads = (unsigned)t;
            }
        } else if (a[0] == '-') {
            return bad_usage("opción desconocida", a);
        } else if (!src) {
            src = a;
        } else {
            return bad_usage("sobra un argumento", a);
        }
    }
    if (!src) return bad_usage("falta el archivo .p5z", NULL);
    if (!mode) return bad_usage("indica --folder (carpeta para la consola) o --original (dump original)", NULL);
    if (!dst) return bad_usage("falta la carpeta de salida (-o)", NULL);
    if (mp_path_kind(dst) == 1) return bad_usage("la salida ya existe y es un archivo", dst);
    if (mp_path_kind(dst) == 2 && mp_dir_is_empty(dst) != 1) return bad_usage("la carpeta de salida no está vacía", dst);
    mp_err e = {{0}};
    mp_p5z *p = NULL;
    if (mp_p5z_open(src, &p, &e) != 0) {
        fprintf(stderr, "error: %s\n", e.msg);
        return 1;
    }
    uint64_t t0 = mp_now_ns();
    int rc = mode == 1 ? mp_unpack_folder(p, dst, threads, 1, &e) : mp_unpack_original(p, dst, threads, 1, &e);
    mp_p5z_close(p);
    if (rc != 0) {
        fprintf(stderr, "\nerror: %s\n(%s queda incompleta: bórrala antes de repetir)\n", e.msg, dst);
        return 1;
    }
    printf("%s en %s (%.0f s)%s\n", mode == 1 ? "Carpeta para la consola" : "Dump original reconstruido", dst,
           (double)(mp_now_ns() - t0) / 1e9, mode == 2 ? "; todos los SHA-256 coinciden" : "");
    return 0;
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
    if (argc >= 2 && strcmp(argv[1], "pack") == 0) return cmd_pack(argc - 2, argv + 2);
    if (argc >= 2 && strcmp(argv[1], "unpack") == 0) return cmd_unpack(argc - 2, argv + 2);
    if (argc >= 2 && strcmp(argv[1], "info") == 0) return cmd_info(argc - 2, argv + 2);
    if (argc >= 2) return bad_usage("orden desconocida", argv[1]);
    usage(stderr);
    return 2;
}
