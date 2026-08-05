package plugins

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
)

const (
	maxPluginSockets       = 4
	maxSocketMessageBytes  = 1 << 20
	socketDialTimeout      = 15 * time.Second
	socketOperationTimeout = 15 * time.Second
	socketSendInterval     = 50 * time.Millisecond
)

type SocketRequest struct {
	URL       string   `json:"url"`
	Protocols []string `json:"protocols,omitempty"`
}

type SocketInfo struct {
	ID          string `json:"id"`
	URL         string `json:"url"`
	ReadyState  string `json:"readyState"`
	Subprotocol string `json:"subprotocol,omitempty"`
}

type SocketEvent struct {
	PluginID string `json:"pluginId"`
	SocketID string `json:"socketId"`
	Type     string `json:"type"`
	Data     string `json:"data,omitempty"`
	Binary   bool   `json:"binary,omitempty"`
	Code     int    `json:"code,omitempty"`
	Reason   string `json:"reason,omitempty"`
	Error    string `json:"error,omitempty"`
}

type pluginSocket struct {
	pluginID string
	id       string
	url      string
	conn     *websocket.Conn

	sendMu   sync.Mutex
	lastSend time.Time
	closeMu  sync.Once
}

func (m *Manager) OpenSocket(id string, request SocketRequest) (SocketInfo, error) {
	if err := m.RequireAPI(id, "network.socket"); err != nil {
		return SocketInfo{}, err
	}
	u, err := url.Parse(strings.TrimSpace(request.URL))
	if err != nil || u.Scheme != "wss" || u.Host == "" || u.User != nil {
		return SocketInfo{}, errors.New("only authenticated-free WSS sockets are allowed")
	}
	if !contains(m.approvedHosts(id), origin(u)) {
		return SocketInfo{}, ErrPermissionDenied
	}
	if len(request.Protocols) > 10 {
		return SocketInfo{}, errors.New("a plugin may negotiate at most 10 socket protocols")
	}
	for _, protocol := range request.Protocols {
		if strings.TrimSpace(protocol) == "" || len(protocol) > 128 || strings.ContainsAny(protocol, "\r\n") {
			return SocketInfo{}, errors.New("invalid WebSocket protocol")
		}
	}

	m.socketsMu.Lock()
	if m.sockets == nil {
		m.sockets = map[string]*pluginSocket{}
	}
	if m.socketOpening == nil {
		m.socketOpening = map[string]int{}
	}
	count := 0
	for _, socket := range m.sockets {
		if socket.pluginID == id {
			count++
		}
	}
	if count+m.socketOpening[id] >= maxPluginSockets {
		m.socketsMu.Unlock()
		return SocketInfo{}, errors.New("plugin socket limit reached (4)")
	}
	m.socketOpening[id]++
	m.socketsMu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), socketDialTimeout)
	conn, _, err := websocket.Dial(ctx, u.String(), &websocket.DialOptions{Subprotocols: request.Protocols})
	cancel()
	m.socketsMu.Lock()
	m.socketOpening[id]--
	if m.socketOpening[id] <= 0 {
		delete(m.socketOpening, id)
	}
	m.socketsMu.Unlock()
	if err != nil {
		return SocketInfo{}, err
	}
	if err := m.RequireAPI(id, "network.socket"); err != nil {
		_ = conn.Close(websocket.StatusNormalClosure, "plugin disabled")
		return SocketInfo{}, err
	}
	conn.SetReadLimit(maxSocketMessageBytes)
	socket := &pluginSocket{pluginID: id, id: randomID("socket"), url: origin(u), conn: conn}
	m.socketsMu.Lock()
	m.sockets[socket.id] = socket
	m.socketsMu.Unlock()

	m.emitRuntime("plugin:socket", SocketEvent{PluginID: id, SocketID: socket.id, Type: "open", Data: socket.url, Reason: conn.Subprotocol()})
	go m.readSocket(socket)
	return SocketInfo{ID: socket.id, URL: socket.url, ReadyState: "open", Subprotocol: conn.Subprotocol()}, nil
}

func (m *Manager) SendSocket(id, socketID, data string) error {
	if err := m.RequireAPI(id, "network.socket"); err != nil {
		return err
	}
	if len(data) > maxSocketMessageBytes {
		return errors.New("socket message exceeds 1 MiB")
	}
	m.socketsMu.Lock()
	socket, ok := m.sockets[socketID]
	m.socketsMu.Unlock()
	if !ok || socket.pluginID != id {
		return ErrPluginNotFound
	}
	socket.sendMu.Lock()
	defer socket.sendMu.Unlock()
	if !socket.lastSend.IsZero() && time.Since(socket.lastSend) < socketSendInterval {
		return errors.New("socket send rate limit exceeded")
	}
	socket.lastSend = time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), socketOperationTimeout)
	err := socket.conn.Write(ctx, websocket.MessageText, []byte(data))
	cancel()
	if err != nil {
		return err
	}
	return nil
}

func (m *Manager) CloseSocket(id, socketID string) error {
	if err := m.RequireAPI(id, "network.socket"); err != nil {
		return err
	}
	return m.closeSocket(id, socketID)
}

func (m *Manager) closeSocket(id, socketID string) error {
	m.socketsMu.Lock()
	socket, ok := m.sockets[socketID]
	if ok && id != "" && socket.pluginID != id {
		ok = false
	}
	if ok {
		delete(m.sockets, socketID)
	}
	m.socketsMu.Unlock()
	if !ok {
		return ErrPluginNotFound
	}
	socket.closeMu.Do(func() {
		_ = socket.conn.Close(websocket.StatusNormalClosure, "plugin closed socket")
	})
	return nil
}

func (m *Manager) ClosePluginSockets(id string) {
	m.socketsMu.Lock()
	var sockets []*pluginSocket
	for socketID, socket := range m.sockets {
		if socket.pluginID == id {
			delete(m.sockets, socketID)
			sockets = append(sockets, socket)
		}
	}
	m.socketsMu.Unlock()
	for _, socket := range sockets {
		socket.closeMu.Do(func() {
			_ = socket.conn.Close(websocket.StatusNormalClosure, "plugin unloaded")
		})
	}
}

func (m *Manager) readSocket(socket *pluginSocket) {
	for {
		// Reads stay open while the socket is idle. Closing the connection during
		// unload interrupts this blocking read and lets the goroutine terminate.
		typ, data, err := socket.conn.Read(context.Background())
		if err != nil {
			code := int(websocket.CloseStatus(err))
			if code == 0 {
				code = int(websocket.StatusAbnormalClosure)
			}
			if !errors.Is(err, context.Canceled) {
				m.emitRuntime("plugin:socket", SocketEvent{PluginID: socket.pluginID, SocketID: socket.id, Type: "error", Error: err.Error()})
			}
			m.socketsMu.Lock()
			if current, ok := m.sockets[socket.id]; ok && current == socket {
				delete(m.sockets, socket.id)
			}
			m.socketsMu.Unlock()
			m.emitRuntime("plugin:socket", SocketEvent{PluginID: socket.pluginID, SocketID: socket.id, Type: "close", Code: code, Reason: err.Error()})
			return
		}
		m.emitRuntime("plugin:socket", SocketEvent{PluginID: socket.pluginID, SocketID: socket.id, Type: "message", Data: string(data), Binary: typ == websocket.MessageBinary})
	}
}

func (m *Manager) approvedHosts(id string) []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if plugin, ok := m.plugins[id]; ok {
		return append([]string(nil), plugin.ApprovedHosts...)
	}
	return nil
}

func randomID(prefix string) string {
	bytes := make([]byte, 12)
	if _, err := rand.Read(bytes); err != nil {
		return prefix + "-" + time.Now().UTC().Format("20060102150405.000000000")
	}
	return prefix + "-" + hex.EncodeToString(bytes)
}
