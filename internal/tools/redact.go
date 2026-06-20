package tools

import "strings"

func redact(value map[string]any) map[string]any {
	out := make(map[string]any, len(value))
	for k, v := range value {
		lower := strings.ToLower(k)
		if strings.Contains(lower, "token") || strings.Contains(lower, "secret") || strings.Contains(lower, "password") || strings.Contains(lower, "api_key") {
			out[k] = "[REDACTED]"
			continue
		}
		if nested, ok := v.(map[string]any); ok {
			out[k] = redact(nested)
		} else {
			out[k] = v
		}
	}
	return out
}
