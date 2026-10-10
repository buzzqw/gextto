package queue

import (
	"reflect"
	"testing"
	"time"
)

func testConfig() Config {
	cfg := DefaultConfig()
	cfg.ActiveDownloads = 2
	cfg.ActiveSeeds = 1
	cfg.ActiveLimit = 4
	return cfg.Normalized()
}

func limitsOf(cfg Config) Limits {
	return Limits{Downloads: cfg.ActiveDownloads, Seeds: cfg.ActiveSeeds, Limit: cfg.ActiveLimit}
}

func TestPlanQueueStartsInQueueOrder(t *testing.T) {
	cfg := testConfig()
	items := []Item{
		{ID: "c", Managed: true, Pos: 3},
		{ID: "a", Managed: true, Pos: 1},
		{ID: "b", Managed: true, Pos: 2},
	}
	plan := PlanQueue(items, limitsOf(cfg), cfg, time.Now())
	if !reflect.DeepEqual(plan.Start, []string{"a", "b"}) || len(plan.Stop) != 0 {
		t.Fatalf("unexpected plan %+v", plan)
	}
}

func TestPlanQueueStopsOverTheSlotLimit(t *testing.T) {
	cfg := testConfig()
	items := []Item{
		{ID: "a", Managed: true, Running: true, Pos: 1},
		{ID: "b", Managed: true, Running: true, Pos: 2},
		{ID: "c", Managed: true, Running: true, Pos: 3},
	}
	plan := PlanQueue(items, limitsOf(cfg), cfg, time.Now())
	if !reflect.DeepEqual(plan.Stop, []string{"c"}) || len(plan.Start) != 0 {
		t.Fatalf("unexpected plan %+v", plan)
	}
}

func TestPlanQueueSlowTorrentFreesItsSlot(t *testing.T) {
	cfg := testConfig()
	items := []Item{
		{ID: "a", Managed: true, Running: true, Slow: true, Pos: 1},
		{ID: "b", Managed: true, Running: true, Pos: 2},
		{ID: "c", Managed: true, Pos: 3},
	}
	plan := PlanQueue(items, limitsOf(cfg), cfg, time.Now())
	if !reflect.DeepEqual(plan.Start, []string{"c"}) || len(plan.Stop) != 0 {
		t.Fatalf("a slow torrent must not hold a slot: %+v", plan)
	}

	cfg.DontCountSlow = false
	plan = PlanQueue(items, limitsOf(cfg), cfg, time.Now())
	if len(plan.Start) != 0 {
		t.Fatalf("with dont_count_slow off the slow torrent keeps its slot: %+v", plan)
	}
}

func TestPlanQueueNeverTouchesUnmanagedTorrents(t *testing.T) {
	cfg := testConfig()
	items := []Item{
		{ID: "paused", Pos: 1},
		{ID: "parked-running", Running: true, Pos: 2},
		{ID: "a", Managed: true, Pos: 3},
	}
	plan := PlanQueue(items, limitsOf(cfg), cfg, time.Now())
	if !reflect.DeepEqual(plan.Start, []string{"a"}) {
		t.Fatalf("user-paused or parked torrents must not start: %+v", plan)
	}
	if !reflect.DeepEqual(plan.Stop, []string{"parked-running"}) {
		t.Fatalf("a parked torrent still running must be stopped: %+v", plan)
	}
}

func TestPlanQueueForcedTorrentsRunWithoutASlot(t *testing.T) {
	cfg := testConfig()
	items := []Item{
		{ID: "pinned", Forced: true, Pos: 9},
		{ID: "a", Managed: true, Running: true, Pos: 1},
		{ID: "b", Managed: true, Running: true, Pos: 2},
	}
	plan := PlanQueue(items, limitsOf(cfg), cfg, time.Now())
	if !reflect.DeepEqual(plan.Start, []string{"pinned"}) || len(plan.Stop) != 0 {
		t.Fatalf("unexpected plan %+v", plan)
	}
}

func TestPlanQueueSeedsUseTheirOwnSlots(t *testing.T) {
	cfg := testConfig()
	items := []Item{
		{ID: "s1", Managed: true, Complete: true, Running: true, Pos: 1},
		{ID: "s2", Managed: true, Complete: true, Pos: 2},
		{ID: "d1", Managed: true, Pos: 3},
	}
	plan := PlanQueue(items, limitsOf(cfg), cfg, time.Now())
	if !reflect.DeepEqual(plan.Start, []string{"d1"}) || len(plan.Stop) != 0 {
		t.Fatalf("unexpected plan %+v", plan)
	}
}

// With every running download slow, the hard cap rotates the slow ones out
// so that waiting torrents get a turn, and the rotated ones are not restarted
// until their cooldown expires.
func TestPlanQueueRotatesSlowTorrentsAtTheCap(t *testing.T) {
	cfg := testConfig()
	now := time.Now()
	items := []Item{
		{ID: "a", Managed: true, Running: true, Slow: true, Pos: 1},
		{ID: "b", Managed: true, Running: true, Slow: true, Pos: 2},
		{ID: "c", Managed: true, Running: true, Slow: true, Pos: 3},
		{ID: "d", Managed: true, Running: true, Slow: true, Pos: 4},
		{ID: "e", Managed: true, Pos: 5},
		{ID: "f", Managed: true, Pos: 6},
	}
	plan := PlanQueue(items, limitsOf(cfg), cfg, now)
	if !reflect.DeepEqual(plan.Start, []string{"e", "f"}) {
		t.Fatalf("waiting torrents must start: %+v", plan)
	}
	if !reflect.DeepEqual(plan.Stop, []string{"c", "d"}) || !reflect.DeepEqual(plan.Rotate, []string{"c", "d"}) {
		t.Fatalf("the slow torrents at the back must rotate out: %+v", plan)
	}

	// Next round: c and d were rotated (back of the queue, cooling down); e
	// and f are in their grace time. Nothing must change.
	items = []Item{
		{ID: "a", Managed: true, Running: true, Slow: true, Pos: 1},
		{ID: "b", Managed: true, Running: true, Slow: true, Pos: 2},
		{ID: "e", Managed: true, Running: true, Pos: 5},
		{ID: "f", Managed: true, Running: true, Pos: 6},
		{ID: "c", Managed: true, RotatedAt: now, Pos: 7},
		{ID: "d", Managed: true, RotatedAt: now, Pos: 8},
	}
	plan = PlanQueue(items, limitsOf(cfg), cfg, now.Add(10*time.Second))
	if len(plan.Start)+len(plan.Stop)+len(plan.Rotate) != 0 {
		t.Fatalf("the queue must be stable: %+v", plan)
	}

	// e and f turn slow too: c and d are cooling down and must not churn.
	items[2].Slow, items[3].Slow = true, true
	plan = PlanQueue(items, limitsOf(cfg), cfg, now.Add(5*time.Minute))
	if len(plan.Start)+len(plan.Stop)+len(plan.Rotate) != 0 {
		t.Fatalf("cooling-down torrents must not churn: %+v", plan)
	}

	// After the cooldown, the rotated torrents get their turn back.
	plan = PlanQueue(items, limitsOf(cfg), cfg, now.Add(time.Duration(cfg.SlowRotateSecs+1)*time.Second))
	if !reflect.DeepEqual(plan.Start, []string{"c", "d"}) || !reflect.DeepEqual(plan.Rotate, []string{"e", "f"}) {
		t.Fatalf("after the cooldown the slow torrents rotate: %+v", plan)
	}
}

func TestPlanQueueCapPrefersHealthyTorrents(t *testing.T) {
	cfg := testConfig()
	cfg.ActiveLimit = 2
	items := []Item{
		{ID: "a", Managed: true, Running: true, Pos: 1},
		{ID: "b", Managed: true, Running: true, Slow: true, Pos: 2},
		{ID: "s", Managed: true, Complete: true, Running: true, Pos: 3},
	}
	plan := PlanQueue(items, limitsOf(cfg), cfg, time.Now())
	if !reflect.DeepEqual(plan.Stop, []string{"b"}) {
		t.Fatalf("the slow download goes first: %+v", plan)
	}
}

func TestDynamicQueueWidensWhenBandwidthIsUnused(t *testing.T) {
	cfg := DefaultConfig()
	cfg.DynamicQueue = true
	cfg.ActiveDownloads = 2
	cfg.DynamicMin = 1
	cfg.DynamicMax = 6
	cfg.SpeedLimitDownload = 1000 // KiB/s
	cfg = cfg.Normalized()
	var q Dynamic
	now := time.Now()
	limits := q.Limits(cfg, 100*1024, 3, now)
	if limits.Downloads != 2 {
		t.Fatalf("starts from active_downloads: %+v", limits)
	}
	for i := 1; i <= 3; i++ {
		limits = q.Limits(cfg, 100*1024, 3, now.Add(time.Duration(i)*100*time.Second))
	}
	if limits.Downloads <= 2 {
		t.Fatalf("an idle line with queued torrents must widen the queue: %+v", limits)
	}
	if limits.Limit < limits.Downloads+limits.Seeds {
		t.Fatalf("the hard cap must leave room for the slots: %+v", limits)
	}
}

func TestDynamicQueueNarrowsWhenSaturated(t *testing.T) {
	cfg := DefaultConfig()
	cfg.DynamicQueue = true
	cfg.ActiveDownloads = 4
	cfg.DynamicMin = 1
	cfg.DynamicMax = 6
	cfg.SpeedLimitDownload = 1000
	cfg = cfg.Normalized()
	q := Dynamic{}
	start := time.Now()
	var limits Limits
	for i := 0; i < 40; i++ {
		limits = q.Limits(cfg, 1000*1024, 0, start.Add(time.Duration(i)*5*time.Minute))
	}
	if limits.Downloads >= 4 {
		t.Fatalf("a saturated line must narrow the queue: %+v", limits)
	}
	if limits.Downloads < cfg.DynamicMin {
		t.Fatalf("never below the minimum: %+v", limits)
	}
}

func TestDynamicQueueOffUsesStaticLimits(t *testing.T) {
	cfg := testConfig()
	var q Dynamic
	if got := q.Limits(cfg, 0, 10, time.Now()); got != limitsOf(cfg) {
		t.Fatalf("static limits expected, got %+v", got)
	}
}
