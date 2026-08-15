//go:build windows

package main

import (
	"os/exec"
	"testing"
)

func TestConfigureDetachedUpdateCommandUsesNoWindowProcessFlags(t *testing.T) {
	command := exec.Command("powershell.exe")
	configureDetachedUpdateCommand(command)
	if command.SysProcAttr == nil {
		t.Fatal("detached updater did not configure Windows process attributes")
	}
	const (
		detachedProcess    = 0x00000008
		createNoWindow     = 0x08000000
		createNewProcGroup = 0x00000200
	)
	want := uint32(detachedProcess | createNoWindow | createNewProcGroup)
	if command.SysProcAttr.CreationFlags&want != want {
		t.Fatalf("CreationFlags = %#x, want detached/no-window flags %#x", command.SysProcAttr.CreationFlags, want)
	}
	if !command.SysProcAttr.HideWindow {
		t.Fatal("detached updater should hide the helper window")
	}
}
