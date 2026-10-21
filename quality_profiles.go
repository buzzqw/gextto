package gextto

// quality_profiles.go stores the named profiles used by the v3 API. A profile
// is a named combination of the same override fields a series or a movie has
// (quality, language, subtitle, exclusions); the profiles page edits the list,
// and v3Profiles adds any combination found in the library that is not named
// yet, so the migration from the per-title data is automatic.

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

const qualityProfilesKey = "quality_profiles"

// QualityProfile is one named combination of selection overrides.
type QualityProfile struct {
	Name     string `json:"name"`
	Quality  string `json:"quality"`
	Language string `json:"language"`
	Subtitle string `json:"subtitle"`
	Exclude  string `json:"exclude"`
}

func decodeQualityProfiles(raw string) []QualityProfile {
	var profiles []QualityProfile
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	_ = json.Unmarshal([]byte(raw), &profiles)
	out := make([]QualityProfile, 0, len(profiles))
	for _, profile := range profiles {
		profile.Name = strings.TrimSpace(profile.Name)
		if profile.Name == "" {
			continue
		}
		profile.Quality = strings.TrimSpace(profile.Quality)
		profile.Language = strings.TrimSpace(profile.Language)
		profile.Subtitle = strings.TrimSpace(profile.Subtitle)
		profile.Exclude = strings.TrimSpace(profile.Exclude)
		out = append(out, profile)
	}
	return out
}

func qualityProfilesFromConfig(cfg *Config) []QualityProfile {
	if cfg == nil {
		return nil
	}
	return decodeQualityProfiles(cfg.Settings[qualityProfilesKey])
}

// ensureQualityProfiles materialises the profiles the library already implies
// on the first read, so their ids stay stable for a request manager.
func ensureQualityProfiles(s *AppState) []QualityProfile {
	cfg := latestConfig(s)
	profiles := qualityProfilesFromConfig(cfg)
	if len(profiles) > 0 {
		return profiles
	}
	for _, profile := range v3Profiles(cfg) {
		profiles = append(profiles, QualityProfile{Name: profile.Name, Quality: profile.Quality,
			Language: profile.Language, Subtitle: profile.Subtitle, Exclude: profile.Exclude})
	}
	if raw, err := json.Marshal(profiles); err == nil && cfg != nil {
		_ = SaveSetting(cfg.DataDir, qualityProfilesKey, string(raw))
	}
	return profiles
}

// QualityProfilesView is GET /api/quality-profiles.
func QualityProfilesView(w http.ResponseWriter, r *http.Request, s *AppState) {
	profiles := ensureQualityProfiles(s)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"items": profiles})
}

// SaveQualityProfiles is POST /api/quality-profiles with {"items":[…]}. It also
// accepts a bare array, so a script can send the list directly.
func SaveQualityProfiles(w http.ResponseWriter, r *http.Request, s *AppState) {
	var payload struct {
		Items []QualityProfile `json:"items"`
	}
	rawBody, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		jsonError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if err := json.Unmarshal(rawBody, &payload); err != nil {
		var direct []QualityProfile
		if err := json.Unmarshal(rawBody, &direct); err != nil {
			jsonError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		payload.Items = direct
	}
	cleaned := make([]QualityProfile, 0, len(payload.Items))
	seen := map[string]bool{}
	for _, profile := range payload.Items {
		profile.Name = strings.TrimSpace(profile.Name)
		if profile.Name == "" || seen[strings.ToLower(profile.Name)] {
			continue
		}
		seen[strings.ToLower(profile.Name)] = true
		cleaned = append(cleaned, QualityProfile{
			Name: profile.Name, Quality: strings.TrimSpace(profile.Quality),
			Language: strings.TrimSpace(profile.Language), Subtitle: strings.TrimSpace(profile.Subtitle),
			Exclude: strings.TrimSpace(profile.Exclude),
		})
	}
	raw, _ := json.Marshal(cleaned)
	cfg := latestConfig(s)
	if err := validateSettingValue(qualityProfilesKey, string(raw), cfg); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := SaveSetting(cfg.DataDir, qualityProfilesKey, string(raw)); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "items": cleaned})
}
