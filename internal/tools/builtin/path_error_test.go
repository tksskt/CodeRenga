package builtin

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tks/coderenga/internal/tools"
)

func TestReadMissingPathReturnsFriendlyCWDRelativeError(t *testing.T) {
	root := t.TempDir()
	_, err := read(context.Background(), tools.Request{Arguments: map[string]any{"path": "acenga.d"}, Context: tools.Context{CWD: root}})
	if err == nil || !strings.Contains(err.Error(), `path "acenga.d" does not exist within cwd`) {
		t.Fatalf("err=%v", err)
	}
	if strings.Contains(err.Error(), "GetFileAttributesEx") {
		t.Fatalf("raw OS error leaked: %v", err)
	}
}

func TestWriteAllowsNewNestedDirectoryWithinCWD(t *testing.T) {
	root := t.TempDir()
	request := tools.Request{Arguments: map[string]any{"path": "new/sub/file.txt", "content": "content"}, Context: tools.Context{CWD: root}}
	if result, err := write(context.Background(), request); err != nil || !result.OK {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	b, err := os.ReadFile(filepath.Join(root, "new", "sub", "file.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "content" {
		t.Fatalf("content=%q", b)
	}
}
