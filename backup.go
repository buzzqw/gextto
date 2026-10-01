package gextto

import (
	"archive/zip"
	"crypto/rand"
	"database/sql"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/jlaffaye/ftp"

	"github.com/buzzqw/gextto/internal/logging"
)

// backupMu serializes in-process backups so a manual and a scheduled backup
// starting together cannot write the same archive.
var backupMu sync.Mutex

// databases are the SQLite databases that get a transaction-consistent
// snapshot.
var databases = []string{
	"gextto_series.db",
	"gextto_archive.db",
	"gextto_config.db",
	"gextto_comics.db",
}

// excludedEntries are names (files or folders) excluded from the backup:
// downloaded content and caches, not configuration. The backup must contain
// only configuration and databases.
var excludedEntries = []string{
	// Downloaded files (comics): content, not configuration.
	"comics",
	// Downloaded blocklist.
	"ipfilter.dat",
	// Scraper and feed cache/generated files.
	"gextto_magnet_cache.json",
	"gextto_magnet_feed.xml",
	// libtorrent session state (fastresume): excluded as before.
	"gextto_torrents_state",
}

// snapshotDatabase produces a consistent copy of a live database with the
// online backup API. Copying the file while the daemon writes can capture a
// torn or stale state (commits still only in the -wal); the backup API
// guarantees a transactionally consistent snapshot.
func snapshotDatabase(source, destination string) error {
	if parent := filepath.Dir(destination); parent != "" {
		if err := os.MkdirAll(parent, 0o755); err != nil {
			return err
		}
	}
	src, err := OpenSQLite(source)
	if err != nil {
		return err
	}
	defer src.Close()
	// SQLite's online backup equivalent: VACUUM INTO writes a compact,
	// transactionally consistent copy. Non bloccare il daemon: attendi i lock
	// invece di fallire (OpenSQLite sets a busy timeout).
	if _, err := src.Exec("VACUUM INTO ?", destination); err != nil {
		return err
	}
	// The destination file must be self-contained (no -wal).
	dst, err := sql.Open("sqlite", "file:"+destination)
	if err != nil {
		return err
	}
	defer dst.Close()
	if _, err := dst.Exec("PRAGMA journal_mode=DELETE;"); err != nil {
		return err
	}
	return nil
}

// verifyZipArchive reads every entry so a truncated archive or a bad entry CRC
// is rejected before it is published. Opening only the central directory would
// accept corruption in compressed entry data.
func verifyZipArchive(path string) error {
	reader, err := zip.OpenReader(path)
	if err != nil {
		return err
	}
	defer reader.Close()
	for _, file := range reader.File {
		entry, err := file.Open()
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(io.Discard, entry)
		closeErr := entry.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}

// addZipFile adds a file to the zip with standard deflate compression.
func addZipFile(writer *zip.Writer, name, path string) error {
	source, err := os.Open(path)
	if err != nil {
		return err
	}
	defer source.Close()
	entry, err := writer.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate})
	if err != nil {
		return err
	}
	_, err = io.Copy(entry, source)
	return err
}

// CreateSnapshot creates a zip archive of the configuration files plus
// transaction-consistent snapshots of the live databases, then prunes the
// existing archives keeping the newest `retain` ones.
func CreateSnapshot(dataDir, backupRoot string, retain int) (string, error) {
	// Serialize in-process backups: a manual and a scheduled backup starting
	// together must not write the same archive.
	if !backupMu.TryLock() {
		return "", fmt.Errorf("backup già in corso")
	}
	defer backupMu.Unlock()

	if err := os.MkdirAll(backupRoot, 0o755); err != nil {
		return "", err
	}
	// Readable, chronologically sortable name (server local time). A numeric
	// suffix is added only on the rare same-second collision so two backups in
	// the same second never overwrite each other.
	stamp := time.Now().Format("2006-01-02_15-04-05")
	destination := filepath.Join(backupRoot, "gextto-backup-"+stamp+".zip")
	for counter := 2; ; counter++ {
		if _, err := os.Stat(destination); os.IsNotExist(err) {
			break
		}
		destination = filepath.Join(backupRoot, fmt.Sprintf("gextto-backup-%s-%d.zip", stamp, counter))
	}
	// Write to a sibling temporary file on the same filesystem, then rename:
	// the published archive is always complete or absent, never truncated.
	tmpZip := destination + ".tmp"

	// Unique temporary folder so two backups starting together (e.g. a manual
	// and a scheduled one) do not collide.
	temp := filepath.Join(dataDir, ".gextto-backup-"+backupUUID())
	if info, err := os.Stat(temp); err == nil && info.IsDir() {
		_ = os.RemoveAll(temp)
	}
	if err := os.MkdirAll(temp, 0o755); err != nil {
		return "", err
	}
	// Always clean the scratch folder, including on every early return.
	defer func() { _ = os.RemoveAll(temp) }()
	type dbSnapshot struct {
		name string
		path string
	}
	var snapshots []dbSnapshot
	for _, name := range databases {
		source := filepath.Join(dataDir, name)
		info, err := os.Stat(source)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		snapshot := filepath.Join(temp, name)
		if err := snapshotDatabase(source, snapshot); err != nil {
			return "", fmt.Errorf("snapshot di %s fallito: %w", name, err)
		}
		snapshots = append(snapshots, dbSnapshot{name: name, path: snapshot})
	}

	// 2) Zip the non-DB files plus the consistent DB snapshots.
	file, err := os.Create(tmpZip)
	if err != nil {
		return "", err
	}
	archive := zip.NewWriter(file)
	fail := func(err error) (string, error) {
		_ = archive.Close()
		_ = file.Close()
		_ = os.Remove(tmpZip)
		return "", err
	}
	if err := addDirectory(archive, dataDir, dataDir, backupRoot, temp); err != nil {
		return fail(err)
	}
	for _, snapshot := range snapshots {
		if err := addZipFile(archive, snapshot.name, snapshot.path); err != nil {
			return fail(err)
		}
	}
	if err := archive.Close(); err != nil {
		_ = file.Close()
		_ = os.Remove(tmpZip)
		return "", err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		_ = os.Remove(tmpZip)
		return "", err
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(tmpZip)
		return "", err
	}

	// Verify the archive is readable before publishing it.
	if err := verifyZipArchive(tmpZip); err != nil {
		_ = os.Remove(tmpZip)
		return "", fmt.Errorf("verifica archivio fallita: %w", err)
	}
	if err := os.Rename(tmpZip, destination); err != nil {
		_ = os.Remove(tmpZip)
		return "", err
	}

	entries, err := os.ReadDir(backupRoot)
	if err != nil {
		return "", err
	}
	type backupFile struct {
		path    string
		modTime time.Time
	}
	var backups []backupFile
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if !info.Mode().IsRegular() || filepath.Ext(entry.Name()) != ".zip" {
			continue
		}
		backups = append(backups, backupFile{
			path:    filepath.Join(backupRoot, entry.Name()),
			modTime: info.ModTime(),
		})
	}
	// Sort by modification time: with legacy and new names mixed together the
	// name alone does not reflect the real chronological order.
	sort.Slice(backups, func(i, j int) bool {
		return backups[i].modTime.Before(backups[j].modTime)
	})
	keep := retain
	if keep < 1 {
		keep = 1
	}
	keepFrom := len(backups) - keep
	if keepFrom < 0 {
		keepFrom = 0
	}
	for _, old := range backups[:keepFrom] {
		if err := os.Remove(old.path); err != nil {
			return "", err
		}
	}
	return destination, nil
}

// CopySnapshot copies a snapshot to an external directory (e.g. a cloud-synced
// folder or a mounted remote), creating the destination if needed. It returns
// the copy path.
func CopySnapshot(archive, targetDir string) (string, error) {
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return "", err
	}
	name := filepath.Base(archive)
	if archive == "" || name == "." || name == ".." || name == string(filepath.Separator) {
		return "", fmt.Errorf("invalid backup filename")
	}
	destination := filepath.Join(targetDir, name)
	if err := copyFile(archive, destination); err != nil {
		return "", err
	}
	return destination, nil
}

func copyFile(source, destination string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	// Write a sibling .partial file and rename it into place: the destination is
	// always a complete copy or absent, never truncated by an interruption.
	tmp := destination + ".partial"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := out.Sync(); err != nil {
		_ = out.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, destination); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// ftpEndpoint appends the default FTP port when the user did not specify one
// (an `[ipv6]:port` literal already has one).
func ftpEndpoint(host string) string {
	host = strings.TrimSpace(host)
	var hasPort bool
	if rest, ok := strings.CutPrefix(host, "["); ok {
		hasPort = strings.Contains(rest, "]:")
	} else {
		hasPort = strings.Contains(host, ":")
	}
	if hasPort || host == "" {
		return host
	}
	return host + ":21"
}

// FtpTestReport is the outcome of a full FTP test (connect, login, CWD, upload
// and delete a probe file). Every step is logged and reported so the UI can
// show what worked.
type FtpTestReport struct {
	Host        string  `json:"host"`
	User        string  `json:"user"`
	Path        string  `json:"path"`
	TestFile    string  `json:"test_file"`
	Connected   bool    `json:"connected"`
	LoggedIn    bool    `json:"logged_in"`
	EnteredPath bool    `json:"entered_path"`
	Uploaded    bool    `json:"uploaded"`
	Deleted     bool    `json:"deleted"`
	Error       *string `json:"error"`
}

// OK reports whether the whole test completed without an error.
func (r FtpTestReport) OK() bool {
	return r.Error == nil
}

// TestFTP connects, logs in, enters the remote directory, uploads a small probe
// file and deletes it again — so a successful test proves the backup copy can
// work.
func TestFTP(host, user, password, remotePath string) FtpTestReport {
	report := FtpTestReport{
		Host: strings.TrimSpace(host),
		User: user,
		Path: strings.TrimSpace(remotePath),
	}
	logging.Info("FTP test: connecting", "host", report.Host, "user", report.User, "endpoint", ftpEndpoint(host))
	conn, err := ftp.Dial(ftpEndpoint(host))
	if err != nil {
		logging.Info("FTP test: connection failed", "host", report.Host, "error", err)
		report.Error = errorString(fmt.Sprintf("connessione fallita: %s", err))
		return report
	}
	if err := conn.Login(user, password); err != nil {
		logging.Info("FTP test: login failed", "host", report.Host, "user", report.User, "error", err)
		report.Error = errorString(fmt.Sprintf("login fallito: %s", err))
		_ = conn.Quit()
		return report
	}
	report.LoggedIn = true
	logging.Info("FTP test: login ok", "host", report.Host, "user", report.User)
	if report.Path != "" {
		if err := conn.ChangeDir(report.Path); err != nil {
			logging.Info("FTP test: remote path not accessible", "host", report.Host, "path", report.Path, "error", err)
			report.Error = errorString(fmt.Sprintf("cartella '%s': %s", report.Path, err))
			_ = conn.Quit()
			return report
		}
		report.EnteredPath = true
		logging.Info("FTP test: remote path ok", "host", report.Host, "path", report.Path)
	}
	testFile := fmt.Sprintf("gextto-test-%s.txt", backupUUID())
	report.TestFile = testFile
	payload := []byte("Gextto FTP test file\n")
	reader := &countingReader{reader: strings.NewReader(string(payload))}
	if err := conn.Stor(testFile, reader); err != nil {
		logging.Info("FTP test: probe upload failed", "host", report.Host, "path", report.Path, "remote", testFile, "error", err)
		report.Error = errorString(fmt.Sprintf("upload del file di prova fallito: %s", err))
		_ = conn.Quit()
		return report
	}
	report.Uploaded = true
	logging.Info("FTP test: probe file uploaded", "host", report.Host, "path", report.Path, "remote", testFile, "bytes", reader.count)
	if err := conn.Delete(testFile); err != nil {
		// The upload already proved write access; deletion is best-effort.
		logging.Info("FTP test: could not remove probe file", "host", report.Host, "remote", testFile, "error", err)
		report.Error = errorString(fmt.Sprintf("file di prova caricato ma non rimosso: %s", err))
	} else {
		report.Deleted = true
		logging.Info("FTP test: probe file removed", "host", report.Host, "remote", testFile)
	}
	_ = conn.Quit()
	logging.Info("FTP test completed", "host", report.Host, "path", report.Path, "ok", report.OK())
	return report
}

// UploadFTP uploads a snapshot archive to an FTP server.
func UploadFTP(archive, host, user, password, remotePath string) error {
	conn, err := ftp.Dial(ftpEndpoint(host))
	if err != nil {
		return fmt.Errorf("connessione FTP fallita: %w", err)
	}
	defer func() { _ = conn.Quit() }()
	if err := conn.Login(user, password); err != nil {
		return err
	}
	if strings.TrimSpace(remotePath) != "" {
		if err := conn.ChangeDir(remotePath); err != nil {
			return err
		}
	}
	name := filepath.Base(archive)
	if archive == "" || name == "." || name == ".." || name == string(filepath.Separator) {
		return fmt.Errorf("invalid backup filename")
	}
	source, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer source.Close()
	if err := conn.Stor(name, source); err != nil {
		return err
	}
	return conn.Quit()
}

// addDirectory walks the data directory and adds every configuration file to
// the archive, skipping symlinks, the backup/temp folders, the excluded entries
// and database/log artifacts (the databases are added separately as consistent
// snapshots).
func addDirectory(writer *zip.Writer, root, current, backupRoot, skip string) error {
	entries, err := os.ReadDir(current)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			continue
		}
		path := filepath.Join(current, entry.Name())
		if backupPathWithin(path, backupRoot) || backupPathWithin(path, skip) {
			continue
		}
		if containsString(excludedEntries, entry.Name()) {
			continue
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		name := filepath.ToSlash(relative)
		if info.IsDir() {
			if err := addDirectory(writer, root, path, backupRoot, skip); err != nil {
				return err
			}
			continue
		}
		// Databases are added separately as consistent snapshots.
		if strings.HasSuffix(name, ".db") ||
			strings.HasSuffix(name, "-wal") ||
			strings.HasSuffix(name, "-shm") ||
			strings.HasSuffix(name, ".log") ||
			strings.Contains(name, ".log.") {
			continue
		}
		if err := addZipFile(writer, name, path); err != nil {
			return err
		}
	}
	return nil
}

// backupPathWithin reports whether path is root or nested under it, mirroring
// the component-based Path::starts_with.
func backupPathWithin(path, root string) bool {
	if root == "" {
		return false
	}
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)))
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func errorString(value string) *string {
	return &value
}

// countingReader counts the bytes read from the underlying reader.
type countingReader struct {
	reader io.Reader
	count  int64
}

func (c *countingReader) Read(buffer []byte) (int, error) {
	n, err := c.reader.Read(buffer)
	c.count += int64(n)
	return n, err
}

// backupUUID returns a random RFC 4122 version-4 UUID (hyphenated lowercase),
// matching uuid::Uuid::new_v4().
func backupUUID() string {
	var buffer [16]byte
	if _, err := rand.Read(buffer[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	buffer[6] = (buffer[6] & 0x0f) | 0x40
	buffer[8] = (buffer[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x",
		buffer[0:4], buffer[4:6], buffer[6:8], buffer[8:10], buffer[10:16])
}
