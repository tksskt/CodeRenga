package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
)

type Level int

const (
	Allow Level = iota
	Unknown
	Confirm
	Block
)

func (l Level) String() string { return [...]string{"allow", "unknown", "confirm", "block"}[l] }
func ParseLevel(s string) Level {
	switch s {
	case "allow":
		return Allow
	case "block":
		return Block
	case "confirm":
		return Confirm
	default:
		return Unknown
	}
}
func Max(v ...Level) Level {
	r := Allow
	for _, x := range v {
		if x > r {
			r = x
		}
	}
	return r
}

type Context struct {
	CWD, Mode, SessionID string
	DryRun               bool
}
type Request struct {
	Name      string         `json:"tool"`
	Arguments map[string]any `json:"arguments"`
	Context   Context        `json:"-"`
}
type Result struct {
	OK       bool           `json:"ok"`
	Content  string         `json:"content,omitempty"`
	Error    string         `json:"error,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}
type Tool interface {
	Name() string
	Description() string
	Policy() Level
	Execute(context.Context, Request) (Result, error)
}
type FileMutator interface {
	ModifiesFiles() bool
}
type Registry struct {
	mu       sync.RWMutex
	items    map[string]Tool
	disabled map[string]bool
}

func NewRegistry() *Registry { return &Registry{items: map[string]Tool{}, disabled: map[string]bool{}} }
func (r *Registry) Register(t Tool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := t.Name()
	if !qualified(n) {
		return fmt.Errorf("tool name %q is not fully qualified", n)
	}
	if _, ok := r.items[n]; ok {
		return fmt.Errorf("duplicate tool %q", n)
	}
	r.items[n] = t
	return nil
}
func qualified(n string) bool {
	p := strings.Split(n, ".")
	if len(p) < 2 {
		return false
	}
	switch p[0] {
	case "builtin", "shell", "git", "plugin":
		return len(p) == 2
	case "mcp":
		return len(p) >= 3
	}
	return false
}
func (r *Registry) Get(n string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.disabled[n] {
		return nil, false
	}
	t, ok := r.items[n]
	return t, ok
}
func (r *Registry) Info(n string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.items[n]
	return t, ok
}
func (r *Registry) SetEnabled(n string, enabled bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.items[n]; !ok {
		return fmt.Errorf("unknown tool %q", n)
	}
	r.disabled[n] = !enabled
	return nil
}
func (r *Registry) Enabled(n string) bool { r.mu.RLock(); defer r.mu.RUnlock(); return !r.disabled[n] }
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	v := make([]string, 0, len(r.items))
	for n := range r.items {
		v = append(v, n)
	}
	sort.Strings(v)
	return v
}

// ParseCalls accepts only standalone JSON tool-call objects. Prose and fenced
// JSON are deliberately not treated as executable requests.
func ParseCalls(text string) ([]Request, error) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return nil, nil
	}
	if strings.Contains(trimmed, "<tool") || strings.Contains(trimmed, "<|tool_call>") {
		return nil, fmt.Errorf("invalid tool call: legacy tag formats are not supported; output one JSON object with tool and arguments")
	}
	if !strings.HasPrefix(trimmed, "{") {
		return nil, nil
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal([]byte(trimmed), &envelope); err != nil {
		return nil, fmt.Errorf("invalid tool call: expected one JSON object with tool and arguments: %w", err)
	}
	if _, ok := envelope["tool"]; !ok {
		return nil, nil
	}
	decoder := json.NewDecoder(strings.NewReader(trimmed))
	decoder.DisallowUnknownFields()
	var req Request
	if err := decoder.Decode(&req); err != nil {
		return nil, fmt.Errorf("invalid tool call: expected only tool and arguments fields: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err == nil {
		return nil, fmt.Errorf("invalid tool call: multiple JSON values are not supported")
	}
	if !qualified(req.Name) {
		return nil, fmt.Errorf("invalid tool call: tool name %q is not fully qualified", req.Name)
	}
	if req.Arguments == nil {
		req.Arguments = map[string]any{}
	}
	return []Request{req}, nil
}
