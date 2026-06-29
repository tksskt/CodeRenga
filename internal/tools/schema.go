package tools

type SchemaProvider interface {
	Schema() map[string]any
}

func ObjectSchema(properties map[string]any, required []string) map[string]any {
	if required == nil {
		required = []string{}
	}
	return map[string]any{
		"type":                 "object",
		"properties":           properties,
		"required":             required,
		"additionalProperties": false,
	}
}

func StringProperty(description string) map[string]any {
	return map[string]any{"type": "string", "description": description}
}

func BoolProperty(description string) map[string]any {
	return map[string]any{"type": "boolean", "description": description}
}

func StringArrayProperty(description string) map[string]any {
	return map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": description}
}
