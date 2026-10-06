package tui

import (
	"fmt"
	"sort"
	"strings"
)

// maintEntry is one line of the Maintenance menu: a running job (Enter
// cancels it) or an operation (Enter runs it).
type maintEntry struct {
	Key    string
	Label  string
	Status string
	Job    *Job
}

// ReportKind tells what the result overlay can do with its rows.
type ReportKind int

const (
	ReportText ReportKind = iota
	ReportDuplicates
	ReportFolderRename
)

// Report is the result overlay of a maintenance operation.
type Report struct {
	Kind     ReportKind
	Title    string
	Lines    []string
	Items    []map[string]any
	Include  []bool
	Path     string
	Selected int
	Scroll   int
}

// SetReport opens the result overlay.
func (m *Model) SetReport(report Report) {
	m.Report = &report
	m.Overlay = OverlayMaintenance
}

func (m *Model) maintenanceEntries() []maintEntry {
	entries := []maintEntry{}
	if m.Maintenance != nil {
		for index := range m.Maintenance.Jobs {
			job := m.Maintenance.Jobs[index]
			if job.Running() {
				entries = append(entries, maintEntry{Key: "job", Label: m.Tr.Format("maint.job", job.Kind), Status: joinNonEmpty(fmt.Sprintf("%.0f%%", job.Progress*100), job.Message), Job: &job})
			}
		}
	}
	add := func(key, status string) {
		entries = append(entries, maintEntry{Key: key, Label: m.Tr.T("maint." + key), Status: status})
	}
	backupStatus := ""
	dbStatus := ""
	ramdiskStatus := ""
	if data := m.Maintenance; data != nil {
		if len(data.Backups) > 0 {
			backupStatus = m.Tr.Format("maint.lastbackup", firstNonEmpty(data.Backups[0].Label, data.Backups[0].Name), len(data.Backups))
		} else {
			backupStatus = m.Tr.T("maint.nobackup")
		}
		total := int64(0)
		for _, file := range data.DBFiles {
			total += file.SizeBytes
		}
		if len(data.DBFiles) > 0 {
			dbStatus = m.Tr.Format("maint.dbsize", len(data.DBFiles), humanSize(total))
		}
		if info := data.Ramdisk; info != nil {
			switch {
			case info.Problem != "":
				ramdiskStatus = info.Problem
			case info.Configured != nil && *info.Configured != "":
				ramdiskStatus = *info.Configured
			default:
				ramdiskStatus = m.Tr.T("maint.ramdiskoff")
			}
		}
	}
	add("backup", backupStatus)
	add("dbcheck", dbStatus)
	add("vacuum", "")
	add("analyze", "")
	add("duplicates", "")
	add("renameall", "")
	add("folderrename", "")
	add("scanarchives", "")
	add("backfill", "")
	add("housekeeping", "")
	add("ramdisk", ramdiskStatus)
	add("trash", "")
	return entries
}

func (m *Model) updateMaintenance(k Key) Action {
	entries := m.maintenanceEntries()
	switch k.Kind {
	case KeyUp, KeyDown, KeyPgUp, KeyPgDn, KeyHome, KeyEnd:
		m.MaintSelected = moveSelection(m.MaintSelected, len(entries), k.Kind)
		return Action{}
	case KeyEnter:
	default:
		return Action{}
	}
	if m.MaintSelected < 0 || m.MaintSelected >= len(entries) {
		return Action{}
	}
	entry := entries[m.MaintSelected]
	ask := func(key string, action Action) Action {
		m.Confirm = &confirm{MessageKey: key, Action: action}
		return Action{}
	}
	switch entry.Key {
	case "job":
		m.Confirm = &confirm{MessageKey: "prompt.canceljob", Args: []any{entry.Job.Kind}, Action: Action{Kind: ActionCancelJob, Text: entry.Job.ID}}
		return Action{}
	case "vacuum", "renameall", "scanarchives", "housekeeping":
		return ask("prompt.maint."+entry.Key, Action{Kind: ActionMaintenance, Domain: entry.Key})
	case "trash":
		return ask("prompt.cleantrash", Action{Kind: ActionCleanTrash})
	case "folderrename":
		m.Prompt = newPrompt(PromptFolderRename, m.lastFolder)
		return Action{}
	case "ramdisk":
		value := ""
		if data := m.Maintenance; data != nil && data.Ramdisk != nil {
			if data.Ramdisk.Configured != nil {
				value = *data.Ramdisk.Configured
			}
			if value == "" {
				value = data.Ramdisk.CreatePath
			}
		}
		m.Prompt = newPrompt(PromptRamdisk, value)
		return Action{}
	}
	return Action{Kind: ActionMaintenance, Domain: entry.Key}
}

func (m *Model) updateMaintenanceReport(k Key) Action {
	report := m.Report
	if report == nil {
		m.Overlay = OverlayNone
		return Action{}
	}
	count := len(report.Lines)
	if report.Kind != ReportText {
		count = len(report.Items)
	}
	switch {
	case k.Kind == KeyEsc || (k.Kind == KeyRune && k.Rune == 'q'):
		m.Overlay = OverlayNone
		m.Report = nil
	case k.Kind == KeyRune && k.Rune == ' ' && report.Kind == ReportFolderRename:
		if report.Selected >= 0 && report.Selected < len(report.Include) {
			report.Include[report.Selected] = !report.Include[report.Selected]
			report.Selected = min(report.Selected+1, max(0, count-1))
		}
	case k.Kind == KeyRune && k.Rune == 'x' && report.Kind == ReportDuplicates && len(report.Items) > 0:
		m.Confirm = &confirm{MessageKey: "prompt.duplicates", Args: []any{len(report.Items)}, Action: Action{Kind: ActionMaintenance, Domain: "duplicatesexecute"}}
	case k.Kind == KeyRune && k.Rune == 'x' && report.Kind == ReportFolderRename:
		pairs := report.chosenPairs()
		if len(pairs) == 0 {
			m.Message = m.Tr.T("msg.nothingselected")
			return Action{}
		}
		m.Confirm = &confirm{MessageKey: "prompt.folderapply", Args: []any{len(pairs)}, Action: Action{Kind: ActionFolderRenameApply, Text: report.Path, Fields: map[string]any{"pairs": pairs}}}
	default:
		report.Selected = moveSelection(report.Selected, count, k.Kind)
	}
	return Action{}
}

// chosenPairs lists the included rows that have a target.
func (r *Report) chosenPairs() []map[string]string {
	pairs := []map[string]string{}
	for index, item := range r.Items {
		target := stringValue(item["target"])
		if index < len(r.Include) && r.Include[index] && target != "" {
			pairs = append(pairs, map[string]string{"source": stringValue(item["source"]), "target": target})
		}
	}
	return pairs
}

// folderRenameReport builds the review of a folder rename scan: the rows
// with a clean proposal start selected, the others need a look first.
func folderRenameReport(tr *Translator, path string, items []map[string]any) Report {
	include := make([]bool, len(items))
	for index, item := range items {
		include[index] = stringValue(item["status"]) == "proposta" && stringValue(item["target"]) != "" && !truthy(item["conflict"])
	}
	return Report{Kind: ReportFolderRename, Title: tr.Format("maint.foldertitle", path, len(items)), Items: items, Include: include, Path: path}
}

func (m *Model) renderMaintenance(width, contentHeight int) []Line {
	lines := []Line{{Text: m.Tr.T("maint.title"), Style: StyleHeader}}
	entries := m.maintenanceEntries()
	texts := make([]string, len(entries))
	styles := make([]Style, len(entries))
	labelWidth := 0
	for _, entry := range entries {
		labelWidth = max(labelWidth, StringWidth(entry.Label))
	}
	labelWidth = min(labelWidth, max(12, width/2))
	for index, entry := range entries {
		texts[index] = PadRight(Shorten(entry.Label, labelWidth), labelWidth) + "  " + entry.Status
		styles[index] = StyleNormal
		if entry.Job != nil {
			styles[index] = StyleWarn
		}
	}
	return append(lines, styledRows(texts, styles, m.MaintSelected, &m.MaintScroll, contentHeight-1, width)...)
}

func (m *Model) renderMaintenanceReport(width, contentHeight int) []Line {
	report := m.Report
	if report == nil {
		return nil
	}
	lines := []Line{{Text: report.Title, Style: StyleHeader, Wrap: true}}
	switch report.Kind {
	case ReportText:
		for _, text := range report.Lines {
			lines = append(lines, Line{Text: text, Style: StyleNormal, Wrap: true, Indent: 2})
		}
		return lines
	case ReportDuplicates:
		if len(report.Items) == 0 {
			return append(lines, Line{Text: m.Tr.T("maint.noduplicates"), Style: StyleOK})
		}
		texts := make([]string, len(report.Items))
		for index, item := range report.Items {
			texts[index] = joinNonEmpty(stringValue(item["series"]), episodeLabel(int64(numberValue(item["season"])), int64(numberValue(item["episode"]))), stringValue(item["path"]))
		}
		return append(lines, catalogRows(texts, report.Selected, &report.Scroll, contentHeight-1, width)...)
	}
	if len(report.Items) == 0 {
		return append(lines, Line{Text: m.Tr.T("maint.folderempty"), Style: StyleMuted})
	}
	texts := make([]string, len(report.Items))
	styles := make([]Style, len(report.Items))
	for index, item := range report.Items {
		mark := "[ ]"
		if report.Include[index] {
			mark = "[x]"
		}
		target := stringValue(item["target"])
		status := stringValue(item["status"])
		styles[index] = StyleNormal
		if target == "" || truthy(item["conflict"]) {
			styles[index] = StyleMuted
		}
		arrow := firstNonEmpty(target, joinNonEmpty(status, stringValue(item["reason"])))
		texts[index] = fmt.Sprintf("%s %s → %s", mark, firstNonEmpty(stringValue(item["relative"]), stringValue(item["source"])), arrow)
	}
	return append(lines, styledRows(texts, styles, report.Selected, &report.Scroll, contentHeight-1, width)...)
}

// dbReport turns a /api/db/action response into readable lines.
func dbReport(tr *Translator, action string, response map[string]any) Report {
	report := Report{Kind: ReportText, Title: tr.T("maint." + action)}
	if checks, ok := response["checks"].([]any); ok {
		for _, raw := range checks {
			check, _ := raw.(map[string]any)
			state := "✓"
			if !truthy(check["ok"]) {
				state = "✗"
			}
			report.Lines = append(report.Lines, joinNonEmpty(state+" "+stringValue(check["name"]), stringValue(check["detail"])))
		}
		return report
	}
	before, _ := response["before"].(map[string]any)
	after, _ := response["after"].(map[string]any)
	if before != nil && after != nil {
		report.Lines = append(report.Lines, tr.Format("maint.dbresult",
			HumanBytes(numberValue(before["size_bytes"])), HumanBytes(numberValue(after["size_bytes"]))))
	}
	if len(report.Lines) == 0 {
		report.Lines = []string{tr.T("maint.done")}
	}
	return report
}

// mapReport lists the counters of a housekeeping-like response.
func mapReport(title string, values map[string]any) Report {
	report := Report{Kind: ReportText, Title: title}
	for key, value := range values {
		if number, ok := value.(float64); ok && number != 0 {
			report.Lines = append(report.Lines, fmt.Sprintf("%s: %.0f", strings.ReplaceAll(key, "_", " "), number))
		}
	}
	if len(report.Lines) == 0 {
		report.Lines = []string{"-"}
	}
	sort.Strings(report.Lines)
	return report
}
