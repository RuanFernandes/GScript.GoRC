package connection

import (
	"context"
	"errors"
	"fmt"
	pathpkg "path"
	"sort"
	"strings"
	"time"

	"graal-rc/rclib"
)

const fileBrowserListingTimeout = 2 * time.Minute

// FileBrowserBackupProgress describes a read-only remote backup operation.
// It is intentionally emitted by the App layer instead of being exposed as a
// Wails method parameter.
type FileBrowserBackupProgress struct {
	Phase        string
	Folder       string
	File         string
	FoldersDone  int
	FoldersTotal int
	FilesDone    int
	FilesTotal   int
	BytesDone    int64
	BytesTotal   int64
}

// FileBrowserBackupResult contains the remote data that was persisted locally.
type FileBrowserBackupResult struct {
	Files int
	Bytes int64
}

type fileBrowserListingWait struct {
	done      chan struct{}
	requested string
	err       error
	closed    bool
}

type fileBrowserBackupFile struct {
	path  string
	entry rclib.FileBrowserEntry
}

// normalizeFileBrowserBackupPath accepts the relative slash-separated paths
// used by the RC File Browser. The root is represented by an empty string when
// allowRoot is true. Rejecting traversal before any native call keeps the
// backup operation read-only and prevents local path escapes later on.
func normalizeFileBrowserBackupPath(value string, allowRoot bool) (string, error) {
	value = strings.TrimSpace(strings.ReplaceAll(value, "\\", "/"))
	if strings.Contains(value, "\x00") {
		return "", errors.New("file-browser path contains NUL")
	}
	value = strings.Trim(value, "/")
	if value == "" {
		if allowRoot {
			return "", nil
		}
		return "", errors.New("file-browser path is required")
	}
	for _, segment := range strings.Split(value, "/") {
		if segment == "" || segment == "." || segment == ".." || strings.Contains(segment, ":") {
			return "", errors.New("file-browser path contains an invalid segment")
		}
	}
	cleaned := pathpkg.Clean(value)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", errors.New("file-browser path cannot escape its root")
	}
	return cleaned, nil
}

func normalizeFileBrowserBackupFolders(folders []string) ([]string, error) {
	seen := make(map[string]struct{}, len(folders))
	normalized := make([]string, 0, len(folders))
	for _, folder := range folders {
		cleaned, err := normalizeFileBrowserBackupPath(folder, true)
		if err != nil {
			return nil, fmt.Errorf("invalid backup folder %q: %w", folder, err)
		}
		if _, exists := seen[cleaned]; exists {
			continue
		}
		seen[cleaned] = struct{}{}
		normalized = append(normalized, cleaned)
	}
	if len(normalized) == 0 {
		return nil, errors.New("at least one backup folder is required")
	}
	sort.Strings(normalized)
	return normalized, nil
}

func fileBrowserBackupEntryPath(folder, entryPath string) (string, error) {
	folder, err := normalizeFileBrowserBackupPath(folder, true)
	if err != nil {
		return "", err
	}
	entryPath, err = normalizeFileBrowserBackupPath(entryPath, false)
	if err != nil {
		return "", err
	}
	if folder == "" || entryPath == folder || strings.HasPrefix(entryPath, folder+"/") {
		return entryPath, nil
	}
	return normalizeFileBrowserBackupPath(folder+"/"+entryPath, false)
}

func (s *Service) registerFileBrowserListing(folder string) *fileBrowserListingWait {
	wait := &fileBrowserListingWait{
		done:      make(chan struct{}),
		requested: normalizeFileBrowserTransferPath(folder),
	}
	s.fileBrowserListingWaitMu.Lock()
	s.fileBrowserListingWait = wait
	s.fileBrowserListingWaitMu.Unlock()
	return wait
}

func (s *Service) resolveFileBrowserListing(folder string) {
	requested := normalizeFileBrowserTransferPath(folder)
	s.fileBrowserListingWaitMu.Lock()
	defer s.fileBrowserListingWaitMu.Unlock()
	wait := s.fileBrowserListingWait
	if wait == nil || wait.closed || wait.requested != requested {
		return
	}
	wait.closed = true
	s.fileBrowserListingWait = nil
	close(wait.done)
}

func (s *Service) cancelFileBrowserListing(wait *fileBrowserListingWait, err error) {
	s.fileBrowserListingWaitMu.Lock()
	defer s.fileBrowserListingWaitMu.Unlock()
	if s.fileBrowserListingWait != wait || wait.closed {
		return
	}
	wait.err = err
	wait.closed = true
	s.fileBrowserListingWait = nil
	close(wait.done)
}

func (s *Service) cancelCurrentFileBrowserListing(err error) {
	s.fileBrowserListingWaitMu.Lock()
	defer s.fileBrowserListingWaitMu.Unlock()
	wait := s.fileBrowserListingWait
	if wait == nil || wait.closed {
		return
	}
	wait.err = err
	wait.closed = true
	s.fileBrowserListingWait = nil
	close(wait.done)
}

// listFileBrowserFolderContext requests one folder and waits for the native
// callback before copying its cache. The caller must hold
// fileBrowserOperationMu exclusively so no other File Browser request can
// replace the native listing while the backup is reading it.
func (s *Service) listFileBrowserFolderContext(ctx context.Context, scope sessionScope, folder string) ([]rclib.FileBrowserEntry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := s.checkSession(scope); err != nil {
		return nil, err
	}

	s.fileBrowserListingMu.Lock()
	defer s.fileBrowserListingMu.Unlock()
	wait := s.registerFileBrowserListing(folder)
	requestFolder := folder
	if requestFolder != "" {
		requestFolder += "/"
	}
	if err := rclib.FileBrowserCd(scope.handle, requestFolder); err != nil {
		s.cancelFileBrowserListing(wait, err)
		return nil, err
	}

	timer := time.NewTimer(fileBrowserListingTimeout)
	defer timer.Stop()
	select {
	case <-wait.done:
		if wait.err != nil {
			return nil, wait.err
		}
		if err := s.checkSession(scope); err != nil {
			return nil, err
		}
		entries, err := rclib.CopyFileBrowserFiles(scope.handle)
		if err != nil {
			return nil, err
		}
		return entries, nil
	case <-ctx.Done():
		s.cancelFileBrowserListing(wait, ctx.Err())
		return nil, ctx.Err()
	case <-scope.done:
		s.cancelFileBrowserListing(wait, errConnectionSessionChanged)
		return nil, errConnectionSessionChanged
	case <-timer.C:
		err := errors.New("file-browser folder listing timed out")
		s.cancelFileBrowserListing(wait, err)
		return nil, err
	}
}

// BackupFileBrowser scans and downloads the selected remote folders in a
// deterministic order. The sink owns local persistence, while this service
// only performs File Browser reads and file downloads.
func (s *Service) BackupFileBrowser(
	ctx context.Context,
	folders []string,
	sink func(path string, entry rclib.FileBrowserEntry, content []byte) error,
	progress func(FileBrowserBackupProgress),
) (FileBrowserBackupResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if sink == nil {
		return FileBrowserBackupResult{}, errors.New("backup sink is required")
	}
	normalizedFolders, err := normalizeFileBrowserBackupFolders(folders)
	if err != nil {
		return FileBrowserBackupResult{}, err
	}
	if err := ctx.Err(); err != nil {
		return FileBrowserBackupResult{}, err
	}

	s.fileBrowserOperationMu.Lock()
	defer s.fileBrowserOperationMu.Unlock()
	scope, err := s.captureSession()
	if err != nil {
		return FileBrowserBackupResult{}, err
	}
	s.fileBrowserBackupActive.Store(true)
	defer s.fileBrowserBackupActive.Store(false)

	report := func(value FileBrowserBackupProgress) {
		if progress != nil {
			progress(value)
		}
	}

	filesByPath := make(map[string]fileBrowserBackupFile)
	var bytesTotal int64
	for index, folder := range normalizedFolders {
		if err := ctx.Err(); err != nil {
			return FileBrowserBackupResult{}, err
		}
		report(FileBrowserBackupProgress{
			Phase:        "scanning",
			Folder:       folder,
			FoldersDone:  index,
			FoldersTotal: len(normalizedFolders),
			FilesTotal:   len(filesByPath),
			BytesTotal:   bytesTotal,
		})
		entries, err := s.listFileBrowserFolderContext(ctx, scope, folder)
		if err != nil {
			return FileBrowserBackupResult{}, fmt.Errorf("read remote folder %q: %w", folder, err)
		}
		for _, entry := range entries {
			if entry.IsDirectory {
				continue
			}
			remotePath, err := fileBrowserBackupEntryPath(folder, entry.Path)
			if err != nil {
				return FileBrowserBackupResult{}, fmt.Errorf("invalid remote file %q: %w", entry.Path, err)
			}
			if _, exists := filesByPath[remotePath]; exists {
				continue
			}
			filesByPath[remotePath] = fileBrowserBackupFile{path: remotePath, entry: entry}
			if entry.Size > 0 {
				bytesTotal += int64(entry.Size)
			}
		}
		report(FileBrowserBackupProgress{
			Phase:        "scanning",
			Folder:       folder,
			FoldersDone:  index + 1,
			FoldersTotal: len(normalizedFolders),
			FilesTotal:   len(filesByPath),
			BytesTotal:   bytesTotal,
		})
	}

	orderedFiles := make([]fileBrowserBackupFile, 0, len(filesByPath))
	for _, file := range filesByPath {
		orderedFiles = append(orderedFiles, file)
	}
	sort.Slice(orderedFiles, func(i, j int) bool { return orderedFiles[i].path < orderedFiles[j].path })

	report(FileBrowserBackupProgress{
		Phase:        "downloading",
		FoldersDone:  len(normalizedFolders),
		FoldersTotal: len(normalizedFolders),
		FilesTotal:   len(orderedFiles),
		BytesTotal:   bytesTotal,
	})

	result := FileBrowserBackupResult{}
	for index, file := range orderedFiles {
		if err := ctx.Err(); err != nil {
			return FileBrowserBackupResult{}, err
		}
		report(FileBrowserBackupProgress{
			Phase:        "downloading",
			File:         file.path,
			FoldersDone:  len(normalizedFolders),
			FoldersTotal: len(normalizedFolders),
			FilesDone:    index,
			FilesTotal:   len(orderedFiles),
			BytesDone:    result.Bytes,
			BytesTotal:   bytesTotal,
		})
		content, err := s.downloadFileContext(ctx, scope, file.path)
		if err != nil {
			return FileBrowserBackupResult{}, fmt.Errorf("download remote file %q: %w", file.path, err)
		}
		if err := sink(file.path, file.entry, content); err != nil {
			return FileBrowserBackupResult{}, fmt.Errorf("save local backup file %q: %w", file.path, err)
		}
		result.Files++
		result.Bytes += int64(len(content))
		report(FileBrowserBackupProgress{
			Phase:        "downloading",
			File:         file.path,
			FoldersDone:  len(normalizedFolders),
			FoldersTotal: len(normalizedFolders),
			FilesDone:    result.Files,
			FilesTotal:   len(orderedFiles),
			BytesDone:    result.Bytes,
			BytesTotal:   bytesTotal,
		})
	}
	return result, nil
}
