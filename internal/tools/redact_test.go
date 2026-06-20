package tools

import "testing"

func TestRedactSecrets(t *testing.T) {
	got := redact(map[string]any{"api_key": "x", "nested": map[string]any{"password": "y"}, "path": "ok"})
	if got["api_key"] != "[REDACTED]" || got["path"] != "ok" || got["nested"].(map[string]any)["password"] != "[REDACTED]" {
		t.Fatalf("got %#v", got)
	}
}
