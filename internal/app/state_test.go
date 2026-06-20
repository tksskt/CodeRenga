package app

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func TestExplicitStateDirBootstrapsDatabase(t *testing.T) {
	root := t.TempDir()
	writeTestConfig(t, root)
	state := filepath.Join(root, "state")
	var out, stderr bytes.Buffer
	code := Run([]string{"--cwd", root, "--state-dir", state}, strings.NewReader("/db status\n/exit\n"), &out, &stderr, Options{ExecutableDir: root})
	if code != 0 {
		t.Fatalf("code=%d stderr=%q", code, stderr.String())
	}
	if _, err := filepath.Glob(filepath.Join(state, "coderenga.db")); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "persistent: true") {
		t.Fatalf("output=%q", out.String())
	}
}
