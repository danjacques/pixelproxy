package web

import (
	"github.com/tdewolff/minify"
	minHTML "github.com/tdewolff/minify/html"
	minSVG "github.com/tdewolff/minify/svg"
)

// DefaultMinifier is a default minifier configuration to use.
func DefaultMinifier() *minify.M {
	m := minify.New()
	m.Add("text/html", minHTML.DefaultMinifier)
	m.Add("image/svg+xml", minSVG.DefaultMinifier)
	return m
}
