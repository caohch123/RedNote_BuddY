//go:build !windows

package app

import "os"

func deletionReparsePoint(_ string, info os.FileInfo) bool { return info.Mode()&os.ModeSymlink != 0 }
