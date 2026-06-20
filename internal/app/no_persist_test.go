package app

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNoPersistDoesNotCreateStateDatabase(t *testing.T) {
	root := t.TempDir()
	writeTestConfig(t, root)
	state := filepath.Join(root, "state")
	var out, errout bytes.Buffer
	code := Run([]string{"--cwd", root, "--state-dir", state, "--no-persist"}, strings.NewReader("/exit\n"), &out, &errout, Options{ExecutableDir: root})
	if code != 0 {
		t.Fatalf("code=%d stderr=%q", code, errout.String())
	}
	if _, err := os.Stat(filepath.Join(state, "coderenga.db")); !os.IsNotExist(err) {
		t.Fatalf("database was created: %v", err)
	}
}
