# go-pgs-parser

Golang PGS Parser

## Installation

```shell
go get github.com/mbiamont/go-pgs-parser
```

## Getting started

The parser requires you specify a SUP file's path.

More info about PGS and SUP file here: https://fileinfo.com/extension/sup

For each subtitle bitmap, it'll ask you in which file to write it.

### Save as PNG images (recommended)

```go
package main

import (
	"fmt"
	"github.com/mbiamont/go-pgs-parser/pgs"
	"os"
)

func main() {
	parser := pgs.NewPgsParser()

	parser.ConvertToPngImages("./sample/input.sup", func(index int) (*os.File, error) {
		return os.Create(fmt.Sprintf("./sample/subs/input.%d.png", index))
	})
}
```
### Save as JPG images

```go
package main

import (
	"fmt"
	"github.com/mbiamont/go-pgs-parser/pgs"
	"os"
)

func main() {
	parser := pgs.NewPgsParser()

	parser.ConvertToJpgImages("./sample/input.sup", func(index int) (*os.File, error) {
		return os.Create(fmt.Sprintf("./sample/subs/input.%d.jpg", index))
	})
}
```

### Output example

<img src="./art/output-example.png" />

## Compose a subtitle timeline

The per-object export above emits one picture per object definition segment,
which is not enough to preview a real PGS frame: a single presentation can
stack several objects (e.g. two dialogue lines plus a positioning note),
recolor them through later palette updates, or clear the screen.

`ComposeTimeline` follows the Presentation Composition Segments and the
palette/object updates to render every display interval onto a transparent
RGBA canvas. Each returned `Frame` carries its start and end time and the
composited image, including fully-transparent clear frames.

```go
package main

import (
	"fmt"
	"github.com/mbiamont/go-pgs-parser/composition"
	"github.com/mbiamont/go-pgs-parser/pgs"
)

func main() {
	frames, err := pgs.NewPgsParser().ComposeTimeline("./sample/input.sup", nil)
	if err != nil {
		panic(err)
	}

	for _, frame := range frames {
		fmt.Printf("%s -> %s empty=%v open=%v issues=%d\n",
			frame.Start, frame.End, frame.Empty, frame.OpenEnded, len(frame.Issues))
	}
}
```

Recoverable problems do not abort parsing: they are attached to the affected
`Frame.Issues` instead. The package exposes sentinel errors for them —
`ErrMissingObject`, `ErrDuplicateTimestamp`, `ErrCropOutOfBounds`,
`ErrRleCorrupt`, `ErrPaletteEntryMissing` and `ErrLongGap` — and each issue
wraps its sentinel so `errors.Is` works. Use `Options` to tune the long-gap
threshold (`MaxFrameDuration`) or drop the final frame whose end time is
unknown (`DropOpenEnded`).

## Extract SUP from MKV

You can extract a SUP file using ffmpeg like this:

```shell
ffmpeg -i input.mkv -map 0:s:0 -c copy input.sup -y
```
