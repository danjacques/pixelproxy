package logging

import (
	"context"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var (
	loggerContextKey = "logger context key"
	nop              = zap.NewNop()
)

type contextConfig struct {
	// Logger is the installed logger.
	Logger *zap.Logger

	WarnMem MemoryLogger
	AllMem  MemoryLogger
}

func getContextConfig(c context.Context) *contextConfig {
	if ccfg, ok := c.Value(&loggerContextKey).(*contextConfig); ok {
		return ccfg
	}
	return nil
}

func (cfg *contextConfig) use(c context.Context) context.Context {
	return context.WithValue(c, &loggerContextKey, cfg)
}

// L returns the logger embedded in rhe Context by WithLogger.
//
// If there is no logger, L will panic.
func L(c context.Context) (l *zap.Logger) {
	ccfg := getContextConfig(c)
	if ccfg == nil {
		return nop
	}
	if ccfg.Logger == nil {
		return nop
	}
	return ccfg.Logger
}

// S is shorthand for L(c).Sugar().
//
// Like L, if there is no logger, S will panic.
func S(c context.Context) *zap.SugaredLogger {
	return L(c).Sugar()
}

// GetRecentLogs returns a list of recent buffered logs.
func GetRecentLogs(c context.Context) []zapcore.Entry {
	ccfg := getContextConfig(c)
	if ccfg != nil {
		return ccfg.AllMem.Get()
	}
	return nil
}

// GetRecentEscalatedLogs returns a list of recent buffered warn and error logs.
func GetRecentEscalatedLogs(c context.Context) []zapcore.Entry {
	ccfg := getContextConfig(c)
	if ccfg != nil {
		return ccfg.WarnMem.Get()
	}
	return nil
}

// WithLogger runs the specified function with a logger embedded in the Context.
func WithLogger(c context.Context, cfg *zap.Config, fn func(context.Context) error) (err error) {
	// Generate memory loggers.
	ctxConfig := contextConfig{
		WarnMem: MemoryLogger{
			Size:     100,
			MinLevel: zapcore.WarnLevel,
		},
		AllMem: MemoryLogger{
			Size:     100,
			MinLevel: zapcore.DebugLevel,
		},
	}

	// Construct our logger.
	l, err := cfg.Build(
		zap.Hooks(ctxConfig.WarnMem.Hook, ctxConfig.AllMem.Hook),
	)
	if err != nil {
		return err
	}
	defer func() {
		// Sync in defer. If an error occurs, propagate it.
		if serr := l.Sync(); serr != nil && err == nil {
			err = serr
		}
	}()

	ctxConfig.Logger = l
	return fn(ctxConfig.use(c))
}

// UseLogger installs the provided Logger into the Context.
func UseLogger(c context.Context, l *zap.Logger) context.Context {
	ctxConfig := contextConfig{
		Logger: l,
	}
	return ctxConfig.use(c)
}

// LogError outputs the contents of the error, err, to the logger.
func LogError(c context.Context, err error) {
	S(c).Errorf("Encountered error: %s", err)
}
