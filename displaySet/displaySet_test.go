package displaySet_test

import (
	"errors"
	"github.com/mbiamont/go-pgs-parser/buffer"
	"github.com/mbiamont/go-pgs-parser/displaySet"
	"github.com/mbiamont/go-pgs-parser/internal/testsup"
	"github.com/mbiamont/go-pgs-parser/segment"
	"image/color"
	"testing"
	"time"
)

// parseAll is a small helper returning every display set in a SUP stream.
func parseAll(t *testing.T, sup []byte) []displaySet.DisplaySet {
	t.Helper()
	var sets []displaySet.DisplaySet
	err := displaySet.ReadDisplaySets(sup, func(set displaySet.DisplaySet, st time.Duration) error {
		sets = append(sets, set)
		return nil
	})
	if err != nil {
		t.Fatalf("ReadDisplaySets: %v", err)
	}
	return sets
}

func TestParsesAllSegmentsInOrder(t *testing.T) {
	sup := testsup.Build([]testsup.DisplaySetSpec{
		{
			PTS:       testsup.Millis(100),
			State:     segment.CompositionStateEpochStart,
			PaletteID: 0,
			Width:     20,
			Height:    10,
			Windows:   []testsup.WindowSpec{{ID: 0, Width: 20, Height: 10}},
			Palettes:  []testsup.PaletteSpec{{ID: 0, Y: 0, Cr: 128, Cb: 128, Transparency: 0}, {ID: 1, Y: 82, Cr: 240, Cb: 90, Transparency: 255}},
			Objects: []testsup.ObjectSpec{
				{ID: 1, Width: 4, Height: 2, Grid: testsup.SolidGrid(4, 2, 1)},
			},
			Composition: []testsup.CompositionObjectSpec{{ObjectID: 1, X: 1, Y: 1}},
		},
		{
			PTS:       testsup.Millis(300),
			State:     segment.CompositionStateNormal,
			PaletteID: 0,
			Width:     20,
			Height:    10,
		},
	})

	sets := parseAll(t, sup)
	if len(sets) != 2 {
		t.Fatalf("got %d display sets, want 2", len(sets))
	}
	if sets[0].StartTime() != 100*time.Millisecond || sets[1].StartTime() != 300*time.Millisecond {
		t.Errorf("start times wrong: %s / %s", sets[0].StartTime(), sets[1].StartTime())
	}
}

func TestPCSCarriesEveryCompositionObject(t *testing.T) {
	sup := testsup.Build([]testsup.DisplaySetSpec{{
		PTS:       testsup.Millis(0),
		State:     segment.CompositionStateEpochStart,
		PaletteID: 0,
		Width:     40,
		Height:    20,
		Palettes:  []testsup.PaletteSpec{{ID: 0, Y: 0, Cr: 128, Cb: 128, Transparency: 0}},
		Objects: []testsup.ObjectSpec{
			{ID: 1, Width: 2, Height: 2, Grid: testsup.SolidGrid(2, 2, 0)},
			{ID: 2, Width: 3, Height: 3, Grid: testsup.SolidGrid(3, 3, 0)},
		},
		Composition: []testsup.CompositionObjectSpec{
			{ObjectID: 1, X: 2, Y: 3},
			{ObjectID: 2, X: 10, Y: 11, Cropped: true, CropX: 0, CropY: 0, CropWidth: 3, CropHeight: 3},
		},
	}})

	sets := parseAll(t, sup)
	pcs := sets[0].Presentation()

	if pcs.CompositionObjectCount != 2 {
		t.Fatalf("object count = %d, want 2", pcs.CompositionObjectCount)
	}
	if len(pcs.CompositionObjects) != 2 {
		t.Fatalf("parsed %d composition objects, want 2", len(pcs.CompositionObjects))
	}

	first := pcs.CompositionObjects[0]
	if first.ObjectId != 1 || first.ObjectHorizontalPosition != 2 || first.ObjectVerticalPosition != 3 || first.ObjectCroppedFlag {
		t.Errorf("first composition object parsed wrong: %+v", first)
	}

	second := pcs.CompositionObjects[1]
	if second.ObjectId != 2 || !second.ObjectCroppedFlag || second.ObjectHorizontalPosition != 10 {
		t.Errorf("second composition object parsed wrong: %+v", second)
	}

	// Legacy single-object fields still mirror the first entry.
	if pcs.ObjectId != 1 || pcs.ObjectHorizontalPosition != 2 {
		t.Errorf("legacy PCS fields not mirrored: %+v", pcs)
	}
}

func TestDecodeRleBitmapRoundTrip(t *testing.T) {
	width, height := 5, 3
	grid := []byte{
		0, 0, 1, 1, 1,
		2, 2, 2, 2, 0,
		1, 0, 0, 0, 0,
	}
	rle := testsup.EncodeRLE(grid, width, height)

	indices, err := displaySet.DecodeRleBitmap(buffer.NewUint8ArrayBuffer(rle), width, height)
	if err != nil {
		t.Fatalf("DecodeRleBitmap: %v", err)
	}
	for i, want := range grid {
		if int(indices[i]) != int(want) {
			t.Fatalf("index %d = %d, want %d", i, indices[i], want)
		}
	}
}

func TestDecodeRleBitmapAllowsShortLines(t *testing.T) {
	// A real-world encoder ends lines before the object width; untouched
	// trailing pixels must remain zero rather than trigger an error.
	width, height := 4, 2
	// Line 0: two color-1 pixels then EOL (two pixels short).
	// Line 1: one color-2 pixel then EOL (three pixels short).
	rle := []byte{0x01, 0x01, 0x00, 0x00, 0x02, 0x00, 0x00}

	indices, err := displaySet.DecodeRleBitmap(buffer.NewUint8ArrayBuffer(rle), width, height)
	if err != nil {
		t.Fatalf("short lines should be accepted: %v", err)
	}

	want := []byte{
		1, 1, 0, 0,
		2, 0, 0, 0,
	}
	for i, w := range want {
		if indices[i] != w {
			t.Fatalf("index %d = %d, want %d (full: %v)", i, indices[i], w, indices)
		}
	}
}

func TestDecodeRleBitmapRejectsTruncated(t *testing.T) {
	// Declares 4x4 but supplies one line then stops: the line count mismatch
	// at end of stream is still genuine corruption.
	rle := []byte{0x00, 0x02, 0x01, 0x01, 0x00, 0x00}
	_, err := displaySet.DecodeRleBitmap(buffer.NewUint8ArrayBuffer(rle), 4, 4)
	if !errors.Is(err, displaySet.ErrRleCorrupt) {
		t.Fatalf("want ErrRleCorrupt, got %v", err)
	}
}

func TestDecodeRleBitmapRejectsLineOverflow(t *testing.T) {
	// Two color-1 pixels + three color-2 pixels on a line declared width 4.
	rle := []byte{0x01, 0x01, 0x00, 0x83, 0x02, 0x00, 0x00}
	_, err := displaySet.DecodeRleBitmap(buffer.NewUint8ArrayBuffer(rle), 4, 1)
	if !errors.Is(err, displaySet.ErrRleCorrupt) {
		t.Fatalf("want ErrRleCorrupt for overflow line, got %v", err)
	}
}

func TestDecodeRleBitmapRejectsTruncatedEscape(t *testing.T) {
	// 0x00 starts an escape sequence but the stream ends immediately.
	rle := []byte{0x00}
	_, err := displaySet.DecodeRleBitmap(buffer.NewUint8ArrayBuffer(rle), 1, 1)
	if !errors.Is(err, displaySet.ErrRleCorrupt) {
		t.Fatalf("want ErrRleCorrupt for truncated escape, got %v", err)
	}
}

func TestYcrcbToRgbaConversion(t *testing.T) {
	// A neutral gray (Y=128, Cr=Cb=128) must convert to neutral gray.
	gray := displaySet.YcrcbToRgba(segment.PaletteEntry{Luminance: 128, ColorDifferenceRed: 128, ColorDifferenceBlue: 128, Transparency: 200})
	if gray.R != 128 || gray.G != 128 || gray.B != 128 || gray.A != 200 {
		t.Errorf("neutral gray conversion wrong: %+v", gray)
	}

	// Transparency is carried through verbatim.
	clear := displaySet.YcrcbToRgba(segment.PaletteEntry{Luminance: 0, ColorDifferenceRed: 128, ColorDifferenceBlue: 128, Transparency: 0})
	if clear.A != 0 {
		t.Errorf("transparency not carried: %+v", clear)
	}
}

func TestPerObjectExportStillWorks(t *testing.T) {
	// The original ToImageData path must keep producing a per-object bitmap.
	sup := testsup.Build([]testsup.DisplaySetSpec{{
		PTS:       testsup.Millis(0),
		State:     segment.CompositionStateEpochStart,
		PaletteID: 0,
		Width:     20,
		Height:    10,
		Palettes: []testsup.PaletteSpec{
			{ID: 0, Y: 0, Cr: 128, Cb: 128, Transparency: 0},
			{ID: 1, Y: 82, Cr: 240, Cb: 90, Transparency: 255},
		},
		Objects: []testsup.ObjectSpec{
			{ID: 1, Width: 4, Height: 2, Grid: testsup.SolidGrid(4, 2, 1)},
		},
		Composition: []testsup.CompositionObjectSpec{{ObjectID: 1, X: 0, Y: 0}},
	}})

	sets := parseAll(t, sup)
	img, err := sets[0].ToImageData()
	if err != nil {
		t.Fatalf("ToImageData: %v", err)
	}
	if img == nil || img.Width != 4 || img.Height != 2 {
		t.Fatalf("image data wrong: %+v", img)
	}

	want := displaySet.YcrcbToRgba(segment.PaletteEntry{Luminance: 82, ColorDifferenceRed: 240, ColorDifferenceBlue: 90, Transparency: 255})
	got := img.Image.At(0, 0)
	r, g, b, a := got.RGBA()
	gotRGBA := color.RGBA{R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(b >> 8), A: uint8(a >> 8)}
	if gotRGBA != want {
		t.Errorf("exported pixel = %v, want %v", got, want)
	}
}

func TestPDSPaletteFallsBackAcrossDisplaySets(t *testing.T) {
	// A normal display set without its own PDS resolves the palette through
	// the previous display set.
	pal := []testsup.PaletteSpec{
		{ID: 0, Y: 0, Cr: 128, Cb: 128, Transparency: 0},
		{ID: 1, Y: 82, Cr: 240, Cb: 90, Transparency: 255},
	}
	epoch := testsup.DisplaySetSpec{
		PTS:         testsup.Millis(0),
		State:       segment.CompositionStateEpochStart,
		PaletteID:   0,
		Width:       10,
		Height:      10,
		Palettes:    pal,
		Objects:     []testsup.ObjectSpec{{ID: 1, Width: 2, Height: 2, Grid: testsup.SolidGrid(2, 2, 1)}},
		Composition: []testsup.CompositionObjectSpec{{ObjectID: 1, X: 0, Y: 0}},
	}
	normal := testsup.DisplaySetSpec{
		PTS:         testsup.Millis(200),
		State:       segment.CompositionStateNormal,
		PaletteID:   0,
		Width:       10,
		Height:      10,
		Objects:     []testsup.ObjectSpec{{ID: 1, Width: 2, Height: 2, Grid: testsup.SolidGrid(2, 2, 1)}},
		Composition: []testsup.CompositionObjectSpec{{ObjectID: 1, X: 0, Y: 0}},
	}

	sets := parseAll(t, testsup.Build([]testsup.DisplaySetSpec{epoch, normal}))
	if _, err := sets[1].ToImageData(); err != nil {
		t.Fatalf("palette fallback failed: %v", err)
	}
}
