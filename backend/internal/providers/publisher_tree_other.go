//go:build !windows

package providers

import "os/exec"

func ownPublisherTree(cmd *exec.Cmd) (func(), error) { return func() { _ = cmd.Process.Kill() }, nil }
