package llm

import (
	"context"
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
