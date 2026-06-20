package storage

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestToolOutputStoresSummaryAndRaw(t *testing.T) {
	s, err := Open("", true)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	if err = s.CreateSession(ctx, "s", "p", "coder", "local"); err != nil {
		t.Fatal(err)
	}
	raw := strings.Repeat("x", 1024)
	if err = s.ToolRunDetailed(ctx, "s", "builtin.read_file", "builtin", "{}", raw, "ok", "allow", true, time.Millisecond); err != nil {
		t.Fatal(err)
	}
	var summary, full string
	if err = s.DB.QueryRow(`SELECT result_summary,result_full FROM tool_runs`).Scan(&summary, &full); err != nil {
		t.Fatal(err)
	}
	if len(summary) != 512 || full != raw {
		t.Fatalf("summary=%d full=%d", len(summary), len(full))
	}
}
