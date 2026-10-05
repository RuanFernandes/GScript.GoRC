package webviewidle

import (
	"testing"
	"time"
)

func TestRunPulsesVisibleWebviewAndStops(t *testing.T) {
	stop := make(chan struct{})
	wakes := make(chan struct{}, 1)
	done := make(chan struct{})
	go func() {
		Run(stop, time.Millisecond, func() bool { return true }, func() {
			select {
			case wakes <- struct{}{}:
			default:
			}
		})
		close(done)
	}()

	select {
	case <-wakes:
	case <-time.After(time.Second):
		t.Fatal("visible WebView did not receive a wake pulse")
	}

	close(stop)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("watchdog did not stop")
	}
}

func TestRunSkipsHiddenWebview(t *testing.T) {
	stop := make(chan struct{})
	checked := make(chan struct{}, 1)
	wakes := make(chan struct{}, 1)
	done := make(chan struct{})
	go func() {
		Run(stop, time.Millisecond, func() bool {
			select {
			case checked <- struct{}{}:
			default:
			}
			return false
		}, func() {
			select {
			case wakes <- struct{}{}:
			default:
			}
		})
		close(done)
	}()

	select {
	case <-checked:
	case <-time.After(time.Second):
		t.Fatal("watchdog did not check window visibility")
	}
	select {
	case <-wakes:
		t.Fatal("hidden WebView unexpectedly received a wake pulse")
	default:
	}

	close(stop)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("watchdog did not stop")
	}
}
