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
#include "mp_complete.h"
#include "mp_describe.h"
#include "mp_grow.h"
#include "mp_hash.h"
#include "mp_levels.h"
#include "mp_p5z.h"
#include "mp_parallel.h"
#include "mp_pack.h"
#include "mp_platform.h"
#include "mp_play.h"
#include "mp_report_out.h"
#include "mp_rules.h"
#include "mp_send.h"
#include "mp_source.h"
#include "mp_verify_quick.h"
#include "mp_version.h"

static void usage(FILE *out) {
    fprintf(out,
            "Usage:\n"
            "  maxpack analyze <folder|image|package> [options]\n"
            "  maxpack analyze <folder|image|package> --levels [L,L,...] [options]   compare the levels\n"
            "  maxpack pack <folder|image|package> -o <output> [--dlc <DLC> ...] [options]\n"
            "  maxpack unpack <game.p5z> --folder|--original -o <folder> [--threads N]\n"
            "  maxpack add <game.p5z> --dlc <DLC> [--dlc ...] [options]   DLC into a .p5z already made\n"
            "  maxpack update <game.p5z> <new dump> [options]   only what a new version of the game changes\n"
            "  maxpack compact <in.p5z> -o <out.p5z> [--threads N]   a copy without the room they leave\n"
            "  maxpack info <game.p5z>\n"
            "  maxpack info --json <folder|image|package|game.p5z>\n"
            "  maxpack info --play <game.p5z>   what the console needs to play it without installing (JSON)\n"
            "  maxpack info --sums <game.p5z>   what its index records of its packs: level, archive_id and each\n"
            "                                   volume's size and SHA-256, in the lines the console's pack prints\n"
            "  maxpack icon <folder|image|package|game.p5z> -o <icon.png>\n"
            "  maxpack verify <game.p5z | folder> [--against <source>] [--threads N]\n"
            "  maxpack verify --quick <game.p5z> [--json]   only that its bytes are those it was written\n"
            "                     with (each volume's and stored file's SHA-256), at the speed of the disk\n"
            "  maxpack measure [<folder>] [options]   measure this PC (and the folder's disk), with no game\n"
            "  maxpack send <game.p5z> <ps5-ip> --code <6 digits> [--drive <id>] [--json]   to MaxPack for\n"
            "                     the console, which verifies it when it arrives (verify --quick)\n"
            "  maxpack send --drives <ps5-ip> --code <6 digits> [--json]   the console's drives, free space\n"
            "  maxpack send --discard <game.p5z> <ps5-ip> --code <6 digits> [--drive <id>]   delete what the\n"
            "                     console received of it\n"
            "  maxpack --version\n"
            "  maxpack --capabilities   what this version can do, as JSON (for a front end)\n"
            "\n"
            "An image is an .exfat, an .ffpkg (UFS2), an .ffpfs (PFS) or an .ffpfsc (a PFS image that holds\n"
            "one of them, usually compressed); a package is a PS5 debug package (FPKG, .pkg), which MaxPack\n"
            "decrypts with its passcode as it reads it. MaxPack tells which by its contents, not by its name.\n"
            "\n"
            "Options for pack, analyze, info, icon and verify --against:\n"
            "  --passcode P       the 32-character passcode of a package, if it is not the default\n"
            "                     (32 zeros, what fake packages use)\n"
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
            "  --leave-out-leftovers  leave out what dump tools leave behind (decrypted/, *.auth_info)\n"
            "                     when the game's sce_sys/playgo-hash-table.dat proves it is not the\n"
            "                     game's; without one, everything stays\n"
            "  --dlc DLC          a DLC of the game to put inside what is made (can be repeated): a\n"
            "                     DLC package (.pkg, with the same --passcode) or a dumped DLC folder\n"
            "                     (sce_sys/param.json and no eboot.bin), of the game's title ID. One\n"
            "                     with files of its own goes to addcont0/, addcont1/... in the order\n"
            "                     given (at most 100), a license only adds nothing but its entry, and\n"
            "                     MaxPack writes the dlc_emu.ini that lists them; a game that has its\n"
            "                     own dlc_emu.ini is not given more\n"
            "\n"
            "add and update grow the .p5z by appending to it (nothing before its end is written again); if\n"
            "they stop, or the power goes, the next command that opens it leaves it as it was. A .p5z that\n"
            "grew needs an ampr-emu-ng that takes it (p5z mode, v0.5.1-maxpack.2 or later); compact makes a\n"
            "copy as a fresh pack writes it, which any one takes. Their options:\n"
            "  --dlc DLC          add: a DLC to put in, as pack --dlc takes it (can be repeated); the ones\n"
            "                     with files of their own go to the addcontN/ after the game's last\n"
            "  --force            update: go on when more than half of the game changed (packing the new\n"
            "                     dump again would give a smaller .p5z)\n"
            "  --leave-out-leftovers, --keep-loose GLOB   update: as pack's, for the new dump\n"
            "  --threads N, --max-file-size N, --volume-limit N   as pack's (--max-file-size: the whole .p5z)\n"
            "\n"
            "analyze, info --json, pack and verify tell whether the dump is complete against the list\n"
            "of the original package's files it carries (sce_sys/playgo-hash-table.dat) or, without it,\n"
            "whether the copy has every file the dump tool wrote (its ampr_emu.index).\n"
            "\n"
            "Options for pack, unpack, verify, add, update, compact, analyze --levels and measure (for a front end):\n"
            "  --progress json    one JSON line per event on stdout (the text stays on stderr)\n"
            "  --control stdin    read from stdin: the line \"cancel\" or closing the input cancels\n"
            "  Exit codes: 0 ok, 1 error, 2 usage, 130 cancelled\n"
            "\n"
            "send: MaxPack for the console shows the code in Settings > \"Receive games from the PC\", which\n"
            "must be on. The .p5z goes to <drive>/homebrew/ as <name>.maxpack-recv and gets its name once the\n"
            "console has verified it. If it stops (a broken connection, Ctrl+C, the PC or the console off),\n"
            "the same command goes on from what the console has; a receive the console paused (idle, a write\n"
            "error) is started again, one stopped on the console is not. Its options:\n"
            "  <ps5-ip>           the console's IP address: a.b.c.d (port 8095), or a.b.c.d:port\n"
            "  --code C           the pairing code (or the environment variable MAXPACK_SEND_CODE)\n"
            "  --drive ID         internal (default), usb0, ...: send --drives lists them\n"
            "  --json             one JSON line per event on stdout, as --progress json\n"
            "  --chunk N          bytes per request, 65536 to 67108864 (default 33554432)\n"
            "  --retries N        tries after a broken connection before it gives up (default 10; 1 while the\n"
            "                     console has never answered); any answer begins the count again\n"
            "  --retry-wait MS    the wait after the first one, 1 to 30000 (default 1000; it doubles, up to 30 s)\n"
            "  --stall S          nothing moving on the console for S seconds stops it (default 300)\n"
            "  --control stdin    \"cancel\" stops it, and the console keeps what arrived (so does the end of\n"
            "                     the input)\n"
            "  --delete-on-cancel with --control stdin: \"cancel\" makes the console delete what it received\n"
            "  Exit codes: 0 sent and verified, 1 error, 2 usage, 3 no answer from the console, 4 receiving\n"
            "  is off there, 5 wrong code, 6 too many wrong codes, 7 already there, 8 the console is busy,\n"
            "  9 no space, 10 FAT32 drive, 11 file name, 12 no such drive, 13 arrived damaged, 14 cancelled\n"
            "  on the console, 15 stopped on the console, 16 nothing moved (stalled), 17 the console cannot write\n"
            "  it, 130 cancelled (or stopped) here\n"
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
static volatile int g_cancel_line;  // the cancel came as the line "cancel" (not as the end of the input)

static const char *phase_code(const char *phase) {
    if (!strncmp(phase, "Copying", 7)) return "copying";
    if (!strncmp(phase, "Comparing", 9)) return "comparing";
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
                g_cancel_line = 1;
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

// --passcode P: takes it out of argv (from argv[2]) and gives it to the sources. 0, or the exit code
// of a usage error.
static int take_passcode(int *argc, char **argv) {
    for (int i = 2; i < *argc; i++) {
        if (strcmp(argv[i], "--passcode")) continue;
        if (i + 1 >= *argc) {
            prescan_json(*argc, argv);
            return finish_cmd(bad_usage("missing value for", "--passcode"));
        }
        if (mp_source_set_passcode(argv[i + 1]) != 0) {
            prescan_json(*argc, argv);
            return finish_cmd(
                bad_usage("--passcode must be 32 characters (letters, digits or signs, no spaces)", argv[i + 1]));
        }
        for (int k = i; k + 2 <= *argc; k++) argv[k] = argv[k + 2];
        *argc -= 2;
        i--;
    }
    return 0;
}

// A package that needs another passcode: its own error code for a front end.
static void passcode_code(const char *msg) {
    if (strstr(msg, "--passcode")) g_err_code = "passcode";
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
        if (r->kind == MP_SRC_FPKG)
            fprintf(out, "  Saved is against the game in it (%s); the package file takes %s.\n", a, b);
        else
            fprintf(out, "  Saved is against the game in it (%s); the image file is compressed and takes %s.\n", a, b);
        fprintf(out, "  Against the %s file:", r->kind == MP_SRC_FPKG ? "package" : "image");
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
        passcode_code(e.msg);
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

// What pack says of a dump that is not complete, or of a copy of it that is not whole, on stderr:
// never a reason not to pack.
static void complete_warnings(const mp_complete *c, int leave_out) {
    char line[256], size[32];
    if (c->checkable && (c->missing || c->size_mismatch)) {
        mp_complete_summary(c, line, sizeof line);
        int dump_tool = c->source && !strcmp(c->source, "dump_index");
        fprintf(stderr, "warning: %s%s: what was made of it lacks them too\n", dump_tool ? "" : "the dump is ", line);
    }
    if (!mp_complete_proves_leftovers(c) && c->leftover_count && leave_out) {
        size_text(size, sizeof size, c->leftover_bytes);
        int one = c->leftover_count == 1;
        fprintf(stderr, "note: kept: %llu %s (%s) that look%s like what dump tools leave behind: only the game's "
                        "sce_sys/playgo-hash-table.dat proves %s not the game's (--leave-out-leftovers)\n",
                (unsigned long long)c->leftover_count, one ? "file" : "files", size, one ? "s" : "", one ? "it is" : "they are");
    }
}

// The completeness of what was verified, as a line for people (stdout, or stderr with --progress
// json) and the object for the "result" line (malloc; NULL when it could not be told).
static char *verify_completeness(const char *target, FILE *hum) {
    mp_complete c;
    mp_err e = {{0}};
    if (mp_describe_complete(target, &c, &e) != 0) return NULL;
    char line[256];
    mp_complete_summary(&c, line, sizeof line);
    fprintf(hum, "%s\n", line);
    mp_complete_extra_line(&c, line, sizeof line);
    if (line[0]) fprintf(hum, "  %s\n", line);
    mp_jbuf b = {0};
    mp_complete_json(&c, 0, &b);
    mp_complete_free(&c);
    if (b.oom) {
        mp_jbuf_free(&b);
        return NULL;
    }
    return b.p;
}

static int run_pack(int argc, char **argv) {
    mp_pack_opts o;
    mp_pack_opts_default(&o);
    const char *src = NULL, *dst = NULL;
    int p5z = 0;
    uint64_t max_size = 0;
    const char **keep = calloc((size_t)argc + 1, sizeof *keep), **dlc = calloc((size_t)argc + 1, sizeof *dlc);
    if (!keep || !dlc) {
        free(keep);
        free(dlc);
        return 1;
    }
    for (int i = 0; i < argc; i++) {
        const char *a = argv[i];
        int has_value = i + 1 < argc;
        int common = common_opt(argc, argv, &i);
        if (common < 0) {
            free(keep);
            free(dlc);
            return 2;
        }
        if (common) {
            continue;
        } else if (!strcmp(a, "--folder")) {
            p5z = 0;
        } else if (!strcmp(a, "--p5z")) {
            p5z = 1;
        } else if (!strcmp(a, "--leave-out-leftovers")) {
            o.leave_out_leftovers = 1;
        } else if (!strcmp(a, "-o") || !strcmp(a, "--level") || !strcmp(a, "--threads") ||
                   !strcmp(a, "--runtime") || !strcmp(a, "--keep-loose") || !strcmp(a, "--max-file-size") ||
                   !strcmp(a, "--volume-limit") || !strcmp(a, "--dlc")) {
            if (!has_value) return bad_usage("missing value for", a);
            const char *v = argv[++i];
            char *end = NULL;
            if (!strcmp(a, "--dlc")) {
                dlc[o.ndlc++] = v;
            } else if (!strcmp(a, "--max-file-size")) {
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
    o.dlc = dlc;
    if (mp_path_kind(src) == 0) {
        g_err_code = "bad_input";
        fprintf(stderr, "error: does not exist: %s\n", src);
        return 1;
    }
    if (mp_same_file(dst, src) || (mp_path_kind(src) == 2 && mp_path_is_inside(dst, src)))
        return bad_usage("the output cannot be the game or be inside it", dst);
    for (size_t k = 0; k < o.ndlc; k++)
        if (mp_same_file(dst, dlc[k]) || (mp_path_kind(dlc[k]) == 2 && mp_path_is_inside(dst, dlc[k])))
            return bad_usage("the output cannot be a DLC or be inside one", dst);
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
        free(dlc);
        return 1;
    }
    mp_pack_result r;
    uint64_t t0 = mp_now_ns();
    int rc = mp_pack_run(src, &out, &o, &r, &e);
    int too_big = rc != 0 && p5z && mp_pack_out_p5z_too_big(&out);
    out.destroy(out.ctx);
    free(keep);
    free(dlc);
    if (rc != 0) {
        if (too_big) g_err_code = "too_big";
        if (r.dlc_error) g_err_code = r.dlc_error;   // a DLC that needs another passcode says so below
        passcode_code(e.msg);
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
    if (r.left_out_files) {
        fprintf(hum, "  left out: %llu files dump tools left behind, ", (unsigned long long)r.left_out_files);
        print_size(hum, r.left_out_bytes);
        fprintf(hum, "\n");
    }
    if (r.dlc_data || r.dlc_license) {
        fprintf(hum, "  DLC:    ");
        if (r.dlc_data == 1)
            fprintf(hum, "1 with data (addcont0/)");
        else if (r.dlc_data)
            fprintf(hum, "%u with data (addcont0/ to addcont%u/)", r.dlc_data, r.dlc_data - 1);
        if (r.dlc_license)
            fprintf(hum, "%s%u %s only", r.dlc_data ? ", " : "", r.dlc_license, r.dlc_license == 1 ? "license" : "licenses");
        fprintf(hum, "\n");
    }
    if (g_json) {
        char extra[640];
        snprintf(extra, sizeof extra,
                 ",\"original_size\":%llu,\"output_size\":%llu,\"loose_bytes\":%llu,\"packed_bytes\":%llu,"
                 "\"stored_bytes\":%llu,\"volumes\":%llu,\"runtime_without_zstd\":%s,\"left_out_files\":%llu,"
                 "\"left_out_bytes\":%llu,\"dlc_data\":%u,\"dlc_license\":%u,\"dlc_bytes\":%llu,\"completeness\":",
                 (unsigned long long)(r.loose_bytes + r.packed_bytes),
                 (unsigned long long)(p5z ? psize : folder_bytes(dst)), (unsigned long long)r.loose_bytes,
                 (unsigned long long)r.packed_bytes, (unsigned long long)r.stored_bytes,
                 (unsigned long long)r.volumes, r.runtime_without_zstd ? "true" : "false",
                 (unsigned long long)r.left_out_files, (unsigned long long)r.left_out_bytes, r.dlc_data, r.dlc_license,
                 (unsigned long long)r.dlc_bytes);
        mp_jbuf b = {0};
        mp_jbuf_raw(&b, extra);
        mp_complete_json(&r.complete, 0, &b);
        json_result(1, NULL, b.p && !b.oom ? b.p : extra);
        mp_jbuf_free(&b);
    }
    complete_warnings(&r.complete, o.leave_out_leftovers);
    if (r.runtime_without_zstd)
        fprintf(stderr, "warning: the fakelib/libSceAmpr.sprx runtime does not seem to read zstd packs: use "
                        "ampr-emu-ng's libSceAmpr.sprx (--runtime)\n");
    if (!r.title_id[0])
        fprintf(stderr, "warning: sce_sys/param.json gives no TITLE_ID for the game (titleId)%s\n",
                p5z ? ": MaxPack's installer will not accept this .p5z" : "");
    return 0;
}

// ---- growing a .p5z: add, update, compact ------------------------------------------------------------

// Before a command reads a .p5z: an append that stopped (a power cut, a crash) is undone, and said. Only
// a .p5z grows, so only a file named .p5z can have a journal of MaxPack next to it.
static int recover_note(const char *path) {
    int what = 0;
    mp_err e = {{0}};
    size_t n = strlen(path);
    if (n < 4 || path[n - 4] != '.' || (path[n - 3] | 32) != 'p' || path[n - 2] != '5' || (path[n - 1] | 32) != 'z')
        return 0;
    if (mp_p5z_recover(path, &what, &e) != 0) {
        fprintf(stderr, "warning: %s\n", e.msg);
        return 0;
    }
    if (what == MP_RECOVER_RESTORED)
        fprintf(stderr, "note: an interrupted change of %s was undone: it is as it was before it\n", path);
    if (what == MP_RECOVER_COMMITTED)
        fprintf(stderr, "note: the last change of %s had ended; the file left of it was deleted\n", path);
    return what == MP_RECOVER_RESTORED;
}

// Options common to add and update. 0 = not ours, 1 = consumed, -1 = bad usage (already said).
static int grow_opt(int argc, char **argv, int *i, mp_grow_opts *o, uint64_t *max_size) {
    const char *a = argv[*i];
    if (strcmp(a, "--threads") && strcmp(a, "--max-file-size") && strcmp(a, "--volume-limit")) return 0;
    if (*i + 1 >= argc) {
        bad_usage("missing value for", a);
        return -1;
    }
    const char *v = argv[++*i];
    char *end = NULL;
    if (!strcmp(a, "--threads")) {
        long t = strtol(v, &end, 10);
        if (end == v || *end || t < 1 || t > 1024) {
            bad_usage("--threads must be between 1 and 1024", v);
            return -1;
        }
        o->threads = (unsigned)t;
    } else if (!strcmp(a, "--max-file-size")) {
        unsigned long long m = strtoull(v, &end, 10);
        if (end == v || *end || m == 0 || v[0] == '-') {
            bad_usage("--max-file-size must be a number of bytes greater than 0", v);
            return -1;
        }
        *max_size = m;
    } else {
        unsigned long long m = strtoull(v, &end, 10);
        if (end == v || *end || v[0] == '-' || m < MP_ZPK_VOL_HEADER + MP_BLOCK_V1 || m > MP_ZPK_VOLUME_LIMIT) {
            bad_usage("--volume-limit must be between 262208 and 4294901760 bytes", v);
            return -1;
        }
        o->volume_limit = m;
    }
    return 1;
}

// What add and update did, for people and for a front end.
static void grow_report(const char *p5z, const mp_grow_result *r, int add, uint64_t t0) {
    FILE *hum = g_json ? stderr : stdout;
    char a[32], b[32], c[32];
    if (r->nothing) {
        fprintf(hum, "Nothing to do: %s has that dump already\n", p5z);
    } else {
        if (add)
            fprintf(hum, "Added to %s (%.0f s)\n", p5z, (double)(mp_now_ns() - t0) / 1e9);
        else
            fprintf(hum, "Updated %s from version %s to %s (%.0f s)\n", p5z, r->old_version[0] ? r->old_version : "?",
                    r->new_version[0] ? r->new_version : "?", (double)(mp_now_ns() - t0) / 1e9);
        if (add) {
            fprintf(hum, "  DLC:      ");
            if (r->dlc_data) fprintf(hum, "%u with data", r->dlc_data);
            if (r->dlc_license)
                fprintf(hum, "%s%u %s only", r->dlc_data ? ", " : "", r->dlc_license, r->dlc_license == 1 ? "license" : "licenses");
            fprintf(hum, "\n");
        }
        fprintf(hum, "  files:    %llu the same, %llu new, %llu changed, %llu removed\n", (unsigned long long)r->same_files,
                (unsigned long long)r->new_files, (unsigned long long)r->changed_files,
                (unsigned long long)r->removed_files);
        size_text(a, sizeof a, r->stored_bytes);
        size_text(b, sizeof b, r->added_bytes);
        size_text(c, sizeof c, r->p5z_size);
        fprintf(hum, "  stored:   %s of files; the .p5z grew by %s and is %s, in %llu volumes\n", a, b, c,
                (unsigned long long)r->volumes);
        if (r->unused_bytes) {
            size_text(a, sizeof a, r->unused_bytes);
            fprintf(hum, "  unused:   %s (maxpack compact %s -o <new.p5z> makes a copy without it)\n", a, p5z);
        }
    }
    if (r->par2 && !r->nothing)
        fprintf(stderr, "note: the repair data (PAR2) next to %s no longer match it: make them again\n", p5z);
    if (r->journal_left)
        fprintf(stderr, "warning: %s%s could not be deleted (another program has it open?); %s is whole as it "
                        "grew, and the next time MaxPack opens it that file goes\n",
                p5z, MP_P5Z_JOURNAL, p5z);
    if (g_json) {
        char extra[800];
        snprintf(extra, sizeof extra,
                 ",\"same_files\":%llu,\"new_files\":%llu,\"changed_files\":%llu,\"removed_files\":%llu,"
                 "\"stored_bytes\":%llu,\"added_bytes\":%llu,\"p5z_size\":%llu,\"volumes\":%llu,\"unused_bytes\":%llu,"
                 "\"grown\":%u,\"nothing\":%s,\"par2\":%s,\"journal_left\":%s,\"dlc_data\":%u,\"dlc_license\":%u,"
                 "\"dlc_bytes\":%llu",
                 (unsigned long long)r->same_files, (unsigned long long)r->new_files,
                 (unsigned long long)r->changed_files, (unsigned long long)r->removed_files,
                 (unsigned long long)r->stored_bytes, (unsigned long long)r->added_bytes,
                 (unsigned long long)r->p5z_size, (unsigned long long)r->volumes, (unsigned long long)r->unused_bytes,
                 r->grown, r->nothing ? "true" : "false", r->par2 ? "true" : "false",
                 r->journal_left ? "true" : "false", r->dlc_data, r->dlc_license, (unsigned long long)r->dlc_bytes);
        json_result(1, NULL, extra);
    }
}

static int grow_target_ok(const char *p5z) {
    if (mp_path_kind(p5z) != 1) {
        g_err_code = "bad_input";
        fprintf(stderr, "error: %s %s\n", mp_path_kind(p5z) == 0 ? "does not exist:" : "is not a .p5z file:", p5z);
        return 0;
    }
    return 1;
}

static int run_grow(int argc, char **argv, int add) {
    mp_grow_opts o;
    mp_grow_opts_default(&o);
    uint64_t max_size = 0;
    const char *p5z = NULL, *dump = NULL;
    const char **dlc = calloc((size_t)argc + 1, sizeof *dlc), **keep = calloc((size_t)argc + 1, sizeof *keep);
    size_t ndlc = 0;
    int rc = 2;
    if (!dlc || !keep) goto out;
    for (int i = 0; i < argc; i++) {
        const char *a = argv[i];
        int common = common_opt(argc, argv, &i);
        if (common < 0) goto out;
        if (common) continue;
        int g = grow_opt(argc, argv, &i, &o, &max_size);
        if (g < 0) goto out;
        if (g) continue;
        if (add && !strcmp(a, "--dlc")) {
            if (i + 1 >= argc) {
                rc = bad_usage("missing value for", a);
                goto out;
            }
            dlc[ndlc++] = argv[++i];
        } else if (!add && !strcmp(a, "--force")) {
            o.force = 1;
        } else if (!add && !strcmp(a, "--leave-out-leftovers")) {
            o.leave_out_leftovers = 1;
        } else if (!add && !strcmp(a, "--keep-loose")) {
            if (i + 1 >= argc) {
                rc = bad_usage("missing value for", a);
                goto out;
            }
            keep[o.nkeep++] = argv[++i];
        } else if (a[0] == '-') {
            rc = bad_usage("unknown option", a);
            goto out;
        } else if (!p5z) {
            p5z = a;
        } else if (!add && !dump) {
            dump = a;
        } else {
            rc = bad_usage("extra argument", a);
            goto out;
        }
    }
    o.keep_loose = keep;
    o.max_size = max_size;
    if (!p5z) {
        rc = bad_usage(add ? "add needs the .p5z and --dlc <DLC>" : "update needs the .p5z and the new dump", NULL);
        goto out;
    }
    if (add && !ndlc) {
        rc = bad_usage("add needs the DLC to add: --dlc <DLC> (can be repeated)", NULL);
        goto out;
    }
    if (!add && !dump) {
        rc = bad_usage("update needs the new dump after the .p5z", NULL);
        goto out;
    }
    rc = 1;
    if (!grow_target_ok(p5z)) goto out;
    if (!add && mp_path_kind(dump) == 0) {
        g_err_code = "bad_input";
        fprintf(stderr, "error: does not exist: %s\n", dump);
        goto out;
    }
    start_json_and_control();
    recover_note(p5z);
    mp_err e = {{0}};
    mp_grow_result r;
    uint64_t t0 = mp_now_ns();
    int grc = add ? mp_grow_add(p5z, dlc, ndlc, &o, &r, &e) : mp_grow_update(p5z, dump, &o, &r, &e);
    if (grc != 0) {
        if (r.code) g_err_code = r.code;
        passcode_code(e.msg);
        fprintf(stderr, "error: %s\n", e.msg);
        goto out;
    }
    grow_report(p5z, &r, add, t0);
    rc = 0;
out:
    free(dlc);
    free(keep);
    return rc;
}

static int cmd_add(int argc, char **argv) {
    prescan_json(argc, argv);
    return finish_cmd(run_grow(argc, argv, 1));
}

static int cmd_update(int argc, char **argv) {
    prescan_json(argc, argv);
    return finish_cmd(run_grow(argc, argv, 0));
}

static int run_compact(int argc, char **argv) {
    const char *in = NULL, *out = NULL;
    unsigned threads = mp_cpu_count();
    uint64_t limit = MP_ZPK_VOLUME_LIMIT;
    for (int i = 0; i < argc; i++) {
        const char *a = argv[i];
        int common = common_opt(argc, argv, &i);
        if (common < 0) return 2;
        if (common) continue;
        if (!strcmp(a, "-o") || !strcmp(a, "--threads") || !strcmp(a, "--volume-limit")) {
            if (i + 1 >= argc) return bad_usage("missing value for", a);
            const char *v = argv[++i];
            char *end = NULL;
            if (!strcmp(a, "-o")) {
                out = v;
            } else if (!strcmp(a, "--threads")) {
                long t = strtol(v, &end, 10);
                if (end == v || *end || t < 1 || t > 1024) return bad_usage("--threads must be between 1 and 1024", v);
                threads = (unsigned)t;
            } else {
                unsigned long long m = strtoull(v, &end, 10);
                if (end == v || *end || v[0] == '-' || m < MP_ZPK_VOL_HEADER + MP_BLOCK_V1 || m > MP_ZPK_VOLUME_LIMIT)
                    return bad_usage("--volume-limit must be between 262208 and 4294901760 bytes", v);
                limit = m;
            }
        } else if (a[0] == '-') {
            return bad_usage("unknown option", a);
        } else if (!in) {
            in = a;
        } else {
            return bad_usage("extra argument", a);
        }
    }
    if (!in) return bad_usage("missing the .p5z to compact", NULL);
    if (!out) return bad_usage("missing the new .p5z (-o)", NULL);
    if (mp_path_kind(out) != 0) {
        g_err_code = "exists";
        return bad_usage("the output .p5z already exists", out);
    }
    char *dir = mp_full_path(out);
    int parent_ok = dir && to_parent(dir) && mp_path_kind(dir) == 2;
    free(dir);
    if (!parent_ok) return bad_usage("the folder to save the .p5z in does not exist", out);
    if (!grow_target_ok(in)) return 1;
    start_json_and_control();
    recover_note(in);
    mp_err e = {{0}};
    uint64_t t0 = mp_now_ns(), size = 0, before = 0;
    mp_stat(in, &before, NULL);
    if (mp_compact(in, out, threads, limit, 1, &size, &e) != 0) {
        fprintf(stderr, "error: %s\n", e.msg);
        return 1;
    }
    FILE *hum = g_json ? stderr : stdout;
    char a[32], b[32];
    size_text(a, sizeof a, size);
    size_text(b, sizeof b, before > size ? before - size : 0);
    fprintf(hum, "Compacted to %s (%.0f s): %s, %s less\n", out, (double)(mp_now_ns() - t0) / 1e9, a, b);
    if (g_json) {
        char extra[96];
        snprintf(extra, sizeof extra, ",\"p5z_size\":%llu,\"saved_bytes\":%llu", (unsigned long long)size,
                 (unsigned long long)(before > size ? before - size : 0));
        json_result(1, NULL, extra);
    }
    return 0;
}

static int cmd_compact(int argc, char **argv) {
    prescan_json(argc, argv);
    return finish_cmd(run_compact(argc, argv));
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
    int recovered = recover_note(path);
    mp_err e = {{0}};
    mp_describe d;
    mp_complete c;
    mp_dlc_list dlc;
    if (mp_describe_full(path, &d, &c, &dlc, &e) != 0) {
        mp_dlc_list_free(&dlc);
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
    if (!strcmp(d.kind, "p5z")) {
        printf(",\"loose_bytes\":%llu,\"grown\":%u,\"unused_bytes\":%llu,\"recovered\":%s", (unsigned long long)d.loose_bytes,
               d.grown, (unsigned long long)d.unused_bytes, recovered ? "true" : "false");
        // what its index records of its packs (info --sums): left out when the index cannot give them
        mp_p5z *p = NULL;
        mp_p5z_sums s;
        if (mp_p5z_open(path, &p, NULL) == 0 && mp_p5z_sums_get(p, &s, NULL) == 0) {
            printf(",\"archive_id\":\"");
            for (int i = 0; i < 16; i++) printf("%02x", s.archive_id[i]);
            printf("\",\"level\":%d,\"volume_sums\":[", s.level);
            for (size_t i = 0; i < s.nvolumes; i++) {
                char sha[65];
                mp_sha256_hex(s.volumes[i].sha256, sha);
                printf("%s{\"name\":\"%s\",\"size\":%llu,\"sha256\":\"%s\"}", i ? "," : "", s.volumes[i].name,
                       (unsigned long long)s.volumes[i].size, sha);
            }
            printf("]");
            mp_p5z_sums_free(&s);
        }
        mp_p5z_close(p);
    }
    printf(",\"packed_files\":%llu,\"volumes\":%llu", (unsigned long long)d.packed_files,
           (unsigned long long)d.volumes);
    printf(",\"has_runtime\":%s,\"has_zstd_runtime\":%s,\"packed\":%s", d.has_runtime ? "true" : "false",
           d.has_zstd_runtime ? "true" : "false", d.packed ? "true" : "false");
    // a game or a DLC (and whether that one has files of its own), and the DLC the game carries
    printf(",\"content\":\"%s\",\"dlc_data\":%s,\"dlc\":[", d.dlc ? "dlc" : "game", d.dlc_data ? "true" : "false");
    for (size_t i = 0; i < dlc.n; i++) {
        printf("%s{\"content_id\":", i ? "," : "");
        print_json_string(dlc.v[i].content_id);
        printf(",\"data\":%s,\"folder\":", dlc.v[i].data ? "true" : "false");
        print_json_string(dlc.v[i].folder);
        printf(",\"size\":%llu,\"files\":%llu}", (unsigned long long)dlc.v[i].bytes, (unsigned long long)dlc.v[i].files);
    }
    printf("]");
    mp_dlc_list_free(&dlc);
    // whether the game is complete against the list of the original package's files (with the
    // first extra paths)
    mp_jbuf b = {0};
    mp_complete_json(&c, 1, &b);
    mp_complete_free(&c);
    if (b.p && !b.oom) printf(",\"completeness\":%s", b.p);
    mp_jbuf_free(&b);
    printf("}\n");
    fflush(stdout);
    return 0;
}

// info --sums: what the index records of the packs, in the lines the console's pack prints after its "# "
// notes (crates/maxpack-logic/src/pack.rs lines()), so that a diff of the two compares them.
static int cmd_info_sums(const char *path) {
    recover_note(path);
    mp_err e = {{0}};
    mp_p5z *p = NULL;
    mp_p5z_sums s;
    if (mp_p5z_open(path, &p, &e) != 0 || mp_p5z_sums_get(p, &s, &e) != 0) {
        fprintf(stderr, "error: %s\n", e.msg);
        mp_p5z_close(p);
        return 1;
    }
    mp_p5z_close(p);
    char *l = mp_p5z_sums_lines(&s);
    mp_p5z_sums_free(&s);
    if (!l) {
        fprintf(stderr, "error: out of memory\n");
        return 1;
    }
#ifdef _WIN32
    fflush(stdout);
    _setmode(_fileno(stdout), _O_BINARY);   // lines end in \n, as the console's
#endif
    fputs(l, stdout);
    fflush(stdout);
    free(l);
    return 0;
}

// info --play: what the console needs to play the .p5z without installing it, which its maxpack.image can
// hold (the console checks that too, and says so).
static int cmd_info_play(const char *path) {
    recover_note(path);
    char err[512] = {0};
    mp_play *p = mp_play_open(path, err, sizeof err);
    if (!p) {
        fprintf(stderr, "error: %s\n", err);
        return 1;
    }
    size_t nvol = mp_play_volume_count(p);
    if (nvol > MP_PLAY_MAX_VOLUMES) {
        mp_play_close(p);
        fprintf(stderr, "error: the .p5z has %zu volumes, more than the console takes (%u)\n", nvol,
                MP_PLAY_MAX_VOLUMES);
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
    if (argc == 2 && !strcmp(argv[0], "--sums")) return cmd_info_sums(argv[1]);
    if (argc == 2 && !strcmp(argv[1], "--sums")) return cmd_info_sums(argv[0]);
    if (argc != 1 || !strcmp(argv[0], "--json"))
        return bad_usage("info needs a .p5z file (or --json, --play or --sums and a path)", NULL);
    recover_note(argv[0]);
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
    recover_note(src);
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
    recover_note(src);
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

// ---- verify --quick ------------------------------------------------------------------------------

typedef struct {
    uint64_t print_ns, json_ns;
} quick_seen;

static void quick_progress(void *ctx, uint64_t done, uint64_t total) {
    quick_seen *q = ctx;
    uint64_t now = mp_now_ns();
    int end = done == total;
    if (g_json && (end || now - q->json_ns >= 100000000ull)) {
        q->json_ns = now;
        json_progress(NULL, "Verifying", done, total);
    }
    if (end || now - q->print_ns >= 1000000000ull) {
        q->print_ns = now;
        fprintf(stderr, "\rVerifying: %.1f %%", total ? 100.0 * (double)done / (double)total : 100.0);
    }
}

static int quick_cancelled(void *ctx) {
    (void)ctx;
    return mp_cancelled();
}

// The report as JSON fields, without the braces.
static void quick_fields(const mp_qv_report *r, mp_jbuf *b) {
    char hex[65];
    mp_jbuf_raw(b, "\"version\":");
    mp_jbuf_u64(b, r->version);
    mp_jbuf_raw(b, ",\"bytes\":");
    mp_jbuf_u64(b, r->bytes);
    mp_jbuf_raw(b, ",\"total\":");
    mp_jbuf_u64(b, r->total);
    mp_jbuf_raw(b, ",\"volumes\":");
    mp_jbuf_u64(b, r->volumes);
    mp_jbuf_raw(b, ",\"files\":");
    mp_jbuf_u64(b, r->files);
    mp_jbuf_raw(b, ",\"bad_count\":");
    mp_jbuf_u64(b, r->nbad);
    mp_jbuf_raw(b, ",\"bad\":[");
    for (size_t i = 0; i < r->nlisted; i++) {
        const mp_qv_bad *x = &r->bad[i];
        mp_jbuf_raw(b, i ? ",{\"kind\":" : "{\"kind\":");
        mp_jbuf_str(b, x->kind == MP_QV_VOLUME ? "volume" : "file");
        if (x->kind == MP_QV_VOLUME) {
            mp_jbuf_raw(b, ",\"volume\":");
            mp_jbuf_u64(b, x->volume);
        }
        mp_jbuf_raw(b, ",\"path\":");
        mp_jbuf_str(b, x->path);
        mp_jbuf_raw(b, ",\"why\":");
        mp_jbuf_str(b, x->why);
        mp_sha256_hex(x->expected, hex);
        mp_jbuf_raw(b, ",\"expected\":");
        mp_jbuf_str(b, hex);
        mp_jbuf_raw(b, ",\"found\":");
        if (x->has_found) {
            mp_sha256_hex(x->found, hex);
            mp_jbuf_str(b, hex);
        } else {
            mp_jbuf_raw(b, "null");
        }
        mp_jbuf_raw(b, "}");
    }
    mp_jbuf_raw(b, "]");
}

// verify --quick: 0 everything matched, 1 something did not or it could not check, 130 cancelled.
static int run_verify_quick(const char *target, int json) {
    if (mp_path_kind(target) != 1) {
        g_err_code = "bad_input";
        fprintf(stderr, "error: %s %s\n", mp_path_kind(target) == 0 ? "does not exist:" : "is not a .p5z file:", target);
        if (json && !g_json) printf("{\"ok\":false,\"error\":\"bad_input\"}\n");
        return 1;
    }
    start_json_and_control();
    recover_note(target);
    quick_seen seen = {0, 0};
    mp_qv_progress pr = {quick_progress, &seen};
    mp_qv_cancel ca = {quick_cancelled, NULL};
    mp_qv_report r;
    mp_err e = {{0}};
    uint64_t t0 = mp_now_ns();
    int rc = mp_p5z_verify_quick(target, &pr, &ca, &r, &e);
    if (seen.print_ns) fprintf(stderr, "\n");
    if (rc == MP_QV_CANCELLED) {
        mp_qv_report_free(&r);
        fprintf(stderr, "error: cancelled\n");
        if (json && !g_json) printf("{\"ok\":false,\"error\":\"cancelled\"}\n");
        return 130;
    }
    if (rc != MP_QV_CHECKED) {
        mp_qv_report_free(&r);
        g_err_code = "invalid";
        fprintf(stderr, "error: %s\n", e.msg);
        if (json && !g_json) {
            printf("{\"ok\":false,\"error\":\"invalid\",\"message\":");
            print_json_string(e.msg);
            printf("}\n");
        }
        return 1;
    }
    for (size_t i = 0; i < r.nlisted; i++) {
        const mp_qv_bad *x = &r.bad[i];
        char want[65], got[65];
        mp_sha256_hex(x->expected, want);
        mp_sha256_hex(x->found, got);
        const char *why = !strcmp(x->why, "header") ? "its 64-byte header is damaged or not of this .p5z"
                          : !strcmp(x->why, "data") ? "its compressed data do not decompress"
                          : !strcmp(x->why, "read")  ? "its bytes could not be read"
                                                     : "the SHA-256 does not match";
        if (x->has_found && strcmp(x->why, "header"))
            fprintf(stderr, "FAILED: %s: %s (expected %s, found %s)\n", x->path, why, want, got);
        else
            fprintf(stderr, "FAILED: %s: %s\n", x->path, why);
    }
    if (r.nbad > r.nlisted) fprintf(stderr, "... and %zu more failures\n", r.nbad - r.nlisted);
    FILE *hum = g_json || json ? stderr : stdout;
    char size[32];
    size_text(size, sizeof size, r.bytes);
    fprintf(hum, "verify --quick: %llu OK, %llu failed: %u volumes and %u files, %s read (%.0f s)\n",
            (unsigned long long)(r.volumes + r.files - r.nbad), (unsigned long long)r.nbad, r.volumes, r.files, size,
            (double)(mp_now_ns() - t0) / 1e9);
    mp_jbuf b = {0};
    mp_jbuf_raw(&b, ",");
    quick_fields(&r, &b);
    if (g_json) {
        json_result(r.ok, r.ok ? NULL : "verify_failed", b.p && !b.oom ? b.p : "");
    } else if (json) {
        printf("{\"ok\":%s%s}\n", r.ok ? "true" : "false", b.p && !b.oom ? b.p : "");
        fflush(stdout);
    }
    mp_jbuf_free(&b);
    int ok = r.ok;
    mp_qv_report_free(&r);
    if (!ok) g_err_code = "verify_failed";
    return ok ? 0 : 1;
}

static int run_verify(int argc, char **argv) {
    const char *target = NULL, *against = NULL;
    unsigned threads = mp_cpu_count();
    int quick = 0, json = 0;
    for (int i = 0; i < argc; i++) {
        const char *a = argv[i];
        int common = common_opt(argc, argv, &i);
        if (common < 0) return 2;
        if (common) {
            continue;
        } else if (!strcmp(a, "--quick")) {
            quick = 1;
        } else if (!strcmp(a, "--json")) {
            json = 1;
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
    if (json && !quick) return bad_usage("--json goes with --quick (verify --progress json gives its result as JSON)", NULL);
    if (quick && against) return bad_usage("--quick checks a .p5z against its own SHA-256 hashes, not --against", NULL);
    if (quick) return run_verify_quick(target, json);
    int kind = mp_path_kind(target);
    if (kind == 0) {
        g_err_code = "bad_input";
        fprintf(stderr, "error: does not exist: %s\n", target);
        return 1;
    }
    if (kind == 1 && against) return bad_usage("--against only works for folders (the .p5z already carries its SHA-256 hashes)", NULL);
    start_json_and_control();
    if (kind == 1) recover_note(target);
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
    // the dump it was made of, against the list of the original package it carries
    char *complete = verify_completeness(target, hum);
    if (g_json) {
        char extra[96];
        snprintf(extra, sizeof extra, ",\"files_ok\":%llu,\"files_bad\":%llu", (unsigned long long)r.files_ok,
                 (unsigned long long)r.files_bad);
        mp_jbuf b = {0};
        mp_jbuf_raw(&b, extra);
        if (complete) {
            mp_jbuf_raw(&b, ",\"completeness\":");
            mp_jbuf_raw(&b, complete);
        }
        json_result(r.files_bad == 0, NULL, b.p && !b.oom ? b.p : extra);
        mp_jbuf_free(&b);
    }
    free(complete);
    return r.files_bad ? 1 : 0;
}

// ---- send ----------------------------------------------------------------------------------------

// The exit code of each way a send ends.
static int send_exit(const char *code) {
    static const char *const codes[] = {"unreachable", "receive_off", "bad_code", "locked", "exists", "busy",
                                        "no_space", "fat32", "bad_name", "no_drive", "verify_failed",
                                        "console_cancelled", "console_stopped", "stalled", "write_error"};
    if (!code) return 0;
    if (!strcmp(code, "cancelled")) return 130;
    for (int i = 0; i < (int)(sizeof codes / sizeof *codes); i++)
        if (!strcmp(code, codes[i])) return 3 + i;
    return 1;
}

typedef struct {
    uint64_t t0, print_ns, json_ns, from;
    int line;   // a progress line is on stderr without its end
} send_seen;

static void send_end_line(send_seen *s) {
    if (s->line) fprintf(stderr, "\n");
    s->line = 0;
}

static void send_started(void *ctx, const char *name, const char *drive, uint64_t have, uint64_t size) {
    send_seen *s = ctx;
    s->t0 = mp_now_ns();
    s->from = have;
    char a[32], b[32];
    size_text(a, sizeof a, have);
    size_text(b, sizeof b, size);
    fprintf(stderr, "Sending %s (%s) to %s of the console\n", name, b, drive);
    if (have) fprintf(stderr, "The console has %s of it already: going on from there\n", a);
    if (g_json) {
        printf("{\"event\":\"started\",\"name\":");
        print_json_string(name);
        printf(",\"drive\":");
        print_json_string(drive);
        printf(",\"have\":%llu,\"size\":%llu}\n", (unsigned long long)have, (unsigned long long)size);
        fflush(stdout);
    }
}

static void send_sending(void *ctx, uint64_t have, uint64_t size, uint64_t sent) {
    send_seen *s = ctx;
    uint64_t now = mp_now_ns();
    double secs = (double)(now - s->t0) / 1e9;
    uint64_t rate = secs > 0 ? (uint64_t)((double)sent / secs) : 0;
    uint64_t eta = rate ? (size - have) / rate : 0;
    int end = have == size;
    if (g_json && (end || now - s->json_ns >= 100000000ull)) {
        s->json_ns = now;
        printf("{\"event\":\"progress\",\"phase\":\"sending\",\"done\":%llu,\"total\":%llu,\"rate\":%llu,\"eta\":%llu}\n",
               (unsigned long long)have, (unsigned long long)size, (unsigned long long)rate, (unsigned long long)eta);
        fflush(stdout);
    }
    if (end || now - s->print_ns >= 1000000000ull) {
        s->print_ns = now;
        char left[32];
        duration_text(left, sizeof left, eta);
        fprintf(stderr, "\rSending: %.1f %%, %.1f MB/s, %s left   ", size ? 100.0 * (double)have / (double)size : 100.0,
                (double)rate / 1e6, left);
        s->line = 1;
    }
}

static void send_verifying(void *ctx, uint64_t done, uint64_t total) {
    send_seen *s = ctx;
    uint64_t now = mp_now_ns();
    if (g_json) {
        printf("{\"event\":\"progress\",\"phase\":\"verifying\",\"done\":%llu,\"total\":%llu}\n", (unsigned long long)done,
               (unsigned long long)total);
        fflush(stdout);
    }
    if (done == total || now - s->print_ns >= 1000000000ull) {
        s->print_ns = now;
        fprintf(stderr, "\rThe console is verifying it: %.1f %%        ", total ? 100.0 * (double)done / (double)total : 0.0);
        s->line = 1;
    }
}

static void send_retry(void *ctx, unsigned attempt, unsigned wait_ms, uint64_t have) {
    send_seen *s = ctx;
    send_end_line(s);
    fprintf(stderr, "The connection to the console broke (try %u): trying again in %.1f s\n", attempt, wait_ms / 1000.0);
    if (g_json) {
        printf("{\"event\":\"retry\",\"attempt\":%u,\"wait_ms\":%u,\"have\":%llu}\n", attempt, wait_ms,
               (unsigned long long)have);
        fflush(stdout);
    }
}

// 1: stop and keep what arrived; 2: the line "cancel" with --delete-on-cancel, the console deletes it (the
// end of the input, a window closed or gone, always keeps it)
static int send_cancelled(void *ctx) {
    (void)ctx;
    return !mp_cancelled() ? 0 : g_cancel_line ? 2 : 1;
}

static void send_paused(void *ctx, const char *reason, uint64_t have) {
    send_seen *s = ctx;
    send_end_line(s);
    fprintf(stderr, "The console paused the receive (%s): starting it again from what it has\n",
            !strcmp(reason, "write_error") ? "it could not write" : "idle");
    if (g_json) {
        printf("{\"event\":\"paused\",\"reason\":\"%s\",\"have\":%llu}\n", !strcmp(reason, "write_error") ? "write_error" : "idle",
               (unsigned long long)have);
        fflush(stdout);
    }
}

static void send_bad_json(const mp_send_result *r, mp_jbuf *b) {
    mp_jbuf_raw(b, ",\"bad\":[");
    for (size_t i = 0; i < r->nbad; i++) {
        const mp_send_bad *x = &r->bad[i];
        mp_jbuf_raw(b, i ? ",{\"path\":" : "{\"path\":");
        mp_jbuf_str(b, x->path);
        if (x->volume >= 0) {
            mp_jbuf_raw(b, ",\"volume\":");
            mp_jbuf_i64(b, x->volume);
        }
        mp_jbuf_raw(b, ",\"why\":");
        mp_jbuf_str(b, x->why);
        mp_jbuf_raw(b, ",\"expected\":");
        mp_jbuf_str(b, x->expected);
        mp_jbuf_raw(b, ",\"found\":");
        if (x->found[0]) mp_jbuf_str(b, x->found);
        else mp_jbuf_raw(b, "null");
        mp_jbuf_raw(b, "}");
    }
    mp_jbuf_raw(b, "]");
}

static int run_send_drives(const mp_send_opts *o, int json) {
    mp_send_result r;
    mp_err e = {{0}};
    mp_console_drive *v = NULL;
    size_t n = 0;
    if (mp_send_drives(o, &v, &n, &r, &e) != 0) {
        fprintf(stderr, "error: %s\n", e.msg);
        if (json) {
            printf("{\"ok\":false,\"error\":\"%s\",\"message\":", r.code ? r.code : "io");
            print_json_string(e.msg);
            printf("}\n");
        }
        g_result_done = 1;
        return send_exit(r.code ? r.code : "io");
    }
    if (json) printf("{\"drives\":[");
    for (size_t i = 0; i < n; i++) {
        if (json) {
            printf("%s{\"id\":", i ? "," : "");
            print_json_string(v[i].id);
            printf(",\"free\":%llu,\"total\":%llu,\"fs\":", (unsigned long long)v[i].free, (unsigned long long)v[i].total);
            print_json_string(v[i].fs);
            printf("}");
        } else {
            char a[32], b[32];
            size_text(a, sizeof a, v[i].free);
            size_text(b, sizeof b, v[i].total);
            printf("%-10s %s free of %s%s%s%s\n", v[i].id, a, b, v[i].fs[0] ? " (" : "", v[i].fs, v[i].fs[0] ? ")" : "");
        }
    }
    if (json) printf("]}\n");
    fflush(stdout);
    free(v);
    return 0;
}

static int run_send_discard(const char *p5z, const mp_send_opts *o, int json) {
    mp_send_result r;
    mp_err e = {{0}};
    int rc = mp_send_discard(p5z, o, &r, &e);
    g_result_done = 1;
    if (rc != 0) {
        fprintf(stderr, "error: %s\n", e.msg);
        if (json) {
            printf("{\"ok\":false,\"error\":\"%s\",\"message\":", r.code ? r.code : "io");
            print_json_string(e.msg);
            printf("}\n");
        }
        return send_exit(r.code ? r.code : "io");
    }
    char had[32];
    size_text(had, sizeof had, r.have);
    fprintf(json ? stderr : stdout, "Deleted what the console had of %s on %s (%s)\n", r.name, r.drive, had);
    if (json) {
        printf("{\"ok\":true,\"name\":");
        print_json_string(r.name);
        printf(",\"drive\":");
        print_json_string(r.drive);
        printf(",\"had\":%llu}\n", (unsigned long long)r.have);
    }
    fflush(stdout);
    return 0;
}

static int run_send(int argc, char **argv) {
    mp_send_opts o;
    mp_send_opts_default(&o);
    const char *p5z = NULL, *addr = NULL, *code = NULL;
    int drives = 0, json = 0, discard = 0;
    for (int i = 0; i < argc; i++) {
        const char *a = argv[i];
        int common = common_opt(argc, argv, &i);
        if (common < 0) return 2;
        if (common) continue;
        if (!strcmp(a, "--drives")) {
            drives = 1;
        } else if (!strcmp(a, "--discard")) {
            discard = 1;
        } else if (!strcmp(a, "--delete-on-cancel")) {
            o.delete_on_cancel = 1;
        } else if (!strcmp(a, "--json")) {
            json = 1;
        } else if (!strcmp(a, "--code") || !strcmp(a, "--drive") || !strcmp(a, "--chunk") || !strcmp(a, "--retries") ||
                   !strcmp(a, "--retry-wait") || !strcmp(a, "--stall")) {
            if (i + 1 >= argc) return bad_usage("missing value for", a);
            const char *v = argv[++i];
            char *end = NULL;
            unsigned long long n = strtoull(v, &end, 10);
            int number = end != v && !*end && v[0] != '-';
            if (!strcmp(a, "--code")) {
                code = v;
            } else if (!strcmp(a, "--drive")) {
                if (!*v || strlen(v) >= 64) return bad_usage("invalid --drive", v);
                o.drive = v;
            } else if (!strcmp(a, "--chunk")) {
                if (!number || n < MP_SEND_MIN_CHUNK || n > MP_SEND_MAX_CHUNK)
                    return bad_usage("--chunk must be between 65536 and 67108864 bytes", v);
                o.chunk = (size_t)n;
            } else if (!strcmp(a, "--retries")) {
                if (!number || n > 1000) return bad_usage("--retries must be between 0 and 1000", v);
                o.retries = (unsigned)n;
            } else if (!strcmp(a, "--stall")) {
                if (!number || n < 1 || n > 86400) return bad_usage("--stall must be between 1 and 86400 seconds", v);
                o.stall_ms = (unsigned)n * 1000u;
            } else {
                if (!number || n < 1 || n > MP_SEND_MAX_WAIT_MS)
                    return bad_usage("--retry-wait must be between 1 and 30000 ms (no wait is longer than 30 s)", v);
                o.retry_ms = (unsigned)n;
            }
        } else if (a[0] == '-') {
            return bad_usage("unknown option", a);
        } else if (!p5z) {
            p5z = a;
        } else if (!addr) {
            addr = a;
        } else {
            return bad_usage("extra argument", a);
        }
    }
    if (drives && addr) return bad_usage("send --drives takes only the console's address", p5z);
    if (drives && discard) return bad_usage("--discard takes a .p5z, not --drives", NULL);
    if (drives) {   // its one argument is the address
        addr = p5z;
        p5z = NULL;
    }
    if (!drives && !p5z) return bad_usage("missing the .p5z to send", NULL);
    if (!addr) return bad_usage("missing the console's IP address", NULL);
    if (mp_send_parse_address(addr, &o) != 0)
        return bad_usage("the console's address must be its IP address, a.b.c.d or a.b.c.d:port", addr);
    if (!code) code = getenv("MAXPACK_SEND_CODE");
    if (!code) return bad_usage("missing --code: the pairing code the console shows", NULL);
    if (mp_send_set_code(&o, code) != 0) return bad_usage("--code must be the 6 digits the console shows", NULL);
    if (json) g_json = 1;
    if (drives) {
        g_json = 0;   // --drives --json is the list itself
        return run_send_drives(&o, json);
    }
    if (discard) {
        g_json = 0;   // --discard --json is its answer itself
        return run_send_discard(p5z, &o, json);
    }
    start_json_and_control();
    // what goes is a whole .p5z: one whose name the console takes, as it is (an append that stopped undone)
    const char *base = p5z + strlen(p5z);
    while (base > p5z && base[-1] != '/' && base[-1] != '\\') base--;
    mp_err e = {{0}};
    mp_send_result r;
    memset(&r, 0, sizeof r);
    const char *fail_code = NULL;
    if (mp_path_kind(p5z) != 1) {
        fail_code = "bad_input";
        mp_err_set(&e, "%s %s", mp_path_kind(p5z) == 0 ? "does not exist:" : "is not a .p5z file:", p5z);
    } else if (!mp_send_name_ok(base)) {
        fail_code = "bad_name";
        mp_err_set(&e, "the console takes only a plain .p5z name of at most 200 bytes (no '/', '\\', '..' or "
                       "control characters): %s", base);
    } else {
        int what = 0;
        recover_note(p5z);
        mp_p5z *pz = NULL;
        size_t pl = strlen(p5z);
        char *journal = malloc(pl + sizeof MP_P5Z_JOURNAL);
        if (journal) {
            memcpy(journal, p5z, pl);
            memcpy(journal + pl, MP_P5Z_JOURNAL, sizeof MP_P5Z_JOURNAL);
        }
        if (mp_p5z_recover(p5z, &what, NULL) == 0 && what == MP_RECOVER_BUSY) {
            fail_code = "p5z_busy";
            mp_err_set(&e, "another MaxPack is changing %s now: send it when that ends", p5z);
        } else if (journal && mp_path_kind(journal) != 0) {
            // a change of it stopped and could not be undone (a read-only file?): what it is cannot be told
            fail_code = "p5z_busy";
            mp_err_set(&e, "a change of %s stopped halfway and could not be undone: open it with MaxPack where it can "
                           "be written, then send it", p5z);
        } else if (mp_p5z_open(p5z, &pz, &e) != 0) {
            fail_code = "invalid";
        }
        mp_p5z_close(pz);
        free(journal);
    }
    int rc = 0;
    send_seen seen;
    memset(&seen, 0, sizeof seen);
    uint64_t t0 = mp_now_ns();
    if (!fail_code) {
        mp_send_hooks h = {send_started, send_sending, send_verifying, send_retry, send_paused, send_cancelled, &seen};
        if (mp_send(p5z, &o, &h, &r, &e) != 0) fail_code = r.code ? r.code : "io";
        send_end_line(&seen);
    }
    mp_jbuf b = {0};
    if (fail_code) {
        fprintf(stderr, "error: %s\n", e.msg);
        for (size_t i = 0; i < r.nbad; i++) {
            const mp_send_bad *x = &r.bad[i];
            if (x->expected[0] && x->found[0])
                fprintf(stderr, "FAILED: %s: %s (expected %s, found %s)\n", x->path, x->why, x->expected, x->found);
            else
                fprintf(stderr, "FAILED: %s%s%s\n", x->path, x->why[0] ? ": " : "", x->why);
        }
        rc = !strcmp(fail_code, "bad_name") ? 11 : send_exit(fail_code);
        if (!strcmp(fail_code, "bad_input") || !strcmp(fail_code, "invalid") || !strcmp(fail_code, "p5z_busy")) rc = 1;
        if (g_json) {
            mp_jbuf_raw(&b, ",\"message\":");
            mp_jbuf_str(&b, e.msg);
            mp_jbuf_raw(&b, ",\"detail\":");
            mp_jbuf_str(&b, r.detail);
            mp_jbuf_raw(&b, r.kept ? ",\"kept\":true" : ",\"kept\":false");
            mp_jbuf_raw(&b, ",\"have\":");
            mp_jbuf_u64(&b, r.have);
            mp_jbuf_raw(&b, ",\"drive\":");
            mp_jbuf_str(&b, r.drive);
            send_bad_json(&r, &b);
            json_result(0, fail_code, b.p && !b.oom ? b.p : "");
        }
        g_result_done = 1;
    } else {
        double secs = (double)(mp_now_ns() - t0) / 1e9;
        char a[32], d[32];
        size_text(a, sizeof a, r.size);
        duration_text(d, sizeof d, (uint64_t)secs);
        FILE *hum = g_json ? stderr : stdout;
        fprintf(hum, "Sent and verified: %s on %s of the console (%s in %s, %.1f MB/s)\n", r.name, r.drive, a, d,
                secs > 0 ? (double)r.sent / 1e6 / secs : 0.0);
        if (g_json) {
            mp_jbuf_raw(&b, ",\"name\":");
            mp_jbuf_str(&b, r.name);
            mp_jbuf_raw(&b, ",\"drive\":");
            mp_jbuf_str(&b, r.drive);
            mp_jbuf_raw(&b, ",\"size\":");
            mp_jbuf_u64(&b, r.size);
            mp_jbuf_raw(&b, ",\"sent\":");
            mp_jbuf_u64(&b, r.sent);
            mp_jbuf_raw(&b, ",\"resumed_from\":");
            mp_jbuf_u64(&b, r.resumed_from);
            mp_jbuf_raw(&b, ",\"retries\":");
            mp_jbuf_u64(&b, r.retries);
            char t[32];
            snprintf(t, sizeof t, ",\"seconds\":%.1f", secs);
            mp_jbuf_raw(&b, t);
            json_result(1, NULL, b.p && !b.oom ? b.p : "");
        }
    }
    mp_jbuf_free(&b);
    mp_send_result_free(&r);
    return rc;
}

static int cmd_send(int argc, char **argv) {
    prescan_json(argc, argv);
    for (int i = 0; i < argc; i++)
        if (!strcmp(argv[i], "--json") && !has_arg(argc, argv, "--drives")) g_json = 1;
    int rc = run_send(argc, argv);
    if (rc == 2) return finish_cmd(rc);
    return rc;
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
               "\"images\":true,\"fpkg\":true,\"passcode\":true,\"completeness\":true,\"dlc\":true,\"grow\":true,"
               "\"verify_quick\":true,\"send\":true}\n");
        return 0;
    }
    if (argc >= 2 && (!strcmp(argv[1], "--help") || !strcmp(argv[1], "-h") || !strcmp(argv[1], "help"))) {
        usage(stdout);
        return 0;
    }
    // --passcode, for the commands that open a game: taken out here, before each one reads its options
    if (argc >= 2 && (!strcmp(argv[1], "analyze") || !strcmp(argv[1], "pack") || !strcmp(argv[1], "info") ||
                      !strcmp(argv[1], "icon") || !strcmp(argv[1], "verify") || !strcmp(argv[1], "add") ||
                      !strcmp(argv[1], "update"))) {
        int rc = take_passcode(&argc, argv);
        if (rc) return rc;
    }
    if (argc >= 2 && strcmp(argv[1], "analyze") == 0) return cmd_analyze(argc - 2, argv + 2);
    if (argc >= 2 && strcmp(argv[1], "pack") == 0) return cmd_pack(argc - 2, argv + 2);
    if (argc >= 2 && strcmp(argv[1], "unpack") == 0) return cmd_unpack(argc - 2, argv + 2);
    if (argc >= 2 && strcmp(argv[1], "info") == 0) return cmd_info(argc - 2, argv + 2);
    if (argc >= 2 && strcmp(argv[1], "icon") == 0) return cmd_icon(argc - 2, argv + 2);
    if (argc >= 2 && strcmp(argv[1], "verify") == 0) return cmd_verify(argc - 2, argv + 2);
    if (argc >= 2 && strcmp(argv[1], "measure") == 0) return cmd_measure(argc - 2, argv + 2);
    if (argc >= 2 && strcmp(argv[1], "add") == 0) return cmd_add(argc - 2, argv + 2);
    if (argc >= 2 && strcmp(argv[1], "update") == 0) return cmd_update(argc - 2, argv + 2);
    if (argc >= 2 && strcmp(argv[1], "compact") == 0) return cmd_compact(argc - 2, argv + 2);
    if (argc >= 2 && strcmp(argv[1], "send") == 0) return cmd_send(argc - 2, argv + 2);
    if (argc >= 2) return bad_usage("unknown command", argv[1]);
    usage(stderr);
    return 2;
}
