package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/tks/coderenga/internal/config"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStreaming(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `data: {"choices":[{"delta":{"content":"hi"}}]}`)
		fmt.Fprintln(w, "data: [DONE]")
	}))
	defer s.Close()
	var b strings.Builder
	p := config.Profile{BaseURL: s.URL, Model: "m"}
	got, e := New().Chat(context.Background(), p, nil, true, func(v string) error { b.WriteString(v); return nil })
	if e != nil || got != "hi" || b.String() != "hi" {
		t.Fatalf("got=%q err=%v", got, e)
	}
}

func TestLlamaCppToolsNonStreamRequestAndResponse(t *testing.T) {
	var request map[string]any
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		fmt.Fprintln(w, `{"choices":[{"message":{"content":null,"reasoning_content":"thinking","tool_calls":[{"id":"call_x","type":"function","function":{"name":"builtin__read_file","arguments":"{\"path\":\"README.md\"}"}}]},"finish_reason":"tool_calls"}]}`)
	}))
	defer s.Close()
	p := config.Profile{BaseURL: s.URL, Model: "m", ToolProtocol: "llamacpp_tools", ToolChoice: "auto", ExtraBody: map[string]any{"tools": []any{map[string]any{"type": "function"}}}}
	got, err := New().ChatResult(context.Background(), p, []Message{{Role: "user", Content: "read"}}, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if request["stream"] != false || request["tool_choice"] != "auto" || request["parallel_tool_calls"] != false || request["tools"] == nil {
		t.Fatalf("bad request body: %#v", request)
	}
	if got.Content != "" || got.Reasoning != "thinking" || got.FinishReason != "tool_calls" || len(got.ToolCalls) != 1 || got.ToolCalls[0].Function.Name != "builtin__read_file" {
		t.Fatalf("bad result: %#v", got)
	}
}
