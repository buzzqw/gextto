package gextto

import (
	"net/http"
	"net/url"
	"strings"
)

// uiweb_v2_settings_mode.go is the Facilitata/Esperto switch of the
// configuration page. It is one filter over the same index, remembered in a
// cookie (like the List/Posters choice in the library): the user who trusts the
// system sees a few limits, the user who wants the internals sees every value.
// Nothing moves between the two.

const (
	// v2SettingsModeCookie remembers the chosen level between pages.
	v2SettingsModeCookie = "gx_settings_mode"
	// v2SettingsModeCookieAge is how long the choice lasts (a year).
	v2SettingsModeCookieAge = 365 * 24 * 3600
	// v2SettingsModeBrief is the default: the handful of limits that matter.
	v2SettingsModeBrief = "brief"
	// v2SettingsModeExpert shows every value, internals included.
	v2SettingsModeExpert = "expert"
)

// v2SettingsMode answers which level to render: the parameter wins, then the
// remembered cookie, then Esperto.
//
// The default is Esperto so nothing changes for an existing installation (no
// row disappears) and the tests that read the full page keep working. The
// guided level is what a new user gets: the setup wizard sets the cookie when
// it finishes, so the first visit to the configuration is Facilitata.
func v2SettingsMode(r *http.Request) string {
	switch strings.ToLower(strings.TrimSpace(r.URL.Query().Get("mode"))) {
	case v2SettingsModeExpert:
		return v2SettingsModeExpert
	case v2SettingsModeBrief:
		return v2SettingsModeBrief
	}
	if cookie, err := r.Cookie(v2SettingsModeCookie); err == nil {
		switch strings.ToLower(strings.TrimSpace(cookie.Value)) {
		case v2SettingsModeBrief:
			return v2SettingsModeBrief
		case v2SettingsModeExpert:
			return v2SettingsModeExpert
		}
	}
	return v2SettingsModeExpert
}

// v2SetSettingsModeBriefAfterSetup stores the guided level for a brand-new
// installation, so the first configuration visit is the simple one. Called by
// the setup wizard when it finishes (or is skipped).
func v2SetSettingsModeBriefAfterSetup(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name: v2SettingsModeCookie, Value: v2SettingsModeBrief, Path: "/",
		MaxAge: v2SettingsModeCookieAge, SameSite: http.SameSiteLaxMode,
		HttpOnly: true, Secure: requestIsHTTPS(r),
	})
}

// v2RememberSettingsMode stores the level chosen with the parameter, so the
// pages reached later (whose links do not carry it) keep it.
func v2RememberSettingsMode(w http.ResponseWriter, r *http.Request) {
	value := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("mode")))
	if value != v2SettingsModeExpert && value != v2SettingsModeBrief {
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: v2SettingsModeCookie, Value: value, Path: "/",
		MaxAge: v2SettingsModeCookieAge, SameSite: http.SameSiteLaxMode,
		HttpOnly: true, Secure: requestIsHTTPS(r),
	})
}

// v2SettingsModeURL is the link that switches level, keeping the current
// section so the page does not jump elsewhere.
func v2SettingsModeURL(tab, mode string) string {
	values := url.Values{"view": {"settings"}, "mode": {mode}}
	if strings.TrimSpace(tab) != "" {
		values.Set("tab", tab)
	}
	return "/?" + values.Encode()
}

// v2SettingsIsBrief reports whether internals must be hidden.
func v2SettingsIsBrief(mode string) bool { return mode != v2SettingsModeExpert }
