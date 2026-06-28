package storage

import "context"

func (s *Store) UncompactedCount(ctx context.Context, sessionID string) (int, error) {
	var n int
	err := s.DB.QueryRowContext(ctx, `SELECT count(*) FROM messages WHERE session_id=? AND compacted=0`, sessionID).Scan(&n)
	return n, err
}

func (s *Store) UncompactedTokenEstimate(ctx context.Context, sessionID string) (int, error) {
	var n int
	err := s.DB.QueryRowContext(ctx, `SELECT COALESCE(sum(token_estimate),0) FROM messages WHERE session_id=? AND compacted=0`, sessionID).Scan(&n)
	return n, err
}
