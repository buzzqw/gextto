package logging

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHumanFormatters(t *testing.T) {
	cases := []struct {
		got, want string
	}{
		{HumanBytes(0), "0 B"},
		{HumanBytes(512), "512 B"},
		{HumanBytes(1536), "1.5 KB"},
		{HumanBytes(3 * 1024 * 1024 * 1024), "3.0 GB"},
		{HumanBytesI64(-5), "0 B"},
		{HumanDuration(0), "0s"},
		{HumanDuration(45), "45s"},
		{HumanDuration(192), "3m 12s"},
		{HumanDuration(7500), "2h 05m"},
		{HumanRate(0), "0 B/s"},
		{HumanRate(1024 * 1024), "1.0 MB/s"},
	}
	for _, tc := range cases {
		if tc.got != tc.want {
			t.Errorf("got %q, want %q", tc.got, tc.want)
		}
	}
}

func TestSourceStatsAccumulateAndDrain(t *testing.T) {
	_ = TakeSourceStats()
	SourceOK("feed", "example.org", 3)
	SourceOK("feed", "example.org", 2)
	SourceFail("indexer", "Prowlarr", "HTTP 500")
	stats := TakeSourceStats()
	if len(stats) != 2 {
		t.Fatalf("stats = %d, want 2", len(stats))
	}
	if stats[0].Kind != "feed" || stats[0].Stats.OK != 2 || stats[0].Stats.Results != 5 {
		t.Fatalf("feed stat = %+v", stats[0])
	}
	if stats[1].Stats.Fail != 1 || stats[1].Stats.LastError != "HTTP 500" {
		t.Fatalf("indexer stat = %+v", stats[1])
	}
	if len(TakeSourceStats()) != 0 {
		t.Fatal("stats not drained")
	}
}

func TestRotatingWriterKeepsAllLinesWithinCap(t *testing.T) {
	dir := t.TempDir()
	writer := NewRotatingWriter(dir, "test.log", 1024, 3)
	for index := 0; index < 400; index++ {
		line := "line-" + pad4(index) + "\n"
		if _, err := writer.Write([]byte(line)); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	_ = writer.Close()
	for _, name := range []string{"test.log", "test.log.1", "test.log.2"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatalf("missing %s: %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "test.log.3")); err == nil {
		t.Fatal("oldest rotation should be dropped")
	}
	active, err := os.ReadFile(filepath.Join(dir, "test.log"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(active), "line-0399") {
		t.Fatal("active file does not hold the newest line")
	}
}

func pad4(value int) string {
	digits := []byte{'0', '0', '0', '0'}
	for i := 3; i >= 0 && value > 0; i-- {
		digits[i] = byte('0' + value%10)
		value /= 10
	}
	return string(digits)
}

func TestReadableFormatterLayout(t *testing.T) {
	dir := t.TempDir()
	closeLog := Init(dir, "readable.log", 1<<20, 2)
	defer closeLog()
	SetLevel("debug")
	Info("hello world", "key", "value", "count", 3)

	data, err := os.ReadFile(FilePath())
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	text := string(data)
	if !strings.Contains(text, "INFO [logging] hello world") {
		t.Fatalf("missing formatted line: %q", text)
	}
	if !strings.Contains(text, "key: value") || !strings.Contains(text, "count: 3") {
		t.Fatalf("missing structured fields: %q", text)
	}
}
