package runtime

import (
	"context"
	"io"
)

func (rt *Runtime) addMessage(ctx context.Context, role, content string) (int64, error) {
	id, err := rt.Store.AddMessage(ctx, rt.SessionID, role, content)
	if err != nil {
		return 0, err
	}
	if !rt.Config.Compact.Enabled || rt.Config.Compact.TriggerTurns <= 0 {
		return id, nil
	}
	count, err := rt.Store.UncompactedCount(ctx, rt.SessionID)
	if err == nil && count >= rt.Config.Compact.TriggerTurns*2 {
		_ = rt.compact(ctx, "normal", io.Discard)
	}
	return id, nil
}
