package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/tks/coderenga/internal/config"
	"github.com/tks/coderenga/internal/tools"
	"net/http"
	"strings"
)

func Discover(ctx context.Context, c Client) ([]ToolInfo, error) {
	raw, e := c.Call(ctx, "tools/list", map[string]any{})
	if e != nil {
		return nil, e
	}
	var v struct {
		Tools []struct {
			Name        string          `json:"name"`
			Description string          `json:"description"`
			InputSchema json.RawMessage `json:"inputSchema"`
		} `json:"tools"`
	}
	if e = json.Unmarshal(raw, &v); e != nil {
		return nil, e
	}
	out := make([]ToolInfo, len(v.Tools))
	for i, t := range v.Tools {
		out[i] = ToolInfo{t.Name, t.Description, t.InputSchema}
	}
	return out, nil
}

type Bridge struct {
	Server string
	Info   ToolInfo
	Client Client
}

func (b Bridge) Name() string        { return "mcp." + sanitize(b.Server) + "." + sanitize(b.Info.Name) }
func (b Bridge) Description() string { return b.Info.Description }
func (b Bridge) Policy() tools.Level { return tools.Unknown }
func (b Bridge) Execute(ctx context.Context, r tools.Request) (tools.Result, error) {
	args := map[string]any{}
	for k, v := range r.Arguments {
		if !strings.HasPrefix(k, "_coderenga_") {
			args[k] = v
		}
	}
	raw, e := b.Client.Call(ctx, "tools/call", map[string]any{"name": b.Info.Name, "arguments": args})
	if e != nil {
		return tools.Result{}, e
	}
	return tools.Result{OK: true, Content: string(raw)}, nil
}
func sanitize(s string) string { return strings.ReplaceAll(strings.TrimSpace(s), ".", "_") }
func Connect(ctx context.Context, _ string, cfg config.MCPServer) (Client, error) {
	switch cfg.Transport {
	case "stdio":
		return NewStdio(ctx, cfg.Command, cfg.Args)
	case "http_sse":
		return NewSSE(ctx, cfg.URL)
	case "http":
		return &HTTPClient{cfg.URL, &http.Client{}}, nil
	default:
		return nil, fmt.Errorf("unsupported MCP transport %q", cfg.Transport)
	}
}
