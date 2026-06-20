package runtime

import (
	"context"
	"github.com/tks/coderenga/internal/mcp"
)

func (rt *Runtime) registerMCPTool(ctx context.Context, server string, info mcp.ToolInfo, client mcp.Client) error {
	if err := rt.Store.CacheMCPTool(ctx, server, info.Name, info.InputSchema, info.Description); err != nil {
		return err
	}
	return rt.Registry.Replace(mcp.Bridge{Server: server, Info: info, Client: client})
}
