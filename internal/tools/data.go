package tools

import (
	"context"
	"reflect"

	"github.com/easydev-org/easybus-sdk/extension"
)

/**
 * normaliseData rewrites a tool's `Data` into shapes every layer below can carry.
 *
 * WHY THIS EXISTS: the result travels as protobuf Struct, whose values are scalars, lists and maps.
 * A Go STRUCT — or a slice of them, which is what the GitHub client returns for files, runs and
 * issues — is not one of those, and the conversion does not fail loudly: the whole `data` field
 * arrives as `null`. No error, no log, and a client that reads a field off it draws an empty card.
 *
 * This was found by testing every tool: `gh-pr-diff` returned data and `gh-pr-files` returned null,
 * and the only difference between them was a `[]FileChange` in the payload.
 *
 * Converting here, at the ONE place every tool's result passes through, is what makes it safe to add
 * a tool that returns a struct without having to remember this rule.
 */
func normaliseData(d map[string]any) map[string]any {
	if d == nil {
		return nil
	}
	out := make(map[string]any, len(d))
	for k, v := range d {
		out[k] = normaliseValue(v)
	}
	return out
}

// normaliseValue converts one value, recursing into what it contains.
func normaliseValue(v any) any {
	if v == nil {
		return nil
	}
	// Scalars and the two container types the transport already understands pass through, so a
	// payload that is already fine is not rebuilt.
	switch v.(type) {
	case string, bool, float64, float32, int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64:
		return v
	case map[string]any:
		return normaliseData(v.(map[string]any))
	case []any:
		return normaliseSlice(v.([]any))
	case []byte:
		// Raw bytes are not a Struct value; base64 is what JSON would do with them anyway.
		return string(v.([]byte))
	}

	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Ptr:
		if rv.IsNil() {
			return nil
		}
		return normaliseValue(rv.Elem().Interface())
	case reflect.Slice, reflect.Array:
		// A []FileChange or []Run: convert element by element.
		out := make([]any, 0, rv.Len())
		for i := 0; i < rv.Len(); i++ {
			out = append(out, normaliseValue(rv.Index(i).Interface()))
		}
		return out
	case reflect.Map:
		out := make(map[string]any, rv.Len())
		iter := rv.MapRange()
		for iter.Next() {
			out[toStringKey(iter.Key())] = normaliseValue(iter.Value().Interface())
		}
		return out
	case reflect.Struct:
		return structToMap(rv)
	}
	// Anything else (a channel, a func) cannot be represented; nil is the honest answer, and it is
	// better than dropping the whole payload.
	return nil
}

func normaliseSlice(in []any) []any {
	out := make([]any, len(in))
	for i, v := range in {
		out[i] = normaliseValue(v)
	}
	return out
}

// structToMap uses the struct's exported fields, honouring a `json` tag when present.
func structToMap(rv reflect.Value) map[string]any {
	rt := rv.Type()
	out := make(map[string]any, rt.NumField())
	for i := 0; i < rt.NumField(); i++ {
		f := rt.Field(i)
		if !f.IsExported() {
			continue
		}
		name := f.Name
		if tag := f.Tag.Get("json"); tag != "" && tag != "-" {
			if comma := indexByte(tag, ','); comma >= 0 {
				tag = tag[:comma]
			}
			if tag != "" {
				name = tag
			}
		}
		out[name] = normaliseValue(rv.Field(i).Interface())
	}
	return out
}

// toStringKey renders a map key for a JSON object, which only has string keys.
func toStringKey(k reflect.Value) string {
	if k.Kind() == reflect.String {
		return k.String()
	}
	return ""
}

func indexByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}

/**
 * withNormalisedData wraps a handler so its `Data` is always transport-safe.
 *
 * One wrapper at the registry, rather than a rule each handler must remember: the failure is silent,
 * so a handler that forgets produces a card with nothing in it and no clue why.
 */
func withNormalisedData(h Handler) Handler {
	return func(ctx context.Context, args map[string]any, session, tenant string) (extension.ToolResultData, error) {
		res, err := h(ctx, args, session, tenant)
		if err != nil {
			return res, err
		}
		// `Data` is `any`: a handler may already return a map, and the wrapper must not assume it.
		res.Data = normaliseValue(res.Data)
		return res, nil
	}
}
