package connection

import (
	"testing"

	"graal-rc/rclib"
)

func TestStatusNativeProbesDoNotHoldSessionMutex(t *testing.T) {
	s, _ := testSession(t)
	assertUnlocked := func() {
		t.Helper()
		if !s.mu.TryLock() {
			t.Fatal("native status probe was called while the session mutex was held")
		}
		s.mu.Unlock()
	}
	st := s.status(func() (string, error) {
		assertUnlocked()
		return "test.dll", nil
	}, func(h rclib.Handle) (bool, bool) {
		assertUnlocked()
		if _, err := s.SessionIdentity(); err != nil {
			t.Fatalf("callback could not read the active session identity: %v", err)
		}
		return h == 1, true
	})
	if !st.Loaded || !st.Connected || !st.Authenticated || st.Account != "account" {
		t.Fatalf("stable session status = %+v", st)
	}
}

func TestStatusDiscardsNativeResultAfterServerSwitch(t *testing.T) {
	s, _ := testSession(t)
	st := s.status(func() (string, error) { return "test.dll", nil }, func(rclib.Handle) (bool, bool) {
		s.mu.Lock()
		s.startSessionLocked()
		s.creds.Account = "replacement"
		s.serverName = "Replacement"
		s.mu.Unlock()
		return true, true
	})
	if st != (Status{Loaded: true, DLLPath: "test.dll"}) {
		t.Fatalf("status attributed an old native result to a session after switching: %+v", st)
	}
}

func TestStatusDiscardsNativeResultAfterDisconnect(t *testing.T) {
	s, scope := testSession(t)
	st := s.status(func() (string, error) { return "test.dll", nil }, func(rclib.Handle) (bool, bool) {
		s.disconnectSession(scope.handle, scope.epoch, "connection closed")
		return true, true
	})
	if st.Connected || st.Authenticated || st.ServerName != "" || st.Account != "" {
		t.Fatalf("disconnected native snapshot was published as active: %+v", st)
	}
}

func TestNCStatusNativeProbeDoesNotHoldSessionMutex(t *testing.T) {
	s, _ := testSession(t)
	want := NCStatus{HasNc: true, Connected: true, Authenticated: true}
	got := s.ncStatus(func(rclib.Handle) NCStatus {
		if !s.mu.TryLock() {
			t.Fatal("NC status entered the DLL with the session mutex held")
		}
		s.mu.Unlock()
		return want
	})
	if got != want {
		t.Fatalf("NC status = %+v; want %+v", got, want)
	}
}

func TestNCStatusDiscardsResultAfterHandleReuse(t *testing.T) {
	s, _ := testSession(t)
	got := s.ncStatus(func(rclib.Handle) NCStatus {
		s.mu.Lock()
		s.startSessionLocked()
		s.mu.Unlock()
		return NCStatus{HasNc: true, Connected: true, Authenticated: true}
	})
	if got != (NCStatus{}) {
		t.Fatalf("NC status survived a session switch on the same handle: %+v", got)
	}
}

func TestInactiveStatusDoesNotQueryNativeConnection(t *testing.T) {
	s, scope := testSession(t)
	s.disconnectSession(scope.handle, scope.epoch, "connection closed")
	st := s.status(func() (string, error) { return "test.dll", nil }, func(rclib.Handle) (bool, bool) {
		t.Fatal("inactive session queried its old native connection")
		return false, false
	})
	if st.Account != "account" || st.ServerName != "" || st.Connected || st.Authenticated {
		t.Fatalf("inactive status lost the in-memory login state: %+v", st)
	}
	s.ncStatus(func(rclib.Handle) NCStatus {
		t.Fatal("inactive NC status queried its old native connection")
		return NCStatus{}
	})
}
