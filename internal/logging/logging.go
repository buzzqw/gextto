// Package logging provides the readable, rotating logger used across gextto.
//
// Each line uses `date time LEVEL message · key: value`: local time,
// right-aligned level and the user-facing message followed by structured fields.
package logging

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// Hashes identify torrent content internally, but add no useful context to the
// operator-facing log. Keep them out of every rendered message centrally so a
// new call site cannot accidentally expose one.
var torrentHashPattern = regexp.MustCompile(`(?i)\b[0-9a-f]{40,64}\b`)

// Level is a log severity.
type Level int

const (
	LevelDebug Level = iota
	LevelInfo
	LevelWarn
	LevelError
)

func (l Level) String() string {
	switch l {
	case LevelDebug:
		return "DEBUG"
	case LevelInfo:
		return "INFO"
	case LevelWarn:
		return "WARN"
	case LevelError:
		return "ERROR"
	default:
		return "INFO"
	}
}

var (
	logMu     sync.Mutex
	logWriter io.Writer = os.Stdout
	minLevel            = LevelInfo
	filePath  string
)

// Init configures the rotating file writer and returns a close function.
func Init(dir, base string, maxBytes int64, maxFiles int) func() {
	rw := NewRotatingWriter(dir, base, maxBytes, maxFiles)
	logMu.Lock()
	filePath = filepath.Join(dir, base)
	logWriter = rw
	logMu.Unlock()
	return func() {
		logMu.Lock()
		defer logMu.Unlock()
		_ = rw.Close()
	}
}

// FilePath returns the active log file path, if Init was called.
func FilePath() string {
	logMu.Lock()
	defer logMu.Unlock()
	return filePath
}

// SetLevel sets the minimum level from a tracing-style filter such as
// `gextto=debug` or `debug`.
func SetLevel(filter string) {
	level := LevelInfo
	lower := strings.ToLower(filter)
	switch {
	case strings.Contains(lower, "debug") || strings.Contains(lower, "trace"):
		level = LevelDebug
	case strings.Contains(lower, "warn"):
		level = LevelWarn
	case strings.Contains(lower, "error"):
		level = LevelError
	}
	logMu.Lock()
	minLevel = level
	logMu.Unlock()
}

// Enabled reports whether the given level is currently logged.
func Enabled(level Level) bool {
	logMu.Lock()
	defer logMu.Unlock()
	return level >= minLevel
}

func logf(level Level, message string, kv []any) {
	logMu.Lock()
	if level < minLevel {
		logMu.Unlock()
		return
	}
	writer := logWriter
	logMu.Unlock()

	var sb strings.Builder
	sb.WriteString(time.Now().Format("2006-01-02 15:04:05"))
	sb.WriteByte(' ')
	fmt.Fprintf(&sb, "%5s", level.String())
	sb.WriteByte(' ')
	sb.WriteString(redactTorrentHashes(message))
	for i := 0; i+1 < len(kv); i += 2 {
		key, _ := kv[i].(string)
		if key == "" {
			key = fmt.Sprint(kv[i])
		}
		if logFieldContainsTorrentHash(key) {
			continue
		}
		sb.WriteString(" · ")
		sb.WriteString(key)
		sb.WriteString(": ")
		sb.WriteString(redactTorrentHashes(formatValue(kv[i+1])))
	}
	sb.WriteByte('\n')

	line := sb.String()
	logMu.Lock()
	_, _ = io.WriteString(writer, line)
	logMu.Unlock()
}

func logFieldContainsTorrentHash(key string) bool {
	key = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(key), "_", ""))
	return strings.Contains(key, "hash") || strings.Contains(key, "magnet")
}

func redactTorrentHashes(value string) string {
	return torrentHashPattern.ReplaceAllString(value, "[redacted]")
}

func formatValue(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	case error:
		return v.Error()
	default:
		// Call sites routinely pass pointer fields (e.g. *int64 season, episode
		// and year). fmt.Sprint would render the memory address, so dereference
		// one level and print the pointed-to value; a nil pointer becomes empty.
		rv := reflect.ValueOf(value)
		if rv.Kind() == reflect.Ptr {
			if rv.IsNil() {
				return ""
			}
			return fmt.Sprint(rv.Elem().Interface())
		}
		return fmt.Sprint(v)
	}
}

// Debug logs a debug message.
func Debug(message string, kv ...any) { logf(LevelDebug, message, kv) }

// Info logs an info message.
func Info(message string, kv ...any) { logf(LevelInfo, message, kv) }

// Warn logs a warning message.
func Warn(message string, kv ...any) { logf(LevelWarn, message, kv) }

// Error logs an error message.
func Error(message string, kv ...any) { logf(LevelError, message, kv) }

// HumanBytes renders a byte size, e.g. `1.4 GB` / `742.3 MB` / `12.0 KB`.
func HumanBytes(bytes uint64) string {
	type unit struct {
		name string
		size float64
	}
	units := []unit{
		{"TB", 1024 * 1024 * 1024 * 1024},
		{"GB", 1024 * 1024 * 1024},
		{"MB", 1024 * 1024},
		{"KB", 1024},
		{"B", 1},
	}
	value := float64(bytes)
	for _, u := range units {
		if value >= u.size || u.name == "B" {
			scaled := value / u.size
			if u.name == "B" {
				return fmt.Sprintf("%.0f %s", scaled, u.name)
			}
			return fmt.Sprintf("%.1f %s", scaled, u.name)
		}
	}
	return "0 B"
}

// HumanBytesI64 is HumanBytes but tolerant of i64 values.
func HumanBytesI64(bytes int64) string {
	if bytes <= 0 {
		return "0 B"
	}
	return HumanBytes(uint64(bytes))
}

// HumanDuration renders a duration from whole seconds, e.g. `2h 05m`.
func HumanDuration(seconds int64) string {
	if seconds < 0 {
		seconds = 0
	}
	hours := seconds / 3600
	minutes := (seconds % 3600) / 60
	secs := seconds % 60
	switch {
	case hours > 0:
		return fmt.Sprintf("%dh %02dm", hours, minutes)
	case minutes > 0:
		return fmt.Sprintf("%dm %02ds", minutes, secs)
	default:
		return fmt.Sprintf("%ds", secs)
	}
}

// HumanRate renders a transfer rate, e.g. `8.3 MB/s`.
func HumanRate(bytesPerSecond int64) string {
	if bytesPerSecond <= 0 {
		return "0 B/s"
	}
	return HumanBytes(uint64(bytesPerSecond)) + "/s"
}

// SourceStat is the per-source outcome accumulated during a scrape cycle.
type SourceStat struct {
	OK        int
	Fail      int
	Results   int
	LastError string
}

func (s *SourceStat) recordOK(results int) {
	s.OK++
	s.Results += results
}

func (s *SourceStat) recordFail(err string) {
	s.Fail++
	s.LastError = err
}

var (
	sourceMu    sync.Mutex
	sourceStats = map[string]*SourceStat{}
)

func sourceKey(kind, name string) string { return kind + "\x01" + name }

// SourceOK records a successful source attempt with the number of releases.
func SourceOK(kind, name string, results int) {
	sourceMu.Lock()
	defer sourceMu.Unlock()
	stat := sourceStats[sourceKey(kind, name)]
	if stat == nil {
		stat = &SourceStat{}
		sourceStats[sourceKey(kind, name)] = stat
	}
	stat.recordOK(results)
}

// SourceFail records a failed source attempt.
func SourceFail(kind, name, err string) {
	sourceMu.Lock()
	defer sourceMu.Unlock()
	stat := sourceStats[sourceKey(kind, name)]
	if stat == nil {
		stat = &SourceStat{}
		sourceStats[sourceKey(kind, name)] = stat
	}
	stat.recordFail(err)
}

// SourceStatEntry is one drained per-source outcome.
type SourceStatEntry struct {
	Kind  string
	Name  string
	Stats SourceStat
}

// TakeSourceStats drains the accumulated per-source outcomes, sorted.
func TakeSourceStats() []SourceStatEntry {
	sourceMu.Lock()
	stats := sourceStats
	sourceStats = map[string]*SourceStat{}
	sourceMu.Unlock()

	var result []SourceStatEntry
	for key, stat := range stats {
		parts := strings.SplitN(key, "\x01", 2)
		if len(parts) != 2 {
			continue
		}
		result = append(result, SourceStatEntry{Kind: parts[0], Name: parts[1], Stats: *stat})
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Kind != result[j].Kind {
			return result[i].Kind < result[j].Kind
		}
		return result[i].Name < result[j].Name
	})
	return result
}

// RotatingWriter is a size-based rotating log writer: keeps maxFiles files of
// at most maxBytes each (gextto.log, gextto.log.1, ...).
type RotatingWriter struct {
	dir      string
	base     string
	maxBytes int64
	maxFiles int
	file     *os.File
	size     int64
	mu       sync.Mutex
}

// NewRotatingWriter creates a rotating writer.
func NewRotatingWriter(dir, base string, maxBytes int64, maxFiles int) *RotatingWriter {
	if maxBytes < 1024 {
		maxBytes = 1024
	}
	if maxFiles < 1 {
		maxFiles = 1
	}
	return &RotatingWriter{dir: dir, base: base, maxBytes: maxBytes, maxFiles: maxFiles}
}

func (w *RotatingWriter) path(index int) string {
	if index == 0 {
		return filepath.Join(w.dir, w.base)
	}
	return filepath.Join(w.dir, fmt.Sprintf("%s.%d", w.base, index))
}

func (w *RotatingWriter) open() error {
	if err := os.MkdirAll(w.dir, 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(w.path(0), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if info, err := file.Stat(); err == nil {
		w.size = info.Size()
	}
	w.file = file
	return nil
}

func (w *RotatingWriter) rotate() error {
	if w.file != nil {
		_ = w.file.Close()
		w.file = nil
	}
	if w.maxFiles <= 1 {
		_ = os.Remove(w.path(0))
		return w.open()
	}
	_ = os.Remove(w.path(w.maxFiles - 1))
	for index := w.maxFiles - 2; index >= 1; index-- {
		from := w.path(index)
		if _, err := os.Stat(from); err == nil {
			_ = os.Rename(from, w.path(index+1))
		}
	}
	current := w.path(0)
	if _, err := os.Stat(current); err == nil {
		_ = os.Rename(current, w.path(1))
	}
	return w.open()
}

// Write implements io.Writer.
func (w *RotatingWriter) Write(buf []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		if err := w.open(); err != nil {
			return 0, err
		}
	}
	if w.size >= w.maxBytes {
		if err := w.rotate(); err != nil {
			return 0, err
		}
	}
	written, err := w.file.Write(buf)
	w.size += int64(written)
	return written, err
}

// Close closes the underlying file.
func (w *RotatingWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file != nil {
		err := w.file.Close()
		w.file = nil
		return err
	}
	return nil
}
