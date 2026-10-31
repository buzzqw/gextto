package gextto

// media_integrity.go checks the archived media files with ffprobe and ffmpeg.
// The probe validates the container and the streams, then ffmpeg decodes every
// video and audio stream to find damage in the middle of a file that a
// header-only probe would never see. The scan walks the library one file at a
// time and keeps its position, so it can run in the background for hours and be
// resumed after a restart.

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/buzzqw/gextto/internal/logging"
	"github.com/buzzqw/gextto/internal/models"
)

const (
	// mediaIntegrityJobKind is the JobManager kind and deduplication key.
	mediaIntegrityJobKind = "media-integrity"

	// mediaIntegrityProbeTimeout bounds the metadata probe: reading the header
	// of a file on a slow network share must never pin the worker.
	mediaIntegrityProbeTimeout = 2 * time.Minute
	// mediaIntegrityDecodeTimeout bounds the full decode. A two-hour movie on a
	// slow disk can take a while; beyond this the file is reported as unreadable
	// with the timeout as the reason, and the scan moves on.
	mediaIntegrityDecodeTimeout = 30 * time.Minute
	// mediaIntegrityDetailLimit bounds the ffmpeg message kept for a file.
	mediaIntegrityDetailLimit = 500
	// mediaIntegrityStateKey is the ui_state key holding the resumable scan.
	mediaIntegrityStateKey = "media_integrity_state"
)

// mediaIntegrityPause lets the disk (and the rest of Gextto) breathe between
// two files. It is a variable so the tests can remove the wait.
var mediaIntegrityPause = 2 * time.Second

// databaseMediaIntegritySchema stores the files the scan found damaged. The
// columns of the identity are NOT NULL with a 0/"" sentinel so the UNIQUE
// constraint recognises the same item across passes (SQLite treats NULL values
// as distinct). target_id is the episode or movie row that the UI uses to offer
// a re-download.
const databaseMediaIntegritySchema = `CREATE TABLE IF NOT EXISTS media_integrity (
                id INTEGER PRIMARY KEY AUTOINCREMENT,
                target_id INTEGER NOT NULL DEFAULT 0,
                kind TEXT NOT NULL,
                series_name TEXT NOT NULL DEFAULT '',
                season INTEGER NOT NULL DEFAULT 0,
                episode INTEGER NOT NULL DEFAULT 0,
                movie_name TEXT NOT NULL DEFAULT '',
                movie_year INTEGER NOT NULL DEFAULT 0,
                path TEXT NOT NULL,
                status TEXT NOT NULL,
                detail TEXT NOT NULL DEFAULT '',
                size_bytes INTEGER NOT NULL DEFAULT 0,
                modified_ns INTEGER NOT NULL DEFAULT 0,
                checked_at TEXT NOT NULL,
                UNIQUE(kind, series_name, season, episode, movie_name, movie_year)
            );
            CREATE INDEX IF NOT EXISTS idx_media_integrity_checked ON media_integrity(checked_at);`

// MediaIntegrityTarget is one archived file the scan has to check.
type MediaIntegrityTarget struct {
	// Kind is "episode" or "movie".
	Kind      string
	ID        int64
	Series    string
	Season    *int64
	Episode   *int64
	MovieName string
	MovieYear *int64
	Path      string
	// SizeBytes and ModifiedNS are the current file identity, read when the
	// target is selected. They are stored with the outcome so a later pass can
	// skip the file while it is unchanged.
	SizeBytes  int64
	ModifiedNS int64
}

// Label names the library item the way the operator sees it.
func (t MediaIntegrityTarget) Label() string {
	if t.Kind == "movie" {
		if t.MovieYear != nil && *t.MovieYear > 0 {
			return fmt.Sprintf("%s (%d)", t.MovieName, *t.MovieYear)
		}
		return t.MovieName
	}
	if t.Season != nil && t.Episode != nil {
		return fmt.Sprintf("%s S%02dE%02d", t.Series, *t.Season, *t.Episode)
	}
	return t.Series
}

// cursor is the resumable position of the target in the scan.
func (t MediaIntegrityTarget) cursor() string {
	if t.Kind == "movie" {
		return fmt.Sprintf("m:%d", t.ID)
	}
	return fmt.Sprintf("e:%d", t.ID)
}

// MediaIntegrityProblem is a damaged or unreadable file stored by the scan.
type MediaIntegrityProblem struct {
	TargetID   int64  `json:"target_id"`
	Kind       string `json:"kind"`
	Series     string `json:"series"`
	Season     int64  `json:"season"`
	Episode    int64  `json:"episode"`
	MovieName  string `json:"movie_name"`
	MovieYear  int64  `json:"movie_year"`
	Path       string `json:"path"`
	Status     string `json:"status"`
	Detail     string `json:"detail"`
	SizeBytes  int64  `json:"size_bytes"`
	ModifiedNS int64  `json:"modified_ns"`
	CheckedAt  string `json:"checked_at"`
}

// Label names the problem the way the operator sees it.
func (p MediaIntegrityProblem) Label() string {
	if p.Kind == "movie" {
		if p.MovieYear > 0 {
			return fmt.Sprintf("%s (%d)", p.MovieName, p.MovieYear)
		}
		return p.MovieName
	}
	if p.Season > 0 || p.Episode > 0 {
		return fmt.Sprintf("%s S%02dE%02d", p.Series, p.Season, p.Episode)
	}
	return p.Series
}

// MediaIntegrityState is the resumable progress of the scan. It is stored as
// JSON in ui_state so a restart continues where it left off.
type MediaIntegrityState struct {
	Cursor string `json:"cursor"`
	// Force rechecks every file, even the ones already checked and unchanged.
	Force      bool   `json:"force"`
	Total      int    `json:"total"`
	Checked    int    `json:"checked"`
	Damaged    int    `json:"damaged"`
	Running    bool   `json:"running"`
	Done       bool   `json:"done"`
	Current    string `json:"current"`
	StartedAt  string `json:"started_at"`
	FinishedAt string `json:"finished_at"`
}

// MediaIntegrityStatus is the answer of the status endpoint and the model of the
// Maintenance panel.
type MediaIntegrityStatus struct {
	OK         bool                    `json:"ok"`
	Available  bool                    `json:"available"`
	Missing    []string                `json:"missing,omitempty"`
	Running    bool                    `json:"running"`
	Done       bool                    `json:"done"`
	Total      int                     `json:"total"`
	Checked    int                     `json:"checked"`
	Damaged    int                     `json:"damaged"`
	Cursor     string                  `json:"cursor"`
	Current    string                  `json:"current"`
	StartedAt  string                  `json:"started_at"`
	FinishedAt string                  `json:"finished_at"`
	Problems   []MediaIntegrityProblem `json:"problems"`
}

// ---------------------------------------------------------------------------
// Database
// ---------------------------------------------------------------------------

// mediaIntegrityCandidate is one archived file with its current identity and
// the outcome stored by an earlier pass (when any).
type mediaIntegrityCandidate struct {
	target      MediaIntegrityTarget
	storedSize  int64
	storedMTime int64
	hasStored   bool
}

// needsCheck reports whether the file has to be probed again: never checked,
// changed since the last check, or a forced pass.
func (c mediaIntegrityCandidate) needsCheck(force bool) bool {
	if force || !c.hasStored {
		return true
	}
	return c.target.SizeBytes != c.storedSize || c.target.ModifiedNS != c.storedMTime
}

// mediaIntegrityFileIdentity returns the size and modification time of the
// video file behind an archived path. A missing or unreadable path yields
// zeros, so a file that appears later is always seen as changed.
func mediaIntegrityFileIdentity(path string) (int64, int64) {
	file, err := mediaIntegrityResolveFile(path)
	if err != nil {
		return 0, 0
	}
	info, err := os.Stat(file)
	if err != nil {
		return 0, 0
	}
	return info.Size(), info.ModTime().UnixNano()
}

// queryMediaIntegrityCandidates reads up to limit archived items after the
// cursor, with the outcome of the previous pass when it exists. Episodes come
// first (ordered by row id), then movies.
func (d *Database) queryMediaIntegrityCandidates(after string, limit int) ([]mediaIntegrityCandidate, error) {
	limit = clampInt(limit, 1, 1000)
	if after == "done" {
		return nil, nil
	}
	kind := ""
	var afterID int64
	switch {
	case strings.HasPrefix(after, "e:"):
		kind = "episode"
		fmt.Sscanf(after[2:], "%d", &afterID)
	case strings.HasPrefix(after, "m:"):
		kind = "movie"
		fmt.Sscanf(after[2:], "%d", &afterID)
	}

	candidates := make([]mediaIntegrityCandidate, 0, limit)
	if kind != "movie" {
		type episodeRow struct {
			id, season, episode, size, modified int64
			series, path, status                string
		}
		var rowsData []episodeRow
		rows, err := d.db.Query(`SELECT e.id, s.name, e.season, e.episode, e.archive_path,
				COALESCE(mi.size_bytes,0), COALESCE(mi.modified_ns,0), COALESCE(mi.status,'')
			FROM episodes e
			JOIN series s ON s.id=e.series_id
			LEFT JOIN media_integrity mi ON mi.kind='episode' AND mi.series_name=s.name
				AND mi.season=e.season AND mi.episode=e.episode
			WHERE e.id > ?1 AND COALESCE(e.archive_path,'') <> ''
			ORDER BY e.id LIMIT ?2`, afterID, limit)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var row episodeRow
			if err := rows.Scan(&row.id, &row.series, &row.season, &row.episode, &row.path,
				&row.size, &row.modified, &row.status); err != nil {
				rows.Close()
				return nil, err
			}
			rowsData = append(rowsData, row)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
		for _, row := range rowsData {
			season, episode := row.season, row.episode
			size, modified := mediaIntegrityFileIdentity(row.path)
			candidates = append(candidates, mediaIntegrityCandidate{
				target: MediaIntegrityTarget{
					Kind: "episode", ID: row.id, Series: row.series,
					Season: &season, Episode: &episode, Path: row.path,
					SizeBytes: size, ModifiedNS: modified,
				},
				storedSize: row.size, storedMTime: row.modified,
				hasStored: strings.TrimSpace(row.status) != "",
			})
			if len(candidates) >= limit {
				return candidates, nil
			}
		}
	}

	remaining := limit - len(candidates)
	if remaining > 0 {
		movieAfter := int64(0)
		if kind == "movie" {
			movieAfter = afterID
		}
		type movieRow struct {
			id, year, size, modified int64
			name, path, status       string
		}
		var rowsData []movieRow
		rows, err := d.db.Query(`SELECT m.id, COALESCE(m.name,''), COALESCE(m.year,0), t.processed_path,
				COALESCE(mi.size_bytes,0), COALESCE(mi.modified_ns,0), COALESCE(mi.status,'')
			FROM movies m
			JOIN torrent_meta t ON lower(t.hash)=lower(m.magnet_hash)
			LEFT JOIN media_integrity mi ON mi.kind='movie' AND mi.movie_name=COALESCE(m.name,'')
				AND mi.movie_year=COALESCE(m.year,0)
			WHERE m.id > ?1 AND m.removed_at IS NULL AND COALESCE(t.processed_path,'') <> ''
			ORDER BY m.id LIMIT ?2`, movieAfter, remaining)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var row movieRow
			if err := rows.Scan(&row.id, &row.name, &row.year, &row.path,
				&row.size, &row.modified, &row.status); err != nil {
				rows.Close()
				return nil, err
			}
			rowsData = append(rowsData, row)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
		for _, row := range rowsData {
			year := row.year
			size, modified := mediaIntegrityFileIdentity(row.path)
			candidates = append(candidates, mediaIntegrityCandidate{
				target: MediaIntegrityTarget{
					Kind: "movie", ID: row.id, MovieName: row.name,
					MovieYear: &year, Path: row.path, SizeBytes: size, ModifiedNS: modified,
				},
				storedSize: row.size, storedMTime: row.modified,
				hasStored: strings.TrimSpace(row.status) != "",
			})
		}
	}
	if len(candidates) == 0 {
		return nil, nil
	}
	return candidates, nil
}

// MediaIntegrityTargets returns up to limit files that need a check after the
// given cursor, and the cursor to use for the next call. Files already checked
// and unchanged are skipped unless force is set. "done" means the library has
// been walked completely.
func (d *Database) MediaIntegrityTargets(after string, limit int, force bool) ([]MediaIntegrityTarget, string, error) {
	limit = clampInt(limit, 1, 100)
	if after == "done" {
		return nil, "done", nil
	}
	// A forced pass reads one file per step. A normal pass reads a wider
	// window so a long run of already checked files is skipped with few
	// queries instead of one per file.
	chunk := limit
	if !force {
		chunk = limit * 20
		if chunk < 200 {
			chunk = 200
		}
		if chunk > 1000 {
			chunk = 1000
		}
	}
	targets := make([]MediaIntegrityTarget, 0, limit)
	cursor := after
	lastExamined := after
	for len(targets) < limit {
		candidates, err := d.queryMediaIntegrityCandidates(cursor, chunk)
		if err != nil {
			return nil, "", err
		}
		if len(candidates) == 0 {
			if len(targets) == 0 {
				return nil, "done", nil
			}
			return targets, lastExamined, nil
		}
		for _, candidate := range candidates {
			lastExamined = candidate.target.cursor()
			if !candidate.needsCheck(force) {
				continue
			}
			targets = append(targets, candidate.target)
			if len(targets) >= limit {
				return targets, lastExamined, nil
			}
		}
		cursor = lastExamined
	}
	return targets, lastExamined, nil
}

// CountMediaIntegrityTargets counts the files the next pass will check.
func (d *Database) CountMediaIntegrityTargets(force bool) (int, error) {
	total := 0
	cursor := ""
	for {
		candidates, err := d.queryMediaIntegrityCandidates(cursor, 500)
		if err != nil {
			return 0, err
		}
		if len(candidates) == 0 {
			return total, nil
		}
		for _, candidate := range candidates {
			if candidate.needsCheck(force) {
				total++
			}
		}
		cursor = candidates[len(candidates)-1].target.cursor()
	}
}

// SaveMediaIntegrityCheck stores or updates the outcome of one file, including
// the identity used to skip it while it does not change.
func (d *Database) SaveMediaIntegrityCheck(check MediaIntegrityProblem) error {
	_, err := d.db.Exec(`INSERT INTO media_integrity
		(target_id, kind, series_name, season, episode, movie_name, movie_year, path, status, detail,
		 size_bytes, modified_ns, checked_at)
		VALUES (?1,?2,?3,?4,?5,?6,?7,?8,?9,?10,?11,?12,?13)
		ON CONFLICT(kind, series_name, season, episode, movie_name, movie_year) DO UPDATE SET
			target_id=excluded.target_id, path=excluded.path, status=excluded.status,
			detail=excluded.detail, size_bytes=excluded.size_bytes, modified_ns=excluded.modified_ns,
			checked_at=excluded.checked_at`,
		check.TargetID, check.Kind, check.Series, check.Season, check.Episode,
		check.MovieName, check.MovieYear, check.Path, check.Status, check.Detail,
		check.SizeBytes, check.ModifiedNS, check.CheckedAt)
	return err
}

// MediaIntegrityProblems lists the damaged or unreadable files, newest first.
func (d *Database) MediaIntegrityProblems() ([]MediaIntegrityProblem, error) {
	rows, err := d.db.Query(`SELECT target_id, kind, series_name, season, episode, movie_name,
		movie_year, path, status, detail, size_bytes, modified_ns, checked_at
		FROM media_integrity WHERE status <> 'ok' ORDER BY checked_at DESC, id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	problems := make([]MediaIntegrityProblem, 0)
	for rows.Next() {
		var problem MediaIntegrityProblem
		if err := rows.Scan(&problem.TargetID, &problem.Kind, &problem.Series, &problem.Season,
			&problem.Episode, &problem.MovieName, &problem.MovieYear, &problem.Path,
			&problem.Status, &problem.Detail, &problem.SizeBytes, &problem.ModifiedNS, &problem.CheckedAt); err != nil {
			return nil, err
		}
		problems = append(problems, problem)
	}
	return problems, rows.Err()
}

// ClearMediaIntegrityChecks forgets every outcome: the next pass rechecks the
// whole library.
func (d *Database) ClearMediaIntegrityChecks() error {
	_, err := d.db.Exec(`DELETE FROM media_integrity`)
	return err
}

// PruneMediaIntegrityChecks removes the outcomes of episodes and movies that
// left the library, so the panel and the skip list only describe current items.
func (d *Database) PruneMediaIntegrityChecks() error {
	_, err := d.db.Exec(`DELETE FROM media_integrity
		WHERE (kind='episode' AND NOT EXISTS (
			SELECT 1 FROM episodes e JOIN series s ON s.id=e.series_id
			WHERE s.name=media_integrity.series_name AND e.season=media_integrity.season
				AND e.episode=media_integrity.episode))
		OR (kind='movie' AND NOT EXISTS (
			SELECT 1 FROM movies m WHERE m.name=media_integrity.movie_name
				AND COALESCE(m.year,0)=media_integrity.movie_year AND m.removed_at IS NULL))`)
	return err
}

// ClearMediaIntegrityProblem removes one item once it has been sent back to the
// downloads, so the panel does not keep offering a file that is already gone.
func (d *Database) ClearMediaIntegrityProblem(problem MediaIntegrityProblem) error {
	_, err := d.db.Exec(`DELETE FROM media_integrity
		WHERE kind=?1 AND series_name=?2 AND season=?3 AND episode=?4 AND movie_name=?5 AND movie_year=?6`,
		problem.Kind, problem.Series, problem.Season, problem.Episode, problem.MovieName, problem.MovieYear)
	return err
}

// LoadMediaIntegrityState reads the resumable scan state.
func (d *Database) LoadMediaIntegrityState() MediaIntegrityState {
	raw, ok := d.uiStateValue(mediaIntegrityStateKey)
	if !ok {
		return MediaIntegrityState{}
	}
	var state MediaIntegrityState
	if json.Unmarshal([]byte(raw), &state) != nil {
		return MediaIntegrityState{}
	}
	return state
}

// SaveMediaIntegrityState writes the resumable scan state.
func (d *Database) SaveMediaIntegrityState(state MediaIntegrityState) error {
	raw, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return d.setUIStateValue(mediaIntegrityStateKey, string(raw))
}

// ---------------------------------------------------------------------------
// Tools and file check
// ---------------------------------------------------------------------------

// mediaIntegrityMissingTools reports which of the two tools is not on PATH.
func mediaIntegrityMissingTools() []string {
	missing := make([]string, 0, 2)
	for _, tool := range []string{"ffprobe", "ffmpeg"} {
		if _, err := exec.LookPath(tool); err != nil {
			missing = append(missing, tool)
		}
	}
	return missing
}

// mediaIntegrityResolveFile turns an archived path into the video file to check:
// a folder is resolved to its largest video file, exactly like ProbeBest.
func mediaIntegrityResolveFile(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", errors.New("empty path")
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("cannot read the file: %w", err)
	}
	if info.Mode().IsRegular() {
		return path, nil
	}
	if !info.IsDir() {
		return "", errors.New("the path is neither a file nor a folder")
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return "", fmt.Errorf("cannot read the folder: %w", err)
	}
	bestPath := ""
	bestSize := int64(-1)
	for _, entry := range entries {
		candidate := filepath.Join(path, entry.Name())
		candidateInfo, statErr := os.Stat(candidate)
		if statErr != nil || !candidateInfo.Mode().IsRegular() {
			continue
		}
		extension := strings.TrimPrefix(strings.ToLower(filepath.Ext(entry.Name())), ".")
		if !videoExtensions[extension] {
			continue
		}
		if bestSize < 0 || candidateInfo.Size() > bestSize {
			bestSize = candidateInfo.Size()
			bestPath = candidate
		}
	}
	if bestPath == "" {
		return "", errors.New("the folder contains no video file")
	}
	return bestPath, nil
}

// mediaIntegrityLimitedBuffer keeps at most limit bytes of the tool output.
type mediaIntegrityLimitedBuffer struct {
	mu    sync.Mutex
	buf   bytes.Buffer
	limit int
}

func (b *mediaIntegrityLimitedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if room := b.limit - b.buf.Len(); room > 0 {
		if len(p) > room {
			_, _ = b.buf.Write(p[:room])
		} else {
			_, _ = b.buf.Write(p)
		}
	}
	return len(p), nil
}

func (b *mediaIntegrityLimitedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// mediaIntegrityDetail turns ffmpeg's multi-line stderr into one readable line.
func mediaIntegrityDetail(raw string) string {
	lines := strings.Split(strings.TrimSpace(raw), "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		kept = append(kept, line)
		if len(kept) == 4 {
			break
		}
	}
	if len(kept) == 0 {
		return ""
	}
	return miTruncateRunes(strings.Join(kept, " · "), mediaIntegrityDetailLimit)
}

// mediaIntegrityDecode decodes every stream of the file with ffmpeg without
// writing anything. It reports "ok", "damaged" or "canceled".
func mediaIntegrityDecode(parent context.Context, file string) (string, string) {
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, mediaIntegrityDecodeTimeout)
	defer cancel()
	command := exec.CommandContext(ctx, "ffmpeg",
		"-nostdin",
		"-hide_banner",
		"-nostats",
		"-v", "error",
		"-protocol_whitelist", "file,pipe,data,crypto",
		"-i", file,
		// Video and audio are decoded. Subtitle streams are not: the null
		// muxer has no subtitle encoder, and copying them would read the whole
		// file a second time. ffprobe already validates their presence.
		"-map", "0:v?",
		"-map", "0:a?",
		"-f", "null",
		"-",
	)
	stderr := &mediaIntegrityLimitedBuffer{limit: 64 * 1024}
	command.Stderr = stderr
	err := command.Run()
	detail := mediaIntegrityDetail(stderr.String())
	if ctx.Err() == context.DeadlineExceeded {
		// A timeout is not proof of damage: a very large file on a slow disk can
		// just be slow. It stays a problem to look at, but is not called broken.
		return "unreadable", fmt.Sprintf("the check did not finish within %s", mediaIntegrityDecodeTimeout)
	}
	if parent.Err() != nil {
		return "canceled", ""
	}
	if err != nil {
		if detail == "" {
			detail = err.Error()
		}
		return "damaged", detail
	}
	if detail != "" {
		return "damaged", detail
	}
	return "ok", ""
}

// mediaIntegrityCheckPath validates the container with ffprobe and then decodes
// the file with ffmpeg.
func mediaIntegrityCheckPath(ctx context.Context, path string) (string, string) {
	file, err := mediaIntegrityResolveFile(path)
	if err != nil {
		return "unreadable", err.Error()
	}
	if _, err := probeFileWithinContext(ctx, file, mediaIntegrityProbeTimeout); err != nil {
		if ctx != nil && ctx.Err() != nil {
			return "canceled", ""
		}
		return "damaged", "ffprobe: " + err.Error()
	}
	if ctx != nil && ctx.Err() != nil {
		return "canceled", ""
	}
	return mediaIntegrityDecode(ctx, file)
}

// ---------------------------------------------------------------------------
// Scan
// ---------------------------------------------------------------------------

// mediaIntegrityStatusFor reads the persisted state and the live job state.
func mediaIntegrityStatusFor(s *AppState) MediaIntegrityStatus {
	status := MediaIntegrityStatus{OK: true}
	status.Missing = mediaIntegrityMissingTools()
	status.Available = len(status.Missing) == 0
	if s.db != nil {
		state := s.db.LoadMediaIntegrityState()
		status.Done = state.Done
		status.Total = state.Total
		status.Checked = state.Checked
		status.Damaged = state.Damaged
		status.Cursor = state.Cursor
		status.Current = state.Current
		status.StartedAt = state.StartedAt
		status.FinishedAt = state.FinishedAt
		if problems, err := s.db.MediaIntegrityProblems(); err == nil {
			status.Problems = problems
			// The panel shows how many files are damaged now, not only the
			// ones found again in this pass: an unchanged damaged file is
			// skipped but still listed.
			status.Damaged = len(problems)
		}
	}
	if s.jobs != nil {
		for _, job := range s.jobs.List(mediaIntegrityJobKind, "") {
			if !job.finished() {
				status.Running = true
				break
			}
		}
	}
	return status
}

// beginMediaIntegrityScan initialises (or resumes) the run and starts the job.
// It returns the existing job when one is already running.
func beginMediaIntegrityScan(s *AppState, restart bool) (*Job, bool, error) {
	job, created := s.jobs.Create(mediaIntegrityJobKind, mediaIntegrityJobKind)
	if !created {
		return job, false, nil
	}
	state := s.db.LoadMediaIntegrityState()
	switch {
	case restart:
		// Forget every outcome: the next pass probes the whole library.
		if err := s.db.ClearMediaIntegrityChecks(); err != nil {
			s.jobs.Fail(job.ID, err)
			return nil, false, err
		}
		state = MediaIntegrityState{Force: true}
	case state.Done:
		// A completed pass is over: start a new one that skips the files
		// already checked and unchanged.
		if err := s.db.PruneMediaIntegrityChecks(); err != nil {
			s.jobs.Fail(job.ID, err)
			return nil, false, err
		}
		state = MediaIntegrityState{Force: false}
	case state.Total == 0:
		// Fresh state, or an empty library: keep the force flag (a forced pass
		// must stay forced) and start from the beginning.
		if err := s.db.PruneMediaIntegrityChecks(); err != nil {
			s.jobs.Fail(job.ID, err)
			return nil, false, err
		}
		state.Cursor = ""
		state.Checked = 0
		state.Damaged = 0
		state.Done = false
		state.Current = ""
		state.FinishedAt = ""
	}
	if state.Total == 0 {
		total, err := s.db.CountMediaIntegrityTargets(state.Force)
		if err != nil {
			s.jobs.Fail(job.ID, err)
			return nil, false, err
		}
		state.Total = total
		state.StartedAt = time.Now().Format(time.RFC3339)
	}
	state.Running = true
	state.Done = false
	state.Current = ""
	state.FinishedAt = ""
	if err := s.db.SaveMediaIntegrityState(state); err != nil {
		s.jobs.Fail(job.ID, err)
		return nil, false, err
	}
	if err := s.jobs.Start(job.ID, func(ctx context.Context, jobID string) {
		report, err := runMediaIntegrityScan(ctx, s, jobID)
		if err != nil {
			s.jobs.Fail(jobID, err)
			return
		}
		s.jobs.SetResult(jobID, report)
	}); err != nil {
		s.jobs.Fail(job.ID, err)
		return nil, false, err
	}
	return job, true, nil
}

// runMediaIntegrityScan walks the library one file at a time, saving its
// position after every file.
func runMediaIntegrityScan(ctx context.Context, s *AppState, jobID string) (map[string]any, error) {
	state := s.db.LoadMediaIntegrityState()
	state.Running = true
	state.Done = false
	state.FinishedAt = ""
	if state.Total > 0 && state.Checked > state.Total {
		state.Total = state.Checked
	}
	if err := s.db.SaveMediaIntegrityState(state); err != nil {
		return nil, err
	}
	logging.Info("🧪 Media integrity check started: the library is verified one file at a time")
	startedAt := time.Now()
	for {
		if ctx.Err() != nil {
			break
		}
		targets, next, err := s.db.MediaIntegrityTargets(state.Cursor, 1, state.Force)
		if err != nil {
			return nil, err
		}
		if len(targets) == 0 {
			state.Done = true
			state.Cursor = next
			break
		}
		target := targets[0]
		state.Current = target.Label()
		if err := s.db.SaveMediaIntegrityState(state); err != nil {
			return nil, err
		}
		if jobID != "" && state.Total > 0 {
			s.jobs.SetProgress(jobID, float64(state.Checked)/float64(state.Total), target.Label())
		}
		status, detail := mediaIntegrityCheckPath(ctx, target.Path)
		if ctx.Err() != nil || status == "canceled" {
			break
		}
		check := MediaIntegrityProblem{
			TargetID: target.ID, Kind: target.Kind, Series: target.Series,
			MovieName: target.MovieName, Path: target.Path,
			Status: status, Detail: detail,
			SizeBytes: target.SizeBytes, ModifiedNS: target.ModifiedNS,
			CheckedAt: time.Now().Format(time.RFC3339),
		}
		if target.Season != nil {
			check.Season = *target.Season
		}
		if target.Episode != nil {
			check.Episode = *target.Episode
		}
		if target.MovieYear != nil {
			check.MovieYear = *target.MovieYear
		}
		if err := s.db.SaveMediaIntegrityCheck(check); err != nil {
			return nil, err
		}
		if status != "ok" {
			state.Damaged++
			logging.Warn(fmt.Sprintf("🧪 The file of «%s» looks damaged: %s", target.Label(), detail))
		}
		state.Checked++
		state.Cursor = next
		if err := s.db.SaveMediaIntegrityState(state); err != nil {
			return nil, err
		}
		if jobID != "" && state.Total > 0 {
			s.jobs.SetProgress(jobID, clampFloat(float64(state.Checked)/float64(state.Total), 0, 1), target.Label())
		}
		if next == "done" {
			state.Done = true
			break
		}
		if !sleepMediaIntegrity(ctx, mediaIntegrityPause) {
			break
		}
	}
	state.Running = false
	state.Current = ""
	state.FinishedAt = time.Now().Format(time.RFC3339)
	if err := s.db.SaveMediaIntegrityState(state); err != nil {
		return nil, err
	}
	report := map[string]any{
		"ok":            true,
		"checked":       state.Checked,
		"total":         state.Total,
		"damaged":       state.Damaged,
		"done":          state.Done,
		"canceled":      ctx.Err() != nil,
		"started_at":    state.StartedAt,
		"finished_at":   state.FinishedAt,
		"duration_secs": int64(time.Since(startedAt).Seconds()),
	}
	switch {
	case ctx.Err() != nil:
		logging.Info(fmt.Sprintf("🧪 Media integrity check stopped: %s checked, %s found damaged",
			countLabel(state.Checked, "file", "files"), countLabel(state.Damaged, "file", "files")))
	case state.Done:
		logging.Info(fmt.Sprintf("🧪 Media integrity check finished: %s checked, %s found damaged",
			countLabel(state.Checked, "file", "files"), countLabel(state.Damaged, "file", "files")))
	}
	return report, nil
}

// sleepMediaIntegrity waits for d, returning false when the job was canceled.
func sleepMediaIntegrity(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

// clampFloat keeps value inside [low, high].
func clampFloat(value, low, high float64) float64 {
	if value < low {
		return low
	}
	if value > high {
		return high
	}
	return value
}

// EpisodeCurrentRelease reconstructs the release stored on an episode row so a
// re-download can blocklist it and search for a different copy.
func (d *Database) EpisodeCurrentRelease(series string, season, episode int64) (models.Release, bool, error) {
	var title, magnet, hash string
	err := d.db.QueryRow(`SELECT COALESCE(e.title,''), COALESCE(e.magnet_link,''), COALESCE(e.magnet_hash,'')
		FROM episodes e JOIN series s ON s.id=e.series_id
		WHERE lower(s.name)=lower(?1) AND e.season=?2 AND e.episode=?3`,
		series, season, episode).Scan(&title, &magnet, &hash)
	if errors.Is(err, sql.ErrNoRows) {
		return models.Release{}, false, nil
	}
	if err != nil {
		return models.Release{}, false, err
	}
	release := models.Release{Title: title, Kind: "series", Magnet: magnet}
	if strings.TrimSpace(release.Magnet) == "" && strings.TrimSpace(hash) != "" {
		release.Magnet = "magnet:?xt=urn:btih:" + hash
	}
	seriesName, seasonValue, episodeValue := series, season, episode
	release.Series = &seriesName
	release.Season = &seasonValue
	release.Episode = &episodeValue
	return release, true, nil
}

// MovieCurrentRelease reconstructs the release stored on a movie row.
func (d *Database) MovieCurrentRelease(id int64) (models.Release, bool, error) {
	var title, magnet, hash string
	var year int64
	err := d.db.QueryRow(`SELECT COALESCE(NULLIF(title,''), COALESCE(name,'')), COALESCE(year,0),
		COALESCE(magnet_link,''), COALESCE(magnet_hash,'') FROM movies WHERE id=?1`, id).
		Scan(&title, &year, &magnet, &hash)
	if errors.Is(err, sql.ErrNoRows) {
		return models.Release{}, false, nil
	}
	if err != nil {
		return models.Release{}, false, err
	}
	release := models.Release{Title: title, Kind: "movie", Magnet: magnet}
	if strings.TrimSpace(release.Magnet) == "" && strings.TrimSpace(hash) != "" {
		release.Magnet = "magnet:?xt=urn:btih:" + hash
	}
	if year > 0 {
		release.Year = &year
	}
	return release, true, nil
}

// MovieProcessedPath returns the archived path of a movie, when known.
func (d *Database) MovieProcessedPath(id int64) (string, error) {
	var path string
	err := d.db.QueryRow(`SELECT COALESCE(t.processed_path,'') FROM movies m
		JOIN torrent_meta t ON lower(t.hash)=lower(m.magnet_hash) WHERE m.id=?1`, id).Scan(&path)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return path, err
}
