package plugins

import (
	"crypto/subtle"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	maxExpressBodyBytes = 1 << 20
	expressRequestTTL   = 15 * time.Second
)

type ExpressServerInfo struct {
	BaseURL string `json:"baseUrl"`
	Token   string `json:"token"`
}

type ExpressRouteInfo struct {
	RouteID string `json:"routeId"`
	URL     string `json:"url"`
	BaseURL string `json:"baseUrl"`
	Token   string `json:"token"`
}

type ExpressRequestEvent struct {
	PluginID  string              `json:"pluginId"`
	RouteID   string              `json:"routeId"`
	RequestID string              `json:"requestId"`
	Method    string              `json:"method"`
	Path      string              `json:"path"`
	Query     map[string][]string `json:"query,omitempty"`
	Headers   map[string]string   `json:"headers,omitempty"`
	Body      string              `json:"body,omitempty"`
}

type ExpressResponse struct {
	Status  int               `json:"status,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    string            `json:"body,omitempty"`
}

type expressRoute struct {
	id       string
	pluginID string
	method   string
	path     string
}

type expressPending struct {
	pluginID string
	response chan ExpressResponse
}

type pluginHTTPServer struct {
	mu         sync.RWMutex
	manager    *Manager
	server     *http.Server
	baseURL    string
	tokens     map[string]string
	routes     map[string]*expressRoute
	routesByID map[string]*expressRoute
	pending    map[string]expressPending
}

func (m *Manager) ExpressListen(id string) (ExpressServerInfo, error) {
	if err := m.RequireAPI(id, "express.http"); err != nil {
		return ExpressServerInfo{}, err
	}
	server, err := m.ensureHTTPServer()
	if err != nil {
		return ExpressServerInfo{}, err
	}
	server.mu.Lock()
	token := server.tokens[id]
	if token == "" {
		token = randomID("gorc")
		server.tokens[id] = token
	}
	server.mu.Unlock()
	return ExpressServerInfo{BaseURL: pluginBaseURL(server.baseURL, id), Token: token}, nil
}

func (m *Manager) RegisterExpressRoute(id, method, path string) (ExpressRouteInfo, error) {
	if err := m.RequireAPI(id, "express.http"); err != nil {
		return ExpressRouteInfo{}, err
	}
	method = strings.ToUpper(strings.TrimSpace(method))
	if !validExpressMethod(method) {
		return ExpressRouteInfo{}, errors.New("unsupported internal API method")
	}
	path = strings.TrimSpace(path)
	if !validExpressPath(path) {
		return ExpressRouteInfo{}, errors.New("internal API path must be an absolute path without traversal")
	}
	server, err := m.ensureHTTPServer()
	if err != nil {
		return ExpressRouteInfo{}, err
	}
	server.mu.Lock()
	token := server.tokens[id]
	if token == "" {
		token = randomID("gorc")
		server.tokens[id] = token
	}
	key := expressRouteKey(id, method, path)
	route := server.routes[key]
	if route == nil {
		route = &expressRoute{id: randomID("route"), pluginID: id, method: method, path: path}
		server.routes[key] = route
		server.routesByID[route.id] = route
	}
	server.mu.Unlock()
	return ExpressRouteInfo{RouteID: route.id, URL: pluginBaseURL(server.baseURL, id) + path, BaseURL: pluginBaseURL(server.baseURL, id), Token: token}, nil
}

func (m *Manager) UnregisterExpressRoute(id, routeID string) error {
	if err := m.RequireAPI(id, "express.http"); err != nil {
		return err
	}
	server := m.httpServerValue()
	if server == nil {
		return ErrPluginNotFound
	}
	server.mu.Lock()
	route, ok := server.routesByID[routeID]
	if !ok || route.pluginID != id {
		server.mu.Unlock()
		return ErrPluginNotFound
	}
	delete(server.routesByID, routeID)
	delete(server.routes, expressRouteKey(route.pluginID, route.method, route.path))
	server.mu.Unlock()
	return nil
}

func (m *Manager) RespondExpressRequest(id, requestID string, response ExpressResponse) error {
	if err := m.RequireAPI(id, "express.http"); err != nil {
		return err
	}
	if len(response.Body) > maxExpressBodyBytes {
		return errors.New("internal API response exceeds 1 MiB")
	}
	if response.Status == 0 {
		response.Status = http.StatusOK
	}
	if response.Status < 100 || response.Status > 599 {
		return errors.New("internal API response status is invalid")
	}
	server := m.httpServerValue()
	if server == nil {
		return ErrPluginNotFound
	}
	server.mu.Lock()
	pending, ok := server.pending[requestID]
	if ok {
		delete(server.pending, requestID)
	}
	server.mu.Unlock()
	if !ok || pending.pluginID != id {
		return ErrPluginNotFound
	}
	select {
	case pending.response <- response:
		return nil
	default:
		return errors.New("internal API request already completed")
	}
}

func (m *Manager) ClosePluginHTTP(id string) {
	server := m.httpServerValue()
	if server == nil {
		return
	}
	server.mu.Lock()
	for key, route := range server.routes {
		if route.pluginID == id {
			delete(server.routes, key)
			delete(server.routesByID, route.id)
		}
	}
	delete(server.tokens, id)
	for requestID, pending := range server.pending {
		if pending.pluginID != id {
			continue
		}
		delete(server.pending, requestID)
		select {
		case pending.response <- ExpressResponse{Status: http.StatusServiceUnavailable, Body: "plugin unloaded"}:
		default:
		}
	}
	server.mu.Unlock()
}

func (m *Manager) ensureHTTPServer() (*pluginHTTPServer, error) {
	m.httpMu.Lock()
	defer m.httpMu.Unlock()
	if m.httpServer != nil {
		return m.httpServer, nil
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	server := &pluginHTTPServer{
		manager:    m,
		baseURL:    "http://" + listener.Addr().String(),
		tokens:     map[string]string{},
		routes:     map[string]*expressRoute{},
		routesByID: map[string]*expressRoute{},
		pending:    map[string]expressPending{},
	}
	server.server = &http.Server{Handler: server}
	m.httpServer = server
	go func() {
		if err := server.server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			m.emitRuntime("plugin:http", map[string]string{"type": "server-error", "error": err.Error()})
		}
	}()
	return server, nil
}

func (m *Manager) httpServerValue() *pluginHTTPServer {
	m.httpMu.Lock()
	server := m.httpServer
	m.httpMu.Unlock()
	return server
}

func (s *pluginHTTPServer) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	pluginID, path, ok := splitPluginPath(request.URL.Path)
	if !ok {
		http.NotFound(writer, request)
		return
	}
	s.mu.RLock()
	token := s.tokens[pluginID]
	route := s.routes[expressRouteKey(pluginID, request.Method, path)]
	s.mu.RUnlock()
	providedToken := request.Header.Get("X-GoRC-Plugin-Token")
	if token == "" || providedToken == "" || subtle.ConstantTimeCompare([]byte(token), []byte(providedToken)) != 1 {
		http.Error(writer, "unauthorized", http.StatusUnauthorized)
		return
	}
	if route == nil {
		http.NotFound(writer, request)
		return
	}
	body, err := io.ReadAll(io.LimitReader(request.Body, maxExpressBodyBytes+1))
	if err != nil {
		http.Error(writer, "unable to read request", http.StatusBadRequest)
		return
	}
	if len(body) > maxExpressBodyBytes {
		http.Error(writer, "request body exceeds 1 MiB", http.StatusRequestEntityTooLarge)
		return
	}
	requestID := randomID("request")
	pending := expressPending{pluginID: pluginID, response: make(chan ExpressResponse, 1)}
	s.mu.Lock()
	s.pending[requestID] = pending
	s.mu.Unlock()
	event := ExpressRequestEvent{PluginID: pluginID, RouteID: route.id, RequestID: requestID, Method: request.Method, Path: path, Query: request.URL.Query(), Headers: safeRequestHeaders(request.Header), Body: string(body)}
	s.manager.emitRuntime("plugin:http", event)
	timer := time.NewTimer(expressRequestTTL)
	defer timer.Stop()
	select {
	case response := <-pending.response:
		s.mu.Lock()
		delete(s.pending, requestID)
		s.mu.Unlock()
		writeExpressResponse(writer, response)
	case <-timer.C:
		s.mu.Lock()
		delete(s.pending, requestID)
		s.mu.Unlock()
		http.Error(writer, "plugin request timed out", http.StatusGatewayTimeout)
	}
}

func validExpressMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions:
		return true
	default:
		return false
	}
}

func validExpressPath(path string) bool {
	if path == "" || len(path) > 256 || !strings.HasPrefix(path, "/") || strings.Contains(path, "?") || strings.Contains(path, "#") || strings.Contains(path, "\\") {
		return false
	}
	for _, segment := range strings.Split(path, "/") {
		if segment == ".." {
			return false
		}
	}
	return true
}

func expressRouteKey(pluginID, method, path string) string {
	return pluginID + "\x00" + method + "\x00" + path
}

func pluginBaseURL(baseURL, pluginID string) string {
	return strings.TrimRight(baseURL, "/") + "/plugins/" + url.PathEscape(pluginID)
}

func splitPluginPath(path string) (string, string, bool) {
	const prefix = "/plugins/"
	if !strings.HasPrefix(path, prefix) {
		return "", "", false
	}
	remaining := strings.TrimPrefix(path, prefix)
	parts := strings.SplitN(remaining, "/", 2)
	if len(parts) != 2 || parts[0] == "" {
		return "", "", false
	}
	pluginID, err := url.PathUnescape(parts[0])
	if err != nil || pluginID == "" {
		return "", "", false
	}
	requestPath := "/" + parts[1]
	if requestPath == "//" {
		requestPath = "/"
	}
	return pluginID, requestPath, true
}

func safeRequestHeaders(headers http.Header) map[string]string {
	result := map[string]string{}
	for key, values := range headers {
		if isSensitiveHeader(key) || len(values) == 0 {
			continue
		}
		result[key] = strings.Join(values, ", ")
	}
	return result
}

func isSensitiveHeader(key string) bool {
	switch strings.ToLower(key) {
	case "authorization", "cookie", "set-cookie", "x-gorc-plugin-token":
		return true
	default:
		return false
	}
}

func writeExpressResponse(writer http.ResponseWriter, response ExpressResponse) {
	for key, value := range response.Headers {
		if isSensitiveHeader(key) {
			continue
		}
		writer.Header().Set(key, value)
	}
	status := response.Status
	if status == 0 {
		status = http.StatusOK
	}
	writer.WriteHeader(status)
	_, _ = writer.Write([]byte(response.Body))
}
