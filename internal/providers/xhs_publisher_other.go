//go:build !windows

package providers

import "os/exec"

func hidePublisherWindow(cmd *exec.Cmd) {}
