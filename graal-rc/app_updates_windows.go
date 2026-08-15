//go:build windows

package main

import (
	"os/exec"
	"syscall"
)

// configureDetachedUpdateCommand keeps the helper independent from the RC's
// console and process group. Without these flags Windows can keep a console
// host alive or attach the helper to the parent process during shutdown.
func configureDetachedUpdateCommand(command *exec.Cmd) {
	const (
		detachedProcess    = 0x00000008
		createNoWindow     = 0x08000000
		createNewProcGroup = 0x00000200
	)

	if command.SysProcAttr == nil {
		command.SysProcAttr = &syscall.SysProcAttr{}
	}
	command.SysProcAttr.CreationFlags |= detachedProcess | createNoWindow | createNewProcGroup
	command.SysProcAttr.HideWindow = true
}
