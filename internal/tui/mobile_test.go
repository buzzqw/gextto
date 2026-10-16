package tui

import (
	"strings"
	"testing"
	"time"
)

// Tests for the compact layout used on phone-width terminals: an SSH session
// from a mobile client is typically only 40-60 columns wide, and the fixed
// columns of a desktop window would push the useful text off the row.

func TestCompactHeaderKeepsStateOnPhoneWidth(t *testing.T) {
	m := NewModel(NewTranslator("en"))
	next := time.Now().Add(5 * time.Minute).UTC().Format(time.RFC3339)
	m.SetStatus(Status{Version: "1.2.3", Active: true, NextCycleAt: &next})
	m.SetDaemonState(true, "")

	narrow := m.headerLine(40)
	if strings.Contains(narrow, "Gextto TUI") {
		t.Fatalf("the brand should give way on a phone header: %q", narrow)
	}
	for _, needle := range []string{"v1.2.3", "[ACTIVE]", "ONLINE"} {
		if !strings.Contains(narrow, needle) {
			t.Fatalf("compact header lost %q: %q", needle, narrow)
		}
	}
	if StringWidth(narrow) > 40 {
		t.Fatalf("compact header is wider than the terminal: %q", narrow)
	}
	if wide := m.headerLine(100); !strings.Contains(wide, "Gextto TUI") {
		t.Fatalf("the wide header should keep the title: %q", wide)
	}
}

func TestActiveTransferKeepsNameOnPhoneWidth(t *testing.T) {
	m := NewModel(NewTranslator("en"))
	m.SetStatus(Status{Active: true})
	m.SetTorrents([]Torrent{{Hash: "a", Name: "Show.S01E01.1080p.WEB-DL.ENDMARK", State: "downloading",
		Progress: 42, DownloadRate: 2048, TotalSize: 1000000, TotalDone: 420000}})
	screen := m.Render(40, 24)
	if !lineContains(screen, "ENDMARK") {
		t.Fatalf("the active torrent name should not be cut on a phone: %q", firstLines(screen, 24))
	}
	// The name and the figures are on separate rows, so the bar cannot push
	// the title off the line.
	if !lineContains(screen, "42%") || !lineContains(screen, "2.0 KB/s") {
		t.Fatalf("the active figures should sit below the name: %q", firstLines(screen, 24))
	}
}

func TestHealthLinesWrapOnPhoneWidth(t *testing.T) {
	m := NewModel(NewTranslator("en"))
	m.Tab = TabHealth
	cpu := 1.5
	m.SetHealth(Health{Status: "ok", ProcessID: 42, ProcessUptimeSeconds: 7200, ProcessCPUPercent: &cpu,
		ResidentBytes: 256 << 20, DiskFreeBytes: 5 << 30, DiskTotalBytes: 100 << 30,
		TrashFileCount: 3, TrashBytes: 1 << 20})
	screen := m.Render(40, 24)
	for _, needle := range []string{"PID 42", "RAM 256.0 MB", "100.0 GB"} {
		if !lineContains(screen, needle) {
			t.Fatalf("health lost %q on a phone: %q", needle, firstLines(screen, 24))
		}
	}
}

func TestDetailValueKeepsRoomOnPhoneWidth(t *testing.T) {
	m := NewModel(NewTranslator("en"))
	m.SetTorrents([]Torrent{{Hash: "aaaa", Name: "Show", State: "downloading"}})
	m.SetDetail(TorrentDetail{Torrent: Torrent{Hash: "aaaa", Name: "Show", State: "downloading", SavePath: "/mnt/media-xy/ENDMARK"}})
	// Scroll to the Save path row: the value is 21 cells wide, which only fits
	// once the label column has shrunk for the phone.
	m.DetailScroll = 21
	screen := m.Render(40, 24)
	if !lineContains(screen, "/mnt/media-xy/ENDMARK") {
		t.Fatalf("detail value cut on a phone: %q", firstLines(screen, 24))
	}
}

func TestEventsCompactOnPhoneWidth(t *testing.T) {
	m := NewModel(NewTranslator("en"))
	m.Overlay = OverlayEvents
	m.SetEvents([]Event{{Kind: "torrent_finished", Name: "Show.S01E01.ENDMARK", Hash: "aaaa"}})
	screen := m.Render(40, 24)
	if !lineContains(screen, "Show.S01E01.ENDMARK") {
		t.Fatalf("the kind column should not push the event name off the row: %q", firstLines(screen, 24))
	}
}

func TestLibraryBarKeepsCountsOnPhoneWidth(t *testing.T) {
	m := NewModel(NewTranslator("en"))
	m.Tab = TabLibrary
	m.SetLibrary([]SeriesLibraryItem{{Name: "S", Enabled: true}})
	m.SetLibrary([]MovieLibraryItem{{Name: "M", Enabled: true}})
	m.SetLibrary([]ComicLibraryItem{})
	screen := m.Render(40, 24)
	if !lineContains(screen, "(0)") {
		t.Fatalf("the library sub-tab bar should wrap on a phone: %q", firstLines(screen, 24))
	}
}

func TestTorrentsTitleWrapsOnPhoneWidth(t *testing.T) {
	m := NewModel(NewTranslator("en"))
	m.Tab = TabTorrents
	m.SetTorrents([]Torrent{{Hash: "a", Name: "Show", State: "downloading"}})
	m.Filter = strings.Repeat("z", 20) + "ENDMARK"
	screen := m.Render(40, 24)
	if !lineContains(screen, "ENDMARK") {
		t.Fatalf("the filter should stay readable on a phone: %q", firstLines(screen, 24))
	}
}

func TestStatusTrafficFirstOnPhoneWidth(t *testing.T) {
	m := NewModel(NewTranslator("en"))
	m.Tab = TabStatus
	cpu := 1.0
	m.SetStatus(Status{Version: "1.2.3", Active: true})
	m.SetHealth(Health{Status: "ok", ProcessCPUPercent: &cpu, DiskFreeBytes: 1 << 30, DiskTotalBytes: 1 << 31})
	m.SetMetrics(DashboardStats{TorrentStats: map[string]any{"download_rate": 1024.0, "upload_rate": 512.0}})
	m.SetLogs([]string{"2026-10-06 INFO hello"})

	// On a phone the traffic line is the first content row, so the download and
	// upload rates are visible without scrolling.
	narrow := m.Render(40, 24).Lines
	traffic, system := -1, -1
	for index, line := range narrow {
		if strings.Contains(line.Text, "Traffic:") {
			traffic = index
		}
		if strings.Contains(line.Text, "System:") {
			system = index
		}
	}
	if traffic < 0 || !strings.Contains(narrow[traffic].Text, "↓") || !strings.Contains(narrow[traffic].Text, "↑") {
		t.Fatalf("the traffic rates must be visible on a phone Status screen: %v", firstLines(Screen{Lines: narrow}, 24))
	}
	if system < 0 || traffic > system {
		t.Fatalf("traffic should come before the system rows on a phone (traffic=%d system=%d): %v", traffic, system, firstLines(Screen{Lines: narrow}, 24))
	}

	// On a wide terminal the order is unchanged: the dashboard rows first, the
	// traffic after the download row.
	wide := m.Render(100, 30).Lines
	traffic, system = -1, -1
	for index, line := range wide {
		if strings.Contains(line.Text, "Traffic:") {
			traffic = index
		}
		if strings.Contains(line.Text, "System:") {
			system = index
		}
	}
	if traffic < 0 || system < 0 || system > traffic {
		t.Fatalf("wide layout order changed (traffic=%d system=%d): %v", traffic, system, firstLines(Screen{Lines: wide}, 30))
	}
}

func TestPhoneWidthNeverOverflowsOnAnyTab(t *testing.T) {
	m := NewModel(NewTranslator("it"))
	cpu := 1.0
	m.SetStatus(Status{Version: "1.2.3", Active: true, TorrentStats: TorrentStats{Count: 2, Downloading: 1}})
	m.SetHealth(Health{Status: "ok", ProcessID: 1, ProcessCPUPercent: &cpu, ResidentBytes: 1 << 28,
		DiskFreeBytes: 1 << 30, DiskTotalBytes: 1 << 31,
		Paths: []PathCheck{{Label: "Download", Path: "/mnt/日本語/ダウンロード", Exists: true, Writable: true}},
		Disks: []DiskInfo{{Mount: "/media/日本語", FreeBytes: 1 << 30, TotalBytes: 1 << 31}}})
	m.SetTorrents([]Torrent{{Hash: "aaaa", Name: "進撃の巨人 🎬 1080p WEB-DL", State: "downloading",
		Progress: 40, DownloadRate: 1024, TotalSize: 1 << 20, TotalDone: 1 << 19}})
	m.SetLogs([]string{"2026-10-06 WARN 日本語のログ " + strings.Repeat("word ", 20)})
	for _, tab := range []Tab{TabStatus, TabTorrents, TabLogs, TabHealth, TabArchive, TabMissing, TabBlocklist, TabLibrary, TabMaintenance} {
		m.Tab = tab
		for _, width := range []int{30, 40, 50, 60} {
			for _, line := range m.Render(width, 20).Lines {
				if got := StringWidth(Sanitize(line.Text)); got > width {
					t.Fatalf("tab %d width %d: line is %d cells: %q", tab, width, got, line.Text)
				}
			}
		}
	}
}
