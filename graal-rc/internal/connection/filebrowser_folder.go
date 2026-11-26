package connection

// The server resolves File Browser transfers against the connection's current
// folder: rc_upload_file sends only the file's base name. Writing into a given
// folder therefore means opening it, confirming that, and keeping every other
// File Browser request out until the transfer has been sent.

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"graal-rc/rclib"
)

const (
	// folderCheckTimeout bounds one listing needed before an upload.
	folderCheckTimeout = 20 * time.Second
	// folderSettleTimeout is how long to wait for the window's pending listing.
	folderSettleTimeout = 3 * time.Second
)

type fileBrowserLister func(ctx context.Context, folder string) ([]rclib.FileBrowserEntry, error)

// fileBrowserView tracks which folder the File Browser window is showing.
type fileBrowserView struct {
	mu      sync.Mutex
	folder  string // as requested or reported; meaningful when known
	known   bool
	pending bool    // a requested folder has not been listed yet
	quiet   *string // folder being checked, whose listing the window must not see
	settle  time.Duration
}

// requested records a folder change asked for by the window.
func (v *fileBrowserView) requested(folder string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.folder, v.known, v.pending = folder, true, true
}

func (v *fileBrowserView) reset() {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.folder, v.known, v.pending, v.quiet = "", false, false, nil
}

// delivered handles a listing callback. It returns the folder to report to the
// window, and whether to withhold the event because it only answers a check.
func (v *fileBrowserView) delivered(folder string, count int) (label string, hidden bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	// The native library reports an empty listing without its folder name.
	empty := count == 0 && normalizeFileBrowserTransferPath(folder) == ""
	if v.quiet != nil && (empty || normalizeFileBrowserTransferPath(folder) == *v.quiet) {
		return folder, true
	}
	v.pending = false
	if empty && v.known {
		return v.folder, false
	}
	v.folder, v.known = folder, true
	return folder, false
}

func (v *fileBrowserView) waitSettled(ctx context.Context) {
	limit := v.settle
	if limit == 0 {
		limit = folderSettleTimeout
	}
	deadline := time.After(limit)
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		v.mu.Lock()
		pending := v.pending
		v.mu.Unlock()
		if !pending {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-deadline:
			return
		case <-ticker.C:
		}
	}
}

// withFolder opens target, runs use with its listing while it is the server's
// current folder, then reopens the folder the window shows. use does not run
// unless the listing of target was obtained. The caller must keep other File
// Browser requests out for the duration.
func (v *fileBrowserView) withFolder(ctx context.Context, target string, list fileBrowserLister, use func([]rclib.FileBrowserEntry) error) error {
	v.waitSettled(ctx)

	v.mu.Lock()
	pending := v.pending
	restore := v.known && (pending || normalizeFileBrowserTransferPath(v.folder) != target)
	if restore {
		checking := target
		v.quiet = &checking
	}
	v.mu.Unlock()

	if restore {
		defer func() {
			v.mu.Lock()
			shown := normalizeFileBrowserTransferPath(v.folder)
			v.mu.Unlock()
			restoreCtx, cancel := context.WithTimeout(context.Background(), folderCheckTimeout)
			defer cancel()
			if _, err := list(restoreCtx, shown); err != nil {
				log.Printf("[filebrowser] reopen folder %q: %v", shown, err)
			}
		}()
	}

	checkCtx, cancel := context.WithTimeout(ctx, folderCheckTimeout)
	entries, err := list(checkCtx, target)
	cancel()
	v.mu.Lock()
	v.quiet = nil
	v.mu.Unlock()

	if err != nil {
		return fmt.Errorf("could not open remote folder %q to check it; nothing was uploaded: %w", target, err)
	}
	// An unnamed empty listing may answer the window's pending request.
	if pending && len(entries) == 0 {
		return errors.New("the file browser is still loading another folder; nothing was uploaded, try again")
	}
	return use(entries)
}

// FileBrowserDirectory is a remote folder that is the server's current one for
// as long as the WithFileBrowserFolder callback runs.
type FileBrowserDirectory struct {
	Entries  []rclib.FileBrowserEntry
	download func(name string) ([]byte, error)
	upload   func(path string, content []byte) error
}

// Download reads a file of this folder by its base name.
func (d FileBrowserDirectory) Download(name string) ([]byte, error) { return d.download(name) }

// Upload writes a file into this folder; only the base name of path is sent.
func (d FileBrowserDirectory) Upload(path string, content []byte) error {
	return d.upload(path, content)
}

// WithFileBrowserFolder opens folder, confirms it with its listing and runs use
// while no other File Browser operation can change the server's current folder.
// use must only reach the File Browser through the directory it receives. ctx
// bounds the wait for transfers already running and the downloads made by use,
// which keep every other operation out while they wait.
func (s *Service) WithFileBrowserFolder(ctx context.Context, folder string, use func(FileBrowserDirectory) error) error {
	cleaned, err := normalizeFileBrowserBackupPath(folder, true)
	if err != nil {
		return err
	}
	if s.fileBrowserBackupActive.Load() {
		return errors.New("a file-browser backup is running; try again when it finishes")
	}
	if err := s.lockFileBrowserOperation(ctx); err != nil {
		return err
	}
	defer s.fileBrowserOperationMu.Unlock()
	scope, err := s.captureSession()
	if err != nil {
		return err
	}
	list := func(listCtx context.Context, name string) ([]rclib.FileBrowserEntry, error) {
		return s.listFileBrowserFolderContext(listCtx, scope, name)
	}
	return s.fileBrowserView.withFolder(ctx, cleaned, list, func(entries []rclib.FileBrowserEntry) error {
		if err := s.checkSession(scope); err != nil {
			return err
		}
		return use(FileBrowserDirectory{
			Entries: entries,
			download: func(name string) ([]byte, error) {
				return downloadListed(entries, name, func(name string) ([]byte, error) {
					return s.downloadFileContext(ctx, scope, name)
				})
			},
			upload: func(path string, content []byte) error {
				if err := s.checkSession(scope); err != nil {
					return err
				}
				return s.uploadFileLocked(path, content)
			},
		})
	})
}

// ShownFileBrowserFolder is the folder whose listing the File Browser window
// shows; "" is the root.
func (s *Service) ShownFileBrowserFolder() string {
	s.fileBrowserView.mu.Lock()
	defer s.fileBrowserView.mu.Unlock()
	return normalizeFileBrowserTransferPath(s.fileBrowserView.folder)
}
