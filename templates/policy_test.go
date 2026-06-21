package templatefs

import (
	"encoding/json"
	"io/fs"
	"strings"
	"testing"
)

func TestInitialModeAndToolPolicies(t *testing.T) {
	modes := map[string]string{"coder.md": "write: allow", "debug.md": "write: confirm", "architect.md": "write: false", "reviewer.md": "write: false"}
	for name, expected := range modes {
		body, err := fs.ReadFile(Files, "coderenga.d/modes/"+name); if err != nil { t.Fatal(err) }
		if !strings.Contains(string(body), expected) { t.Fatalf("%s does not contain %q: %s", name, expected, body) }
	}
	body, err := fs.ReadFile(Files, "coderenga.d/tools.json"); if err != nil { t.Fatal(err) }
	var config struct { Policies map[string]string `json:"policies"` }
	if err := json.Unmarshal(body, &config); err != nil { t.Fatalf("%v body=%q", err, body) }
	if config.Policies["builtin.write_file"] != "allow" || config.Policies["builtin.apply_patch"] != "allow" || config.Policies["shell.run"] != "confirm" { t.Fatalf("policies=%v", config.Policies) }
}


