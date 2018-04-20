package logging

import (
	"go.uber.org/zap/zapcore"
)

// VerbosityFlag is a pflag.Value wrapper around a zapcore.Level, allowing it
// to be used as a pflag.Value.
//
// zapcore.Level provides flag.Value interface, so we only need to augment it.
type VerbosityFlag struct {
	// Level is the underlying zapcore.Level variable to write to.
	*zapcore.Level
}

// Type implements cobra's "pflag.Value" interface.
func (vf *VerbosityFlag) Type() string { return "zapcore.Level" }
