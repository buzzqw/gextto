package constants

import "testing"

func TestVersionFromBuild(t *testing.T) {
	cases := []struct {
		name          string
		build         string
		firstRollover int
		want          string
	}{
		// Gextto: the current 1.1 series keeps the full build number until it
		// reaches 2000, then the third number restarts every thousand.
		{"gextto current", "1925", appFirstRollover, "1.1.1925"},
		{"gextto last of the series", "1999", appFirstRollover, "1.1.1999"},
		{"gextto first rollover", "2000", appFirstRollover, "1.2.0"},
		{"gextto last of 1.2", "2999", appFirstRollover, "1.2.999"},
		{"gextto second rollover", "3000", appFirstRollover, "1.3.0"},
		{"gextto middle", "2500", appFirstRollover, "1.2.500"},
		// gx-torrent: its counter is independent and starts at zero, so its
		// 1.1 series ends at 999 and rolls over at 1000.
		{"gx current", "242", gxFirstRollover, "1.1.242"},
		{"gx last of the series", "999", gxFirstRollover, "1.1.999"},
		{"gx first rollover", "1000", gxFirstRollover, "1.2.0"},
		{"gx last of 1.2", "1999", gxFirstRollover, "1.2.999"},
		{"gx second rollover", "2000", gxFirstRollover, "1.3.0"},
		// Unknown or empty builds keep the old concatenation instead of
		// pretending to be a real version.
		{"empty", "", appFirstRollover, "1.1."},
		{"not a number", "dev", gxFirstRollover, "1.1.dev"},
	}
	for _, tc := range cases {
		if got := versionFromBuild(tc.build, tc.firstRollover); got != tc.want {
			t.Errorf("%s: versionFromBuild(%q, %d) = %q, want %q", tc.name, tc.build, tc.firstRollover, got, tc.want)
		}
	}
}

func TestAppVersionRollsOverAtTwoThousand(t *testing.T) {
	previous := Build
	t.Cleanup(func() { Build = previous })

	Build = "1999"
	if got := AppVersion(); got != "1.1.1999" {
		t.Fatalf("AppVersion at build 1999 = %q", got)
	}
	Build = "2000"
	if got := AppVersion(); got != "1.2.0" {
		t.Fatalf("AppVersion at build 2000 = %q", got)
	}
}

func TestGxTorrentVersionRollsOverAtOneThousand(t *testing.T) {
	previous := GxTorrentBuild
	t.Cleanup(func() { GxTorrentBuild = previous })

	GxTorrentBuild = "999"
	if got := GxTorrentVersion(); got != "1.1.999" {
		t.Fatalf("GxTorrentVersion at build 999 = %q", got)
	}
	GxTorrentBuild = "1000"
	if got := GxTorrentVersion(); got != "1.2.0" {
		t.Fatalf("GxTorrentVersion at build 1000 = %q", got)
	}
}

func TestVersionLabelKeepsTheBuild(t *testing.T) {
	cases := []struct {
		name    string
		version string
		build   string
		want    string
	}{
		// The historical form already ends with the build: no repetition.
		{"legacy", "1.1.1925", "1925", "1.1.1925"},
		{"rolled over", "1.2.925", "2925", "1.2.925 (build 2925)"},
		{"exact thousand", "1.2.0", "2000", "1.2.0 (build 2000)"},
		{"no build", "1.2.0", "", "1.2.0"},
		{"build unknown", "1.1.242", "0", "1.1.242"},
		{"no version", "", "0", ""},
	}
	for _, tc := range cases {
		if got := VersionLabelWithBuild(tc.version, tc.build); got != tc.want {
			t.Errorf("%s: VersionLabelWithBuild(%q, %q) = %q, want %q", tc.name, tc.version, tc.build, got, tc.want)
		}
	}

	previous := Build
	t.Cleanup(func() { Build = previous })
	Build = "2000"
	if got := VersionLabel(); got != "1.2.0 (build 2000)" {
		t.Fatalf("VersionLabel at build 2000 = %q", got)
	}
}
