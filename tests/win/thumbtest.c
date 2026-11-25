// SPDX-License-Identifier: GPL-3.0-or-later
// maxpack-thumbs.dll, the thumbnail provider of .p5z files, loaded as the Explorer loads it: its class
// from DllGetClassObject, a stream on the file, Initialize and GetThumbnail. The icon comes out at the
// size asked (never larger than its own), with its alpha; anything that is not a readable .p5z with an icon gives a
// failure (the Explorer shows the normal icon). The DLL carries its C runtime and exports only the two
// functions COM needs.
//
// mp_thumbtest <maxpack-thumbs.dll>
#define COBJMACROS
#include <windows.h>
#include <objbase.h>
#include <propsys.h>
#include <shlwapi.h>
#include <thumbcache.h>

#include <stdio.h>
#include <stdlib.h>
#include <zlib.h>

#include "mp_fixture.h"

#define TMP MP_TEST_TMP "/thumbtest"

static const CLSID CLSID_MaxPackThumbs = {0x266D6364, 0x17C0, 0x4B3E, {0xBB, 0x33, 0x7E, 0xEB, 0x90, 0xC9, 0x18, 0x37}};

static HMODULE dll;
static HRESULT(STDAPICALLTYPE *get_class)(REFCLSID, REFIID, void **);
static HRESULT(STDAPICALLTYPE *can_unload)(void);

// ---- a PNG of w x h pixels in three bands from left to right: opaque red, blue at half alpha, and
// ---- green with no alpha -----------------------------------------------------------------------

static void be32(uint8_t *p, uint32_t v) {
    p[0] = (uint8_t)(v >> 24);
    p[1] = (uint8_t)(v >> 16);
    p[2] = (uint8_t)(v >> 8);
    p[3] = (uint8_t)v;
}

static size_t chunk(uint8_t *out, const char *type, const uint8_t *data, uint32_t n) {
    be32(out, n);
    memcpy(out + 4, type, 4);
    if (n) memcpy(out + 8, data, n);
    be32(out + 8 + n, (uint32_t)crc32(0, out + 4, n + 4));
    return 12 + (size_t)n;
}

static uint8_t *make_png(uint32_t w, uint32_t h, size_t *len) {
    size_t row = 1 + 4 * (size_t)w, raw_n = row * h;
    uint8_t *raw = malloc(raw_n);
    for (uint32_t y = 0; y < h; y++) {
        uint8_t *r = raw + row * y;
        r[0] = 0;   // no filter
        for (uint32_t x = 0; x < w; x++) {
            uint8_t *p = r + 1 + 4 * (size_t)x;
            int band = x < w / 3 ? 0 : x < 2 * w / 3 ? 1 : 2;
            p[0] = band == 0 ? 255 : 0;
            p[1] = band == 2 ? 255 : 0;
            p[2] = band == 1 ? 255 : 0;
            p[3] = band == 0 ? 255 : band == 1 ? 128 : 0;
        }
    }
    uLongf zn = compressBound((uLong)raw_n);
    uint8_t *z = malloc(zn);
    compress2(z, &zn, raw, (uLong)raw_n, 9);
    uint8_t *png = malloc(8 + 25 + 12 + zn + 12);
    memcpy(png, "\x89PNG\r\n\x1a\n", 8);
    uint8_t ihdr[13];
    be32(ihdr, w);
    be32(ihdr + 4, h);
    ihdr[8] = 8;    // bits per sample
    ihdr[9] = 6;    // RGBA
    ihdr[10] = ihdr[11] = ihdr[12] = 0;
    size_t n = 8;
    n += chunk(png + n, "IHDR", ihdr, 13);
    n += chunk(png + n, "IDAT", z, (uint32_t)zn);
    n += chunk(png + n, "IEND", NULL, 0);
    free(z);
    free(raw);
    *len = n;
    return png;
}

// A game with that icon (none if icon is NULL) packed into name.p5z, whose path it gives.
static const char *game(const char *name, const uint8_t *icon, size_t icon_n) {
    static char p5z[512];
    char dir[512], f[600];
    snprintf(dir, sizeof dir, TMP "/%s", name);
    snprintf(p5z, sizeof p5z, TMP "/%s.p5z", name);
    mpt_clear(dir);
    snprintf(f, sizeof f, "%s/sce_sys/param.json", dir);
    mpt_put_text(f, "{\"titleId\":\"ICON00003\",\"contentVersion\":\"01.000.000\"}");
    snprintf(f, sizeof f, "%s/eboot.bin", dir);
    mpt_put_text(f, "eboot");
    if (icon) {
        snprintf(f, sizeof f, "%s/sce_sys/icon0.png", dir);
        mpt_put(f, icon, icon_n);
    }
    mpt_pack(dir, p5z, 1, NULL);
    return p5z;
}

// ---- the provider ------------------------------------------------------------------------------

typedef struct {
    HRESULT hr;
    HBITMAP bmp;
    WTS_ALPHATYPE alpha;
} thumb;

// The thumbnail of a file, as the Explorer asks for it.
static thumb thumbnail_of(const char *path, UINT cx) {
    thumb t = {E_FAIL, NULL, WTSAT_UNKNOWN};
    IClassFactory *cf = NULL;
    IInitializeWithStream *init = NULL;
    IThumbnailProvider *tp = NULL;
    IStream *s = NULL;
    wchar_t wpath[600];
    MultiByteToWideChar(CP_UTF8, 0, path, -1, wpath, 600);
    if (FAILED(get_class(&CLSID_MaxPackThumbs, &IID_IClassFactory, (void **)&cf))) {
        MP_FAIL_("no class factory");
        return t;
    }
    if (FAILED(IClassFactory_CreateInstance(cf, NULL, &IID_IInitializeWithStream, (void **)&init))) {
        MP_FAIL_("no object");
        goto done;
    }
    if (FAILED(IInitializeWithStream_QueryInterface(init, &IID_IThumbnailProvider, (void **)&tp))) {
        MP_FAIL_("no IThumbnailProvider");
        goto done;
    }
    if (FAILED(SHCreateStreamOnFileEx(wpath, STGM_READ | STGM_SHARE_DENY_NONE, 0, FALSE, NULL, &s))) {
        MP_FAIL_("no stream on %s", path);
        goto done;
    }
    if (FAILED(IInitializeWithStream_Initialize(init, s, STGM_READ))) {
        MP_FAIL_("Initialize");
        goto done;
    }
    // initialized once only
    MP_CHECK(IInitializeWithStream_Initialize(init, s, STGM_READ) == HRESULT_FROM_WIN32(ERROR_ALREADY_INITIALIZED));
    t.hr = IThumbnailProvider_GetThumbnail(tp, cx, &t.bmp, &t.alpha);
done:
    if (s) IStream_Release(s);
    if (tp) IThumbnailProvider_Release(tp);
    if (init) IInitializeWithStream_Release(init);
    IClassFactory_Release(cf);
    return t;
}

// The thumbnail is a 32-bit DIB of w x h with the icon's three bands, in straight (not premultiplied)
// BGRA, as the Explorer takes WTSAT_ARGB: opaque red, blue at half alpha (premultiplied, it would be half
// as bright), and fully transparent.
static void check_bitmap(HBITMAP bmp, LONG w, LONG h) {
    DIBSECTION ds;
    if (!bmp || GetObject(bmp, sizeof ds, &ds) != sizeof ds) {
        MP_FAIL_("not a DIB section");
        return;
    }
    MP_CHECK_EQ_U64(ds.dsBm.bmWidth, w);
    MP_CHECK_EQ_U64(ds.dsBm.bmHeight, h);
    MP_CHECK_EQ_U64(ds.dsBm.bmBitsPixel, 32);
    if (ds.dsBm.bmWidth != w || ds.dsBm.bmHeight != h || ds.dsBm.bmBitsPixel != 32 || !ds.dsBm.bmBits) return;
    const uint8_t *bits = ds.dsBm.bmBits;
    size_t stride = (size_t)ds.dsBm.bmWidthBytes;
    int opaque = 0;
    for (LONG y = 0; y < h; y++)
        for (LONG x = 0; x < w; x++)
            if (bits[stride * y + 4 * (size_t)x + 3]) opaque = 1;
    MP_CHECK(opaque);
    const uint8_t *row = bits + stride * (size_t)(h / 2);
    const uint8_t *red = row + 4 * (size_t)(w / 6), *blue = row + 4 * (size_t)(w / 2), *clear = row + 4 * (size_t)(5 * w / 6);
    if (!(red[0] < 8 && red[1] < 8 && red[2] > 247 && red[3] == 255))
        MP_FAIL_("the red band is %u %u %u %u (BGRA)", red[0], red[1], red[2], red[3]);
    if (!(blue[0] > 247 && blue[1] < 8 && blue[2] < 8 && blue[3] >= 126 && blue[3] <= 130))
        MP_FAIL_("the blue band at half alpha is %u %u %u %u (BGRA)", blue[0], blue[1], blue[2], blue[3]);
    if (clear[3]) MP_FAIL_("the transparent band is %u %u %u %u (BGRA)", clear[0], clear[1], clear[2], clear[3]);
}

static void expect_none(const char *what, const char *path) {
    thumb t = thumbnail_of(path, 256);
    if (SUCCEEDED(t.hr) || t.bmp) MP_FAIL_("%s: a thumbnail (0x%08lx)", what, (unsigned long)t.hr);
    if (t.bmp) DeleteObject(t.bmp);
}

MP_TEST(thumb_dll_square_icon) {
    size_t n = 0;
    uint8_t *png = make_png(512, 512, &n);
    const char *p = game("square", png, n);
    thumb t = thumbnail_of(p, 256);
    MP_CHECK(t.hr == S_OK);
    MP_CHECK(t.alpha == WTSAT_ARGB);
    check_bitmap(t.bmp, 256, 256);
    if (t.bmp) DeleteObject(t.bmp);
    t = thumbnail_of(p, 48);
    MP_CHECK(t.hr == S_OK);
    check_bitmap(t.bmp, 48, 48);
    if (t.bmp) DeleteObject(t.bmp);
    // never larger than the icon: the Explorer takes a smaller bitmap
    t = thumbnail_of(p, 1024);
    MP_CHECK(t.hr == S_OK);
    check_bitmap(t.bmp, 512, 512);
    if (t.bmp) DeleteObject(t.bmp);
    free(png);
}

// The longer side is the size asked, or the icon's when it is smaller; the other keeps the proportion.
MP_TEST(thumb_dll_wide_icon) {
    size_t n = 0;
    uint8_t *png = make_png(400, 200, &n);
    thumb t = thumbnail_of(game("wide", png, n), 256);
    MP_CHECK(t.hr == S_OK);
    check_bitmap(t.bmp, 256, 128);
    if (t.bmp) DeleteObject(t.bmp);
    free(png);
    png = make_png(100, 50, &n);
    t = thumbnail_of(game("small", png, n), 256);
    MP_CHECK(t.hr == S_OK);
    check_bitmap(t.bmp, 100, 50);
    if (t.bmp) DeleteObject(t.bmp);
    free(png);
}

MP_TEST(thumb_dll_refuses) {
    size_t n = 0, pn = 0;
    uint8_t *png = make_png(64, 64, &n);
    const char *p = game("full", png, n);
    uint8_t *d = mpt_slurp(p, &pn);
    if (d) {
        remove(TMP "/truncated.p5z");
        mpt_put(TMP "/truncated.p5z", d, pn - 1);
        expect_none("truncated", TMP "/truncated.p5z");
        for (size_t i = 0; i < pn; i++) d[i] = (uint8_t)(i * 31 + 7);
        remove(TMP "/junk.p5z");
        mpt_put(TMP "/junk.p5z", d, pn);
        expect_none("not a .p5z", TMP "/junk.p5z");
    }
    free(d);
    expect_none("no icon", game("noicon", NULL, 0));
    uint8_t notpng[300];
    for (size_t i = 0; i < sizeof notpng; i++) notpng[i] = (uint8_t)(i * 13 + 1);
    expect_none("an icon that is not a PNG", game("notpng", notpng, sizeof notpng));
    // a PNG too large in pixels, which is never decoded
    uint8_t *huge = make_png(64, 64, &n);
    be32(huge + 16, 100000);
    be32(huge + 20, 100000);
    be32(huge + 29, (uint32_t)crc32(0, huge + 12, 17));
    expect_none("a PNG of 100000 x 100000", game("huge", huge, n));
    free(huge);
    free(png);
}

// The class, and nothing else: another CLSID, a missing stream, no output.
MP_TEST(thumb_dll_class) {
    static const CLSID other = {0x266D6364, 0x17C0, 0x4B3E, {0xBB, 0x33, 0x7E, 0xEB, 0x90, 0xC9, 0x18, 0x38}};
    void *x = NULL;
    MP_CHECK(get_class(&other, &IID_IClassFactory, &x) == CLASS_E_CLASSNOTAVAILABLE && x == NULL);
    IClassFactory *cf = NULL;
    MP_CHECK(get_class(&CLSID_MaxPackThumbs, &IID_IClassFactory, (void **)&cf) == S_OK);
    if (!cf) return;
    IUnknown *outer = (IUnknown *)cf;
    MP_CHECK(IClassFactory_CreateInstance(cf, outer, &IID_IUnknown, &x) == CLASS_E_NOAGGREGATION);
    IThumbnailProvider *tp = NULL;
    MP_CHECK(IClassFactory_CreateInstance(cf, NULL, &IID_IThumbnailProvider, (void **)&tp) == S_OK);
    if (tp) {
        MP_CHECK(can_unload() == S_FALSE);   // an object is alive
        HBITMAP bmp = (HBITMAP)1;
        WTS_ALPHATYPE a = WTSAT_ARGB;
        MP_CHECK(FAILED(IThumbnailProvider_GetThumbnail(tp, 256, &bmp, &a)));   // not initialized
        MP_CHECK(bmp == NULL);
        IInitializeWithStream *init = NULL;
        MP_CHECK(IThumbnailProvider_QueryInterface(tp, &IID_IInitializeWithStream, (void **)&init) == S_OK);
        if (init) {
            MP_CHECK(IInitializeWithStream_Initialize(init, NULL, STGM_READ) == E_INVALIDARG);
            IInitializeWithStream_Release(init);
        }
        IThumbnailProvider_Release(tp);
    }
    IClassFactory_Release(cf);
    MP_CHECK(can_unload() == S_OK);
}

// The DLL imports no C runtime DLL (it may be loaded where none is installed), and exports only
// what COM needs: it never registers itself.
MP_TEST(thumb_dll_alone) {
    const uint8_t *base = (const uint8_t *)dll;
    const IMAGE_NT_HEADERS *nt = (const IMAGE_NT_HEADERS *)(base + ((const IMAGE_DOS_HEADER *)base)->e_lfanew);
    const IMAGE_DATA_DIRECTORY *dir = &nt->OptionalHeader.DataDirectory[IMAGE_DIRECTORY_ENTRY_IMPORT];
    int n = 0;
    for (const IMAGE_IMPORT_DESCRIPTOR *im = (const void *)(base + dir->VirtualAddress); dir->VirtualAddress && im->Name; im++, n++) {
        const char *name = (const char *)(base + im->Name);
        if (!_strnicmp(name, "vcruntime", 9) || !_strnicmp(name, "msvcr", 5) || !_strnicmp(name, "ucrtbase", 8) ||
            !_strnicmp(name, "api-ms-win-crt", 14))
            MP_FAIL_("imports %s", name);
    }
    MP_CHECK(n > 0);
    MP_CHECK(GetProcAddress(dll, "DllRegisterServer") == NULL);
    MP_CHECK(GetProcAddress(dll, "DllUnregisterServer") == NULL);
    MP_CHECK(GetProcAddress(dll, "mp_p5z_icon_read") == NULL);
}

int main(int argc, char **argv) {
    if (argc != 2) {
        fprintf(stderr, "usage: mp_thumbtest <maxpack-thumbs.dll>\n");
        return 2;
    }
    if (FAILED(CoInitializeEx(NULL, COINIT_APARTMENTTHREADED))) return 1;
    dll = LoadLibraryA(argv[1]);
    if (!dll) {
        fprintf(stderr, "cannot load %s\n", argv[1]);
        return 1;
    }
    get_class = (HRESULT(STDAPICALLTYPE *)(REFCLSID, REFIID, void **))(void *)GetProcAddress(dll, "DllGetClassObject");
    can_unload = (HRESULT(STDAPICALLTYPE *)(void))(void *)GetProcAddress(dll, "DllCanUnloadNow");
    if (!get_class || !can_unload) {
        fprintf(stderr, "the DLL does not export DllGetClassObject and DllCanUnloadNow\n");
        return 1;
    }
    mpt_clear(TMP);
    MP_RUN(thumb_dll_square_icon);
    MP_RUN(thumb_dll_wide_icon);
    MP_RUN(thumb_dll_refuses);
    MP_RUN(thumb_dll_class);
    MP_RUN(thumb_dll_alone);
    MP_CHECK(can_unload() == S_OK);
    CoUninitialize();
    MP_TEST_MAIN_END();
}
