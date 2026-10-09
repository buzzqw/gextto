package gextto

import (
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/buzzqw/gextto/internal/logging"
	"github.com/buzzqw/gextto/internal/models"
)

// Dashboard "Da controllare": what needs attention right now, gathered from
// the stall monitor, the pending storage moves and the acquisition history, so
// the operator does not have to read the log to find it.

const v2ProblemsWindow = 24 * time.Hour

type v2StalledRow struct {
	Hash      string
	Name      string
	Progress  string
	Since     string
	Reason    string
	GiveUpAt  string
	NextRetry string
}

type v2MoveRow struct {
	Hash        string
	Name        string
	Destination string
	Attempts    int
}

type v2ProblemsView struct {
	Stalled []v2StalledRow
	Moves   []v2MoveRow
	Recent  []AcquisitionEvent
	Error   string
}

// Empty reports whether there is nothing to show.
func (v v2ProblemsView) Empty() bool {
	return len(v.Stalled) == 0 && len(v.Moves) == 0 && len(v.Recent) == 0 && v.Error == ""
}

// v2ProblemTime formats a moment for the panel in local time (the stall state
// is stored in UTC): time only today, day and time otherwise.
func v2ProblemTime(at, now time.Time) string {
	if at.IsZero() {
		return "—"
	}
	at, now = at.Local(), now.Local()
	y1, m1, d1 := at.Date()
	y2, m2, d2 := now.Date()
	if y1 == y2 && m1 == m2 && d1 == d2 {
		return at.Format("15:04")
	}
	return at.Format("02/01 15:04")
}

func v2ProblemsViewFrom(s *AppState) v2ProblemsView {
	view := v2ProblemsView{}
	now := time.Now()
	cfg := latestConfig(s)
	session := map[string]models.TorrentView{}
	for _, torrent := range s.activeEngine().List() {
		session[strings.ToLower(torrent.Hash)] = torrent
	}
	listed := map[string]struct{}{}

	watches, err := s.db.LoadStallWatches()
	if err != nil {
		view.Error = "Stato dei download fermi non disponibile: " + err.Error()
	}
	for hash, entry := range watches {
		torrent, ok := session[strings.ToLower(hash)]
		if !ok || entry.stalledSince == nil {
			continue
		}
		_, reason, _ := DiagnoseTorrent(&torrent)
		row := v2StalledRow{
			Hash:      torrent.Hash,
			Name:      torrent.Name,
			Progress:  logPercent(torrent.Progress),
			Since:     v2ProblemTime(*entry.stalledSince, now),
			Reason:    reason,
			GiveUpAt:  "mai",
			NextRetry: v2ProblemTime(entry.nextRetryAt, now),
		}
		if window, _, ok := tev_stallGiveupWindow(cfg, &torrent); ok {
			row.GiveUpAt = v2ProblemTime(entry.stalledSince.Add(window), now)
		}
		view.Stalled = append(view.Stalled, row)
		listed[strings.ToLower(hash)] = struct{}{}
	}
	sort.Slice(view.Stalled, func(i, j int) bool { return view.Stalled[i].Name < view.Stalled[j].Name })

	moves, err := s.db.LoadTorrentMoves()
	if err != nil && view.Error == "" {
		view.Error = "Stato degli spostamenti non disponibile: " + err.Error()
	}
	for hash, state := range moves {
		if state.Retry == nil {
			continue
		}
		key := strings.ToLower(hash)
		name := logging.AcqID(key)
		if torrent, ok := session[key]; ok {
			name = torrent.Name
		} else if meta, metaErr := s.db.TorrentMeta(key); metaErr == nil && meta != nil && meta.Release.Title != "" {
			name = meta.Release.Title
		}
		view.Moves = append(view.Moves, v2MoveRow{Hash: key, Name: name, Destination: state.Retry.destination, Attempts: int(state.Retry.attempts)})
		listed[key] = struct{}{}
	}
	sort.Slice(view.Moves, func(i, j int) bool { return view.Moves[i].Name < view.Moves[j].Name })

	recent, err := s.db.AcquisitionProblemsSince(now.Add(-v2ProblemsWindow), 20)
	if err != nil && view.Error == "" {
		view.Error = "Storia dei download non disponibile: " + err.Error()
	}
	for _, event := range recent {
		// Already listed above with its live state.
		if _, ok := listed[event.Hash]; ok {
			continue
		}
		view.Recent = append(view.Recent, event)
	}
	return view
}

// V2DashboardProblems renders the "Da controllare" panel body; the dashboard
// loads it on its own and refreshes it every minute.
func V2DashboardProblems(w http.ResponseWriter, r *http.Request, s *AppState) {
	dict, eng := v2Dictionaries(s)
	v2Render(w, http.StatusOK, "v2_dashboard_problems", v2ProblemsViewFrom(s), dict, eng)
}
