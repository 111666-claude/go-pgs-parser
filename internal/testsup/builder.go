// Package testsup builds small, fully-specified PGS/SUP byte streams for the
// parser and composer tests. It is an internal test helper, not part of the
// public API.
package testsup

import (
	"encoding/binary"
	"github.com/mbiamont/go-pgs-parser/segment"
)

const (
	segPDS = 0x14
	segODS = 0x15
	segPCS = 0x16
	segWDS = 0x17
	segEND = 0x80
)

// PaletteSpec describes one PDS palette entry in YCbCr.
type PaletteSpec struct {
	ID           byte
	Y, Cr, Cb    byte
	Transparency byte
}

// WindowSpec describes one WDS window.
type WindowSpec struct {
	ID            byte
	X, Y          int
	Width, Height int
}

// ObjectSpec describes one ODS object. Grid holds palette indices in
// row-major order; the RLE stream is encoded from it. When RawRLE is non-nil
// it is embedded verbatim instead, allowing tests to feed corrupt data.
type ObjectSpec struct {
	ID     int
	Width  int
	Height int
	Grid   []byte
	RawRLE []byte
}

// CompositionObjectSpec is one object reference inside a PCS.
type CompositionObjectSpec struct {
	ObjectID   int
	WindowID   byte
	X, Y       int
	Cropped    bool
	CropX      int
	CropY      int
	CropWidth  int
	CropHeight int
}

// DisplaySetSpec fully describes one display set.
type DisplaySetSpec struct {
	PTS           int64
	State         segment.CompositionState
	PaletteUpdate bool
	PaletteID     byte
	Width         int
	Height        int
	Windows       []WindowSpec
	Palettes      []PaletteSpec
	Objects       []ObjectSpec
	Composition   []CompositionObjectSpec
}

// Millis converts milliseconds into the nanosecond duration used by specs.
func Millis(ms int64) int64 {
	return ms * 1_000_000
}

// Build serializes the given display sets into a complete SUP byte stream.
func Build(sets []DisplaySetSpec) []byte {
	var out []byte

	for _, set := range sets {
		pts := uint32(set.PTS / 1_000_000 * 90)

		// Canonical on-disc order: PCS, WDS, PDS, ODS, END.
		out = append(out, buildPCS(pts, set)...)
		if len(set.Windows) > 0 {
			out = append(out, buildWDS(pts, set.Windows)...)
		}
		if len(set.Palettes) > 0 {
			out = append(out, buildPDS(pts, set.PaletteID, set.Palettes)...)
		}
		for _, obj := range set.Objects {
			out = append(out, buildODS(pts, obj)...)
		}
		out = append(out, buildEND(pts)...)
	}
	return out
}

func writeHeader(pts uint32, segType byte, payload []byte) []byte {
	var out []byte
	out = append(out, 0x50, 0x47) // "PG"
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, pts)
	out = append(out, b...)
	binary.BigEndian.PutUint32(b, pts) // DTS mirrors PTS
	out = append(out, b...)
	out = append(out, segType)
	out = append(out, byte(len(payload)>>8), byte(len(payload)))
	return append(out, payload...)
}

func buildWDS(pts uint32, windows []WindowSpec) []byte {
	payload := []byte{byte(len(windows))}
	for _, w := range windows {
		payload = append(payload, w.ID)
		payload = appendU16(payload, w.X)
		payload = appendU16(payload, w.Y)
		payload = appendU16(payload, w.Width)
		payload = appendU16(payload, w.Height)
	}
	return writeHeader(pts, segWDS, payload)
}

func buildPDS(pts uint32, paletteID byte, entries []PaletteSpec) []byte {
	payload := []byte{paletteID, 0} // palette id, version
	for _, e := range entries {
		payload = append(payload, e.ID, e.Y, e.Cr, e.Cb, e.Transparency)
	}
	return writeHeader(pts, segPDS, payload)
}

func buildODS(pts uint32, obj ObjectSpec) []byte {
	var rle []byte
	if obj.RawRLE != nil {
		rle = obj.RawRLE
	} else {
		rle = EncodeRLE(obj.Grid, obj.Width, obj.Height)
	}
	// object_data_length covers width+height+data
	dataLength := 4 + len(rle)
	payload := []byte{
		byte(obj.ID >> 8), byte(obj.ID), // object id
		0,    // version
		0xC0, // first and last in sequence
		byte(dataLength >> 16), byte(dataLength >> 8), byte(dataLength),
	}
	payload = appendU16(payload, obj.Width)
	payload = appendU16(payload, obj.Height)
	payload = append(payload, rle...)
	return writeHeader(pts, segODS, payload)
}

func buildPCS(pts uint32, set DisplaySetSpec) []byte {
	var stateByte byte
	switch set.State {
	case segment.CompositionStateNormal:
		stateByte = 0x00
	case segment.CompositionStateAcquisitionState:
		stateByte = 0x40
	case segment.CompositionStateEpochStart:
		stateByte = 0x80
	}

	paletteFlag := byte(0x00)
	if set.PaletteUpdate {
		paletteFlag = 0x80
	}

	payload := appendU16(nil, set.Width)
	payload = appendU16(payload, set.Height)
	payload = append(payload, 0x10) // frame rate
	payload = appendU16(payload, 0) // composition number
	payload = append(payload, stateByte)
	payload = append(payload, paletteFlag)
	payload = append(payload, set.PaletteID)
	payload = append(payload, byte(len(set.Composition)))

	for _, c := range set.Composition {
		payload = appendU16(payload, c.ObjectID)
		payload = append(payload, c.WindowID)
		if c.Cropped {
			payload = append(payload, 0x40)
		} else {
			payload = append(payload, 0x00)
		}
		payload = appendU16(payload, c.X)
		payload = appendU16(payload, c.Y)
		payload = appendU16(payload, c.CropX)
		payload = appendU16(payload, c.CropY)
		payload = appendU16(payload, c.CropWidth)
		payload = appendU16(payload, c.CropHeight)
	}

	return writeHeader(pts, segPCS, payload)
}

func buildEND(pts uint32) []byte {
	return writeHeader(pts, segEND, nil)
}

func appendU16(out []byte, v int) []byte {
	return append(out, byte(v>>8), byte(v))
}

// EncodeRLE encodes a row-major palette-index grid using PGS run-length
// encoding. Every line ends with an explicit end-of-line marker.
func EncodeRLE(grid []byte, width int, height int) []byte {
	var out []byte

	for y := 0; y < height; y++ {
		row := grid[y*width : (y+1)*width]
		for x := 0; x < width; {
			value := row[x]
			run := 1
			for x+run < width && row[x+run] == value && run < 16383 {
				run++
			}

			out = appendRLERun(out, value, run)
			x += run
		}
		out = append(out, 0x00, 0x00) // end of line
	}

	return out
}

func appendRLERun(out []byte, value byte, run int) []byte {
	if run == 1 && value != 0 {
		// Single non-zero pixel.
		return append(out, value)
	}

	if value == 0 {
		if run <= 63 {
			return append(out, 0x00, byte(run))
		}
		return append(out, 0x00, 0x40|byte(run>>8), byte(run))
	}

	if run <= 63 {
		return append(out, 0x00, 0x80|byte(run), value)
	}
	return append(out, 0x00, 0xC0|byte(run>>8), byte(run), value)
}

// SolidGrid returns a width*height grid filled with palette index v.
func SolidGrid(width, height int, v byte) []byte {
	grid := make([]byte, width*height)
	for i := range grid {
		grid[i] = v
	}
	return grid
}
