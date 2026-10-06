package tui

import (
	"context"
	"net/url"
)

// Job is one background job of GET /api/jobs.
type Job struct {
	ID       string  `json:"id"`
	Kind     string  `json:"kind"`
	State    string  `json:"state"`
	Progress float64 `json:"progress"`
	Message  string  `json:"message"`
	Error    string  `json:"error"`
}

// Running reports a job that can still be cancelled.
func (j Job) Running() bool { return j.State == "running" || j.State == "queued" }

// BackupItem is one backup archive.
type BackupItem struct {
	Name      string `json:"name"`
	SizeBytes int64  `json:"size_bytes"`
	Label     string `json:"label"`
}

// DBFile is one database file of GET /api/db/info.
type DBFile struct {
	Name      string `json:"name"`
	SizeBytes int64  `json:"size_bytes"`
	Exists    bool   `json:"exists"`
}

// RamdiskPath is one candidate RAM disk folder.
type RamdiskPath struct {
	Path       string `json:"path"`
	Filesystem string `json:"filesystem"`
	Writable   bool   `json:"writable"`
	FreeBytes  int64  `json:"free_bytes"`
	Configured bool   `json:"configured"`
}

// RamdiskInfo is the response of GET /api/ramdisk.
type RamdiskInfo struct {
	Enabled      bool          `json:"enabled"`
	Configured   *string       `json:"configured"`
	ConfiguredOK bool          `json:"configured_ok"`
	Problem      string        `json:"problem"`
	Paths        []RamdiskPath `json:"paths"`
	CreatePath   string        `json:"create_path"`
}

// MaintenanceData is what the Maintenance tab shows.
type MaintenanceData struct {
	Jobs    []Job
	Backups []BackupItem
	DBFiles []DBFile
	Ramdisk *RamdiskInfo
}

// Jobs lists the background jobs, newest first.
func (c *Client) Jobs(ctx context.Context) ([]Job, error) {
	var response struct {
		Jobs []Job `json:"jobs"`
	}
	err := c.get(ctx, "/api/jobs", &response)
	return response.Jobs, err
}

// CancelJob asks a running job to stop.
func (c *Client) CancelJob(ctx context.Context, id string) error {
	return c.postJSON(ctx, "/api/jobs/"+url.PathEscape(id)+"/cancel", map[string]any{}, nil)
}

// Backups lists the backup archives, newest first.
func (c *Client) Backups(ctx context.Context) ([]BackupItem, error) {
	var response struct {
		Items []BackupItem `json:"items"`
	}
	err := c.get(ctx, "/api/backup/list", &response)
	return response.Items, err
}

// CreateBackup writes a new backup and returns its path.
func (c *Client) CreateBackup(ctx context.Context) (string, int64, error) {
	var response struct {
		Path      string `json:"path"`
		SizeBytes int64  `json:"size_bytes"`
	}
	err := c.postJSON(ctx, "/api/backup", map[string]any{}, &response)
	return response.Path, response.SizeBytes, err
}

// DBInfo lists the database files.
func (c *Client) DBInfo(ctx context.Context) ([]DBFile, error) {
	var response struct {
		Files []DBFile `json:"files"`
	}
	err := c.get(ctx, "/api/db/info", &response)
	return response.Files, err
}

// DBAction runs check, vacuum or analyze and returns the raw response.
func (c *Client) DBAction(ctx context.Context, action string) (map[string]any, error) {
	var response map[string]any
	err := c.postJSON(ctx, "/api/db/action", map[string]any{"action": action}, &response)
	return response, err
}

// Ramdisk describes the RAM disk configuration and candidates.
func (c *Client) Ramdisk(ctx context.Context) (RamdiskInfo, error) {
	var info RamdiskInfo
	err := c.get(ctx, "/api/ramdisk", &info)
	return info, err
}

// SelectRamdisk configures (create=false) or creates a RAM disk folder.
func (c *Client) SelectRamdisk(ctx context.Context, path string, create bool) error {
	endpoint := "/api/ramdisk/select"
	if create {
		endpoint = "/api/ramdisk/create"
	}
	return c.postJSON(ctx, endpoint, map[string]any{"path": path}, nil)
}

// Duplicates previews (execute=false) or removes the inferior duplicates.
func (c *Client) Duplicates(ctx context.Context, execute bool) ([]map[string]any, int, error) {
	var response struct {
		Removed int              `json:"removed"`
		Items   []map[string]any `json:"items"`
	}
	err := c.postJSON(ctx, "/api/maintenance/clean-duplicates", map[string]any{"execute": execute}, &response)
	return response.Items, response.Removed, err
}

// FolderRenameScan proposes new names for the files in a folder.
func (c *Client) FolderRenameScan(ctx context.Context, path string) ([]map[string]any, error) {
	var response struct {
		Items []map[string]any `json:"items"`
	}
	err := c.postJSON(ctx, "/api/maintenance/rename-folder/scan", map[string]any{"path": path}, &response)
	return response.Items, err
}

// FolderRenameApply renames the chosen source → target pairs.
func (c *Client) FolderRenameApply(ctx context.Context, path string, pairs []map[string]string) (int, error) {
	var response struct {
		Renamed int `json:"renamed"`
	}
	err := c.postJSON(ctx, "/api/maintenance/rename-folder/apply", map[string]any{"path": path, "items": pairs}, &response)
	return response.Renamed, err
}

// StartMaintenance posts one of the fire-and-forget maintenance endpoints
// and returns its response.
func (c *Client) StartMaintenance(ctx context.Context, path string) (map[string]any, error) {
	var response map[string]any
	err := c.postJSON(ctx, path, map[string]any{}, &response)
	return response, err
}
