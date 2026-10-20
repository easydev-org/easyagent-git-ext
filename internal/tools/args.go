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
// The username is the TOKEN ITSELF when `user` is empty. That looks odd but is
// what Gitea actually accepts: it treats the basic-auth user as an account name
// and looks it up, so the conventional placeholders ("token", "x-access-token")
// fail there with "user does not exist ... name: <placeholder>" and the client
// only sees "remote: Verify". Passing the token as BOTH parts works because Gitea
// resolves a PAT in the username position.
//
// Security: the secret is never written into the command. `$GIT_TOKEN` is
// expanded by the shell inside the helper at run time, so `ps` and the job log
// show the variable, never its value.
func gitWithToken(cmd, user string) string {
	// `user` empty => the helper echoes "$GIT_TOKEN" as the username too, so both
	// halves of the credential stay in the variable.
	userExpr := `"` + strings.ReplaceAll(user, `"`, ``) + `"`
	if user == "" {
		userExpr = `"$GIT_TOKEN"`
	}
	helper := `!f() { echo "username=` + userExpr + `"; echo "password=$GIT_TOKEN"; }; f`
	return `IFS= read -r GIT_TOKEN; git -c credential.helper='` + helper + `' ` + strings.TrimPrefix(cmd, "git ") + `; __rc=$?; unset GIT_TOKEN; exit $__rc`
}
