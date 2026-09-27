package tui

import "unicode/utf8"

// KeyKind classifies a decoded key press.
type KeyKind int

const (
	KeyNone KeyKind = iota
	KeyRune
	KeyUp
	KeyDown
	KeyLeft
	KeyRight
	KeyEnter
	KeyEsc
	KeyTab
	KeyBackTab
	KeyBackspace
	KeyPgUp
	KeyPgDn
	KeyHome
	KeyEnd
	KeyCtrlC
)

// Key is one decoded key.
type Key struct {
	Kind KeyKind
	Rune rune
}

// Printable reports whether the key is a printable character.
func (k Key) Printable() bool {
	return k.Kind == KeyRune && k.Rune >= 32
}

// KeyParser turns a raw terminal byte stream into keys, buffering incomplete
// escape sequences (an arrow key can be split across reads).
type KeyParser struct {
	buf []byte
}

// Feed consumes bytes and returns the keys that are complete. When flush is
// true a trailing lone Escape is emitted as KeyEsc.
func (p *KeyParser) Feed(data []byte, flush bool) []Key {
	p.buf = append(p.buf, data...)
	keys := []Key{}
	for len(p.buf) > 0 {
		key, consumed, ok := p.parseOne(flush)
		if !ok {
			break
		}
		p.buf = p.buf[consumed:]
		keys = append(keys, key)
	}
	return keys
}

func (p *KeyParser) parseOne(flush bool) (Key, int, bool) {
	b := p.buf
	if b[0] == 0x1b { // escape sequence
		if len(b) == 1 {
			if flush {
				return Key{Kind: KeyEsc}, 1, true
			}
			return Key{}, 0, false
		}
		if b[1] == '[' || b[1] == 'O' {
			// Need at least the third byte.
			if len(b) < 3 {
				return Key{}, 0, false
			}
			third := b[2]
			switch third {
			case 'A':
				return Key{Kind: KeyUp}, 3, true
			case 'B':
				return Key{Kind: KeyDown}, 3, true
			case 'C':
				return Key{Kind: KeyRight}, 3, true
			case 'D':
				return Key{Kind: KeyLeft}, 3, true
			case 'H':
				return Key{Kind: KeyHome}, 3, true
			case 'F':
				return Key{Kind: KeyEnd}, 3, true
			case 'Z':
				return Key{Kind: KeyBackTab}, 3, true
			}
			if third >= '0' && third <= '9' {
				// ESC [ N ~
				index := 2
				for index < len(b) && b[index] >= '0' && b[index] <= '9' {
					index++
				}
				if index >= len(b) {
					return Key{}, 0, false
				}
				if b[index] == '~' {
					code := string(b[2:index])
					consumed := index + 1
					switch code {
					case "1", "7":
						return Key{Kind: KeyHome}, consumed, true
					case "4", "8":
						return Key{Kind: KeyEnd}, consumed, true
					case "5":
						return Key{Kind: KeyPgUp}, consumed, true
					case "6":
						return Key{Kind: KeyPgDn}, consumed, true
					}
					return Key{}, consumed, true // unknown, drop
				}
				return Key{}, index, true
			}
			return Key{}, 3, true // unknown CSI
		}
		// Alt+key etc: drop the escape, treat the next byte as a key.
		return Key{}, 1, true
	}
	switch b[0] {
	case '\r', '\n':
		return Key{Kind: KeyEnter}, 1, true
	case 0x7f, 0x08:
		return Key{Kind: KeyBackspace}, 1, true
	case 0x09:
		return Key{Kind: KeyTab}, 1, true
	case 0x03:
		return Key{Kind: KeyCtrlC}, 1, true
	}
	if b[0] < 0x20 {
		return Key{}, 1, true // other control chars: ignore
	}
	runes, size := utf8.DecodeRune(b)
	if runes == utf8.RuneError && size == 1 && !utf8.FullRune(b) {
		if flush {
			return Key{}, 1, true
		}
		return Key{}, 0, false
	}
	return Key{Kind: KeyRune, Rune: runes}, size, true
}
