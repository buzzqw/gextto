package gextto

import (
	"strings"
	"testing"
)

// TestProblemNameShort covers the label of the "Da controllare" tables: the
// release name stops at its season/episode marker (the long quality tail was
// squeezing the reason column), names without a marker are bounded, and short
// names stay as they are. The full name remains the cell tooltip.
func TestProblemNameShort(t *testing.T) {
	cases := []struct{ name, want string }{
		{
			name: "Star.Trek.Starfleet.Academy.S01E01.I.Ragazzi.Di.Oggi.ITA.ENG.2160p.PMTP.WEB-DL.DDP5.1.DV.HDR.H265-UBi.mkv",
			want: "Star.Trek.Starfleet.Academy.S01E01",
		},
		{name: "Morgane.S04", want: "Morgane.S04"},
		{name: "Show.S02.E10.ITA.1080p.WEB-DL", want: "Show.S02.E10"},
		{name: "Show S03-E04", want: "Show S03-E04"},
		{name: "  Morgane.S04  ", want: "Morgane.S04"},
		{name: "", want: ""},
	}
	for _, tc := range cases {
		if got := v2ProblemNameShort(tc.name); got != tc.want {
			t.Errorf("v2ProblemNameShort(%q) = %q, want %q", tc.name, got, tc.want)
		}
	}
	long := strings.Repeat("parola ", 20) + "film.mkv"
	got := v2ProblemNameShort(strings.TrimSpace(long))
	if runes := []rune(got); len(runes) > v2ProblemNameLimit+1 || !strings.HasSuffix(got, "…") {
		t.Fatalf("long name not bounded: %q (%d runes)", got, len(runes))
	}
}
