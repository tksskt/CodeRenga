package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

type Store struct {
	DB         *sql.DB
	Persistent bool
	Path       string
}

func Open(path string, noPersist bool) (*Store, error) {
	dsn := path
	persistent := !noPersist && path != ""
	if !persistent {
		dsn = "file:coderenga-memory?mode=memory&cache=shared"
	}
	db, e := sql.Open("sqlite", dsn)
	if e != nil {
		return nil, e
	}
	s := &Store{db, persistent, path}
	if !persistent {
		if _, e = db.Exec(schema); e != nil {
			db.Close()
			return nil, e
		}
	}
	return s, nil
}
func (s *Store) Close() error { return s.DB.Close() }
func (s *Store) CreateSession(ctx context.Context, id, project, mode, profile string) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, e := s.DB.ExecContext(ctx, `INSERT INTO sessions(id,project_path,project_hash,title,active_mode,active_profile,status,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?)`, id, project, project, "", mode, profile, "active", now, now)
	return e
}

type Session struct{ ID, ProjectPath, Title, Mode, Profile, Status, CreatedAt, UpdatedAt string }

func (s *Store) Sessions(ctx context.Context, query string) ([]Session, error) {
	q := `SELECT id,project_path,COALESCE(title,''),COALESCE(active_mode,''),COALESCE(active_profile,''),status,created_at,updated_at FROM sessions`
	args := []any{}
	if query != "" {
		q += ` WHERE id LIKE ? OR title LIKE ?`
		args = []any{"%" + query + "%", "%" + query + "%"}
	}
	q += ` ORDER BY updated_at DESC`
	rows, e := s.DB.QueryContext(ctx, q, args...)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	var out []Session
	for rows.Next() {
		var v Session
		if e = rows.Scan(&v.ID, &v.ProjectPath, &v.Title, &v.Mode, &v.Profile, &v.Status, &v.CreatedAt, &v.UpdatedAt); e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (s *Store) AddMessage(ctx context.Context, sid, role, content string) (int64, error) {
	r, e := s.DB.ExecContext(ctx, `INSERT INTO messages(session_id,role,content,created_at) VALUES(?,?,?,?)`, sid, role, content, time.Now().UTC().Format(time.RFC3339Nano))
	if e != nil {
		return 0, e
	}
	return r.LastInsertId()
}
func (s *Store) RecentMessages(ctx context.Context, sid string, limit int) ([]struct {
	ID            int64
	Role, Content string
}, error) {
	rows, e := s.DB.QueryContext(ctx, `SELECT id,role,content FROM messages WHERE session_id=? AND compacted=0 ORDER BY id DESC LIMIT ?`, sid, limit)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	var rev []struct {
		ID            int64
		Role, Content string
	}
	for rows.Next() {
		var v struct {
			ID            int64
			Role, Content string
		}
		if e = rows.Scan(&v.ID, &v.Role, &v.Content); e != nil {
			return nil, e
		}
		rev = append(rev, v)
	}
	for i, j := 0, len(rev)-1; i < j; i, j = i+1, j-1 {
		rev[i], rev[j] = rev[j], rev[i]
	}
	return rev, rows.Err()
}
func (s *Store) Audit(ctx context.Context, sid, event string, data any) error {
	b, _ := json.Marshal(data)
	_, e := s.DB.ExecContext(ctx, `INSERT INTO audit_logs(session_id,event_type,event_json,created_at) VALUES(?,?,?,?)`, nullable(sid), event, string(b), time.Now().UTC().Format(time.RFC3339Nano))
	return e
}
func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}
func (s *Store) ToolRun(ctx context.Context, sid, name, ns, args, result, status, decision string, approved bool, d time.Duration) error {
	_, e := s.DB.ExecContext(ctx, `INSERT INTO tool_runs(session_id,tool_name,namespace,arguments_json,result_summary,result_full,status,policy_decision,approved,duration_ms,started_at,finished_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, sid, name, ns, args, result, result, status, decision, boolInt(approved), d.Milliseconds(), time.Now().Add(-d).UTC().Format(time.RFC3339Nano), time.Now().UTC().Format(time.RFC3339Nano))
	return e
}
func (s *Store) ShellRun(ctx context.Context, sid, command, cwd string, exit int, stdout, stderr, level string, approved bool, d time.Duration) error {
	_, e := s.DB.ExecContext(ctx, `INSERT INTO shell_runs(session_id,command,cwd,exit_code,stdout_summary,stderr_summary,stdout_full,stderr_full,policy_level,approved_by_user,started_at,finished_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, sid, command, cwd, exit, clip(stdout), clip(stderr), stdout, stderr, level, boolInt(approved), time.Now().Add(-d).UTC().Format(time.RFC3339Nano), time.Now().UTC().Format(time.RFC3339Nano))
	return e
}
func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
func clip(s string) string {
	if len(s) > 512 {
		return s[:512]
	}
	return s
}
func (s *Store) Compact(ctx context.Context, sid, level, content string, toID int64) error {
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if _, e = tx.ExecContext(ctx, `UPDATE summaries SET active=0 WHERE session_id=?`, sid); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO summaries(session_id,level,content,source_to_message_id,active,created_at) VALUES(?,?,?,?,1,?)`, sid, level, content, toID, time.Now().UTC().Format(time.RFC3339Nano)); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, `UPDATE messages SET compacted=1 WHERE session_id=? AND id<=?`, sid, toID); e != nil {
		return e
	}
	return tx.Commit()
}
func (s *Store) ActiveSummary(ctx context.Context, sid string) (string, error) {
	var v string
	e := s.DB.QueryRowContext(ctx, `SELECT content FROM summaries WHERE session_id=? AND active=1 ORDER BY id DESC LIMIT 1`, sid).Scan(&v)
	if e == sql.ErrNoRows {
		return "", nil
	}
	return v, e
}
func (s *Store) Status(ctx context.Context) string {
	var n int
	if e := s.DB.QueryRowContext(ctx, `SELECT count(*) FROM sessions`).Scan(&n); e != nil {
		return e.Error()
	}
	return fmt.Sprintf("database: %s\npersistent: %t\nsessions: %d", s.Path, s.Persistent, n)
}
