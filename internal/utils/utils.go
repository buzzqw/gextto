// Package utils provides shared helpers based on gextto's utils module.
package utils

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// regexCache is a global cache of compiled regexes.
var (
	regexMu    sync.RWMutex
	regexCache = map[string]*regexp.Regexp{}
)

// CachedRegex compiles (and caches) a regular expression.
func CachedRegex(pattern string) (*regexp.Regexp, error) {
	regexMu.RLock()
	compiled, ok := regexCache[pattern]
	regexMu.RUnlock()
	if ok {
		return compiled, nil
	}
	compiled, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}
	regexMu.Lock()
	if len(regexCache) < 8192 {
		regexCache[pattern] = compiled
	}
	regexMu.Unlock()
	return compiled, nil
}

// MustCachedRegex is CachedRegex for constant patterns.
func MustCachedRegex(pattern string) *regexp.Regexp {
	re, err := CachedRegex(pattern)
	if err != nil {
		panic(err)
	}
	return re
}

// ParseDateAny parses a date out of arbitrary listing text: relative Italian
// expressions and the common numeric formats.
func ParseDateAny(text string) (time.Time, bool) {
	if strings.TrimSpace(text) == "" {
		return time.Time{}, false
	}
	txt := strings.ToLower(text)
	now := time.Now().UTC()
	if strings.Contains(txt, "oggi") {
		return now, true
	}
	if strings.Contains(txt, "ieri") {
		return now.Add(-24 * time.Hour), true
	}
	if m := MustCachedRegex(`(\d+)\s*(minut[oi]|min|or[ae]|h|giorn[oi]|settiman[ae])\s*fa`).FindStringSubmatch(txt); m != nil {
		quantity, err := strconv.ParseInt(m[1], 10, 64)
		if err != nil {
			return time.Time{}, false
		}
		unit := m[2]
		var delta time.Duration
		switch {
		case strings.HasPrefix(unit, "min"):
			delta = time.Duration(quantity) * time.Minute
		case strings.HasPrefix(unit, "or") || unit == "h":
			delta = time.Duration(quantity) * time.Hour
		case strings.HasPrefix(unit, "settiman"):
			delta = time.Duration(quantity) * 7 * 24 * time.Hour
		default:
			delta = time.Duration(quantity) * 24 * time.Hour
		}
		return now.Add(-delta), true
	}
	if m := MustCachedRegex(`(20\d{2})[-/](\d{1,2})[-/](\d{1,2})`).FindStringSubmatch(txt); m != nil {
		y, _ := strconv.Atoi(m[1])
		mo, _ := strconv.Atoi(m[2])
		d, _ := strconv.Atoi(m[3])
		if date, ok := utcDate(y, mo, d); ok {
			return date, true
		}
	}
	if m := MustCachedRegex(`(\d{1,2})[-/](\d{1,2})[-/](20\d{2})`).FindStringSubmatch(txt); m != nil {
		d, _ := strconv.Atoi(m[1])
		mo, _ := strconv.Atoi(m[2])
		y, _ := strconv.Atoi(m[3])
		if date, ok := utcDate(y, mo, d); ok {
			return date, true
		}
	}
	return time.Time{}, false
}

func utcDate(year, month, day int) (time.Time, bool) {
	t := time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC)
	if t.Year() != year || int(t.Month()) != month || t.Day() != day {
		return time.Time{}, false
	}
	return t, true
}

// FlareSolverrLimiter is a small queue shared between RSS and web-search
// callers, because FlareSolverr is stateful and becomes unresponsive when many
// challenges are submitted at once.
var FlareSolverrLimiter = make(chan struct{}, 2)

// AcquireFlareSolverr blocks until a FlareSolverr slot is free.
func AcquireFlareSolverr() {
	FlareSolverrLimiter <- struct{}{}
}

// ReleaseFlareSolverr releases a FlareSolverr slot.
func ReleaseFlareSolverr() {
	select {
	case <-FlareSolverrLimiter:
	default:
	}
}

// AtomicWrite writes content to path atomically (tmp file + rename + fsync).
func AtomicWrite(path string, content []byte) error {
	tmp := path + ".tmp"
	file, err := os.Create(tmp)
	if err != nil {
		return fmt.Errorf("create %s: %w", tmp, err)
	}
	if _, err = file.Write(content); err != nil {
		file.Close()
		return fmt.Errorf("write %s: %w", tmp, err)
	}
	if err = file.Sync(); err != nil {
		file.Close()
		return fmt.Errorf("sync %s: %w", tmp, err)
	}
	if err = file.Close(); err != nil {
		return err
	}
	if err = os.Rename(tmp, path); err != nil {
		return fmt.Errorf("replace %s: %w", path, err)
	}
	if parent := filepath.Dir(path); parent != "" {
		if dir, err := os.Open(parent); err == nil {
			_ = dir.Sync()
			_ = dir.Close()
		}
	}
	return nil
}

const (
	magnetHashV1 = iota
	magnetHashV2
)

// ParsedMagnetHash returns the canonical hash used internally for a magnet.
func ParsedMagnetHash(magnet string) (string, bool) {
	parsed, err := url.Parse(magnet)
	if err != nil {
		return "", false
	}
	var v2 string
	for _, value := range parsed.Query()["xt"] {
		lower := strings.ToLower(value)
		if strings.HasPrefix(lower, "urn:btih:") {
			value := strings.TrimPrefix(lower, "urn:btih:")
			var filtered strings.Builder
			for _, r := range value {
				if (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') {
					filtered.WriteRune(r)
				}
			}
			h := filtered.String()
			if len(h) == 40 || len(h) == 32 {
				return h, true
			}
		} else if strings.HasPrefix(lower, "urn:btmh:") {
			value := strings.TrimPrefix(lower, "urn:btmh:")
			digest, ok := strings.CutPrefix(value, "1220")
			if !ok {
				continue
			}
			if len(digest) == 64 && isHex(digest) {
				v2 = digest
			}
		}
	}
	if v2 != "" {
		return v2, true
	}
	return "", false
}

func isHex(value string) bool {
	for _, r := range value {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
			return false
		}
	}
	return true
}

// MagnetHash extracts the canonical infohash from a magnet.
func MagnetHash(magnet string) (string, bool) {
	return ParsedMagnetHash(magnet)
}

// ParseTimestamp parses the timestamp formats used in the databases: SQLite
// datetime and RFC3339.
func ParseTimestamp(value string) (time.Time, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, false
	}
	if t, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return t.UTC(), true
	}
	for _, layout := range []string{"2006-01-02 15:04:05", "2006-01-02T15:04:05"} {
		if t, err := time.ParseInLocation(layout, value, time.UTC); err == nil {
			return t.UTC(), true
		}
	}
	return time.Time{}, false
}

// ParseSizeMB parses sizes like "4.5 GB" / "700 MiB" into megabytes. Returns 0
// when no size is found.
func ParseSizeMB(text string) float64 {
	if strings.TrimSpace(text) == "" {
		return 0
	}
	re := MustCachedRegex(`(?i)([\d.]+)\s*([KMGT])i?B`)
	m := re.FindStringSubmatch(text)
	if m == nil {
		return 0
	}
	value, _ := strconv.ParseFloat(m[1], 64)
	unit := strings.ToUpper(m[2])
	switch unit {
	case "K":
		return value / 1024.0
	case "M":
		return value
	case "G":
		return value * 1024.0
	case "T":
		return value * 1024.0 * 1024.0
	default:
		return value
	}
}

func encodeTracker(value string) string {
	var out strings.Builder
	for i := 0; i < len(value); i++ {
		b := value[i]
		if (b >= '0' && b <= '9') || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') ||
			b == '/' || b == ':' || b == '?' || b == '.' || b == '_' || b == '-' || b == '~' {
			out.WriteByte(b)
		} else {
			fmt.Fprintf(&out, "%%%02X", b)
		}
	}
	return out.String()
}

// SanitizeMagnet normalises a magnet: keeps the canonical hash, a display name
// and the announce trackers (sorted and de-duplicated).
func SanitizeMagnet(input string, fallbackTitle *string) (string, bool) {
	input = strings.TrimSpace(input)
	if !strings.HasPrefix(input, "magnet:?") {
		return "", false
	}
	parsed, err := url.Parse(input)
	if err != nil {
		return "", false
	}
	kind, hash, ok := parsedMagnetKind(input)
	if !ok {
		return "", false
	}
	var out string
	if kind == magnetHashV2 {
		out = "magnet:?xt=urn:btmh:1220" + hash
	} else {
		out = "magnet:?xt=urn:btih:" + hash
	}
	name := ""
	query := parsed.Query()
	for _, dn := range query["dn"] {
		name = dn
		break
	}
	if name == "" && fallbackTitle != nil {
		name = *fallbackTitle
	}
	if name != "" {
		out += "&dn=" + url.QueryEscape(name)
	}
	trackers := query["tr"]
	var filtered []string
	for _, t := range trackers {
		if strings.TrimSpace(t) != "" {
			filtered = append(filtered, t)
		}
	}
	sort.Strings(filtered)
	filtered = dedup(filtered)
	for _, t := range filtered {
		out += "&tr=" + encodeTracker(t)
	}
	return out, true
}

func parsedMagnetKind(magnet string) (int, string, bool) {
	parsed, err := url.Parse(magnet)
	if err != nil {
		return 0, "", false
	}
	var v2 string
	for _, value := range parsed.Query()["xt"] {
		lower := strings.ToLower(value)
		if strings.HasPrefix(lower, "urn:btih:") {
			value := strings.TrimPrefix(lower, "urn:btih:")
			var filtered strings.Builder
			for _, r := range value {
				if (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') {
					filtered.WriteRune(r)
				}
			}
			h := filtered.String()
			if len(h) == 40 || len(h) == 32 {
				return magnetHashV1, h, true
			}
		} else if strings.HasPrefix(lower, "urn:btmh:") {
			value := strings.TrimPrefix(lower, "urn:btmh:")
			if digest, ok := strings.CutPrefix(value, "1220"); ok && len(digest) == 64 && isHex(digest) {
				v2 = digest
			}
		}
	}
	if v2 != "" {
		return magnetHashV2, v2, true
	}
	return 0, "", false
}

func dedup(values []string) []string {
	if len(values) == 0 {
		return values
	}
	out := values[:1]
	for _, value := range values[1:] {
		if value != out[len(out)-1] {
			out = append(out, value)
		}
	}
	return out
}

// RedactURLSecrets removes API keys from URLs embedded in error messages.
func RedactURLSecrets(input string) string {
	output := input
	searchFrom := 0
	for {
		if searchFrom >= len(output) {
			break
		}
		lower := strings.ToLower(output[searchFrom:])
		idx := strings.Index(lower, "apikey=")
		if idx < 0 {
			idx = strings.Index(lower, "api_key=")
		}
		if idx < 0 {
			break
		}
		keyStart := searchFrom + idx
		eq := strings.IndexByte(output[keyStart:], '=')
		if eq < 0 {
			break
		}
		valueStart := keyStart + eq + 1
		if valueStart >= len(output) {
			break
		}
		valueEnd := len(output)
		for i := valueStart; i < len(output); i++ {
			switch output[i] {
			case '&', ' ', ')', ']', '"':
				valueEnd = i
			}
			if valueEnd != len(output) {
				break
			}
		}
		if strings.EqualFold(output[valueStart:valueEnd], "[redacted]") {
			searchFrom = valueEnd
			continue
		}
		output = output[:valueStart] + "[redacted]" + output[valueEnd:]
		searchFrom = valueStart + len("[redacted]")
	}
	return output
}

// EnsureDir creates a directory tree.
func EnsureDir(path string) error {
	return os.MkdirAll(path, 0o755)
}

// StableID returns the SHA-1 hex digest of value.
func StableID(value string) string {
	sum := sha1.Sum([]byte(value))
	return hex.EncodeToString(sum[:])
}

// TorrentInfoHash returns the v1 infohash of a bencoded .torrent file.
func TorrentInfoHash(bytes []byte) (string, bool) {
	start, end, ok := bencodeInfoSpan(bytes)
	if !ok {
		return "", false
	}
	sum := sha1.Sum(bytes[start:end])
	return hex.EncodeToString(sum[:]), true
}

func bencodeValueEnd(bytes []byte, start int) (int, bool) {
	if start >= len(bytes) {
		return 0, false
	}
	switch {
	case bytes[start] == 'i':
		idx := indexByteFrom(bytes, start+1, 'e')
		if idx < 0 {
			return 0, false
		}
		return idx + 1, true
	case bytes[start] == 'l':
		index := start + 1
		for index < len(bytes) && bytes[index] != 'e' {
			next, ok := bencodeValueEnd(bytes, index)
			if !ok {
				return 0, false
			}
			index = next
		}
		if index >= len(bytes) {
			return 0, false
		}
		return index + 1, true
	case bytes[start] == 'd':
		index := start + 1
		for index < len(bytes) && bytes[index] != 'e' {
			next, ok := bencodeValueEnd(bytes, index)
			if !ok {
				return 0, false
			}
			next, ok = bencodeValueEnd(bytes, next)
			if !ok {
				return 0, false
			}
			index = next
		}
		if index >= len(bytes) {
			return 0, false
		}
		return index + 1, true
	case bytes[start] >= '0' && bytes[start] <= '9':
		colon := indexByteFrom(bytes, start, ':')
		if colon < 0 {
			return 0, false
		}
		length, err := strconv.Atoi(string(bytes[start:colon]))
		if err != nil {
			return 0, false
		}
		return colon + 1 + length, true
	default:
		return 0, false
	}
}

func indexByteFrom(bytes []byte, start int, target byte) int {
	for i := start; i < len(bytes); i++ {
		if bytes[i] == target {
			return i
		}
	}
	return -1
}

func bencodeInfoSpan(bytes []byte) (int, int, bool) {
	if len(bytes) == 0 || bytes[0] != 'd' {
		return 0, 0, false
	}
	index := 1
	for index < len(bytes) && bytes[index] != 'e' {
		keyStart := index
		keyEnd, ok := bencodeValueEnd(bytes, keyStart)
		if !ok {
			return 0, 0, false
		}
		valueStart := keyEnd
		valueEnd, ok := bencodeValueEnd(bytes, valueStart)
		if !ok {
			return 0, 0, false
		}
		if string(bytes[keyStart:keyEnd]) == "4:info" {
			return valueStart, valueEnd, true
		}
		index = valueEnd
	}
	return 0, 0, false
}

// CondensedKey lowercases and keeps only alphanumerics (legacy mfs_key).
func CondensedKey(value string) string {
	var out strings.Builder
	for _, r := range value {
		if (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			out.WriteRune(r)
		}
	}
	return strings.ToLower(out.String())
}

// ExtractCleanMovieName extracts the readable movie name from a release title.
func ExtractCleanMovieName(title string) string {
	cleaned := stripBrackets(title)
	cleaned = strings.NewReplacer(".", " ", "_", " ").Replace(cleaned)
	cleaned = strings.Join(strings.Fields(cleaned), " ")
	trimmed := func(value string) string {
		return strings.TrimSpace(strings.TrimRight(value, "(-._ "))
	}
	if found := MustCachedRegex(`\b(19\d{2}|20[012]\d)\b`).FindStringIndex(cleaned); found != nil {
		name := trimmed(cleaned[:found[0]])
		if len([]rune(name)) > 2 {
			return name
		}
	}
	techRe := `(?i)\b(2160p|1080p|720p|576p|480p|4k|uhd|blu[-\s]?ray|bluray|bdrip|dvdrip|dvdscr|dvd|webrip|web[-\s]?dl|webdl|web|hdtv|pdtv|ts|cam|hdrip|h[\.\s]?264|h[\.\s]?265|x264|x265|xvid|divx|hevc|avc|aac|ac3|ddp[57]|dd[57]\.?1|dts|truehd|flac|mp3|opus|ita|eng|multi|sub|subs|dub|hdr10\+|hdr10|hdr|dv|sdr|remux|proper|repack|extended|theatrical|mkv|mp4|avi|m4v)\b`
	if found := MustCachedRegex(techRe).FindStringIndex(cleaned); found != nil {
		name := trimmed(cleaned[:found[0]])
		if len([]rune(name)) > 2 {
			return name
		}
	}
	fallback := strings.TrimSpace(cleaned)
	if fallback == "" {
		return strings.TrimSpace(title)
	}
	return fallback
}

func stripBrackets(value string) string {
	var out strings.Builder
	depth := 0
	for _, r := range value {
		switch r {
		case '[', '{':
			depth++
		case ']', '}':
			if depth > 0 {
				depth--
			}
		default:
			if depth == 0 {
				out.WriteRune(r)
			}
		}
	}
	return out.String()
}

// NetworkInterface describes one network interface with IPv4 and type.
type NetworkInterface struct {
	Name string `json:"name"`
	IP   string `json:"ip"`
	Kind string `json:"kind"`
}

// ClassifyInterface classifies an interface from its name.
func ClassifyInterface(name string) string {
	lower := strings.ToLower(name)
	switch {
	case strings.HasPrefix(lower, "lo"):
		return "Loopback"
	case strings.HasPrefix(lower, "wl"),
		strings.HasPrefix(lower, "wlan"),
		strings.HasPrefix(lower, "wifi"),
		strings.HasPrefix(lower, "wi-fi"),
		strings.HasPrefix(lower, "airport"):
		return "WiFi"
	case strings.HasPrefix(lower, "tun"),
		strings.HasPrefix(lower, "wg"),
		strings.HasPrefix(lower, "ppp"),
		strings.HasPrefix(lower, "utun"),
		strings.HasPrefix(lower, "tailscale"),
		strings.HasPrefix(lower, "tap"):
		return "VPN"
	default:
		return "Ethernet"
	}
}

// NetworkInterfaces detects interfaces with IPv4 and type, excluding loopback.
func NetworkInterfaces() []NetworkInterface {
	ifaces, err := net.Interfaces()
	var result []NetworkInterface
	if err == nil {
		for _, iface := range ifaces {
			addrs, _ := iface.Addrs()
			ip := ""
			for _, addr := range addrs {
				var parsed net.IP
				switch v := addr.(type) {
				case *net.IPNet:
					parsed = v.IP
				case *net.IPAddr:
					parsed = v.IP
				}
				if parsed != nil && parsed.To4() != nil {
					ip = parsed.String()
					break
				}
			}
			if ip != "" {
				result = append(result, NetworkInterface{Name: iface.Name, IP: ip, Kind: ClassifyInterface(iface.Name)})
			}
		}
	}
	if result == nil {
		result = interfacesViaSysfs()
	}
	filtered := result[:0]
	for _, iface := range result {
		if iface.Kind != "Loopback" {
			filtered = append(filtered, iface)
		}
	}
	sort.Slice(filtered, func(i, j int) bool { return filtered[i].Name < filtered[j].Name })
	return filtered
}

func interfacesViaSysfs() []NetworkInterface {
	entries, err := os.ReadDir("/sys/class/net")
	if err != nil {
		return nil
	}
	var result []NetworkInterface
	for _, entry := range entries {
		result = append(result, NetworkInterface{Name: entry.Name(), Kind: ClassifyInterface(entry.Name())})
	}
	return result
}
