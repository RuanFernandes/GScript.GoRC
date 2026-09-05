package connection

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"graal-rc/rclib"
)

var (
	// ErrIncompleteScriptSnapshot means at least one listed script could not be
	// read. Partial replies are useful for updating those files, never for
	// deciding that an omitted file was deleted on the server.
	ErrIncompleteScriptSnapshot = errors.New("script snapshot is incomplete")
	ErrReconnectUnavailable     = errors.New("no unambiguous previous server is available")
	ErrReconnectRejected        = errors.New("server rejected reconnection")
	ErrConnectionSessionChanged = errConnectionSessionChanged
)

// SessionIdentity contains no credentials and can safely identify local drafts.
// Epoch is process-local and prevents work started before a transition from
// opening a window or dispatching a request in the replacement session.
type SessionIdentity struct {
	Account            string `json:"account"`
	ServerName         string `json:"serverName"`
	ListserverEndpoint string `json:"listserverEndpoint"`
	ServerEndpoint     string `json:"serverEndpoint"`
	Epoch              uint64 `json:"epoch"`
}

func (s *Service) SessionIdentity() (SessionIdentity, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.handle == 0 || s.serverName == "" || sessionCanceled(s.sessionDone) {
		return SessionIdentity{}, errConnectionSessionChanged
	}
	host, port := s.creds.Host, s.creds.Port
	if host == "" {
		host = rclib.DefaultListserverHost
	}
	if port == 0 {
		port = rclib.DefaultListserverPort
	}
	return SessionIdentity{
		Account: s.creds.Account, ServerName: s.serverName,
		ListserverEndpoint: net.JoinHostPort(host, strconv.Itoa(port)),
		ServerEndpoint:     s.serverEndpoint, Epoch: s.serverEpoch,
	}, nil
}

func (s *Service) IsSessionCurrent(identity SessionIdentity) bool {
	current, err := s.SessionIdentity()
	return err == nil && current == identity
}

func sessionCanceled(done <-chan struct{}) bool {
	select {
	case <-done:
		return true
	default:
		return false
	}
}

// Caller holds mu. A closed channel is retained until a fresh session starts,
// so requests that arrive after a disconnect cannot adopt a live cancellation
// signal while the old native handle is still being cleaned up.
func (s *Service) invalidateSessionLocked() {
	s.serverEpoch++
	if s.sessionDone != nil && !sessionCanceled(s.sessionDone) {
		close(s.sessionDone)
	}
}

func (s *Service) startSessionLocked() {
	s.invalidateSessionLocked()
	s.sessionDone = make(chan struct{})
}

type sessionScope struct {
	handle rclib.Handle
	epoch  uint64
	done   <-chan struct{}
}

func (s *Service) captureSession() (sessionScope, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.handle == 0 || s.serverName == "" || sessionCanceled(s.sessionDone) {
		return sessionScope{}, errConnectionSessionChanged
	}
	if s.sessionDone == nil {
		s.sessionDone = make(chan struct{})
	}
	return sessionScope{handle: s.handle, epoch: s.serverEpoch, done: s.sessionDone}, nil
}

func (s *Service) checkSession(scope sessionScope) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if scope.handle != s.handle || scope.epoch != s.serverEpoch || sessionCanceled(scope.done) {
		return errConnectionSessionChanged
	}
	return nil
}

// disconnectSession cancels backend work before an event is forwarded to any
// webview. Native callbacks from an older server cannot invalidate its successor.
func (s *Service) disconnectSession(h rclib.Handle, epoch uint64, reason string) bool {
	s.mu.Lock()
	if s.handle != h || s.serverEpoch != epoch {
		s.mu.Unlock()
		return false
	}
	s.serverName = ""
	s.serverEndpoint = ""
	s.invalidateSessionLocked()
	s.ncConnectionAttempted = true
	s.nextNCConnectAttempt = time.Time{}
	s.nextNCKeepalive = time.Time{}
	s.ncConnectionWasUp = false
	s.resetNCRetryLocked()
	s.mu.Unlock()
	s.cancelWaiters(fmt.Errorf("%w: disconnected: %s", errConnectionSessionChanged, reason))
	return true
}

func (s *Service) lockFileDispatch(ctx context.Context, scope sessionScope) error {
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := s.checkSession(scope); err != nil {
			return err
		}
		if s.fileTransferDispatchMu.TryRLock() {
			if err := s.checkSession(scope); err != nil {
				s.fileTransferDispatchMu.RUnlock()
				return err
			}
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-scope.done:
			return errConnectionSessionChanged
		case <-ticker.C:
		}
	}
}

// acquireFileDownload holds only the native transfer slot, not a goroutine
// blocked on a mutex. Waiting callers can leave immediately after cancellation
// and cannot adopt a different server when the previous transfer finishes.
func (s *Service) acquireFileDownload(ctx context.Context, scope sessionScope) (func(), error) {
	s.mu.Lock()
	if s.fileDownloadSlot == nil {
		s.fileDownloadSlot = make(chan struct{}, 1)
	}
	slot := s.fileDownloadSlot
	s.mu.Unlock()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-scope.done:
		return nil, errConnectionSessionChanged
	case slot <- struct{}{}:
	}
	release := func() { <-slot }
	if err := ctx.Err(); err != nil {
		release()
		return nil, err
	}
	if err := s.checkSession(scope); err != nil {
		release()
		return nil, err
	}
	return release, nil
}

// CanReconnect is an in-memory check, including after an unexpected drop. It
// does not call the native library, which may be unavailable during recovery.
func (s *Service) CanReconnect() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.creds.Account != "" && s.creds.Password != "" && s.lastServerName != ""
}

func (s *Service) ReconnectLastServer() error {
	return s.ReconnectLastServerContext(context.Background())
}

// ReconnectLastServerContext creates a fresh native connection and resolves the
// server against the freshly authenticated list. No writes or commands from the
// previous session are replayed. A caller may retry transport errors separately.
func (s *Service) ReconnectLastServerContext(ctx context.Context) (resultErr error) {
	if ctx == nil {
		ctx = context.Background()
	}
	defer func() { resultErr = reconnectContextError(ctx, resultErr) }()
	if err := ctx.Err(); err != nil {
		return err
	}
	operation := s.beginLifecycleOperation()
	defer s.endLifecycleOperation(operation)
	if err := ctx.Err(); err != nil {
		operation.cancel()
		return err
	}
	stop := context.AfterFunc(ctx, operation.cancel)
	defer stop()
	s.fileTransferDispatchMu.Lock()
	defer s.fileTransferDispatchMu.Unlock()
	s.mu.Lock()
	creds, target, protocol := s.creds, s.lastServerName, s.newProtocol
	s.mu.Unlock()
	if creds.Account == "" || creds.Password == "" || target == "" {
		return ErrReconnectUnavailable
	}
	servers, err := s.login(operation.ctx, creds)
	if err != nil {
		return classifyReconnectError(err)
	}
	index, err := reconnectServerIndex(servers, target)
	if err != nil {
		return err
	}
	s.mu.Lock()
	h := s.handle
	s.mu.Unlock()
	if err := operationErr(operation.ctx); err != nil {
		return err
	}
	if err := rclib.SetNewProtocol(h, protocol); err != nil {
		return err
	}
	return classifyReconnectError(s.connectToServer(operation.ctx, index))
}

func reconnectContextError(ctx context.Context, err error) error {
	// The lifecycle operation uses a cancellation-only context so newer manual
	// operations can supersede it. Restore the caller's timeout cause; otherwise
	// the recovery supervisor would mistake a timeout for an explicit logout.
	if errors.Is(err, context.Canceled) && ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}

func reconnectServerIndex(servers []rclib.Server, target string) (int, error) {
	index := -1
	for i, server := range servers {
		if !strings.EqualFold(displayServerName(server.Name), displayServerName(target)) {
			continue
		}
		if index >= 0 {
			return -1, fmt.Errorf("%w: server %q has multiple matches", ErrReconnectUnavailable, target)
		}
		index = i
	}
	if index < 0 {
		return -1, fmt.Errorf("%w: server %q is no longer listed", ErrReconnectUnavailable, target)
	}
	return index, nil
}

func rejectedConnectionReason(reason string) bool {
	reason = strings.ToLower(reason)
	for _, term := range []string{"password", "banned", "ban ", "kick", "denied", "rejected", "not approved", "not allowed", "not authorized", "unauthorized", "no rights", "not staff", "authentication failed", "invalid account"} {
		if strings.Contains(reason, term) {
			return true
		}
	}
	return false
}

func classifyReconnectError(err error) error {
	if err != nil && rejectedConnectionReason(err.Error()) {
		return fmt.Errorf("%w: %w", ErrReconnectRejected, err)
	}
	return err
}

// IsTransientDisconnect deliberately accepts known transport failures only.
// Server kicks, authorization failures and unknown server messages require an
// explicit user action instead of reconnecting against a server's decision.
func IsTransientDisconnect(reason string) bool {
	if rejectedConnectionReason(reason) {
		return false
	}
	reason = strings.ToLower(strings.TrimSpace(reason))
	if reason == "eof" || reason == "unexpected eof" {
		return true
	}
	for _, term := range []string{"timed out", "timeout", "connection reset", "connection refused", "network unreachable", "network is unreachable", "host unreachable", "connection closed", "remote host closed", "broken pipe", "connection aborted"} {
		if strings.Contains(reason, term) {
			return true
		}
	}
	return false
}

func IsTransientConnectionError(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, ErrReconnectUnavailable) || errors.Is(err, ErrReconnectRejected) {
		return false
	}
	return errors.Is(err, context.DeadlineExceeded) || IsTransientDisconnect(err.Error())
}
