package assets

import (
	"github.com/danjacques/pixelproxy/web"

	"github.com/gobuffalo/packr"
)

// WWW is the root of static data.
var WWW = web.PackrBox{Box: packr.NewBox("www")}

// Templates is the root of template data.
var Templates = web.PackrBox{Box: packr.NewBox("templates")}
