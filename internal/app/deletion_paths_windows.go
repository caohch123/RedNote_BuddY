//go:build windows

package app

import (
	"os"
	"syscall"
)

// A junction is a reparse point even on Go versions which expose it as an
// ordinary directory and do not resolve it through filepath.EvalSymlinks.
func deletionReparsePoint(path string, _ os.FileInfo) bool {
	encoded, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return true
	}
	attributes, err := syscall.GetFileAttributes(encoded)
	return err != nil || attributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT != 0
}
