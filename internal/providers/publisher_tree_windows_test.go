//go:build windows

package providers

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func TestOwnedPublisherTreeHelper(t *testing.T) {
	switch os.Getenv("DOUYIN_TREE_TEST_HELPER") {
	case "child":
		time.Sleep(30 * time.Second)
		os.Exit(0)
	case "parent":
		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			var request struct {
				ID uint64 `json:"id"`
			}
			json.Unmarshal(scanner.Bytes(), &request)
			child := exec.Command(os.Args[0], "-test.run=^TestOwnedPublisherTreeHelper$")
			child.Env = append(os.Environ(), "DOUYIN_TREE_TEST_HELPER=child")
			hidePublisherWindow(child)
			if err := child.Start(); err != nil {
				os.Exit(1)
			}
			json.NewEncoder(os.Stdout).Encode(map[string]any{"id": request.ID, "result": map[string]any{"child_pid": child.Process.Pid}})
			child.Wait()
		}
		os.Exit(0)
	}
}

func TestOwnedPublisherCloseKillsBrowserDescendants(t *testing.T) {
	p := NewXHSPublisher("unused-test-root")
	p.ownTree = true
	p.command = func() (*exec.Cmd, error) {
		cmd := exec.Command(os.Args[0], "-test.run=^TestOwnedPublisherTreeHelper$")
		cmd.Env = append(os.Environ(), "DOUYIN_TREE_TEST_HELPER=parent")
		hidePublisherWindow(cmd)
		return cmd, nil
	}
	defer p.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var result struct {
		ChildPID uint32 `json:"child_pid"`
	}
	if err := p.Call(ctx, "spawn", nil, &result); err != nil {
		t.Fatal(err)
	}
	handle, err := windows.OpenProcess(windows.SYNCHRONIZE, false, result.ChildPID)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(handle)
	if err = p.Close(); err != nil {
		t.Fatal(err)
	}
	state, err := windows.WaitForSingleObject(handle, 2000)
	if err != nil || state != windows.WAIT_OBJECT_0 {
		t.Fatalf("owned child survived publisher close: state %d err %v", state, err)
	}
}
