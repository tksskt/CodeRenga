package templatefs

import (
	"encoding/json"
	"io/fs"
	"strings"
	"testing"
)

func TestCoderengaToolsPolicy(t *testing.T) {
	data, err := fs.ReadFile(Files, "coderenga.d/tools.json")
	if err != nil {
		t.Fatalf("failed to read tools.json: %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("failed to parse tools.json: %v", err)
	}

	// Assert tool_policy exists
	toolPolicyRaw, ok := raw["tool_policy"]
	if !ok {
		t.Fatal("tools.json is missing tool_policy")
	}

	var toolPolicy map[string]interface{}
	if err := json.Unmarshal(toolPolicyRaw, &toolPolicy); err != nil {
		t.Fatalf("failed to parse tool_policy: %v", err)
	}

	// Assert explicit tool policy entries
	expectedTools := map[string]string{
		"builtin.read_file":   "allow",
		"builtin.write_file":  "allow",
		"builtin.apply_patch": "allow",
		"shell.run":           "confirm",
		"git.status":          "allow",
		"git.diff":            "allow",
	}

	for tool, want := range expectedTools {
		got, exists := toolPolicy[tool]
		if !exists {
			t.Fatalf("tool_policy is missing %q", tool)
		}
		if got != want {
			t.Fatalf("tool_policy[%q] = %q; want %q", tool, got, want)
		}
	}

	// Assert shell_policy exists and unknown is confirm
	shellPolicyRaw, ok := raw["shell_policy"]
	if !ok {
		t.Fatal("tools.json is missing shell_policy")
	}

	var shellPolicy map[string]interface{}
	if err := json.Unmarshal(shellPolicyRaw, &shellPolicy); err != nil {
		t.Fatalf("failed to parse shell_policy: %v", err)
	}

	unknown, ok := shellPolicy["unknown"]
	if !ok {
		t.Fatal("shell_policy is missing unknown")
	}
	if unknown != "confirm" {
		t.Fatalf("shell_policy.unknown = %q; want \"confirm\"", unknown)
	}
}

func TestCoderengaModeWritePolicy(t *testing.T) {
	expectedModes := map[string]string{
		"coder.md":     "\nwrite: allow\n",
		"debug.md":     "\nwrite: confirm\n",
		"architect.md": "\nwrite: false\n",
		"reviewer.md":  "\nwrite: false\n",
		"documenter.md": "\nwrite: allow\n",
	}

	for name, expected := range expectedModes {
		body, err := fs.ReadFile(Files, "coderenga.d/modes/"+name)
		if err != nil {
			t.Fatalf("failed to read %s: %v", name, err)
		}

		text := strings.ReplaceAll(string(body), "\r\n", "\n")
		if !strings.Contains(text, expected) {
			t.Fatalf("%s does not contain %q", name, expected)
		}
	}
}

func TestWorkerPromptTemplateHardening(t *testing.T) {
	body, err := fs.ReadFile(Files, "coderenga.d/prompts/default.md")
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	for _, want := range []string{"Critical tool-call protocol", "exactly one JSON object", "Do not repeat the same tool call", "The write example demonstrates the JSON protocol only"} {
		if !strings.Contains(text, want) {
			t.Fatalf("default prompt missing %q", want)
		}
	}
}

func TestReviewerPromptLanguageNeutral(t *testing.T) {
	body, err := fs.ReadFile(Files, "coderenga.d/modes/reviewer.md")
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	for _, want := range []string{
		"user's language",
		"No significant defects were found",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("reviewer prompt missing %q", want)
		}
	}
}

func TestCompactionPromptKeepsHandoffDetails(t *testing.T) {
	body, err := fs.ReadFile(Files, "coderenga.d/prompts/compact.md")
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	for _, want := range []string{
		"confirmed facts",
		"assumptions",
		"unresolved questions",
		"exact file paths",
		"what was verified",
		"what was not tested",
		"next concrete action",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("compact prompt missing %q", want)
		}
	}
}

func TestDocumenterModeTemplate(t *testing.T) {
	body, err := fs.ReadFile(Files, "coderenga.d/modes/documenter.md")
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	for _, want := range []string{"name: documenter", "tool_allow:", "builtin.apply_patch"} {
		if !strings.Contains(text, want) {
			t.Fatalf("documenter mode missing %q", want)
		}
	}
}
func TestPublicContractPreservationPromptTemplates(t *testing.T) {
	checks := map[string][]string{
		"coderenga.d/prompts/default.md": {
			"Public contract preservation",
			"JSON keys, CLI flags, output formats, file names, function names",
			"If the specification says `line`, keep `line`",
			"do not change it to `line_number`, `lineNo`, `lineNum`",
		},
		"coderenga.d/modes/coder.md": {
			"Public contract discipline",
			"Preserve exact names and shapes",
			"if a spec requires `line`, do not output `line_number`",
			"If a spec requires `--format text`, keep that form working",
		},
	}
	for path, wants := range checks {
		body, err := fs.ReadFile(Files, path)
		if err != nil {
			t.Fatalf("failed to read %s: %v", path, err)
		}
		text := string(body)
		for _, want := range wants {
			if !strings.Contains(text, want) {
				t.Fatalf("%s missing %q", path, want)
			}
		}
	}
}
