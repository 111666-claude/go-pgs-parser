package pgs_test

import (
	"errors"
	"github.com/mbiamont/go-pgs-parser/composition"
	"github.com/mbiamont/go-pgs-parser/internal/testsup"
	"github.com/mbiamont/go-pgs-parser/pgs"
	"github.com/mbiamont/go-pgs-parser/segment"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeSup(t *testing.T, sup []byte) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "input.sup")
	if err := os.WriteFile(path, sup, 0o600); err != nil {
		t.Fatalf("write sup: %v", err)
	}
	return path
}

func timelineSup() []byte {
	const w, h = 16, 8
	return testsup.Build([]testsup.DisplaySetSpec{
		{
			PTS:       testsup.Millis(100),
			State:     segment.CompositionStateEpochStart,
			PaletteID: 0,
			Width:     w,
			Height:    h,
			Palettes: []testsup.PaletteSpec{
				{ID: 0, Y: 0, Cr: 128, Cb: 128, Transparency: 0},
				{ID: 1, Y: 82, Cr: 240, Cb: 90, Transparency: 255},
			},
			Objects: []testsup.ObjectSpec{
				{ID: 1, Width: 4, Height: 2, Grid: testsup.SolidGrid(4, 2, 1)},
			},
			Composition: []testsup.CompositionObjectSpec{{ObjectID: 1, X: 1, Y: 1}},
		},
		{
			PTS:       testsup.Millis(600),
			State:     segment.CompositionStateNormal,
			PaletteID: 0,
			Width:     w,
			Height:    h,
		},
	})
}

func TestComposeTimelineFromFile(t *testing.T) {
	path := writeSup(t, timelineSup())
	frames, err := pgs.NewPgsParser().ComposeTimeline(path, nil)
	if err != nil {
		t.Fatalf("ComposeTimeline: %v", err)
	}

	if len(frames) != 2 {
		t.Fatalf("got %d frames, want 2", len(frames))
	}

	if frames[0].Start != 100*time.Millisecond || frames[0].End != 600*time.Millisecond {
		t.Errorf("first interval wrong: %s -> %s", frames[0].Start, frames[0].End)
	}
	if frames[0].Empty {
		t.Errorf("first frame should contain an object")
	}
	if frames[0].Image.Bounds().Dx() != 16 || frames[0].Image.Bounds().Dy() != 8 {
		t.Errorf("canvas size wrong: %v", frames[0].Image.Bounds())
	}
	if !frames[1].Empty {
		t.Errorf("second frame should be a clear")
	}
	if !frames[1].OpenEnded {
		t.Errorf("final frame should be open-ended")
	}
}

func TestComposeTimelineMissingFile(t *testing.T) {
	_, err := pgs.NewPgsParser().ComposeTimeline(filepath.Join(t.TempDir(), "nope.sup"), nil)
	if err == nil {
		t.Fatal("expected an error for a missing file")
	}
}

func TestComposeTimelineReportsFrameIssues(t *testing.T) {
	// Epoch references an object that is never defined: parsing still
	// succeeds, the condition lands on the frame's issues.
	sup := testsup.Build([]testsup.DisplaySetSpec{{
		PTS:       testsup.Millis(0),
		State:     segment.CompositionStateEpochStart,
		PaletteID: 0,
		Width:     8,
		Height:    8,
		Palettes:  []testsup.PaletteSpec{{ID: 0, Y: 0, Cr: 128, Cb: 128, Transparency: 0}},
		Composition: []testsup.CompositionObjectSpec{
			{ObjectID: 3, X: 0, Y: 0},
		},
	}})

	frames, err := pgs.NewPgsParser().ComposeTimeline(writeSup(t, sup), nil)
	if err != nil {
		t.Fatalf("ComposeTimeline should not hard-fail: %v", err)
	}
	var found bool
	for _, issue := range frames[0].Issues {
		if errors.Is(issue, composition.ErrMissingObject) {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing-object issue not reported: %+v", frames[0].Issues)
	}
}

func TestLegacyPerObjectPngExportStillWorks(t *testing.T) {
	path := writeSup(t, timelineSup())
	dir := t.TempDir()

	var count int
	err := pgs.NewPgsParser().ConvertToPngImages(path, func(index int, startTime time.Duration) (*os.File, error) {
		count++
		return os.Create(filepath.Join(dir, "out.png"))
	})
	if err != nil {
		t.Fatalf("ConvertToPngImages: %v", err)
	}
	if count != 1 {
		t.Fatalf("exported %d per-object images, want 1 (clear set has no ODS)", count)
	}
}
