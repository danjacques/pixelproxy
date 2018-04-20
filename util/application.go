package util

import (
	"context"
	"os"
	"os/signal"

	"github.com/danjacques/pixelproxy/util/logging"
	"github.com/danjacques/pixelproxy/util/profiling"

	"github.com/spf13/pflag"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// Application is a configuration for a generic application entry point.
//
// Application will set up a basic Context, signal handler, and run an execute
// method.
type Application struct {
	// Verbosity is the logging verbosity level.
	Verbosity zapcore.Level

	// Production, if true, means to run in production logging mode.
	Production bool

	// ColorizeLogs, if true, allows the application to colorize its logs.
	ColorizeLogs bool

	// LogPath, if not nil, is a path to output logs to.
	LogPath string

	// Profiler is the configured profiler to use.
	Profiler profiling.Profiler
}

// AddFlags adds application-level flags to fs.
func (a *Application) AddFlags(fs *pflag.FlagSet) {
	var verbosity = logging.VerbosityFlag{Level: &a.Verbosity}
	fs.VarP(&verbosity, "verbose", "v", "Set verbosity level.")

	fs.BoolVar(&a.Production, "production", a.Production, "Enable production configuration.")

	fs.BoolVar(&a.ColorizeLogs, "colorize_logs", a.ColorizeLogs,
		"When running in non-production, colorize log output.")

	fs.StringVar(&a.LogPath, "log_path", a.LogPath, "If set, write logs to this path.")

	// Add Profiler flags.
	a.Profiler.AddFlags(fs)
}

// Run runs the Application in a generic harness.
//
// It creates a derivative Context from c which will be cancelled when/if a
// signal is encountered. It also installs a logger.
//
// If the callback returns a non-nil error, Run will exit with a status of 1
// and log the error.
func (a *Application) Run(c context.Context, fn func(context.Context) error) {
	// Construct logger config.
	var logConfig zap.Config
	if a.Production {
		logConfig = zap.NewProductionConfig()
	} else {
		logConfig = zap.NewDevelopmentConfig()
		if a.ColorizeLogs {
			logConfig.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
		}
	}
	logConfig.Level.SetLevel(a.Verbosity)
	if a.LogPath != "" {
		logConfig.OutputPaths = append(logConfig.OutputPaths, a.LogPath)
	}

	err := logging.WithLogger(c, &logConfig, func(c context.Context) error {
		// Start the Profiler.
		if err := a.Profiler.Start(); err != nil {
			logging.S(c).Warnf("Failed to start profiler: %s", err)
		} else {
			defer a.Profiler.Stop()
		}

		// Wait for interrupt signal and cancel Context.
		c, cancelFunc := context.WithCancel(c)
		defer cancelFunc()

		signalC := make(chan os.Signal, 1)
		signal.Notify(signalC, os.Interrupt)
		go func() {
			received := false
			for sig := range signalC {
				if received {
					logging.S(c).Warnf("Signal %q received (multiple times), killing.", sig)
					os.Exit(1)
				}

				logging.S(c).Infof("Signal %q received, shutting down...", sig)
				cancelFunc()
				received = true
			}
		}()
		defer func() {
			signal.Stop(signalC)
			close(signalC)
		}()

		return fn(c)
	})
	if err != nil {
		os.Exit(1)
	}
}
