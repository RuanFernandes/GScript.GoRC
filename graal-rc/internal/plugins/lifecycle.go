package plugins

import "net/http"

// CloseAllResources releases resources that are owned by plugin capabilities.
// It is called during application shutdown as a final safety net, including
// resources left behind by a plugin that failed while unloading.
func (m *Manager) CloseAllResources() {
	if m == nil {
		return
	}

	m.socketsMu.Lock()
	sockets := make([]*pluginSocket, 0, len(m.sockets))
	for id, socket := range m.sockets {
		delete(m.sockets, id)
		sockets = append(sockets, socket)
	}
	m.socketsMu.Unlock()
	for _, socket := range sockets {
		socket.closeMu.Do(func() {
			_ = socket.conn.Close(1000, "application shutting down")
		})
	}

	m.fileEditorsMu.Lock()
	clear(m.fileEditors)
	m.fileEditorsMu.Unlock()

	m.httpMu.Lock()
	server := m.httpServer
	m.httpServer = nil
	m.httpMu.Unlock()
	if server == nil {
		return
	}

	server.mu.Lock()
	for requestID, pending := range server.pending {
		delete(server.pending, requestID)
		select {
		case pending.response <- ExpressResponse{Status: http.StatusServiceUnavailable, Body: "application shutting down"}:
		default:
		}
	}
	server.routes = map[string]*expressRoute{}
	server.routesByID = map[string]*expressRoute{}
	server.tokens = map[string]string{}
	server.mu.Unlock()
	_ = server.server.Close()
}
