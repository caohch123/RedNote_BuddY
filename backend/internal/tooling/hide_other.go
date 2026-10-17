//go:build !windows

package tooling

import "os/exec"

func hide(cmd *exec.Cmd) {}
