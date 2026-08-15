//go:build !windows

package main

import "os/exec"

func configureDetachedUpdateCommand(_ *exec.Cmd) {}
