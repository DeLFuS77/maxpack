// SPDX-License-Identifier: GPL-3.0-or-later
#include "mp_rules.h"
#include "mp_test.h"

MP_TEST(classify_table) {
    mp_rules *r = mp_rules_new();
    static const char *loose[] = {
        "eboot.bin", "sub/dir/EBOOT.BIN", "x.elf", "a/b.self", "prx/libfoo.prx", "libX.SPRX",
        "settings.ini", "sce_sys/param.json", "sce_module/x.bin", "fakelib/libSceAmpr.sprx",
        "fakelib2/foo.bin", "ampr_emu.index", "ampr_zstd.index", "ampr_zstd-000.pak",
        "ampr_assets.bin", "ampr_assets/x.dat", "apr_emu.log", "ampr_commands.bin",
    };
    static const char *packable[] = {
        "re_chunk_000.pak", "b1/content/paks/pakchunk0-ps5.ucas", "data/sce_sys/x.bin",
        "media/fakelib/x.bin", "eboot.bin.bak", "readme.txt", "ampr_zstd.pak", "sub/ampr_emu.index",
    };
    MP_CHECK(mp_rules_classify(r, "data/e.dat", 0) == MP_LOOSE);
    for (size_t i = 0; i < sizeof loose / sizeof *loose; i++)
        if (mp_rules_classify(r, loose[i], 100) != MP_LOOSE) MP_FAIL_("should be loose: %s", loose[i]);
    for (size_t i = 0; i < sizeof packable / sizeof *packable; i++)
        if (mp_rules_classify(r, packable[i], 100) != MP_PACKABLE)
            MP_FAIL_("should be packable: %s", packable[i]);
    mp_rules_free(r);
}

MP_TEST(keep_loose) {
    mp_err e = {{0}};
    mp_rules *r = mp_rules_new();
    MP_CHECK(mp_rules_add_keep_loose(r, "b1/content/paks/*.utoc", &e) == 0);
    MP_CHECK(mp_rules_add_keep_loose(r, "**/*.bnk", &e) == 0);
    MP_CHECK(mp_rules_classify(r, "b1/content/paks/pakchunk0-ps5.utoc", 100) == MP_LOOSE);
    MP_CHECK(mp_rules_classify(r, "b1/content/paks/pakchunk0-ps5.ucas", 100) == MP_PACKABLE);
    MP_CHECK(mp_rules_classify(r, "a/b/c.BNK", 100) == MP_LOOSE);
    MP_CHECK(mp_rules_add_keep_loose(r, "a\\b", &e) == -1);
    MP_CHECK(e.msg[0] != 0);
    MP_CHECK(mp_rules_add_keep_loose(r, "", &e) == -1);
    mp_rules_free(r);
}

MP_TEST(glob_semantics) {
    MP_CHECK(!mp_glob_match("*.pak", "a/b.pak"));
    MP_CHECK(mp_glob_match("*.pak", "B.PAK"));
    MP_CHECK(mp_glob_match("**/x", "x"));
    MP_CHECK(mp_glob_match("**/x", "a/b/x"));
    MP_CHECK(!mp_glob_match("**/x", "a/bx"));
    MP_CHECK(mp_glob_match("?.bin", "a.bin"));
    MP_CHECK(!mp_glob_match("?.bin", "ab.bin"));
    MP_CHECK(mp_glob_match("many/**", "many/f000.json"));
    MP_CHECK(mp_glob_match("a/**/c", "a/c"));
    MP_CHECK(mp_glob_match("a/**/c", "a/x/y/c"));
    MP_CHECK(!mp_glob_match("a/*", "a/b/c"));
}

int main(void) {
    MP_RUN(classify_table);
    MP_RUN(keep_loose);
    MP_RUN(glob_semantics);
    MP_TEST_MAIN_END();
}
