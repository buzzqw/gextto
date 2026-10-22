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
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"

	"github.com/buzzqw/gextto/internal/logging"
)

const (
	qualityProfilesKey = "quality_profiles"
	// maxQualityProfiles and maxQualityProfileName bound what a script may
	// store: every profile is rendered on each title page and served to the
	// request manager, so a runaway list would slow both down.
	maxQualityProfiles    = 200
	maxQualityProfileName = 120
	// duplicateProfileNameError prefixes the 400 of a repeated name. The
	// settings editor recognises it to show a translated message with the name.
	duplicateProfileNameError = "duplicate profile name: "
)

// QualityProfile is one named combination of selection overrides. Audio is
// the minimum audio preset (empty = any); DisableUpgrades is «Permetti
// upgrade: No». Upgrades is the same switch as the profile editor writes it
// ("allow"/"deny"): normalizeProfileUpgrades keeps the two in step.
type QualityProfile struct {
	ID              int    `json:"id"`
	Name            string `json:"name"`
	Quality         string `json:"quality"`
	Audio           string `json:"audio,omitempty"`
	Language        string `json:"language"`
	Subtitle        string `json:"subtitle"`
	Exclude         string `json:"exclude"`
	DisableUpgrades bool   `json:"disable_upgrades,omitempty"`
	Upgrades        string `json:"upgrades,omitempty"`
}

// normalizeProfileUpgrades reads the editor's "upgrades" choice when present
// and then writes it back from DisableUpgrades, so both spellings agree.
func normalizeProfileUpgrades(profile *QualityProfile) {
	switch strings.ToLower(strings.TrimSpace(profile.Upgrades)) {
	case "deny":
		profile.DisableUpgrades = true
	case "allow":
		profile.DisableUpgrades = false
	}
	profile.Upgrades = "allow"
	if profile.DisableUpgrades {
		profile.Upgrades = "deny"
	}
}

func decodeQualityProfiles(raw string) []QualityProfile {
	var profiles []QualityProfile
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	if err := json.Unmarshal([]byte(raw), &profiles); err != nil {
		// A corrupted value is rebuilt from the library on the next read; say
		// so once, so the operator knows the old list was no longer readable.
		logging.Warn("quality profiles: stored list is not valid JSON; rebuilding it from the library", "error", err.Error())
	}
	out := make([]QualityProfile, 0, len(profiles))
	for _, profile := range profiles {
		profile.Name = strings.TrimSpace(profile.Name)
		if profile.Name == "" {
			continue
		}
		profile.Quality = strings.TrimSpace(profile.Quality)
		profile.Audio = normalizeAudioRequirement(profile.Audio)
		normalizeProfileUpgrades(&profile)
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
	profiles := decodeQualityProfiles(cfg.Settings[qualityProfilesKey])
	// An empty language means the configured default language, exactly like an
	// empty language field in the add form. Filling it at read time keeps the
	// title pages, the add form and the v3 API on the same rule.
	for index := range profiles {
		profiles[index].Language = qualityProfileLanguage(cfg, profiles[index].Language)
	}
	return profiles
}

// qualityProfileLanguage is the language a profile gives a title: an empty
// value means the configured default language.
func qualityProfileLanguage(cfg *Config, language string) string {
	if trimmed := strings.TrimSpace(language); trimmed != "" {
		return trimmed
	}
	if cfg != nil {
		return cfg.DefaultLanguage()
	}
	return "ita"
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
		seeded := QualityProfile{ID: profile.ID, Name: profile.Name, Quality: profile.Quality,
			Audio: profile.Audio, Language: profile.Language, Subtitle: profile.Subtitle,
			Exclude: profile.Exclude, DisableUpgrades: profile.DisableUpgrades}
		// Both spellings travel to the editor, which writes «upgrades».
		normalizeProfileUpgrades(&seeded)
		profiles = append(profiles, seeded)
	}
	if raw, err := json.Marshal(profiles); err == nil && cfg != nil {
		if err := SaveSetting(cfg.DataDir, qualityProfilesKey, string(raw)); err != nil {
			// The list is still returned and stays usable: the seed will be
			// attempted again on the next read.
			logging.Warn("quality profiles: could not save the generated list", "error", err.Error())
		}
	}
	return profiles
}

// applyQualityProfileToAdd gives a title being added the values of profile:
// the profile decides quality, audio, language, subtitles and upgrades, and
// the title's own exclusions are added to the profile's ones.
func applyQualityProfileToAdd(input *TmdbAddInput, profile QualityProfile) {
	input.Quality = profile.Quality
	input.Audio = profile.Audio
	input.AudioChannels = ""
	input.Language = profile.Language
	input.Subtitle = profile.Subtitle
	input.DisableUpgrades = profile.DisableUpgrades
	input.Exclude = mergeExclusions(profile.Exclude, input.Exclude)
}

// profileTitleValues are the values a profile gives a title.
type profileTitleValues struct {
	Quality, Audio, Language, Subtitle, Exclude string
	DisableUpgrades                             bool
}

// qualityProfileExact returns the profile with id, if it exists (no fallback:
// a title page applies only the profile it was given).
func qualityProfileExact(profiles []QualityProfile, id int) (QualityProfile, bool) {
	for _, profile := range profiles {
		if profile.ID == id {
			return profile, true
		}
	}
	return QualityProfile{}, false
}

// normalizeQualityValue folds a quality requirement into the form the release
// check compares: lower case, and the "any" sentinel as no limit. A title
// stored as "1080P" therefore matches a profile's "1080p", exactly as
// resolutionAllowed reads both (it lower-cases the requirement too).
func normalizeQualityValue(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "any" {
		return ""
	}
	return value
}

// normalizeLanguageList folds a comma-separated language list into a canonical
// set: every code goes through normalizeLanguageCode (it = ita, en = eng),
// empty and repeated entries are dropped and the list is sorted. "eng,ita"
// and "ITA,eng" are the same requirement.
func normalizeLanguageList(value string) string {
	seen := map[string]bool{}
	parts := []string{}
	for _, item := range strings.Split(value, ",") {
		code := normalizeLanguageCode(item)
		if code == "" || seen[code] {
			continue
		}
		seen[code] = true
		parts = append(parts, code)
	}
	sort.Strings(parts)
	return strings.Join(parts, ",")
}

// matchingQualityProfile returns the id of the first profile whose values are
// the title's, or 0. Exclusions count: applying a profile adds its exclusions
// to the title's, so a title whose exclusions differ would change on the next
// save even if the operator edited an unrelated field.
func matchingQualityProfile(profiles []QualityProfile, title profileTitleValues, defaultLanguage string) int {
	norm := func(values profileTitleValues) profileTitleValues {
		if strings.TrimSpace(values.Language) == "" {
			values.Language = defaultLanguage
		}
		if strings.TrimSpace(values.Language) == "" {
			values.Language = "ita"
		}
		values.Quality = normalizeQualityValue(values.Quality)
		values.Language = normalizeLanguageList(values.Language)
		values.Subtitle = normalizeLanguageList(values.Subtitle)
		values.Audio = normalizeAudioRequirement(values.Audio)
		return values
	}
	want := norm(title)
	for _, profile := range profiles {
		candidate := norm(profileTitleValues{Quality: profile.Quality, Audio: profile.Audio,
			Language: profile.Language, Subtitle: profile.Subtitle,
			DisableUpgrades: profile.DisableUpgrades})
		if candidate.Quality == want.Quality && candidate.Audio == want.Audio &&
			candidate.Language == want.Language && candidate.Subtitle == want.Subtitle &&
			candidate.DisableUpgrades == want.DisableUpgrades &&
			sameExclusions(profile.Exclude, title.Exclude) {
			return profile.ID
		}
	}
	return 0
}

// sameExclusions compares two exclusion lists as sets: the order and the
// duplicates mergeExclusions removes do not make a different policy.
func sameExclusions(a, b string) bool {
	words := func(list string) []string {
		seen := map[string]bool{}
		out := []string{}
		for _, word := range strings.Split(list, ",") {
			word = strings.ToLower(strings.TrimSpace(word))
			if word == "" || seen[word] {
				continue
			}
			seen[word] = true
			out = append(out, word)
		}
		sort.Strings(out)
		return out
	}
	left, right := words(a), words(b)
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

// mergeExclusions joins comma-separated exclusion lists, dropping empty and
// repeated words (case-insensitive) and keeping the first spelling.
func mergeExclusions(lists ...string) string {
	seen := map[string]bool{}
	out := []string{}
	for _, list := range lists {
		for _, word := range strings.Split(list, ",") {
			word = strings.TrimSpace(word)
			if word == "" || seen[strings.ToLower(word)] {
				continue
			}
			seen[strings.ToLower(word)] = true
			out = append(out, word)
		}
	}
	return strings.Join(out, ", ")
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
	cfg := latestConfig(s)
	if len(payload.Items) > maxQualityProfiles {
		jsonError(w, http.StatusBadRequest, fmt.Sprintf("too many quality profiles (max %d)", maxQualityProfiles))
		return
	}
	cleaned := make([]QualityProfile, 0, len(payload.Items))
	seen := map[string]bool{}
	for _, profile := range payload.Items {
		profile.Name = strings.TrimSpace(profile.Name)
		// A row left without a name is an empty row of the editor, not a
		// profile: it is dropped. A repeated name is a mistake the operator has
		// to see, not a row to lose silently.
		if profile.Name == "" {
			continue
		}
		if len([]rune(profile.Name)) > maxQualityProfileName {
			jsonError(w, http.StatusBadRequest,
				fmt.Sprintf("profile name too long (max %d characters): %s", maxQualityProfileName, profile.Name))
			return
		}
		key := strings.ToLower(profile.Name)
		if seen[key] {
			jsonError(w, http.StatusBadRequest, duplicateProfileNameError+profile.Name)
			return
		}
		seen[key] = true
		normalizeProfileUpgrades(&profile)
		cleaned = append(cleaned, QualityProfile{
			ID: profile.ID, Name: profile.Name, Quality: strings.TrimSpace(profile.Quality),
			Language: qualityProfileLanguage(cfg, profile.Language), Subtitle: strings.TrimSpace(profile.Subtitle),
			Exclude: mergeExclusions(profile.Exclude), Audio: normalizeAudioRequirement(profile.Audio),
			DisableUpgrades: profile.DisableUpgrades, Upgrades: profile.Upgrades,
		})
	}
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
