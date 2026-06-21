package runtime

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestNonInteractiveCoderRunsAllowedWrite(t *testing.T) {
	root := t.TempDir()
	rt := newModePolicyRuntime(t, root, "coder", "allow", "allow", true, []string{
		`{"tool":"builtin.write_file","arguments":{"path":"worker.txt","content":"hello"}}`,
		"done",
	})
	defer rt.Close()
	rt.Approve = func(string, map[string]any) bool { t.Fatal("allowed non-interactive write prompted"); return false }
	if err := rt.RunInstruction(context.Background(), "write worker.txt", &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(root, "worker.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "hello" {
		t.Fatalf("content=%q", b)
	}
}
