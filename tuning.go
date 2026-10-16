package gextto

import (
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

// proposedMinFreeSpaceGB is the free-space guard the tuning would set: at least
// 20 GB or 5% of the volume, capped at half of the free space.
func proposedMinFreeSpaceGB(facts SystemFacts) int64 {
	const gib = uint64(1024 * 1024 * 1024)
	if !facts.Download.Exists || facts.Download.Total == 0 {
		return 0
	}
	target := uint64(20) * gib
	if fivePercent := facts.Download.Total / 20; fivePercent > target {
		target = fivePercent
	}
	if half := facts.Download.Free / 2; target > half {
		target = half
	}
	if target < gib {
		return 0
	}
	return int64(target / gib)
}

// humanBytesString renders a byte count for a sentence ("1.8 TB").
func humanBytesString(bytes uint64) string {
	return logging.HumanBytesI64(saturatingInt64(bytes))
}
