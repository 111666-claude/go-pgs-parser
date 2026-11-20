package composition

import (
	"github.com/mbiamont/go-pgs-parser/displaySet"
	"image"
	"time"
)

// Frame is one valid display interval of the composed subtitle stream: the
// transparent RGBA canvas that is on screen from Start (inclusive) until End
// (exclusive). When Empty is true the canvas is fully transparent and the
// frame represents an explicit clear; its End equals its Start when another
// display set immediately replaces it.
type Frame struct {
	// Start is the presentation timestamp at which the frame appears.
	Start time.Duration

	// End is the presentation timestamp of the following display set. When
	// OpenEnded is true no following set exists and End equals Start.
	End time.Duration

	// Image is the full composition canvas (PCS dimensions), with every
	// uncovered pixel fully transparent.
	Image *image.RGBA

	// Empty reports whether the frame is an explicit clear (no objects).
	Empty bool

	// OpenEnded reports whether the frame extends beyond the last display
	// set in the file, so its real end time is unknown.
	OpenEnded bool

	// Issues holds every non-fatal condition encountered while building the
	// frame (missing objects, corrupt RLE, crop overflow, duplicate
	// timestamps...). The frame is still emitted.
	Issues []Issue
}

// Options controls timeline composition.
type Options struct {
	// MaxFrameDuration flags frames that remain on screen longer than this
	// without any new display set (ErrLongGap). Zero disables the check.
	MaxFrameDuration time.Duration

	// DropOpenEnded omits the final frame whose real end time is unknown
	// because no display set follows it. By default it is kept with
	// OpenEnded=true; set this to only receive fully-bounded intervals.
	DropOpenEnded bool
}

// Composer turns a sequence of display sets into a time-ordered list of
// composited frames.
type Composer interface {
	// ComposeDisplaySets consumes display sets in presentation order and
	// returns one Frame per valid interval, including transparent clear
	// frames. Structural stream errors are returned; recoverable per-frame
	// problems land in Frame.Issues.
	ComposeDisplaySets(sets []displaySet.DisplaySet, opts *Options) ([]Frame, error)

	// ComposeBytes parses a complete SUP byte stream and composes its
	// timeline.
	ComposeBytes(sup []byte, opts *Options) ([]Frame, error)
}

// NewComposer builds a timeline composer.
func NewComposer() Composer {
	return &composer{}
}
