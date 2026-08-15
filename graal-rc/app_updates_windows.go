//go:build windows

package main

import (
	"fmt"
	"os/exec"
	"strings"
	"syscall"

	"golang.org/x/sys/windows"
)

// launchDetachedUpdateHelper starts the updater through the Windows shell
// broker. Elevating the helper before the RC exits prevents a child process
// inherited from the GUI process from being terminated with it, while the
// helper itself still waits before replacing the installed executable.
func launchDetachedUpdateHelper(scriptPath string) error {
	powershellPath, err := exec.LookPath("powershell.exe")
	if err != nil {
		return fmt.Errorf("resolve Windows PowerShell for updater: %w", err)
	}

	verb, err := windows.UTF16PtrFromString("runas")
	if err != nil {
		return fmt.Errorf("prepare updater elevation verb: %w", err)
	}
	file, err := windows.UTF16PtrFromString(powershellPath)
	if err != nil {
		return fmt.Errorf("prepare Windows PowerShell path: %w", err)
	}
	parameters, err := windows.UTF16PtrFromString(strings.Join([]string{
		"-NoLogo",
		"-NoProfile",
		"-NonInteractive",
		"-ExecutionPolicy",
		"Bypass",
		"-WindowStyle",
		"Hidden",
		"-File",
		quoteWindowsArgument(scriptPath),
	}, " "))
	if err != nil {
		return fmt.Errorf("prepare updater arguments: %w", err)
	}

	if err := windows.ShellExecute(0, verb, file, parameters, nil, windows.SW_HIDE); err != nil {
		return fmt.Errorf("start elevated updater helper: %w", err)
	}
	return nil
}

func quoteWindowsArgument(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `\"`) + `"`
}

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
