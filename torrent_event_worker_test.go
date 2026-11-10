package gextto

// torrent_event_worker_test.go drives the event worker's tick: the recovery of
// a completion whose event was lost, the storage moves restored after a
// restart, and an event going all the way from the engine to the library
// move. It pins what the worker does across ticks, as
// torrent_event_behaviour_test.go pins the handler.

import (
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/models"
)

// workerEngine is the placeholder engine with a stub session behind the
// calls the worker's tick makes on a torrent.
type workerEngine struct {
	nullEngine
	s      *countingSession
	events []models.TorrentEvent
}

func (e *workerEngine) Name() string                   { return BackendGxTorrent }
func (e *workerEngine) List() []models.TorrentView     { return e.s.List() }
func (e *workerEngine) Snapshot() []models.TorrentView { return e.s.List() }
func (e *workerEngine) PollEvents() []models.TorrentEvent {
	events := e.events
	e.events = nil
	return events
}
func (e *workerEngine) MoveStorage(hash, destination string) (bool, error) {
	return e.s.MoveStorage(hash, destination)
}
func (e *workerEngine) ForceRecheck(hash string) (bool, error) { return e.s.ForceRecheck(hash) }
func (e *workerEngine) Remove(hash string, deleteFiles bool) (bool, error) {
	return e.s.Remove(hash, deleteFiles)
}
func (e *workerEngine) Pause(hash string) (bool, error)  { return e.s.Pause(hash) }
func (e *workerEngine) Resume(hash string) (bool, error) { return e.s.Resume(hash) }

// newTestEventWorker builds a worker on the behaviour environment's film, with
// the engine reporting it complete and paused.
func newTestEventWorker(t *testing.T, e *behaviourEnv) (*eventWorker, *workerEngine) {
	t.Helper()
	state := newTestAppState(t)
	engine := &workerEngine{s: e.session}
	state.setActiveEngine(engine)
	e.session.list[0].HasMetadata = true
	e.session.list[0].State = "paused"
	w := newEventWorker(state.config_path, e.cfg, state, e.db, state.comics, state.torrent_events)
	// The tick reloads the configuration from the test state's file: keep the
	// environment's folders instead.
	w.lastConfigReload, w.lastConfigForce = time.Now().Add(time.Hour), time.Now().Add(time.Hour)
	w.cfg = e.cfg
	return w, engine
}

// A torrent that reached 100% while its completion event was lost gets the
// event back from the recovery scan; once completed it is settled and not
// queried again.
func TestEventWorkerRecoversALostCompletion(t *testing.T) {
	e := newBehaviourEnv(t)
	w, _ := newTestEventWorker(t, e)
	now := time.Now()

	events := w.recoverCompletions(now, nil, map[string]struct{}{})
	if len(events) != 1 || events[0].Kind != "torrent_finished" || events[0].Hash != behaviourHash {
		t.Fatalf("recovered events = %+v, want one torrent_finished", events)
	}
	if !w.lastRecoveryScan.Equal(now) {
		t.Fatal("the scan time was not recorded")
	}

	// An event already in this tick is not recovered a second time.
	if events := w.recoverCompletions(now, nil, map[string]struct{}{behaviourHash: {}}); len(events) != 0 {
		t.Fatalf("a torrent with its own event was recovered: %+v", events)
	}

	// Completed in the database: nothing to recover, and it is settled.
	if err := e.db.MarkTorrentCompleted(behaviourHash, e.downloads, 64*1024); err != nil {
		t.Fatal(err)
	}
	if events := w.recoverCompletions(now, nil, map[string]struct{}{}); len(events) != 0 {
		t.Fatalf("a completed torrent was recovered: %+v", events)
	}
	if _, ok := w.settledTorrents[behaviourHash]; !ok {
		t.Fatal("the completed torrent was not settled")
	}
}

// A storage move scheduled before a restart is restored by the next worker,
// and the recovery scan leaves that torrent to the move.
func TestEventWorkerRestoresStorageMovesAfterARestart(t *testing.T) {
	e := newBehaviourEnv(t)
	first, _ := newTestEventWorker(t, e)
	tevScheduleStorageMoveRetry(first.storageMoveRetries, behaviourHash, e.library, false, time.Now())
	syncTorrentMoves(e.db, first.persistedMoves, first.storageMoveRetries, first.postSeedMoves)

	second, _ := newTestEventWorker(t, e)
	retry, ok := second.storageMoveRetries[behaviourHash]
	if !ok || retry.destination != e.library {
		t.Fatalf("restored retry = %+v ok=%v, want the move to %q", retry, ok, e.library)
	}
	if events := second.recoverCompletions(time.Now(), nil, map[string]struct{}{}); len(events) != 0 {
		t.Fatalf("a torrent waiting for its move was recovered as a completion: %+v", events)
	}
}

// One tick carries an engine event to the handler: the finished film is
// moved into the library and the event reaches the event log.
func TestEventWorkerTickDispatchesTheEngineEvents(t *testing.T) {
	e := newBehaviourEnv(t)
	w, engine := newTestEventWorker(t, e)
	engine.events = []models.TorrentEvent{{Kind: "torrent_finished", Hash: behaviourHash, Name: e.name, SavePath: e.downloads}}
	before := len(w.eventLog.Snapshot())

	w.tick(time.Now())

	if got := e.session.moved[behaviourHash]; got != e.library {
		t.Fatalf("moved to %q, want the library %q", got, e.library)
	}
	if e.session.moves != 1 {
		t.Fatalf("%d moves after one tick", e.session.moves)
	}
	if _, ok := w.moveRequests[behaviourHash]; !ok {
		t.Fatal("the worker did not keep the move request across ticks")
	}
	if got := len(w.eventLog.Snapshot()); got != before+1 {
		t.Fatalf("event log grew by %d, want 1", got-before)
	}

	// The next tick: no new event, the move is still in flight, nothing repeats.
	w.lastRecoveryScan = time.Time{}
	w.tick(time.Now())
	if e.session.moves != 1 {
		t.Fatalf("%d moves after a second tick", e.session.moves)
	}
}
