//go:build !windows

package app

import "errors"

func cryptSecret(value string, encrypt bool) (string, error) {
	return "", errors.New("此版本的密钥保存仅支持 Windows DPAPI")
}
