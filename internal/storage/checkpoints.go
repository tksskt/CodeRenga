package storage

import (
	"context"
	"encoding/json"
	"time"
)

func (s *Store) SaveCheckpoint(ctx context.Context, sessionID string, summaryID *int64, gitHead string, changedFiles []string) error {
	files, _ := json.Marshal(changedFiles)
	_, err := s.DB.ExecContext(ctx, `INSERT INTO checkpoints(session_id,summary_id,git_head,changed_files_json,created_at) VALUES(?,?,?,?,?)`, sessionID, summaryID, gitHead, string(files), time.Now().UTC().Format(time.RFC3339Nano))
	return err
}
