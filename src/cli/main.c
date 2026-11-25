// SPDX-License-Identifier: GPL-3.0-or-later
#include <stdarg.h>
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
#include <signal.h>
#include <unistd.h>
#endif

#include "mp_analyze.h"
#include "mp_bench.h"
#include "mp_describe.h"
#include "mp_levels.h"
#include "mp_p5z.h"
#include "mp_parallel.h"
#include "mp_pack.h"
#include "mp_platform.h"
#include "mp_play.h"
#include "mp_report_out.h"
#include "mp_rules.h"
#include "mp_version.h"

static void usage(FILE *out) {
    fprintf(out,
            "Usage:\n"
            "  maxpack analyze <folder|image> [options]\n"
            "  maxpack analyze <folder|image> --levels [L,L,...] [options]   compare the levels\n"
            "  maxpack pack <folder|image> -o <output> [options]\n"
            "  maxpack unpack <game.p5z> --folder|--original -o <folder> [--threads N]\n"
            "  maxpack info <game.p5z>\n"
            "  maxpack info --json <folder|image|game.p5z>\n"
            "  maxpack info --play <game.p5z>   what the console needs to play it without installing (JSON)\n"
            "  maxpack icon <folder|image|game.p5z> -o <icon.png>\n"
            "  maxpack verify <game.p5z | folder> [--against <source>] [--threads N]\n"
            "  maxpack measure [<folder>] [options]   measure this PC (and the folder's disk), with no game\n"
            "  maxpack --version\n"
            "  maxpack --capabilities   what this version can do, as JSON (for a front end)\n"
            "\n"
            "An image is an .exfat, an .ffpkg (UFS2), an .ffpfs (PFS) or an .ffpfsc (a PFS image that holds\n"
            "one of them, usually compressed); MaxPack tells which by its contents, not by its name.\n"
            "\n"
            "Pack options:\n"
            "  -o OUTPUT          new or empty folder (--folder) to copy to the console\n"
            "  --folder           output as a folder (default)\n"
            "  --p5z              output as a single .p5z file to keep on the PC\n"
            "  --level N          zstd level, 1 to 22 (default 19)\n"
            "  --threads N        threads (default: all the CPU's)\n"
            "  --runtime FILE     ampr-emu-ng's libSceAmpr.sprx for fakelib/\n"
            "  --keep-loose GLOB  also leave whatever matches loose (can be repeated)\n"
            "  --max-file-size N  with --p5z: stop and delete the .p5z if it goes over N bytes\n"
            "  --volume-limit N   for testing: volumes of at most N bytes (262208 to 4294901760,\n"
            "                     default 4294901760), to get several from a small game\n"
            "\n"
            "Options for pack, unpack, verify, analyze --levels and measure (for a front end):\n"
            "  --progress json    one JSON line per event on stdout (the text stays on stderr)\n"
            "  --control stdin    read from stdin: the line \"cancel\" or closing the input cancels\n"
            "  Exit codes: 0 ok, 1 error, 2 usage, 130 cancelled\n"
            "\n"
            "Options for analyze:\n"
            "  --sample N%%        sample percentage (default 2%%, at least 1 GiB)\n"
            "  --full             measure the whole game, no sample\n"
            "  --json FILE        save the report as JSON\n"
            "  --threads N        threads (default: all the CPU's)\n"
            "  --keep-loose GLOB  also leave whatever matches loose (can be repeated)\n"
            "  --no-dedup         skip the deduplication pass (reads the whole game)\n"
            "  --no-transport     do not measure the transport .p5z (zstd 19, long window)\n"
            "  --list             only list the files and their class, without measuring\n"
            "\n"
            "analyze --levels (also takes --sample, --full, --threads, --keep-loose and --json):\n"
            "  --levels [LIST]    pack a sample at each level with the pack's own pipeline and tell the\n"
            "                     size of the .p5z and of the folder and the time to pack the whole game\n"
            "                     on this PC (default 3,9,15,19; levels 1 to 22 separated by commas)\n"
            "\n"
            "Options for measure (it packs generated data at 25, 50, 75 and 100 %% of the CPU's threads\n"
            "and recommends the fewest threads less than 5 %% slower than the fastest):\n"
            "  <folder>           also measure its disk (the destination): a temporary file is written\n"
            "                     there, read back and deleted; nothing else is written\n"
            "  --level N          zstd level, 1 to 22 (default 19)\n"
            "  --seconds S        about how long each measure lasts, 0.1 to 60 (default 3)\n"
            "  --json FILE        save the result as JSON\n");
}

// ---- --progress json and --control stdin -------------------------------------------------------

static int g_json;                  // --progress json
static int g_control;               // --control stdin
static const char *g_err_code;      // error code for the JSON result
static int g_result_done;           // the "result" line has already been written

static const char *phase_code(const char *phase) {
    if (!strncmp(phase, "Copying", 7)) return "copying";
    if (!strncmp(phase, "Packing", 7)) return "packing";
    if (!strncmp(phase, "Verifying", 9)) return "verifying";
    if (!strncmp(phase, "Extracting", 10) || !strncmp(phase, "Rebuilding", 10)) return "unpacking";
    return "working";
}

static void json_progress(void *ctx, const char *phase, uint64_t done, uint64_t total) {
    (void)ctx;
    printf("{\"event\":\"progress\",\"phase\":\"%s\",\"done\":%llu,\"total\":%llu}\n", phase_code(phase),
           (unsigned long long)done, (unsigned long long)total);
    fflush(stdout);
}

// "result" line: ok and, on failure, the code; extra is already-formatted fields (with a leading comma) or "".
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
    mp_cancel_request();   // end of input: whoever launched us is gone or closed stdin
}

static void start_json_and_control(void) {
#ifndef _WIN32
    // if whoever launched us goes away, writing to its pipe must not kill us before we clean up
    if (g_json || g_control) signal(SIGPIPE, SIG_IGN);
#endif
    if (g_json) {
#ifdef _WIN32
        _setmode(_fileno(stdout), _O_BINARY);   // lines end in \n, not \r\n
#endif
        mp_progress_hook h = {json_progress, NULL};
        mp_progress_set_hook(&h);
    }
    if (g_control) {
        mp_thread *t = NULL;
        mp_thread_start(&t, control_thread, NULL);   // never joined: it may stay blocked reading
    }
}

// Finishes a command: if it failed and JSON was requested, writes the "result" line; cancelled exits with 130.
static int finish_cmd(int rc) {
    if (rc != 0 && mp_cancelled()) {
        g_err_code = "cancelled";
        rc = 130;
    }
    if (g_json && rc != 0) json_result(0, g_err_code ? g_err_code : "io", NULL);
    return rc;
}

// Leaves the output as it was before: deletes it if it did not exist, or empties its contents if it
// was an empty folder. Returns 0, or -1 with the reason in e if something could not be deleted (everything is tried).
typedef struct {
    const char *dir;
    int failed;
    mp_err *e;
} rm_ctx;

static int rm_entry_cb(void *ctx, const char *name, int is_dir, uint64_t size) {
    (void)is_dir;
    (void)size;
    rm_ctx *c = ctx;
    size_t n = strlen(c->dir) + strlen(name) + 2;
    char *full = malloc(n);
    if (!full) {
        if (!c->failed) mp_err_set(c->e, "out of memory");
        c->failed = 1;
        return 0;
    }
    snprintf(full, n, "%s/%s", c->dir, name);
    mp_err one = {{0}};
    if (mp_remove_tree(full, &one) != 0) {
        if (!c->failed) mp_err_set(c->e, "%s", one.msg);
        c->failed = 1;
    }
    free(full);
    return 0;
}

static int clean_output(const char *dst, int existed, mp_err *e) {
    if (!existed) return mp_remove_tree(dst, e);
    if (mp_path_kind(dst) != 2) return 0;
    rm_ctx c = {dst, 0, e};
    if (mp_dir_list(dst, rm_entry_cb, &c, e) != 0) return -1;
    return c.failed ? -1 : 0;
}

// After a failure: the error and what is left in dst. left is whether what was written could not be deleted, and why, the
// reason.
static void say_left(const char *msg, const char *dst, int left, const char *why) {
    fprintf(stderr, "\nerror: %s\n", msg);
    if (!left)
        fprintf(stderr, "(nothing is left written in %s)\n", dst);
    else if (why && why[0])
        fprintf(stderr, "(could not delete what was written: it is left half-done in %s; %s)\n", dst, why);
    else
        fprintf(stderr, "(could not delete what was written: it is left half-done in %s)\n", dst);
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

// Options common to pack, unpack and verify. 0 = not ours, 1 = consumed, -1 = bad usage.
static int common_opt(int argc, char **argv, int *i) {
    const char *a = argv[*i];
    if (strcmp(a, "--progress") && strcmp(a, "--control")) return 0;
    if (*i + 1 >= argc) {
        bad_usage("missing value for", a);
        return -1;
    }
    const char *v = argv[++*i];
    if (!strcmp(a, "--progress")) {
        if (strcmp(v, "json")) {
            bad_usage("--progress only accepts json", v);
            return -1;
        }
        g_json = 1;
    } else {
        if (strcmp(v, "stdin")) {
            bad_usage("--control only accepts stdin", v);
            return -1;
        }
        g_control = 1;
    }
    return 1;
}

// Before interpreting anything: with --progress json, usage errors also come out as JSON.
static void prescan_json(int argc, char **argv) {
    for (int i = 0; i + 1 < argc; i++)
        if (!strcmp(argv[i], "--progress") && !strcmp(argv[i + 1], "json")) g_json = 1;
}

static int is_sep(char c) { return c == '/' || c == '\\'; }

// Leaves p at its parent folder (C:\a\b -> C:\a, C:\a -> C:\, /a -> /).
// Returns 0 if p is already a root.
static int to_parent(char *p) {
    size_t n = strlen(p);
    while (n > 0 && is_sep(p[n - 1])) n--;
    size_t end = n;
    while (n > 0 && !is_sep(p[n - 1])) n--;
    if (n == 0 || n == end) return 0;
    size_t sep = n - 1;
    if (sep == 0 || (sep == 2 && p[1] == ':'))
        p[n] = 0;   // root: keeps its separator
    else
        p[sep] = 0;
    return 1;
}

// Checks --json before analysing: its folder exists and it does not overwrite the game or fall inside it.
static int check_json_target(const char *src, const char *json) {
    char *dir = mp_full_path(json);
    if (!dir) return bad_usage("invalid --json path", json);
    int rc = 0;
    if (mp_path_kind(json) == 2) {
        rc = bad_usage("--json points to a folder", json);
    } else if (mp_same_file(json, src)) {
        rc = bad_usage("--json cannot overwrite the game being analysed", json);
    } else if (!to_parent(dir) || mp_path_kind(dir) != 2) {
        rc = bad_usage("the folder to save --json in does not exist", json);
    } else if (mp_path_kind(src) == 2) {
        do {
            if (mp_same_file(dir, src)) {
                rc = bad_usage("--json cannot write inside the folder being analysed", json);
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
               mp_rules_classify(r, en->path, en->size) == MP_LOOSE ? "loose" : "packable",
               en->path);
    }
    mp_rules_free(r);
    mp_source_close(s);
    return 0;
}

static int run_levels(int argc, char **argv);
static int has_arg(int argc, char **argv, const char *a) {
    for (int i = 0; i < argc; i++)
        if (!strcmp(argv[i], a)) return 1;
    return 0;
}

static int cmd_analyze(int argc, char **argv) {
    if (has_arg(argc, argv, "--levels")) {
        prescan_json(argc, argv);
        return finish_cmd(run_levels(argc, argv));
    }
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
            if (!has_value) return bad_usage("missing value for", a);
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
                    return bad_usage("--sample must be between 0 and 100 %", v);
                o.sample_fraction = p / 100.0;
            } else {
                char *end = NULL;
                long t = strtol(v, &end, 10);
                if (end == v || *end || t < 1 || t > 1024)
                    return bad_usage("--threads must be between 1 and 1024", v);
                o.threads = (unsigned)t;
            }
        } else if (a[0] == '-' && a[1] == '-') {
            return bad_usage("unknown option", a);
        } else if (!src) {
            src = a;
        } else {
            return bad_usage("extra argument", a);
        }
    }
    if (!src) return bad_usage("missing the folder or image", NULL);
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

static void size_text(char *s, size_t cap, uint64_t b) {
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
    snprintf(s, cap, "%.2f %s", v, u);
}

static void print_size(FILE *out, uint64_t b) {
    char s[32];
    size_text(s, sizeof s, b);
    fputs(s, out);
}

// A time for people: "45 s", "12 min 5 s", "2 h 7 min".
static void duration_text(char *s, size_t cap, uint64_t sec) {
    if (sec < 60)
        snprintf(s, cap, "%llu s", (unsigned long long)sec);
    else if (sec < 3600)
        snprintf(s, cap, "%llu min %llu s", (unsigned long long)(sec / 60), (unsigned long long)(sec % 60));
    else
        snprintf(s, cap, "%llu h %llu min", (unsigned long long)(sec / 3600), (unsigned long long)(sec / 60 % 60));
}

// Appends to a JSON text being made in a fixed buffer; *at stays at the end (and the text is cut, never
// overrun, if it does not fit).
static void add_json(char *buf, size_t cap, size_t *at, const char *fmt, ...) {
    if (*at >= cap) return;
    va_list ap;
    va_start(ap, fmt);
    int n = vsnprintf(buf + *at, cap - *at, fmt, ap);
    va_end(ap);
    if (n > 0) *at = *at + (size_t)n < cap ? *at + (size_t)n : cap;
}

// ---- analyze --levels ----------------------------------------------------------------------------

// The fields of a level, without the braces.
static void level_json(char *buf, size_t cap, size_t *at, const mp_level_row *row) {
    add_json(buf, cap, at,
             "\"level\":%d,\"packed\":%llu,\"p5z_size\":%llu,\"folder_size\":%llu,\"rate\":%llu,"
             "\"p5z_seconds\":%llu,\"folder_seconds\":%llu",
             row->level, (unsigned long long)row->packed, (unsigned long long)row->p5z_size,
             (unsigned long long)row->folder_size, (unsigned long long)row->rate,
             (unsigned long long)row->p5z_seconds, (unsigned long long)row->folder_seconds);
}

// With --progress json, each level as it is measured: {"event":"level", <its fields>}.
static void on_level_json(void *ctx, const mp_level_row *row) {
    (void)ctx;
    char line[512] = "{\"event\":\"level\",";
    size_t at = strlen(line);
    level_json(line, sizeof line, &at, row);
    printf("%s}\n", line);
    fflush(stdout);
}

// The fields of the comparison, for the "result" line and for --json (without the braces).
static void levels_fields(const mp_levels_report *r, char *buf, size_t cap) {
    size_t at = 0;
    buf[0] = 0;
    add_json(buf, cap, &at,
             "\"threads\":%u,\"original_size\":%llu,\"files\":%llu,\"loose_files\":%llu,\"loose_bytes\":%llu,"
             "\"packable_files\":%llu,\"packable_bytes\":%llu,\"sample_bytes\":%llu,\"sample_rate\":%.6g,"
             "\"loose_compressed\":%llu,\"loose_seconds\":%llu,\"read_rate\":%llu,\"levels\":[",
             r->threads, (unsigned long long)r->original_size, (unsigned long long)r->files,
             (unsigned long long)r->loose_files, (unsigned long long)r->loose_bytes,
             (unsigned long long)r->packable_files, (unsigned long long)r->packable_bytes,
             (unsigned long long)r->sample_bytes, r->sample_rate, (unsigned long long)r->loose_compressed,
             (unsigned long long)(r->loose_bytes ? (r->loose_ns + 999999999ull) / 1000000000ull : 0),
             (unsigned long long)r->read_rate);
    for (size_t i = 0; i < r->nlevels; i++) {
        add_json(buf, cap, &at, i ? ",{" : "{");
        level_json(buf, cap, &at, &r->rows[i]);
        add_json(buf, cap, &at, "}");
    }
    add_json(buf, cap, &at, "]");
}

static void levels_text(FILE *out, const char *name, const mp_levels_report *r) {
    char a[32], b[32], c[32];
    size_text(a, sizeof a, r->original_size);
    fprintf(out, "Levels compared on this PC: %s, %s in %llu files\n", name, a, (unsigned long long)r->files);
    size_text(a, sizeof a, r->loose_bytes);
    size_text(b, sizeof b, r->packable_bytes);
    fprintf(out, "  %s loose (%llu files), %s to pack (%llu files)\n", a, (unsigned long long)r->loose_files, b,
            (unsigned long long)r->packable_files);
    size_text(a, sizeof a, r->sample_bytes);
    size_text(c, sizeof c, r->read_rate);
    fprintf(out, "  sample: %s of what is packed (%.1f %%)", a, 100.0 * r->sample_rate);
    if (r->read_rate) fprintf(out, ", read at %s/s", c);
    fprintf(out, "; %u threads\n", r->threads);
    if (r->loose_bytes) {
        size_text(a, sizeof a, r->loose_bytes);
        size_text(b, sizeof b, r->loose_compressed);
        fprintf(out, "  loose files in a .p5z: %s -> %s (zstd 19, long window)\n", a, b);
    }
    fprintf(out, "\n  level        .p5z    saved  time (.p5z)      folder  time (folder)\n");
    for (size_t i = 0; i < r->nlevels; i++) {
        const mp_level_row *row = &r->rows[i];
        double saved = r->original_size ? 100.0 * (1.0 - (double)row->p5z_size / (double)r->original_size) : 0;
        char t1[32], t2[32];
        size_text(a, sizeof a, row->p5z_size);
        size_text(b, sizeof b, row->folder_size);
        duration_text(t1, sizeof t1, row->p5z_seconds);
        duration_text(t2, sizeof t2, row->folder_seconds);
        fprintf(out, "  %5d  %10s  %5.1f %%  %11s  %10s  %13s\n", row->level, a, saved, t1, b, t2);
    }
    // a compressed image takes less than the game in it: saved is against the game, and each
    // .p5z is also compared with the image file
    if (r->image_size) {
        size_text(a, sizeof a, r->original_size);
        size_text(b, sizeof b, r->image_size);
        fprintf(out, "  Saved is against the game in it (%s); the image file is compressed and takes %s.\n", a, b);
        fprintf(out, "  Against the image file:");
        for (size_t i = 0; i < r->nlevels; i++) {
            double change = 100.0 * ((double)r->rows[i].p5z_size / (double)r->image_size - 1.0);
            fprintf(out, "%s level %d %s%.1f %%", i ? "," : "", r->rows[i].level, change > 0 ? "+" : "-",
                    change > 0 ? change : -change);
        }
        fprintf(out, "\n");
    }
    fprintf(out, "  The sizes are estimated from the sample; the times are for packing the whole game on this\n"
                 "  PC with %u threads, at the speed the sample was packed or read at.\n",
            r->threads);
}

static int run_levels(int argc, char **argv) {
    mp_levels_opts o;
    mp_levels_opts_default(&o);
    const char *src = NULL, *json = NULL;
    const char **keep = calloc((size_t)argc + 1, sizeof *keep);
    if (!keep) return 1;
    int rc = 2;
    for (int i = 0; i < argc; i++) {
        const char *a = argv[i];
        int has_value = i + 1 < argc;
        int common = common_opt(argc, argv, &i);
        if (common < 0) goto out;
        if (common) continue;
        if (!strcmp(a, "--levels")) {
            // an optional list: the next argument when it is digits and commas
            if (has_value && argv[i + 1][0] && strspn(argv[i + 1], "0123456789,") == strlen(argv[i + 1])) {
                mp_err e = {{0}};
                if (mp_levels_parse(argv[++i], &o, &e) != 0) {
                    bad_usage(e.msg, NULL);
                    goto out;
                }
            }
        } else if (!strcmp(a, "--full")) {
            o.full = 1;
        } else if (!strcmp(a, "--sample") || !strcmp(a, "--json") || !strcmp(a, "--threads") ||
                   !strcmp(a, "--keep-loose")) {
            if (!has_value) {
                bad_usage("missing value for", a);
                goto out;
            }
            const char *v = argv[++i];
            char *end = NULL;
            if (!strcmp(a, "--json")) {
                json = v;
            } else if (!strcmp(a, "--keep-loose")) {
                keep[o.nkeep++] = v;
            } else if (!strcmp(a, "--sample")) {
                double p = strtod(v, &end);
                if (end && *end == '%') end++;
                if (end == v || *end || !(p > 0.0 && p <= 100.0)) {
                    bad_usage("--sample must be between 0 and 100 %", v);
                    goto out;
                }
                o.sample_fraction = p / 100.0;
            } else {
                long t = strtol(v, &end, 10);
                if (end == v || *end || t < 1 || t > 1024) {
                    bad_usage("--threads must be between 1 and 1024", v);
                    goto out;
                }
                o.threads = (unsigned)t;
            }
        } else if (!strcmp(a, "--list") || !strcmp(a, "--no-dedup") || !strcmp(a, "--no-transport")) {
            bad_usage("--levels only compares the levels: it does not take", a);
            goto out;
        } else if (a[0] == '-' && a[1] == '-') {
            bad_usage("unknown option", a);
            goto out;
        } else if (!src) {
            src = a;
        } else {
            bad_usage("extra argument", a);
            goto out;
        }
    }
    if (!src) {
        bad_usage("missing the folder or image", NULL);
        goto out;
    }
    o.keep_loose = keep;
    if (mp_path_kind(src) == 0) {
        g_err_code = "bad_input";
        fprintf(stderr, "error: does not exist: %s\n", src);
        rc = 1;
        goto out;
    }
    if (json && (rc = check_json_target(src, json)) != 0) goto out;
    start_json_and_control();
    if (g_json) o.on_level = on_level_json;
    mp_levels_report r;
    mp_err e = {{0}};
    if (mp_levels_run(src, &o, &r, &e) != 0) {
        fprintf(stderr, "\nerror: %s\n", e.msg);
        rc = 1;
        goto out;
    }
    char *name = mp_strdup(src);
    if (name) {
        size_t n = strlen(name);
        while (n > 1 && (name[n - 1] == '/' || name[n - 1] == '\\')) name[--n] = 0;
    }
    const char *base = name ? name : src;
    for (const char *p = base; *p; p++)
        if (*p == '/' || *p == '\\') base = p + 1;
    levels_text(g_json ? stderr : stdout, base, &r);
    free(name);
    char *fields = malloc(16384);
    if (!fields) {
        fprintf(stderr, "error: out of memory\n");
        rc = 1;
        goto out;
    }
    levels_fields(&r, fields, 16384);
    rc = 0;
    if (json) {
        size_t n = strlen(fields);
        char *doc = malloc(n + 4);
        if (doc) snprintf(doc, n + 4, "{%s}\n", fields);
        if (!doc || mp_write_file(json, doc, strlen(doc), &e) != 0) {
            fprintf(stderr, "error: %s\n", doc ? e.msg : "out of memory");
            rc = 1;
        }
        free(doc);
    }
    if (g_json && rc == 0) {
        size_t n = strlen(fields);
        char *extra = malloc(n + 2);
        if (extra) {
            snprintf(extra, n + 2, ",%s", fields);
            json_result(1, NULL, extra);
            free(extra);
        }
    }
    free(fields);
out:
    free(keep);
    return rc;
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
                   !strcmp(a, "--runtime") || !strcmp(a, "--keep-loose") || !strcmp(a, "--max-file-size") ||
                   !strcmp(a, "--volume-limit")) {
            if (!has_value) return bad_usage("missing value for", a);
            const char *v = argv[++i];
            char *end = NULL;
            if (!strcmp(a, "--max-file-size")) {
                unsigned long long m = strtoull(v, &end, 10);
                if (end == v || *end || m == 0 || v[0] == '-') return bad_usage("--max-file-size must be a number of bytes greater than 0", v);
                max_size = m;
            } else if (!strcmp(a, "--volume-limit")) {
                // for testing: small volumes, to get several from a small game
                unsigned long long m = strtoull(v, &end, 10);
                if (end == v || *end || v[0] == '-' || m < MP_ZPK_VOL_HEADER + MP_BLOCK_V1 || m > MP_ZPK_VOLUME_LIMIT)
                    return bad_usage("--volume-limit must be between 262208 and 4294901760 bytes", v);
                o.volume_limit = m;
            } else if (!strcmp(a, "-o")) {
                dst = v;
            } else if (!strcmp(a, "--runtime")) {
                o.runtime = v;
            } else if (!strcmp(a, "--keep-loose")) {
                keep[o.nkeep++] = v;
            } else if (!strcmp(a, "--level")) {
                long l = strtol(v, &end, 10);
                if (end == v || *end || l < 1 || l > 22) return bad_usage("--level must be between 1 and 22", v);
                o.level = (int)l;
            } else {
                long t = strtol(v, &end, 10);
                if (end == v || *end || t < 1 || t > 1024) return bad_usage("--threads must be between 1 and 1024", v);
                o.threads = (unsigned)t;
            }
        } else if (a[0] == '-') {
            return bad_usage("unknown option", a);
        } else if (!src) {
            src = a;
        } else {
            return bad_usage("extra argument", a);
        }
    }
    if (!src) return bad_usage("missing the game's folder or image", NULL);
    if (!dst) return bad_usage("missing the output (-o)", NULL);
    if (max_size && !p5z) return bad_usage("--max-file-size only works with --p5z", NULL);
    o.keep_loose = keep;
    if (mp_path_kind(src) == 0) {
        g_err_code = "bad_input";
        fprintf(stderr, "error: does not exist: %s\n", src);
        return 1;
    }
    if (mp_same_file(dst, src) || (mp_path_kind(src) == 2 && mp_path_is_inside(dst, src)))
        return bad_usage("the output cannot be the game or be inside it", dst);
    if (p5z) {
        if (mp_path_kind(dst) != 0) {
            g_err_code = "exists";
            return bad_usage("the output .p5z already exists", dst);
        }
        char *dir = mp_full_path(dst);
        int parent_ok = dir && to_parent(dir) && mp_path_kind(dir) == 2;
        free(dir);
        if (!parent_ok) return bad_usage("the folder to save the .p5z in does not exist", dst);
    } else {
        if (mp_path_kind(dst) == 1) {
            g_err_code = "exists";
            return bad_usage("the output already exists and is a file", dst);
        }
        if (mp_path_kind(dst) == 2 && mp_dir_is_empty(dst) != 1) {
            g_err_code = "exists";
            return bad_usage("the output folder is not empty", dst);
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
        if (too_big) g_err_code = "too_big";
        mp_err why = {{0}};
        int left;
        if (p5z)
            left = mp_path_kind(dst) != 0;   // the half-written .p5z is deleted by the writer when it is destroyed
        else
            left = clean_output(dst, existed, &why) != 0;
        say_left(e.msg, dst, left, why.msg);
        return 1;
    }
    FILE *hum = g_json ? stderr : stdout;   // with --progress json, stdout is JSON only
    fprintf(hum, "Packed to %s (%.0f s)\n", dst, (double)(mp_now_ns() - t0) / 1e9);
    fprintf(hum, "  loose:  %llu files, ", (unsigned long long)r.loose_files);
    print_size(hum, r.loose_bytes);
    fprintf(hum, "\n  packed: %llu files, ", (unsigned long long)r.packed_files);
    print_size(hum, r.packed_bytes);
    fprintf(hum, " -> ");
    print_size(hum, r.stored_bytes);
    fprintf(hum, " in %llu volumes", (unsigned long long)r.volumes);
    if (r.packed_bytes) {
        char pct[32];
        snprintf(pct, sizeof pct, "%.1f", 100.0 * (1.0 - (double)r.stored_bytes / (double)r.packed_bytes));
        fprintf(hum, " (%s%% smaller)", pct);
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
        fprintf(stderr, "warning: the fakelib/libSceAmpr.sprx runtime does not seem to read zstd packs: use "
                        "ampr-emu-ng's libSceAmpr.sprx (--runtime)\n");
    if (!r.title_id[0])
        fprintf(stderr, "warning: sce_sys/param.json gives no TITLE_ID for the game (titleId)%s\n",
                p5z ? ": MaxPack's installer will not accept this .p5z" : "");
    return 0;
}

// Writes s as a JSON string: quotes, backslashes and control characters escaped; invalid UTF-8 becomes U+FFFD.
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
            for (int i = 1; ok && i < n; i++) ok = (p[i] & 0xc0) == 0x80;   // a NUL fails here too
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
    printf(",\"content_id\":");
    print_json_string(d.content_id);
    printf(",\"inner\":\"%s\",\"compressed\":%s", d.inner, d.compressed ? "true" : "false");
    printf(",\"original_size\":%llu,\"install_size\":%llu,\"p5z_size\":%llu,\"files\":%llu",
           (unsigned long long)d.original_size, (unsigned long long)d.install_size,
           (unsigned long long)d.p5z_size, (unsigned long long)d.files);
    if (!strcmp(d.kind, "p5z")) printf(",\"loose_bytes\":%llu", (unsigned long long)d.loose_bytes);
    printf(",\"packed_files\":%llu,\"volumes\":%llu", (unsigned long long)d.packed_files,
           (unsigned long long)d.volumes);
    printf(",\"has_runtime\":%s,\"has_zstd_runtime\":%s,\"packed\":%s}\n", d.has_runtime ? "true" : "false",
           d.has_zstd_runtime ? "true" : "false", d.packed ? "true" : "false");
    fflush(stdout);
    return 0;
}

// info --play: what the console needs to play the .p5z without installing it.
static int cmd_info_play(const char *path) {
    char err[512] = {0};
    mp_play *p = mp_play_open(path, err, sizeof err);
    if (!p) {
        fprintf(stderr, "error: %s\n", err);
        return 1;
    }
    char *j = mp_play_json(p);
    mp_play_close(p);
    if (!j) {
        fprintf(stderr, "error: out of memory\n");
        return 1;
    }
    puts(j);
    fflush(stdout);
    mp_play_free_str(j);
    return 0;
}

static int cmd_info(int argc, char **argv) {
    if (argc == 2 && !strcmp(argv[0], "--play")) return cmd_info_play(argv[1]);
    if (argc == 2 && !strcmp(argv[1], "--play")) return cmd_info_play(argv[0]);
    if (argc == 2 && !strcmp(argv[0], "--json")) return cmd_info_json(argv[1]);
    if (argc == 2 && !strcmp(argv[1], "--json")) return cmd_info_json(argv[0]);
    if (argc != 1 || !strcmp(argv[0], "--json")) return bad_usage("info needs a .p5z file (or --json or --play and a path)", NULL);
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

// icon <path> -o <png>: 0 with the PNG written, 1 if the game has no icon (or it could not be written).
static int cmd_icon(int argc, char **argv) {
    const char *src = NULL, *out = NULL;
    for (int i = 0; i < argc; i++) {
        if (!strcmp(argv[i], "-o")) {
            if (i + 1 >= argc || out) return bad_usage("-o needs a value", NULL);
            out = argv[++i];
        } else if (argv[i][0] == '-' && argv[i][1]) {
            return bad_usage("unknown option", argv[i]);
        } else if (!src) {
            src = argv[i];
        } else {
            return bad_usage("icon accepts a single source", argv[i]);
        }
    }
    if (!src || !out) return bad_usage("icon needs a source and -o <icon.png>", NULL);
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
    int mode = 0;   // 1 folder, 2 original
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
            if (i + 1 >= argc) return bad_usage("missing value for", a);
            const char *v = argv[++i];
            if (!strcmp(a, "-o")) {
                dst = v;
            } else {
                char *end = NULL;
                long t = strtol(v, &end, 10);
                if (end == v || *end || t < 1 || t > 1024) return bad_usage("--threads must be between 1 and 1024", v);
                threads = (unsigned)t;
            }
        } else if (a[0] == '-') {
            return bad_usage("unknown option", a);
        } else if (!src) {
            src = a;
        } else {
            return bad_usage("extra argument", a);
        }
    }
    if (!src) return bad_usage("missing the .p5z file", NULL);
    if (!mode) return bad_usage("give --folder (folder for the console) or --original (original dump)", NULL);
    if (!dst) return bad_usage("missing the output folder (-o)", NULL);
    if (mp_path_kind(dst) == 1) {
        g_err_code = "exists";
        return bad_usage("the output already exists and is a file", dst);
    }
    if (mp_path_kind(dst) == 2 && mp_dir_is_empty(dst) != 1) {
        g_err_code = "exists";
        return bad_usage("the output folder is not empty", dst);
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
        mp_err why = {{0}};
        int left = clean_output(dst, existed, &why) != 0;
        say_left(e.msg, dst, left, why.msg);
        return 1;
    }
    FILE *hum = g_json ? stderr : stdout;
    fprintf(hum, "%s to %s (%.0f s)%s\n", mode == 1 ? "Console folder" : "Original dump rebuilt", dst,
            (double)(mp_now_ns() - t0) / 1e9, mode == 2 ? "; all SHA-256 hashes match" : "");
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
            if (i + 1 >= argc) return bad_usage("missing value for", a);
            const char *v = argv[++i];
            if (!strcmp(a, "--against")) {
                against = v;
            } else {
                char *end = NULL;
                long t = strtol(v, &end, 10);
                if (end == v || *end || t < 1 || t > 1024) return bad_usage("--threads must be between 1 and 1024", v);
                threads = (unsigned)t;
            }
        } else if (a[0] == '-') {
            return bad_usage("unknown option", a);
        } else if (!target) {
            target = a;
        } else {
            return bad_usage("extra argument", a);
        }
    }
    if (!target) return bad_usage("missing the .p5z or the folder to verify", NULL);
    int kind = mp_path_kind(target);
    if (kind == 0) {
        g_err_code = "bad_input";
        fprintf(stderr, "error: does not exist: %s\n", target);
        return 1;
    }
    if (kind == 1 && against) return bad_usage("--against only works for folders (the .p5z already carries its SHA-256 hashes)", NULL);
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
    fprintf(hum, "verify: %llu OK, %llu failed (%.0f s)\n", (unsigned long long)r.files_ok,
            (unsigned long long)r.files_bad, (double)(mp_now_ns() - t0) / 1e9);
    if (g_json) {
        char extra[96];
        snprintf(extra, sizeof extra, ",\"files_ok\":%llu,\"files_bad\":%llu", (unsigned long long)r.files_ok,
                 (unsigned long long)r.files_bad);
        json_result(r.files_bad == 0, NULL, extra);
    }
    return r.files_bad ? 1 : 0;
}

// ---- measure -------------------------------------------------------------------------------------

// Appends s as a JSON string (quotes, backslashes and control characters escaped).
static void add_json_string(char *buf, size_t cap, size_t *at, const char *s) {
    add_json(buf, cap, at, "\"");
    for (const unsigned char *p = (const unsigned char *)s; *p; p++) {
        if (*p == '"' || *p == '\\') add_json(buf, cap, at, "\\%c", *p);
        else if (*p < 0x20 || *p == 0x7f) add_json(buf, cap, at, "\\u%04x", *p);
        else add_json(buf, cap, at, "%c", *p);
    }
    add_json(buf, cap, at, "\"");
}

// With --progress json, each count of threads once tried: {"event":"trial", ...}.
static void on_trial_json(void *ctx, const mp_bench_trial *t) {
    (void)ctx;
    printf("{\"event\":\"trial\",\"threads\":%u,\"bytes\":%llu,\"seconds\":%.3f,\"rate\":%llu}\n", t->threads,
           (unsigned long long)t->bytes, (double)t->ns / 1e9, (unsigned long long)t->rate);
    fflush(stdout);
}

static void bench_fields(const mp_bench_report *r, char *buf, size_t cap) {
    size_t at = 0;
    buf[0] = 0;
    add_json(buf, cap, &at, "\"cpu_threads\":%u,\"level\":%d,\"trials\":[", r->cpu_threads, r->level);
    for (size_t i = 0; i < r->ntrials; i++)
        add_json(buf, cap, &at, "%s{\"threads\":%u,\"bytes\":%llu,\"seconds\":%.3f,\"rate\":%llu}", i ? "," : "",
                 r->trials[i].threads, (unsigned long long)r->trials[i].bytes, (double)r->trials[i].ns / 1e9,
                 (unsigned long long)r->trials[i].rate);
    add_json(buf, cap, &at, "],\"recommended_threads\":%u,\"fastest_threads\":%u,\"disk\":", r->best, r->fastest);
    if (r->disk == 1)
        add_json(buf, cap, &at, "{\"written\":%llu,\"write_rate\":%llu,\"read\":%llu,\"read_rate\":%llu}",
                 (unsigned long long)r->written, (unsigned long long)r->write_rate, (unsigned long long)r->read,
                 (unsigned long long)r->read_rate);
    else if (r->disk == -1) {
        add_json(buf, cap, &at, "{\"error\":");
        add_json_string(buf, cap, &at, r->disk_error);
        add_json(buf, cap, &at, "}");
    } else
        add_json(buf, cap, &at, "null");
}

static void bench_text(FILE *out, const char *dir, const mp_bench_report *r) {
    char a[32], b[32];
    fprintf(out, "Measured on this PC: %u threads, zstd level %d, generated data packed as a game is\n",
            r->cpu_threads, r->level);
    fprintf(out, "  threads        speed\n");
    for (size_t i = 0; i < r->ntrials; i++) {
        size_text(a, sizeof a, r->trials[i].rate);
        fprintf(out, "  %7u  %9s/s%s\n", r->trials[i].threads, a, r->trials[i].threads == r->best ? "  <-" : "");
    }
    if (r->disk == 1) {
        size_text(a, sizeof a, r->write_rate);
        size_text(b, sizeof b, r->read_rate);
        fprintf(out, "  disk of %s: writes at %s/s, reads at %s/s", dir, a, b);
        size_text(a, sizeof a, r->written);
        fprintf(out, " (%s written, then deleted)\n", a);
    } else if (r->disk == -1) {
        fprintf(out, "  disk of %s: not measured: %s\n", dir, r->disk_error);
    }
    if (r->best == r->cpu_threads)
        fprintf(out, "Recommended: all the CPU's threads (--threads %u), the default\n", r->best);
    else if (r->best == r->fastest)
        fprintf(out, "Recommended: --threads %u, the fastest\n", r->best);
    else
        fprintf(out, "Recommended: --threads %u, less than 5 %% slower than the fastest (%u threads) with fewer\n",
                r->best, r->fastest);
}

static int run_measure(int argc, char **argv) {
    mp_bench_opts o;
    mp_bench_opts_default(&o);
    const char *dir = NULL, *json = NULL;
    for (int i = 0; i < argc; i++) {
        const char *a = argv[i];
        int common = common_opt(argc, argv, &i);
        if (common < 0) return 2;
        if (common) {
            continue;
        } else if (!strcmp(a, "--level") || !strcmp(a, "--seconds") || !strcmp(a, "--json")) {
            if (i + 1 >= argc) return bad_usage("missing value for", a);
            const char *v = argv[++i];
            char *end = NULL;
            if (!strcmp(a, "--json")) {
                json = v;
            } else if (!strcmp(a, "--level")) {
                long l = strtol(v, &end, 10);
                if (end == v || *end || l < 1 || l > 22) return bad_usage("--level must be between 1 and 22", v);
                o.level = (int)l;
            } else {
                double sec = strtod(v, &end);
                if (end == v || *end || !(sec >= 0.1 && sec <= 60)) return bad_usage("--seconds must be between 0.1 and 60", v);
                o.trial_seconds = o.disk_seconds = sec;
            }
        } else if (a[0] == '-' && a[1]) {
            return bad_usage("unknown option", a);
        } else if (!dir) {
            dir = a;
        } else {
            return bad_usage("extra argument", a);
        }
    }
    if (dir && mp_path_kind(dir) != 2)
        return bad_usage(mp_path_kind(dir) ? "the folder to measure is a file" : "the folder to measure does not exist", dir);
    if (json) {
        char *parent = mp_full_path(json);
        int ok = parent && mp_path_kind(json) != 2 && to_parent(parent) && mp_path_kind(parent) == 2;
        free(parent);
        if (!ok) return bad_usage("--json must be a file in a folder that exists", json);
    }
    o.dir = dir;
    start_json_and_control();
    if (g_json) o.on_trial = on_trial_json;
    mp_bench_report r;
    mp_err e = {{0}};
    if (mp_bench_run(&o, &r, &e) != 0) {
        fprintf(stderr, "\nerror: %s\n", e.msg);
        return 1;
    }
    bench_text(g_json ? stderr : stdout, dir ? dir : "", &r);
    char fields[4096];
    bench_fields(&r, fields, sizeof fields);
    int rc = 0;
    if (json) {
        char doc[4100];
        snprintf(doc, sizeof doc, "{%s}\n", fields);
        if (mp_write_file(json, doc, strlen(doc), &e) != 0) {
            fprintf(stderr, "error: %s\n", e.msg);
            rc = 1;
        }
    }
    if (g_json && rc == 0) {
        char extra[4100];
        snprintf(extra, sizeof extra, ",%s", fields);
        json_result(1, NULL, extra);
    }
    return rc;
}

static int cmd_measure(int argc, char **argv) {
    prescan_json(argc, argv);
    return finish_cmd(run_measure(argc, argv));
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
    if (argc >= 2 && strcmp(argv[1], "--capabilities") == 0) {
        // what the window needs from this version of the engine
        printf("{\"progress_json\":true,\"control_stdin\":true,\"max_file_size\":true,\"info_json\":true,"
               "\"icon\":true,\"packs\":true,\"content_id\":true,\"levels\":true,\"measure\":true,"
               "\"images\":true}\n");
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
    if (argc >= 2 && strcmp(argv[1], "measure") == 0) return cmd_measure(argc - 2, argv + 2);
    if (argc >= 2) return bad_usage("unknown command", argv[1]);
    usage(stderr);
    return 2;
}
