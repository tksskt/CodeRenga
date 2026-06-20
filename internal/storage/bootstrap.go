package storage

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

const schema = `
PRAGMA foreign_keys = ON;
CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL);
CREATE TABLE sessions (id TEXT PRIMARY KEY, project_path TEXT NOT NULL, project_hash TEXT NOT NULL, title TEXT, active_mode TEXT, active_profile TEXT, status TEXT NOT NULL, config_fingerprint TEXT, prompt_fingerprint TEXT, created_at TEXT NOT NULL, updated_at TEXT NOT NULL);
CREATE TABLE messages (id INTEGER PRIMARY KEY AUTOINCREMENT, session_id TEXT NOT NULL, role TEXT NOT NULL, content TEXT NOT NULL, token_estimate INTEGER, compacted INTEGER NOT NULL DEFAULT 0, created_at TEXT NOT NULL, FOREIGN KEY(session_id) REFERENCES sessions(id));
CREATE TABLE tool_runs (id INTEGER PRIMARY KEY AUTOINCREMENT, session_id TEXT NOT NULL, message_id INTEGER, tool_name TEXT NOT NULL, namespace TEXT NOT NULL, arguments_json TEXT, result_summary TEXT, result_full TEXT, status TEXT NOT NULL, policy_decision TEXT, approved INTEGER NOT NULL DEFAULT 0, duration_ms INTEGER, started_at TEXT NOT NULL, finished_at TEXT, FOREIGN KEY(session_id) REFERENCES sessions(id), FOREIGN KEY(message_id) REFERENCES messages(id));
CREATE TABLE shell_runs (id INTEGER PRIMARY KEY AUTOINCREMENT, session_id TEXT NOT NULL, command TEXT NOT NULL, cwd TEXT NOT NULL, exit_code INTEGER, stdout_summary TEXT, stderr_summary TEXT, stdout_full TEXT, stderr_full TEXT, policy_level TEXT NOT NULL, approved_by_user INTEGER NOT NULL DEFAULT 0, started_at TEXT NOT NULL, finished_at TEXT, FOREIGN KEY(session_id) REFERENCES sessions(id));
CREATE TABLE summaries (id INTEGER PRIMARY KEY AUTOINCREMENT, session_id TEXT NOT NULL, level TEXT NOT NULL, content TEXT NOT NULL, source_from_message_id INTEGER, source_to_message_id INTEGER, active INTEGER NOT NULL DEFAULT 0, created_at TEXT NOT NULL, FOREIGN KEY(session_id) REFERENCES sessions(id));
CREATE TABLE checkpoints (id INTEGER PRIMARY KEY AUTOINCREMENT, session_id TEXT NOT NULL, summary_id INTEGER, git_head TEXT, changed_files_json TEXT, created_at TEXT NOT NULL, FOREIGN KEY(session_id) REFERENCES sessions(id), FOREIGN KEY(summary_id) REFERENCES summaries(id));
CREATE TABLE mcp_tools_cache (id INTEGER PRIMARY KEY AUTOINCREMENT, server_name TEXT NOT NULL, tool_name TEXT NOT NULL, schema_json TEXT, description TEXT, updated_at TEXT NOT NULL, UNIQUE(server_name, tool_name));
CREATE TABLE audit_logs (id INTEGER PRIMARY KEY AUTOINCREMENT, session_id TEXT, event_type TEXT NOT NULL, event_json TEXT NOT NULL, created_at TEXT NOT NULL);
INSERT INTO schema_migrations(version, applied_at) VALUES (1, CURRENT_TIMESTAMP);
`

func Bootstrap(path string) error {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return fmt.Errorf("open SQLite database: %w", err)
	}
	defer db.Close()
	if _, err := db.Exec(schema); err != nil {
		return fmt.Errorf("apply schema migration 1: %w", err)
	}
	return nil
}
