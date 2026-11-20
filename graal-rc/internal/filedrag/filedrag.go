// Package filedrag serves remote files to the operating system while the user
// drags them out of the File Browser.
//
// WebView2 cannot start a native file drag by itself, but it does support the
// Chromium "DownloadURL" drag format: the page names a URL, and when the file is
// dropped on Explorer (or anything else that accepts files) the browser fetches
// that URL and writes the result there. Nothing is downloaded when the drag
// starts, so a drag costs nothing until it lands.
//
// The URL has to point at something the browser can fetch. This package is that
// something: a small HTTP server on the loopback interface that downloads the
// requested remote file through the RC session and returns it.
//
// The server is reachable by every local program, and by any web page that can
// guess its port, so each request must prove it comes from the application:
//
//   - the URL carries a random 256-bit secret that exists only in this process
//     and in the windows it is handed to;
//   - the Host header must be the literal loopback address, which refuses DNS
//     rebinding;
//   - only GET and HEAD are served, and the remote path is validated like any
//     other remote path in the application.
package filedrag

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"mime"
	"net"
	"net/http"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Downloader fetches the content of one remote file. It must honour ctx, which
// is canceled when the client that asked for the file goes away.
type Downloader func(ctx context.Context, remotePath string) ([]byte, error)

const routePrefix = "/drag/"

// Server is the loopback download server. The zero value is not usable; use New.
type Server struct {
	download Downloader

	mu     sync.Mutex
	srv    *http.Server
	secret string
	host   string
}

// New returns a server that has not started listening yet.
func New(download Downloader) *Server { return &Server{download: download} }

// Base returns the URL prefix of the files this server hands out, ending in a
// slash. Append the escaped remote path to it. The server starts on first use.
func (s *Server) Base() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.srv == nil {
		if err := s.startLocked(); err != nil {
			return "", err
		}
	}
	return "http://" + s.host + routePrefix + s.secret + "/", nil
}

func (s *Server) startLocked() error {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return fmt.Errorf("create drag secret: %w", err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("listen for file drags: %w", err)
	}
	secret := hex.EncodeToString(raw)
	host := listener.Addr().String()
	srv := &http.Server{
		Handler:           s.handler(secret, host),
		ReadHeaderTimeout: 5 * time.Second,
		// No write timeout: a large remote file legitimately takes minutes.
		IdleTimeout: 60 * time.Second,
	}
	s.srv, s.secret, s.host = srv, secret, host
	go func() {
		if err := srv.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("[filedrag] server stopped: %v", err)
		}
	}()
	return nil
}

// Close stops the server. Base starts a new one, with a new secret.
func (s *Server) Close() error {
	s.mu.Lock()
	srv := s.srv
	s.srv, s.secret, s.host = nil, "", ""
	s.mu.Unlock()
	if srv == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		return srv.Close()
	}
	return nil
}

func (s *Server) handler(secret, host string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != host {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		rest, ok := strings.CutPrefix(r.URL.Path, routePrefix)
		token, remote, found := strings.Cut(rest, "/")
		if !ok || !found || subtle.ConstantTimeCompare([]byte(token), []byte(secret)) != 1 {
			http.NotFound(w, r)
			return
		}
		remotePath, err := CleanRemotePath(remote)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		name := path.Base(remotePath)
		contentType := mime.TypeByExtension(path.Ext(name))
		if contentType == "" {
			contentType = "application/octet-stream"
		}
		header := w.Header()
		header.Set("Content-Type", contentType)
		header.Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
		header.Set("Cache-Control", "no-store")
		header.Set("X-Content-Type-Options", "nosniff")
		if r.Method == http.MethodHead {
			// The size is unknown until the file has been downloaded, and a probe
			// must not cost a download.
			w.WriteHeader(http.StatusOK)
			return
		}

		content, err := s.download(r.Context(), remotePath)
		if err != nil {
			log.Printf("[filedrag] %q: %v", remotePath, err)
			header.Del("Content-Disposition")
			http.Error(w, "download failed: "+err.Error(), http.StatusBadGateway)
			return
		}
		header.Set("Content-Length", strconv.Itoa(len(content)))
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write(content); err != nil {
			log.Printf("[filedrag] %q: the client stopped reading: %v", remotePath, err)
		}
	})
}

// CleanRemotePath validates a remote file path with the rules the rest of the
// application applies: no empty path, no NUL, no parent traversal. The result
// uses forward slashes and has no leading slash.
func CleanRemotePath(value string) (string, error) {
	value = strings.TrimSpace(strings.ReplaceAll(value, "\\", "/"))
	value = strings.TrimPrefix(value, "/")
	if value == "" || strings.Contains(value, "\x00") {
		return "", errors.New("remote file path is required")
	}
	for _, segment := range strings.Split(value, "/") {
		if segment == ".." {
			return "", errors.New("remote file path cannot contain parent traversal")
		}
	}
	cleaned := path.Clean(value)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", errors.New("invalid remote file path")
	}
	return cleaned, nil
}
