package tools

import (
	"strings"
)

func argStr(args map[string]any, key string) string {
	v, _ := args[key].(string)
	return strings.TrimSpace(v)
}

func argInt(args map[string]any, key string, def int) int {
	switch v := args[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	case int64:
		return int(v)
	}
	return def
}

func argBool(args map[string]any, key string, def bool) bool {
	if v, ok := args[key].(bool); ok {
		return v
	}
	return def
}

func argStrList(args map[string]any, key string) []string {
	raw, ok := args[key].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
			out = append(out, strings.TrimSpace(s))
		}
	}
	return out
}

// shellQuote single-quotes a value for the sandbox shell.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// gitWithToken wraps a git command so it reads the credential from the shell
// variable GIT_TOKEN (fed via stdin, never in the command/env). The helper
// prints the username + token; `unset` clears it immediately after.
func gitWithToken(cmd, user string) string {
	helper := `!f() { echo "username=` + strings.ReplaceAll(user, `"`, ``) + `"; echo "password=$GIT_TOKEN"; }; f`
	return `IFS= read -r GIT_TOKEN; git -c credential.helper='` + helper + `' ` + strings.TrimPrefix(cmd, "git ") + `; __rc=$?; unset GIT_TOKEN; exit $__rc`
}
