package tui

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// Tests for the behaviour that matters over SSH: safe text, wrapping,
// incremental redraws, paste and confirmations.

func TestEnterNeverConfirmsDeletion(t *testing.T) {
	m := NewModel(NewTranslator("it"))
	m.Tab = TabTorrents
	m.SetTorrents(sampleTorrents())
	m.Update(runeKey('D'))
	if m.Confirm == nil {
		t.Fatal("D should ask for confirmation")
	}
	if action := m.Update(kindKey(KeyEnter)); action.Kind != ActionNone {
		t.Fatalf("Enter must not confirm a deletion, got %+v", action)
	}
	if m.Confirm != nil {
		t.Fatal("Enter should cancel the confirmation")
	}
}

func TestPasteNeverConfirms(t *testing.T) {
	m := NewModel(NewTranslator("en"))
	m.Tab = TabTorrents
	m.SetTorrents(sampleTorrents())
	m.Update(runeKey('d'))
	if action := m.Update(Key{Kind: KeyPaste, Text: "yyy"}); action.Kind != ActionNone || m.Confirm == nil {
		t.Fatalf("a paste must not answer a confirmation: action=%+v confirm=%+v", action, m.Confirm)
	}
}

func TestPasteMagnetOpensAddPrompt(t *testing.T) {
	m := NewModel(NewTranslator("en"))
	m.Tab = TabTorrents
	m.SetTorrents(sampleTorrents())
	magnet := "magnet:?xt=urn:btih:abcdef&dn=Some+Show"
	if action := m.Update(Key{Kind: KeyPaste, Text: magnet + "\n"}); action.Kind != ActionNone {
		t.Fatalf("paste should not act directly, got %+v", action)
	}
	if m.Prompt == nil || m.Prompt.Kind != PromptMagnet || m.Prompt.Buffer != magnet {
		t.Fatalf("paste should open a pre-filled magnet prompt, got %+v", m.Prompt)
	}
	if action := m.Update(kindKey(KeyEnter)); action.Kind != ActionAddMagnet || action.Text != magnet {
		t.Fatalf("Enter should add the pasted magnet, got %+v", action)
	}
	if m.Confirm != nil || len(m.Torrents) != len(sampleTorrents()) {
		t.Fatal("paste must not trigger torrent commands")
	}
}

func TestPasteOtherTextIsIgnored(t *testing.T) {
	m := NewModel(NewTranslator("en"))
	m.Tab = TabTorrents
	m.SetTorrents(sampleTorrents())
	if action := m.Update(Key{Kind: KeyPaste, Text: "dDXq"}); action.Kind != ActionNone || m.Confirm != nil {
		t.Fatalf("pasted letters must not run commands: %+v %+v", action, m.Confirm)
	}
	if m.Message != m.Tr.T("msg.pasteignored") {
		t.Fatalf("message = %q", m.Message)
	}
}

func TestPasteIntoPromptInsertsAtCursor(t *testing.T) {
	m := NewModel(NewTranslator("en"))
	m.Prompt = newPrompt(PromptSearch, "ab")
	m.Update(kindKey(KeyLeft))
	m.Update(Key{Kind: KeyPaste, Text: "X\nY\x1b[31m"})
	if m.Prompt.Buffer != "aX Yb" || m.Prompt.Cursor != 4 {
		t.Fatalf("prompt = %q cursor %d", m.Prompt.Buffer, m.Prompt.Cursor)
	}
}

func TestLongPromptKeepsCursorVisible(t *testing.T) {
	m := NewModel(NewTranslator("en"))
	m.Prompt = newPrompt(PromptMagnet, "magnet:?xt=urn:btih:"+strings.Repeat("a", 200))
	screen := m.Render(60, 20)
	last := screen.Lines[len(screen.Lines)-1].Text
	if StringWidth(last) > 60 || !strings.HasSuffix(last, "aaaa") {
		t.Fatalf("prompt line should show the end of the buffer: %q", last)
	}
	if screen.CursorCol >= 60 || screen.CursorCol < 50 {
		t.Fatalf("cursor column = %d", screen.CursorCol)
	}
}

func TestRenderSanitizesHostileNames(t *testing.T) {
	m := NewModel(NewTranslator("en"))
	m.Tab = TabTorrents
	m.SetTorrents([]Torrent{{Hash: "aaaa", Name: "x\x1b]52;c;ZXZpbA==\x07\x1b[2Jtitle", State: "downloading"}})
	for _, width := range []int{40, 60, 120} {
		for _, line := range m.Render(width, 20).Lines {
			if strings.ContainsAny(line.Text, "\x1b\x07") {
				t.Fatalf("escape sequence reached the screen at width %d: %q", width, line.Text)
			}
		}
	}
}

func TestRenderNeverExceedsWidthWithWideNames(t *testing.T) {
	m := NewModel(NewTranslator("it"))
	m.SetStatus(Status{Active: true})
	m.SetTorrents([]Torrent{
		{Hash: "aaaa", Name: "進撃の巨人 The Final Season 完結編 🎬 1080p WEB-DL", State: "downloading", Progress: 40, DownloadRate: 1000, TotalSize: 1000000, TotalDone: 400000},
		{Hash: "bbbb", Name: "한국 드라마 시리즈 에피소드 전체 모음", State: "seeding"},
	})
	m.SetLogs([]string{"2026-10-06 10:00:00 INFO 日本語のログ行 " + strings.Repeat("parola ", 30)})
	for _, tab := range []Tab{TabStatus, TabTorrents, TabLogs} {
		m.Tab = tab
		for _, width := range []int{30, 45, 60, 80, 132} {
			for _, line := range m.Render(width, 24).Lines {
				if StringWidth(line.Text) > width {
					t.Fatalf("tab %d width %d: line is %d cells: %q", tab, width, StringWidth(line.Text), line.Text)
				}
			}
		}
	}
}

func TestLogsWrapInsteadOfCutting(t *testing.T) {
	m := NewModel(NewTranslator("en"))
	m.Tab = TabLogs
	long := "2026-10-06 INFO " + strings.Repeat("word ", 30) + "END"
	m.SetLogs([]string{"first", long})
	screen := m.Render(40, 20)
	if !lineContains(screen, "END") || !lineContains(screen, "first") {
		t.Fatalf("wrapped log should be fully visible: %q", firstLines(screen, 14))
	}
}

func TestLogsWrapKeepNewestAtBottom(t *testing.T) {
	m := NewModel(NewTranslator("en"))
	m.Tab = TabLogs
	logs := []string{}
	for index := 0; index < 50; index++ {
		logs = append(logs, "entry "+strings.Repeat("x", index%3*30)+" #"+string(rune('A'+index%26)))
	}
	logs = append(logs, "NEWEST")
	m.SetLogs(logs)
	screen := m.Render(40, 12)
	contentEnd := len(screen.Lines) - 2 // the footer is the last row
	if !strings.Contains(screen.Lines[contentEnd].Text, "NEWEST") {
		t.Fatalf("newest log should be on the last content row: %q", firstLines(screen, 12))
	}
}

func TestStatusShowsRecentLogsAndDashboard(t *testing.T) {
	m := NewModel(NewTranslator("it"))
	next := time.Now().Add(10 * time.Minute).UTC().Format(time.RFC3339)
	started := time.Now().Add(-5 * time.Minute).UTC().Format(time.RFC3339)
	load := 0.42
	m.SetStatus(Status{Version: "1.2.3", Active: true, NextCycleAt: &next,
		TorrentStats: TorrentStats{Count: 3, Downloading: 1, Seeding: 2},
		LastCycle:    LastCycle{Scraped: 120, Candidates: 4, LastStartedAt: &started}})
	m.SetHealth(Health{Status: "ok", ProcessID: 42, ProcessUptimeSeconds: 7200, LoadAverage: &load,
		MemoryTotalBytes: 16 << 30, MemoryAvailableBytes: 8 << 30, DiskTotalBytes: 100 << 30, DiskFreeBytes: 5 << 30})
	m.SetTorrents([]Torrent{{Hash: "a", Name: "Serie S01E01", State: "downloading", Progress: 50, DownloadRate: 2048, TotalSize: 4096, TotalDone: 2048}})
	m.SetLogs([]string{"log uno", "log due", "log tre", "log quattro", "log cinque"})
	screen := m.Render(100, 30)
	for _, needle := range []string{"v1.2.3", "PID 42", "carico 0.42", "prossimo tra", "ultimo avvio", "Serie S01E01", "ETA", "disco pieno al 95%", "Log recenti", "log due", "log cinque"} {
		if !lineContains(screen, needle) {
			t.Errorf("status should contain %q: %q", needle, firstLines(screen, 30))
		}
	}
	if lineContains(screen, "log uno") {
		t.Error("status should show only the last four log lines")
	}
	// The log block is pinned at the bottom of the content area.
	if !strings.Contains(screen.Lines[len(screen.Lines)-2].Text, "log cinque") {
		t.Errorf("newest log should sit just above the footer: %q", screen.Lines[len(screen.Lines)-2].Text)
	}
}

func TestStatusScrollsOnSmallWindows(t *testing.T) {
	m := NewModel(NewTranslator("en"))
	m.SetStatus(Status{Active: true})
	m.SetHealth(Health{Status: "degraded", LastErrors: []string{strings.Repeat("problem ", 20)}})
	m.SetLogs([]string{"a", "b"})
	screen := m.Render(50, 12)
	if !lineContains(screen, "more lines") {
		t.Fatalf("a cut status page should say there is more: %q", firstLines(screen, 12))
	}
	m.Update(kindKey(KeyEnd))
	screen = m.Render(50, 12)
	if lineContains(screen, "more lines") || !lineContains(screen, "Active") {
		t.Fatalf("End should reach the bottom of the page: %q", firstLines(screen, 12))
	}
	m.Update(kindKey(KeyTab))
	m.Update(kindKey(KeyBackTab))
	if m.PageScroll != 0 {
		t.Fatalf("switching tab should reset the page scroll, got %d", m.PageScroll)
	}
}

func TestSelectedRowShowsFullTitle(t *testing.T) {
	m := NewModel(NewTranslator("en"))
	m.Tab = TabArchive
	m.Archive = []ArchiveEntry{
		{ID: 1, Title: "A very long release title that cannot fit in a narrow SSH window at all ENDMARK", Source: "rss"},
		{ID: 2, Title: "Another long release title that also cannot fit OTHERMARK", Source: "rss"},
	}
	screen := m.Render(40, 20)
	if !lineContains(screen, "ENDMARK") {
		t.Fatalf("selected entry should wrap to show its whole title: %q", firstLines(screen, 12))
	}
	if lineContains(screen, "OTHERMARK") {
		t.Fatalf("unselected entries stay on one row: %q", firstLines(screen, 12))
	}
}

func TestSelectedTorrentNameWrapsUnderNameColumn(t *testing.T) {
	m := NewModel(NewTranslator("en"))
	m.Tab = TabTorrents
	m.SetTorrents([]Torrent{{Hash: "aaaa", Name: "Show.Name.S01E01.1080p.WEB-DL.DDP5.1.H.264-GROUP.ENDMARK", State: "downloading"}})
	screen := m.Render(100, 20)
	if !lineContains(screen, "ENDMARK") {
		t.Fatalf("selected torrent name should wrap: %q", firstLines(screen, 8))
	}
}

func TestFrameRendererSendsOnlyChangedRows(t *testing.T) {
	var out bytes.Buffer
	renderer := newFrameRenderer(&out, false)
	screen := Screen{Lines: []Line{{Text: "one"}, {Text: "two"}, {Text: "three"}}}
	renderer.draw(screen, 20, 3)
	if !strings.Contains(out.String(), "\x1b[2J") || !strings.Contains(out.String(), "three") {
		t.Fatalf("first frame should clear and paint everything: %q", out.String())
	}
	out.Reset()
	if written := renderer.draw(screen, 20, 3); written != 0 || out.Len() != 0 {
		t.Fatalf("an identical frame should send nothing, sent %q", out.String())
	}
	screen.Lines[1].Text = "TWO"
	renderer.draw(screen, 20, 3)
	frame := out.String()
	if !strings.Contains(frame, "\x1b[2;1HTWO") || strings.Contains(frame, "one") || strings.Contains(frame, "three") {
		t.Fatalf("only row 2 should be sent: %q", frame)
	}
	if !strings.HasPrefix(frame, "\x1b[?2026h") || !strings.HasSuffix(frame, "\x1b[?2026l") {
		t.Fatalf("frame should be a synchronized update: %q", frame)
	}
	out.Reset()
	renderer.draw(screen, 30, 3) // resize
	if !strings.Contains(out.String(), "\x1b[2J") || !strings.Contains(out.String(), "one") {
		t.Fatalf("a resize should repaint everything: %q", out.String())
	}
	out.Reset()
	renderer.invalidate()
	renderer.draw(screen, 30, 3)
	if !strings.Contains(out.String(), "\x1b[2J") {
		t.Fatalf("invalidate (Ctrl-L) should repaint everything: %q", out.String())
	}
}

func TestFrameRendererSanitizesAndFolds(t *testing.T) {
	var out bytes.Buffer
	renderer := newFrameRenderer(&out, true)
	renderer.draw(Screen{Lines: []Line{{Text: "Modalità · \x1b]52;c;eA==\x07ok"}}}, 40, 1)
	frame := out.String()
	if strings.Contains(frame, "]52") || !strings.Contains(frame, "Modalita | ok") {
		t.Fatalf("frame = %q", frame)
	}
}

func TestOSC52MultiplexerPassthrough(t *testing.T) {
	env := func(values map[string]string) func(string) string {
		return func(key string) string { return values[key] }
	}
	var out bytes.Buffer
	_ = writeOSC52(&out, "hi", env(nil))
	if out.String() != "\x1b]52;c;aGk=\x07" {
		t.Fatalf("plain OSC52 = %q", out.String())
	}
	out.Reset()
	_ = writeOSC52(&out, "hi", env(map[string]string{"TMUX": "/tmp/tmux-1000/default,1,0"}))
	if !strings.Contains(out.String(), "\x1bPtmux;\x1b\x1b]52;c;aGk=\x07\x1b\\") {
		t.Fatalf("tmux passthrough missing: %q", out.String())
	}
	out.Reset()
	_ = writeOSC52(&out, "hi", env(map[string]string{"STY": "1234.pts-0"}))
	if out.String() != "\x1bP\x1b]52;c;aGk=\x07\x1b\\" {
		t.Fatalf("screen passthrough = %q", out.String())
	}
}

func TestReadInputClosesOnEOF(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	ch := make(chan []byte, 4)
	done := make(chan struct{})
	go func() { readInput(context.Background(), reader, ch); close(done) }()
	_, _ = writer.Write([]byte("q"))
	writer.Close()
	select {
	case data := <-ch:
		if string(data) != "q" {
			t.Fatalf("data = %q", data)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("input not forwarded")
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("readInput kept running after end of file")
	}
	if _, ok := <-ch; ok {
		t.Fatal("channel should be closed at end of file")
	}
}

func TestEscapeDelayAndASCIIMode(t *testing.T) {
	if got := escapeDelay(""); got != 100*time.Millisecond {
		t.Fatalf("default delay = %v", got)
	}
	if got := escapeDelay("250"); got != 250*time.Millisecond {
		t.Fatalf("custom delay = %v", got)
	}
	if got := escapeDelay("99999"); got != 100*time.Millisecond {
		t.Fatalf("out of range delay = %v", got)
	}
	env := func(values map[string]string) func(string) string {
		return func(key string) string { return values[key] }
	}
	if !asciiMode(env(map[string]string{"LANG": "C"})) {
		t.Fatal("C locale should use ASCII")
	}
	if asciiMode(env(map[string]string{"LANG": "C", "GEXTTO_TUI_ASCII": "0"})) {
		t.Fatal("GEXTTO_TUI_ASCII=0 should force UTF-8")
	}
	if !asciiMode(env(map[string]string{"LANG": "it_IT.UTF-8", "GEXTTO_TUI_ASCII": "1"})) {
		t.Fatal("GEXTTO_TUI_ASCII=1 should force ASCII")
	}
}

func TestFrameRendererRewritesOnlyChangedTail(t *testing.T) {
	var out bytes.Buffer
	renderer := newFrameRenderer(&out, false)
	screen := Screen{Lines: []Line{{Text: "torrent name          1.2 MB/s"}}}
	renderer.draw(screen, 40, 1)
	out.Reset()
	screen.Lines[0].Text = "torrent name          1.5 MB/s"
	renderer.draw(screen, 40, 1)
	frame := out.String()
	if !strings.Contains(frame, "\x1b[1;25H5 MB/s") || strings.Contains(frame, "torrent") {
		t.Fatalf("only the changed tail should be sent: %q", frame)
	}
	out.Reset()
	screen.Lines[0].Text = "torrent name          9 B/s"
	renderer.draw(screen, 40, 1)
	if !strings.Contains(out.String(), "\x1b[K") {
		t.Fatalf("a shorter row must clear its old tail: %q", out.String())
	}
}

func TestFrameRendererScrollsMovedRegion(t *testing.T) {
	var out bytes.Buffer
	renderer := newFrameRenderer(&out, false)
	lines := func(first int) []Line {
		rows := []Line{{Text: "header"}}
		for index := first; index < first+10; index++ {
			rows = append(rows, Line{Text: fmt.Sprintf("log line number %03d with some text", index)})
		}
		return append(rows, Line{Text: "footer"})
	}
	renderer.draw(Screen{Lines: lines(0)}, 60, 12)
	full := out.Len()
	out.Reset()
	renderer.draw(Screen{Lines: lines(2)}, 60, 12) // two new log lines
	frame := out.String()
	if !strings.Contains(frame, "\x1b[2;11r") || strings.Count(frame, "log line number") != 2 {
		t.Fatalf("expected a region scroll and two new rows: %q", frame)
	}
	if out.Len()*3 > full {
		t.Fatalf("scrolled frame should be much smaller than a full one: %d vs %d", out.Len(), full)
	}
	// What the renderer believes is on screen must match the new frame.
	out.Reset()
	if renderer.draw(Screen{Lines: lines(2)}, 60, 12) != 0 {
		t.Fatalf("after scrolling the state should be in sync, sent %q", out.String())
	}
}

func TestBandwidthMeter(t *testing.T) {
	meter := &bandwidthMeter{}
	start := time.Unix(1000, 0)
	meter.sample(start, [trafficKinds]int64{})
	rates := meter.sample(start.Add(2*time.Second), [trafficKinds]int64{4096, 20, 6})
	if text := formatBandwidth(rates); text != "TUI ↓1.0KB/s ↑5B/s · 1.5 req/s" {
		t.Fatalf("meter = %q", text)
	}
	if compactBytes(15*1024) != "15KB" || compactBytes(3.5*1024*1024) != "3.5MB" {
		t.Fatalf("compactBytes = %q %q", compactBytes(15*1024), compactBytes(3.5*1024*1024))
	}
}

func TestFooterShowsBandwidthWhenRoom(t *testing.T) {
	m := NewModel(NewTranslator("en"))
	m.SetStatus(Status{Active: true})
	m.Bandwidth = "TUI ↓1.0KB/s ↑5B/s · 1.5 req/s"
	screen := m.Render(100, 20)
	footer := screen.Lines[len(screen.Lines)-1].Text
	if !strings.HasSuffix(footer, m.Bandwidth) || StringWidth(footer) != 100 {
		t.Fatalf("footer = %q", footer)
	}
	m.Message = strings.Repeat("long message ", 10)
	screen = m.Render(60, 20)
	if footer := screen.Lines[len(screen.Lines)-1].Text; strings.Contains(footer, "req/s") {
		t.Fatalf("the message should win over the meter: %q", footer)
	}
}

func TestClientCountsTraffic(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"active":true,"name":"` + strings.Repeat("x", 1000) + `"}`))
	}))
	defer server.Close()
	client := NewClient(server.URL)
	if _, err := client.Status(context.Background()); err != nil {
		t.Fatal(err)
	}
	sent, received, requests := client.Traffic()
	if sent <= 0 || received < 1000 || requests != 1 {
		t.Fatalf("traffic sent=%d received=%d requests=%d", sent, received, requests)
	}
}

func TestStatusLogBlockShowsWholeEntries(t *testing.T) {
	m := NewModel(NewTranslator("en"))
	m.SetStatus(Status{Active: true})
	m.SetLogs([]string{"old " + strings.Repeat("filler ", 40) + "OLDTAIL", "new entry"})
	screen := m.Render(50, 14)
	if lineContains(screen, "OLDTAIL") && !lineContains(screen, "old filler") {
		t.Fatalf("an entry must not start from its middle: %q", firstLines(screen, 14))
	}
	if !lineContains(screen, "new entry") {
		t.Fatalf("the newest entry must be shown: %q", firstLines(screen, 14))
	}
}
