package storage

import (
	"context"
	"time"
)

func (s *Store) ToolRunDetailed(ctx context.Context, sid, name, namespace, args, result, status, decision string, approved bool, duration time.Duration) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO tool_runs(session_id,tool_name,namespace,arguments_json,result_summary,result_full,status,policy_decision,approved,duration_ms,started_at,finished_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, sid, name, namespace, args, clip(result), result, status, decision, boolInt(approved), duration.Milliseconds(), time.Now().Add(-duration).UTC().Format(time.RFC3339Nano), time.Now().UTC().Format(time.RFC3339Nano))
	return err
}
