package web

import (
	"io"

	"github.com/ajstarks/svgo"
)

// A Pixel is a single RGB pixel.
type Pixel struct {
	R uint8
	G uint8
	B uint8
}

// A Strip is a collection of Pixel.
type Strip struct {
	Number int
	Pixels []Pixel
}

// RenderStripSVG renders a SVG for the specified strips.
func RenderStripSVG(strips []Strip, w io.Writer) error {
	const (
		pixelWidth   = 4
		pixelHeight  = 8
		stripPadding = 2
	)

	// Identify the longest strip (they should all be the same, but...)
	longestStrip := 0
	for i := range strips {
		if l := len(strips[i].Pixels); l > longestStrip {
			longestStrip = l
		}
	}

	canvas := svg.New(w)
	canvas.Start(pixelWidth*longestStrip, (pixelHeight+stripPadding)*len(strips))

	yOffset := 0
	for i := range strips {
		strip := &strips[i]

		for p := range strip.Pixels {
			pixel := &strip.Pixels[p]
			color := canvas.RGB(int(pixel.R), int(pixel.G), int(pixel.B))
			canvas.Rect(p*pixelWidth, yOffset, pixelWidth, pixelHeight, color)
		}

		yOffset += pixelHeight + stripPadding
	}

	canvas.End()
	return nil
}
