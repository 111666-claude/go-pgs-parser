package pgs

import (
	"github.com/mbiamont/go-pgs-parser/composition"
	"github.com/mbiamont/go-pgs-parser/displaySet"
	"image/jpeg"
	"image/png"
	"os"
	"time"
)

type PgsParser interface {
	// ParsePgsFile Parse the input file path and call the onImage function for each ImageData found
	ParsePgsFile(inputFilePath string, onImage func(index int, startTime time.Duration, data displaySet.ImageData) error) error

	// ParseDisplaySets Parse the input file path and call the onDisplaySet function for each DisplaySet found
	ParseDisplaySets(inputFilePath string, onDisplaySet func(data displaySet.DisplaySet, startTime time.Duration) error) error

	// ComposeTimeline composes every display set into a transparent RGBA
	// canvas and returns the start/end interval of each valid display period,
	// including transparent clear frames. Per-frame problems are reported in
	// Frame.Issues; structural stream errors are returned.
	ComposeTimeline(inputFilePath string, opts *composition.Options) ([]composition.Frame, error)

	// ConvertToPngImages Parse the input file path and save each subtitle picture as a PNG using fileCreator function to create the PNG file
	ConvertToPngImages(inputFilePath string, fileCreator func(index int, startTime time.Duration) (*os.File, error)) error

	// ConvertToJpgImages Parse the input file path and save each subtitle picture as a JPG using fileCreator function to create the JPG file
	ConvertToJpgImages(inputFilePath string, fileCreator func(index int, startTime time.Duration) (*os.File, error)) error
}

type pgsParser struct {
}

// NewPgsParser Initialize a new PGS parser
func NewPgsParser() PgsParser {
	return &pgsParser{}
}

func (p *pgsParser) ParsePgsFile(inputFilePath string, onImage func(index int, startTime time.Duration, data displaySet.ImageData) error) error {
	i := 0
	return p.ParseDisplaySets(inputFilePath, func(data displaySet.DisplaySet, startTime time.Duration) error {
		imageData, err := data.ToImageData()

		if err != nil {
			return err
		} else if imageData != nil {
			err = onImage(i, startTime, *imageData)

			if err != nil {
				return err
			}
			i++
		}
		return nil
	})
}

func (p *pgsParser) ParseDisplaySets(inputFilePath string, onDisplaySet func(data displaySet.DisplaySet, startTime time.Duration) error) error {
	return displaySet.ReadDisplaySetsFromFile(inputFilePath, onDisplaySet)
}

func (p *pgsParser) ComposeTimeline(inputFilePath string, opts *composition.Options) ([]composition.Frame, error) {
	var sets []displaySet.DisplaySet

	err := p.ParseDisplaySets(inputFilePath, func(data displaySet.DisplaySet, startTime time.Duration) error {
		sets = append(sets, data)
		return nil
	})

	if err != nil {
		return nil, err
	}

	return composition.NewComposer().ComposeDisplaySets(sets, opts)
}

func (p *pgsParser) ConvertToPngImages(inputFilePath string, fileCreator func(index int, startTime time.Duration) (*os.File, error)) error {
	return p.ParsePgsFile(inputFilePath, func(index int, startTime time.Duration, data displaySet.ImageData) error {
		f, err := fileCreator(index, startTime)

		if err != nil {
			return err
		}

		defer f.Close()

		if err != nil {
			return err
		}

		return png.Encode(f, data.Image)
	})
}

func (p *pgsParser) ConvertToJpgImages(inputFilePath string, fileCreator func(index int, startTime time.Duration) (*os.File, error)) error {
	return p.ParsePgsFile(inputFilePath, func(index int, startTime time.Duration, data displaySet.ImageData) error {
		f, err := fileCreator(index, startTime)

		if err != nil {
			return err
		}

		defer f.Close()

		if err != nil {
			return err
		}

		return jpeg.Encode(f, data.Image, &jpeg.Options{
			Quality: 100,
		})
	})
}
