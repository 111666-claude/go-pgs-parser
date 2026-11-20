package composition

import (
	"errors"
	"github.com/mbiamont/go-pgs-parser/displaySet"
)

// Sentinel errors describing the non-fatal conditions a composed frame can
// encounter. Frame issues wrap one of these, so errors.Is works on both the
// issue and the returned sentinel.
var (
	// ErrMissingObject means a PCS references an object id that has never
	// been defined by an ODS in the current epoch.
	ErrMissingObject = errors.New("composition references missing object")

	// ErrMalformedObject means an ODS group cannot be decoded, for example
	// because none of its segments carries object dimensions.
	ErrMalformedObject = errors.New("malformed object definition segment")

	// ErrDuplicateTimestamp means two consecutive display sets share the
	// same presentation timestamp (or timestamps are not monotonically
	// increasing), producing a zero-duration frame.
	ErrDuplicateTimestamp = errors.New("duplicate or out-of-order presentation timestamp")

	// ErrCropOutOfBounds means a PCS crop rectangle lies (partly or fully)
	// outside the decoded object bitmap. The visible intersection is still
	// rendered.
	ErrCropOutOfBounds = errors.New("object crop rectangle outside object bounds")

	// ErrRleCorrupt re-exports the decoder error for truncated or otherwise
	// invalid RLE object data.
	ErrRleCorrupt = displaySet.ErrRleCorrupt

	// ErrPaletteEntryMissing means an object pixel references a palette
	// index that the active palette does not define; the pixel is rendered
	// fully transparent.
	ErrPaletteEntryMissing = errors.New("palette entry missing for palette index")

	// ErrLongGap means a single composition stayed on screen longer than the
	// configured MaxFrameDuration without any new display set arriving.
	ErrLongGap = errors.New("no new display set for longer than maximum frame duration")
)

// IssueKind categorizes a frame issue.
type IssueKind string

const (
	IssueKindMissingObject       IssueKind = "missing-object"
	IssueKindMalformedObject     IssueKind = "malformed-object"
	IssueKindDuplicateTimestamp  IssueKind = "duplicate-timestamp"
	IssueKindCropOutOfBounds     IssueKind = "crop-out-of-bounds"
	IssueKindRleCorrupt          IssueKind = "rle-corrupt"
	IssueKindPaletteEntryMissing IssueKind = "palette-entry-missing"
	IssueKindLongGap             IssueKind = "long-gap"
)

// Issue is a non-fatal problem attached to the frame whose display set caused
// it. Unwrap it (or use errors.Is) to match against the package sentinels.
type Issue struct {
	Kind     IssueKind
	ObjectID int
	Err      error
}

func (i Issue) Error() string {
	return i.Err.Error()
}

func (i Issue) Unwrap() error {
	return i.Err
}

func newIssue(kind IssueKind, objectID int, err error) Issue {
	return Issue{Kind: kind, ObjectID: objectID, Err: err}
}
