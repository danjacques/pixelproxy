package pixelproxy

import (
	"context"
)

// SystemControl is a control to the local system.
type SystemControl struct {
	// ValidateAccess attempts to validate whether or not the current user has
	// access to system commands. It will return an error if they do not.
	ValidateAccess func(context.Context) error

	// Shutdown issues a shutdown command.
	Shutdown func(context.Context) error

	// Restart issues a restart command.
	Restart func(context.Context) error
}
