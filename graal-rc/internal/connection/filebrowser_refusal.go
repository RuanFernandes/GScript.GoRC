package connection

import (
	"fmt"
	"log"
	"path"
	"strings"

	"graal-rc/rclib"
)

// handleFileBrowserMessage reports a File Browser message to the window. A
// refused download arrives as a message holding only the file name; its wait
// would otherwise keep the transfer slot until downloadTimeout.
func (s *Service) handleFileBrowserMessage(message string) {
	s.refuseDownload(message)
	s.emitEvent("rc:fbMessage", message)
}

func (s *Service) refuseDownload(message string) {
	name := normalizeFileBrowserTransferPath(message)
	if name == "" {
		return
	}
	s.pendingFilesMu.Lock()
	defer s.pendingFilesMu.Unlock()
	for requested, waiter := range s.pendingFiles {
		full := normalizeFileBrowserTransferPath(requested)
		if strings.EqualFold(name, full) || strings.EqualFold(name, path.Base(full)) {
			log.Printf("[filebrowser] server refused download %q", requested)
			delete(s.pendingFiles, requested)
			waiter.finish(nil, fmt.Errorf("the server could not send %q", requested))
			return
		}
	}
}

// downloadListed reads a file of a listed folder. The server refuses to send an
// empty file, so one is not requested.
func downloadListed(entries []rclib.FileBrowserEntry, name string, fetch func(string) ([]byte, error)) ([]byte, error) {
	for _, entry := range entries {
		if !entry.IsDirectory && entry.Size == 0 && path.Base(normalizeFileBrowserTransferPath(entry.Path)) == name {
			return []byte{}, nil
		}
	}
	return fetch(name)
}
