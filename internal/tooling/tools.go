// Package tooling resolves local tools identically for health checks and work.
package tooling

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

func Resolve(root, configured, name string) (string, error) {
	configured = strings.TrimSpace(configured)
	configured = strings.Trim(configured, `"`)
	if configured != "" {
		if filepath.Base(configured) == configured {
			path, err := exec.LookPath(configured)
			if err != nil {
				return "", errors.New("工具不在系统路径中")
			}
			return executable(path)
		}
		if !filepath.IsAbs(configured) {
			configured = filepath.Join(root, configured)
		}
		return executable(configured)
	}
	if path, err := exec.LookPath(name); err == nil {
		return executable(path)
	}
	file := name
	if runtime.GOOS == "windows" {
		file += ".exe"
	}
	candidates := []string{filepath.Join(root, "tools", name, "bin", file), filepath.Join(root, "tools", name, file)}
	if name == "ffprobe" {
		candidates = append(candidates, filepath.Join(root, "tools", "ffmpeg", "bin", file))
	}
	if runtime.GOOS == "windows" && name == "node" {
		for _, base := range []string{os.Getenv("ProgramFiles"), os.Getenv("ProgramFiles(x86)")} {
			if base != "" {
				candidates = append(candidates, filepath.Join(base, "nodejs", file))
			}
		}
	}
	if runtime.GOOS == "windows" && name == "msedge" {
		for _, base := range []string{os.Getenv("ProgramFiles(x86)"), os.Getenv("ProgramFiles"), os.Getenv("LOCALAPPDATA")} {
			if base != "" {
				candidates = append(candidates, filepath.Join(base, "Microsoft", "Edge", "Application", "msedge.exe"))
			}
		}
	}
	for _, path := range candidates {
		if resolved, err := executable(path); err == nil {
			return resolved, nil
		}
	}
	return "", errors.New("未找到工具，请安装或填写可执行文件路径")
}

func executable(path string) (string, error) {
	path, err := filepath.Abs(path)
	if err != nil {
		return "", errors.New("工具路径无效")
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return "", errors.New("工具文件不存在或不是可执行文件")
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0111 == 0 {
		return "", errors.New("工具文件没有执行权限")
	}
	return filepath.Clean(path), nil
}

func Command(ctx context.Context, root, configured, name string, args ...string) *exec.Cmd {
	path, err := Resolve(root, configured, name)
	cmd := exec.CommandContext(ctx, path, args...)
	if err != nil {
		cmd.Err = err
	}
	hide(cmd)
	return cmd
}

type CheckResult struct {
	OK      bool   `json:"ok"`
	Path    string `json:"path,omitempty"`
	Version string `json:"version,omitempty"`
	Message string `json:"message,omitempty"`
}

type boundedOutput struct{ bytes.Buffer }

func (b *boundedOutput) Write(p []byte) (int, error) {
	n := len(p)
	if left := 4096 - b.Len(); left > 0 {
		_, _ = b.Buffer.Write(p[:min(left, len(p))])
	}
	return n, nil
}

func Check(ctx context.Context, root, configured, name string) CheckResult {
	path, err := Resolve(root, configured, name)
	if err != nil {
		return CheckResult{Message: err.Error()}
	}
	result := CheckResult{Path: path}
	// Windows may scan a cold executable before its first launch. Keep the
	// check bounded without reporting an installed tool as missing during it.
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	arg := "-version"
	if name == "node" {
		arg = "--version"
	}
	cmd := Command(ctx, root, path, name, arg)
	var out boundedOutput
	cmd.Stdout, cmd.Stderr = &out, io.Discard
	if err := cmd.Run(); err != nil {
		result.Message = "工具无法启动或版本检测超时"
		return result
	}
	line := strings.TrimSpace(strings.SplitN(out.String(), "\n", 2)[0])
	if name == "node" {
		match := regexp.MustCompile(`^v(\d+)\.\d+\.\d+(?:[-+].*)?$`).FindStringSubmatch(line)
		if len(match) == 0 {
			result.Message = "该文件未返回有效的 Node.js 版本"
			return result
		}
		major, _ := strconv.Atoi(match[1])
		if major < 22 {
			result.Message = "需要 Node.js 22 或更高版本"
			return result
		}
	} else if !strings.HasPrefix(line, name+" version ") {
		result.Message = fmt.Sprintf("该文件未返回有效的 %s 版本", name)
		return result
	}
	result.OK, result.Version = true, line[:min(len(line), 160)]
	return result
}
