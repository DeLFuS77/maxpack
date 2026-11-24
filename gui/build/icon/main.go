// SPDX-License-Identifier: GPL-3.0-or-later

// Command icon draws the provisional icon of MaxPack: the glyph of Compress in the sidebar (two
// arrows pointing at a line) in white on a rounded square of the accent color of the page. It
// uses only the standard library, so the program of the window depends on nothing for it:
//
//	go run ./build/icon            (from gui/, writes build/appicon.png, build/appicon.ico and
//	                                build/linux/maxpack.png)
package main

import (
	"bytes"
	"encoding/binary"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
)

const (
	// the PNG is 1024 px; the square has a margin around it, as the icons of macOS have
	pngSize = 1024
	margin  = 64.0 / 1024
	corner  = 0.225 * (1 - 2*margin)
	// the glyph is the 24 x 24 grid of the icons of the page, drawn this big (of the image) with a
	// line of 2.4 of those units
	glyph  = 23.0 * 24 / 1024
	stroke = 2.4 * glyph / 24 / 2 // half the width of the line
	sub    = 4                    // samples per side of a pixel, for smooth edges
)

// the sizes of the Windows icon
var icoSizes = []int{16, 32, 48, 64, 256}

// the glyph of Compress in index.html: M12 3v6 m0 0-3-3 m3 3 3-3 M12 21v-6 m0 0-3 3 m3-3 3 3 M5 12h14
var segments = [][4]float64{
	{12, 3, 12, 9}, {12, 9, 9, 6}, {12, 9, 15, 6},
	{12, 21, 12, 15}, {12, 15, 9, 18}, {12, 15, 15, 18},
	{5, 12, 19, 12},
}

func main() {
	dir := flag.String("dir", "build", "the build folder, where the icons are written")
	flag.Parse()
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, render(pngSize)); err != nil {
		fail(err)
	}
	if err := os.WriteFile(filepath.Join(*dir, "appicon.png"), encoded.Bytes(), 0o644); err != nil {
		fail(err)
	}
	if err := os.WriteFile(filepath.Join(*dir, "appicon.ico"), icon(), 0o644); err != nil {
		fail(err)
	}
	// the icon of the menu of Linux, at the size install.sh puts it for
	encoded.Reset()
	if err := png.Encode(&encoded, render(256)); err != nil {
		fail(err)
	}
	if err := os.WriteFile(filepath.Join(*dir, "linux", "maxpack.png"), encoded.Bytes(), 0o644); err != nil {
		fail(err)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "icon:", err)
	os.Exit(1)
}

// render draws the icon n pixels wide.
func render(n int) *image.NRGBA {
	// the accent of the light theme of the page (style.css), which white reads well on
	back := color.NRGBA{R: 0x0a, G: 0x6f, B: 0xb0, A: 255}
	img := image.NewNRGBA(image.Rect(0, 0, n, n))
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			var square, white float64
			for sy := 0; sy < sub; sy++ {
				for sx := 0; sx < sub; sx++ {
					px := (float64(x) + (float64(sx)+0.5)/sub) / float64(n)
					py := (float64(y) + (float64(sy)+0.5)/sub) / float64(n)
					if inSquare(px, py) {
						square++
						if inGlyph(px, py) {
							white++
						}
					}
				}
			}
			if square == 0 {
				continue
			}
			mix := white / square
			img.SetNRGBA(x, y, color.NRGBA{
				R: uint8(math.Round(float64(back.R)*(1-mix) + 255*mix)),
				G: uint8(math.Round(float64(back.G)*(1-mix) + 255*mix)),
				B: uint8(math.Round(float64(back.B)*(1-mix) + 255*mix)),
				A: uint8(math.Round(255 * square / (sub * sub))),
			})
		}
	}
	return img
}

// icon is the Windows icon: the same drawing at several sizes, as bitmaps (what the installer
// maker and the resources of a program take everywhere).
func icon() []byte {
	var head, body bytes.Buffer
	le := binary.LittleEndian
	binary.Write(&head, le, [3]uint16{0, 1, uint16(len(icoSizes))})
	offset := 6 + 16*len(icoSizes)
	for _, n := range icoSizes {
		var dib bytes.Buffer
		binary.Write(&dib, le, struct {
			Size          uint32
			Width, Height int32
			Planes, Bits  uint16
			Compression   uint32
			ImageSize     uint32
			X, Y          int32
			Used, Needed  uint32
		}{40, int32(n), int32(2 * n), 1, 32, 0, uint32(4 * n * n), 0, 0, 0, 0})
		img := render(n)
		for y := n - 1; y >= 0; y-- { // bottom up, as BGRA
			for x := 0; x < n; x++ {
				c := img.NRGBAAt(x, y)
				dib.Write([]byte{c.B, c.G, c.R, c.A})
			}
		}
		dib.Write(make([]byte, ((n+31)/32*4)*n)) // the mask: all opaque, the alpha rules
		width := byte(n)
		if n >= 256 {
			width = 0
		}
		head.Write([]byte{width, width, 0, 0})
		binary.Write(&head, le, [2]uint16{1, 32})
		binary.Write(&head, le, [2]uint32{uint32(dib.Len()), uint32(offset)})
		offset += dib.Len()
		body.Write(dib.Bytes())
	}
	return append(head.Bytes(), body.Bytes()...)
}

// inSquare tells whether the point (in 0..1) is inside the rounded square.
func inSquare(x, y float64) bool {
	lo, hi := margin, 1-margin
	if x < lo || x > hi || y < lo || y > hi {
		return false
	}
	// the distance to the nearest corner centre, when the point is in a corner block
	cx := math.Min(math.Max(x, lo+corner), hi-corner)
	cy := math.Min(math.Max(y, lo+corner), hi-corner)
	return math.Hypot(x-cx, y-cy) <= corner
}

// inGlyph tells whether the point (in 0..1) is within the line of the glyph, round at its ends.
func inGlyph(x, y float64) bool {
	offset := (1 - glyph) / 2
	gx, gy := (x-offset)/glyph*24, (y-offset)/glyph*24
	for _, s := range segments {
		if distance(gx, gy, s[0], s[1], s[2], s[3]) <= stroke/glyph*24 {
			return true
		}
	}
	return false
}

// distance is how far the point is from the segment.
func distance(px, py, ax, ay, bx, by float64) float64 {
	dx, dy := bx-ax, by-ay
	t := ((px-ax)*dx + (py-ay)*dy) / (dx*dx + dy*dy)
	t = math.Max(0, math.Min(1, t))
	return math.Hypot(px-(ax+t*dx), py-(ay+t*dy))
}
