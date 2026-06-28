package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tks/coderenga/internal/config"
	"github.com/tks/coderenga/internal/mcp"
	"github.com/tks/coderenga/internal/storage"
	"github.com/tks/coderenga/internal/tools"
)

func TestToolReloadReloadsMCPTools(t *testing.T) {
	ctx := context.Background()
	store, err := storage.Open("", true)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch req["method"] {
		case "initialize":
			w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-03-26","capabilities":{}}}`))
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
		case "tools/list":
			w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"tools":[{"name":"search","description":"Search","inputSchema":{}},{"name":"search","description":"Duplicate Search","inputSchema":{}}]}}`))
		default:
			t.Fatalf("unexpected method: %v", req["method"])
		}
	}))
	defer server.Close()

	oldClient := &trackingMCPClient{}
	rt := &Runtime{
		Store:           store,
		Registry:        tools.NewRegistry(),
		MCP:             map[string]mcp.Client{"web": oldClient},
		ToolDiagnostics: map[string]string{},
		Config: config.Config{MCP: config.MCPConfig{Enabled: true, Servers: map[string]config.MCPServer{
			"web": {Transport: "http", URL: server.URL, Enabled: true},
		}}},
	}
	if err := rt.Registry.RegisterDynamic(mcp.Bridge{Server: "web", Info: mcp.ToolInfo{Name: "old", Description: "Old", InputSchema: json.RawMessage(`{}`)}, Client: oldClient}); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := rt.toolCommand(ctx, []string{"/tool", "reload"}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "tools reloaded") {
		t.Fatalf("output=%q", out.String())
	}
	if _, ok := rt.Registry.Info("mcp.web.search"); !ok {
		t.Fatalf("mcp.web.search was not registered; names=%v diagnostics=%v", rt.Registry.Names(), rt.Diagnostics)
	}
	if _, ok := rt.Registry.Info("mcp.web.old"); ok {
		t.Fatalf("stale MCP tool was not removed on reload; names=%v", rt.Registry.Names())
	}
	if !oldClient.closed {
		t.Fatal("old MCP client was not closed on reload")
	}
	if !strings.Contains(rt.ToolDiagnostics["mcp.web.search"], "duplicate tool in reload batch") {
		t.Fatalf("duplicate MCP tool was not diagnosed: %#v", rt.ToolDiagnostics)
	}
}

func TestInitialMCPRegistrationRefusesDuplicate(t *testing.T) {
	ctx := context.Background()
	store, err := storage.Open("", true)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	rt := &Runtime{Store: store, Registry: tools.NewRegistry()}
	client := fakeMCPClient{}
	info := mcp.ToolInfo{Name: "search", Description: "first", InputSchema: json.RawMessage(`{}`)}
	if err := rt.registerMCPTool(ctx, "web", info, client, false); err != nil {
		t.Fatal(err)
	}
	info.Description = "second"
	if err := rt.registerMCPTool(ctx, "web", info, client, false); err == nil || !strings.Contains(err.Error(), "duplicate tool") {
		t.Fatalf("expected duplicate error, got %v", err)
	}
	tool, ok := rt.Registry.Info("mcp.web.search")
	if !ok || tool.Description() != "first" {
		t.Fatalf("duplicate registration overwrote first tool: ok=%t desc=%q", ok, tool.Description())
	}
	if err := rt.registerMCPTool(ctx, "web", info, client, true); err != nil {
		t.Fatal(err)
	}
	tool, ok = rt.Registry.Info("mcp.web.search")
	if !ok || tool.Description() != "second" {
		t.Fatalf("reload did not replace MCP tool: ok=%t desc=%q", ok, tool.Description())
	}
}

type fakeMCPClient struct{}

func (fakeMCPClient) Call(context.Context, string, any) (json.RawMessage, error) {
	return json.RawMessage(`{}`), nil
}
func (fakeMCPClient) Close() error { return nil }

type trackingMCPClient struct{ closed bool }

func (c *trackingMCPClient) Call(context.Context, string, any) (json.RawMessage, error) {
	return json.RawMessage(`{}`), nil
}
func (c *trackingMCPClient) Close() error { c.closed = true; return nil }
