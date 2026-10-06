package tui

import "testing"

func TestKeyParserBasic(t *testing.T) {
	parser := &KeyParser{}
	keys := parser.Feed([]byte("a"), false)
	if len(keys) != 1 || keys[0].Kind != KeyRune || keys[0].Rune != 'a' {
		t.Fatalf("expected rune a, got %+v", keys)
	}
}

func TestKeyParserArrowsAndSpecial(t *testing.T) {
	cases := []struct {
		input string
		want  KeyKind
	}{
		{"\x1b[A", KeyUp},
		{"\x1b[B", KeyDown},
		{"\x1b[C", KeyRight},
		{"\x1b[D", KeyLeft},
		{"\x1bOA", KeyUp},
		{"\x1b[5~", KeyPgUp},
		{"\x1b[6~", KeyPgDn},
		{"\x1b[H", KeyHome},
		{"\x1b[F", KeyEnd},
		{"\x1b[1~", KeyHome},
		{"\x1b[4~", KeyEnd},
		{"\x1b[3~", KeyDelete},
		{"\x1b[Z", KeyBackTab},
		{"\r", KeyEnter},
		{"\n", KeyEnter},
		{"\x7f", KeyBackspace},
		{"\t", KeyTab},
		{"\x03", KeyCtrlC},
		{"\x01", KeyCtrlA},
		{"\x05", KeyCtrlE},
		{"\x0b", KeyCtrlK},
		{"\x15", KeyCtrlU},
		{"\x17", KeyCtrlW},
	}
	for _, tc := range cases {
		parser := &KeyParser{}
		keys := parser.Feed([]byte(tc.input), false)
		if len(keys) != 1 || keys[0].Kind != tc.want {
			t.Errorf("Feed(%q) = %+v, want kind %d", tc.input, keys, tc.want)
		}
	}
}

func TestKeyParserUTF8Rune(t *testing.T) {
	parser := &KeyParser{}
	keys := parser.Feed([]byte("è"), false)
	if len(keys) != 1 || keys[0].Rune != 'è' {
		t.Fatalf("expected UTF-8 rune è, got %+v", keys)
	}
}

func TestKeyParserBuffersPartialSequence(t *testing.T) {
	parser := &KeyParser{}
	if keys := parser.Feed([]byte("\x1b["), false); len(keys) != 0 {
		t.Fatalf("partial sequence should be buffered, got %+v", keys)
	}
	keys := parser.Feed([]byte("A"), false)
	if len(keys) != 1 || keys[0].Kind != KeyUp {
		t.Fatalf("completed sequence should decode to Up, got %+v", keys)
	}
}

func TestKeyParserLoneEscapeNeedsFlush(t *testing.T) {
	parser := &KeyParser{}
	if keys := parser.Feed([]byte("\x1b"), false); len(keys) != 0 {
		t.Fatalf("lone ESC should wait for flush, got %+v", keys)
	}
	keys := parser.Feed(nil, true)
	if len(keys) != 1 || keys[0].Kind != KeyEsc {
		t.Fatalf("flush should emit Esc, got %+v", keys)
	}
}

func TestKeyParserUTF8SplitAcrossReads(t *testing.T) {
	parser := &KeyParser{}
	encoded := []byte("à") // 0xC3 0xA0
	if keys := parser.Feed(encoded[:1], false); len(keys) != 0 {
		t.Fatalf("split rune should be buffered, got %+v", keys)
	}
	keys := parser.Feed(encoded[1:], false)
	if len(keys) != 1 || keys[0].Rune != 'à' {
		t.Fatalf("completed rune should decode, got %+v", keys)
	}
}

func TestKeyParserModifiedKeysDoNotLeak(t *testing.T) {
	cases := []struct {
		input string
		want  KeyKind
	}{
		{"\x1b[1;5A", KeyUp},    // Ctrl+Up
		{"\x1b[1;2B", KeyDown},  // Shift+Down
		{"\x1b[1;3C", KeyRight}, // Alt+Right
		{"\x1b[1;5H", KeyHome},
		{"\x1b[5;5~", KeyPgUp}, // Ctrl+PgUp
		{"\x1b[3;2~", KeyDelete},
	}
	for _, tc := range cases {
		parser := &KeyParser{}
		keys := parser.Feed([]byte(tc.input), false)
		if len(keys) != 1 || keys[0].Kind != tc.want {
			t.Errorf("Feed(%q) = %+v, want only kind %d", tc.input, keys, tc.want)
		}
	}
	// F-keys, focus events and mouse reports are dropped entirely.
	for _, input := range []string{"\x1b[15~", "\x1b[I", "\x1b[O", "\x1b[<0;10;5M", "\x1bOP"} {
		parser := &KeyParser{}
		if keys := parser.Feed([]byte(input), false); len(keys) != 0 {
			t.Errorf("Feed(%q) should produce no key, got %+v", input, keys)
		}
	}
}

func TestKeyParserCtrlL(t *testing.T) {
	parser := &KeyParser{}
	keys := parser.Feed([]byte{0x0c}, false)
	if len(keys) != 1 || keys[0].Kind != KeyCtrlL {
		t.Fatalf("expected Ctrl-L, got %+v", keys)
	}
}

func TestKeyParserBracketedPaste(t *testing.T) {
	parser := &KeyParser{}
	keys := parser.Feed([]byte("x\x1b[200~magnet:?xt=urn:btih:abc\x1b[201~y"), false)
	if len(keys) != 3 || keys[0].Rune != 'x' || keys[1].Kind != KeyPaste || keys[1].Text != "magnet:?xt=urn:btih:abc" || keys[2].Rune != 'y' {
		t.Fatalf("paste = %+v", keys)
	}
}

func TestKeyParserPasteSplitAcrossReads(t *testing.T) {
	parser := &KeyParser{}
	chunks := []string{"\x1b[20", "0~hello ", "wor", "ld\x1b[2", "01~"}
	var keys []Key
	for _, chunk := range chunks {
		keys = append(keys, parser.Feed([]byte(chunk), false)...)
		if len(keys) == 0 && parser.Pending() && chunk != "\x1b[20" {
			t.Fatalf("paste in progress must not count as a pending escape")
		}
	}
	// A flush in the middle of a paste must not cut it.
	if len(keys) != 1 || keys[0].Kind != KeyPaste || keys[0].Text != "hello world" {
		t.Fatalf("split paste = %+v", keys)
	}
}

func TestKeyParserPasteSurvivesFlush(t *testing.T) {
	parser := &KeyParser{}
	parser.Feed([]byte("\x1b[200~abc"), false)
	if keys := parser.Feed(nil, true); len(keys) != 0 {
		t.Fatalf("flush during paste emitted %+v", keys)
	}
	keys := parser.Feed([]byte("def\x1b[201~"), false)
	if len(keys) != 1 || keys[0].Text != "abcdef" {
		t.Fatalf("paste after flush = %+v", keys)
	}
}

func TestKeyParserFlushDropsIncompleteSequence(t *testing.T) {
	parser := &KeyParser{}
	parser.Feed([]byte("\x1b[1;5"), false)
	if !parser.Pending() {
		t.Fatal("incomplete CSI should be pending")
	}
	if keys := parser.Feed(nil, true); len(keys) != 0 {
		t.Fatalf("incomplete CSI should be dropped, got %+v", keys)
	}
	keys := parser.Feed([]byte("q"), false)
	if len(keys) != 1 || keys[0].Rune != 'q' {
		t.Fatalf("parser should recover, got %+v", keys)
	}
}

func TestKeyParserDoubleEscape(t *testing.T) {
	parser := &KeyParser{}
	keys := parser.Feed([]byte("\x1b\x1b"), false)
	keys = append(keys, parser.Feed(nil, true)...)
	if len(keys) != 2 || keys[0].Kind != KeyEsc || keys[1].Kind != KeyEsc {
		t.Fatalf("double Esc = %+v", keys)
	}
}
