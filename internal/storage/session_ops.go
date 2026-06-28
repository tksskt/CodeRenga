package storage

import (
	"context"
	"database/sql"
	"fmt"
)

func (s *Store) SessionByID(ctx context.Context, id string) (Session, error) {
	var v Session
	e := s.DB.QueryRowContext(ctx, `SELECT id,project_path,COALESCE(title,''),COALESCE(active_mode,''),COALESCE(active_profile,''),status,COALESCE(config_fingerprint,''),COALESCE(prompt_fingerprint,''),created_at,updated_at FROM sessions WHERE id=?`, id).Scan(&v.ID, &v.ProjectPath, &v.Title, &v.Mode, &v.Profile, &v.Status, &v.ConfigFingerprint, &v.PromptFingerprint, &v.CreatedAt, &v.UpdatedAt)
	if e == sql.ErrNoRows {
		return v, fmt.Errorf("session %q not found", id)
	}
	return v, e
}
