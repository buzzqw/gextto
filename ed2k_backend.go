package gextto

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/buzzqw/gextto/internal/amule"
	"github.com/buzzqw/gextto/internal/emuleqt"
)

// ed2k_backend.go is the one contract Gextto speaks to an eD2k core through.
// Two cores implement it: aMule, over its External Connections protocol
// (*amule.Client satisfies the interface as it is), and emule-qt, over its
// REST API (emuleqtBackend below, which maps the answers onto the same
// types). The rest of Gextto — search, Downloads, the eD2k page, the import,
// the cooperation with torrents — does not know which one is running.

// The cores Gextto can drive (ed2k_backend).
const (
	ed2kBackendAmule = "amule"
	// ed2kBackendEmuleqtManaged is emule-qt run by Gextto in Docker.
	ed2kBackendEmuleqtManaged = "emuleqt_managed"
	// ed2kBackendEmuleqt is an emule-qt core already running (a container
	// of its own, a NAS, another machine).
	ed2kBackendEmuleqt = "emuleqt"
)

// ed2kBackend is what Gextto asks of an eD2k core.
type ed2kBackend interface {
	Status(ctx context.Context) (amule.Status, error)
	Preferences(ctx context.Context) (amule.Preferences, error)
	SetLimits(ctx context.Context, downloadKiB, uploadKiB int64) error
	SetFilePreallocation(ctx context.Context, enabled bool) error

	Connect(ctx context.Context) error
	Disconnect(ctx context.Context) error
	ConnectServer(ctx context.Context, address string) error
	DisconnectServer(ctx context.Context) error
	StartKad(ctx context.Context) error
	StopKad(ctx context.Context) error
	Servers(ctx context.Context) ([]amule.Server, error)
	UpdateServers(ctx context.Context, url string) error
	UpdateNodes(ctx context.Context, url string) error

	Search(ctx context.Context, query string, options amule.SearchOptions) ([]amule.SearchResult, error)
	AddLink(ctx context.Context, link string, category int) error
	Downloads(ctx context.Context) ([]amule.Download, error)
	Pause(ctx context.Context, hash string) error
	Resume(ctx context.Context, hash string) error
	Delete(ctx context.Context, hash string) error
	SetPriority(ctx context.Context, hash string, priority int) error
	ClearCompleted(ctx context.Context) error

	SharedFiles(ctx context.Context) ([]amule.SharedFile, error)
	ReloadSharedFiles(ctx context.Context) error
	UploadQueue(ctx context.Context) ([]amule.UploadClient, error)
	LogLines(ctx context.Context) ([]string, error)

	Close() error
}

var _ ed2kBackend = (*amule.Client)(nil)
var _ ed2kBackend = (*emuleqtBackend)(nil)

// ed2kBackendKind is the configured core; anything unknown is aMule, the
// one Gextto drove before the choice existed.
func ed2kBackendKind(cfg *Config) string {
	if cfg == nil {
		return ed2kBackendAmule
	}
	switch strings.TrimSpace(cfg.Settings[settingEd2kBackend]) {
	case ed2kBackendEmuleqtManaged:
		return ed2kBackendEmuleqtManaged
	case ed2kBackendEmuleqt:
		return ed2kBackendEmuleqt
	}
	return ed2kBackendAmule
}

// ed2kUsesEmuleqt reports an emule-qt core, managed or not.
func ed2kUsesEmuleqt(cfg *Config) bool { return ed2kBackendKind(cfg) != ed2kBackendAmule }

// ed2kCoreName is the core's name for the log and the messages.
func ed2kCoreName(cfg *Config) string {
	if ed2kUsesEmuleqt(cfg) {
		return "emule-qt"
	}
	return "aMule"
}

// ed2kCoreAddress is where the core is reached, for the pages.
func ed2kCoreAddress(cfg *Config) string {
	switch ed2kBackendKind(cfg) {
	case ed2kBackendEmuleqt:
		address, _ := emuleqtExternalSettings(cfg)
		return address
	case ed2kBackendEmuleqtManaged:
		return "127.0.0.1:" + strconv.Itoa(emuleqtWebPort)
	}
	if settings, err := ed2kSettingsFromConfig(cfg); err == nil {
		return settings.Address
	}
	return ""
}

// errEd2kUnsupported is an operation the running core does not have.
var errEd2kUnsupported = errors.New("the eD2k core in use does not offer this operation")

// emuleqtBackend drives an emule-qt core through its REST API.
type emuleqtBackend struct {
	client *emuleqt.Client
	// containerData/hostData translate the paths of a core in a container
	// ("/data/incoming") into the paths Gextto sees; both empty for a core
	// that shares Gextto's filesystem.
	containerData string
	hostData      string

	mu       sync.Mutex
	dirs     emuleqt.Directories
	dirsRead time.Time
}

func newEmuleqtBackend(baseURL, apiKey, containerData, hostData string) *emuleqtBackend {
	return &emuleqtBackend{
		client:        emuleqt.New(emuleqt.Config{BaseURL: baseURL, APIKey: apiKey, Timeout: ed2kRequestTime}),
		containerData: strings.TrimRight(containerData, "/"),
		hostData:      strings.TrimRight(hostData, string(filepath.Separator)),
	}
}

// hostPath turns a path of the core into one Gextto can open.
func (b *emuleqtBackend) hostPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" || b.containerData == "" {
		return path
	}
	if path == b.containerData {
		return b.hostData
	}
	if rest, ok := strings.CutPrefix(path, b.containerData+"/"); ok {
		return filepath.Join(b.hostData, filepath.FromSlash(rest))
	}
	return path
}

// directories returns the core's folders as Gextto sees them, read again
// at most once a minute.
func (b *emuleqtBackend) directories(ctx context.Context) (emuleqt.Directories, error) {
	b.mu.Lock()
	if !b.dirsRead.IsZero() && time.Since(b.dirsRead) < time.Minute {
		dirs := b.dirs
		b.mu.Unlock()
		return dirs, nil
	}
	b.mu.Unlock()
	dirs, err := b.client.SharedDirectories(ctx)
	if err != nil {
		return emuleqt.Directories{}, err
	}
	dirs.Incoming = b.hostPath(dirs.Incoming)
	for i := range dirs.Temp {
		dirs.Temp[i] = b.hostPath(dirs.Temp[i])
	}
	for i := range dirs.Shared {
		dirs.Shared[i] = b.hostPath(dirs.Shared[i])
	}
	b.mu.Lock()
	b.dirs, b.dirsRead = dirs, time.Now()
	b.mu.Unlock()
	return dirs, nil
}

func (b *emuleqtBackend) Status(ctx context.Context) (amule.Status, error) {
	snap, err := b.client.Snapshot(ctx)
	if err != nil {
		return amule.Status{}, err
	}
	status := amule.Status{
		Version:        "emule-qt " + snap.Version,
		Ed2kConnected:  snap.Ed2k.Connected,
		Ed2kConnecting: snap.Ed2k.Connecting,
		HighID:         snap.Ed2k.Connected && !snap.Ed2k.LowID,
		ServerAddress:  snap.Ed2k.Server,
		KadRunning:     snap.Kad.Running,
		KadConnected:   snap.Kad.Connected,
		KadFirewalled:  snap.Kad.Firewalled,
		DownloadRate:   int64(snap.Rates.DownKiBps * 1024),
		UploadRate:     int64(snap.Rates.UpKiBps * 1024),
		UploadQueue:    snap.Uploads.Waiting,
		SharedFiles:    snap.SharedFiles,
	}
	if connection, err := b.client.Connection(ctx); err == nil {
		status.Ed2kID = uint32(connection.ClientID)
		if connection.CurrentServer != nil {
			status.ServerName = connection.CurrentServer.Name
		}
	}
	if stats, err := b.client.Stats(ctx); err == nil {
		status.SessionRecv, status.SessionSent = stats.ReceivedBytes, stats.SentBytes
	}
	if prefs, err := b.client.Preferences(ctx); err == nil {
		status.DownloadLimit, status.UploadLimit = prefs.MaxDownload*1024, prefs.MaxUpload*1024
	}
	return status, nil
}

func (b *emuleqtBackend) Preferences(ctx context.Context) (amule.Preferences, error) {
	prefs, err := b.client.Preferences(ctx)
	if err != nil {
		return amule.Preferences{}, err
	}
	out := amule.Preferences{IncomingDir: b.hostPath(prefs.IncomingDir), MaxDownload: prefs.MaxDownload, MaxUpload: prefs.MaxUpload}
	if dirs, err := b.directories(ctx); err == nil {
		if dirs.Incoming != "" {
			out.IncomingDir = dirs.Incoming
		}
		if len(dirs.Temp) > 0 {
			out.TempDir = dirs.Temp[0]
		}
		out.SharedDirs = dirs.Shared
	}
	return out, nil
}

func (b *emuleqtBackend) SetLimits(ctx context.Context, downloadKiB, uploadKiB int64) error {
	return b.client.SetLimits(ctx, downloadKiB, uploadKiB)
}

func (b *emuleqtBackend) SetFilePreallocation(context.Context, bool) error {
	return errEd2kUnsupported
}

// Connect connects every network, like aMule's: an eD2k server and Kad.
func (b *emuleqtBackend) Connect(ctx context.Context) error {
	serverErr := b.client.Connect(ctx)
	kadErr := b.client.StartKad(ctx)
	if serverErr != nil && kadErr != nil {
		return serverErr
	}
	return nil
}

func (b *emuleqtBackend) Disconnect(ctx context.Context) error {
	serverErr := b.client.Disconnect(ctx)
	kadErr := b.client.StopKad(ctx)
	if serverErr != nil {
		return serverErr
	}
	return kadErr
}

func (b *emuleqtBackend) ConnectServer(ctx context.Context, address string) error {
	if strings.TrimSpace(address) == "" {
		return b.client.Connect(ctx)
	}
	return b.client.ConnectServer(ctx, address)
}

func (b *emuleqtBackend) DisconnectServer(ctx context.Context) error { return b.client.Disconnect(ctx) }
func (b *emuleqtBackend) StartKad(ctx context.Context) error         { return b.client.StartKad(ctx) }
func (b *emuleqtBackend) StopKad(ctx context.Context) error          { return b.client.StopKad(ctx) }

func (b *emuleqtBackend) Servers(ctx context.Context) ([]amule.Server, error) {
	servers, err := b.client.Servers(ctx)
	if err != nil {
		return nil, err
	}
	var current string
	if connection, err := b.client.Connection(ctx); err == nil && connection.Connected && connection.CurrentServer != nil {
		current = fmt.Sprintf("%s:%d", connection.CurrentServer.Address, connection.CurrentServer.Port)
	}
	out := make([]amule.Server, 0, len(servers))
	for _, server := range servers {
		address := fmt.Sprintf("%s:%d", server.Address, server.Port)
		out = append(out, amule.Server{
			Name: server.Name, Address: address, Description: server.Description, Version: server.Version,
			Users: server.Users, Files: server.Files, Ping: server.Ping, Failed: server.Failed,
			Static: server.Static, Connected: address == current,
		})
	}
	return out, nil
}

func (b *emuleqtBackend) UpdateServers(ctx context.Context, url string) error {
	return b.client.ImportServers(ctx, url)
}

func (b *emuleqtBackend) UpdateNodes(ctx context.Context, url string) error {
	return b.client.ImportNodes(ctx, url)
}

// emuleqtSearchType maps aMule's search types onto emule-qt's.
func emuleqtSearchType(value uint8) string {
	switch value {
	case amule.SearchKad:
		return emuleqt.SearchKad
	case amule.SearchGlobal:
		return emuleqt.SearchGlobal
	case amule.SearchLocal:
		return emuleqt.SearchServer
	}
	return emuleqt.SearchAutomatic
}

func (b *emuleqtBackend) Search(ctx context.Context, query string, options amule.SearchOptions) ([]amule.SearchResult, error) {
	results, err := b.client.Search(ctx, query, emuleqt.SearchOptions{
		Type: emuleqtSearchType(options.Type), MinSize: int64(options.MinSize), MaxSize: int64(options.MaxSize),
		Wait: options.Wait, Poll: options.Poll,
	})
	if err != nil {
		return nil, err
	}
	out := make([]amule.SearchResult, 0, len(results))
	for _, result := range results {
		out = append(out, amule.SearchResult{
			Hash: strings.ToLower(result.Hash), Name: result.FileName, Size: result.FileSize,
			Sources: result.Sources, CompleteSources: result.CompleteSources,
		})
	}
	return out, nil
}

func (b *emuleqtBackend) AddLink(ctx context.Context, link string, category int) error {
	return b.client.AddLink(ctx, link, category)
}

// emuleqtStatus maps emule-qt's file states onto aMule's codes.
func emuleqtStatus(download emuleqt.Download) int {
	if download.Paused {
		return amule.StatusPaused
	}
	switch strings.ToLower(download.Status) {
	case "empty":
		return amule.StatusEmpty
	case "waitingforhash":
		return amule.StatusWaitingHash
	case "hashing":
		return amule.StatusHashing
	case "error":
		return amule.StatusError
	case "insufficient":
		return amule.StatusInsufficient
	case "paused":
		return amule.StatusPaused
	case "completing":
		return amule.StatusCompleting
	case "complete":
		return amule.StatusComplete
	}
	return amule.StatusReady
}

// emuleqtPriority maps emule-qt's priority onto aMule's value (auto adds 10,
// as aMule reports it).
func emuleqtPriority(name string, auto bool) int {
	value := amule.PriorityNormal
	switch strings.ToLower(name) {
	case "verylow", "low":
		value = amule.PriorityLow
	case "high", "veryhigh":
		value = amule.PriorityHigh
	}
	if auto {
		value += 10
	}
	return value
}

func (b *emuleqtBackend) Downloads(ctx context.Context) ([]amule.Download, error) {
	downloads, err := b.client.Downloads(ctx)
	if err != nil {
		return nil, err
	}
	var parts map[string]int
	if dirs, err := b.directories(ctx); err == nil {
		parts = ed2kPartFilesByHash(dirs.Temp)
	}
	out := make([]amule.Download, 0, len(downloads))
	for _, download := range downloads {
		hash := strings.ToLower(download.Hash)
		out = append(out, amule.Download{
			Hash: hash, Name: download.FileName, Size: download.FileSize,
			Done: download.CompletedSize, Transferred: download.CompletedSize, Speed: download.Datarate,
			Status: emuleqtStatus(download), Stopped: download.Stopped,
			Sources: download.Sources, Active: download.Transferring,
			Priority: emuleqtPriority(download.Priority, download.AutoPriority), Category: download.Category,
			Link:      amule.SearchResult{Hash: hash, Name: download.FileName, Size: download.FileSize}.Link(),
			PartMetID: parts[hash],
		})
	}
	return out, nil
}

func (b *emuleqtBackend) Pause(ctx context.Context, hash string) error {
	return b.client.Pause(ctx, hash)
}

func (b *emuleqtBackend) Resume(ctx context.Context, hash string) error {
	return b.client.Resume(ctx, hash)
}

func (b *emuleqtBackend) Delete(ctx context.Context, hash string) error {
	return b.client.Cancel(ctx, hash)
}

func (b *emuleqtBackend) SetPriority(ctx context.Context, hash string, priority int) error {
	return b.client.SetPriority(ctx, hash, amule.PriorityName(priority))
}

func (b *emuleqtBackend) ClearCompleted(ctx context.Context) error {
	return b.client.ClearCompleted(ctx)
}

// SharedFiles lists the shared files. emule-qt does not say which folder
// holds each one: a file present in the incoming folder is placed there,
// which is where Gextto looks for a finished download.
func (b *emuleqtBackend) SharedFiles(ctx context.Context) ([]amule.SharedFile, error) {
	files, err := b.client.SharedFiles(ctx)
	if err != nil {
		return nil, err
	}
	incoming := ""
	if dirs, err := b.directories(ctx); err == nil {
		incoming = dirs.Incoming
	}
	out := make([]amule.SharedFile, 0, len(files))
	for _, file := range files {
		shared := amule.SharedFile{Hash: strings.ToLower(file.Hash), Name: file.FileName, Size: file.FileSize}
		if incoming != "" {
			if info, err := os.Stat(filepath.Join(incoming, file.FileName)); err == nil && info.Size() == file.FileSize {
				shared.Dir = incoming
			}
		}
		out = append(out, shared)
	}
	return out, nil
}

func (b *emuleqtBackend) ReloadSharedFiles(ctx context.Context) error {
	return b.client.ReloadSharedFiles(ctx)
}

func (b *emuleqtBackend) UploadQueue(ctx context.Context) ([]amule.UploadClient, error) {
	clients, err := b.client.Uploads(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]amule.UploadClient, 0, len(clients))
	for _, client := range clients {
		out = append(out, amule.UploadClient{
			Name: client.UserName, Software: client.Software, IP: client.Addr, FileName: client.UploadFileName,
			UpSpeed: client.UpDatarate, Uploaded: client.SessionUp, UploadedAll: client.TransferredUp,
		})
	}
	return out, nil
}

// LogLines returns the core's newest log lines, oldest first.
func (b *emuleqtBackend) LogLines(ctx context.Context) ([]string, error) {
	records, err := b.client.Logs(ctx, 500)
	if err != nil {
		return nil, err
	}
	lines := make([]string, 0, len(records))
	for _, record := range records {
		stamp := ""
		switch value := record.Timestamp.(type) {
		case string:
			stamp = value
		case float64:
			stamp = time.UnixMilli(int64(value)).Format("2006-01-02 15:04:05")
			if value < 1e11 { // seconds, not milliseconds
				stamp = time.Unix(int64(value), 0).Format("2006-01-02 15:04:05")
			}
		}
		line := strings.TrimSpace(record.Message)
		if stamp != "" {
			line = stamp + "  " + line
		}
		lines = append(lines, line)
	}
	return lines, nil
}

func (b *emuleqtBackend) Close() error { return nil }

// ed2kPartFilesByHash maps the eD2k hash of every download in the temporary
// folders to its part number ("007.part" → 7). An eMule part file comes in
// two: NNN.part holds the data at its real offsets, NNN.part.met starts with
// a version byte, a 4-byte date and the 16-byte file hash — the layout
// eMule, aMule and emule-qt all write. It is how Gextto finds the data of a
// download the core does not name.
func ed2kPartFilesByHash(dirs []string) map[string]int {
	out := map[string]int{}
	for _, dir := range dirs {
		if strings.TrimSpace(dir) == "" {
			continue
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			name := entry.Name()
			number, ok := strings.CutSuffix(name, ".part.met")
			if !ok || entry.IsDir() {
				continue
			}
			id, err := strconv.Atoi(number)
			if err != nil || id <= 0 {
				continue
			}
			if hash := ed2kPartMetHash(filepath.Join(dir, name)); hash != "" {
				out[hash] = id
			}
		}
	}
	return out
}

// ed2kPartMetHash reads the file hash from a .part.met header, "" when the
// file is not one.
func ed2kPartMetHash(path string) string {
	file, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer file.Close()
	header := make([]byte, 21)
	if _, err := io.ReadFull(file, header); err != nil {
		return ""
	}
	switch header[0] {
	case 0xE0, 0xE2: // PARTFILE_VERSION, PARTFILE_VERSION_LARGEFILE
	default:
		return ""
	}
	return hex.EncodeToString(header[5:21])
}
