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

func TestRunStopsWhileVisibilityCheckIsBlocked(t *testing.T) {
	stop := make(chan struct{})
	checked := make(chan struct{}, 1)
	releaseCheck := make(chan struct{})
	done := make(chan struct{})
	go func() {
		Run(stop, time.Millisecond, func() bool {
			select {
			case checked <- struct{}{}:
			default:
			}
			<-releaseCheck
			return true
		}, func() {})
		close(done)
	}()

	select {
	case <-checked:
	case <-time.After(time.Second):
		t.Fatal("watchdog did not check window visibility")
	}
	close(stop)
	select {
	case <-done:
	case <-time.After(time.Second):
		close(releaseCheck)
		t.Fatal("watchdog waited for the in-flight visibility check during shutdown")
	}
	close(releaseCheck)
}

func TestRunStopsWhileWakeIsBlocked(t *testing.T) {
	stop := make(chan struct{})
	wakeStarted := make(chan struct{}, 1)
	releaseWake := make(chan struct{})
	done := make(chan struct{})
	go func() {
		Run(stop, time.Millisecond, func() bool { return true }, func() {
			select {
			case wakeStarted <- struct{}{}:
			default:
			}
			<-releaseWake
		})
		close(done)
	}()

	select {
	case <-wakeStarted:
	case <-time.After(time.Second):
		t.Fatal("visible WebView did not begin a wake pulse")
	}
	close(stop)
	select {
	case <-done:
	case <-time.After(time.Second):
		close(releaseWake)
		t.Fatal("watchdog waited for the in-flight wake pulse during shutdown")
	}
	close(releaseWake)
}
