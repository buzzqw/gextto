package constants

import "testing"

func TestAppVersionKeepsTheBuildAndUsesTheManualSeries(t *testing.T) {
	previousBuild, previousSeries := Build, VersionSeries
	t.Cleanup(func() { Build, VersionSeries = previousBuild, previousSeries })

	Build, VersionSeries = "1961", "1.1"
	if got := AppVersion(); got != "1.1.1961" {
		t.Fatalf("AppVersion = %q, want 1.1.1961", got)
	}
	Build, VersionSeries = "2000", "1.1"
	if got := AppVersion(); got != "1.1.2000" {
		t.Fatalf("AppVersion = %q, want 1.1.2000 (the build is the third number)", got)
	}
	Build, VersionSeries = "2500", "1.2"
	if got := AppVersion(); got != "1.2.2500" {
		t.Fatalf("AppVersion = %q, want the manually raised series 1.2.2500", got)
	}
}

func TestGxTorrentVersionKeepsTheBuildAndUsesItsOwnSeries(t *testing.T) {
	previousBuild, previousSeries := GxTorrentBuild, GxVersionSeries
	t.Cleanup(func() { GxTorrentBuild, GxVersionSeries = previousBuild, previousSeries })

	GxTorrentBuild, GxVersionSeries = "249", "1.1"
	if got := GxTorrentVersion(); got != "1.1.249" {
		t.Fatalf("GxTorrentVersion = %q, want 1.1.249", got)
	}
	GxTorrentBuild, GxVersionSeries = "1000", "1.1"
	if got := GxTorrentVersion(); got != "1.1.1000" {
		t.Fatalf("GxTorrentVersion = %q, want 1.1.1000", got)
	}
}

func TestVersionLabelKeepsTheBuild(t *testing.T) {
	cases := []struct {
		name    string
		version string
		build   string
		want    string
	}{
		// The current form already ends with the build: no repetition.
		{"current", "1.1.1961", "1961", "1.1.1961"},
		{"manual series", "1.2.2500", "2500", "1.2.2500"},
		// An old manifest that dropped the build still gets it back.
		{"old manifest", "1.2.925", "2925", "1.2.925 (build 2925)"},
		{"no build", "1.2.0", "", "1.2.0"},
		{"build unknown", "1.1.242", "0", "1.1.242"},
		{"no version", "", "0", ""},
	}
	for _, tc := range cases {
		if got := VersionLabelWithBuild(tc.version, tc.build); got != tc.want {
			t.Errorf("%s: VersionLabelWithBuild(%q, %q) = %q, want %q", tc.name, tc.version, tc.build, got, tc.want)
		}
	}

	previousBuild, previousSeries := Build, VersionSeries
	t.Cleanup(func() { Build, VersionSeries = previousBuild, previousSeries })
	Build, VersionSeries = "2000", "1.1"
	if got := VersionLabel(); got != "1.1.2000" {
		t.Fatalf("VersionLabel = %q, want 1.1.2000", got)
	}
}
