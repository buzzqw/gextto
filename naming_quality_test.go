package gextto

import (
	"testing"

	"github.com/buzzqw/gextto/internal/models"
)

// TestNamingAudioTokenReadsTheTagGroup checks that the token comes from the
// quality tags the template writes, not from a word of the episode title.
func TestNamingAudioTokenReadsTheTagGroup(t *testing.T) {
	for name, want := range map[string]string{
		"Wolf Like Me - S01E05 - Episodio 5 - [WEB-DL][2160p][h265][EAC3 5.1][IT+EN].mkv": "EAC3",
		"Show - S01E01 - Titolo - [1080p][h264][ddp 5.1][IT+EN].mkv":                      "ddp",
		"Show - S01E01 - DTS [2160p][h265][AAC 2.0][IT].mkv":                              "AAC",
		"Show.S01E01.1080p.WEB-DL.DDP5.1.H.265-GRP.mkv":                                   "DDP",
		"Show - S01E01 - Titolo - [1080p][h264][IT+EN].mkv":                               "",
	} {
		if got := namingAudioToken(name); got != want {
			t.Errorf("namingAudioToken(%q) = %q, want %q", name, got, want)
		}
	}
}

// TestNamingAudioKeepsTheExistingSpelling is the regression for the library
// check that renamed Wolf Like Me S01E05/E06 between "EAC3" and "ddp" every six
// hours: MediaInfo answers "EAC3", ffprobe answers "ddp", and the repair must
// keep the spelling the file already carries when the two mean the same codec.
func TestNamingAudioKeepsTheExistingSpelling(t *testing.T) {
	media := func(value string) mediaTags {
		return mediaTags{AudioCodec: stringPtr(value)}
	}
	release := models.Quality{Audio: "ddp"}
	name := "Show - S01E01 - Titolo - [1080p][h265][EAC3 5.1][IT+EN].mkv"

	if got := namingAudio(name, release, media("EAC3")); got != "EAC3" {
		t.Fatalf("MediaInfo spelling: %q, want EAC3", got)
	}
	if got := namingAudio(name, release, media("ddp")); got != "EAC3" {
		t.Fatalf("ffprobe spelling: %q, want the file spelling EAC3", got)
	}
	if got := namingAudio(name, release, mediaTags{}); got != "EAC3" {
		t.Fatalf("no probe: %q, want the file spelling EAC3", got)
	}

	// A really different codec is still corrected, and the next pass keeps it.
	broken := "Show - S01E01 - Titolo - [1080p][h265][AAC 2.0][IT+EN].mkv"
	if got := namingAudio(broken, release, media("EAC3")); got != "EAC3" {
		t.Fatalf("correction: %q, want EAC3", got)
	}
	if got := namingAudio("Show - S01E01 - Titolo - [1080p][h265][EAC3 5.1][IT+EN].mkv", release, mediaTags{}); got != "EAC3" {
		t.Fatalf("after the correction: %q, want EAC3", got)
	}

	// No token in the name: the release value wins over an alias from a probe.
	plain := "Show - S01E01 - Titolo - [1080p][h265][IT+EN].mkv"
	if got := namingAudio(plain, release, media("EAC3")); got != "ddp" {
		t.Fatalf("no name token: %q, want the release value ddp", got)
	}
}

func TestNamingHDRKeepsTheMoreSpecificLabel(t *testing.T) {
	release := models.Quality{HDR: "DV"}
	media := func(value string) mediaTags {
		return mediaTags{HDR: stringPtr(value)}
	}
	name := "Show - S01E01 - Titolo - [2160p][h265][DV HDR10][AC3][IT+EN].mkv"
	if got := namingHDR(name, release, media("DV")); got != "DV HDR10" {
		t.Fatalf("less specific probe: %q, want the file label DV HDR10", got)
	}
	if got := namingHDR(name, release, mediaTags{}); got != "DV HDR10" {
		t.Fatalf("no probe: %q, want the file label DV HDR10", got)
	}
	if got := namingHDR("Show - S01E01 - Titolo - [2160p][h265][DV][AC3][IT+EN].mkv", release, media("DV HDR10")); got != "DV HDR10" {
		t.Fatalf("more specific probe: %q, want DV HDR10", got)
	}
}

func BenchmarkNamingTags(b *testing.B) {
	name := "Wolf Like Me - S01E05 - Episodio 5 - [WEB-DL][2160p][h265][EAC3 5.1][IT+EN].mkv"
	release := models.Quality{Audio: "ddp", HDR: "DV"}
	media := mediaTags{AudioCodec: stringPtr("EAC3"), HDR: stringPtr("DV HDR10")}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if namingAudio(name, release, media) == "" || namingHDR(name, release, media) == "" {
			b.Fatal("empty label")
		}
	}
}
