package connection

import "graal-rc/rclib"

// Status probes run without Service.mu: native event processing holds the DLL
// lock while callbacks acquire mu. Holding mu across a native probe would invert
// that order and deadlock polling against the event pump.
func (s *Service) status(dllPath func() (string, error), readNative func(rclib.Handle) (bool, bool)) Status {
	s.pumpMu.Lock()
	pumpFailed := s.pumpErr != nil
	s.pumpMu.Unlock()

	s.mu.Lock()
	scope := sessionScope{handle: s.handle, epoch: s.serverEpoch, done: s.sessionDone}
	st := Status{Account: s.creds.Account, Nickname: s.creds.Nickname, ServerName: s.serverName}
	active := scope.handle != 0 && st.ServerName != "" && !sessionCanceled(scope.done) && !pumpFailed
	s.mu.Unlock()

	if path, err := dllPath(); err == nil {
		st.Loaded = true
		st.DLLPath = path
	}
	if active {
		st.Connected, st.Authenticated = readNative(scope.handle)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.handle != scope.handle || s.serverEpoch != scope.epoch {
		// A native read may have waited for a disconnect or a server switch. Its
		// result belongs to the old session; only the global DLL metadata survives.
		return Status{Loaded: st.Loaded, DLLPath: st.DLLPath}
	}
	s.rightsMu.RLock()
	st.RealAccount = s.selfRightsAccount
	st.CommunityName = s.selfRightsCommunityName
	st.Rights = s.selfStaffRights
	st.RightsReady = s.selfRightsLoaded
	st.CanBanPlayers = s.selfRightsLoaded && s.selfStaffRights&(1<<banPlayersRightBit) != 0
	st.ScriptWriteAccess = s.selfRightsLoaded && s.selfRights.HasWriteAccessForScriptTypes("weapon", "class", "npc")
	s.rightsMu.RUnlock()
	return st
}

func (s *Service) ncStatus(readNative func(rclib.Handle) NCStatus) NCStatus {
	scope, err := s.captureSession()
	if err != nil {
		return NCStatus{}
	}
	st := readNative(scope.handle)
	if s.checkSession(scope) != nil {
		return NCStatus{}
	}
	return st
}
