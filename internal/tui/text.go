package tui

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// Text that reaches the terminal comes from the daemon, and through it from
// RSS feeds, trackers and file names. Everything in this file measures text in
// terminal cells (not runes or bytes) and removes control sequences, so a
// hostile or merely odd title can neither break the layout nor talk to the
// terminal (set the clipboard, retitle the window, move the cursor).

// Sanitize removes ANSI escape sequences and control characters. Tabs become a
// space and line breaks a visible separator, so one entry stays one line.
func Sanitize(value string) string {
	if !needsSanitize(value) {
		return value
	}
	var builder strings.Builder
	builder.Grow(len(value))
	for index := 0; index < len(value); {
		r, size := utf8.DecodeRuneInString(value[index:])
		switch {
		case r == 0x1b:
			index += escapeLength(value[index:])
			continue
		case r == 0x9b: // 8-bit CSI
			index += size + csiBodyLength(value[index+size:])
			continue
		case r == '\t':
			builder.WriteByte(' ')
		case r == '\n' || r == '\r':
			if index+size < len(value) && builder.Len() > 0 {
				builder.WriteString(" ")
			}
		case r == utf8.RuneError && size == 1:
			builder.WriteRune('?')
		case unicode.IsControl(r), r == 0x2028, r == 0x2029:
			// drop C0/C1 controls and Unicode line separators
		default:
			builder.WriteString(value[index : index+size])
		}
		index += size
	}
	return builder.String()
}

func needsSanitize(value string) bool {
	for _, r := range value {
		if r < 0x20 || (r >= 0x7f && r <= 0x9f) || r == utf8.RuneError || r == 0x2028 || r == 0x2029 {
			return true
		}
	}
	return false
}

// escapeLength returns how many bytes of value (which starts with ESC) form
// one escape sequence: CSI, OSC/DCS/APC/PM/SOS strings, or a two-byte escape.
func escapeLength(value string) int {
	if len(value) < 2 {
		return len(value)
	}
	switch value[1] {
	case '[':
		return 2 + csiBodyLength(value[2:])
	case ']', 'P', '_', '^', 'X':
		// String sequence: ends with BEL or ST (ESC \).
		for index := 2; index < len(value); index++ {
			if value[index] == 0x07 {
				return index + 1
			}
			if value[index] == 0x1b && index+1 < len(value) && value[index+1] == '\\' {
				return index + 2
			}
		}
		return len(value)
	default:
		return 2
	}
}

// csiBodyLength measures parameter, intermediate and final bytes of a CSI.
func csiBodyLength(value string) int {
	for index := 0; index < len(value); index++ {
		if value[index] >= 0x40 && value[index] <= 0x7e {
			return index + 1
		}
		if value[index] < 0x20 || value[index] > 0x7e {
			return index // malformed: stop at the first foreign byte
		}
	}
	return len(value)
}

// RuneWidth is the number of terminal cells a rune occupies: 0 for combining
// marks and zero-width characters, 2 for wide East Asian characters and emoji.
func RuneWidth(r rune) int {
	switch {
	case r < 0x20 || (r >= 0x7f && r < 0xa0):
		return 0
	case r < 0x300:
		return 1
	case r == 0x200b || r == 0x200c || r == 0x200d || r == 0x2060 || r == 0xfeff:
		return 0
	case unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Me, r) || unicode.Is(unicode.Cf, r):
		return 0
	case r >= 0xfe00 && r <= 0xfe0f: // variation selectors
		return 0
	case isWide(r):
		return 2
	default:
		return 1
	}
}

func isWide(r rune) bool {
	return (r >= 0x1100 && r <= 0x115f) || // Hangul Jamo
		(r >= 0x231a && r <= 0x231b) || (r >= 0x2329 && r <= 0x232a) ||
		(r >= 0x23e9 && r <= 0x23ec) || r == 0x23f0 || r == 0x23f3 ||
		(r >= 0x25fd && r <= 0x25fe) || (r >= 0x2614 && r <= 0x2615) ||
		(r >= 0x2648 && r <= 0x2653) || r == 0x267f || r == 0x2693 || r == 0x26a1 ||
		(r >= 0x26aa && r <= 0x26ab) || (r >= 0x26bd && r <= 0x26be) ||
		(r >= 0x26c4 && r <= 0x26c5) || r == 0x26ce || r == 0x26d4 || r == 0x26ea ||
		(r >= 0x26f2 && r <= 0x26f3) || r == 0x26f5 || r == 0x26fa || r == 0x26fd ||
		r == 0x2705 || (r >= 0x270a && r <= 0x270b) || r == 0x2728 || r == 0x274c ||
		r == 0x274e || (r >= 0x2753 && r <= 0x2755) || r == 0x2757 ||
		(r >= 0x2795 && r <= 0x2797) || r == 0x27b0 || r == 0x27bf ||
		(r >= 0x2b1b && r <= 0x2b1c) || r == 0x2b50 || r == 0x2b55 ||
		(r >= 0x2e80 && r <= 0x303e) || // CJK radicals, punctuation
		(r >= 0x3041 && r <= 0x33ff) || // Kana, CJK symbols
		(r >= 0x3400 && r <= 0x4dbf) || // CJK Ext A
		(r >= 0x4e00 && r <= 0x9fff) || // CJK Unified
		(r >= 0xa000 && r <= 0xa4cf) || // Yi
		(r >= 0xa960 && r <= 0xa97f) ||
		(r >= 0xac00 && r <= 0xd7a3) || // Hangul syllables
		(r >= 0xf900 && r <= 0xfaff) || // CJK compatibility
		(r >= 0xfe10 && r <= 0xfe19) || (r >= 0xfe30 && r <= 0xfe6f) ||
		(r >= 0xff00 && r <= 0xff60) || (r >= 0xffe0 && r <= 0xffe6) || // fullwidth
		(r >= 0x16fe0 && r <= 0x18cff) || (r >= 0x1b000 && r <= 0x1b2ff) ||
		r == 0x1f004 || r == 0x1f0cf || r == 0x1f18e || (r >= 0x1f191 && r <= 0x1f19a) ||
		(r >= 0x1f200 && r <= 0x1f251) ||
		(r >= 0x1f300 && r <= 0x1f64f) || // pictographs, emoticons
		(r >= 0x1f680 && r <= 0x1f6ff) || // transport
		(r >= 0x1f7e0 && r <= 0x1f7eb) ||
		(r >= 0x1f900 && r <= 0x1f9ff) || (r >= 0x1fa70 && r <= 0x1faff) ||
		(r >= 0x20000 && r <= 0x3fffd)
}

// StringWidth returns the number of terminal cells value occupies.
func StringWidth(value string) int {
	width := 0
	for _, r := range value {
		width += RuneWidth(r)
	}
	return width
}

// Shorten truncates a string to width cells, adding an ellipsis when needed.
func Shorten(value string, width int) string {
	if width <= 0 {
		return ""
	}
	if StringWidth(value) <= width {
		return value
	}
	if width == 1 {
		return "…"
	}
	var builder strings.Builder
	used := 0
	for _, r := range value {
		cells := RuneWidth(r)
		if used+cells > width-1 {
			break
		}
		builder.WriteRune(r)
		used += cells
	}
	return builder.String() + "…"
}

// PadRight pads a string with spaces to width cells (truncating if longer).
func PadRight(value string, width int) string {
	value = Shorten(value, width)
	padding := width - StringWidth(value)
	if padding <= 0 {
		return value
	}
	return value + strings.Repeat(" ", padding)
}

// PadLeft pads a string on the left to width cells.
func PadLeft(value string, width int) string {
	value = Shorten(value, width)
	padding := width - StringWidth(value)
	if padding <= 0 {
		return value
	}
	return strings.Repeat(" ", padding) + value
}

// WrapText breaks value into lines of at most width cells. It prefers to
// break at spaces and hard-breaks words longer than a line (URLs, paths,
// magnets). Continuation lines start with indent spaces, so a "label: value"
// row keeps its value aligned.
func WrapText(value string, width, indent int) []string {
	if width <= 0 {
		return []string{value}
	}
	if indent < 0 || indent > width/2 {
		indent = 0
	}
	if StringWidth(value) <= width {
		return []string{value}
	}
	prefix := strings.Repeat(" ", indent)
	lines := []string{}
	var current strings.Builder
	currentWidth := 0
	limit := width
	flush := func() {
		lines = append(lines, strings.TrimRight(current.String(), " "))
		current.Reset()
		current.WriteString(prefix)
		currentWidth = indent
		limit = width
	}
	words := strings.SplitAfter(value, " ")
	for _, word := range words {
		if word == "" {
			continue
		}
		wordWidth := StringWidth(word)
		trimmedWidth := StringWidth(strings.TrimRight(word, " "))
		if currentWidth+trimmedWidth <= limit {
			current.WriteString(word)
			currentWidth += wordWidth
			continue
		}
		if currentWidth > indent && trimmedWidth <= limit-indent {
			flush()
			if strings.TrimSpace(word) == "" {
				continue
			}
			current.WriteString(word)
			currentWidth += wordWidth
			continue
		}
		// The word does not fit on any line: split it across lines.
		for _, r := range word {
			cells := RuneWidth(r)
			if currentWidth+cells > limit {
				if r == ' ' {
					continue
				}
				flush()
			}
			current.WriteRune(r)
			currentWidth += cells
		}
	}
	if rest := strings.TrimRight(current.String(), " "); strings.TrimSpace(rest) != "" {
		lines = append(lines, rest)
	}
	if len(lines) == 0 {
		return []string{""}
	}
	return lines
}

// asciiReplacements maps the symbols the TUI draws to single ASCII cells, so
// the layout computed in UTF-8 keeps the same width in an ASCII terminal.
var asciiReplacements = map[rune]rune{
	'…': '~', '·': '|', '↑': '^', '↓': 'v', '←': '<', '→': '>', '─': '-', '│': '|',
	'▁': '_', '▂': '.', '▃': ':', '▄': '-', '▅': '=', '▆': '+', '▇': '*', '█': '#',
	'░': '.', '▒': ':', '▓': '#', '•': '*', '✓': 'v', '✗': 'x', '«': '<', '»': '>',
	'à': 'a', 'á': 'a', 'â': 'a', 'ä': 'a', 'è': 'e', 'é': 'e', 'ê': 'e', 'ë': 'e',
	'ì': 'i', 'í': 'i', 'î': 'i', 'ï': 'i', 'ò': 'o', 'ó': 'o', 'ô': 'o', 'ö': 'o',
	'ù': 'u', 'ú': 'u', 'û': 'u', 'ü': 'u', 'ñ': 'n', 'ç': 'c',
	'À': 'A', 'Á': 'A', 'È': 'E', 'É': 'E', 'Ì': 'I', 'Ò': 'O', 'Ù': 'U', 'Ü': 'U',
}

// ASCIIFold rewrites value for terminals without UTF-8. Every rune becomes
// exactly one cell (wide runes become two), keeping column alignment.
func ASCIIFold(value string) string {
	ascii := true
	for index := 0; index < len(value); index++ {
		if value[index] >= 0x80 {
			ascii = false
			break
		}
	}
	if ascii {
		return value
	}
	var builder strings.Builder
	for _, r := range value {
		switch {
		case r < 0x80:
			builder.WriteRune(r)
		case asciiReplacements[r] != 0:
			builder.WriteRune(asciiReplacements[r])
		default:
			switch RuneWidth(r) {
			case 0:
			case 2:
				builder.WriteString("??")
			default:
				builder.WriteByte('?')
			}
		}
	}
	return builder.String()
}

// LocaleIsUTF8 decides whether the terminal can show UTF-8, from the usual
// locale variables in POSIX priority order. With no locale at all it assumes
// UTF-8, which is what every modern terminal emulator speaks.
func LocaleIsUTF8(getenv func(string) string) bool {
	for _, name := range []string{"LC_ALL", "LC_CTYPE", "LANG"} {
		value := strings.TrimSpace(getenv(name))
		if value == "" {
			continue
		}
		lower := strings.ToLower(value)
		return strings.Contains(lower, "utf-8") || strings.Contains(lower, "utf8")
	}
	return true
}
