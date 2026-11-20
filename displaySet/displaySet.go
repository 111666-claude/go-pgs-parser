package displaySet

import (
	"errors"
	"fmt"
	"github.com/mbiamont/go-pgs-parser/buffer"
	"github.com/mbiamont/go-pgs-parser/segment"
	"image"
	"image/color"
	"math"
	"os"
	"time"
)

// ErrRleCorrupt is returned by DecodeRleBitmap when the encoded object data
// cannot be decoded into the declared object dimensions.
var ErrRleCorrupt = errors.New("corrupt RLE object data")

type ImageData struct {
	Image  image.Image
	Width  int
	Height int
}

type DisplaySet interface {
	paletteDefinitionSegment(paletteId int) (*segment.PaletteDefinitionSegment, error)

	parseImageData() (*ImageData, error)

	rleDecode(encodedBuffer buffer.BufferAdapter, callback func(int, int, int)) error

	ycrcbToRgba(palette segment.PaletteEntry) color.RGBA

	clamp(number float64, min int, max int) int

	objectData() []buffer.BufferAdapter

	paletteEntriesToRgba(entries []segment.PaletteEntry) []color.RGBA

	ToImageData() (*ImageData, error)

	StartTime() time.Duration

	// Presentation returns the presentation composition segment (PCS) of this
	// display set, including every referenced composition object.
	Presentation() segment.PresentationCompositionSegment

	// Palettes returns every palette definition segment (PDS) carried by this
	// display set.
	Palettes() []segment.PaletteDefinitionSegment

	// Objects returns every object definition segment (ODS) carried by this
	// display set, in stream order.
	Objects() []segment.ObjectDefinitionSegment
}

type displaySet struct {
	PresentationCompositionSegment segment.PresentationCompositionSegment
	WindowDefinitionSegments       []segment.WindowDefinitionSegment
	PaletteDefinitionSegments      []segment.PaletteDefinitionSegment
	ObjectDefinitionSegments       []segment.ObjectDefinitionSegment
	EndDefinitionSegment           segment.Segment

	PreviousDisplaySet *DisplaySet
}

func NewDisplaySet(
	presentationCompositionSegment segment.PresentationCompositionSegment,
	windowDefinitionSegments []segment.WindowDefinitionSegment,
	paletteDefinitionSegments []segment.PaletteDefinitionSegment,
	objectDefinitionSegments []segment.ObjectDefinitionSegment,
	endDefinitionSegment segment.Segment,
	previousDisplaySet *DisplaySet) DisplaySet {
	return &displaySet{
		PresentationCompositionSegment: presentationCompositionSegment,
		WindowDefinitionSegments:       windowDefinitionSegments,
		PaletteDefinitionSegments:      paletteDefinitionSegments,
		ObjectDefinitionSegments:       objectDefinitionSegments,
		EndDefinitionSegment:           endDefinitionSegment,
		PreviousDisplaySet:             previousDisplaySet,
	}
}

func (d *displaySet) Presentation() segment.PresentationCompositionSegment {
	return d.PresentationCompositionSegment
}

func (d *displaySet) Palettes() []segment.PaletteDefinitionSegment {
	return d.PaletteDefinitionSegments
}

func (d *displaySet) Objects() []segment.ObjectDefinitionSegment {
	return d.ObjectDefinitionSegments
}

func (d *displaySet) ToImageData() (*ImageData, error) {
	if len(d.ObjectDefinitionSegments) <= 0 {
		//No image found
		return nil, nil
	}
	return d.parseImageData()
}

func (d *displaySet) firstOds() *segment.ObjectDefinitionSegment {
	for _, ods := range d.ObjectDefinitionSegments {
		if ods.LastInSequenceFlag == segment.LastInSequenceFlagFirstInSequence || ods.LastInSequenceFlag == segment.LastInSequenceFlagFirstAndLastInSequence {
			return &ods
		}
	}
	return nil
}

func (d *displaySet) paletteDefinitionSegment(paletteId int) (*segment.PaletteDefinitionSegment, error) {
	for _, pds := range d.PaletteDefinitionSegments {
		if pds.PaletteId == paletteId {
			return &pds, nil
		}
	}

	if d.PresentationCompositionSegment.CompositionState != segment.CompositionStateNormal {
		return nil, errors.New("PCS references invalid PDS and composition state is not 'normal'")
	}

	if d.PreviousDisplaySet != nil {
		return (*d.PreviousDisplaySet).paletteDefinitionSegment(paletteId)
	}

	return nil, errors.New("PCS references invalid PDS and no previous display set to fallback to")
}

func (d *displaySet) parseImageData() (*ImageData, error) {
	pds, err := d.paletteDefinitionSegment(d.PresentationCompositionSegment.PaletteId)

	if err != nil {
		return nil, err
	}

	if pds == nil {
		return nil, errors.New("PCS references invalid PDS")
	}

	firstOds := d.firstOds()

	if firstOds == nil || firstOds.Width == nil || firstOds.Height == nil {
		return nil, errors.New("missing first ODS with defined width and height")
	}
	rgbaPalette := d.paletteEntriesToRgba(pds.PaletteEntries)
	width := *firstOds.Width
	height := *firstOds.Height

	// New

	upLeft := image.Pt(0, 0)
	lowRight := image.Pt(width, height)
	img := image.NewRGBA(image.Rectangle{Min: upLeft, Max: lowRight})
	err = d.rleDecode(buffer.NewCompositeBuffer(d.objectData()), func(x int, y int, paletteIndex int) {

		if paletteIndex >= len(rgbaPalette) {
			img.Set(0, 0, color.RGBA{R: 0, G: 0, B: 0, A: 255})
		} else {
			rgba := rgbaPalette[paletteIndex]
			img.Set(x, y, rgba)
		}
	})

	if err != nil {
		return nil, err
	}

	return &ImageData{
		Image:  img,
		Width:  width,
		Height: height,
	}, nil
}

func (d *displaySet) StartTime() time.Duration {
	return d.EndDefinitionSegment.Header.StartTime
}

// rleScan is the shared PGS run-length decoding core. When validate is true it
// enforces expectedWidth/expectedHeight and turns truncated or inconsistent
// data into an ErrRleCorrupt error; otherwise it preserves the historical
// lenient behavior used by the per-object export path.
func rleScan(encodedBuffer buffer.BufferAdapter, expectedWidth int, expectedHeight int, validate bool, callback func(x int, y int, paletteIndex int) error) error {
	encodedIndex := 0
	decodedLineIndex := 0
	currentLine := 0
	encodedLength := encodedBuffer.Length()

	if validate && (expectedWidth <= 0 || expectedHeight <= 0) {
		return fmt.Errorf("%w: invalid object dimensions %dx%d", ErrRleCorrupt, expectedWidth, expectedHeight)
	}

	for encodedIndex < encodedLength {
		firstByte, err := encodedBuffer.At(encodedIndex)

		if err != nil {
			if validate {
				return fmt.Errorf("%w: truncated run at byte %d", ErrRleCorrupt, encodedIndex)
			}
			return err
		}

		var runLength int
		var colorB int
		var increment int

		// Deal with each possible code
		if firstByte > 0 {
			// CCCCCCCC	- One pixel in color C
			colorB = firstByte
			runLength = 1
			increment = 1
		} else {
			secondByte, err := encodedBuffer.At(encodedIndex + 1)

			if err != nil {
				if validate {
					return fmt.Errorf("%w: truncated escape sequence at byte %d", ErrRleCorrupt, encodedIndex)
				}
				return err
			}

			if secondByte == 0 {
				// 00000000 00000000 - End of line. Real encoders may end a line
				// before the object width: the untouched trailing pixels stay at
				// their zero (transparent) value. Overflowing the width, however,
				// is genuine corruption.
				if validate {
					if currentLine >= expectedHeight {
						return fmt.Errorf("%w: more than %d decoded lines", ErrRleCorrupt, expectedHeight)
					}
					if decodedLineIndex > expectedWidth {
						return fmt.Errorf("%w: line %d has %d pixels, expected at most %d", ErrRleCorrupt, currentLine, decodedLineIndex, expectedWidth)
					}
				}
				colorB = 0
				runLength = 0
				increment = 2
				decodedLineIndex = 0
				currentLine++
			} else if secondByte < 64 {
				// 00000000 00LLLLLL - L pixels in color 0 (L between 1 and 63)
				colorB = 0
				runLength = secondByte
				increment = 2
			} else if secondByte < 128 {
				// 00000000 01LLLLLL LLLLLLLL - L pixels in color 0 (L between 64 and 16383)
				thirdByte, err := encodedBuffer.At(encodedIndex + 2)

				if err != nil {
					if validate {
						return fmt.Errorf("%w: truncated long zero run at byte %d", ErrRleCorrupt, encodedIndex)
					}
					return err
				}

				colorB = 0
				runLength = ((secondByte - 64) << 8) + thirdByte
				increment = 3
			} else if secondByte < 192 {
				// 00000000 10LLLLLL CCCCCCCC - L pixels in color C (L between 3 and 63)
				thirdByte, err := encodedBuffer.At(encodedIndex + 2)

				if err != nil {
					if validate {
						return fmt.Errorf("%w: truncated color run at byte %d", ErrRleCorrupt, encodedIndex)
					}
					return err
				}

				colorB = thirdByte
				runLength = secondByte - 128
				increment = 3
			} else {
				// 00000000 11LLLLLL LLLLLLLL CCCCCCCC - L pixels in color C (L between 64 and 16383)
				thirdByte, err := encodedBuffer.At(encodedIndex + 2)

				if err != nil {
					if validate {
						return fmt.Errorf("%w: truncated long color run at byte %d", ErrRleCorrupt, encodedIndex)
					}
					return err
				}

				fourthByte, err := encodedBuffer.At(encodedIndex + 3)

				if err != nil {
					if validate {
						return fmt.Errorf("%w: truncated long color run at byte %d", ErrRleCorrupt, encodedIndex)
					}
					return err
				}

				colorB = fourthByte
				runLength = ((secondByte - 192) << 8) + thirdByte
				increment = 4
			}
		}

		if validate && runLength <= 0 && firstByte != 0 {
			return fmt.Errorf("%w: non-positive run length %d at byte %d", ErrRleCorrupt, runLength, encodedIndex)
		}

		if runLength > 0 {
			for x := decodedLineIndex; x < decodedLineIndex+runLength; x++ {
				if err := callback(x, currentLine, colorB); err != nil {
					return err
				}
			}

			decodedLineIndex += runLength
		}

		encodedIndex += increment
	}

	if validate && currentLine != expectedHeight {
		return fmt.Errorf("%w: decoded %d lines, expected %d", ErrRleCorrupt, currentLine, expectedHeight)
	}

	return nil
}

func (d *displaySet) rleDecode(encodedBuffer buffer.BufferAdapter, callback func(int, int, int)) error {
	return rleScan(encodedBuffer, 0, 0, false, func(x int, y int, paletteIndex int) error {
		callback(x, y, paletteIndex)
		return nil
	})
}

// DecodeRleBitmap strictly decodes a single object's RLE data into a row-major
// slice of palette indices. It returns ErrRleCorrupt (wrapped) when the data is
// truncated, a run escapes the declared dimensions, or the number/size of
// decoded lines does not match width x height.
func DecodeRleBitmap(encodedBuffer buffer.BufferAdapter, width int, height int) ([]uint8, error) {
	if width <= 0 || height <= 0 {
		return nil, fmt.Errorf("%w: invalid object dimensions %dx%d", ErrRleCorrupt, width, height)
	}

	indices := make([]uint8, width*height)

	err := rleScan(encodedBuffer, width, height, true, func(x int, y int, paletteIndex int) error {
		if x < 0 || x >= width || y < 0 || y >= height {
			return fmt.Errorf("%w: pixel (%d,%d) outside %dx%d object", ErrRleCorrupt, x, y, width, height)
		}

		indices[y*width+x] = uint8(paletteIndex)
		return nil
	})

	if err != nil {
		return nil, err
	}

	return indices, nil
}

func clamp(number float64, min int, max int) int {
	return int(math.Max(float64(min), math.Min(float64(max), number)))
}

// YcrcbToRgba converts a PGS YCbCr palette entry into a straight-alpha RGBA
// color using the BT.601 conversion defined by the PGS specification.
func YcrcbToRgba(palette segment.PaletteEntry) color.RGBA {
	y := float64(palette.Luminance)
	cb := float64(palette.ColorDifferenceBlue)
	cr := float64(palette.ColorDifferenceRed)

	r := clamp(math.Floor(y+1.4075*(cr-128)), 0, 255)
	g := clamp(math.Floor(y-0.3455*(cb-128)-0.7169*(cr-128)), 0, 255)
	b := clamp(math.Floor(y+1.779*(cb-128)), 0, 255)

	return color.RGBA{
		R: uint8(r),
		G: uint8(g),
		B: uint8(b),
		A: uint8(palette.Transparency),
	}
}

func (d *displaySet) ycrcbToRgba(palette segment.PaletteEntry) color.RGBA {
	return YcrcbToRgba(palette)
}

func (d *displaySet) clamp(number float64, min int, max int) int {
	return clamp(number, min, max)
}

func (d *displaySet) objectData() []buffer.BufferAdapter {
	var objectData []buffer.BufferAdapter

	for _, ods := range d.ObjectDefinitionSegments {
		objectData = append(objectData, ods.ObjectData)
	}

	return objectData
}

func (d *displaySet) paletteEntriesToRgba(entries []segment.PaletteEntry) []color.RGBA {
	var rgbas []color.RGBA

	for _, palette := range entries {
		rgbas = append(rgbas, d.ycrcbToRgba(palette))
	}

	return rgbas
}

// ReadDisplaySets parses a SUP byte stream and invokes onDisplaySet for every
// complete display set in stream order.
func ReadDisplaySets(file []byte, onDisplaySet func(data DisplaySet, startTime time.Duration) error) error {
	parser := NewDisplaySetParser()
	accumulatedBuffer := buffer.NewCompositeBufferReader()

	accumulatedBuffer.Add(file)

	// Drive the parser through its documented Consume contract:
	//   - a 13-byte segment header returns the payload size that follows
	//     (possibly zero, e.g. an END segment);
	//   - consuming a payload returns 13, the size of the next header;
	//   - a zero-payload segment is finalized by feeding an empty buffer.
	requestedBytes := 13

	for {
		if requestedBytes == 0 {
			// Finalize a zero-payload segment even when no bytes remain, so a
			// display set whose last segment is an END still becomes ready.
			var err error
			requestedBytes, err = parser.Consume(buffer.NewCompositeBuffer(nil))

			if err != nil {
				return err
			}

			if parser.IsReady() {
				if err = emitReady(parser, onDisplaySet); err != nil {
					return err
				}
			}

			// No bytes left to satisfy the next header request: the stream is
			// fully consumed.
			if accumulatedBuffer.Length() == 0 {
				return nil
			}
			continue
		}

		if accumulatedBuffer.Length() < requestedBytes {
			// A trailing partial header or payload means the stream is
			// truncated; everything complete has already been emitted.
			return nil
		}

		chunk, readErr := accumulatedBuffer.ReadBytes(requestedBytes)

		if readErr != nil {
			return readErr
		}

		var err error
		requestedBytes, err = parser.Consume(chunk)

		if err != nil {
			return err
		}

		if parser.IsReady() {
			if err = emitReady(parser, onDisplaySet); err != nil {
				return err
			}
		}
	}
}

func emitReady(parser DisplaySetParser, onDisplaySet func(data DisplaySet, startTime time.Duration) error) error {
	ds := parser.Next()
	if ds == nil {
		return nil
	}
	set := *ds
	return onDisplaySet(set, set.StartTime())
}

// ReadDisplaySetsFromFile parses a SUP file and invokes onDisplaySet for every
// complete display set in stream order.
func ReadDisplaySetsFromFile(inputFilePath string, onDisplaySet func(data DisplaySet, startTime time.Duration) error) error {
	file, err := os.ReadFile(inputFilePath)

	if err != nil {
		return err
	}

	return ReadDisplaySets(file, onDisplaySet)
}
