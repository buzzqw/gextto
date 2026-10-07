package gextto

import (
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/models"
)

func TestV2ReplacementIsReportedOnce(t *testing.T) {
	series := "Silo"
	season, episode := int64(3), int64(4)
	v2 := &models.Release{Kind: "series", Series: &series, Season: &season, Episode: &episode, Title: "Silo.S03E04.V2.1080p"}
	v1 := &models.Release{Kind: "series", Series: &series, Season: &season, Episode: &episode, Title: "Silo.S03E04.1080p"}
	rememberV2Skip(v2, time.Now())
	v2Skipped.Lock()
	_, known := v2Skipped.byTarget[releaseTarget(v1)]
	v2Skipped.Unlock()
	if !known {
		t.Fatal("the v2 refusal must be remembered for the episode")
	}
	reportV2Replacement(v1)
	v2Skipped.Lock()
	_, still := v2Skipped.byTarget[releaseTarget(v1)]
	v2Skipped.Unlock()
	if still {
		t.Fatal("the replacement must be reported only once")
	}
}

func TestPendingV2SkipsAreRemindedThenDropped(t *testing.T) {
	series := "From"
	season, episode := int64(2), int64(1)
	v2 := &models.Release{Kind: "series", Series: &series, Season: &season, Episode: &episode, Title: "From.S02E01.V2"}
	start := time.Now()
	rememberV2Skip(v2, start)
	key := releaseTarget(v2)
	notice := func() time.Time {
		v2Skipped.Lock()
		defer v2Skipped.Unlock()
		return v2Skipped.byTarget[key].noticeAt
	}
	reportPendingV2Skips(start)
	first := notice()
	if first.IsZero() {
		t.Fatal("the first missing replacement must be logged at the end of the cycle")
	}
	reportPendingV2Skips(start.Add(time.Hour))
	if !notice().Equal(first) {
		t.Fatal("reminders are at most daily")
	}
	reportPendingV2Skips(start.Add(25 * time.Hour))
	if notice().Equal(first) {
		t.Fatal("a daily reminder is due")
	}
	reportPendingV2Skips(start.Add(v2SkipMemory + time.Hour))
	v2Skipped.Lock()
	_, still := v2Skipped.byTarget[key]
	v2Skipped.Unlock()
	if still {
		t.Fatal("after 30 days the release is no longer tracked")
	}
}
