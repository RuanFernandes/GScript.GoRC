package connection

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"graal-rc/rclib"
)

func testSession(t *testing.T) (*Service, sessionScope) {
	t.Helper()
	s := NewService()
	s.handle = 1
	s.serverName = "Example"
	s.serverEndpoint = "127.0.0.1:14900"
	s.lastServerName = "H Example"
	s.creds = Credentials{Account: "account", Password: "test-secret"}
	s.startSessionLocked()
	scope, err := s.captureSession()
	if err != nil {
		t.Fatal(err)
	}
	return s, scope
}

func TestRunScriptFetchJobsReportsIncompleteSnapshot(t *testing.T) {
	readErr := errors.New("script request timed out")
	jobs := []scriptFetchJob{
		{stype: "weapon", key: "received", name: "received"},
		{stype: "weapon", key: "missing", name: "missing"},
	}
	replies, err := runScriptFetchJobs(context.Background(), jobs, 2, func(_ context.Context, job scriptFetchJob) (rclib.ScriptReply, error) {
		if job.key == "missing" {
			return rclib.ScriptReply{}, readErr
		}
		return rclib.ScriptReply{Type: job.stype, Name: job.name, Script: "content"}, nil
	}, nil)
	if !errors.Is(err, ErrIncompleteScriptSnapshot) || !errors.Is(err, readErr) {
		t.Fatalf("fetch error = %v; want incomplete snapshot preserving the read failure", err)
	}
	if len(replies) != 1 || replies[0].Name != "received" {
		t.Fatalf("partial replies = %+v; want the successful script", replies)
	}
}

func TestRunScriptFetchJobsDoesNotTreatAllTimeoutsAsEmptyServer(t *testing.T) {
	jobs := []scriptFetchJob{{stype: "class", key: "slow", name: "slow"}}
	replies, err := runScriptFetchJobs(context.Background(), jobs, 1, func(context.Context, scriptFetchJob) (rclib.ScriptReply, error) {
		return rclib.ScriptReply{}, context.DeadlineExceeded
	}, nil)
	if len(replies) != 0 || !errors.Is(err, ErrIncompleteScriptSnapshot) {
		t.Fatalf("fetch returned %d replies, %v; want an incomplete snapshot", len(replies), err)
	}
}

func TestRetryScriptFailuresReadsNeighboursOfALostReplyOnTheirOwn(t *testing.T) {
	failures := []scriptFetchFailure{
		{job: scriptFetchJob{stype: "weapon", key: "neighbour", name: "neighbour"}, err: errors.New("script request timed out")},
		{job: scriptFetchJob{stype: "weapon", key: "Staff/News", name: "Staff/News"}, err: errors.New("script request timed out")},
		{job: scriptFetchJob{stype: "npc", key: "1004", name: "Guard"}, err: errors.New("script request timed out")},
	}
	var order []string
	recovered, unreadable, err := retryScriptFailures(context.Background(), failures, func(_ context.Context, job scriptFetchJob) (rclib.ScriptReply, error) {
		order = append(order, job.stype+":"+job.key)
		if job.key == "Staff/News" {
			return rclib.ScriptReply{}, errors.New("script request timed out")
		}
		return rclib.ScriptReply{Type: job.stype, Script: "content"}, nil
	})
	if err != nil {
		t.Fatalf("retry error = %v", err)
	}
	if got := fmt.Sprint(order); got != "[weapon:neighbour weapon:Staff/News npc:1004]" {
		t.Fatalf("retry order = %s; each failed script is read again, in order", got)
	}
	if len(recovered) != 2 || recovered[0].Type != "weapon" {
		t.Fatalf("recovered = %+v; want the neighbour and the NPC", recovered)
	}
	if recovered[1].Type != "npc" || recovered[1].Name != "Guard" {
		t.Fatalf("recovered NPC = %+v; the name of the cached list is kept", recovered[1])
	}
	if len(unreadable) != 1 || unreadable[0].job.key != "Staff/News" {
		t.Fatalf("unreadable = %+v; want only the script that failed again", unreadable)
	}
}

func TestRetryScriptFailuresStopsWhenTheConnectionGoesAway(t *testing.T) {
	failures := []scriptFetchFailure{
		{job: scriptFetchJob{stype: "weapon", key: "a", name: "a"}, err: errors.New("script request timed out")},
		{job: scriptFetchJob{stype: "weapon", key: "b", name: "b"}, err: errors.New("script request timed out")},
	}
	for _, lost := range []error{errNCUnavailable, errConnectionSessionChanged} {
		calls := 0
		_, unreadable, err := retryScriptFailures(context.Background(), failures, func(context.Context, scriptFetchJob) (rclib.ScriptReply, error) {
			calls++
			return rclib.ScriptReply{}, lost
		})
		if !errors.Is(err, lost) || calls != 1 || len(unreadable) != 0 {
			t.Fatalf("%v: err=%v calls=%d unreadable=%d; a lost connection must end the pass, not mark scripts unreadable", lost, err, calls, len(unreadable))
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := retryScriptFailures(ctx, failures, func(context.Context, scriptFetchJob) (rclib.ScriptReply, error) {
		t.Fatal("a canceled context still started a read")
		return rclib.ScriptReply{}, nil
	}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled retry error = %v", err)
	}
}

func TestUnreadableScriptsErrorIsAnIncompleteSnapshotThatNamesEachScript(t *testing.T) {
	err := error(newUnreadableScriptsError([]scriptFetchFailure{
		{job: scriptFetchJob{stype: "weapon", key: "Staff/News", name: "Staff/News"}, err: errors.New("script request timed out")},
		{job: scriptFetchJob{stype: "npc", key: "1004", name: "Guard"}, err: errors.New("script request timed out")},
	}, 3702))
	if !errors.Is(err, ErrIncompleteScriptSnapshot) {
		t.Fatal("a caller that does not know about unreadable scripts must still see an incomplete snapshot")
	}
	var unreadable *UnreadableScriptsError
	if !errors.As(err, &unreadable) {
		t.Fatal("the typed error is not reachable")
	}
	scripts, reasons := unreadable.UnreadableScripts()
	if len(scripts) != 2 || scripts[0].Type != "weapon" || scripts[0].Name != "Staff/News" || scripts[1].Type != "npc" || scripts[1].ID != 1004 || scripts[1].Name != "Guard" {
		t.Fatalf("scripts = %+v", scripts)
	}
	if len(reasons) != 2 || reasons[0] != "script request timed out" {
		t.Fatalf("reasons = %v", reasons)
	}
	for _, want := range []string{"2 of 3702", "weapon:Staff/News", "npc:Guard"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not mention %q", err.Error(), want)
		}
	}
	// The returned copies cannot be used to change the error.
	scripts[0].Name = "changed"
	if again, _ := unreadable.UnreadableScripts(); again[0].Name != "Staff/News" {
		t.Fatal("UnreadableScripts returned the error's own slice")
	}
}

func TestDisconnectSessionCancelsBackendWaitersAndPreservesReconnectTarget(t *testing.T) {
	s, scope := testSession(t)
	script, _ := s.registerPending("weapon:example")
	editor, _ := s.registerEditor("rights:self")
	file, _ := s.registerFile("example.txt")
	if !s.disconnectSession(scope.handle, scope.epoch, "connection reset") {
		t.Fatal("active disconnect was ignored")
	}
	for name, done := range map[string]<-chan struct{}{"script": script.done, "editor": editor.done, "file": file.done, "session": scope.done} {
		select {
		case <-done:
		default:
			t.Fatalf("%s was not canceled synchronously", name)
		}
	}
	for name, err := range map[string]error{"script": script.err, "editor": editor.err, "file": file.err} {
		if !errors.Is(err, ErrConnectionSessionChanged) {
			t.Fatalf("%s error = %v; want session changed", name, err)
		}
	}
	if !s.CanReconnect() {
		t.Fatal("transport drop discarded the reconnect target")
	}
	if _, err := s.SessionIdentity(); !errors.Is(err, ErrConnectionSessionChanged) {
		t.Fatalf("disconnected session identity error = %v", err)
	}
	if s.disconnectSession(scope.handle, scope.epoch, "late old callback") {
		t.Fatal("stale disconnect callback changed session state")
	}
}

func TestQueuedDownloadCannotAdoptReplacementSession(t *testing.T) {
	s, scope := testSession(t)
	release, err := s.acquireFileDownload(context.Background(), scope)
	if err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		releaseQueued, err := s.acquireFileDownload(context.Background(), scope)
		if releaseQueued != nil {
			releaseQueued()
		}
		result <- err
	}()
	s.mu.Lock()
	s.startSessionLocked()
	s.mu.Unlock()
	release()
	select {
	case err := <-result:
		if !errors.Is(err, ErrConnectionSessionChanged) {
			t.Fatalf("queued download error = %v; want session changed", err)
		}
	case <-time.After(time.Second):
		t.Fatal("queued download remained blocked on a replaced session")
	}
}

func TestDownloadQueueAndDispatchWaitRespectCancellation(t *testing.T) {
	s, scope := testSession(t)
	release, err := s.acquireFileDownload(context.Background(), scope)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.acquireFileDownload(ctx, scope); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled queue error = %v", err)
	}
	s.fileTransferDispatchMu.Lock()
	defer s.fileTransferDispatchMu.Unlock()
	if err := s.lockFileDispatch(ctx, scope); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled dispatch error = %v", err)
	}
}

func TestActiveFileTransferReleasesSlotWhenSessionEnds(t *testing.T) {
	s, scope := testSession(t)
	release, err := s.acquireFileDownload(context.Background(), scope)
	if err != nil {
		t.Fatal(err)
	}
	waiter, _ := s.registerFile("active.txt")
	finished := make(chan struct{})
	go func() {
		s.finishFileTransfer(scope, "active.txt", waiter, release)
		close(finished)
	}()
	s.disconnectSession(scope.handle, scope.epoch, "connection reset")
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("native transfer slot was retained after the session ended")
	}
	if len(s.fileDownloadSlot) != 0 {
		t.Fatal("native transfer slot was not released")
	}
}

func TestSessionIdentityRejectsReusedHandleAfterTransition(t *testing.T) {
	s, _ := testSession(t)
	identity, err := s.SessionIdentity()
	if err != nil || !s.IsSessionCurrent(identity) {
		t.Fatalf("active identity unavailable: %v", err)
	}
	s.mu.Lock()
	s.startSessionLocked()
	s.mu.Unlock()
	if s.IsSessionCurrent(identity) {
		t.Fatal("identity remained valid after the same handle was reused")
	}
}

func TestDownloadDoesNotQueueWhileServerSelectionIsInProgress(t *testing.T) {
	s, scope := testSession(t)
	release, err := s.acquireFileDownload(context.Background(), scope)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	s.mu.Lock()
	s.startSessionLocked()
	s.serverName = ""
	s.mu.Unlock()
	result := make(chan error, 1)
	go func() {
		_, err := s.DownloadFileContext(context.Background(), "old-server.txt")
		result <- err
	}()
	select {
	case err := <-result:
		if !errors.Is(err, ErrConnectionSessionChanged) {
			t.Fatalf("download during server selection returned %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("request from a closing window queued against the incoming server")
	}
}

func TestNCHandshakeAndEndpointRefreshDoNotOverlapRetries(t *testing.T) {
	s := NewService()
	now := time.Now()
	s.ncConnectionAttempted = true
	s.ncAttemptDeadline = now.Add(ncConnectionTimeout)
	if s.claimNCAutomaticReconnectAt(now, 0) || !s.nextNCAutomaticReconnect.IsZero() {
		t.Fatal("retry overlapped an initial handshake")
	}
	s.ncAttemptDeadline = time.Time{}
	s.nextNCConnectAttempt = now.Add(ncReconnectLocationDelay)
	if s.claimNCAutomaticReconnectAt(now, 0) || !s.nextNCAutomaticReconnect.IsZero() {
		t.Fatal("retry overlapped a pending endpoint refresh")
	}
	s.nextNCConnectAttempt = time.Time{}
	if s.claimNCAutomaticReconnectAt(now, 0) {
		t.Fatal("failed initial connection skipped its backoff")
	}
	if !s.claimNCAutomaticReconnectAt(now.Add(ncRetryBaseDelay), 0) {
		t.Fatal("initial connection failure did not receive a retry")
	}
}

func TestNCFlappingDoesNotResetRetryBudget(t *testing.T) {
	s := NewService()
	now := time.Now()
	s.ncReconnectAttempts = 3
	s.markNCConnectionObservedAt(now)
	s.markNCConnectionObservedAt(now.Add(5 * time.Second))
	if s.ncReconnectAttempts != 3 {
		t.Fatal("brief NC authentication reset its retry budget")
	}
	s.markNCConnectionObservedAt(now.Add(ncStableResetInterval))
	if s.ncReconnectAttempts != 0 {
		t.Fatal("stable NC connection did not reset its retry budget")
	}
}

func TestNCRetryDelayIsBoundedAndJittered(t *testing.T) {
	for attempt := 0; attempt < 8; attempt++ {
		base, jittered := ncRetryDelay(attempt, 0), ncRetryDelay(attempt, 1)
		if base < ncRetryBaseDelay || jittered < base || jittered > ncRetryMaxDelay || jittered > base+base/4 {
			t.Fatalf("invalid backoff attempt=%d base=%s jittered=%s", attempt, base, jittered)
		}
	}
	if ncRetryDelay(0, 1) == ncRetryDelay(0, 0) {
		t.Fatal("retry schedule has no jitter")
	}
}

func TestReconnectServerUsesStableNameAndRejectsAmbiguity(t *testing.T) {
	servers := []rclib.Server{{Name: "H Other"}, {Name: "U Example"}}
	index, err := reconnectServerIndex(servers, "H Example")
	if err != nil || index != 1 {
		t.Fatalf("resolved reconnect index=%d err=%v; want the matching name at its new index", index, err)
	}
	for _, entries := range [][]rclib.Server{nil, {{Name: "H Example"}, {Name: "U Example"}}} {
		if _, err := reconnectServerIndex(entries, "H Example"); !errors.Is(err, ErrReconnectUnavailable) {
			t.Fatalf("missing/ambiguous target error = %v", err)
		}
	}
}

func TestReconnectCancellationAndMissingTargetAvoidNativeCalls(t *testing.T) {
	s := NewService()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := s.ReconnectLastServerContext(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled reconnect error = %v", err)
	}
	if err := s.ReconnectLastServer(); !errors.Is(err, ErrReconnectUnavailable) {
		t.Fatalf("empty reconnect target error = %v", err)
	}
}

func TestReconnectOnlyRetriesKnownTransportFailures(t *testing.T) {
	for _, reason := range []string{"connection reset by peer", "server connection timed out", "connection refused", "unexpected EOF"} {
		if !IsTransientDisconnect(reason) {
			t.Fatalf("transport failure %q was not retryable", reason)
		}
	}
	for _, reason := range []string{"", "disconnected by server", "IP not approved", "wrong password", "banned", "kicked after timeout", "You're not staff in any server.", "unknown reason"} {
		if IsTransientDisconnect(reason) {
			t.Fatalf("server rejection/unknown reason %q was retryable", reason)
		}
	}
	if !errors.Is(classifyReconnectError(errors.New("wrong password")), ErrReconnectRejected) {
		t.Fatal("authentication error did not expose the terminal sentinel")
	}
	if IsTransientConnectionError(context.Canceled) || IsTransientConnectionError(ErrReconnectUnavailable) {
		t.Fatal("terminal reconnect state was retryable")
	}
}

func TestReconnectPreservesCallerDeadlineCause(t *testing.T) {
	timedOut, stopTimeout := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer stopTimeout()
	err := reconnectContextError(timedOut, context.Canceled)
	if !errors.Is(err, context.DeadlineExceeded) || !IsTransientConnectionError(err) {
		t.Fatalf("timed-out operation returned %v; want a retryable deadline", err)
	}
	manual, cancel := context.WithCancel(context.Background())
	cancel()
	err = reconnectContextError(manual, context.Canceled)
	if !errors.Is(err, context.Canceled) || IsTransientConnectionError(err) {
		t.Fatalf("manually canceled operation returned retryable error %v", err)
	}
	if got := reconnectContextError(timedOut, ErrReconnectRejected); !errors.Is(got, ErrReconnectRejected) {
		t.Fatalf("deadline replaced a terminal server rejection: %v", got)
	}
}
