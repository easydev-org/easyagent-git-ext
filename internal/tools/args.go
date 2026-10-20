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
// The credential is sent as a PRE-EMPTIVE Authorization header, not through a
// credential.helper. A helper is only consulted after the server has issued a 401
// challenge, and Gitea answers an unauthenticated push with the opaque
// "remote: Verify" — which is why every helper-based attempt failed while the
// same username and token worked from a plain `git push` with http.extraHeader.
//
// Security: the secret never appears in the command. `$GIT_TOKEN` is expanded by
// the shell at run time, so `ps` and the job log show the variable, not its value.
func gitWithToken(cmd, user string) string {
	u := strings.ReplaceAll(user, `"`, ``)
	// base64("user:token") is computed inside the shell, from the variable.
	return `IFS= read -r GIT_TOKEN; ` +
		`GIT_BASIC="$(printf '%s:%s' ` + shellQuote(u) + ` "$GIT_TOKEN" | base64 | tr -d '\n')"; ` +
		`git -c http.extraHeader="Authorization: Basic $GIT_BASIC" ` +
		strings.TrimPrefix(cmd, "git ") +
		`; __rc=$?; unset GIT_TOKEN GIT_BASIC; exit $__rc`
}
