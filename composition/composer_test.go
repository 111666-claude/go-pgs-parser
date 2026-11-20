package composition_test

import (
	"errors"
	"github.com/mbiamont/go-pgs-parser/composition"
	"github.com/mbiamont/go-pgs-parser/displaySet"
	"github.com/mbiamont/go-pgs-parser/internal/testsup"
	"github.com/mbiamont/go-pgs-parser/segment"
	"image"
	"image/color"
	"testing"
	"time"
)

// Palette Y/Cr/Cb values used by the fixed SUP.
const (
	colRedY  = 82
	colRedCr = 240
	colRedCb = 90

	colBlueY  = 41
	colBlueCr = 110
	colBlueCb = 240

	colGreenY  = 145
	colGreenCr = 54
	colGreenCb = 34
)

func basePalette() []testsup.PaletteSpec {
	return []testsup.PaletteSpec{
		{ID: 0, Y: 0, Cr: 128, Cb: 128, Transparency: 0},
		{ID: 1, Y: colRedY, Cr: colRedCr, Cb: colRedCb, Transparency: 255},
		{ID: 2, Y: colBlueY, Cr: colBlueCr, Cb: colBlueCb, Transparency: 255},
	}
}

// updatedPalette replaces entry 1 (red) with green and is a partial update:
// entry 2 (blue) must remain untouched.
func updatedPalette() []testsup.PaletteSpec {
	return []testsup.PaletteSpec{
		{ID: 1, Y: colGreenY, Cr: colGreenCr, Cb: colGreenCb, Transparency: 255},
	}
}

func rgbaOf(y, cr, cb, alpha int) color.RGBA {
	return displaySet.YcrcbToRgba(segment.PaletteEntry{
		Luminance:           y,
		ColorDifferenceRed:  cr,
		ColorDifferenceBlue: cb,
		Transparency:        alpha,
	})
}

// expectedOver mirrors the composer's straight-alpha source-over blend.
func expectedOver(dst, src color.RGBA) color.RGBA {
	if src.A == 255 || dst.A == 0 {
		return src
	}
	srcA, dstA := int(src.A), int(dst.A)
	outA := srcA + dstA*(255-srcA)/255
	return color.RGBA{
		R: uint8((int(src.R)*srcA + int(dst.R)*dstA*(255-srcA)/255) / outA),
		G: uint8((int(src.G)*srcA + int(dst.G)*dstA*(255-srcA)/255) / outA),
		B: uint8((int(src.B)*srcA + int(dst.B)*dstA*(255-srcA)/255) / outA),
		A: uint8(outA),
	}
}

// buildFixedSup builds the canonical fixture:
//
//	t=100ms epoch start: two overlapping opaque objects on a 20x10 canvas
//	t=500ms normal set:   partial palette update (red -> green), same geometry
//	t=900ms clear set:    zero-object PCS
//	t=900ms clear set:    duplicate timestamp, second clear (zero-duration)
func buildFixedSup() []byte {
	const w, h = 20, 10

	redObject := testsup.ObjectSpec{
		ID:     1,
		Width:  6,
		Height: 4,
		Grid:   testsup.SolidGrid(6, 4, 1), // palette index 1
	}
	blueObject := testsup.ObjectSpec{
		ID:     2,
		Width:  6,
		Height: 4,
		Grid:   testsup.SolidGrid(6, 4, 2), // palette index 2
	}

	window := testsup.WindowSpec{ID: 0, X: 0, Y: 0, Width: w, Height: h}

	epoch := testsup.DisplaySetSpec{
		PTS:       testsup.Millis(100),
		State:     segment.CompositionStateEpochStart,
		PaletteID: 0,
		Width:     w,
		Height:    h,
		Windows:   []testsup.WindowSpec{window},
		Palettes:  basePalette(),
		Objects:   []testsup.ObjectSpec{redObject, blueObject},
		Composition: []testsup.CompositionObjectSpec{
			{ObjectID: 1, WindowID: 0, X: 2, Y: 1},
			{ObjectID: 2, WindowID: 0, X: 6, Y: 3}, // overlaps object 1 in x=6..7, y=3..4
		},
	}

	paletteUpdate := testsup.DisplaySetSpec{
		PTS:           testsup.Millis(500),
		State:         segment.CompositionStateNormal,
		PaletteUpdate: true,
		PaletteID:     0,
		Width:         w,
		Height:        h,
		Palettes:      updatedPalette(),
		Composition:   nil, // no ODS and no objects: only palette changes
	}

	clear1 := testsup.DisplaySetSpec{
		PTS:         testsup.Millis(900),
		State:       segment.CompositionStateNormal,
		PaletteID:   0,
		Width:       w,
		Height:      h,
		Composition: nil,
	}
	clear2 := clear1 // identical PTS: duplicate timestamp

	return testsup.Build([]testsup.DisplaySetSpec{epoch, paletteUpdate, clear1, clear2})
}

// fixedSupSpecs returns the four display-set specs behind the fixed SUP so
// other tests can splice additional sets into the timeline.
func fixedSupSpecs() []testsup.DisplaySetSpec {
	const w, h = 20, 10

	redObject := testsup.ObjectSpec{ID: 1, Width: 6, Height: 4, Grid: testsup.SolidGrid(6, 4, 1)}
	blueObject := testsup.ObjectSpec{ID: 2, Width: 6, Height: 4, Grid: testsup.SolidGrid(6, 4, 2)}
	window := testsup.WindowSpec{ID: 0, X: 0, Y: 0, Width: w, Height: h}

	epoch := testsup.DisplaySetSpec{
		PTS:       testsup.Millis(100),
		State:     segment.CompositionStateEpochStart,
		PaletteID: 0,
		Width:     w,
		Height:    h,
		Windows:   []testsup.WindowSpec{window},
		Palettes:  basePalette(),
		Objects:   []testsup.ObjectSpec{redObject, blueObject},
		Composition: []testsup.CompositionObjectSpec{
			{ObjectID: 1, WindowID: 0, X: 2, Y: 1},
			{ObjectID: 2, WindowID: 0, X: 6, Y: 3},
		},
	}
	paletteUpdate := testsup.DisplaySetSpec{
		PTS:           testsup.Millis(500),
		State:         segment.CompositionStateNormal,
		PaletteUpdate: true,
		PaletteID:     0,
		Width:         w,
		Height:        h,
		Palettes:      updatedPalette(),
	}
	clear1 := testsup.DisplaySetSpec{
		PTS:       testsup.Millis(900),
		State:     segment.CompositionStateNormal,
		PaletteID: 0,
		Width:     w,
		Height:    h,
	}
	return []testsup.DisplaySetSpec{epoch, paletteUpdate, clear1, clear1}
}

func parseFrames(t *testing.T, sup []byte, opts *composition.Options) []composition.Frame {
	t.Helper()
	frames, err := composition.NewComposer().ComposeBytes(sup, opts)
	if err != nil {
		t.Fatalf("ComposeBytes returned error: %v", err)
	}
	return frames
}

func assertPixel(t *testing.T, img *image.RGBA, x, y int, want color.RGBA) {
	t.Helper()
	got := img.RGBAAt(x, y)
	if got != want {
		t.Errorf("pixel (%d,%d) = %v, want %v", x, y, got, want)
	}
}

func TestFixedSupIntervalsAndClear(t *testing.T) {
	frames := parseFrames(t, buildFixedSup(), nil)

	if len(frames) != 4 {
		t.Fatalf("got %d frames, want 4 (composition, palette update, clear, duplicate clear)", len(frames))
	}

	// Intervals: each frame is closed by the next display set's PTS.
	wantStarts := []time.Duration{100 * time.Millisecond, 500 * time.Millisecond, 900 * time.Millisecond, 900 * time.Millisecond}
	wantEnds := []time.Duration{500 * time.Millisecond, 900 * time.Millisecond, 900 * time.Millisecond, 900 * time.Millisecond}
	wantEmpty := []bool{false, false, true, true}
	wantOpen := []bool{false, false, false, true}

	for i, f := range frames {
		if f.Start != wantStarts[i] {
			t.Errorf("frame %d start = %s, want %s", i, f.Start, wantStarts[i])
		}
		if f.End != wantEnds[i] {
			t.Errorf("frame %d end = %s, want %s", i, f.End, wantEnds[i])
		}
		if f.Empty != wantEmpty[i] {
			t.Errorf("frame %d empty = %v, want %v", i, f.Empty, wantEmpty[i])
		}
		if f.OpenEnded != wantOpen[i] {
			t.Errorf("frame %d openEnded = %v, want %v", i, f.OpenEnded, wantOpen[i])
		}
	}

	// The duplicate timestamp must be reported on the first clear frame.
	var dup *composition.Issue
	for i := range frames[2].Issues {
		if frames[2].Issues[i].Kind == composition.IssueKindDuplicateTimestamp {
			dup = &frames[2].Issues[i]
		}
	}
	if dup == nil {
		t.Fatalf("duplicate timestamp issue missing on frame 2: %+v", frames[2].Issues)
	}
	if !errors.Is(dup, composition.ErrDuplicateTimestamp) {
		t.Fatalf("issue does not wrap ErrDuplicateTimestamp: %v", dup.Err)
	}

	// The open-ended final frame must not carry another duplicate issue.
	for _, issue := range frames[3].Issues {
		if issue.Kind == composition.IssueKindDuplicateTimestamp {
			t.Errorf("unexpected duplicate issue on open-ended final frame")
		}
	}
}

func TestFixedSupOverlapPixels(t *testing.T) {
	frames := parseFrames(t, buildFixedSup(), nil)
	img := frames[0].Image

	if img.Bounds().Dx() != 20 || img.Bounds().Dy() != 10 {
		t.Fatalf("canvas size = %v, want 20x10", img.Bounds())
	}

	red := rgbaOf(colRedY, colRedCr, colRedCb, 255)
	blue := rgbaOf(colBlueY, colBlueCr, colBlueCb, 255)
	transparent := color.RGBA{0, 0, 0, 0}

	// Object 1 (red) occupies x=2..7, y=1..4.
	assertPixel(t, img, 2, 1, red)
	assertPixel(t, img, 5, 2, red)

	// Object 2 (blue) occupies x=6..11, y=3..6. Outside the overlap it wins.
	assertPixel(t, img, 10, 5, blue)
	assertPixel(t, img, 11, 6, blue)

	// Overlap region x=6..7, y=3..4: object 2 is later in the PCS, so it
	// paints over object 1. Both are opaque, hence plain blue.
	assertPixel(t, img, 6, 3, blue)
	assertPixel(t, img, 7, 4, blue)

	// Transparent canvas everywhere else.
	assertPixel(t, img, 0, 0, transparent)
	assertPixel(t, img, 19, 9, transparent)
	assertPixel(t, img, 8, 1, transparent)
}

func TestFixedSupPaletteUpdatePixels(t *testing.T) {
	frames := parseFrames(t, buildFixedSup(), nil)
	updated := frames[1].Image

	green := rgbaOf(colGreenY, colGreenCr, colGreenCb, 255)
	blue := rgbaOf(colBlueY, colBlueCr, colBlueCb, 255)

	// Entry 1 changed red -> green across the whole cached object.
	assertPixel(t, updated, 2, 1, green)
	assertPixel(t, updated, 5, 2, green)

	// Entry 2 was not part of the partial PDS and must remain blue.
	assertPixel(t, updated, 10, 5, blue)

	// Overlap still resolves to the later object (blue) after the update.
	assertPixel(t, updated, 6, 3, blue)
	assertPixel(t, updated, 7, 4, blue)

	// The earlier frame must be untouched by the later palette update.
	red := rgbaOf(colRedY, colRedCr, colRedCb, 255)
	assertPixel(t, frames[0].Image, 2, 1, red)
}

func TestFixedSupClearIsTransparent(t *testing.T) {
	frames := parseFrames(t, buildFixedSup(), nil)

	for _, idx := range []int{2, 3} {
		img := frames[idx].Image
		if !frames[idx].Empty {
			t.Errorf("frame %d should be marked empty", idx)
		}
		for y := 0; y < 10; y++ {
			for x := 0; x < 20; x++ {
				if img.RGBAAt(x, y) != (color.RGBA{0, 0, 0, 0}) {
					t.Fatalf("clear frame %d has non-transparent pixel at (%d,%d): %v", idx, x, y, img.RGBAAt(x, y))
				}
			}
		}
	}
}

func TestAlphaBlendingInOverlap(t *testing.T) {
	// Two semi-transparent overlapping objects: source-over must blend rather
	// than replace.
	palette := []testsup.PaletteSpec{
		{ID: 0, Y: 0, Cr: 128, Cb: 128, Transparency: 0},
		{ID: 1, Y: colRedY, Cr: colRedCr, Cb: colRedCb, Transparency: 128},
		{ID: 2, Y: colBlueY, Cr: colBlueCr, Cb: colBlueCb, Transparency: 200},
	}
	sup := testsup.Build([]testsup.DisplaySetSpec{{
		PTS:       testsup.Millis(0),
		State:     segment.CompositionStateEpochStart,
		PaletteID: 0,
		Width:     8,
		Height:    8,
		Palettes:  palette,
		Objects: []testsup.ObjectSpec{
			{ID: 1, Width: 4, Height: 4, Grid: testsup.SolidGrid(4, 4, 1)},
			{ID: 2, Width: 4, Height: 4, Grid: testsup.SolidGrid(4, 4, 2)},
		},
		Composition: []testsup.CompositionObjectSpec{
			{ObjectID: 1, X: 0, Y: 0},
			{ObjectID: 2, X: 2, Y: 2},
		},
	}})

	frames := parseFrames(t, sup, nil)
	img := frames[0].Image

	red := rgbaOf(colRedY, colRedCr, colRedCb, 128)
	blue := rgbaOf(colBlueY, colBlueCr, colBlueCb, 200)

	assertPixel(t, img, 0, 0, red)
	assertPixel(t, img, 5, 5, blue)
	assertPixel(t, img, 3, 3, expectedOver(red, blue))
}

func TestMissingObjectIssue(t *testing.T) {
	// Epoch references object 9 but never defines it.
	sup := testsup.Build([]testsup.DisplaySetSpec{{
		PTS:       testsup.Millis(0),
		State:     segment.CompositionStateEpochStart,
		PaletteID: 0,
		Width:     8,
		Height:    8,
		Palettes:  basePalette(),
		Composition: []testsup.CompositionObjectSpec{
			{ObjectID: 9, X: 0, Y: 0},
		},
	}})

	frames := parseFrames(t, sup, nil)
	if len(frames) != 1 {
		t.Fatalf("got %d frames, want 1", len(frames))
	}
	if frames[0].Empty {
		t.Errorf("frame with a (missing) composition object must not be marked empty")
	}
	if !hasIssue(frames[0].Issues, composition.IssueKindMissingObject) {
		t.Fatalf("missing-object issue not found: %+v", frames[0].Issues)
	}
	if !issueWraps(frames[0].Issues, composition.ErrMissingObject) {
		t.Fatalf("issue does not wrap ErrMissingObject")
	}

	// Nothing was drawn: canvas stays transparent.
	if frames[0].Image.RGBAAt(0, 0) != (color.RGBA{}) {
		t.Errorf("missing object should leave the canvas transparent")
	}
}

func TestCorruptRleIssue(t *testing.T) {
	specs := fixedSupSpecs()

	// Splice a normal display set at 700ms whose object 7 declares 4x4
	// dimensions but carries only one encoded line: the strict decoder must
	// reject it instead of silently accepting garbage.
	corruptSet := testsup.DisplaySetSpec{
		PTS:       testsup.Millis(700),
		State:     segment.CompositionStateNormal,
		PaletteID: 0,
		Width:     20,
		Height:    10,
		Objects: []testsup.ObjectSpec{{
			ID:     7,
			Width:  4,
			Height: 4,
			RawRLE: []byte{0x00, 0x02, 0x01, 0x01, 0x00, 0x00},
		}},
		Composition: []testsup.CompositionObjectSpec{{ObjectID: 7, X: 0, Y: 0}},
	}

	ordered := []testsup.DisplaySetSpec{
		specs[0], specs[1], corruptSet, specs[2], specs[3],
	}
	frames := parseFrames(t, testsup.Build(ordered), nil)

	var corruptFrame *composition.Frame
	for i := range frames {
		for _, issue := range frames[i].Issues {
			if issue.Kind == composition.IssueKindRleCorrupt && errors.Is(issue, composition.ErrRleCorrupt) {
				corruptFrame = &frames[i]
			}
		}
	}
	if corruptFrame == nil {
		t.Fatalf("no RLE-corrupt issue in frames: %+v", summarize(frames))
	}

	// Object 7 never entered the cache, so rendering the merged composition
	// additionally reports it as missing and draws nothing for it.
	if !hasIssue(corruptFrame.Issues, composition.IssueKindMissingObject) {
		t.Errorf("corrupt object 7 should also surface as missing: %+v", corruptFrame.Issues)
	}

	// The previously decoded objects remain part of the merged composition and
	// stay on screen (here recolored green by the 500ms palette update).
	green := rgbaOf(colGreenY, colGreenCr, colGreenCb, 255)
	assertPixel(t, corruptFrame.Image, 2, 1, green)
}

func TestCropOutOfBoundsIssue(t *testing.T) {
	sup := testsup.Build([]testsup.DisplaySetSpec{{
		PTS:       testsup.Millis(0),
		State:     segment.CompositionStateEpochStart,
		PaletteID: 0,
		Width:     12,
		Height:    12,
		Palettes:  basePalette(),
		Objects: []testsup.ObjectSpec{
			{ID: 1, Width: 4, Height: 4, Grid: testsup.SolidGrid(4, 4, 1)},
		},
		Composition: []testsup.CompositionObjectSpec{{
			ObjectID:   1,
			X:          0,
			Y:          0,
			Cropped:    true,
			CropX:      2,
			CropY:      2,
			CropWidth:  10, // exceeds the 4x4 bitmap
			CropHeight: 10,
		}},
	}})

	frames := parseFrames(t, sup, nil)
	if !hasIssue(frames[0].Issues, composition.IssueKindCropOutOfBounds) {
		t.Fatalf("crop issue missing: %+v", frames[0].Issues)
	}

	// The visible intersection (x=2..3, y=2..3 of the bitmap) is still drawn.
	red := rgbaOf(colRedY, colRedCr, colRedCb, 255)
	assertPixel(t, frames[0].Image, 2, 2, red)
	assertPixel(t, frames[0].Image, 3, 3, red)
	assertPixel(t, frames[0].Image, 4, 4, color.RGBA{})
}

func TestLongGapIssue(t *testing.T) {
	sup := testsup.Build([]testsup.DisplaySetSpec{
		{
			PTS:       testsup.Millis(0),
			State:     segment.CompositionStateEpochStart,
			PaletteID: 0,
			Width:     8,
			Height:    8,
			Palettes:  basePalette(),
			Objects: []testsup.ObjectSpec{
				{ID: 1, Width: 2, Height: 2, Grid: testsup.SolidGrid(2, 2, 1)},
			},
			Composition: []testsup.CompositionObjectSpec{{ObjectID: 1, X: 0, Y: 0}},
		},
		{
			PTS:         testsup.Millis(15000),
			State:       segment.CompositionStateNormal,
			PaletteID:   0,
			Width:       8,
			Height:      8,
			Composition: nil,
		},
	})

	opts := &composition.Options{MaxFrameDuration: 10 * time.Second}
	frames := parseFrames(t, sup, opts)

	if !hasIssue(frames[0].Issues, composition.IssueKindLongGap) {
		t.Fatalf("long-gap issue missing: %+v", frames[0].Issues)
	}
	if frames[0].End != 15*time.Second {
		t.Errorf("long frame end = %s, want 15s", frames[0].End)
	}
}

func TestEpochStartResetsState(t *testing.T) {
	// First epoch draws object 1; a later epoch start without object 1 must
	// not retain it from the previous epoch.
	first := testsup.DisplaySetSpec{
		PTS:       testsup.Millis(0),
		State:     segment.CompositionStateEpochStart,
		PaletteID: 0,
		Width:     8,
		Height:    8,
		Palettes:  basePalette(),
		Objects: []testsup.ObjectSpec{
			{ID: 1, Width: 2, Height: 2, Grid: testsup.SolidGrid(2, 2, 1)},
		},
		Composition: []testsup.CompositionObjectSpec{{ObjectID: 1, X: 0, Y: 0}},
	}
	second := testsup.DisplaySetSpec{
		PTS:       testsup.Millis(500),
		State:     segment.CompositionStateEpochStart,
		PaletteID: 0,
		Width:     8,
		Height:    8,
		Palettes:  basePalette(),
		Objects: []testsup.ObjectSpec{
			{ID: 2, Width: 2, Height: 2, Grid: testsup.SolidGrid(2, 2, 2)},
		},
		Composition: []testsup.CompositionObjectSpec{{ObjectID: 2, X: 4, Y: 4}},
	}

	frames := parseFrames(t, testsup.Build([]testsup.DisplaySetSpec{first, second}), nil)

	red := rgbaOf(colRedY, colRedCr, colRedCb, 255)
	blue := rgbaOf(colBlueY, colBlueCr, colBlueCb, 255)

	assertPixel(t, frames[1].Image, 4, 4, blue)
	assertPixel(t, frames[1].Image, 0, 0, color.RGBA{})
	assertPixel(t, frames[0].Image, 0, 0, red)
}

func TestImplicitTransparentIndices(t *testing.T) {
	// Palette defines only index 1; pixels with indices 0 and 255 must render
	// fully transparent without raising a missing-palette issue, matching the
	// PGS background (0) and cutout (255) conventions.
	sup := testsup.Build([]testsup.DisplaySetSpec{{
		PTS:       testsup.Millis(0),
		State:     segment.CompositionStateEpochStart,
		PaletteID: 0,
		Width:     8,
		Height:    4,
		Palettes: []testsup.PaletteSpec{
			{ID: 1, Y: colRedY, Cr: colRedCr, Cb: colRedCb, Transparency: 255},
		},
		Objects: []testsup.ObjectSpec{{
			ID:     1,
			Width:  3,
			Height: 1,
			// index 1 (defined red), index 0 (background), index 255 (cutout).
			Grid: []byte{1, 0, 255},
		}},
		Composition: []testsup.CompositionObjectSpec{{ObjectID: 1, X: 0, Y: 0}},
	}})

	frames := parseFrames(t, sup, nil)
	img := frames[0].Image

	red := rgbaOf(colRedY, colRedCr, colRedCb, 255)
	assertPixel(t, img, 0, 0, red)
	assertPixel(t, img, 1, 0, color.RGBA{0, 0, 0, 0})
	assertPixel(t, img, 2, 0, color.RGBA{0, 0, 0, 0})

	if hasIssue(frames[0].Issues, composition.IssueKindPaletteEntryMissing) {
		t.Fatalf("implicit transparent indices must not be flagged: %+v", frames[0].Issues)
	}
}

func TestDropOpenEndedOption(t *testing.T) {
	sup := buildFixedSup()

	kept := parseFrames(t, sup, nil)
	dropped := parseFrames(t, sup, &composition.Options{DropOpenEnded: true})

	if len(kept) != len(dropped)+1 {
		t.Fatalf("dropped open frame: kept %d frames, dropped %d", len(kept), len(dropped))
	}

	lastKept := kept[len(kept)-1]
	if !lastKept.OpenEnded {
		t.Fatalf("default final frame should be open-ended")
	}

	for _, f := range dropped {
		if f.OpenEnded {
			t.Fatalf("no open-ended frame should survive DropOpenEnded")
		}
	}
}

func hasIssue(issues []composition.Issue, kind composition.IssueKind) bool {
	for _, issue := range issues {
		if issue.Kind == kind {
			return true
		}
	}
	return false
}

func issueWraps(issues []composition.Issue, target error) bool {
	for _, issue := range issues {
		if errors.Is(issue, target) {
			return true
		}
	}
	return false
}

func summarize(frames []composition.Frame) []composition.Issue {
	var all []composition.Issue
	for _, f := range frames {
		all = append(all, f.Issues...)
	}
	return all
}
