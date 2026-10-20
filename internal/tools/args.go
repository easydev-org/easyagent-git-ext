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

// gitWithToken wraps a git command so the credential comes from the shell
// variable GIT_TOKEN, fed via stdin.
//
// The username must be the REAL account name. Gitea looks the basic-auth user up
// as an account, so:
//   - "token" / "x-access-token" -> "user does not exist", client sees
//     "remote: Verify" (no hint that the username was the problem);
//   - the token in the username slot -> "remote: Unauthorized".
//
// Only `<real-user>:<token>` authenticates, so the caller must configure `user`
// on the server entry whenever it needs to PUSH.
//
// Security: the secret is never written into the command. `$GIT_TOKEN` is
// expanded by the shell inside the helper at run time, so `ps` and the job log
// show the variable, never its value.
func gitWithToken(cmd, user string) string {
	userExpr := `"` + strings.ReplaceAll(user, `"`, ``) + `"`
	helper := `!f() { echo "username=` + userExpr + `"; echo "password=$GIT_TOKEN"; }; f`
	return `IFS= read -r GIT_TOKEN; git -c credential.helper='` + helper + `' ` + strings.TrimPrefix(cmd, "git ") + `; __rc=$?; unset GIT_TOKEN; exit $__rc`
}
