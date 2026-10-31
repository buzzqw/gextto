package gextto

import (
	"encoding/base32"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/buzzqw/gextto/internal/models"
)

const (
	dedupHashA = "52ad082d9b694eefa921d20f8468a1428fc5649a"
	dedupHashB = "469e10c8c83e11179de4b52af001770f60f17863"
)

func dedupMagnet(hash, title string) string {
	return "magnet:?xt=urn:btih:" + hash + "&dn=" + strings.ReplaceAll(title, " ", ".")
}

func dedupTorrent(title, link string, size, seeders int64) models.Release {
	return models.Release{Title: title, TorrentURL: &link, Source: "jackett:" + link, SizeBytes: size, Seeders: seeders}
}

// TestCanonicalInfoHashDecodesTheDoubleHexForm covers the identity of a
// release whose indexer hex-encoded the 40-character hash a second time
// (Jackett issue #17045). The outer value is not a v2 hash: it is the v1 hash,
// and both the magnet built from it and the dedup key must use the inner one.
func TestCanonicalInfoHashDecodesTheDoubleHexForm(t *testing.T) {
	const inner = "234493f324ff4fcbf35c7dfea71f3b962cf5e016"
	const doubled = "32333434393366333234666634666362663335633764666561373166336239363263663565303136"
	if got := canonicalInfoHash(doubled); got != inner {
		t.Fatalf("canonicalInfoHash(double hex) = %q, want %q", got, inner)
	}
	if got := prowlarrInfoHashMagnet(doubled, "Some.Release"); !strings.Contains(got, "urn:btih:"+inner) {
		t.Fatalf("magnet = %q, want the decoded v1 hash", got)
	}
	// An 80-character value that does not decode to a hash stays invalid.
	notAHash := strings.Repeat("ab", 40)
	if got := canonicalInfoHash(notAHash); got != "" {
		t.Fatalf("canonicalInfoHash(%q) = %q, want an invalid value", notAHash, got)
	}
}

func dedupMagnetRelease(title, hash string, size, seeders int64) models.Release {
	return models.Release{Title: title, Magnet: dedupMagnet(hash, title), Source: "prowlarr", SizeBytes: size, Seeders: seeders}
}

// The real case measured on the coordinator's machine: five copies of two
// torrents with the same name, from three indexers, with sizes rounded
// differently. They become two releases, never one: the two hashes differ.
func TestMergeDuplicateReleasesFoldsCopiesOfTheSameTorrent(t *testing.T) {
	const title = "Severance S02E03 Who Is Alive 2160p ATVP WEB-DL DDP5 1 DV HDR H 265-NTb"
	releases := []models.Release{
		dedupTorrent(title, "http://jackett/dl/tgx/1", 9_320_000_000, 130),
		dedupMagnetRelease("Severance.S02E03.Who.Is.Alive.2160p.ATVP.WEB-DL.DDP5.1.DV.HDR.H.265-NTb", dedupHashA, 9_320_000_000, 132),
		dedupTorrent(title, "http://jackett/dl/lime/2", 9_300_000_000, 30),
		dedupMagnetRelease("Severance S02E03 Who is Alive 2160p ATVp WEB-DL DDP5 1 DV HDR H 265-NTb", dedupHashB, 9_410_000_000, 13),
		dedupTorrent("Severance S02E03 Who is Alive 2160p ATVp WEB-DL DDP5 1 DV HDR H 265-NTb", "http://jackett/dl/1337x/3", 9_400_000_000, 13),
	}
	merged := mergeDuplicateReleases(releases)
	if len(merged) != 2 {
		t.Fatalf("got %d releases, want 2: %+v", len(merged), merged)
	}
	first, second := merged[0], merged[1]
	if releaseInfoHash(&first) != dedupHashA || len(first.Mirrors) != 2 || first.Seeders != 132 {
		t.Fatalf("first group = hash %q, %d mirrors, %d seeders", releaseInfoHash(&first), len(first.Mirrors), first.Seeders)
	}
	if releaseInfoHash(&second) != dedupHashB || len(second.Mirrors) != 1 {
		t.Fatalf("second group = hash %q, %d mirrors", releaseInfoHash(&second), len(second.Mirrors))
	}
}

// TestMergePrefersAFreeCopyWithTheSameSwarm: the freeleech flag is a current
// tracker fact, so when two copies are otherwise equal the free one is kept as
// the primary and the paid one becomes a mirror.
func TestMergePrefersAFreeCopyWithTheSameSwarm(t *testing.T) {
	const title = "Show S01E02 1080p WEB-DL ITA ENG x264-GRP"
	paid := dedupTorrent(title, "http://indexer/dl/1", 2_000_000_000, 10)
	free := dedupTorrent(title, "http://mirror/dl/2", 2_000_000_000, 10)
	free.Freeleech = true
	merged := mergeDuplicateReleases([]models.Release{paid, free})
	if len(merged) != 1 {
		t.Fatalf("got %d releases, want one", len(merged))
	}
	if !merged[0].Freeleech {
		t.Fatal("the free copy did not become the primary")
	}
	if len(merged[0].Mirrors) != 1 || merged[0].Mirrors[0].Freeleech {
		t.Fatalf("mirrors = %+v", merged[0].Mirrors)
	}
}

// TestSwitchToMirrorPreservesFreeleech: a fallback copy from a freeleech
// tracker must carry its flag with the link, or the log and the score would
// describe the paid copy that failed.
func TestSwitchToMirrorPreservesFreeleech(t *testing.T) {
	link := "http://indexer/dl/1"
	mirrorLink := "http://mirror/dl/2"
	release := models.Release{
		Title:      "Show S01E02 1080p",
		TorrentURL: &link,
		Freeleech:  false,
		Mirrors: []models.Release{{
			Title:      "Show S01E02 1080p",
			TorrentURL: &mirrorLink,
			Freeleech:  true,
			Seeders:    5,
		}},
	}
	if !switchToMirror(&release, nil) {
		t.Fatal("switchToMirror returned false")
	}
	if !release.Freeleech {
		t.Fatal("the freeleech flag of the mirror was lost")
	}
	if release.TorrentURL == nil || *release.TorrentURL != mirrorLink {
		t.Fatalf("link = %v, want the mirror", release.TorrentURL)
	}
}

// Releases that look alike but are different torrents stay apart: in doubt an
// extra candidate is cheaper than a lost release.
func TestMergeDuplicateReleasesKeepsDifferentReleasesApart(t *testing.T) {
	const size = 2_000_000_000
	base := "Show S01E02 1080p WEB-DL ITA ENG x264-GRP"
	cases := map[string]string{
		"group":      "Show S01E02 1080p WEB-DL ITA ENG x264-OTHER",
		"resolution": "Show S01E02 2160p WEB-DL ITA ENG x264-GRP",
		"proper":     "Show S01E02 PROPER 1080p WEB-DL ITA ENG x264-GRP",
		"language":   "Show S01E02 1080p WEB-DL ENG x264-GRP",
		"pack":       "Show S01 1080p WEB-DL ITA ENG x264-GRP",
		"episode":    "Show S01E03 1080p WEB-DL ITA ENG x264-GRP",
	}
	for name, other := range cases {
		t.Run(name, func(t *testing.T) {
			merged := mergeDuplicateReleases([]models.Release{
				dedupTorrent(base, "http://a/1", size, 10),
				dedupTorrent(other, "http://b/2", size, 10),
			})
			if len(merged) != 2 {
				t.Fatalf("%q and %q were merged", base, other)
			}
		})
	}
	t.Run("size beyond rounding", func(t *testing.T) {
		merged := mergeDuplicateReleases([]models.Release{
			dedupTorrent(base, "http://a/1", 9_320_000_000, 10),
			dedupTorrent(base, "http://b/2", 9_410_000_000, 10),
		})
		if len(merged) != 2 {
			t.Fatal("sizes 1% apart were merged")
		}
	})
	t.Run("unknown size", func(t *testing.T) {
		merged := mergeDuplicateReleases([]models.Release{
			dedupTorrent(base, "http://a/1", 0, 10),
			dedupTorrent(base, "http://b/2", 0, 10),
		})
		if len(merged) != 2 {
			t.Fatal("releases without a size were merged by name")
		}
	})
	t.Run("different known hashes", func(t *testing.T) {
		merged := mergeDuplicateReleases([]models.Release{
			dedupMagnetRelease(base, dedupHashA, size, 10),
			dedupMagnetRelease(base, dedupHashB, size, 10),
		})
		if len(merged) != 2 {
			t.Fatal("two different infohashes were merged")
		}
	})
}

// A `.torrent` link whose indexer declared the hash is the same torrent as a
// magnet with that hash, also when one spells it in base32.
func TestMergeDuplicateReleasesUsesDeclaredAndBase32Hashes(t *testing.T) {
	raw, err := hex.DecodeString(dedupHashA)
	if err != nil {
		t.Fatal(err)
	}
	base32Hash := strings.ToLower(base32.StdEncoding.EncodeToString(raw))
	link := dedupTorrent("Private name", "http://tracker/dl/1?passkey=x", 0, 50)
	link.InfoHash = canonicalInfoHash(dedupHashA)
	merged := mergeDuplicateReleases([]models.Release{
		link,
		{Title: "Public name", Magnet: "magnet:?xt=urn:btih:" + base32Hash, Source: "public", Seeders: 10},
	})
	if len(merged) != 1 {
		t.Fatalf("got %d releases, want 1", len(merged))
	}
	// More seeders wins: the private copy, delivered as its file.
	if merged[0].TorrentURL == nil || merged[0].Magnet != "" || len(merged[0].Mirrors) != 1 {
		t.Fatalf("kept copy = %+v", merged[0])
	}
}

// The kept copy does not depend on the order the sources answered in.
func TestMergeDuplicateReleasesIsOrderIndependent(t *testing.T) {
	const title = "Movie 2021 1080p BluRay x264-GRP"
	a := dedupTorrent(title, "http://a/1", 10_000_000_000, 5)
	b := dedupMagnetRelease(title, dedupHashA, 10_010_000_000, 80)
	c := dedupTorrent(title, "http://c/3", 10_000_000_000, 20)
	for _, order := range [][]models.Release{{a, b, c}, {c, b, a}, {b, a, c}} {
		merged := mergeDuplicateReleases(order)
		if len(merged) != 1 || releaseInfoHash(&merged[0]) != dedupHashA || len(merged[0].Mirrors) != 2 {
			t.Fatalf("order %v: %+v", []string{order[0].Source, order[1].Source, order[2].Source}, merged)
		}
	}
}

// When the chosen link fails, the best mirror that is not quarantined takes
// its place; the matched name, quality and episodes stay.
func TestSwitchToMirrorPicksTheBestUsableCopy(t *testing.T) {
	season, episode := int64(1), int64(2)
	release := dedupTorrent("Show S01E02 1080p WEB-DL-GRP", "http://dead/1", 1_000_000_000, 100)
	release.Season, release.Episode = &season, &episode
	release.Mirrors = []models.Release{
		dedupTorrent("other name", "http://quarantined/2", 1_000_000_000, 90),
		dedupTorrent("other name", "http://weak/3", 1_000_000_000, 5),
		dedupTorrent("other name", "http://good/4", 1_000_000_000, 40),
	}
	usable := mirrorNotQuarantined(map[string]string{"http://quarantined/2": "404"})
	if !switchToMirror(&release, usable) {
		t.Fatal("no mirror chosen")
	}
	if *release.TorrentURL != "http://good/4" || release.Seeders != 40 || release.Title != "Show S01E02 1080p WEB-DL-GRP" || *release.Episode != 2 {
		t.Fatalf("after the first switch: %+v", release)
	}
	if !switchToMirror(&release, usable) || *release.TorrentURL != "http://weak/3" {
		t.Fatalf("after the second switch: %+v", release)
	}
	if switchToMirror(&release, usable) {
		t.Fatalf("a quarantined mirror was used: %+v", release)
	}
}

// benchInput is a cycle of the coordinator's size: about 24,000 releases, a
// third of them copies of another one.
func benchInput() []models.Release {
	releases := make([]models.Release, 0, 24_000)
	for i := range 16_000 {
		title := "Series " + strings.Repeat("x", i%7) + " S01E" + string(rune('A'+i%26)) + " 1080p WEB-DL-GRP" + hex.EncodeToString([]byte{byte(i), byte(i >> 8)})
		releases = append(releases, dedupTorrent(title, "http://a/"+title, int64(1_000_000_000+i*1000), int64(i%50)))
		if i%2 == 0 {
			releases = append(releases, dedupTorrent(title, "http://b/"+title, int64(1_000_000_000+i*1000), int64(i%30)))
		}
	}
	return releases
}

func BenchmarkMergeDuplicateReleases(b *testing.B) {
	releases := benchInput()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		input := make([]models.Release, len(releases))
		copy(input, releases)
		_ = mergeDuplicateReleases(input)
	}
}
