package webviewidle

import "time"

func Run(stop <-chan struct{}, interval time.Duration, isVisible func() bool, wake func()) {
	if interval <= 0 || isVisible == nil || wake == nil {
		return
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			if isVisible() {
				wake()
			}
		}
	}
}
