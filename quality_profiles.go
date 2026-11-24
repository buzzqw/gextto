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
	"regexp"
	"sort"
	"strings"

	"github.com/buzzqw/gextto/internal/logging"
	"github.com/buzzqw/gextto/internal/models"
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
// the minimum audio preset (empty = any). Qualities are the accepted
// resolutions, lowest first: they are both the filter on incoming releases
// and the ceiling of the upgrade. Upgrade has two values: "improve" (stay
// inside the accepted qualities, with REPACK/PROPER and better source, codec,
// HDR or audio still allowed) and "freeze" (never replace an archived file).
//
// Cutoff, DisableUpgrades and Upgrades are the pre-simplification fields. They
// are still decoded so an existing configuration migrates on read, and still
// written so older API clients keep working, but they are always derived from
// Qualities and Upgrade: the editor no longer offers them.
type QualityProfile struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	Quality  string `json:"quality"`
	Audio    string `json:"audio,omitempty"`
	Language string `json:"language"`
	Subtitle string `json:"subtitle"`
	Exclude  string `json:"exclude"`

	Qualities []string `json:"qualities,omitempty"`
	Upgrade   string   `json:"upgrade,omitempty"`

	Cutoff          string `json:"cutoff,omitempty"`
	DisableUpgrades bool   `json:"disable_upgrades,omitempty"`
	Upgrades        string `json:"upgrades,omitempty"`
}

const (
	profileUpgradeImprove = "improve"
	profileUpgradeFreeze  = "freeze"
)

// profileQualityLabels are the resolutions a profile can accept, lowest first.
// The index (plus one) is the stable id used by the v3 API.
var profileQualityLabels = []string{"360p", "480p", "576p", "720p", "1080p", "2160p"}

// UnmarshalJSON accepts both a JSON array and the comma-separated string the
// settings editor posts for "qualities". Every other field keeps its default
// decoding.
func (profile *QualityProfile) UnmarshalJSON(data []byte) error {
	type plain QualityProfile
	var raw struct {
		plain
		Qualities json.RawMessage `json:"qualities"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*profile = QualityProfile(raw.plain)
	if len(raw.Qualities) == 0 || string(raw.Qualities) == "null" {
		return nil
	}
	switch raw.Qualities[0] {
	case '[':
		return json.Unmarshal(raw.Qualities, &profile.Qualities)
	case '"':
		var text string
		if err := json.Unmarshal(raw.Qualities, &text); err != nil {
			return err
		}
		profile.Qualities = splitProfileQualityList(text)
		return nil
	default:
		return fmt.Errorf("qualities must be a list or a comma-separated string")
	}
}

func profileQualityLabel(rank int) string {
	if rank <= 0 || rank > len(profileQualityLabels) {
		return ""
	}
	return profileQualityLabels[rank-1]
}

func splitProfileQualityList(raw string) []string {
	return strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ';' || r == ' ' || r == '\t' || r == '\n'
	})
}

func profileQualityRank(value string) int {
	value = strings.ToLower(strings.TrimSpace(value))
	if strings.HasSuffix(value, "p") {
		value = strings.TrimSuffix(value, "p")
	}
	for index, label := range profileQualityLabels {
		if strings.TrimSuffix(label, "p") == value {
			return index + 1
		}
	}
	return 0
}

// qualityListFromLegacy expands the old single quality expression using the
// same filter the release check uses, so "1080p+" becomes 1080p and 2160p and
// "720p-1080p" becomes the two resolutions in between.
func qualityListFromLegacy(quality string) []string {
	quality = strings.ToLower(strings.TrimSpace(quality))
	if quality == "" || quality == "any" {
		return nil
	}
	out := []string{}
	for _, label := range profileQualityLabels {
		if resolutionAllowed(&models.Quality{Resolution: label}, quality) {
			out = append(out, label)
		}
	}
	return out
}

func normalizeQualityList(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		rank := profileQualityRank(value)
		if rank == 0 || seen[profileQualityLabels[rank-1]] {
			continue
		}
		seen[profileQualityLabels[rank-1]] = true
		out = append(out, profileQualityLabels[rank-1])
	}
	sort.Slice(out, func(i, j int) bool {
		return profileQualityRank(out[i]) < profileQualityRank(out[j])
	})
	return out
}

// legacyQualityFromList keeps the single-value field meaningful for API
// clients that still read it: a contiguous set folds into a minimum, a range
// or a single resolution; a non-contiguous one is described by its minimum
// (the explicit qualities list stays authoritative for the release filter).
func legacyQualityFromList(list []string) string {
	if len(list) == 0 {
		return ""
	}
	if len(list) == 1 {
		return list[0]
	}
	first, last := profileQualityRank(list[0]), profileQualityRank(list[len(list)-1])
	contiguous := last-first+1 == len(list)
	if !contiguous {
		return list[0] + "+"
	}
	if last == len(profileQualityLabels) {
		return list[0] + "+"
	}
	if len(list) == 2 {
		return list[0] + "-" + list[1]
	}
	return list[0] + "-" + list[len(list)-1]
}

// normalizeProfileQualities fills the explicit list from the legacy field when
// absent and keeps the cutoff inside the accepted set. It never rewrites the
// legacy expression: a read must not change what is stored (an expression such
// as "<1080p" stays as the operator wrote it). The save handler derives the
// expression from a list the editor actually submitted.
func normalizeProfileQualities(profile *QualityProfile) {
	explicit := len(profile.Qualities) > 0
	profile.Qualities = normalizeQualityList(profile.Qualities)
	if !explicit {
		profile.Qualities = qualityListFromLegacy(profile.Quality)
	}
	// Cutoff is left untouched here: normalizeProfileUpgrade still has to read
	// it to migrate profiles saved before the single "Upgrade" choice existed.
}

// resolutionInList decides whether a parsed resolution satisfies a title's
// accepted qualities. An empty list keeps the legacy single-value expression.
func resolutionInList(quality *models.Quality, list []string, legacy string) bool {
	if len(list) > 0 {
		rank := profileQualityRank(quality.Resolution)
		for _, allowed := range list {
			if rank != 0 && rank == profileQualityRank(allowed) {
				return true
			}
		}
		return false
	}
	return resolutionAllowed(quality, legacy)
}

// migrateTitleCutoff folds a cutoff saved on a title before the single
// "Upgrade" choice existed into the accepted qualities: the resolutions above
// the old ceiling are dropped. Called when the library is loaded, so no rule
// is left behind the scenes.
func migrateTitleCutoff(qualities []string, legacyQuality, cutoff string) []string {
	rank := profileQualityRank(cutoff)
	if rank == 0 {
		return qualities
	}
	if len(qualities) == 0 {
		qualities = qualityListFromLegacy(legacyQuality)
	}
	narrowed := make([]string, 0, len(qualities))
	for _, quality := range qualities {
		if profileQualityRank(quality) <= rank {
			narrowed = append(narrowed, quality)
		}
	}
	if len(narrowed) == 0 && len(qualities) > 0 {
		narrowed = []string{qualities[0]}
	}
	return narrowed
}

// normalizeProfileUpgrade folds the old "allow upgrades" switch and the old
// cutoff into the single choice the editor now offers:
//
//   - freeze: nothing already archived is ever replaced, REPACK/PROPER included;
//   - improve: upgrades stay inside the accepted qualities, whose highest
//     resolution is the ceiling. A cutoff saved before the change is migrated
//     by dropping the accepted resolutions above it, so the old ceiling keeps
//     its meaning without a second, invisible rule.
//
// The legacy fields are then written back for API compatibility.
func normalizeProfileUpgrade(profile *QualityProfile) {
	mode := strings.ToLower(strings.TrimSpace(profile.Upgrade))
	// The old editor wrote both spellings; "upgrades" wins when present, as it
	// did before the simplification, so a saved choice is not silently changed.
	switch strings.ToLower(strings.TrimSpace(profile.Upgrades)) {
	case "deny":
		mode = profileUpgradeFreeze
	case "allow":
		mode = profileUpgradeImprove
	default:
		if profile.DisableUpgrades {
			mode = profileUpgradeFreeze
		} else if mode != profileUpgradeFreeze {
			mode = profileUpgradeImprove
		}
	}
	cutoff := strings.ToLower(strings.TrimSpace(profile.Cutoff))
	if mode == profileUpgradeImprove && profileQualityRank(cutoff) > 0 {
		ceiling := profileQualityRank(cutoff)
		narrowed := make([]string, 0, len(profile.Qualities))
		for _, quality := range profile.Qualities {
			if profileQualityRank(quality) <= ceiling {
				narrowed = append(narrowed, quality)
			}
		}
		if len(narrowed) == 0 && len(profile.Qualities) > 0 {
			// The cutoff was below everything accepted: keep the lowest
			// resolution so the list stays a usable filter.
			narrowed = []string{profile.Qualities[0]}
		}
		profile.Qualities = narrowed
		profile.Quality = legacyQualityFromList(narrowed)
	}
	profile.Cutoff = ""
	profile.Upgrade = mode
	profile.DisableUpgrades = mode == profileUpgradeFreeze
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
		normalizeProfileQualities(&profile)
		normalizeProfileUpgrade(&profile)
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
			Qualities: qualityListFromLegacy(profile.Quality),
			Upgrade:   ternaryString(profile.DisableUpgrades, profileUpgradeFreeze, profileUpgradeImprove),
			Audio:     profile.Audio, Language: profile.Language, Subtitle: profile.Subtitle,
			Exclude: profile.Exclude}
		// Both spellings travel to the editor, which writes «upgrades».
		normalizeProfileUpgrade(&seeded)
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
	input.Qualities = append([]string(nil), profile.Qualities...)
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
	Qualities                                   []string
	Upgrade                                     string
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
		// A title saved before the explicit list existed has only the legacy
		// expression: derive the same list the profile's migration derives, so
		// it still matches the profile it was created from.
		if len(values.Qualities) == 0 {
			values.Qualities = qualityListFromLegacy(values.Quality)
		}
		values.Qualities = normalizeQualityList(values.Qualities)
		values.Language = normalizeLanguageList(values.Language)
		values.Subtitle = normalizeLanguageList(values.Subtitle)
		values.Audio = normalizeAudioRequirement(values.Audio)
		return values
	}
	want := norm(title)
	for _, profile := range profiles {
		candidate := norm(profileTitleValues{Quality: profile.Quality, Audio: profile.Audio,
			Language: profile.Language, Subtitle: profile.Subtitle,
			Qualities: profile.Qualities, Upgrade: profile.Upgrade,
			DisableUpgrades: profile.DisableUpgrades})
		if candidate.Quality == want.Quality && candidate.Audio == want.Audio &&
			candidate.Language == want.Language && candidate.Subtitle == want.Subtitle &&
			candidate.DisableUpgrades == want.DisableUpgrades &&
			strings.Join(normalizeQualityList(candidate.Qualities), ",") == strings.Join(normalizeQualityList(want.Qualities), ",") &&
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

var (
	// profileQualityRe accepts every quality expression the release filter
	// understands: a resolution, a minimum ("1080p+"), a range
	// ("720p-1080p") and the legacy maximum-only form ("<1080p"). A typo such
	// as "1080pp" used to be stored as "no limit", silently widening the
	// profile: it is refused on save now.
	profileQualityRe = regexp.MustCompile(`^<?(360p|480p|576p|720p|1080p|2160p)(\+|-((360p|480p|576p|720p|1080p|2160p)))?$`)
	// A language code is two or three letters after canonicalisation ("it"
	// becomes "ita", "english" becomes "eng"); "any" is the UI sentinel.
	profileLanguageCodeRe = regexp.MustCompile(`^[a-z]{2,3}$`)
)

// validateQualityProfileFields checks the values the release filters read.
// Names, the list size and duplicate names are checked by the caller; this is
// about a value that would otherwise be read as "no requirement" and let in
// everything. Empty values stay legal and mean "any".
func validateQualityProfileFields(profile QualityProfile) error {
	if value := strings.TrimSpace(profile.Quality); value != "" && !strings.EqualFold(value, "any") {
		if !profileQualityRe.MatchString(strings.ToLower(value)) {
			return fmt.Errorf("invalid quality %q", profile.Quality)
		}
	}
	if value := strings.TrimSpace(profile.Audio); value != "" && normalizeAudioRequirement(value) == "" {
		return fmt.Errorf("invalid minimum audio %q", profile.Audio)
	}
	for _, field := range []struct{ label, value string }{
		{"language", profile.Language}, {"subtitle", profile.Subtitle},
	} {
		for _, entry := range strings.Split(field.value, ",") {
			code := strings.ToLower(strings.TrimSpace(entry))
			if code == "" || code == "any" || code == "*" {
				continue
			}
			if !profileLanguageCodeRe.MatchString(normalizeLanguageCode(code)) {
				return fmt.Errorf("invalid %s %q", field.label, strings.TrimSpace(entry))
			}
		}
	}
	return nil
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
		for _, quality := range profile.Qualities {
			if profileQualityRank(quality) == 0 {
				jsonError(w, http.StatusBadRequest, fmt.Sprintf("invalid quality profile %q: invalid quality %q", profile.Name, quality))
				return
			}
		}
		if mode := strings.ToLower(strings.TrimSpace(profile.Upgrade)); mode != "" && mode != profileUpgradeImprove && mode != profileUpgradeFreeze {
			jsonError(w, http.StatusBadRequest, fmt.Sprintf("invalid quality profile %q: invalid upgrade %q", profile.Name, profile.Upgrade))
			return
		}
		explicitList := len(profile.Qualities) > 0
		normalizeProfileQualities(&profile)
		if explicitList && len(profile.Qualities) > 0 {
			profile.Quality = legacyQualityFromList(profile.Qualities)
		}
		normalizeProfileUpgrade(&profile)
		if err := validateQualityProfileFields(profile); err != nil {
			jsonError(w, http.StatusBadRequest, fmt.Sprintf("invalid quality profile %q: %v", profile.Name, err))
			return
		}
		cleaned = append(cleaned, QualityProfile{
			ID: profile.ID, Name: profile.Name, Quality: strings.TrimSpace(profile.Quality),
			Language: qualityProfileLanguage(cfg, profile.Language), Subtitle: strings.TrimSpace(profile.Subtitle),
			Exclude: mergeExclusions(profile.Exclude), Audio: normalizeAudioRequirement(profile.Audio),
			Qualities: profile.Qualities, Upgrade: profile.Upgrade,
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
