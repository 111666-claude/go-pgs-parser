package composition

import (
	"fmt"
	"github.com/mbiamont/go-pgs-parser/buffer"
	"github.com/mbiamont/go-pgs-parser/displaySet"
	"github.com/mbiamont/go-pgs-parser/segment"
	"image"
	"image/color"
	"time"
)

// DefaultMaxFrameDuration is applied when Options.MaxFrameDuration is left at
// its zero value: a composition longer than ten seconds with no intervening
// display set is reported through an ErrLongGap issue.
const DefaultMaxFrameDuration = 10 * time.Second

// paletteState is the active palette for one palette id: the RGBA lookup plus
// an explicit definition bitmap, since a legitimately-defined transparent
// black entry is otherwise indistinguishable from an untouched slot.
type paletteState struct {
	colors  [256]color.RGBA
	defined [256]bool
}

// decodedObject is a fully RLE-decoded object kept in its own coordinates:
// Indices[y*Width+x] is a palette index.
type decodedObject struct {
	width   int
	height  int
	indices []uint8
}

type composer struct {
	// Active palettes, keyed by palette id.
	palettes map[int]*paletteState

	// Objects known to the current epoch, keyed by object id.
	objects map[int]decodedObject

	// Current composition: object ids in PCS order with placement info.
	composition []segment.CompositionObject

	// Canvas dimensions from the current PCS.
	canvasWidth  int
	canvasHeight int

	// The frame waiting for the next display set to close it.
	pending     *Frame
	frames      []Frame
	issues      []Issue
	lastPTS     time.Duration
	haveLastPTS bool
	maxDuration time.Duration
	dropOpenEnd bool
}

func (c *composer) ComposeBytes(sup []byte, opts *Options) ([]Frame, error) {
	var sets []displaySet.DisplaySet

	err := displaySet.ReadDisplaySets(sup, func(set displaySet.DisplaySet, startTime time.Duration) error {
		sets = append(sets, set)
		return nil
	})

	if err != nil {
		return nil, err
	}

	return c.ComposeDisplaySets(sets, opts)
}

func (c *composer) ComposeDisplaySets(sets []displaySet.DisplaySet, opts *Options) ([]Frame, error) {
	c.reset()

	if opts != nil {
		c.maxDuration = opts.MaxFrameDuration
		c.dropOpenEnd = opts.DropOpenEnded
	}

	if c.maxDuration == 0 {
		c.maxDuration = DefaultMaxFrameDuration
	}

	for _, set := range sets {
		if err := c.consume(set); err != nil {
			return nil, err
		}
	}

	if c.pending != nil {
		// No following display set exists: the real end time is unknown, so
		// the frame is inherently open-ended. DropOpenEnded omits it for
		// callers that only want fully-bounded intervals; otherwise it is
		// always retained.
		if !c.dropOpenEnd {
			c.pending.OpenEnded = true
			c.pending.End = c.pending.Start
			c.frames = append(c.frames, *c.pending)
		}
		c.pending = nil
	}

	return c.frames, nil
}

func (c *composer) reset() {
	c.palettes = make(map[int]*paletteState)
	c.objects = make(map[int]decodedObject)
	c.composition = nil
	c.canvasWidth = 0
	c.canvasHeight = 0
	c.pending = nil
	c.frames = nil
	c.issues = nil
	c.lastPTS = 0
	c.haveLastPTS = false
}

func (c *composer) consume(set displaySet.DisplaySet) error {
	pts := set.StartTime()
	pcs := set.Presentation()

	// A new display set closes the frame that was on screen until now.
	if c.pending != nil {
		c.pending.End = pts
		if c.haveLastPTS && pts <= c.lastPTS {
			c.pending.Issues = append(c.pending.Issues,
				newIssue(IssueKindDuplicateTimestamp, 0,
					fmt.Errorf("%w: pts=%s after pts=%s", ErrDuplicateTimestamp, pts, c.lastPTS)))
		}
		if c.maxDuration > 0 && c.pending.End-c.pending.Start > c.maxDuration {
			c.pending.Issues = append(c.pending.Issues,
				newIssue(IssueKindLongGap, 0,
					fmt.Errorf("%w: frame lasted %s, limit %s", ErrLongGap, c.pending.End-c.pending.Start, c.maxDuration)))
		}
		c.frames = append(c.frames, *c.pending)
		c.pending = nil
	}

	c.issues = nil

	// Epoch start and acquisition points redefine the whole composition; an
	// epoch additionally discards every cached object and palette.
	if pcs.CompositionState == segment.CompositionStateEpochStart {
		c.palettes = make(map[int]*paletteState)
		c.objects = make(map[int]decodedObject)
		c.composition = nil
	} else if pcs.CompositionState == segment.CompositionStateAcquisitionState {
		c.composition = nil
	}

	// Palettes apply before rendering; PDS entries overwrite matching ids and
	// may arrive on palette-update display sets without any ODS.
	for _, pds := range set.Palettes() {
		c.applyPalette(pds)
	}

	// Objects are decoded into the cache even when this set only refines an
	// existing epoch. Decode failures become frame issues, never hard errors.
	c.consumeObjects(set.Objects())

	// A normal PCS either merges new composition objects into the current
	// composition or, when it carries no objects, explicitly clears the
	// screen. A palette-update-only PCS (flag set, no objects) leaves the
	// existing composition untouched while the PDS above recolors it.
	if pcs.CompositionState == segment.CompositionStateNormal {
		if pcs.CompositionObjectCount == 0 && !pcs.PaletteUpdateFlag {
			c.composition = nil
		} else if pcs.CompositionObjectCount > 0 {
			c.composition = append(append([]segment.CompositionObject{}, c.composition...), pcs.CompositionObjects...)
		}
	} else {
		c.composition = append([]segment.CompositionObject{}, pcs.CompositionObjects...)
	}

	if pcs.Width > 0 && pcs.Height > 0 {
		c.canvasWidth = pcs.Width
		c.canvasHeight = pcs.Height
	}

	frame := c.render(pts, pcs.PaletteId)
	frame.Issues = append(frame.Issues, c.issues...)
	c.issues = nil

	c.pending = &frame
	c.lastPTS = pts
	c.haveLastPTS = true

	return nil
}

func (c *composer) applyPalette(pds segment.PaletteDefinitionSegment) {
	palette, ok := c.palettes[pds.PaletteId]
	if !ok {
		palette = &paletteState{}
		c.palettes[pds.PaletteId] = palette
	}

	// PDS segments may be partial updates: defined entries overwrite, others
	// stay untouched.
	for _, entry := range pds.PaletteEntries {
		if entry.PaletteEntryId < 0 || entry.PaletteEntryId > 255 {
			continue
		}
		palette.colors[entry.PaletteEntryId] = displaySet.YcrcbToRgba(entry)
		palette.defined[entry.PaletteEntryId] = true
	}
}

func (c *composer) consumeObjects(odsList []segment.ObjectDefinitionSegment) {
	// Group contiguous segments belonging to the same object id. Split
	// objects arrive as a first segment (dimensions + initial data) followed
	// by last-in-sequence continuation segments carrying only data.
	type group struct {
		id       int
		segments []segment.ObjectDefinitionSegment
	}

	var groups []group
	indexByID := make(map[int]int)

	for _, ods := range odsList {
		if idx, ok := indexByID[ods.ObjectId]; ok {
			groups[idx].segments = append(groups[idx].segments, ods)
		} else {
			indexByID[ods.ObjectId] = len(groups)
			groups = append(groups, group{id: ods.ObjectId, segments: []segment.ObjectDefinitionSegment{ods}})
		}
	}

	for _, g := range groups {
		first := g.segments[0]
		if first.Width == nil || first.Height == nil {
			c.issues = append(c.issues, newIssue(IssueKindMalformedObject, g.id,
				fmt.Errorf("%w: object %d has no dimension header", ErrMalformedObject, g.id)))
			continue
		}

		var chunks []buffer.BufferAdapter
		for _, seg := range g.segments {
			if seg.ObjectData != nil && seg.ObjectData.Length() > 0 {
				chunks = append(chunks, seg.ObjectData)
			}
		}

		indices, err := displaySet.DecodeRleBitmap(buffer.NewCompositeBuffer(chunks), *first.Width, *first.Height)
		if err != nil {
			c.issues = append(c.issues, newIssue(IssueKindRleCorrupt, g.id,
				fmt.Errorf("%w: object %d: %v", ErrRleCorrupt, g.id, err)))
			continue
		}

		c.objects[g.id] = decodedObject{
			width:   *first.Width,
			height:  *first.Height,
			indices: indices,
		}
	}
}

func (c *composer) render(pts time.Duration, paletteID int) Frame {
	width, height := c.canvasWidth, c.canvasHeight
	if width <= 0 || height <= 0 {
		width, height = 0, 0
	}

	img := image.NewRGBA(image.Rect(0, 0, width, height))
	frame := Frame{
		Start: pts,
		End:   pts,
		Image: img,
		Empty: len(c.composition) == 0,
	}

	palette, havePalette := c.palettes[paletteID]
	missingPaletteLogged := false
	missingIndexLogged := make(map[int]bool)

	for _, comp := range c.composition {
		obj, ok := c.objects[comp.ObjectId]
		if !ok {
			frame.Issues = append(frame.Issues, newIssue(IssueKindMissingObject, comp.ObjectId,
				fmt.Errorf("%w: object %d", ErrMissingObject, comp.ObjectId)))
			continue
		}

		srcX0, srcY0, srcW, srcH := 0, 0, obj.width, obj.height

		if comp.ObjectCroppedFlag {
			cx0 := comp.ObjectCroppingHorizontalPosition
			cy0 := comp.ObjectCroppingVerticalPosition
			cx1 := cx0 + comp.ObjectCroppingWidth
			cy1 := cy0 + comp.ObjectCroppingHeight

			if cx0 < 0 || cy0 < 0 || cx1 > obj.width || cy1 > obj.height ||
				comp.ObjectCroppingWidth <= 0 || comp.ObjectCroppingHeight <= 0 {
				frame.Issues = append(frame.Issues, newIssue(IssueKindCropOutOfBounds, comp.ObjectId,
					fmt.Errorf("%w: object %d crop (%d,%d)-(%d,%d) outside %dx%d",
						ErrCropOutOfBounds, comp.ObjectId, cx0, cy0, cx1, cy1, obj.width, obj.height)))
			}

			// Clip the crop rectangle to the bitmap; an empty intersection
			// means nothing is drawn.
			clippedX0 := maxInt(cx0, 0)
			clippedY0 := maxInt(cy0, 0)
			clippedX1 := minInt(cx1, obj.width)
			clippedY1 := minInt(cy1, obj.height)

			if clippedX1 <= clippedX0 || clippedY1 <= clippedY0 {
				continue
			}

			srcX0, srcY0 = clippedX0, clippedY0
			srcW, srcH = clippedX1-clippedX0, clippedY1-clippedY0
		}

		baseX := comp.ObjectHorizontalPosition
		baseY := comp.ObjectVerticalPosition

		for sy := 0; sy < srcH; sy++ {
			dstY := baseY + srcY0 + sy
			if dstY < 0 || dstY >= height {
				continue
			}
			for sx := 0; sx < srcW; sx++ {
				dstX := baseX + srcX0 + sx
				if dstX < 0 || dstX >= width {
					continue
				}

				idx := int(obj.indices[(srcY0+sy)*obj.width+(srcX0+sx)])

				// PGS treats palette indices 0 and 255 as implicit
				// transparency: 0 is the conventional background entry and 255
				// an object-level cutout marker. Partial palettes may omit
				// either, so they render transparent without being flagged as
				// missing entries.
				if idx == 0 || idx == 255 {
					continue
				}

				if !havePalette {
					if !missingPaletteLogged {
						frame.Issues = append(frame.Issues, newIssue(IssueKindPaletteEntryMissing, comp.ObjectId,
							fmt.Errorf("%w: palette %d is not defined", ErrPaletteEntryMissing, paletteID)))
						missingPaletteLogged = true
					}
					continue
				}

				if !palette.defined[idx] {
					if !missingIndexLogged[idx] {
						frame.Issues = append(frame.Issues, newIssue(IssueKindPaletteEntryMissing, comp.ObjectId,
							fmt.Errorf("%w: index %d", ErrPaletteEntryMissing, idx)))
						missingIndexLogged[idx] = true
					}
					continue
				}

				col := palette.colors[idx]
				if col.A == 0 {
					// Fully transparent palette entry (typical index 0):
					// nothing to blend.
					continue
				}

				blendOver(img, dstX, dstY, col)
			}
		}
	}

	return frame
}

// blendOver paints src over the existing canvas pixel using straight-alpha
// source-over compositing.
func blendOver(img *image.RGBA, x int, y int, src color.RGBA) {
	off := img.PixOffset(x, y)
	dst := color.RGBA{R: img.Pix[off+0], G: img.Pix[off+1], B: img.Pix[off+2], A: img.Pix[off+3]}

	if src.A == 255 || dst.A == 0 {
		img.Pix[off+0] = src.R
		img.Pix[off+1] = src.G
		img.Pix[off+2] = src.B
		img.Pix[off+3] = src.A
		return
	}

	srcA := int(src.A)
	dstA := int(dst.A)
	outA := srcA + dstA*(255-srcA)/255
	if outA == 0 {
		return
	}

	img.Pix[off+0] = uint8((int(src.R)*srcA + int(dst.R)*dstA*(255-srcA)/255) / outA)
	img.Pix[off+1] = uint8((int(src.G)*srcA + int(dst.G)*dstA*(255-srcA)/255) / outA)
	img.Pix[off+2] = uint8((int(src.B)*srcA + int(dst.B)*dstA*(255-srcA)/255) / outA)
	img.Pix[off+3] = uint8(outA)
}

func minInt(a int, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a int, b int) int {
	if a > b {
		return a
	}
	return b
}
