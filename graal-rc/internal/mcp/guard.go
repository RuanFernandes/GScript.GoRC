package mcp

import (
	"errors"
	"mime"
	"net"
	"net/http"
	"strings"
)

// checkRequest keeps web pages away from the tools. Listening on loopback is not
// enough: any page the user visits can send requests there, and these tools
// write live server scripts. MCP clients are not browsers, so they send no
// Origin.
func (s *Server) checkRequest(r *http.Request) (int, error) {
	// DNS rebinding reaches loopback under the attacker's own host name.
	if !s.allowedHost(r.Host) {
		return http.StatusForbidden, errors.New("MCP endpoint rejected this Host")
	}
	if r.Header.Get("Origin") != "" {
		return http.StatusForbidden, errors.New("MCP endpoint does not accept browser requests")
	}
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "none" {
		return http.StatusForbidden, errors.New("MCP endpoint does not accept browser requests")
	}
	// A page can send text/plain cross-origin without a preflight, but not JSON.
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return http.StatusUnsupportedMediaType, errors.New("MCP requests must use Content-Type: application/json")
	}
	return 0, nil
}

func (s *Server) allowedHost(host string) bool {
	for _, allowed := range s.hosts {
		if strings.EqualFold(host, allowed) {
			return true
		}
	}
	name, _, err := net.SplitHostPort(host)
	if err != nil {
		name = strings.Trim(host, "[]")
	}
	if strings.EqualFold(name, "localhost") {
		return true
	}
	ip := net.ParseIP(name)
	return ip != nil && ip.IsLoopback()
}
