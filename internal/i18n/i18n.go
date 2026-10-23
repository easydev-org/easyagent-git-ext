// Package i18n holds the extension's user-facing strings.
package i18n

import (
	"fmt"
	"strings"
)

// Message keys.
const (
	MsgNotConfigured = "not_configured"
	MsgUnknownRepo   = "unknown_repo"
)

var messages = map[string]map[string]string{
	"en": {
		MsgNotConfigured: "%{name} is not configured for this session",
		MsgUnknownRepo:   "unknown repository: %{repo}",
	},
	"zh": {
		MsgNotConfigured: "此会话未配置 %{name}",
		MsgUnknownRepo:   "未知仓库：%{repo}",
	},
}

// T returns the message for a locale, falling back to English and then to the key itself.
func T(locale, key string, params map[string]any) string {
	m, ok := messages[locale]
	if !ok {
		m = messages["en"]
	}
	s, ok := m[key]
	if !ok {
		s = messages["en"][key]
	}
	if s == "" {
		return key
	}
	// %{name} placeholders, substituted without a template dependency: the message set is tiny
	// and fixed, so a full template engine would be more moving parts than it saves.
	out := s
	for k, v := range params {
		ph := "%{" + k + "}"
		out = strings.ReplaceAll(out, ph, fmt.Sprint(v))
	}
	return out
}
