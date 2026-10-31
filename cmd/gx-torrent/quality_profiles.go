package main

// quality_profiles.go holds the standalone quality profiles: a named list of
// accepted resolutions with an optional cutoff. A wanted title that chooses a
// profile uses it instead of the plain quality field; the cutoff turns the
// profile into an upgrade rule (a better release is taken until the cutoff).

import (
	"encoding/json"
	"html"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/buzzqw/gextto/internal/rss"
)

const qualityProfilesKey = "quality-profiles"

// qualityProfile is one named profile. Qualities are the resolutions accepted,
// in the order shown; Cutoff is the resolution at which upgrades stop (empty =
// no upgrades).
type qualityProfile struct {
	Name      string   `json:"name"`
	Qualities []string `json:"qualities"`
	Cutoff    string   `json:"cutoff,omitempty"`
}

// qualityLabels are the resolutions a profile can contain, lowest first.
var qualityLabels = []string{"360p", "480p", "576p", "720p", "1080p", "2160p"}

// defaultQualityProfiles are offered until the operator saves their own list.
func defaultQualityProfiles() []qualityProfile {
	return []qualityProfile{
		{Name: "HD", Qualities: []string{"720p", "1080p"}, Cutoff: "1080p"},
		{Name: "Best", Qualities: []string{"720p", "1080p", "2160p"}, Cutoff: "2160p"},
	}
}

// qualityProfiles returns the configured profiles, normalized; an empty saved
// list (the operator removed every profile) stays empty.
func (d *Daemon) qualityProfiles() []qualityProfile {
	if d.opts.Settings == nil {
		return defaultQualityProfiles()
	}
	raw := strings.TrimSpace(d.opts.Settings.Get(qualityProfilesKey, ""))
	if raw == "" {
		return defaultQualityProfiles()
	}
	var list []qualityProfile
	if err := json.Unmarshal([]byte(raw), &list); err != nil {
		logfError("cannot parse the quality profiles: %v", err)
		return defaultQualityProfiles()
	}
	return normalizeQualityProfiles(list)
}

func normalizeQualityProfiles(list []qualityProfile) []qualityProfile {
	out := make([]qualityProfile, 0, len(list))
	seen := map[string]bool{}
	for _, profile := range list {
		normalized := normalizeQualityProfile(profile)
		if normalized.Name == "" || len(normalized.Qualities) == 0 {
			continue
		}
		key := strings.ToLower(normalized.Name)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, normalized)
	}
	return out
}

func normalizeQualityProfile(profile qualityProfile) qualityProfile {
	profile.Name = strings.TrimSpace(profile.Name)
	var qualities []string
	seen := map[int]bool{}
	for _, quality := range profile.Qualities {
		quality = strings.ToLower(strings.TrimSpace(quality))
		rank := rss.QualityRankOf(quality)
		if rank <= 0 || seen[rank] {
			continue
		}
		seen[rank] = true
		qualities = append(qualities, qualityLabel(rank))
	}
	sort.Slice(qualities, func(i, j int) bool { return rss.QualityRankOf(qualities[i]) < rss.QualityRankOf(qualities[j]) })
	profile.Qualities = qualities
	profile.Cutoff = qualityLabel(rss.QualityRankOf(profile.Cutoff))
	if profile.Cutoff != "" && !seen[rss.QualityRankOf(profile.Cutoff)] {
		profile.Cutoff = ""
	}
	return profile
}

func qualityLabel(rank int) string {
	switch rank {
	case 1:
		return "360p"
	case 2:
		return "480p"
	case 3:
		return "576p"
	case 4:
		return "720p"
	case 5:
		return "1080p"
	case 6:
		return "2160p"
	}
	return ""
}

func (p qualityProfile) allows(title string) bool {
	if len(p.Qualities) == 0 {
		return true
	}
	_, rank := rss.QualityRank(title)
	if rank == 0 {
		return false
	}
	for _, quality := range p.Qualities {
		if rss.QualityRankOf(quality) == rank {
			return true
		}
	}
	return false
}

func (p qualityProfile) cutoffRank() int {
	return rss.QualityRankOf(p.Cutoff)
}

func (p qualityProfile) maxRank() int {
	max := 0
	for _, quality := range p.Qualities {
		if rank := rss.QualityRankOf(quality); rank > max {
			max = rank
		}
	}
	return max
}

// qualityProfileByName finds a profile by name (case-insensitive).
func (d *Daemon) qualityProfileByName(name string) (qualityProfile, bool) {
	name = strings.TrimSpace(name)
	if name == "" {
		return qualityProfile{}, false
	}
	for _, profile := range d.qualityProfiles() {
		if strings.EqualFold(profile.Name, name) {
			return profile, true
		}
	}
	return qualityProfile{}, false
}

// saveQualityProfiles validates and stores the list.
func (d *Daemon) saveQualityProfiles(list []qualityProfile) error {
	if d.opts.Settings == nil {
		return nil
	}
	list = normalizeQualityProfiles(list)
	raw, err := json.Marshal(list)
	if err != nil {
		return err
	}
	d.opts.Settings.Set(qualityProfilesKey, string(raw))
	return d.opts.Settings.Save()
}

// handleUIQualityProfile adds, edits or removes one profile from settings.
func (d *Daemon) handleUIQualityProfile(w http.ResponseWriter, r *http.Request) {
	if d.opts.Mode != ModeStandalone {
		http.NotFound(w, r)
		return
	}
	if !d.uiAuthorized(w, r) || !d.uiSameOrigin(w, r) {
		return
	}
	_ = r.ParseForm()
	op := strings.TrimSpace(r.FormValue("op"))
	name := strings.TrimSpace(r.FormValue("name"))
	list := d.qualityProfiles()
	var err error
	switch op {
	case "add", "edit":
		if name == "" {
			err = errQualityProfileName
			break
		}
		profile := normalizeQualityProfile(qualityProfile{
			Name:      name,
			Qualities: splitQualityList(r.FormValue("qualities")),
			Cutoff:    r.FormValue("cutoff"),
		})
		if len(profile.Qualities) == 0 {
			err = errQualityProfileQualities
			break
		}
		found := false
		for i := range list {
			if strings.EqualFold(list[i].Name, name) {
				list[i] = profile
				found = true
				break
			}
		}
		if op == "add" && !found {
			list = append(list, profile)
		}
		if op == "edit" && !found {
			err = errQualityProfileNotFound
		}
	case "remove":
		found := false
		filtered := list[:0]
		for _, profile := range list {
			if strings.EqualFold(profile.Name, name) {
				found = true
				continue
			}
			filtered = append(filtered, profile)
		}
		list = filtered
		if !found {
			err = errQualityProfileNotFound
		}
	default:
		err = errQualityProfileAction
	}
	if err == nil {
		err = d.saveQualityProfiles(list)
	}
	query := url.Values{}
	if err != nil {
		query.Set("ok", "0")
		query.Set("msg", "Error: "+err.Error())
	} else {
		query.Set("ok", "1")
		query.Set("msg", "Quality profiles saved")
	}
	http.Redirect(w, r, uiSettingsPath+"?"+query.Encode()+"#quality-profiles", http.StatusSeeOther)
}

func splitQualityList(raw string) []string {
	return strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ';' || r == ' ' || r == '\n' || r == '\t'
	})
}

var (
	errQualityProfileName      = &qualityProfileError{"the profile name is required"}
	errQualityProfileQualities = &qualityProfileError{"a profile needs at least one known quality (360p, 480p, 576p, 720p, 1080p, 2160p)"}
	errQualityProfileNotFound  = &qualityProfileError{"the quality profile does not exist"}
	errQualityProfileAction    = &qualityProfileError{"unknown quality profile action"}
)

type qualityProfileError struct{ message string }

func (e *qualityProfileError) Error() string { return e.message }

// qualityProfileRows renders the settings table of quality profiles.
func (d *Daemon) qualityProfileRows() string {
	profiles := d.qualityProfiles()
	if len(profiles) == 0 {
		return `<tr><td colspan="4" class="muted">No quality profiles. Add one below to choose allowed qualities and an optional cutoff.</td></tr>`
	}
	var b strings.Builder
	for _, profile := range profiles {
		name := html.EscapeString(profile.Name)
		b.WriteString(`<tr><td><strong>` + name + `</strong></td><td>` +
			`<form method="post" action="/ui/quality-profile" class="cat-edit">` +
			`<input type="hidden" name="op" value="edit"><input type="hidden" name="name" value="` + name + `">` +
			`<input type="text" name="qualities" value="` + html.EscapeString(strings.Join(profile.Qualities, ", ")) + `" autocomplete="off" aria-label="Qualities of ` + name + `" title="Accepted resolutions, separated by commas (360p, 480p, 576p, 720p, 1080p, 2160p)">` +
			`<select name="cutoff" aria-label="Cutoff of ` + name + `" title="Stop upgrading when this quality is downloaded (empty = no upgrades)">` + qualityCutoffOptions(profile.Cutoff) + `</select>` +
			`<button type="submit" class="sm" title="Save this quality profile">Save</button>` +
			`</form></td><td>` + cutoffLabel(profile) + `</td><td>` +
			`<form method="post" action="/ui/quality-profile" data-confirm="Remove this quality profile? Titles that use it fall back to their quality field.">` +
			`<input type="hidden" name="op" value="remove"><input type="hidden" name="name" value="` + name + `">` +
			`<button type="submit" class="sm danger" title="Remove this quality profile">Remove</button>` +
			`</form></td></tr>`)
	}
	return b.String()
}

func qualityCutoffOptions(selected string) string {
	selected = strings.ToLower(strings.TrimSpace(selected))
	var b strings.Builder
	b.WriteString(`<option value=""` + selectedIf(selected == "") + `>No upgrade</option>`)
	for _, quality := range qualityLabels {
		b.WriteString(`<option value="` + quality + `"` + selectedIf(selected == quality) + `>` + quality + `</option>`)
	}
	return b.String()
}

func selectedIf(ok bool) string {
	if ok {
		return " selected"
	}
	return ""
}

func cutoffLabel(profile qualityProfile) string {
	if profile.Cutoff == "" {
		return "no upgrades"
	}
	return "up to " + profile.Cutoff
}

// wantedProfileOptions is the select in the add/edit dialog.
func (d *Daemon) wantedProfileOptions(selected string) string {
	selected = strings.TrimSpace(selected)
	var b strings.Builder
	b.WriteString(`<option value="">No profile (use quality)</option>`)
	for _, profile := range d.qualityProfiles() {
		mark := ""
		if strings.EqualFold(profile.Name, selected) {
			mark = " selected"
		}
		b.WriteString(`<option value="` + html.EscapeString(profile.Name) + `"` + mark + `>` + html.EscapeString(profile.Name) + ` (up to ` + html.EscapeString(strings.Join(profile.Qualities, "/")) + `)</option>`)
	}
	return b.String()
}
