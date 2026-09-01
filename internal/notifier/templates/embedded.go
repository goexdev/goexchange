// Package templates: embedded fallback.
//
// V1 of this package embedded the templates via //go:embed and read
// them at package init. V2 (2026-09-01, BOSS request) externalised
// the templates to config/email/* so copy edits don't require a
// rebuild. This file keeps an embedded copy as a last-resort
// fallback so a deploy that forgets to seed config/email still
// boots and serves email (with a stderr warning every request).
//
// The embedded copy is intentionally not the primary source. Edits
// here are picked up only when initFromEmbeddedFallback() is called,
// which happens only when LocaleFileDir is missing or unreadable.

package templates

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// embeddedHTML is the canonical HTML body committed in source.
// Edit here ONLY if you want to ship a new default; for runtime
// edits copy the file from config/email/transactional.html.tmpl.
const embeddedHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>{{.Title}}</title>
</head>
<body>
<h1>{{.Title}}</h1>
<p>{{.Greeting}}</p>
<p>{{.Intro}}</p>
<p><a href="{{.ActionURL}}">{{.Button}}</a></p>
<p>{{.Fallback}} <code>{{.ActionURL}}</code></p>
<p>{{.Expiry}}</p>
<hr>
<p><small>{{.Ignore}}</small></p>
<p><small>{{.Footer}}</small></p>
</body>
</html>`

// embeddedText is the plain-text fallback body.
const embeddedText = `{{.Title}}

{{.Greeting}}

{{.Intro}}

{{.Button}}: {{.ActionURL}}

{{.Fallback}}: {{.ActionURL}}

{{.Expiry}}

{{.Ignore}}

{{.Footer}}
`

// embeddedLocales is the canonical copy committed in source. Edit
// here ONLY if you want to ship a new default; for runtime edits
// edit config/email/locales/<lang>.json.
var embeddedLocales = map[string]string{
	"en": `{
  "verify_email": {
    "subject": "Verify your goexchange email",
    "title": "Welcome to goexchange",
    "greeting": "Hi {{Email}}",
    "intro": "Click the button below to verify your email address and activate your account.",
    "button": "Verify email",
    "fallback": "Or copy this link into your browser:",
    "expiry": "This link expires in {{TTL}} minutes.",
    "ignore": "If you did not create this account, you can safely ignore this email.",
    "footer": "goexchange — small exchange for BNB and SOL"
  },
  "reset_password": {
    "subject": "Reset your goexchange password",
    "title": "Password reset",
    "greeting": "Hi {{Email}}",
    "intro": "Someone (hopefully you) requested a password reset. Click the button below to choose a new one.",
    "button": "Reset password",
    "fallback": "Or copy this link into your browser:",
    "expiry": "This link expires in {{TTL}} minutes.",
    "ignore": "If you did not request a reset, you can safely ignore this email — your password has not changed.",
    "footer": "goexchange — small exchange for BNB and SOL"
  }
}`,
	"zh": `{
  "verify_email": {
    "subject": "验证你的 goexchange 邮箱",
    "title": "欢迎使用 goexchange",
    "greeting": "你好 {{Email}}",
    "intro": "点击下方按钮验证你的邮箱地址并激活账户。",
    "button": "验证邮箱",
    "fallback": "或复制以下链接到浏览器:",
    "expiry": "链接 {{TTL}} 分钟后过期。",
    "ignore": "如果你并未注册此账户,请忽略本邮件。",
    "footer": "goexchange — 小型 BNB / SOL 交易所"
  },
  "reset_password": {
    "subject": "重置你的 goexchange 密码",
    "title": "密码重置",
    "greeting": "你好 {{Email}}",
    "intro": "有人(希望是你本人)请求重置密码。点击下方按钮设置新密码。",
    "button": "重置密码",
    "fallback": "或复制以下链接到浏览器:",
    "expiry": "链接 {{TTL}} 分钟后过期。",
    "ignore": "如果你并未请求重置,请忽略本邮件 — 你的密码未被修改。",
    "footer": "goexchange — 小型 BNB / SOL 交易所"
  }
}`,
}

// writeEmbeddedToDir drops the embedded copies into dir so an
// operator who landed at an empty filesystem still has a starting
// point to edit.
func writeEmbeddedToDir(dir string) error {
	if err := os.MkdirAll(filepath.Join(dir, "locales"), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "transactional.html.tmpl"), []byte(embeddedHTML), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "transactional.text.tmpl"), []byte(embeddedText), 0o644); err != nil {
		return err
	}
	for lang, body := range embeddedLocales {
		if err := os.WriteFile(filepath.Join(dir, "locales", lang+".json"), []byte(body), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// initFromEmbeddedFallback is called by package init() when no
// LocaleFileDir exists at process start. It writes the embedded
// copies to disk and returns the in-memory cache.
func initFromEmbeddedFallback() (*cache, error) {
	if err := writeEmbeddedToDir(LocaleFileDir); err != nil {
		return nil, err
	}
	htmlT, err := parseHTML(embeddedHTML)
	if err != nil {
		return nil, err
	}
	textT, err := parseText(embeddedText)
	if err != nil {
		return nil, err
	}
	locales := map[string]*locale{}
	for lang, body := range embeddedLocales {
		var l locale
		if err := json.Unmarshal([]byte(body), &l); err != nil {
			return nil, fmt.Errorf("parse embedded locale %s: %w", lang, err)
		}
		locales[lang] = &l
	}
	return &cache{htmlTmpl: htmlT, textTmpl: textT, locales: locales}, nil
}