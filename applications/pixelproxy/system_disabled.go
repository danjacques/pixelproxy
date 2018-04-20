// +build !linux

package pixelproxy

import (
	"context"

	"github.com/pkg/errors"
)

var errSystemControlNotSupported = errors.New("system control not supported for this system")

// DefaultSystemControl implements SystemControl, returning an error for each
// command.
var DefaultSystemControl = &SystemControl{
	ValidateAccess: func(context.Context) error { return errSystemControlNotSupported },
	Shutdown:       func(context.Context) error { return errSystemControlNotSupported },
	Restart:        func(context.Context) error { return errSystemControlNotSupported },
}
