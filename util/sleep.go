package util

import (
	"context"
	"time"
)

// Sleeper is a device that facilitates Context-cancellable sleeping.
//
// Sleeper is not safe for concurrent usage.
type Sleeper struct {
	t *time.Timer
}

// Sleep sleeps until either the specified period, d, has expired, or the
// supplied Context has been cancelled.
//
// If Sleep exits naturally, it will return nil. Otherwise, if it is cancelled
// prematurely, the Context's error will be returned.
func (s *Sleeper) Sleep(c context.Context, d time.Duration) error {
	// If we're not sleeping for a positive amount of time, return immediately.
	if d <= 0 {
		return nil
	}

	// If our Context is already cancelled, don't do anything.
	select {
	case <-c.Done():
		return c.Err()
	default:
	}

	// We assume that t is in a triggered state from previous use.
	if s.t == nil {
		s.t = time.NewTimer(d)
	} else {
		s.t.Reset(d)
	}

	select {
	case <-c.Done():
		// Our Context has finished before our timer has finished. Cancel the timer.
		if !s.t.Stop() {
			<-s.t.C
		}
		return c.Err()

	case <-s.t.C:
		// The timer has ticked, our sleep completed successfully.
		return nil
	}
}

// Close closes the Sleeper, releasing any resources that it owns.
//
// Close is optional, but may offer better resource management if called.
func (s *Sleeper) Close() {
	s.t.Stop()
	s.t = nil
}

// Sleep is a shortcut for a single-use Sleeper.
func Sleep(c context.Context, d time.Duration) error {
	var s Sleeper
	defer s.Close()

	return s.Sleep(c, d)
}
