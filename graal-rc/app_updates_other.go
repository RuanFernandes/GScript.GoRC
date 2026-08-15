//go:build !windows

package main

import (
	"errors"
	"os/exec"
)

func configureDetachedUpdateCommand(_ *exec.Cmd) {}

func launchDetachedUpdateHelper(_ string) error {
	return errors.New("the Windows updater helper is unavailable on this platform")
}
