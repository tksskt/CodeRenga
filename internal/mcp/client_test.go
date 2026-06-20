package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHTTPInitializeAndDiscover(t *testing.T) {
	initialized := false
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		if e := json.NewDecoder(r.Body).Decode(&req); e != nil {
			t.Error(e)
			return
		}
		method, _ := req["method"].(string)
		if method == "notifications/initialized" {
			initialized = true
			w.WriteHeader(http.StatusAccepted)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch method {
		case "initialize":
			w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-03-26","capabilities":{}}}`))
		case "tools/list":
			w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"tools":[{"name":"search","description":"Search","inputSchema":{}}]}}`))
		}
	}))
	defer s.Close()
	c := &HTTPClient{s.URL, s.Client()}
	if e := Initialize(context.Background(), c); e != nil {
		t.Fatal(e)
	}
	items, e := Discover(context.Background(), c)
	if e != nil || len(items) != 1 || !initialized {
		t.Fatalf("items=%v initialized=%t err=%v", items, initialized, e)
	}
}
