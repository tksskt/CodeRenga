package storage

import (
	"context"
	"time"
)

func (s *Store) CacheMCPTool(ctx context.Context, server, name string, schema []byte, description string) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO mcp_tools_cache(server_name,tool_name,schema_json,description,updated_at)
		VALUES(?,?,?,?,?) ON CONFLICT(server_name,tool_name) DO UPDATE SET schema_json=excluded.schema_json,description=excluded.description,updated_at=excluded.updated_at`,
		server, name, string(schema), description, time.Now().UTC().Format(time.RFC3339Nano))
	return err
}
