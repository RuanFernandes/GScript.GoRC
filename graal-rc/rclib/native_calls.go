package rclib

import (
	"log"
	"sync"
	"sync/atomic"
	"time"
)

const (
	nativeSlowCallThreshold = 250 * time.Millisecond
	nativeSlowWaitThreshold = 250 * time.Millisecond
	nativeStallThreshold    = 5 * time.Second
	nativeReportInterval    = 30 * time.Second
)

// NativeCallStats contains timings and counts only. Operation names are native
// export names; arguments, handles, server addresses and payloads are excluded.
// CompletedCalls counts calls that returned or unwound, not successful requests.
type NativeCallStats struct {
	ActiveOperation      string
	ActiveCallID         uint64
	ActiveDuration       time.Duration
	WaitingCalls         int
	CompletedCalls       uint64
	SlowCalls            uint64
	SlowWaits            uint64
	MaxCallDuration      time.Duration
	MaxWaitDuration      time.Duration
	LastSlowOperation    string
	LastSlowCallDuration time.Duration
	LastWaitOperation    string
	LastSlowWaitDuration time.Duration
}

type nativeCallTracker struct {
	mu        sync.Mutex
	stats     NativeCallStats
	startedAt time.Time
	nextID    uint64
}

var (
	nativeCalls          nativeCallTracker
	nativeMonitorRunning atomic.Bool
)

// beginNativeCall is the only entry to the native serialization mutex. The
// diagnostics mutex is never held while waiting for or executing native code.
func beginNativeCall(operation string) time.Time {
	waitStarted := time.Now()
	nativeCalls.queued()
	dllMu.Lock()
	started := time.Now()
	nativeCalls.started(operation, waitStarted, started)
	return started
}

func endNativeCall(started time.Time) {
	nativeCalls.completed(started, time.Now())
	dllMu.Unlock()
}

func (t *nativeCallTracker) queued() {
	t.mu.Lock()
	t.stats.WaitingCalls++
	t.mu.Unlock()
}

func (t *nativeCallTracker) started(operation string, waitStarted, started time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.stats.WaitingCalls--
	t.nextID++
	t.stats.ActiveCallID = t.nextID
	t.stats.ActiveOperation = operation
	t.startedAt = started
	waitDuration := started.Sub(waitStarted)
	if waitDuration > t.stats.MaxWaitDuration {
		t.stats.MaxWaitDuration = waitDuration
	}
	if waitDuration >= nativeSlowWaitThreshold {
		t.stats.SlowWaits++
		t.stats.LastWaitOperation = operation
		t.stats.LastSlowWaitDuration = waitDuration
	}
}

func (t *nativeCallTracker) completed(started, finished time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	duration := finished.Sub(started)
	t.stats.CompletedCalls++
	if duration > t.stats.MaxCallDuration {
		t.stats.MaxCallDuration = duration
	}
	if duration >= nativeSlowCallThreshold {
		t.stats.SlowCalls++
		t.stats.LastSlowOperation = t.stats.ActiveOperation
		t.stats.LastSlowCallDuration = duration
	}
	t.stats.ActiveOperation = ""
	t.stats.ActiveCallID = 0
	t.startedAt = time.Time{}
}

func (t *nativeCallTracker) snapshot(now time.Time) NativeCallStats {
	t.mu.Lock()
	defer t.mu.Unlock()
	stats := t.stats
	if !t.startedAt.IsZero() {
		stats.ActiveDuration = max(0, now.Sub(t.startedAt))
	}
	return stats
}

// ReadNativeCallStats remains available even while a DLL call holds dllMu.
// Taking a snapshot never calls the DLL or reads its memory.
func ReadNativeCallStats() NativeCallStats {
	return nativeCalls.snapshot(time.Now())
}

// MonitorNativeCalls blocks until stop closes and runs at most once per process.
// Start it in an application-owned background goroutine. It observes native
// calls independently of dllMu, reports stalls repeatedly at a bounded rate,
// and records when a previously stalled call has exited. It does not cancel,
// disconnect or retry a native call: none of those actions can safely interrupt
// arbitrary native code in this process.
func MonitorNativeCalls(stop <-chan struct{}) {
	if !nativeMonitorRunning.CompareAndSwap(false, true) {
		return
	}
	defer nativeMonitorRunning.Store(false)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	monitorNativeCalls(stop, ticker.C, &nativeCalls, logNativeCallReport)
}

type nativeCallReport struct {
	event     string
	operation string
	stats     NativeCallStats
}

type nativeCallWatchdog struct {
	stalledID        uint64
	stalledOperation string
	lastStallReport  time.Time
	lastSlowReport   time.Time
	reportedSlow     uint64
	reportedWaits    uint64
}

func monitorNativeCalls(stop <-chan struct{}, ticks <-chan time.Time, tracker *nativeCallTracker, report func(nativeCallReport)) {
	var watchdog nativeCallWatchdog
	for {
		select {
		case <-stop:
			return
		case now, ok := <-ticks:
			if !ok {
				return
			}
			watchdog.observe(tracker.snapshot(now), now, report)
		}
	}
}

func (w *nativeCallWatchdog) observe(stats NativeCallStats, now time.Time, report func(nativeCallReport)) {
	if w.stalledID != 0 && stats.ActiveCallID != w.stalledID {
		report(nativeCallReport{event: "resumed", operation: w.stalledOperation, stats: stats})
		w.stalledID = 0
		w.lastStallReport = time.Time{}
	}
	if stats.ActiveCallID != 0 && stats.ActiveDuration >= nativeStallThreshold &&
		(w.stalledID != stats.ActiveCallID || now.Sub(w.lastStallReport) >= nativeReportInterval) {
		report(nativeCallReport{event: "stalled", operation: stats.ActiveOperation, stats: stats})
		w.stalledID = stats.ActiveCallID
		w.stalledOperation = stats.ActiveOperation
		w.lastStallReport = now
	}
	if (stats.SlowCalls != w.reportedSlow || stats.SlowWaits != w.reportedWaits) &&
		(w.lastSlowReport.IsZero() || now.Sub(w.lastSlowReport) >= nativeReportInterval) {
		report(nativeCallReport{event: "slow", stats: stats})
		w.reportedSlow = stats.SlowCalls
		w.reportedWaits = stats.SlowWaits
		w.lastSlowReport = now
	}
}

func logNativeCallReport(report nativeCallReport) {
	s := report.stats
	switch report.event {
	case "stalled":
		log.Printf("[rclib] native call stalled operation=%s elapsed=%s waiting=%d; call remains active", report.operation, s.ActiveDuration.Round(time.Millisecond), s.WaitingCalls)
	case "resumed":
		log.Printf("[rclib] native call exited after stall operation=%s completed=%d waiting=%d", report.operation, s.CompletedCalls, s.WaitingCalls)
	case "slow":
		log.Printf("[rclib] native timings completed=%d slow_calls=%d slow_waits=%d last_slow_operation=%s last_call=%s last_wait_operation=%s last_wait=%s max_call=%s max_wait=%s",
			s.CompletedCalls, s.SlowCalls, s.SlowWaits, s.LastSlowOperation, s.LastSlowCallDuration.Round(time.Millisecond),
			s.LastWaitOperation, s.LastSlowWaitDuration.Round(time.Millisecond), s.MaxCallDuration.Round(time.Millisecond), s.MaxWaitDuration.Round(time.Millisecond))
	}
}
