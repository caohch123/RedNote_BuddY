package tooling

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func init() {
	if value := os.Getenv("WORKBENCH_TOOL_TEST_VERSION"); value != "" && len(os.Args) == 2 && (os.Args[1] == "--version" || os.Args[1] == "-version") {
		if value == "wait" {
			time.Sleep(10 * time.Second)
		}
		fmt.Println(value)
		os.Exit(0)
	}
}

func toolFixture(t *testing.T, dir, name string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	path := filepath.Join(dir, name)
	source, _ := os.Executable()
	// Windows cannot unlink another hard link to the running test executable.
	if runtime.GOOS != "windows" {
		if err := os.Link(source, path); err == nil {
			return path
		}
	}
	{
		input, err := os.Open(source)
		if err != nil {
			t.Fatal(err)
		}
		defer input.Close()
		output, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0755)
		if err != nil {
			t.Fatal(err)
		}
		_, err = io.Copy(output, input)
		output.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
	return path
}

func TestToolChecksAndCommandsUseSameExplicitRelativeAndAutomaticPath(t *testing.T) {
	root := t.TempDir()
	path := toolFixture(t, root, "node")
	t.Setenv("PATH", root)
	t.Setenv("WORKBENCH_TOOL_TEST_VERSION", "v24.1.0")
	for _, configured := range []string{"", filepath.Base(path), path, `"` + path + `"`, "./" + filepath.Base(path)} {
		result := Check(context.Background(), root, configured, "node")
		cmd := Command(context.Background(), root, configured, "node", "--version")
		if !result.OK || result.Path != path || cmd.Path != result.Path || result.Version != "v24.1.0" {
			t.Fatalf("path mismatch for %q: %+v %s", configured, result, cmd.Path)
		}
		if _, err := cmd.Output(); err != nil {
			t.Fatal(err)
		}
	}
	for _, configured := range []string{filepath.Join(root, "missing-node"), root} {
		if result := Check(context.Background(), root, configured, "node"); result.OK {
			t.Fatal("invalid explicit path fell back to PATH")
		}
		if _, err := Command(context.Background(), root, configured, "node", "--version").Output(); err == nil {
			t.Fatal("runtime ignored invalid explicit path")
		}
	}
}

func TestToolChecksRejectOldVersionsWrongBinariesAndTimeouts(t *testing.T) {
	path := toolFixture(t, t.TempDir(), "custom-tool")
	for _, item := range []struct {
		name, version string
		ok            bool
	}{
		{"node", "v20.1.0", false}, {"node", "v22.0.0", true}, {"node", "Cookie=SECRET", false},
		{"ffmpeg", "ffprobe version 9.0", false}, {"ffmpeg", "ffmpeg version 9.0", true}, {"ffprobe", "ffprobe version 9.0", true},
	} {
		t.Setenv("WORKBENCH_TOOL_TEST_VERSION", item.version)
		result := Check(context.Background(), "", path, item.name)
		if result.OK != item.ok || strings.Contains(result.Message, "SECRET") || (!item.ok && result.Version != "") {
			t.Fatalf("incorrect tool status: %+v", result)
		}
	}
	t.Setenv("WORKBENCH_TOOL_TEST_VERSION", "wait")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	started := time.Now()
	if result := Check(ctx, "", path, "node"); result.OK || time.Since(started) > time.Second {
		t.Fatal("tool timeout didn't stop check")
	}
}
