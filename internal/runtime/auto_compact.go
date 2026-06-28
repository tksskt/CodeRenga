package runtime

import "context"

func (rt *Runtime) addMessage(ctx context.Context, role, content string) (int64, error) {
	return rt.Store.AddMessage(ctx, rt.SessionID, role, content)
}

func (rt *Runtime) addMessageNoCompact(ctx context.Context, role, content string) (int64, error) {
	return rt.addMessage(ctx, role, content)
}

func (rt *Runtime) maybeAutoCompact(ctx context.Context) error {
	if !rt.Config.Compact.Enabled {
		return nil
	}
	if rt.Config.Compact.TriggerContextRatio > 0 {
		estimate, err := rt.Store.UncompactedTokenEstimate(ctx, rt.SessionID)
		limit := rt.currentContextTokenLimit()
		if err == nil && limit > 0 && float64(estimate)/float64(limit) >= rt.Config.Compact.TriggerContextRatio {
			_ = rt.compact(ctx, rt.autoCompactLevel(), discardWriter{})
			return nil
		}
	}
	if rt.Config.Compact.TriggerTurns <= 0 {
		return nil
	}
	count, err := rt.Store.UncompactedCount(ctx, rt.SessionID)
	if err == nil && count >= rt.Config.Compact.TriggerTurns*2 {
		_ = rt.compact(ctx, rt.autoCompactLevel(), discardWriter{})
	}
	return nil
}

func (rt *Runtime) autoCompactLevel() string {
	if rt.Config.Compact.Level == "" {
		return "normal"
	}
	return rt.Config.Compact.Level
}

func (rt *Runtime) currentContextTokenLimit() int {
	if rt.Config.Compact.ContextTokens > 0 {
		return rt.Config.Compact.ContextTokens
	}
	return 4096
}

type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }
