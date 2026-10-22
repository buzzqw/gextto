package gextto

// quality_profiles.go stores the named profiles used by the v3 API. A profile
// is a named combination of the same override fields a series or a movie has
// (quality, language, subtitle, exclusions); the profiles page edits the list.
// On the first read the list is seeded from the combinations already found in
// the library (v3Profiles), so the migration from the per-title data is
// automatic. Every profile keeps its id: a request manager stores the id it
// was given, so removing or reordering a profile must not shift the others.

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

const qualityProfilesKey = "quality_profiles"

// QualityProfile is one named combination of selection overrides. Audio is
// the minimum audio preset (empty = any); DisableUpgrades is «Niente upgrade».
type QualityProfile struct {
	ID              int    `json:"id"`
	Name            string `json:"name"`
	Quality         string `json:"quality"`
	Audio           string `json:"audio,omitempty"`
	Language        string `json:"language"`
	Subtitle        string `json:"subtitle"`
	Exclude         string `json:"exclude"`
	DisableUpgrades bool   `json:"disable_upgrades,omitempty"`
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
		profile.Audio = normalizeAudioRequirement(profile.Audio)
		profile.Language = strings.TrimSpace(profile.Language)
		profile.Subtitle = strings.TrimSpace(profile.Subtitle)
		profile.Exclude = strings.TrimSpace(profile.Exclude)
		out = append(out, profile)
	}
	return assignQualityProfileIDs(out, nil)
}

// assignQualityProfileIDs keeps the id of every profile that has one, gives a
// profile without an id the one its name had in previous (a page row does not
// carry the id), and numbers the rest after the highest id in use. Ids are
// never reused within the list.
func assignQualityProfileIDs(profiles, previous []QualityProfile) []QualityProfile {
	byName := map[string]int{}
	for _, profile := range previous {
		byName[strings.ToLower(profile.Name)] = profile.ID
	}
	used := map[int]bool{}
	highest := 0
	for _, profile := range previous {
		if profile.ID > highest {
			highest = profile.ID
		}
	}
	for i := range profiles {
		if profiles[i].ID <= 0 || used[profiles[i].ID] {
			profiles[i].ID = byName[strings.ToLower(profiles[i].Name)]
		}
		if profiles[i].ID > 0 && !used[profiles[i].ID] {
			used[profiles[i].ID] = true
			if profiles[i].ID > highest {
				highest = profiles[i].ID
			}
			continue
		}
		profiles[i].ID = 0
	}
	for i := range profiles {
		if profiles[i].ID == 0 {
			highest++
			profiles[i].ID = highest
			used[highest] = true
		}
	}
	return profiles
}

// qualityProfileByID returns the profile with id, or the first one (the
// default policy) when id is unknown, as Sonarr does for a missing profile.
func qualityProfileByID(profiles []QualityProfile, id int) QualityProfile {
	for _, profile := range profiles {
		if profile.ID == id {
			return profile
		}
	}
	if len(profiles) > 0 {
		return profiles[0]
	}
	return QualityProfile{ID: 1, Name: "Default"}
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
		profiles = append(profiles, QualityProfile{ID: profile.ID, Name: profile.Name, Quality: profile.Quality,
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
			ID: profile.ID, Name: profile.Name, Quality: strings.TrimSpace(profile.Quality),
			Language: strings.TrimSpace(profile.Language), Subtitle: strings.TrimSpace(profile.Subtitle),
			Exclude: strings.TrimSpace(profile.Exclude), Audio: normalizeAudioRequirement(profile.Audio),
			DisableUpgrades: profile.DisableUpgrades,
		})
	}
	cfg := latestConfig(s)
	cleaned = assignQualityProfileIDs(cleaned, qualityProfilesFromConfig(cfg))
	raw, _ := json.Marshal(cleaned)
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
