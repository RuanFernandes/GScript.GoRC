package rclib

import (
	"testing"
	"time"
)

func TestNativeCallTrackerSeparatesQueueWaitAndExecution(t *testing.T) {
	var tracker nativeCallTracker
	queuedAt := time.Unix(100, 0)
	startedAt := queuedAt.Add(400 * time.Millisecond)
	tracker.queued()
	tracker.started("rc_execute", queuedAt, startedAt)
	tracker.queued()

	active := tracker.snapshot(startedAt.Add(100 * time.Millisecond))
	if active.ActiveOperation != "rc_execute" || active.ActiveCallID == 0 || active.ActiveDuration != 100*time.Millisecond {
		t.Fatalf("unexpected active call: %+v", active)
	}
	if active.WaitingCalls != 1 || active.SlowWaits != 1 || active.MaxWaitDuration != 400*time.Millisecond || active.SlowCalls != 0 {
		t.Fatalf("queue wait was not tracked independently: %+v", active)
	}

	tracker.completed(startedAt, startedAt.Add(600*time.Millisecond))
	completed := tracker.snapshot(startedAt.Add(time.Second))
	if completed.ActiveCallID != 0 || completed.ActiveOperation != "" || completed.ActiveDuration != 0 {
		t.Fatalf("completed call remained active: %+v", completed)
	}
	if completed.CompletedCalls != 1 || completed.SlowCalls != 1 || completed.WaitingCalls != 1 ||
		completed.LastSlowCallDuration != 600*time.Millisecond || completed.MaxCallDuration != 600*time.Millisecond ||
		completed.LastSlowOperation != "rc_execute" || completed.LastWaitOperation != "rc_execute" {
		t.Fatalf("unexpected completed timing counters: %+v", completed)
	}

	tracker.started("rc_process_events", startedAt.Add(time.Second), startedAt.Add(time.Second))
	next := tracker.snapshot(startedAt.Add(time.Second))
	if next.ActiveCallID == active.ActiveCallID || next.WaitingCalls != 0 {
		t.Fatalf("next call did not receive independent identity: %+v", next)
	}
	tracker.completed(startedAt.Add(time.Second), startedAt.Add(time.Second+time.Millisecond))
	final := tracker.snapshot(startedAt.Add(2 * time.Second))
	if final.CompletedCalls != 2 || final.SlowCalls != 1 || final.SlowWaits != 1 || final.MaxCallDuration != 600*time.Millisecond {
		t.Fatalf("fast call changed slow-operation counters: %+v", final)
	}
}

func TestNativeWatchdogRepeatsStallsAndReportsProgress(t *testing.T) {
	var watchdog nativeCallWatchdog
	var reports []nativeCallReport
	report := func(value nativeCallReport) { reports = append(reports, value) }
	started := time.Unix(100, 0)
	stats := NativeCallStats{ActiveOperation: "rc_connect", ActiveCallID: 1}

	stats.ActiveDuration = nativeStallThreshold - time.Millisecond
	watchdog.observe(stats, started.Add(stats.ActiveDuration), report)
	if len(reports) != 0 {
		t.Fatalf("reported a stall before threshold: %+v", reports)
	}
	stats.ActiveDuration = nativeStallThreshold
	watchdog.observe(stats, started.Add(stats.ActiveDuration), report)
	if len(reports) != 1 || reports[0].event != "stalled" {
		t.Fatalf("missing first stall report: %+v", reports)
	}
	stats.ActiveDuration += time.Second
	watchdog.observe(stats, started.Add(stats.ActiveDuration), report)
	if len(reports) != 1 {
		t.Fatalf("stall reports were not rate limited: %+v", reports)
	}
	stats.ActiveDuration = nativeStallThreshold + nativeReportInterval
	watchdog.observe(stats, started.Add(stats.ActiveDuration), report)
	if len(reports) != 2 || reports[1].event != "stalled" {
		t.Fatalf("watchdog stopped reporting a call that never returned: %+v", reports)
	}

	// A different active call proves the previously stalled call exited even if
	// the watchdog did not sample the idle interval between calls.
	stats = NativeCallStats{ActiveOperation: "rc_process_events", ActiveCallID: 2, CompletedCalls: 1}
	watchdog.observe(stats, started.Add(time.Minute), report)
	if len(reports) != 3 || reports[2].event != "resumed" || reports[2].operation != "rc_connect" {
		t.Fatalf("missing native progress report: %+v", reports)
	}
	watchdog.observe(stats, started.Add(2*time.Minute), report)
	if len(reports) != 3 {
		t.Fatalf("progress was reported more than once: %+v", reports)
	}
}

func TestNativeWatchdogSummarizesOnlyChangedSlowCounters(t *testing.T) {
	var watchdog nativeCallWatchdog
	var reports []nativeCallReport
	report := func(value nativeCallReport) { reports = append(reports, value) }
	now := time.Unix(100, 0)
	stats := NativeCallStats{SlowCalls: 1, SlowWaits: 1}
	watchdog.observe(stats, now, report)
	watchdog.observe(stats, now.Add(time.Minute), report)
	if len(reports) != 1 || reports[0].event != "slow" {
		t.Fatalf("unchanged slow counters generated duplicate reports: %+v", reports)
	}
	stats.SlowCalls++
	watchdog.observe(stats, now.Add(time.Second), report)
	if len(reports) != 1 {
		t.Fatalf("changed counters bypassed rate limit: %+v", reports)
	}
	watchdog.observe(stats, now.Add(nativeReportInterval), report)
	if len(reports) != 2 || reports[1].stats.SlowCalls != 2 {
		t.Fatalf("rate limiting lost updated counters: %+v", reports)
	}
}

func TestNativeMonitorReportsAndStopsWhileNativeMutexIsHeld(t *testing.T) {
	// No native library is called. Holding its serialization mutex models a
	// native call which never returns and blocks every subsequent DLL operation.
	dllMu.Lock()
	defer dllMu.Unlock()
	var tracker nativeCallTracker
	now := time.Now()
	tracker.queued()
	tracker.started("rc_connect", now, now)
	tracker.queued()
	ticks := make(chan time.Time, 1)
	stop := make(chan struct{})
	done := make(chan struct{})
	reports := make(chan nativeCallReport, 1)
	go func() {
		defer close(done)
		monitorNativeCalls(stop, ticks, &tracker, func(report nativeCallReport) { reports <- report })
	}()
	ticks <- now.Add(nativeStallThreshold)
	select {
	case report := <-reports:
		if report.event != "stalled" || report.stats.WaitingCalls != 1 {
			t.Errorf("unexpected stalled report: %+v", report)
		}
	case <-time.After(time.Second):
		t.Error("watchdog could not report while native mutex was held")
	}
	close(stop)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("watchdog could not stop while native mutex was held")
	}
}

func TestNativeCallBoundaryReleasesSerializationAfterPanic(t *testing.T) {
	func() {
		defer func() {
			if recover() == nil {
				t.Error("expected simulated native-boundary panic")
			}
		}()
		started := beginNativeCall("rc_test_panic")
		defer endNativeCall(started)
		panic("simulated native-boundary panic")
	}()
	if stats := ReadNativeCallStats(); stats.ActiveCallID != 0 || stats.ActiveOperation != "" {
		t.Fatalf("panic left diagnostics in an active state: %+v", stats)
	}
	if !dllMu.TryLock() {
		t.Fatal("panic left the native serialization mutex locked")
	}
	dllMu.Unlock()
}
