package connection

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"graal-rc/internal/folderrights"
	"graal-rc/rclib"
)

// snapshotLockedForTest is a test-only view of the joined set under the lock.
func (s *Service) snapshotLockedForTest() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.snapshotLocked()
}

// leaveDeadlinePassed returns a leave deadline already in the past, used to
// force a deferred leave to be committable by settleChannelLeaves.
func leaveDeadlinePassed() time.Time { return time.Now().Add(-time.Second) }

func TestScriptListsRequestsAreCoalescedAcrossFilters(t *testing.T) {
	s := NewService()
	const callers = 32
	type result struct {
		request *scriptListsRequest
		owner   bool
	}
	start := make(chan struct{})
	results := make(chan result, callers)
	for range callers {
		go func() {
			<-start
			request, owner := s.beginScriptListsRequest()
			results <- result{request: request, owner: owner}
		}()
	}
	close(start)

	var owner *scriptListsRequest
	owners := 0
	requests := make([]*scriptListsRequest, 0, callers)
	for range callers {
		got := <-results
		requests = append(requests, got.request)
		if got.owner {
			owner = got.request
			owners++
		}
	}
	if owners != 1 {
		t.Fatalf("concurrent callers elected %d owners, want exactly 1", owners)
	}
	for _, request := range requests {
		if request != owner {
			t.Fatalf("concurrent callers did not share the owner request")
		}
	}

	want := ScriptLists{
		Weapons: []rclib.Weapon{{Name: "Sword"}},
		Classes: []rclib.Class{{Name: "Warrior"}},
		NPCs:    []rclib.NPC{{ID: 7, Name: "Guard"}},
	}
	s.completeScriptListsRequest(owner, want, nil)

	for _, request := range requests {
		select {
		case <-request.done:
		case <-time.After(time.Second):
			t.Fatal("joined script-list request was not released")
		}
	}
	if len(owner.lists.Weapons) != 1 || owner.lists.Weapons[0].Name != "Sword" {
		t.Fatalf("joined request received %+v, want the completed snapshot", owner.lists)
	}

	next, owns := s.beginScriptListsRequest()
	if !owns || next == owner {
		t.Fatal("a completed request should allow a new snapshot")
	}
}

func TestExecuteRejectsAStoppedEventPump(t *testing.T) {
	s := NewService()
	s.mu.Lock()
	s.handle = 1
	s.mu.Unlock()
	s.pumpMu.Lock()
	s.pumpErr = errors.New("callback fault")
	s.pumpMu.Unlock()

	err := s.Execute("/test")
	if err == nil || !strings.Contains(err.Error(), "event pump unavailable") {
		t.Fatalf("Execute() error = %v, want the event-pump failure", err)
	}
}

func TestFailPumpClearsServerAndPublishesFailure(t *testing.T) {
	s := NewService()
	s.mu.Lock()
	s.handle = 1
	s.serverName = "Testbed3d"
	s.mu.Unlock()
	done := make(chan struct{})
	s.pumpMu.Lock()
	s.pumpDone = done
	s.pumpMu.Unlock()

	type event struct {
		name string
		data []any
	}
	var events []event
	s.SetEmitter(func(name string, data ...any) {
		events = append(events, event{name: name, data: data})
	})
	s.failPump(1, done, errors.New("callback fault"))

	status := s.Status()
	if status.ServerName != "" || status.Connected || status.Authenticated {
		t.Fatalf("failed session status = %+v, want no active server", status)
	}
	if len(events) != 1 || events[0].name != "rc:pumpError" {
		t.Fatalf("pump events = %+v, want one rc:pumpError event", events)
	}
	if len(events[0].data) != 1 || !strings.Contains(events[0].data[0].(string), "callback fault") {
		t.Fatalf("pump event data = %#v, want callback fault", events[0].data)
	}
}

func TestSyncScriptFetchUsesReferenceClientConcurrency(t *testing.T) {
	if ncFetchConcurrency != 16 {
		t.Fatalf("ncFetchConcurrency = %d, want bounded pipeline of 16 requests", ncFetchConcurrency)
	}
}

// TestApplyChannelDelta_LoginBurst proves the reported bug stays fixed: the
// login burst sends Left then Joined for a channel we were never in. The Left
// is ignored so the following Joined keeps the channel.
func TestApplyChannelDelta_LoginBurst(t *testing.T) {
	s := NewService()

	// Left for a never-joined channel: ignored, no snapshot emitted.
	if got := s.applyChannelDelta("#a", "* Left #a"); got != nil {
		t.Fatalf("Left before any Join should be ignored, got snapshot %v", got)
	}
	// Following Joined creates the channel and emits a snapshot.
	if got := s.applyChannelDelta("#a", "* Joined #a"); !eq(got, []string{"#a"}) {
		t.Fatalf("after Join: got %v want [#a]", got)
	}
	// Duplicate Joined is idempotent: no snapshot.
	if s.applyChannelDelta("#a", "* Joined #a") != nil {
		t.Fatalf("duplicate Join should not emit snapshot")
	}
}

func TestFileBrowserPathsMatchCurrentFolderPrefixes(t *testing.T) {
	for _, test := range []struct {
		requested string
		received  string
		want      bool
	}{
		{requested: "emoticon_conf.png", received: "world/images/emoticons/emoticon_conf.png", want: true},
		{requested: "world/images/emoticons/emoticon_conf.png", received: "emoticon_conf.png", want: true},
		{requested: `folder\photo.png`, received: "folder/photo.png", want: true},
		{requested: "folder/photo.png", received: "other/photo.png", want: false},
		{requested: "photo.png", received: "photo.jpg", want: false},
	} {
		if got := fileBrowserPathsMatch(test.requested, test.received); got != test.want {
			t.Fatalf("fileBrowserPathsMatch(%q, %q) = %v, want %v", test.requested, test.received, got, test.want)
		}
	}
}

func TestResolveFileMatchesNativeFolderPrefix(t *testing.T) {
	s := NewService()
	waiter := &fileWait{done: make(chan struct{})}
	s.pendingFiles = map[string]*fileWait{"emoticon_conf.png": waiter}

	s.resolveFile("world/images/emoticons/emoticon_conf.png", []byte("png"))

	select {
	case <-waiter.done:
		if string(waiter.content) != "png" || waiter.err != nil {
			t.Fatalf("resolved file = %q, err = %v", waiter.content, waiter.err)
		}
	default:
		t.Fatal("native path with folder prefix did not resolve the download")
	}
}

func TestRunScriptFetchJobsUsesFixedWorkerPool(t *testing.T) {
	const (
		total       = 2048
		workerCount = 4
	)

	jobs := make([]scriptFetchJob, total)
	for i := range jobs {
		jobs[i] = scriptFetchJob{stype: "weapon", key: string(rune(i)), name: "weapon"}
	}

	var active int32
	var maxActive int32
	var completed int32
	replies, err := runScriptFetchJobs(context.Background(), jobs, workerCount, func(_ context.Context, job scriptFetchJob) (rclib.ScriptReply, error) {
		current := atomic.AddInt32(&active, 1)
		for {
			previous := atomic.LoadInt32(&maxActive)
			if current <= previous || atomic.CompareAndSwapInt32(&maxActive, previous, current) {
				break
			}
		}
		time.Sleep(time.Microsecond)
		atomic.AddInt32(&active, -1)
		atomic.AddInt32(&completed, 1)
		return rclib.ScriptReply{Type: job.stype, Name: job.name}, nil
	}, nil)
	if err != nil {
		t.Fatalf("runScriptFetchJobs returned error: %v", err)
	}
	if len(replies) != total {
		t.Fatalf("replies = %d, want %d", len(replies), total)
	}
	if got := atomic.LoadInt32(&completed); got != total {
		t.Fatalf("completed = %d, want %d", got, total)
	}
	if got := atomic.LoadInt32(&maxActive); got > workerCount {
		t.Fatalf("max active workers = %d, want <= %d", got, workerCount)
	}
}

func TestWaitForWeaponListRequiresNextGeneration(t *testing.T) {
	s := NewService()
	s.weaponListMu.Lock()
	generation := s.weaponListGeneration
	s.weaponListSignal = make(chan struct{})
	s.weaponListMu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	result := make(chan error, 1)
	go func() {
		result <- s.waitForWeaponList(ctx, generation)
	}()

	select {
	case err := <-result:
		t.Fatalf("wait returned before a new list callback: %v", err)
	case <-time.After(20 * time.Millisecond):
	}

	s.markWeaponListReceived(3523)
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("wait returned error after list callback: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("wait did not return after weapon list callback")
	}
}

// TestApplyChannelDelta_PartDebounced proves a PART does not remove the channel
// immediately: it stays joined (pending leave) so a rejoin within the cooldown
// cancels it and the tab survives.
func TestApplyChannelDelta_PartDebounced(t *testing.T) {
	s := NewService()
	s.applyChannelDelta("#a", "* Joined #a")
	s.applyChannelDelta("#b", "* Joined #b")

	// Real part: deferred, no immediate snapshot, channel still joined.
	if got := s.applyChannelDelta("#a", "* Left #a"); got != nil {
		t.Fatalf("Part should be deferred (nil snapshot), got %v", got)
	}
	if got := s.snapshotLockedForTest(); !eq(got, []string{"#a", "#b"}) {
		t.Fatalf("channel still joined after deferred part: got %v want [#a #b]", got)
	}

	// Rejoin within cooldown cancels the pending leave; #a stays joined.
	s.applyChannelDelta("#a", "* Joined #a")
	if got := s.snapshotLockedForTest(); !eq(got, []string{"#a", "#b"}) {
		t.Fatalf("after rejoin: got %v want [#a #b]", got)
	}
}

// TestSettleChannelLeaves proves that once the cooldown elapses the deferred
// leave is committed and the channel is dropped.
func TestSettleChannelLeaves(t *testing.T) {
	s := NewService()
	s.applyChannelDelta("#a", "* Joined #a")
	s.applyChannelDelta("#b", "* Joined #b")
	s.applyChannelDelta("#a", "* Left #a") // schedule leave for #a

	// Force #a's leave deadline into the past, then settle.
	s.mu.Lock()
	if cs := s.channels["#a"]; cs != nil {
		cs.leaveAt = leaveDeadlinePassed()
	}
	s.mu.Unlock()

	s.settleChannelLeaves()
	if got := s.snapshotLockedForTest(); !eq(got, []string{"#b"}) {
		t.Fatalf("after settle: got %v want [#b]", got)
	}
}

// TestApplyChannelDelta_NonMarker proves normal chat lines do not touch the set.
func TestApplyChannelDelta_NonMarker(t *testing.T) {
	s := NewService()
	s.applyChannelDelta("#a", "* Joined #a")
	if got := s.applyChannelDelta("#a", "hello there"); got != nil {
		t.Fatalf("chat line should not emit snapshot, got %v", got)
	}
}

// TestApplyChannelDelta_EmptyChannel proves server-channel lines are ignored.
func TestApplyChannelDelta_EmptyChannel(t *testing.T) {
	s := NewService()
	if got := s.applyChannelDelta("", "* Joined "); got != nil {
		t.Fatalf("empty channel should not emit snapshot, got %v", got)
	}
}

func TestRightsChangedMessageDetection(t *testing.T) {
	for _, text := range []string{
		"[08:51] ruanf has set rights of (offline) player ruanf",
		"ADMIN HAS SET RIGHTS OF (offline) player Test",
	} {
		if !isRightsChangedMessage(text) {
			t.Fatalf("rights notification was not detected: %q", text)
		}
	}
	if got := rightsChangedTarget("[08:51] ruanf has set rights of (offline) player ruanf"); got != "ruanf" {
		t.Fatalf("rights target = %q, want ruanf", got)
	}
	if got := rightsChangedTarget("ADMIN HAS SET RIGHTS OF (online) player Test"); got != "Test" {
		t.Fatalf("online rights target = %q, want Test", got)
	}
	if isRightsChangedMessage("ruanf changed the player rights") {
		t.Fatal("unrelated rights message matched the notification marker")
	}
}

func TestParseLoadedRightsMessage(t *testing.T) {
	withCommunity, ok := parseLoadedRightsMessage("[08:56 ] Repinho loaded the rights of Repinho (Graal5766947)")
	if !ok {
		t.Fatal("community identity message was not parsed")
	}
	if withCommunity.Actor != "Repinho" || withCommunity.Target != "Repinho" || withCommunity.Account != "Graal5766947" || withCommunity.CommunityName != "Repinho" || !withCommunity.ExplicitAccount {
		t.Fatalf("unexpected community identity: %+v", withCommunity)
	}

	withoutCommunity, ok := parseLoadedRightsMessage("[08:57 ] ruanf loaded the rights of ruanf")
	if !ok {
		t.Fatal("account-only identity message was not parsed")
	}
	if withoutCommunity.Actor != "ruanf" || withoutCommunity.Target != "ruanf" || withoutCommunity.Account != "ruanf" || withoutCommunity.CommunityName != "" || withoutCommunity.ExplicitAccount {
		t.Fatalf("unexpected account-only identity: %+v", withoutCommunity)
	}
}

func TestCaptureSelfRightsIdentityIgnoresOtherTarget(t *testing.T) {
	s := NewService()
	s.creds = Credentials{Account: "Repinho", Nickname: "Repinho"}

	self, ok := parseLoadedRightsMessage("Repinho loaded the rights of Repinho (Graal5766947)")
	if !ok {
		t.Fatal("self identity message was not parsed")
	}
	s.captureSelfRightsIdentity(self)

	s.rightsMu.RLock()
	account, community := s.selfRightsAccount, s.selfRightsCommunityName
	s.rightsMu.RUnlock()
	if account != "Graal5766947" || community != "Repinho" {
		t.Fatalf("captured identity = community %q account %q", community, account)
	}

	other, ok := parseLoadedRightsMessage("ruanf loaded the rights of ruanf")
	if !ok {
		t.Fatal("other identity message was not parsed")
	}
	s.captureSelfRightsIdentity(other)

	s.rightsMu.RLock()
	account, community = s.selfRightsAccount, s.selfRightsCommunityName
	s.rightsMu.RUnlock()
	if account != "Graal5766947" || community != "Repinho" {
		t.Fatalf("other player's identity replaced self identity: community %q account %q", community, account)
	}
}

func TestCaptureSelfRightsIdentityWithoutCommunity(t *testing.T) {
	s := NewService()
	s.creds = Credentials{Account: "ruanf", Nickname: "ruanf"}

	message, ok := parseLoadedRightsMessage("ruanf loaded the rights of ruanf")
	if !ok {
		t.Fatal("account-only identity message was not parsed")
	}
	s.captureSelfRightsIdentity(message)

	s.rightsMu.RLock()
	account, community := s.selfRightsAccount, s.selfRightsCommunityName
	s.rightsMu.RUnlock()
	if account != "ruanf" || community != "" {
		t.Fatalf("captured account-only identity = community %q account %q", community, account)
	}
}

func TestRequireBanPlayersRightFailsClosed(t *testing.T) {
	s := NewService()
	if err := s.RequireBanPlayersRight(); err == nil {
		t.Fatal("unloaded rights must not authorize ban actions")
	}

	s.rightsMu.Lock()
	s.selfRightsLoaded = true
	s.selfStaffRights = 1 << banPlayersRightBit
	s.rightsMu.Unlock()
	if err := s.RequireBanPlayersRight(); err != nil {
		t.Fatalf("Ban players right should authorize the action: %v", err)
	}

	s.rightsMu.Lock()
	s.selfStaffRights = 0
	s.rightsMu.Unlock()
	if err := s.RequireBanPlayersRight(); err == nil {
		t.Fatal("missing Ban players right must be rejected")
	}
}

func TestSelfAccountPrefersCanonicalRightsAccount(t *testing.T) {
	s := NewService()
	s.creds = Credentials{Account: "login-alias"}
	if got := s.SelfAccount(); got != "login-alias" {
		t.Fatalf("startup account = %q, want login-alias", got)
	}

	s.rightsMu.Lock()
	s.selfRightsAccount = "Graal123"
	s.rightsMu.Unlock()
	if got := s.SelfAccount(); got != "Graal123" {
		t.Fatalf("canonical account = %q, want Graal123", got)
	}
}

func TestStatusReportsScriptWriteAccessFromFolderRights(t *testing.T) {
	access, err := folderrights.Parse("r WEAPONS/*\nr CLASSES/*\nr NPCS/*")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	s := NewService()
	s.rightsMu.Lock()
	s.selfRights = access
	s.selfRightsLoaded = true
	s.rightsMu.Unlock()
	if status := s.Status(); !status.RightsReady || status.ScriptWriteAccess {
		t.Fatalf("read-only status = %+v", status)
	}

	access, err = folderrights.Parse("rw WEAPONS/*")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	s.rightsMu.Lock()
	s.selfRights = access
	s.rightsMu.Unlock()
	if status := s.Status(); !status.ScriptWriteAccess {
		t.Fatalf("write status = %+v", status)
	}
}

func TestReplaceSelfFolderRightsOnlyReportsRealChanges(t *testing.T) {
	readOnly, err := folderrights.Parse("r WEAPONS/*")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	s := NewService()
	var events []struct {
		name string
		data []any
	}
	s.SetEmitter(func(name string, data ...any) {
		events = append(events, struct {
			name string
			data []any
		}{name: name, data: data})
	})

	if stateChanged, permissionChanged := s.replaceSelfFolderRights(readOnly, 0, "Graal123", "Testbed3d"); !stateChanged || permissionChanged {
		t.Fatalf("initial rights load = stateChanged %v permissionChanged %v, want true false", stateChanged, permissionChanged)
	}
	if len(events) != 0 {
		t.Fatalf("rights replacement helper must not emit by itself, got %d events", len(events))
	}
	if stateChanged, permissionChanged := s.replaceSelfFolderRights(readOnly, 0, "Graal123", "Testbed3d"); stateChanged || permissionChanged {
		t.Fatalf("identical rights refresh = stateChanged %v permissionChanged %v, want false false", stateChanged, permissionChanged)
	}

	writeAccess, err := folderrights.Parse("rw WEAPONS/*")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if stateChanged, permissionChanged := s.replaceSelfFolderRights(writeAccess, 0, "Graal123", "Testbed3d"); !stateChanged || !permissionChanged {
		t.Fatalf("changed folder rights = stateChanged %v permissionChanged %v, want true true", stateChanged, permissionChanged)
	}
	if stateChanged, permissionChanged := s.replaceSelfFolderRights(writeAccess, 0, "Graal123", "Testbed3d"); stateChanged || permissionChanged {
		t.Fatalf("repeated changed rights refresh = stateChanged %v permissionChanged %v, want false false", stateChanged, permissionChanged)
	}

	// Disconnecting clears the active cache, but a reconnect to the same server
	// must still compare against the last successful server snapshot.
	s.clearSelfFolderRights()
	if stateChanged, permissionChanged := s.replaceSelfFolderRights(writeAccess, 0, "Graal123", "Testbed3d"); !stateChanged || permissionChanged {
		t.Fatalf("reconnect with unchanged rights = stateChanged %v permissionChanged %v, want true false", stateChanged, permissionChanged)
	}
	if stateChanged, permissionChanged := s.replaceSelfFolderRights(readOnly, 0, "Graal123", "Testbed3d"); !stateChanged || !permissionChanged {
		t.Fatalf("reconnect with changed rights = stateChanged %v permissionChanged %v, want true true", stateChanged, permissionChanged)
	}
}

func eq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
