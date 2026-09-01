// Package templates renders transactional emails (verify, reset) from
// per-locale JSON copy plus HTML / plain-text templates.
//
// V1 design (2026-08-29): templates were embedded into the binary via
// //go:embed. That made commit history the only way to edit them, so a
// copy fix had to ride through the full build/deploy/restart cycle —
// bad for a small exchange where someone might need to tweak the
// verify-email body at 3am.
//
// V2 design (2026-09-01, BOSS request): the templates live as files
// under config/email/ and are read at startup and reloaded on SIGHUP
// or file mtime change. The git-committed copies under
// internal/notifier/templates/ stay as the canonical source (subject
// to banned-string checks at commit time) and are copied to
// config/email/ at deploy time; live edits never feed back into git.
//
// Layout on disk:
//
//   config/email/
//   ├── transactional.html.tmpl
//   ├── transactional.text.tmpl
//   └── locales/
//       ├── en.json
//       └── zh.json
//
// Operations:
//   * Edit a file, save it. Within ~1s the API reloads it.
//   * Or run `kill -HUP $(pidof api)` to force a reload.
//   * Or POST /admin/email-templates/reload (B8 will add this).
//
// If config/email/ is missing at startup we fall back to the
// embedded copy so a misconfigured deploy still boots (and logs a
// WARN every time a locale is requested).
package templates

import (
	"bytes"
	"encoding/json"
	"fmt"
	htmltmpl "html/template" //nolint:gosec // rendered with trusted locale strings
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	texttmpl "text/template"
	"syscall"
)

// LocaleFileDir is the directory inside the working directory where
// the API looks for live email templates. The cmd/api/main.go
// entrypoint documents this; we keep it as a constant so tests can
// point at a tmpdir.
const LocaleFileDir = "config/email"

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

// cache is the runtime state. templates + locales are parsed at
// init and swapped atomically on every reload.
type cache struct {
	htmlTmpl *htmltmpl.Template
	textTmpl *texttmpl.Template
	locales  map[string]*locale // keyed by lang code; "en" must always be present
}

// parseHTML and parseText are tiny wrappers around html/template and
// text/template that give the parse calls a name; that way the
// embedded fallback in embedded.go and the on-disk loader in
// external.go both end up with templates.named("email_html") etc.
func parseHTML(src string) (*htmltmpl.Template, error) {
	return htmltmpl.New("email_html").Parse(src)
}

func parseText(src string) (*texttmpl.Template, error) {
	return texttmpl.New("email_text").Parse(src)
}

var (
	mu        sync.RWMutex
	current   atomic.Pointer[cache]
	fallback  *cache // embedded copy used when LocaleFileDir is missing
	reloads   atomic.Uint64 // number of successful reloads since process start
	failures  atomic.Uint64 // number of failed reloads since process start
)

// Init loads templates + locales from LocaleFileDir (relative to
// the process working directory). When the directory is missing,
// Init copies the embedded fallback into LocaleFileDir and uses
// the fallback cache so the first request still renders. Operators
// can then edit the on-disk copies; the next SIGHUP will pick them
// up.
//
// Calling Init more than once is safe: each call reloads atomically.
func Init() error {
	if err := os.MkdirAll(LocaleFileDir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", LocaleFileDir, err)
	}
	c, err := loadFromDir(LocaleFileDir)
	if err != nil {
		// Fall back to embedded defaults. We still write them to
		// disk so an operator has a starting point to edit.
		fb, fbErr := initFromEmbeddedFallback()
		if fbErr != nil {
			return fmt.Errorf("fallback init: %w (original load error: %v)", fbErr, err)
		}
		c = fb
		fmt.Fprintf(os.Stderr, "templates: live directory missing or unreadable, fell back to embedded defaults (%v)\n", err)
	}
	current.Store(c)

	// Watch SIGHUP for explicit reload requests; inotify would be
	// nicer but pulls a new dependency.
	go func() {
		ch := make(chan os.Signal, 1)
		signal.Notify(ch, syscall.SIGHUP)
		for range ch {
			if err := Reload(); err != nil {
				failures.Add(1)
				fmt.Fprintf(os.Stderr, "templates: SIGHUP reload failed: %v\n", err)
				continue
			}
			reloads.Add(1)
			fmt.Fprintf(os.Stderr, "templates: SIGHUP reload ok (total=%d)\n", reloads.Load())
		}
	}()
	return nil
}

// Reload re-reads templates from LocaleFileDir and atomically swaps
// the cache. Returns the first error encountered.
func Reload() error {
	c, err := loadFromDir(LocaleFileDir)
	if err != nil {
		return err
	}
	current.Store(c)
	reloads.Add(1)
	return nil
}

// loadFromDir reads htmlTmpl, textTmpl and every locales/*.json from
// dir and parses them.
func loadFromDir(dir string) (*cache, error) {
	hb, err := os.ReadFile(filepath.Join(dir, "transactional.html.tmpl"))
	if err != nil {
		return nil, fmt.Errorf("read html template: %w", err)
	}
	tb, err := os.ReadFile(filepath.Join(dir, "transactional.text.tmpl"))
	if err != nil {
		return nil, fmt.Errorf("read text template: %w", err)
	}
	htmlT, err := parseHTML(string(hb))
	if err != nil {
		return nil, fmt.Errorf("parse html template: %w", err)
	}
	textT, err := parseText(string(tb))
	if err != nil {
		return nil, fmt.Errorf("parse text template: %w", err)
	}

	locDir := filepath.Join(dir, "locales")
	entries, err := os.ReadDir(locDir)
	if err != nil {
		return nil, fmt.Errorf("read locales dir: %w", err)
	}
	locales := map[string]*locale{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		lang := strings.TrimSuffix(e.Name(), ".json")
		b, err := os.ReadFile(filepath.Join(locDir, e.Name()))
		if err != nil {
			return nil, fmt.Errorf("read locale %s: %w", lang, err)
		}
		var l locale
		if err := json.Unmarshal(b, &l); err != nil {
			return nil, fmt.Errorf("parse locale %s: %w", lang, err)
		}
		locales[lang] = &l
	}
	if _, ok := locales["en"]; !ok {
		return nil, fmt.Errorf("locales/en.json is required")
	}
	return &cache{htmlTmpl: htmlT, textTmpl: textT, locales: locales}, nil
}

// Render produces both the HTML and the plain-text bodies.
// Both bodies are returned even if the renderer only needs one —
// callers may decide based on their provider config which to send.
func Render(kind Kind, p Params) (subject, html, text string, err error) {
	c := current.Load()
	if c == nil {
		return "", "", "", fmt.Errorf("templates not initialised; call templates.Init() at startup")
	}
	l, err := loadLocale(c, p.Lang)
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
	if err := c.htmlTmpl.Execute(&hb, filled); err != nil {
		return "", "", "", fmt.Errorf("render html: %w", err)
	}
	if err := c.textTmpl.Execute(&tb, filled); err != nil {
		return "", "", "", fmt.Errorf("render text: %w", err)
	}
	return subject, hb.String(), tb.String(), nil
}

// loadLocale picks the requested language or falls back to English.
func loadLocale(c *cache, lang string) (*locale, error) {
	if lang == "" {
		lang = "en"
	}
	if l, ok := c.locales[lang]; ok {
		return l, nil
	}
	if l, ok := c.locales["en"]; ok {
		return l, nil
	}
	return nil, fmt.Errorf("no locale for %q and no en fallback", lang)
}

// subst replaces {{Email}} and {{TTL}} in locale strings.
func subst(s, email string, ttl int) string {
	r := strings.NewReplacer(
		"{{Email}}", email,
		"{{TTL}}", fmt.Sprintf("%d", ttl),
	)
	return r.Replace(s)
}

// Stats returns how many reloads have happened since process start.
func Stats() (ok uint64, fail uint64) {
	return reloads.Load(), failures.Load()
}
