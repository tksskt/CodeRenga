package runtime

import (
	"context"

	"github.com/tks/coderenga/internal/mcp"
)

func (rt *Runtime) registerMCPTool(ctx context.Context, server string, info mcp.ToolInfo, client mcp.Client, reload bool) error {
	if err := rt.Store.CacheMCPTool(ctx, server, info.Name, info.InputSchema, info.Description); err != nil {
		return err
	}
	tool := mcp.Bridge{Server: server, Info: info, Client: client}
	if reload {
		return rt.Registry.Replace(tool)
	}
	return rt.Registry.RegisterDynamic(tool)
}
