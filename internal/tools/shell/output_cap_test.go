package shell

import (
	"strings"
	"testing"
)

func TestCapOutputTruncatesLongOutput(t *testing.T) {
	value := strings.Repeat("x", outputCapBytes+10)
	got := capOutput(value)
	if len(got) <= outputCapBytes || !strings.Contains(got, "[output truncated]") {
		t.Fatalf("output was not truncated: len=%d", len(got))
	}
}
