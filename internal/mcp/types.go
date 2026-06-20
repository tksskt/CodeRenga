package mcp

import (
	"context"
	"encoding/json"
)

type rpcReq struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int    `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}
type rpcResp struct {
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}
type ToolInfo struct {
	Name, Description string
	InputSchema       json.RawMessage
}
type Client interface {
	Call(context.Context, string, any) (json.RawMessage, error)
	Close() error
}
