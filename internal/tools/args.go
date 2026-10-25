package tools

import (
	"fmt"
	"strings"
)

// Argument helpers.
//
// Tool arguments arrive as map[string]any from JSON, so every read is a type assertion that
// can fail. These helpers make the failure explicit instead of silently yielding a zero value
// that the handler would then act on.

func argStr(args map[string]any, key string) string {
	v, ok := args[key]
	if !ok || v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return strings.TrimSpace(s)
	}
	return ""
}

func argBool(args map[string]any, key string) bool {
	v, ok := args[key]
	if !ok || v == nil {
		return false
	}
	b, _ := v.(bool)
	return b
}

// argBoolDefault reads a boolean, falling back to def when the key is ABSENT.
//
// Distinct from argBool because "absent" and "explicitly false" must differ when the default is
// true: with argBool alone, a parameter defaulting to true could never be turned off.
func argBoolDefault(args map[string]any, key string, def bool) bool {
	v, ok := args[key]
	if !ok || v == nil {
		return def
	}
	b, ok := v.(bool)
	if !ok {
		return def
	}
	return b
}

func argInt(args map[string]any, key string) int {
	v, ok := args[key]
	if !ok || v == nil {
		return 0
	}
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		// JSON numbers arrive as float64 through encoding/json.
		return int(n)
	}
	return 0
}

// argSliceStr reads a []any of strings, as JSON delivers it.
func argSliceStr(args map[string]any, key string) []string {
	v, ok := args[key]
	if !ok || v == nil {
		return nil
	}
	raw, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, e := range raw {
		if s, ok := e.(string); ok && strings.TrimSpace(s) != "" {
			out = append(out, strings.TrimSpace(s))
		}
	}
	return out
}

// required returns the value or an error naming the argument.
func required(args map[string]any, key string) (string, error) {
	s := argStr(args, key)
	if s == "" {
		return "", errArg(fmt.Sprintf("%q is required", key))
	}
	return s, nil
}
