package main

import (
	"context"
	"errors"

	filedraglib "graal-rc/internal/filedrag"
)

// GetFileDragBase returns the URL prefix the File Browser puts in a drag so a
// remote file can be dropped straight onto Explorer. The window appends the
// escaped remote path to it; the file is downloaded only when it is dropped.
//
// The prefix holds a secret that is created for this run of the application and
// handed only to the windows that ask for it. See package filedrag.
func (a *App) GetFileDragBase() (string, error) {
	if a == nil || a.sessions == nil {
		return "", errors.New("file browser is unavailable")
	}
	a.fileDragOnce.Do(func() {
		a.fileDrag = filedraglib.New(func(ctx context.Context, remotePath string) ([]byte, error) {
			return a.sessions.DownloadFileContext(ctx, remotePath)
		})
	})
	return a.fileDrag.Base()
}

func (a *App) stopFileDragServer() {
	if a == nil || a.fileDrag == nil {
		return
	}
	_ = a.fileDrag.Close()
}
