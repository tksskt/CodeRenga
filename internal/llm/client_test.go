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
	parallel := true
	p := config.Profile{BaseURL: s.URL, Model: "m", ToolProtocol: "llamacpp_tools", ToolChoice: "auto", ParallelToolCalls: &parallel, NativeTools: []map[string]any{{"type": "function"}}}
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

func TestLlamaCppToolsReservedExtraBodyKeysCannotOverrideManagedFields(t *testing.T) {
	var request map[string]any
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		fmt.Fprintln(w, `{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}]}`)
	}))
	defer s.Close()
	p := config.Profile{
		BaseURL:      s.URL,
		Model:        "managed-model",
		ToolProtocol: "llamacpp_tools",
		ToolChoice:   "required",
		ExtraBody: map[string]any{
			"model":               "evil-model",
			"messages":            []any{"evil"},
			"stream":              true,
			"tools":               []any{map[string]any{"type": "evil"}},
			"tool_choice":         "none",
			"parallel_tool_calls": true,
			"top_k":               40,
		},
		NativeTools: []map[string]any{{"type": "function", "function": map[string]any{"name": "builtin__read_file"}}},
	}
	if _, err := New().ChatResult(context.Background(), p, []Message{{Role: "user", Content: "read"}}, true, nil); err != nil {
		t.Fatal(err)
	}
	if request["model"] != "managed-model" || request["stream"] != false || request["tool_choice"] != "required" || request["parallel_tool_calls"] != false {
		t.Fatalf("reserved fields were not protected: %#v", request)
	}
	if got := request["messages"].([]any)[0].(map[string]any)["content"]; got != "read" {
		t.Fatalf("messages overridden: %#v", request["messages"])
	}
	tools, ok := request["tools"].([]any)
	if !ok || len(tools) != 1 || tools[0].(map[string]any)["type"] != "function" {
		t.Fatalf("tools overridden: %#v", request["tools"])
	}
	if request["top_k"] != float64(40) {
		t.Fatalf("non-reserved extra body field missing: %#v", request)
	}
}

func TestLlamaCppToolsOmitsToolFieldsWhenNativeToolsEmpty(t *testing.T) {
	for _, tc := range []struct {
		name  string
		tools []map[string]any
	}{
		{name: "nil"},
		{name: "empty", tools: []map[string]any{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var request map[string]any
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Fatal(err)
				}
				fmt.Fprintln(w, `{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}]}`)
			}))
			defer s.Close()
			p := config.Profile{BaseURL: s.URL, Model: "m", ToolProtocol: "llamacpp_tools", ToolChoice: "required", NativeTools: tc.tools}
			if _, err := New().ChatResult(context.Background(), p, []Message{{Role: "user", Content: "hello"}}, true, nil); err != nil {
				t.Fatal(err)
			}
			if request["stream"] != false {
				t.Fatalf("llamacpp_tools must force non-stream: %#v", request)
			}
			for _, key := range []string{"tools", "tool_choice", "parallel_tool_calls"} {
				if _, ok := request[key]; ok {
					t.Fatalf("%s should be omitted when native tools are empty: %#v", key, request)
				}
			}
		})
	}
}

func TestPromptJSONReservedExtraBodyKeysCannotOverrideManagedFields(t *testing.T) {
	var request map[string]any
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		fmt.Fprintln(w, `{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}]}`)
	}))
	defer s.Close()
	p := config.Profile{
		BaseURL: s.URL,
		Model:   "managed-model",
		ExtraBody: map[string]any{
			"model":               "evil-model",
			"messages":            []any{"evil"},
			"stream":              true,
			"tools":               []any{map[string]any{"type": "evil"}},
			"tool_choice":         "required",
			"parallel_tool_calls": true,
			"top_p":               0.5,
		},
	}
	if _, err := New().ChatResult(context.Background(), p, []Message{{Role: "user", Content: "hello"}}, false, nil); err != nil {
		t.Fatal(err)
	}
	if request["model"] != "managed-model" || request["stream"] != false {
		t.Fatalf("reserved fields were not protected: %#v", request)
	}
	if got := request["messages"].([]any)[0].(map[string]any)["content"]; got != "hello" {
		t.Fatalf("messages overridden: %#v", request["messages"])
	}
	for _, key := range []string{"tools", "tool_choice", "parallel_tool_calls"} {
		if _, ok := request[key]; ok {
			t.Fatalf("%s should be omitted for prompt_json request: %#v", key, request)
		}
	}
	if request["top_p"] != 0.5 {
		t.Fatalf("non-reserved extra body field missing: %#v", request)
	}
}
