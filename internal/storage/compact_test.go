package storage

import (
	"context"
	"testing"
)

func TestCompactPreservesRawMessages(t *testing.T) {
	s, err := Open("", true)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	if err = s.CreateSession(ctx, "s", "p", "coder", "local"); err != nil {
		t.Fatal(err)
	}
	id, err := s.AddMessage(ctx, "s", "user", "raw")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Compact(ctx, "s", "normal", "summary", id); err != nil {
		t.Fatal(err)
	}
	var content string
	var compacted int
	if err = s.DB.QueryRow(`SELECT content,compacted FROM messages WHERE id=?`, id).Scan(&content, &compacted); err != nil {
		t.Fatal(err)
	}
	if content != "raw" || compacted != 1 {
		t.Fatalf("content=%q compacted=%d", content, compacted)
	}
	summary, err := s.ActiveSummary(ctx, "s")
	if err != nil || summary != "summary" {
		t.Fatalf("summary=%q err=%v", summary, err)
	}
}
