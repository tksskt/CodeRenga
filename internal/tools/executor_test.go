package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/tks/coderenga/internal/storage"
)

func TestExecutorAuditsPolicyReasons(t *testing.T) {
	ctx := context.Background()
	store, err := storage.Open("", true)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.CreateSession(ctx, "s", "p", "coder", "local"); err != nil {
		t.Fatal(err)
	}
	registry := NewRegistry()
	if err := registry.Register(testExecutorTool("builtin.test")); err != nil {
		t.Fatal(err)
	}
	executor := &Executor{Registry: registry, Store: store, PolicyDecision: func(string) Level { return Block }}
	res, err := executor.Execute(ctx, Request{Name: "builtin.test", Arguments: map[string]any{}, Context: Context{SessionID: "s", Mode: "coder"}})
	if err != nil {
		t.Fatal(err)
	}
	if res.OK {
		t.Fatal("blocked tool unexpectedly succeeded")
	}
	var eventJSON string
	if err := store.DB.QueryRowContext(ctx, `SELECT event_json FROM audit_logs WHERE event_type='tool_finished' ORDER BY id DESC LIMIT 1`).Scan(&eventJSON); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(eventJSON, "policy_reasons") || !strings.Contains(eventJSON, "tool_policy: block") || !strings.Contains(eventJSON, "tool manifest policy: allow") {
		t.Fatalf("audit event did not include policy reasons: %s", eventJSON)
	}
}

type testExecutorTool string

func (t testExecutorTool) Name() string        { return string(t) }
func (t testExecutorTool) Description() string { return string(t) }
func (t testExecutorTool) Policy() Level       { return Allow }
func (t testExecutorTool) Execute(context.Context, Request) (Result, error) {
	return Result{OK: true, Content: "ok"}, nil
}
