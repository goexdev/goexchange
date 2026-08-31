// Package templates renders transactional emails (verify, reset) from
// in-repo HTML / plain-text templates plus per-locale JSON strings.
//
// Why in-repo templates (BOSS rule 2026-08-29):
//   * Git review / blame / revert for email copy.
//   * Banned-string and chinese-text hooks cover them just like code.
//   * No external dashboard drift to chase; one source of truth lives
//     in the source tree alongside the Go code that sends them.
//
// The locale JSON files use {{Token}} placeholders so they are pure
// data (json package, no Go-template execution at locale load time).
// The HTML and text templates use the same {{.Token}} Go-template
// syntax — these are unrelated parsers (text/template vs encoding/json)
// so the overlap is intentional and harmless.
package templates

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	htmltmpl "html/template" //nolint:gosec // rendered with trusted locale strings
	texttmpl "text/template"
	"strings"
)

//go:embed transactional.html.tmpl transactional.text.tmpl locales/*.json
var fs embed.FS

// htmlTmpl parses the HTML body once at package init. We escape all
// user-controlled strings via html/template; locale strings are
// trusted (committed by us) but using html/template gives free
// defense-in-depth against copy-paste drift.
var htmlTmpl = htmltmpl.Must(htmltmpl.ParseFS(fs, "transactional.html.tmpl"))

// textTmpl parses the plain-text fallback once at package init.
var textTmpl = texttmpl.Must(texttmpl.ParseFS(fs, "transactional.text.tmpl"))

// locale holds one language's copy.
type locale struct {
	VerifyEmail copyBlock `json:"verify_email"`
	ResetPwd    copyBlock `json:"reset_password"`
}

// copyBlock is the structured copy for a single email.
type copyBlock struct {
	Subject  string `json:"subject"`
	Title    string `json:"title"`
	Greeting string `json:"greeting"`
	Intro    string `json:"intro"`
	Button   string `json:"button"`
	Fallback string `json:"fallback"`
	Expiry   string `json:"expiry"`
	Ignore   string `json:"ignore"`
	Footer   string `json:"footer"`
}

func loadLocale(lang string) (*locale, error) {
	if lang == "" {
		lang = "en"
	}
	// Try the requested language, fall back to English.
	paths := []string{
		"locales/" + lang + ".json",
		"locales/en.json",
	}
	var lastErr error
	for _, p := range paths {
		b, err := fs.ReadFile(p)
		if err != nil {
			lastErr = err
			continue
		}
		var l locale
		if err := json.Unmarshal(b, &l); err != nil {
			return nil, fmt.Errorf("parse %s: %w", p, err)
		}
		return &l, nil
	}
	return nil, fmt.Errorf("no locale found for %q (last error: %v)", lang, lastErr)
}

// Kind selects which copy block to render.
type Kind string

const (
	KindVerifyEmail Kind = "verify_email"
	KindResetPwd    Kind = "reset_password"
)

// Params are the dynamic values plugged into the copy.
type Params struct {
	Lang      string // "en" or "zh"; empty defaults to en
	Email     string // recipient email
	ActionURL string // full link with token
	TTL       int    // minutes until expiry, for human display
}

// Render produces both the HTML and the plain-text bodies.
// Both bodies are returned even if the renderer only needs one —
// callers may decide based on their provider config which to send.
func Render(kind Kind, p Params) (subject, html, text string, err error) {
	l, err := loadLocale(p.Lang)
	if err != nil {
		return "", "", "", err
	}
	var blk copyBlock
	switch kind {
	case KindVerifyEmail:
		blk = l.VerifyEmail
	case KindResetPwd:
		blk = l.ResetPwd
	default:
		return "", "", "", fmt.Errorf("unknown template kind %q", kind)
	}
	subject = blk.Subject

	// Locale strings carry {{Email}} / {{TTL}} placeholders. We
	// substitute them with the actual values before passing to the
	// HTML/text templates, so the templates only see {{.ActionURL}}
	// and the per-block fields (Title, Intro, etc.).
	filled := struct {
		Title     string
		Greeting  string
		Intro     string
		Button    string
		Fallback  string
		ActionURL string
		Expiry    string
		Ignore    string
		Footer    string
	}{
		Title:     blk.Title,
		Greeting:  subst(blk.Greeting, p.Email, p.TTL),
		Intro:     blk.Intro,
		Button:    blk.Button,
		Fallback:  blk.Fallback,
		ActionURL: p.ActionURL,
		Expiry:    subst(blk.Expiry, p.Email, p.TTL),
		Ignore:    blk.Ignore,
		Footer:    blk.Footer,
	}

	var hb, tb bytes.Buffer
	if err := htmlTmpl.Execute(&hb, filled); err != nil {
		return "", "", "", fmt.Errorf("render html: %w", err)
	}
	if err := textTmpl.Execute(&tb, filled); err != nil {
		return "", "", "", fmt.Errorf("render text: %w", err)
	}
	return subject, hb.String(), tb.String(), nil
}

// subst replaces {{Email}} and {{TTL}} in locale strings.
func subst(s, email string, ttl int) string {
	r := strings.NewReplacer(
		"{{Email}}", email,
		"{{TTL}}", fmt.Sprintf("%d", ttl),
	)
	return r.Replace(s)
}
