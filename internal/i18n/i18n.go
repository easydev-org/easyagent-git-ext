// Package i18n localises the gitea-ext RUNTIME tool text (content + errors) using
// the easybus SDK's catalog core. Tool DESCRIPTIONS are localized through the
// manifest; this covers the results a tool returns.
package i18n

import (
	easybusi18n "github.com/easydev-org/easybus-sdk/i18n"
)

// Message keys (typed; a typo is a compile error at the call site).
const (
	MsgCloned     = "cloned"
	MsgOnBranch   = "onBranch"
	MsgClean      = "clean"
	MsgCheckedOut = "checkedOut"
	MsgStaged     = "staged"
	MsgCommitted  = "committed"
	MsgPulled     = "pulled"
	MsgPushed     = "pushed"
	// MsgRemoteNoRefs: a repository with no branches or tags (an empty init).
	MsgRemoteNoRefs = "remoteNoRefs"
)

// Catalog is the message table (open locale set: add a column per language).
var Catalog = easybusi18n.Catalog{
	MsgCloned: {
		"en": "Cloned {remote} ({branch} @ {hash})",
		"zh": "已克隆 {remote}（{branch} @ {hash}）",
	},
	MsgRemoteNoRefs: {
		"en": "(no branches or tags)",
		"zh": "（没有分支或标签）",
	},
	MsgOnBranch: {
		"en": "On branch {branch}",
		"zh": "当前分支 {branch}",
	},
	MsgClean: {
		"en": "working tree clean",
		"zh": "工作区干净",
	},
	MsgCheckedOut: {
		"en": "Checked out {branch}",
		"zh": "已切换到 {branch}",
	},
	MsgStaged: {
		"en": "Staged {n} path(s)",
		"zh": "已暂存 {n} 个路径",
	},
	MsgCommitted: {
		"en": "Committed {hash}",
		"zh": "已提交 {hash}",
	},
	MsgPulled: {
		"en": "Pulled {branch} (now {hash})",
		"zh": "已拉取 {branch}（现在 {hash}）",
	},
	MsgPushed: {
		"en": "Pushed {branch}",
		"zh": "已推送 {branch}",
	},
}

// Params are the `{placeholder}` interpolation values.
type Params = easybusi18n.Params

var tr = easybusi18n.New(Catalog, "")

// T resolves a message key for a locale (” => the base locale).
func T(locale, key string, params easybusi18n.Params) string {
	return tr.T(locale, key, params)
}
