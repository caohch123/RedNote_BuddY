package providers

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"xhs-workbench/internal/tooling"
)

func TestPublisherUsesConfiguredNode(t *testing.T) {
	root := t.TempDir()
	node, _ := os.Executable()
	python := filepath.Join(root, "publisher", ".venv", "bin", "python")
	if runtime.GOOS == "windows" {
		python = filepath.Join(root, "publisher", ".venv", "Scripts", "python.exe")
	}
	if err := os.MkdirAll(filepath.Dir(python), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(python, []byte("fixture"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WORKBENCH_NODE_PATH", "unrelated-ambient-node")
	selected := node
	source := func() (string, error) { return selected, nil }
	xhs := NewXHSPublisher(root, source)
	cmd, err := xhs.workerCommand()
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := tooling.Resolve(root, selected, "node")
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, value := range cmd.Env {
		if strings.HasPrefix(value, "WORKBENCH_NODE_PATH=") {
			count++
			if value != "WORKBENCH_NODE_PATH="+resolved {
				t.Fatal("signer used another Node")
			}
		}
		if strings.HasPrefix(value, "PATH=") && !strings.HasPrefix(value, "PATH="+filepath.Dir(resolved)+string(os.PathListSeparator)) {
			t.Fatal("vendor signer path differs")
		}
	}
	if count != 1 {
		t.Fatal("ambiguous signer node environment")
	}
	selected = filepath.Join(root, "missing-node.exe")
	if _, err = xhs.workerCommand(); err == nil {
		t.Fatal("publisher fell back to ambient Node")
	}
}
