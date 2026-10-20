// Package ed2k parses and formats eD2k links (`ed2k://|file|...|/` and
// `ed2k://|server|...|/`). Transfers and searches go through aMule
// (internal/amule); this package only validates what the user pastes and
// what the core returns.
package ed2k

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
)

// FileLink is a parsed `ed2k://|file|...|...|...|/` link.
type FileLink struct {
	Name string
	Size int64
	Hash string // lowercase MD4, 32 hex characters
}

// Errors returned by the link parser.
var (
	ErrNotEd2kFile   = errors.New("not an eD2k file link")
	ErrBadEd2kHash   = errors.New("invalid eD2k MD4 hash")
	ErrBadEd2kSize   = errors.New("invalid eD2k file size")
	ErrBadEd2kName   = errors.New("invalid eD2k file name")
	ErrNotEd2kServer = errors.New("not an eD2k server link")
)

// ValidHash reports whether value is a 32-character hexadecimal MD4 hash.
func ValidHash(value string) bool {
	if len(value) != 32 {
		return false
	}
	for _, r := range value {
		switch {
		case r >= '0' && r <= '9':
		case r >= 'a' && r <= 'f':
		case r >= 'A' && r <= 'F':
		default:
			return false
		}
	}
	return true
}

// ParseFileLink accepts an `ed2k://|file|name|size|hash|/` link. The trailing
// part after the hash is tolerated (eMule writes `|/`), and the file name is
// percent-decoded when it contains escapes. A literal `+` stays a plus: eMule
// links encode spaces as `%20`, so turning `+` into a space would corrupt
// legitimate names.
func ParseFileLink(raw string) (FileLink, error) {
	raw = strings.TrimSpace(raw)
	if !strings.HasPrefix(strings.ToLower(raw), "ed2k://|file|") {
		return FileLink{}, ErrNotEd2kFile
	}
	rest := raw[len("ed2k://|file|"):]
	parts := strings.Split(rest, "|")
	if len(parts) < 3 {
		return FileLink{}, ErrNotEd2kFile
	}
	name, err := url.PathUnescape(parts[0])
	if err != nil {
		return FileLink{}, fmt.Errorf("%w: %v", ErrBadEd2kName, err)
	}
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 4096 || strings.ContainsAny(name, "\x00\r\n") {
		return FileLink{}, ErrBadEd2kName
	}
	size, err := strconv.ParseInt(strings.TrimSpace(parts[1]), 10, 64)
	if err != nil || size <= 0 {
		return FileLink{}, ErrBadEd2kSize
	}
	hash := strings.ToLower(strings.TrimSpace(parts[2]))
	if !ValidHash(hash) {
		return FileLink{}, ErrBadEd2kHash
	}
	return FileLink{Name: name, Size: size, Hash: hash}, nil
}

// String renders the canonical link. PathEscape keeps the name readable while
// still escaping `|`, which would otherwise break the link structure.
func (l FileLink) String() string {
	name := url.PathEscape(l.Name)
	// eMule uses `%20` for spaces; url.PathEscape leaves spaces as `%20` too,
	// but be explicit in case a future change alters that.
	name = strings.ReplaceAll(name, "+", "%2B")
	hash := strings.ToLower(l.Hash)
	return fmt.Sprintf("ed2k://|file|%s|%d|%s|/", name, l.Size, hash)
}

// ParseServerLink accepts `ed2k://|server|ip|port|/` and returns host:port.
func ParseServerLink(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if !strings.HasPrefix(strings.ToLower(raw), "ed2k://|server|") {
		return "", ErrNotEd2kServer
	}
	parts := strings.Split(raw[len("ed2k://|server|"):], "|")
	if len(parts) < 2 {
		return "", ErrNotEd2kServer
	}
	host := strings.TrimSpace(parts[0])
	port, err := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil || port < 1 || port > 65535 || host == "" {
		return "", ErrNotEd2kServer
	}
	if ip := net.ParseIP(host); ip == nil {
		return "", fmt.Errorf("%w: %q is not an IP address", ErrNotEd2kServer, host)
	}
	return net.JoinHostPort(host, strconv.Itoa(port)), nil
}

// NormalizeServerList splits a comma/newline separated list of `host:port`
// entries, dropping blanks and duplicates while preserving order.
func NormalizeServerList(raw string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, entry := range strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == '\n' || r == '\r' || r == ';'
	}) {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		if _, err := net.ResolveTCPAddr("tcp", entry); err != nil {
			continue
		}
		if _, ok := seen[entry]; ok {
			continue
		}
		seen[entry] = struct{}{}
		out = append(out, entry)
	}
	return out
}
