package util

import (
	"context"
	"time"
)

// LoopUntil runs the function fn periodically, every p, until the Context is
// cancelled or the function returns an error. fn will run immediately, and then
// periodicalkly after that.
//
// If fn returns an error, LoopUntil will terminate immediately and return that
// error. Otherwise, LoopUntil will return the Context's cancellation error.
func LoopUntil(c context.Context, p time.Duration, fn func(context.Context) error) error {
	// Does our Context start cancelled?
	select {
	case <-c.Done():
		return c.Err()
	default:
	}

	var s Sleeper
	defer s.Close()

	for {
		// Run our function.
		if err := fn(c); err != nil {
			return err
		}

		// Block until our timer expires or our Context is cancelled.
		if err := s.Sleep(c, p); err != nil {
			return err
		}
	}
}
