package main

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Own branding for the login pages: name, logo, accent colour and footer text.
// Stored in settings key "branding"; the admin panel itself keeps the Wicket look.

const maxLogoBytes = 256 << 10

var (
	accentRE   = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)
	logoTypes  = map[string]bool{"image/png": true, "image/jpeg": true, "image/webp": true, "image/svg+xml": true, "image/gif": true}
	dataURIRE  = regexp.MustCompile(`^data:(image/[a-z+]+);base64,(.+)$`)
	svgRejects = regexp.MustCompile(`(?i)<script|javascript:|\son[a-z]+\s*=|<foreignobject`)
)

type Branding struct {
	Name       string `json:"name"`       // shown instead of "wicket"
	Accent     string `json:"accent"`     // #rrggbb for the primary button, "" = default
	Footer     string `json:"footer"`     // replaces "Protected by Wicket"
	HideFooter bool   `json:"hideFooter"` // no footer line at all
	LogoType   string `json:"logoType,omitempty"`
	Logo       []byte `json:"logo,omitempty"`
}

// brandView is what templates see.
type brandView struct {
	Name, Footer, Accent, AccentFG string
	Logo                           string // URL with cache buster, "" = default mark
	HideFooter                     bool
}

func (a *App) branding() Branding {
	var b Branding
	if raw, ok, err := a.store.GetSetting("branding"); err == nil && ok {
		_ = json.Unmarshal([]byte(raw), &b)
	}
	return b
}

func (a *App) saveBranding(b Branding) error {
	raw, err := json.Marshal(b)
	if err != nil {
		return err
	}
	return a.store.SetSetting("branding", string(raw))
}

func (b Branding) logoTag() string {
	if len(b.Logo) == 0 {
		return ""
	}
	sum := sha256.Sum256(b.Logo)
	return hex.EncodeToString(sum[:6])
}

func (b Branding) view() brandView {
	v := brandView{Name: b.Name, Footer: b.Footer, HideFooter: b.HideFooter}
	if v.Name == "" {
		v.Name = "wicket"
	}
	if accentRE.MatchString(b.Accent) {
		v.Accent = strings.ToLower(b.Accent)
		v.AccentFG = contrastText(v.Accent)
	}
	if tag := b.logoTag(); tag != "" {
		v.Logo = "/branding/logo?v=" + tag
	}
	return v
}

// AccentCSS is injected into the page head; the value is validated, so it is safe CSS.
func (v brandView) AccentCSS() template.CSS {
	if v.Accent == "" {
		return ""
	}
	return template.CSS(fmt.Sprintf(":root .btn-primary.btn-primary{background:%s;color:%s}:root .check input:checked+.box{background:%s;border-color:%s}", v.Accent, v.AccentFG, v.Accent, v.Accent))
}

// contrastText picks black or white text for a background colour (WCAG relative luminance),
// leaning towards white.
func contrastText(hexColor string) string {
	lin := func(s string) float64 {
		n, _ := strconv.ParseUint(s, 16, 8)
		c := float64(n) / 255
		if c <= 0.03928 {
			return c / 12.92
		}
		return math.Pow((c+0.055)/1.055, 2.4)
	}
	l := 0.2126*lin(hexColor[1:3]) + 0.7152*lin(hexColor[3:5]) + 0.0722*lin(hexColor[5:7])
	// 0.179 is the exact tie point; mid-tone brand colours (e.g. #0070f3) read better with white
	if l > 0.3 {
		return "#000"
	}
	return "#fff"
}

// parseLogo accepts a data: URI and returns type and bytes.
func parseLogo(uri string) (string, []byte, error) {
	m := dataURIRE.FindStringSubmatch(uri)
	if m == nil || !logoTypes[m[1]] {
		return "", nil, userErr("err.brandLogo")
	}
	data, err := base64.StdEncoding.DecodeString(m[2])
	if err != nil || len(data) == 0 {
		return "", nil, userErr("err.brandLogo")
	}
	if len(data) > maxLogoBytes {
		return "", nil, userErr("err.brandLogoSize", maxLogoBytes>>10)
	}
	if m[1] == "image/svg+xml" && svgRejects.Match(data) {
		return "", nil, userErr("err.brandLogoSVG")
	}
	return m[1], data, nil
}

func (a *App) handleBrandLogo(w http.ResponseWriter, r *http.Request) {
	b := a.branding()
	if len(b.Logo) == 0 {
		http.NotFound(w, r)
		return
	}
	h := w.Header()
	h.Set("Content-Type", b.LogoType)
	h.Set("Cache-Control", "public, max-age=86400")
	h.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; sandbox")
	w.Write(b.Logo)
}

// ---------------------------------------------------------------- admin API

type brandingOut struct {
	Name       string `json:"name"`
	Accent     string `json:"accent"`
	Footer     string `json:"footer"`
	HideFooter bool   `json:"hideFooter"`
	Logo       string `json:"logo"` // URL or ""
}

type brandingIn struct {
	Name       string  `json:"name"`
	Accent     string  `json:"accent"`
	Footer     string  `json:"footer"`
	HideFooter bool    `json:"hideFooter"`
	Logo       *string `json:"logo"` // nil = keep, "" = remove, data: URI = replace
}

func (a *App) apiBrandingGet(w http.ResponseWriter, r *http.Request) {
	b := a.branding()
	v := b.view()
	writeJSON(w, http.StatusOK, brandingOut{Name: b.Name, Accent: b.Accent, Footer: b.Footer, HideFooter: b.HideFooter, Logo: v.Logo})
}

func (a *App) apiBrandingPut(w http.ResponseWriter, r *http.Request) {
	var in brandingIn
	if err := readJSON(r, &in); err != nil {
		a.fail(w, r, err)
		return
	}
	b := a.branding()
	b.Name = strings.TrimSpace(in.Name)
	b.Footer = strings.TrimSpace(in.Footer)
	b.Accent = strings.TrimSpace(in.Accent)
	b.HideFooter = in.HideFooter
	if utf8.RuneCountInString(b.Name) > 40 || utf8.RuneCountInString(b.Footer) > 120 {
		a.errKey(w, r, http.StatusBadRequest, "err.brandText")
		return
	}
	if b.Accent != "" && !accentRE.MatchString(b.Accent) {
		a.errKey(w, r, http.StatusBadRequest, "err.brandAccent")
		return
	}
	if in.Logo != nil {
		if *in.Logo == "" {
			b.Logo, b.LogoType = nil, ""
		} else {
			typ, data, err := parseLogo(*in.Logo)
			if err != nil {
				a.fail(w, r, err)
				return
			}
			b.Logo, b.LogoType = data, typ
		}
	}
	if err := a.saveBranding(b); err != nil {
		a.fail(w, r, err)
		return
	}
	a.event(r, "settings_changed", reqUser(r).Username, "", "branding")
	a.apiBrandingGet(w, r)
}
