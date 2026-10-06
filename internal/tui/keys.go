package tui

import (
	"strings"
	"unicode/utf8"
)

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
	KeyDelete
	KeyCtrlC
	KeyCtrlA
	KeyCtrlE
	KeyCtrlK
	KeyCtrlU
	KeyCtrlW
	KeyCtrlL
	// KeyPaste carries text pasted while bracketed paste mode is on.
	KeyPaste
)

// Key is one decoded key.
type Key struct {
	Kind KeyKind
	Rune rune
	// Text is the pasted text of a KeyPaste.
	Text string
}

// Printable reports whether the key is a printable character.
func (k Key) Printable() bool {
	return k.Kind == KeyRune && k.Rune >= 32
}

const (
	pasteEnd = "\x1b[201~"
	// maxPaste bounds a paste whose end marker never arrives.
	maxPaste = 64 * 1024
)

// KeyParser turns a raw terminal byte stream into keys, buffering incomplete
// escape sequences (an arrow key can be split across reads, especially over a
// slow SSH link) and collecting bracketed pastes into a single key.
type KeyParser struct {
	buf     []byte
	pasting bool
	paste   strings.Builder
}

// Pending reports whether bytes are buffered waiting for the rest of an
// escape sequence. The runner flushes them only after a quiet period, so a
// sequence split across network packets is not mistaken for Esc + letters.
func (p *KeyParser) Pending() bool { return len(p.buf) > 0 && !p.pasting }

// Feed consumes bytes and returns the keys that are complete. When flush is
// true a trailing lone Escape is emitted as KeyEsc and an incomplete escape
// sequence is dropped.
func (p *KeyParser) Feed(data []byte, flush bool) []Key {
	p.buf = append(p.buf, data...)
	keys := []Key{}
	for len(p.buf) > 0 {
		if p.pasting {
			key, done := p.consumePaste()
			if !done {
				break
			}
			keys = append(keys, key)
			continue
		}
		key, consumed, ok := p.parseOne(flush)
		if !ok {
			break
		}
		p.buf = p.buf[consumed:]
		if key.Kind != KeyNone {
			keys = append(keys, key)
		}
	}
	return keys
}

// consumePaste moves buffered bytes into the paste until the end marker.
func (p *KeyParser) consumePaste() (Key, bool) {
	if index := strings.Index(string(p.buf), pasteEnd); index >= 0 {
		p.paste.Write(p.buf[:index])
		p.buf = p.buf[index+len(pasteEnd):]
		return p.finishPaste(), true
	}
	// Keep a possible partial end marker in the buffer.
	keep := 0
	for size := min(len(p.buf), len(pasteEnd)-1); size > 0; size-- {
		if strings.HasPrefix(pasteEnd, string(p.buf[len(p.buf)-size:])) {
			keep = size
			break
		}
	}
	p.paste.Write(p.buf[:len(p.buf)-keep])
	p.buf = p.buf[len(p.buf)-keep:]
	if p.paste.Len() >= maxPaste {
		p.buf = nil
		return p.finishPaste(), true
	}
	return Key{}, false
}

func (p *KeyParser) finishPaste() Key {
	text := p.paste.String()
	if len(text) > maxPaste {
		text = text[:maxPaste]
	}
	p.paste.Reset()
	p.pasting = false
	return Key{Kind: KeyPaste, Text: text}
}

func (p *KeyParser) parseOne(flush bool) (Key, int, bool) {
	b := p.buf
	if b[0] == 0x1b {
		return p.parseEscape(flush)
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
	case 0x01:
		return Key{Kind: KeyCtrlA}, 1, true
	case 0x05:
		return Key{Kind: KeyCtrlE}, 1, true
	case 0x0b:
		return Key{Kind: KeyCtrlK}, 1, true
	case 0x0c:
		return Key{Kind: KeyCtrlL}, 1, true
	case 0x15:
		return Key{Kind: KeyCtrlU}, 1, true
	case 0x17:
		return Key{Kind: KeyCtrlW}, 1, true
	}
	if b[0] < 0x20 {
		return Key{}, 1, true // other control chars: ignore
	}
	r, size := utf8.DecodeRune(b)
	if r == utf8.RuneError && size == 1 {
		if !utf8.FullRune(b) && !flush {
			return Key{}, 0, false
		}
		return Key{}, 1, true
	}
	return Key{Kind: KeyRune, Rune: r}, size, true
}

func (p *KeyParser) parseEscape(flush bool) (Key, int, bool) {
	b := p.buf
	if len(b) == 1 {
		if flush {
			return Key{Kind: KeyEsc}, 1, true
		}
		return Key{}, 0, false
	}
	switch b[1] {
	case '[':
		return p.parseCSI(flush)
	case 'O':
		// SS3: ESC O <final>, sent by arrows in application mode and F1-F4.
		if len(b) < 3 {
			if flush {
				return Key{}, len(b), true
			}
			return Key{}, 0, false
		}
		return ss3Key(b[2]), 3, true
	case 0x1b:
		// Esc pressed twice: the first one is a real Esc.
		return Key{Kind: KeyEsc}, 1, true
	}
	// Alt+key: drop the escape, the next byte is read as an ordinary key.
	return Key{}, 1, true
}

// parseCSI decodes ESC [ <params 0x30-0x3F>* <intermediates 0x20-0x2F>* <final>.
// Modifier parameters (Ctrl/Shift/Alt + arrow: ESC [ 1 ; 5 A) are consumed as
// part of the sequence, so they can never leak out as typed characters.
func (p *KeyParser) parseCSI(flush bool) (Key, int, bool) {
	b := p.buf
	index := 2
	for index < len(b) && b[index] >= 0x30 && b[index] <= 0x3f {
		index++
	}
	for index < len(b) && b[index] >= 0x20 && b[index] <= 0x2f {
		index++
	}
	if index >= len(b) {
		if flush {
			return Key{}, len(b), true
		}
		return Key{}, 0, false
	}
	final := b[index]
	consumed := index + 1
	if final < 0x40 || final > 0x7e {
		// Malformed: drop what was read and resume at the foreign byte.
		return Key{}, index, true
	}
	params := string(b[2:index])
	first := params
	if cut := strings.IndexByte(params, ';'); cut >= 0 {
		first = params[:cut]
	}
	switch final {
	case 'A':
		return Key{Kind: KeyUp}, consumed, true
	case 'B':
		return Key{Kind: KeyDown}, consumed, true
	case 'C':
		return Key{Kind: KeyRight}, consumed, true
	case 'D':
		return Key{Kind: KeyLeft}, consumed, true
	case 'H':
		return Key{Kind: KeyHome}, consumed, true
	case 'F':
		return Key{Kind: KeyEnd}, consumed, true
	case 'Z':
		return Key{Kind: KeyBackTab}, consumed, true
	case '~':
		switch first {
		case "1", "7":
			return Key{Kind: KeyHome}, consumed, true
		case "4", "8":
			return Key{Kind: KeyEnd}, consumed, true
		case "5":
			return Key{Kind: KeyPgUp}, consumed, true
		case "6":
			return Key{Kind: KeyPgDn}, consumed, true
		case "3":
			return Key{Kind: KeyDelete}, consumed, true
		case "200":
			p.pasting = true
			p.paste.Reset()
			return Key{}, consumed, true
		}
	}
	return Key{}, consumed, true // unknown key (F-keys, focus, mouse): drop
}

func ss3Key(final byte) Key {
	switch final {
	case 'A':
		return Key{Kind: KeyUp}
	case 'B':
		return Key{Kind: KeyDown}
	case 'C':
		return Key{Kind: KeyRight}
	case 'D':
		return Key{Kind: KeyLeft}
	case 'H':
		return Key{Kind: KeyHome}
	case 'F':
		return Key{Kind: KeyEnd}
	}
	return Key{}
}
