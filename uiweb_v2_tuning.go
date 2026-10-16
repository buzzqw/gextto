package gextto

import (
	"fmt"
	"net/http"
	"strings"
)

// uiweb_v2_tuning.go is the "Ottimizza per il sistema" panel: it shows the
// values the guided tuning would set for this hardware, each with a checkbox,
// and writes nothing until the user confirms. The proposal itself lives in
// tuning.go; this file only renders it and handles the one apply action.

// v2TuningRow is one proposed change, ready to render.
type v2TuningRow struct {
	Key       string
	Label     string
	Current   string
	Proposed  string
	Why       string
	CheckLabel string
}

// v2TuningPanel is the whole panel, or an empty proposal.
type v2TuningPanel struct {
	Title      string
	Hint       string
	Empty      string
	DryRunNote string
	DryRun     bool
	ApplyLabel string
	Rows       []v2TuningRow
	Notice     string
	Error      bool
}

// v2TuningPanelFrom builds the panel from the current facts and the saved
// origin. It never writes anything.
func v2TuningPanelFrom(s *AppState, cfg *Config) *v2TuningPanel {
	panel := &v2TuningPanel{
		Title:      uiText(s, "Ottimizza per il sistema"),
		Hint:       uiText(s, "Valori adatti a questo hardware: nulla viene scritto finché non applichi."),
		Empty:      uiText(s, "Il sistema è già configurato bene per questo hardware."),
		DryRunNote: uiText(s, "In prova (dry-run) la proposta si mostra ma non si applica."),
		ApplyLabel: uiText(s, "Applica selezionate"),
		DryRun:     cfg != nil && cfg.DryRun,
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
	return panel
}

// V2SettingsTuningApply applies the confirmed proposal rows and re-renders the
// panel. The value written is the server's own proposal, never one coming from
// the request: the form only chooses which rows to apply.
func V2SettingsTuningApply(w http.ResponseWriter, r *http.Request, s *AppState) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	cfg := latestConfig(s)
	chosen := map[string]bool{}
	for _, key := range r.Form["key"] {
		if key = strings.TrimSpace(key); key != "" {
			chosen[key] = true
		}
	}

	panel := v2TuningPanelFrom(s, cfg)
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
			// Reload so the panel shows the values just written, and report.
			panel = v2TuningPanelFrom(s, latestConfig(s))
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
