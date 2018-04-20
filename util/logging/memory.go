package logging

import (
	"sync"

	"go.uber.org/zap/zapcore"
)

// MemoryLogger integrates as a zap Logger hook which retains the last set of
// logs.
//
// MemoryLogger's fields may not be adjusted after it has been installed as a
// hook.
//
// Internally, MemoryLogger uses a ring buffer.
type MemoryLogger struct {
	// Size is the number of log entries to retain.
	Size int

	// Level is the minimum log level to retain.
	MinLevel zapcore.Level

	mu      sync.Mutex
	entries []*zapcore.Entry

	pos   int
	count int
}

// Hook is a zap logging hook that adds this log entry to the MemoryLogger.
func (ml *MemoryLogger) Hook(e zapcore.Entry) error {
	if e.Level < ml.MinLevel {
		return nil
	}

	if ml.Size <= 0 {
		panic("Size must be positive")
	}

	ml.mu.Lock()
	defer ml.mu.Unlock()

	if cap(ml.entries) != ml.Size {
		ml.entries = make([]*zapcore.Entry, ml.Size)
		ml.pos = 0
	}

	ml.entries[ml.pos] = &e

	// Advance our write pointer.
	ml.pos++
	if ml.pos >= len(ml.entries) {
		ml.pos = 0
	}

	// Increment our count. Ths is how we tell whether the ring buffer is full.
	if ml.count < len(ml.entries) {
		ml.count++
	}

	return nil
}

// Get returns the active entries.
func (ml *MemoryLogger) Get() []zapcore.Entry {
	ml.mu.Lock()
	defer ml.mu.Unlock()

	result := make([]zapcore.Entry, 0, ml.count)
	for i := 0; i < ml.count; i++ {
		index := (ml.pos - ml.count + i)
		if index < 0 {
			index += len(ml.entries)
		} else if index >= len(ml.entries) {
			index -= len(ml.entries)
		}

		if e := ml.entries[index]; e != nil {
			result = append(result, *e)
		}
	}

	return result
}
