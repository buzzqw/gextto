package gextto

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/buzzqw/gextto/internal/logging"
)

// tuning.go turns the machine facts into a proposal: which settings would suit
// this hardware better, and why. It is a pure function — it never writes — and
// it deliberately stays away from two kinds of setting: the ones the torrent
// engine manages by itself (the tuning guides them, it does not overwrite them)
// and the ones the user set by hand (they are never proposed again).

// TuningProposal is one suggested change, with the reason already parameterised
// (WhyTemplate/WhyArgs) so the interface can translate it.
type TuningProposal struct {
	Key      string
	Current  string
	Proposed string
	// WhyTemplate is the Italian reason with %s/%d placeholders; WhyArgs are its
	// arguments (already human-readable strings where it matters).
	WhyTemplate string
	WhyArgs     []any
}

// ProposeTuning builds the proposal for the given machine facts. `manual` marks
// the settings the user changed by hand: they are never proposed, so
// re-optimizing cannot overwrite a deliberate choice.
func ProposeTuning(facts SystemFacts, cfg *Config, manual map[string]bool) []TuningProposal {
	out := []TuningProposal{}
	add := func(key, proposed, why string, args ...any) {
		if manual[key] || gxAutoManagedKey(key) {
			return
		}
		proposed = strings.TrimSpace(proposed)
		if proposed == "" {
			return
		}
		current := ""
		if cfg != nil {
			current = strings.TrimSpace(cfg.Settings[key])
		}
		if tuningAlreadySet(key, current, proposed) {
			return
		}
		out = append(out, TuningProposal{
			Key: key, Current: current, Proposed: proposed,
			WhyTemplate: why, WhyArgs: args,
		})
	}

	// Free-space guard: at least 20 GB or 5% of the volume, never more than half
	// of what is free, so the guard cannot itself stop every download.
	if gib := proposedMinFreeSpaceGB(facts); gib > 0 {
		add("min_free_space_gb", strconv.FormatInt(gib, 10),
			"Il volume dei download ha %s liberi su %s: tengo %d GB di margine.",
			humanBytesString(facts.Download.Free), humanBytesString(facts.Download.Total), gib)
	}

	if facts.Download.Exists && facts.Archive.Exists {
		if facts.SameDevice {
			add("hardlink_seeding", "yes",
				"Download e libreria sono sullo stesso filesystem: gli hardlink evitano la copia.")
		} else {
			add("hardlink_seeding", "no",
				"Download e libreria sono su filesystem diversi: la copia è inevitabile.")
		}

		network := facts.DownloadNetwork || facts.ArchiveNetwork
		if network {
			add("torrent_preallocate", "no",
				"Una cartella è su filesystem di rete: la preallocazione bloccherebbe l'avvio.")
			add("gxtorrent_request_timeout_secs", "90",
				"Archivio o download su filesystem di rete: alzo il timeout delle richieste a %d secondi.", 90)
		} else {
			add("torrent_preallocate", "yes",
				"Download e libreria sono su disco locale: la preallocazione è sicura.")
			add("gxtorrent_request_timeout_secs", "30",
				"Tutto in locale: il timeout delle richieste a %d secondi basta.", 30)
		}
	}

	// Bandwidth: a limit at 80% of the highest rate the line actually reached,
	// leaving headroom for the ACKs (a saturated upload slows the download). Only
	// once a peak was observed; a fresh install has none, so the limit stays the
	// user's until the line has been measured.
	if peak := speedPeakKib(cfg, autotunePeakDownKey); peak > 0 {
		if limit := peak * 8 / 10; limit > 0 {
			add("torrent_dl_limit", strconv.FormatInt(limit, 10),
				"Picco di download osservato %d KiB/s: limite all'80%% → %d KiB/s.", peak, limit)
		}
	}
	if peak := speedPeakKib(cfg, autotunePeakUpKey); peak > 0 {
		if limit := peak * 8 / 10; limit > 0 {
			add("torrent_ul_limit", strconv.FormatInt(limit, 10),
				"Picco di upload osservato %d KiB/s: limite all'80%% → %d KiB/s.", peak, limit)
		}
	}

	// Connections per torrent: the engine does not manage this by itself, so the
	// tuning picks a bounded value from the RAM (50 to 400).
	connections := proposedConnectionsPerTorrent(facts.MemTotal)
	add("torrent_max_connections_per_torrent", strconv.FormatInt(connections, 10),
		"%s di RAM: %d connessioni per torrent.", humanBytesString(facts.MemTotal), connections)

	// RAM disk: recommend it when one is available, switch it off when none is.
	if facts.RamdiskDir != "" {
		recommendation := RamdiskRecommendation(facts.RamdiskDir)
		threshold := jsonStringField(recommendation, "threshold_gb", "")
		margin := jsonStringField(recommendation, "margin_gb", "")
		add("torrent_ramdisk_enabled", "yes",
			"RAM disk disponibile (%s): lo abilito per ridurre le scritture su disco.", facts.RamdiskDir)
		if threshold != "" {
			add("torrent_ramdisk_threshold_gb", threshold,
				"RAM disk: soglia per torrent pari a %s GB, in base allo spazio disponibile.", threshold)
		}
		if margin != "" {
			add("torrent_ramdisk_margin_gb", margin,
				"RAM disk: mantengo %s GB liberi, in base allo spazio disponibile.", margin)
		}
	} else if cfg != nil && settingTruthy(cfg.Settings["torrent_ramdisk_enabled"]) {
		add("torrent_ramdisk_enabled", "no",
			"Nessun RAM disk disponibile su questa macchina: lo disattivo.")
	}

	return out
}

// tuningAlreadySet reports whether the current value already means the proposed
// one, comparing the way the settings readers do (booleans by meaning, numbers
// numerically, text exactly).
func tuningAlreadySet(key, current, proposed string) bool {
	if current == "" {
		return false
	}
	if proposed == "yes" || proposed == "no" {
		return settingTruthy(current) == (proposed == "yes")
	}
	currentNumber, currentErr := strconv.ParseFloat(current, 64)
	proposedNumber, proposedErr := strconv.ParseFloat(proposed, 64)
	if currentErr == nil && proposedErr == nil {
		return currentNumber == proposedNumber
	}
	return current == proposed
}

// Free-space guard bounds: at least 20 GB, never more than 50 GB. The 5% of the
// volume rule alone is wrong on a multi-terabyte disk (it asked for hundreds of
// GB), so the percentage only raises the floor up to the ceiling.
const (
	minFreeSpaceFloorGB = 20
	minFreeSpaceCeilGB  = 50
)

// proposedMinFreeSpaceGB is the free-space guard the tuning would set: between
// 20 and 50 GB (5% of the volume raises the floor, the ceiling caps it), never
// more than half of the free space.
func proposedMinFreeSpaceGB(facts SystemFacts) int64 {
	const gib = uint64(1024 * 1024 * 1024)
	if !facts.Download.Exists || facts.Download.Total == 0 {
		return 0
	}
	target := uint64(minFreeSpaceFloorGB) * gib
	if fivePercent := facts.Download.Total / 20; fivePercent > target {
		target = fivePercent
	}
	if ceiling := uint64(minFreeSpaceCeilGB) * gib; target > ceiling {
		target = ceiling
	}
	if half := facts.Download.Free / 2; target > half {
		target = half
	}
	if target < gib {
		return 0
	}
	return int64(target / gib)
}

// ApplyTuning writes the confirmed proposals. It applies only the keys the
// caller confirmed, using the proposed value the server computed itself (never a
// value taken from the request), each through validateSettingValue. A key that
// is not in the proposal list is ignored, so the request can never write a
// setting the tuning did not suggest. Every write is recorded as "auto" with
// the reason, so re-optimizing recognises its own values and the user's stay
// theirs.
func ApplyTuning(dataDir string, cfg *Config, proposals []TuningProposal, chosen map[string]bool) (int, error) {
	applied := 0
	for _, proposal := range proposals {
		if !chosen[proposal.Key] {
			continue
		}
		if err := validateSettingValue(proposal.Key, proposal.Proposed, cfg); err != nil {
			return applied, fmt.Errorf("%s: %w", proposal.Key, err)
		}
		if err := saveConfigSetting(dataDir, proposal.Key, proposal.Proposed); err != nil {
			return applied, err
		}
		_ = MarkSettingOrigin(dataDir, proposal.Key, OriginAuto, fmt.Sprintf(proposal.WhyTemplate, proposal.WhyArgs...))
		applied++
	}
	return applied, nil
}

// adaptiveHysteresisFraction is how far a numeric value must move before the
// slow loop touches it again: small drifts are left alone, so the values do not
// wobble.
const adaptiveHysteresisFraction = 0.20

// adaptiveTuningChanges are the proposals the slow loop would apply now: only
// values the system already chose (origin auto) and only when they moved beyond
// the hysteresis. The user's values and the never-tuned ones are left alone, so
// the loop only ever adjusts its own work.
func adaptiveTuningChanges(dataDir string, cfg *Config) []TuningProposal {
	origins, err := SettingOrigins(dataDir)
	if err != nil {
		return nil
	}
	manual := map[string]bool{}
	for key, provenance := range origins {
		if provenance.Origin == OriginManual {
			manual[key] = true
		}
	}
	out := []TuningProposal{}
	for _, proposal := range ProposeTuning(ProbeSystem(cfg), cfg, manual) {
		if origins[proposal.Key].Origin != OriginAuto {
			continue
		}
		if !tuningMovedEnough(proposal.Current, proposal.Proposed) {
			continue
		}
		out = append(out, proposal)
	}
	return out
}

// tuningMovedEnough is the hysteresis: a value never set yet always qualifies;
// numbers must move by at least adaptiveHysteresisFraction (or one unit); other
// values by any change.
func tuningMovedEnough(current, proposed string) bool {
	if strings.TrimSpace(current) == "" {
		return true
	}
	currentNumber, currentErr := strconv.ParseFloat(current, 64)
	proposedNumber, proposedErr := strconv.ParseFloat(proposed, 64)
	if currentErr == nil && proposedErr == nil {
		limit := math.Abs(currentNumber) * adaptiveHysteresisFraction
		if limit < 1 {
			limit = 1
		}
		return math.Abs(proposedNumber-currentNumber) >= limit
	}
	return current != proposed
}

// RunAdaptiveTuning is the slow loop: it re-tunes the values the system already
// chose when the facts moved and returns how many it changed. It never touches a
// value the user set, and it is a no-op when nothing moved.
func RunAdaptiveTuning(dataDir string, cfg *Config) int {
	changes := adaptiveTuningChanges(dataDir, cfg)
	if len(changes) == 0 {
		return 0
	}
	chosen := map[string]bool{}
	for _, change := range changes {
		chosen[change.Key] = true
	}
	applied, err := ApplyTuning(dataDir, cfg, changes, chosen)
	if err != nil {
		logging.Warn("adaptive tuning could not be applied", "error", err)
	}
	return applied
}

// Connections-per-torrent bounds and brackets: the engine does not auto-manage
// this value, so the tuning keeps it between 50 and 400 by the RAM.
const (
	connectionsFloor = int64(50)
	connectionsCeil  = int64(400)
)

func proposedConnectionsPerTorrent(memTotal uint64) int64 {
	const gib = uint64(1024 * 1024 * 1024)
	switch {
	case memTotal == 0:
		return connectionsFloor
	case memTotal < 4*gib:
		return 50
	case memTotal < 8*gib:
		return 100
	case memTotal < 16*gib:
		return 200
	default:
		return connectionsCeil
	}
}

// humanBytesString renders a byte count for a sentence ("1.8 TB").
func humanBytesString(bytes uint64) string {
	return logging.HumanBytesI64(saturatingInt64(bytes))
}
