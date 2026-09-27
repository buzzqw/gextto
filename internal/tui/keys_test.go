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
		{"\x1b[Z", KeyBackTab},
		{"\r", KeyEnter},
		{"\n", KeyEnter},
		{"\x7f", KeyBackspace},
		{"\t", KeyTab},
		{"\x03", KeyCtrlC},
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
