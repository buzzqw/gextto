package gextto

// uiweb_v2_theme.go is the theme picker: one dialog with the built-in themes,
// the classic editor palettes and the themes the person saved with a name.
// Saved themes live on the server (setting `_ui_themes`), so every device sees
// them; which theme is active stays a per-browser choice (localStorage).

import (
	"encoding/json"
	"math"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/buzzqw/gextto/internal/logging"
)

// v2ThemesSettingKey stores the saved themes as a JSON array. The leading
// underscore keeps it out of the generic settings pages.
const v2ThemesSettingKey = "_ui_themes"

const (
	v2ThemeNameMax  = 40
	v2ThemeSavedMax = 50
)

// v2Theme is one entry of the picker. Mode is the data-theme value: the three
// hand-tuned themes use their own CSS palette, every other theme is "custom"
// and is painted from the four colours (the rest is derived in CSS).
type v2Theme struct {
	ID      string `json:"-"`
	Name    string `json:"name"`
	Mode    string `json:"-"`
	Bg      string `json:"bg"`
	Surface string `json:"surface"`
	Text    string `json:"text"`
	Accent  string `json:"accent"`
	Saved   bool   `json:"-"`
}

// Light reports whether the theme has a light background.
func (t v2Theme) Light() bool { return v2ThemeLuminance(t.Bg) > 0.4 }

// v2BuiltinThemes are the hand-tuned palettes of gextto-ui.css; the colours
// here only draw the preview and seed the editor.
var v2BuiltinThemes = []v2Theme{
	{ID: "dark", Name: "Scuro", Mode: "dark", Bg: "#0b1120", Surface: "#131c2e", Text: "#e9f0fb", Accent: "#4fd6a1"},
	{ID: "light", Name: "Chiaro", Mode: "light", Bg: "#d6e0ea", Surface: "#edf1f7", Text: "#2a3444", Accent: "#08704f"},
	{ID: "cream", Name: "Crema", Mode: "cream", Bg: "#f4ecda", Surface: "#fbf6ea", Text: "#3a3222", Accent: "#1f6b47"},
}

// v2PresetThemes are the classic editor palettes, adapted where needed so
// text, secondary text and accent stay readable (TestV2ThemePresetsContrast).
var v2PresetThemes = []v2Theme{
	{ID: "nord", Name: "Nord", Bg: "#2e3440", Surface: "#3b4252", Text: "#eceff4", Accent: "#88c0d0"},
	{ID: "dracula", Name: "Dracula", Bg: "#282a36", Surface: "#343746", Text: "#f8f8f2", Accent: "#bd93f9"},
	{ID: "solarized-dark", Name: "Solarized scuro", Bg: "#002b36", Surface: "#073642", Text: "#eee8d5", Accent: "#35b5ab"},
	{ID: "gruvbox-dark", Name: "Gruvbox scuro", Bg: "#282828", Surface: "#3c3836", Text: "#ebdbb2", Accent: "#fabd2f"},
	{ID: "monokai", Name: "Monokai", Bg: "#272822", Surface: "#33342c", Text: "#f8f8f2", Accent: "#a6e22e"},
	{ID: "one-dark", Name: "One Dark", Bg: "#282c34", Surface: "#31353f", Text: "#dcdfe4", Accent: "#61afef"},
	{ID: "tokyo-night", Name: "Tokyo Night", Bg: "#1a1b26", Surface: "#24283b", Text: "#c0caf5", Accent: "#7aa2f7"},
	{ID: "catppuccin-mocha", Name: "Catppuccin Mocha", Bg: "#1e1e2e", Surface: "#313244", Text: "#cdd6f4", Accent: "#cba6f7"},
	{ID: "github-dark", Name: "GitHub scuro", Bg: "#0d1117", Surface: "#161b22", Text: "#e6edf3", Accent: "#58a6ff"},
	{ID: "rose-pine", Name: "Rosé Pine", Bg: "#191724", Surface: "#1f1d2e", Text: "#e0def4", Accent: "#ebbcba"},
	{ID: "everforest", Name: "Everforest", Bg: "#2d353b", Surface: "#343f44", Text: "#ebe4d0", Accent: "#a7c080"},
	{ID: "oled", Name: "Nero OLED", Bg: "#000000", Surface: "#121212", Text: "#f2f2f2", Accent: "#4fd6a1"},
	{ID: "high-contrast", Name: "Alto contrasto", Bg: "#000000", Surface: "#0a0a0a", Text: "#ffffff", Accent: "#ffd400"},
	{ID: "solarized-light", Name: "Solarized chiaro", Bg: "#fdf6e3", Surface: "#eee8d5", Text: "#073642", Accent: "#1a6a9e"},
	{ID: "github-light", Name: "GitHub chiaro", Bg: "#ffffff", Surface: "#f6f8fa", Text: "#1f2328", Accent: "#0969da"},
	{ID: "gruvbox-light", Name: "Gruvbox chiaro", Bg: "#fbf1c7", Surface: "#f2e5bc", Text: "#32302f", Accent: "#af3a03"},
	{ID: "catppuccin-latte", Name: "Catppuccin Latte", Bg: "#eff1f5", Surface: "#e6e9ef", Text: "#303246", Accent: "#7a2fe0"},
	{ID: "one-light", Name: "One Light", Bg: "#fafafa", Surface: "#f0f0f1", Text: "#383a42", Accent: "#2c5fd6"},
	{ID: "rose-pine-dawn", Name: "Rosé Pine Dawn", Bg: "#faf4ed", Surface: "#fffaf3", Text: "#464261", Accent: "#6e5a8c"},
	{ID: "nord-light", Name: "Nord chiaro", Bg: "#eceff4", Surface: "#e5e9f0", Text: "#2e3440", Accent: "#4a6890"},
}

var v2HexColor = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

type v2ThemeView struct {
	Builtin []v2Theme
	Presets []v2Theme
	Saved   []v2Theme
	// Selected is the id the client selects and applies after a save.
	Selected string
	Error    string
}

// V2ThemeModal renders the theme picker.
func V2ThemeModal(w http.ResponseWriter, r *http.Request, s *AppState) {
	v2RenderThemeModal(w, s, "", "")
}

// V2ThemeSave stores (or replaces, by name) a theme with four colours.
func V2ThemeSave(w http.ResponseWriter, r *http.Request, s *AppState) {
	theme := v2Theme{
		Name:    strings.Join(strings.Fields(r.FormValue("name")), " "),
		Bg:      strings.ToLower(r.FormValue("bg")),
		Surface: strings.ToLower(r.FormValue("surface")),
		Text:    strings.ToLower(r.FormValue("text")),
		Accent:  strings.ToLower(r.FormValue("accent")),
	}
	if theme.Name == "" || utf8.RuneCountInString(theme.Name) > v2ThemeNameMax {
		v2RenderThemeModal(w, s, "", "Dai al tema un nome di al massimo 40 caratteri.")
		return
	}
	for _, c := range []string{theme.Bg, theme.Surface, theme.Text, theme.Accent} {
		if !v2HexColor.MatchString(c) {
			v2RenderThemeModal(w, s, "", "Colore non valido.")
			return
		}
	}
	saved := v2SavedThemes(s)
	replaced := false
	for i := range saved {
		if strings.EqualFold(saved[i].Name, theme.Name) {
			saved[i] = theme
			replaced = true
		}
	}
	if !replaced {
		if len(saved) >= v2ThemeSavedMax {
			v2RenderThemeModal(w, s, "", "Hai raggiunto il numero massimo di temi salvati: eliminane uno.")
			return
		}
		saved = append(saved, theme)
	}
	if err := v2StoreThemes(s, saved); err != nil {
		logging.Error("theme save failed", "error", err)
		v2RenderThemeModal(w, s, "", "Salvataggio del tema non riuscito.")
		return
	}
	v2RenderThemeModal(w, s, v2SavedThemeID(theme.Name), "")
}

// V2ThemeDelete removes a saved theme by name.
func V2ThemeDelete(w http.ResponseWriter, r *http.Request, s *AppState) {
	name := r.FormValue("name")
	saved := v2SavedThemes(s)
	kept := saved[:0]
	for _, theme := range saved {
		if !strings.EqualFold(theme.Name, name) {
			kept = append(kept, theme)
		}
	}
	if err := v2StoreThemes(s, kept); err != nil {
		logging.Error("theme delete failed", "error", err)
		v2RenderThemeModal(w, s, "", "Eliminazione del tema non riuscita.")
		return
	}
	v2RenderThemeModal(w, s, "", "")
}

func v2RenderThemeModal(w http.ResponseWriter, s *AppState, selected, message string) {
	view := v2ThemeView{Builtin: v2BuiltinThemes, Presets: v2PresetThemes, Saved: v2SavedThemes(s), Selected: selected, Error: message}
	dict, eng := v2Dictionaries(s)
	v2Render(w, http.StatusOK, "v2_theme_modal", view, dict, eng)
}

func v2SavedThemeID(name string) string { return "saved:" + name }

// v2SavedThemes reads the saved themes, dropping malformed entries, sorted by
// name.
func v2SavedThemes(s *AppState) []v2Theme {
	raw := strings.TrimSpace(latestConfig(s).Settings[v2ThemesSettingKey])
	if raw == "" {
		return nil
	}
	var stored []v2Theme
	if err := json.Unmarshal([]byte(raw), &stored); err != nil {
		logging.Debug("saved themes unreadable", "error", err)
		return nil
	}
	out := make([]v2Theme, 0, len(stored))
	for _, theme := range stored {
		if theme.Name == "" || !v2HexColor.MatchString(theme.Bg) || !v2HexColor.MatchString(theme.Surface) ||
			!v2HexColor.MatchString(theme.Text) || !v2HexColor.MatchString(theme.Accent) {
			continue
		}
		theme.ID = v2SavedThemeID(theme.Name)
		theme.Mode = "custom"
		theme.Saved = true
		out = append(out, theme)
	}
	sort.SliceStable(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out
}

func v2StoreThemes(s *AppState, themes []v2Theme) error {
	if themes == nil {
		themes = []v2Theme{}
	}
	payload, err := json.Marshal(themes)
	if err != nil {
		return err
	}
	return SaveSetting(s.cfg.DataDir, v2ThemesSettingKey, string(payload))
}

func init() {
	for i := range v2PresetThemes {
		v2PresetThemes[i].Mode = "custom"
	}
}

// v2ThemeLuminance is the WCAG relative luminance of a #rrggbb colour (the
// same formula as accentOnColor in v2-core.js).
func v2ThemeLuminance(hex string) float64 {
	h := strings.TrimPrefix(hex, "#")
	if len(h) != 6 {
		return 0
	}
	channel := func(part string) float64 {
		value, _ := strconv.ParseUint(part, 16, 8)
		v := float64(value) / 255
		if v <= 0.03928 {
			return v / 12.92
		}
		return math.Pow((v+0.055)/1.055, 2.4)
	}
	return 0.2126*channel(h[0:2]) + 0.7152*channel(h[2:4]) + 0.0722*channel(h[4:6])
}
