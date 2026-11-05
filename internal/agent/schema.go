package agent

// The schemas are written out as maps rather than inferred from Go
// structs, for two reasons. The enums come from the domain
// (domain.AllUnits), which a struct tag cannot do, and this is the exact
// JSON the model is shown — "additionalProperties": false included,
// which is what makes a misspelled field a refusal instead of a silently
// dropped value.

// object is a closed object schema: anything not listed is rejected.
func object(props map[string]any, required ...string) map[string]any {
	schema := map[string]any{
		"type":                 "object",
		"properties":           props,
		"additionalProperties": false,
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

// noArgs is the schema for a tool that takes nothing. It still has to be
// an object: the protocol has no other shape for arguments.
func noArgs() map[string]any { return object(map[string]any{}) }

func str(description string) map[string]any {
	return map[string]any{"type": "string", "description": description}
}

func enum(description string, values []string) map[string]any {
	options := make([]any, 0, len(values))
	for _, v := range values {
		options = append(options, v)
	}
	return map[string]any{"type": "string", "description": description, "enum": options}
}

func number(description string) map[string]any {
	return map[string]any{"type": "number", "description": description}
}

func integer(description string) map[string]any {
	return map[string]any{"type": "integer", "description": description}
}

func boolean(description string) map[string]any {
	return map[string]any{"type": "boolean", "description": description}
}

func array(description string, items map[string]any) map[string]any {
	return map[string]any{"type": "array", "description": description, "items": items}
}

// portionList is the shape a vocabulary is given in, used by both
// record_events and update_item. A list of pairs rather than an object
// keyed by the word: the keys are the person's own words, and a schema
// cannot close an object whose keys are unknown.
func portionList(description string) map[string]any {
	return array(description, object(map[string]any{
		"name": str(descPortionEntryName),
		"size": number(descPortionEntrySize),
	}, "name", "size"))
}
