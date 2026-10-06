package tui

import (
	"context"
	"fmt"
)

// fetchMaintenance loads the Maintenance tab: the jobs at every poll, the
// rest with the slow data.
func fetchMaintenance(ctx context.Context, client *Client, slow bool, previous *MaintenanceData) *MaintenanceData {
	data := &MaintenanceData{}
	if previous != nil {
		*data = *previous
	}
	if jobs, err := client.Jobs(ctx); err == nil {
		data.Jobs = jobs
	}
	if slow || previous == nil {
		if backups, err := client.Backups(ctx); err == nil {
			data.Backups = backups
		}
		if files, err := client.DBInfo(ctx); err == nil {
			data.DBFiles = files
		}
		if info, err := client.Ramdisk(ctx); err == nil {
			data.Ramdisk = &info
		}
	}
	return data
}

// performMaintenanceAction runs the Maintenance tab operations. It reports
// whether action was one of them.
func performMaintenanceAction(ctx context.Context, client *Client, tr *Translator, action Action) (actionResult, bool) {
	result := actionResult{}
	fail := func(err error) (actionResult, bool) {
		result.message = tr.Format("msg.maintfailed", tr.T("maint."+action.Domain), err)
		return result, true
	}
	report := func(value Report) {
		result.apply = func(m *Model) { m.SetReport(value) }
	}
	// started reports a background job; the jobs list shows its progress.
	started := func(response map[string]any) {
		result.message = tr.Format("msg.jobstarted", firstNonEmpty(stringValue(response["message"]), tr.T("maint."+action.Domain)))
		result.refresh = true
	}
	switch action.Kind {
	case ActionCancelJob:
		if err := client.CancelJob(ctx, action.Text); err != nil {
			action.Domain = "job"
			return fail(err)
		}
		result.message = tr.T("msg.jobcancelled")
		result.refresh = true
		return result, true
	case ActionFolderRenameApply:
		pairs, _ := action.Fields["pairs"].([]map[string]string)
		renamed, err := client.FolderRenameApply(ctx, action.Text, pairs)
		if err != nil {
			action.Domain = "folderrename"
			return fail(err)
		}
		result.apply = func(m *Model) {
			m.Overlay = OverlayNone
			m.Report = nil
		}
		result.message = tr.Format("msg.folderrenamed", renamed, len(pairs))
		return result, true
	case ActionMaintenance:
	default:
		return result, false
	}

	switch action.Domain {
	case "backup":
		path, size, err := client.CreateBackup(ctx)
		if err != nil {
			return fail(err)
		}
		result.message = tr.Format("msg.backupdone", path, humanSize(size))
		result.refresh = true
	case "dbcheck", "vacuum", "analyze":
		name := map[string]string{"dbcheck": "check", "vacuum": "vacuum", "analyze": "analyze"}[action.Domain]
		response, err := client.DBAction(ctx, name)
		if err != nil {
			return fail(err)
		}
		report(dbReport(tr, action.Domain, response))
		result.clearMessage = true
	case "duplicates":
		items, _, err := client.Duplicates(ctx, false)
		if err != nil {
			return fail(err)
		}
		report(Report{Kind: ReportDuplicates, Title: tr.Format("maint.duptitle", len(items)), Items: items})
		result.clearMessage = true
	case "duplicatesexecute":
		_, removed, err := client.Duplicates(ctx, true)
		if err != nil {
			action.Domain = "duplicates"
			return fail(err)
		}
		result.apply = func(m *Model) {
			m.Overlay = OverlayNone
			m.Report = nil
		}
		result.message = tr.Format("msg.duplicatesremoved", removed)
	case "folderscan":
		items, err := client.FolderRenameScan(ctx, action.Text)
		if err != nil {
			action.Domain = "folderrename"
			return fail(err)
		}
		report(folderRenameReport(tr, action.Text, items))
		result.clearMessage = true
	case "ramdisk":
		info, _ := client.Ramdisk(ctx)
		create := info.CreatePath != "" && action.Text == info.CreatePath
		if err := client.SelectRamdisk(ctx, action.Text, create); err != nil {
			return fail(err)
		}
		result.message = tr.Format("msg.ramdiskset", action.Text)
		result.refresh = true
	case "renameall", "scanarchives", "backfill":
		path := map[string]string{
			"renameall":    "/api/rename-all",
			"scanarchives": "/api/scan-all-archives",
			"backfill":     "/api/maintenance/backfill-media-info",
		}[action.Domain]
		response, err := client.StartMaintenance(ctx, path)
		if err != nil {
			return fail(err)
		}
		started(response)
	case "housekeeping":
		response, err := client.StartMaintenance(ctx, "/api/maintenance/housekeeping")
		if err != nil {
			return fail(err)
		}
		values, _ := response["report"].(map[string]any)
		report(mapReport(tr.T("maint.housekeeping"), values))
		result.clearMessage = true
	default:
		result.message = fmt.Sprintf("? %s", action.Domain)
	}
	return result, true
}
