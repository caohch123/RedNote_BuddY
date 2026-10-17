//go:build windows

package app

import (
	"encoding/base64"
	"fmt"
	"golang.org/x/sys/windows"
	"unsafe"
)

type dataBlob struct {
	Size uint32
	Data *byte
}

var crypt32 = windows.NewLazySystemDLL("crypt32.dll")
var protect = crypt32.NewProc("CryptProtectData")
var unprotect = crypt32.NewProc("CryptUnprotectData")
var localFree = windows.NewLazySystemDLL("kernel32.dll").NewProc("LocalFree")

func cryptSecret(value string, encrypt bool) (string, error) {
	b := []byte(value)
	if !encrypt {
		var e error
		b, e = base64.StdEncoding.DecodeString(value)
		if e != nil {
			return "", e
		}
	}
	if len(b) == 0 {
		return "", nil
	}
	in := dataBlob{uint32(len(b)), &b[0]}
	var out dataBlob
	proc := protect
	if !encrypt {
		proc = unprotect
	}
	ok, _, e := proc.Call(uintptr(unsafe.Pointer(&in)), 0, 0, 0, 0, 1, uintptr(unsafe.Pointer(&out)))
	if ok == 0 {
		return "", fmt.Errorf("DPAPI: %w", e)
	}
	defer localFree.Call(uintptr(unsafe.Pointer(out.Data)))
	result := append([]byte(nil), unsafe.Slice(out.Data, int(out.Size))...)
	if encrypt {
		return base64.StdEncoding.EncodeToString(result), nil
	}
	return string(result), nil
}
