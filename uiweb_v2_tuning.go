package gextto

import (
	"fmt"
	"net/http"
	"strings"
)

// uiweb_v2_tuning.go is the "Gestione automatica" card: it shows the values the
// system would set for this hardware and writes nothing until the user confirms.
// The proposal itself lives in tuning.go; this file only renders it and handles
// the one apply action.
//
// Facilitata and Esperto show the same card differently. In Facilitata the user
// trusts the system, so the card is a single button and a line of summary: the
// internals are applied without asking, and they stay visible in Esperto. In
// Esperto the user reviews each proposed value with a checkbox and applies the
// ones they want.

// v2TuningRow is one proposed change, ready to render.
type v2TuningRow struct {
	Key        string
	Label      string
	Current    string
	Proposed   string
	Why        string
	CheckLabel string
}

// v2TuningPanel is the whole card, or an empty proposal.
type v2TuningPanel struct {
	Title      string
	Hint       string
	Summary    string
	Empty      string
	DryRunNote string
	DryRun     bool
	Brief      bool
	ApplyLabel string
	Rows       []v2TuningRow
	Notice     string
	Error      bool
}

// v2TuningPanelFrom builds the card from the current facts and the saved origin.
// It never writes anything.
func v2TuningPanelFrom(s *AppState, cfg *Config, brief bool) *v2TuningPanel {
	panel := &v2TuningPanel{
		Title:      uiText(s, "Gestione automatica"),
		DryRunNote: uiText(s, "In prova (dry-run) la proposta si mostra ma non si applica."),
		Empty:      uiText(s, "Il sistema è già configurato bene per questo hardware."),
		Brief:      brief,
		DryRun:     cfg != nil && cfg.DryRun,
	}
	if brief {
		panel.Hint = uiText(s, "Il sistema adatta cache, coda, spazio e RAM disk a questo hardware, senza superare i tuoi limiti.")
		panel.ApplyLabel = uiText(s, "Adatta ora")
	} else {
		panel.Hint = uiText(s, "Valori adatti a questo hardware: nulla viene scritto finché non applichi.")
		panel.ApplyLabel = uiText(s, "Applica selezionate")
	}
	if cfg == nil {
		return panel
	}
	tr := func(italian string) string { return uiText(s, italian) }
	for _, proposal := range ProposeTuning(ProbeSystem(cfg), cfg, ManualSettings(s.cfg.DataDir)) {
		label := v2SettingLabelFor(proposal.Key)
		panel.Rows = append(panel.Rows, v2TuningRow{
			Key:        proposal.Key,
			Label:      label,
			Current:    proposal.Current,
			Proposed:   proposal.Proposed,
			Why:        fmt.Sprintf(tr(proposal.WhyTemplate), proposal.WhyArgs...),
			CheckLabel: fmt.Sprintf(tr("Applica %s"), label),
		})
	}
	if brief && len(panel.Rows) > 0 {
		panel.Summary = fmt.Sprintf(tr("Il sistema adatterà %d valori a questo hardware."), len(panel.Rows))
	}
	return panel
}

// V2SettingsTuningApply applies the proposal and re-renders the card. The value
// written is the server's own proposal, never one coming from the request: the
// form only chooses which rows to apply (all of them in Facilitata).
func V2SettingsTuningApply(w http.ResponseWriter, r *http.Request, s *AppState) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	cfg := latestConfig(s)
	brief := v2SettingsIsBrief(v2SettingsMode(r))

	chosen := map[string]bool{}
	if brief {
		for _, proposal := range ProposeTuning(ProbeSystem(cfg), cfg, ManualSettings(s.cfg.DataDir)) {
			chosen[proposal.Key] = true
		}
	} else {
		for _, key := range r.Form["key"] {
			if key = strings.TrimSpace(key); key != "" {
				chosen[key] = true
			}
		}
	}

	panel := v2TuningPanelFrom(s, cfg, brief)
	switch {
	case cfg.DryRun:
		panel.Notice = uiText(s, "In prova (dry-run) nulla viene applicato.")
	case len(chosen) == 0:
		panel.Notice = uiText(s, "Nessuna modifica selezionata.")
	default:
		proposals := ProposeTuning(ProbeSystem(cfg), cfg, ManualSettings(s.cfg.DataDir))
		applied, err := ApplyTuning(s.cfg.DataDir, cfg, proposals, chosen)
		if err != nil {
			panel.Error = true
			panel.Notice = err.Error()
		} else {
			// Reload so the card reflects the values just written, and report.
			panel = v2TuningPanelFrom(s, latestConfig(s), brief)
			if applied == 1 {
				panel.Notice = uiText(s, "Un'impostazione applicata.")
			} else {
				panel.Notice = fmt.Sprintf(uiText(s, "%d impostazioni applicate."), applied)
			}
		}
	}

	dict, eng := v2Dictionaries(s)
	v2Render(w, http.StatusOK, "v2_tuning_panel", panel, dict, eng)
}
