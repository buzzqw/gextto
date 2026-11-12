package gextto

import "testing"

func TestEd2kConfidence(t *testing.T) {
	ep := func(name string, size int64) ed2kCandidate { return ed2kCandidate{Name: name, Size: size, Sources: 5} }
	cases := []struct {
		name string
		c    ed2kCandidate
		all  []ed2kCandidate
		want string
	}{
		{"a normal episode", ep("Serie S01E02 1080p WEB-DL ITA.mkv", 900<<20), nil, ed2kTrustGood},
		{"a program", ep("Serie S01E02 1080p.mkv.exe", 2<<20), nil, ed2kTrustSuspect},
		{"an archive", ep("Serie S01E02 1080p.rar", 900<<20), nil, ed2kTrustDoubtful},
		{"a tiny 1080p episode", ep("Serie S01E02 1080p.mkv", 20<<20), nil, ed2kTrustSuspect},
		{"a small 1080p episode", ep("Serie S01E02 1080p.mkv", 200<<20), nil, ed2kTrustDoubtful},
		{"a spam name", ep("Serie S01E02 www.example.com 720p.avi", 500<<20), nil, ed2kTrustDoubtful},
		{"one size under two episodes", ep("Serie S01E02 720p.mkv", 600<<20),
			[]ed2kCandidate{ep("Serie S01E02 720p.mkv", 600<<20), ep("Serie S01E05 720p.mkv", 600<<20)}, ed2kTrustSuspect},
		{"one size, same episode, two names", ep("Serie S01E02 720p.mkv", 600<<20),
			[]ed2kCandidate{ep("Serie.S01E02.720p.ITA.mkv", 600<<20)}, ed2kTrustGood},
	}
	for _, c := range cases {
		if got := ed2kConfidence(c.c, c.all); got.Level != c.want {
			t.Errorf("%s: level %q (%v), want %q", c.name, got.Level, got.Reasons, c.want)
		}
	}
	one := ed2kConfidence(ed2kCandidate{Name: "Serie S01E02 720p.mkv", Size: 600 << 20, Sources: 1}, nil)
	if one.Level != ed2kTrustGood || len(one.Reasons) != 1 || one.Reasons[0] != ed2kReasonOneSource {
		t.Fatalf("one source: %+v", one)
	}
}
