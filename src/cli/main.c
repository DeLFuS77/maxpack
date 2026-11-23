// SPDX-License-Identifier: GPL-3.0-or-later
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <zlib.h>
#include <zstd.h>
#ifdef _WIN32
#define WIN32_LEAN_AND_MEAN
#include <windows.h>
#include <fcntl.h>
#include <io.h>
#else
#include <unistd.h>
#endif

#include "mp_analyze.h"
#include "mp_describe.h"
#include "mp_p5z.h"
#include "mp_parallel.h"
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
            "  maxpack verify <juego.p5z | carpeta> [--against <origen>] [--threads N]\n"
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
            "  --max-file-size N  con --p5z: parar y borrar el .p5z si pasa de N bytes\n"
            "\n"
            "Opciones de pack, unpack y verify (para una interfaz):\n"
            "  --progress json    una línea JSON por evento en stdout (el texto sigue en stderr)\n"
            "  --control stdin    leer de stdin: la línea \"cancel\" o cerrar la entrada cancela\n"
            "  Códigos de salida: 0 bien, 1 error, 2 uso, 130 cancelado\n"
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

// ---- --progress json y --control stdin -------------------------------------------------------

static int g_json;                  // --progress json
static int g_control;               // --control stdin
static const char *g_err_code;      // código de error para el resultado JSON
static int g_result_done;           // ya se escribió la línea "result"

static const char *phase_code(const char *phase) {
    if (!strncmp(phase, "Copiando", 8)) return "copying";
    if (!strncmp(phase, "Empaquetando", 12)) return "packing";
    if (!strncmp(phase, "Verificando", 11)) return "verifying";
    if (!strncmp(phase, "Extrayendo", 10) || !strncmp(phase, "Reconstruyendo", 14)) return "unpacking";
    return "working";
}

static void json_progress(void *ctx, const char *phase, uint64_t done, uint64_t total) {
    (void)ctx;
    printf("{\"event\":\"progress\",\"phase\":\"%s\",\"done\":%llu,\"total\":%llu}\n", phase_code(phase),
           (unsigned long long)done, (unsigned long long)total);
    fflush(stdout);
}

// Línea "result": ok y, si falla, el código; extra son campos ya formateados (con coma inicial) o "".
static void json_result(int ok, const char *err, const char *extra) {
    if (g_result_done) return;
    g_result_done = 1;
    printf("{\"event\":\"result\",\"ok\":%s", ok ? "true" : "false");
    if (err) printf(",\"error\":\"%s\"", err);
    printf("%s}\n", extra ? extra : "");
    fflush(stdout);
}

static void control_thread(void *arg) {
    (void)arg;
    char line[64];
    size_t n = 0;
    for (;;) {
        char c;
#ifdef _WIN32
        DWORD got = 0;
        if (!ReadFile(GetStdHandle(STD_INPUT_HANDLE), &c, 1, &got, NULL) || got == 0) break;
#else
        if (read(0, &c, 1) != 1) break;
#endif
        if (c == '\n') {
            line[n] = 0;
            if (!strcmp(line, "cancel")) {
                mp_cancel_request();
                return;
            }
            n = 0;
        } else if (c != '\r' && n < sizeof line - 1) {
            line[n++] = c;
        }
    }
    mp_cancel_request();   // fin de la entrada: quien nos lanzó se fue o cerró stdin
}

static void start_json_and_control(void) {
    if (g_json) {
#ifdef _WIN32
        _setmode(_fileno(stdout), _O_BINARY);   // líneas con \n, sin \r
#endif
        mp_progress_hook h = {json_progress, NULL};
        mp_progress_set_hook(&h);
    }
    if (g_control) {
        mp_thread *t = NULL;
        mp_thread_start(&t, control_thread, NULL);   // no se une: puede quedar bloqueado leyendo
    }
}

// Cierra una orden: si falló y se pidió JSON, escribe la línea "result"; cancelado sale con 130.
static int finish_cmd(int rc) {
    if (rc != 0 && mp_cancelled()) {
        g_err_code = "cancelled";
        rc = 130;
    }
    if (g_json && rc != 0) json_result(0, g_err_code ? g_err_code : "io", NULL);
    return rc;
}

// Deja la salida como estaba antes: la borra si no existía, o vacía su contenido si era una
// carpeta vacía.
static int rm_entry_cb(void *ctx, const char *name, int is_dir, uint64_t size) {
    (void)is_dir;
    (void)size;
    const char *dir = ctx;
    size_t n = strlen(dir) + strlen(name) + 2;
    char *full = malloc(n);
    if (!full) return 0;
    snprintf(full, n, "%s/%s", dir, name);
    mp_remove_tree(full, NULL);
    free(full);
    return 0;
}

static void clean_output(const char *dst, int existed) {
    if (!existed)
        mp_remove_tree(dst, NULL);
    else if (mp_path_kind(dst) == 2)
        mp_dir_list(dst, rm_entry_cb, (void *)dst, NULL);
}

static int sum_cb(void *ctx, const char *rel, int is_dir, uint64_t size, int64_t mtime) {
    (void)rel;
    (void)mtime;
    if (!is_dir) *(uint64_t *)ctx += size;
    return 0;
}

static uint64_t folder_bytes(const char *dir) {
    uint64_t total = 0;
    mp_err e = {{0}};
    mp_dir_walk(dir, sum_cb, &total, &e);
    return total;
}

static int bad_usage(const char *msg, const char *arg) {
    if (!g_err_code) g_err_code = "bad_input";
    fprintf(stderr, "error: %s%s%s\n\n", msg, arg ? ": " : "", arg ? arg : "");
    usage(stderr);
    return 2;
}

// Opciones comunes a pack, unpack y verify. 0 = no es nuestra, 1 = consumida, -1 = uso incorrecto.
static int common_opt(int argc, char **argv, int *i) {
    const char *a = argv[*i];
    if (strcmp(a, "--progress") && strcmp(a, "--control")) return 0;
    if (*i + 1 >= argc) {
        bad_usage("falta el valor de", a);
        return -1;
    }
    const char *v = argv[++*i];
    if (!strcmp(a, "--progress")) {
        if (strcmp(v, "json")) {
            bad_usage("--progress solo admite json", v);
            return -1;
        }
        g_json = 1;
    } else {
        if (strcmp(v, "stdin")) {
            bad_usage("--control solo admite stdin", v);
            return -1;
        }
        g_control = 1;
    }
    return 1;
}

// Antes de interpretar nada: con --progress json, los errores de uso también salen en JSON.
static void prescan_json(int argc, char **argv) {
    for (int i = 0; i + 1 < argc; i++)
        if (!strcmp(argv[i], "--progress") && !strcmp(argv[i + 1], "json")) g_json = 1;
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

static int run_pack(int argc, char **argv);
static int run_unpack(int argc, char **argv);
static int run_verify(int argc, char **argv);

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
    prescan_json(argc, argv);
    return finish_cmd(run_pack(argc, argv));
}

static int run_pack(int argc, char **argv) {
    mp_pack_opts o;
    mp_pack_opts_default(&o);
    const char *src = NULL, *dst = NULL;
    int p5z = 0;
    uint64_t max_size = 0;
    const char **keep = calloc((size_t)argc + 1, sizeof *keep);
    if (!keep) return 1;
    for (int i = 0; i < argc; i++) {
        const char *a = argv[i];
        int has_value = i + 1 < argc;
        int common = common_opt(argc, argv, &i);
        if (common < 0) {
            free(keep);
            return 2;
        }
        if (common) {
            continue;
        } else if (!strcmp(a, "--folder")) {
            p5z = 0;
        } else if (!strcmp(a, "--p5z")) {
            p5z = 1;
        } else if (!strcmp(a, "-o") || !strcmp(a, "--level") || !strcmp(a, "--threads") ||
                   !strcmp(a, "--runtime") || !strcmp(a, "--keep-loose") || !strcmp(a, "--max-file-size")) {
            if (!has_value) return bad_usage("falta el valor de", a);
            const char *v = argv[++i];
            char *end = NULL;
            if (!strcmp(a, "--max-file-size")) {
                unsigned long long m = strtoull(v, &end, 10);
                if (end == v || *end || m == 0 || v[0] == '-') return bad_usage("--max-file-size debe ser un número de bytes mayor que 0", v);
                max_size = m;
            } else if (!strcmp(a, "-o")) {
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
    if (max_size && !p5z) return bad_usage("--max-file-size solo sirve con --p5z", NULL);
    o.keep_loose = keep;
    if (mp_path_kind(src) == 0) {
        g_err_code = "bad_input";
        fprintf(stderr, "error: no existe: %s\n", src);
        return 1;
    }
    if (mp_same_file(dst, src) || (mp_path_kind(src) == 2 && mp_path_is_inside(dst, src)))
        return bad_usage("la salida no puede ser el juego ni estar dentro de él", dst);
    if (p5z) {
        if (mp_path_kind(dst) != 0) {
            g_err_code = "exists";
            return bad_usage("el .p5z de salida ya existe", dst);
        }
        char *dir = mp_full_path(dst);
        int parent_ok = dir && to_parent(dir) && mp_path_kind(dir) == 2;
        free(dir);
        if (!parent_ok) return bad_usage("no existe la carpeta donde guardar el .p5z", dst);
    } else {
        if (mp_path_kind(dst) == 1) {
            g_err_code = "exists";
            return bad_usage("la salida ya existe y es un archivo", dst);
        }
        if (mp_path_kind(dst) == 2 && mp_dir_is_empty(dst) != 1) {
            g_err_code = "exists";
            return bad_usage("la carpeta de salida no está vacía", dst);
        }
    }

    int existed = mp_path_kind(dst) == 2;
    start_json_and_control();
    mp_err e = {{0}};
    mp_pack_out out;
    if ((p5z ? mp_pack_out_p5z_ex(dst, o.threads, max_size, &out, &e) : mp_pack_out_dir(dst, &out, &e)) != 0) {
        fprintf(stderr, "error: %s\n", e.msg);
        free(keep);
        return 1;
    }
    mp_pack_result r;
    uint64_t t0 = mp_now_ns();
    int rc = mp_pack_run(src, &out, &o, &r, &e);
    int too_big = rc != 0 && p5z && mp_pack_out_p5z_too_big(&out);
    out.destroy(out.ctx);
    free(keep);
    if (rc != 0) {
        if (!p5z) clean_output(dst, existed);   // el .p5z a medias ya lo borra el escritor
        if (too_big) g_err_code = "too_big";
        fprintf(stderr, "\nerror: %s\n(no queda nada escrito en %s)\n", e.msg, dst);
        return 1;
    }
    FILE *hum = g_json ? stderr : stdout;   // con --progress json, stdout es solo JSON
    fprintf(hum, "Empaquetado en %s (%.0f s)\n", dst, (double)(mp_now_ns() - t0) / 1e9);
    fprintf(hum, "  sueltos:      %llu archivos, ", (unsigned long long)r.loose_files);
    print_size(hum, r.loose_bytes);
    fprintf(hum, "\n  empaquetados: %llu archivos, ", (unsigned long long)r.packed_files);
    print_size(hum, r.packed_bytes);
    fprintf(hum, " -> ");
    print_size(hum, r.stored_bytes);
    fprintf(hum, " en %llu volúmenes", (unsigned long long)r.volumes);
    if (r.packed_bytes) {
        char pct[32];
        snprintf(pct, sizeof pct, "%.1f", 100.0 * (1.0 - (double)r.stored_bytes / (double)r.packed_bytes));
        for (char *p = pct; *p; p++)
            if (*p == '.') *p = ',';
        fprintf(hum, " (%s %% menos)", pct);
    }
    fprintf(hum, "\n");
    uint64_t psize = 0;
    if (p5z && mp_stat(dst, &psize, NULL) == 0) {
        fprintf(hum, "  .p5z: ");
        print_size(hum, psize);
        fprintf(hum, "\n");
    }
    if (g_json) {
        char extra[256];
        snprintf(extra, sizeof extra,
                 ",\"original_size\":%llu,\"output_size\":%llu,\"loose_bytes\":%llu,\"packed_bytes\":%llu,"
                 "\"stored_bytes\":%llu,\"volumes\":%llu,\"runtime_without_zstd\":%s",
                 (unsigned long long)(r.loose_bytes + r.packed_bytes),
                 (unsigned long long)(p5z ? psize : folder_bytes(dst)), (unsigned long long)r.loose_bytes,
                 (unsigned long long)r.packed_bytes, (unsigned long long)r.stored_bytes,
                 (unsigned long long)r.volumes, r.runtime_without_zstd ? "true" : "false");
        json_result(1, NULL, extra);
    }
    if (r.runtime_without_zstd)
        fprintf(stderr, "aviso: el runtime de fakelib/libSceAmpr.sprx no parece leer packs zstd: usa el "
                        "libSceAmpr.sprx de ampr-emu-ng (--runtime)\n");
    return 0;
}

// Escribe s como cadena JSON: comillas, barras y controles escapados; UTF-8 no válido pasa a U+FFFD.
static void print_json_string(const char *s) {
    const unsigned char *p = (const unsigned char *)s;
    putchar('"');
    while (*p) {
        unsigned c = *p;
        if (c == '"' || c == '\\') {
            printf("\\%c", (char)c);
            p++;
        } else if (c < 0x20 || c == 0x7f) {
            printf("\\u%04x", c);
            p++;
        } else if (c < 0x80) {
            putchar((int)c);
            p++;
        } else {
            int n = c >= 0xf0 && c < 0xf5 ? 4 : c >= 0xe0 ? 3 : c >= 0xc2 && c < 0xe0 ? 2 : 0;
            int ok = n > 0 && c < 0xf5;
            for (int i = 1; ok && i < n; i++) ok = (p[i] & 0xc0) == 0x80;   // un NUL también falla aquí
            if (ok && n == 3) {
                unsigned cp = (c & 0x0f) << 12 | (p[1] & 0x3f) << 6 | (p[2] & 0x3f);
                ok = cp >= 0x800 && !(cp >= 0xd800 && cp < 0xe000);
            } else if (ok && n == 4) {
                unsigned cp = (c & 0x07) << 18 | (p[1] & 0x3f) << 12 | (p[2] & 0x3f) << 6 | (p[3] & 0x3f);
                ok = cp >= 0x10000 && cp <= 0x10ffff;
            }
            if (ok) {
                fwrite(p, 1, (size_t)n, stdout);
                p += n;
            } else {
                fputs("\xef\xbf\xbd", stdout);
                p++;
            }
        }
    }
    putchar('"');
}

static int cmd_info_json(const char *path) {
    mp_err e = {{0}};
    mp_describe d;
    if (mp_describe_path(path, &d, &e) != 0) {
        fprintf(stderr, "error: %s\n", e.msg);
        return 1;
    }
    printf("{\"kind\":\"%s\",\"title_id\":", d.kind);
    print_json_string(d.title_id);
    printf(",\"version\":");
    print_json_string(d.version);
    printf(",\"name\":");
    print_json_string(d.name);
    printf(",\"original_size\":%llu,\"install_size\":%llu,\"p5z_size\":%llu,\"files\":%llu",
           (unsigned long long)d.original_size, (unsigned long long)d.install_size,
           (unsigned long long)d.p5z_size, (unsigned long long)d.files);
    printf(",\"packed_files\":%llu,\"volumes\":%llu", (unsigned long long)d.packed_files,
           (unsigned long long)d.volumes);
    printf(",\"has_runtime\":%s,\"has_zstd_runtime\":%s,\"packed\":%s}\n", d.has_runtime ? "true" : "false",
           d.has_zstd_runtime ? "true" : "false", d.packed ? "true" : "false");
    fflush(stdout);
    return 0;
}

static int cmd_info(int argc, char **argv) {
    if (argc == 2 && !strcmp(argv[0], "--json")) return cmd_info_json(argv[1]);
    if (argc == 2 && !strcmp(argv[1], "--json")) return cmd_info_json(argv[0]);
    if (argc != 1 || !strcmp(argv[0], "--json")) return bad_usage("info necesita un archivo .p5z (o --json y una ruta)", NULL);
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

// icon <ruta> -o <png>: 0 con el PNG escrito, 1 si el juego no tiene icono (o no se pudo escribir).
static int cmd_icon(int argc, char **argv) {
    const char *src = NULL, *out = NULL;
    for (int i = 0; i < argc; i++) {
        if (!strcmp(argv[i], "-o")) {
            if (i + 1 >= argc || out) return bad_usage("-o necesita un valor", NULL);
            out = argv[++i];
        } else if (argv[i][0] == '-' && argv[i][1]) {
            return bad_usage("opción desconocida", argv[i]);
        } else if (!src) {
            src = argv[i];
        } else {
            return bad_usage("icon admite un solo origen", argv[i]);
        }
    }
    if (!src || !out) return bad_usage("icon necesita un origen y -o <icono.png>", NULL);
    mp_err e = {{0}};
    uint8_t *png = NULL;
    size_t len = 0;
    if (mp_describe_icon(src, &png, &len, &e) != 0) {
        fprintf(stderr, "error: %s\n", e.msg);
        return 1;
    }
    int rc = mp_write_file(out, png, len, &e);
    free(png);
    if (rc != 0) {
        fprintf(stderr, "error: %s\n", e.msg);
        return 1;
    }
    return 0;
}

static int cmd_unpack(int argc, char **argv) {
    prescan_json(argc, argv);
    return finish_cmd(run_unpack(argc, argv));
}

static int run_unpack(int argc, char **argv) {
    const char *src = NULL, *dst = NULL;
    int mode = 0;   // 1 carpeta, 2 original
    unsigned threads = mp_cpu_count();
    for (int i = 0; i < argc; i++) {
        const char *a = argv[i];
        int common = common_opt(argc, argv, &i);
        if (common < 0) return 2;
        if (common) {
            continue;
        } else if (!strcmp(a, "--folder")) {
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
    if (mp_path_kind(dst) == 1) {
        g_err_code = "exists";
        return bad_usage("la salida ya existe y es un archivo", dst);
    }
    if (mp_path_kind(dst) == 2 && mp_dir_is_empty(dst) != 1) {
        g_err_code = "exists";
        return bad_usage("la carpeta de salida no está vacía", dst);
    }
    int existed = mp_path_kind(dst) == 2;
    start_json_and_control();
    mp_err e = {{0}};
    mp_p5z *p = NULL;
    if (mp_p5z_open(src, &p, &e) != 0) {
        g_err_code = "bad_input";
        fprintf(stderr, "error: %s\n", e.msg);
        return 1;
    }
    uint64_t t0 = mp_now_ns();
    int rc = mode == 1 ? mp_unpack_folder(p, dst, threads, 1, &e) : mp_unpack_original(p, dst, threads, 1, &e);
    mp_p5z_close(p);
    if (rc != 0) {
        clean_output(dst, existed);
        fprintf(stderr, "\nerror: %s\n(no queda nada escrito en %s)\n", e.msg, dst);
        return 1;
    }
    FILE *hum = g_json ? stderr : stdout;
    fprintf(hum, "%s en %s (%.0f s)%s\n", mode == 1 ? "Carpeta para la consola" : "Dump original reconstruido", dst,
            (double)(mp_now_ns() - t0) / 1e9, mode == 2 ? "; todos los SHA-256 coinciden" : "");
    if (g_json) {
        char extra[64];
        snprintf(extra, sizeof extra, ",\"bytes\":%llu", (unsigned long long)folder_bytes(dst));
        json_result(1, NULL, extra);
    }
    return 0;
}

static int cmd_verify(int argc, char **argv) {
    prescan_json(argc, argv);
    return finish_cmd(run_verify(argc, argv));
}

static int run_verify(int argc, char **argv) {
    const char *target = NULL, *against = NULL;
    unsigned threads = mp_cpu_count();
    for (int i = 0; i < argc; i++) {
        const char *a = argv[i];
        int common = common_opt(argc, argv, &i);
        if (common < 0) return 2;
        if (common) {
            continue;
        } else if (!strcmp(a, "--against") || !strcmp(a, "--threads")) {
            if (i + 1 >= argc) return bad_usage("falta el valor de", a);
            const char *v = argv[++i];
            if (!strcmp(a, "--against")) {
                against = v;
            } else {
                char *end = NULL;
                long t = strtol(v, &end, 10);
                if (end == v || *end || t < 1 || t > 1024) return bad_usage("--threads debe estar entre 1 y 1024", v);
                threads = (unsigned)t;
            }
        } else if (a[0] == '-') {
            return bad_usage("opción desconocida", a);
        } else if (!target) {
            target = a;
        } else {
            return bad_usage("sobra un argumento", a);
        }
    }
    if (!target) return bad_usage("falta el .p5z o la carpeta que verificar", NULL);
    int kind = mp_path_kind(target);
    if (kind == 0) {
        g_err_code = "bad_input";
        fprintf(stderr, "error: no existe: %s\n", target);
        return 1;
    }
    if (kind == 1 && against) return bad_usage("--against solo sirve para carpetas (el .p5z ya lleva sus SHA-256)", NULL);
    start_json_and_control();
    mp_err e = {{0}};
    mp_verify_result r;
    uint64_t t0 = mp_now_ns();
    int rc = kind == 2 ? mp_verify_folder(target, against, threads, 1, &r, &e)
                       : mp_verify_p5z(target, threads, 1, &r, &e);
    if (rc != 0) {
        fprintf(stderr, "error: %s\n", e.msg);
        return 1;
    }
    FILE *hum = g_json ? stderr : stdout;
    fprintf(hum, "verify: %llu correctos, %llu con fallos (%.0f s)\n", (unsigned long long)r.files_ok,
            (unsigned long long)r.files_bad, (double)(mp_now_ns() - t0) / 1e9);
    if (g_json) {
        char extra[96];
        snprintf(extra, sizeof extra, ",\"files_ok\":%llu,\"files_bad\":%llu", (unsigned long long)r.files_ok,
                 (unsigned long long)r.files_bad);
        json_result(r.files_bad == 0, NULL, extra);
    }
    return r.files_bad ? 1 : 0;
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
    if (argc >= 2 && strcmp(argv[1], "icon") == 0) return cmd_icon(argc - 2, argv + 2);
    if (argc >= 2 && strcmp(argv[1], "verify") == 0) return cmd_verify(argc - 2, argv + 2);
    if (argc >= 2) return bad_usage("orden desconocida", argv[1]);
    usage(stderr);
    return 2;
}
