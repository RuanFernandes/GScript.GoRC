package webviewidle

import (
	"sync/atomic"
	"time"
)

// Run invokes one visibility and wake callback at a time without blocking its
// stop loop, since window APIs may wait for the UI thread during shutdown.
func Run(stop <-chan struct{}, interval time.Duration, isVisible func() bool, wake func()) {
	if interval <= 0 || isVisible == nil || wake == nil {
		return
	}

	var callbackInFlight atomic.Bool
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			if !callbackInFlight.CompareAndSwap(false, true) {
				continue
			}
			go func() {
				defer callbackInFlight.Store(false)
				select {
				case <-stop:
					return
				default:
				}
				if !isVisible() {
					return
				}
				select {
				case <-stop:
					return
				default:
				}
				wake()
			}()
		}
	}
}
