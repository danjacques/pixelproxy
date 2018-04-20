// Package bootstrap exposes a bundle containing Twitter Bootstrap files.
package bootstrap

import (
	"github.com/danjacques/pixelproxy/web"

	"github.com/gobuffalo/packr"
)

// Bundle is the bundle of bootstrap files.
var Bundle = web.PackrBox{Box: packr.NewBox("www")}
