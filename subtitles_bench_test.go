package gextto

import "testing"

func BenchmarkSubtitleReleaseSimilarity(b *testing.B) {
	video := "Show.S01E01.1080p.WEB-DL.DDP5.1.H.264-GROUP.mkv"
	release := "Show.S01E01.1080p.WEB-DL.x264-GROUP"
	b.ReportAllocs()
	for index := 0; index < b.N; index++ {
		if got := subtitleReleaseSimilarity(video, release); got < 80 {
			b.Fatalf("similarity = %d, want a close release match", got)
		}
	}
}

func BenchmarkSubtitleCandidateScore50(b *testing.B) {
	trusted, human := true, false
	candidates := make([]subtitleCandidate, 50)
	for index := range candidates {
		candidates[index] = subtitleCandidate{
			HashMatch: index%3 == 0, SizeMatch: index%3 == 0, ExternalIDMatch: index%4 == 0,
			ReleaseSimilarity: 70 + index%31, FromTrusted: &trusted,
			AITranslated: &human, MachineTranslated: &human, DownloadCount: 1000 + index*137,
		}
	}
	b.ReportAllocs()
	for index := 0; index < b.N; index++ {
		total := 0
		for _, candidate := range candidates {
			total += subtitleCandidateScore(candidate)
		}
		if total <= 0 {
			b.Fatal("candidate scores were empty")
		}
	}
}
