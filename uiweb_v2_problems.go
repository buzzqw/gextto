package gextto

import (
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/buzzqw/gextto/internal/logging"
	"github.com/buzzqw/gextto/internal/models"
)

// Dashboard "Da controllare": what needs attention right now, gathered from
// the stall monitor, the pending storage moves and the acquisition history, so
// the operator does not have to read the log to find it.

// v2ProblemsLimit caps how many unresolved warnings the panel lists (one row
// per download, newest first). v2ProblemsScan is how many the query may fetch
// before the closed cases (downloads no longer in the session) are filtered out.
const (
	v2ProblemsLimit = 50
	v2ProblemsScan  = 200
)

type v2StalledRow struct {
	Hash string
	// Name is the full torrent name (the cell tooltip); ShortName is what the
	// table shows, so the reason column keeps the room it needs.
	Name      string
	ShortName string
	Progress  string
	Since     string
	Reason    string
	GiveUpAt  string
	NextRetry string
}

type v2MoveRow struct {
	Hash        string
	Name        string
	ShortName   string
	Destination string
	Attempts    int
}

type v2ProblemsView struct {
	Stalled []v2StalledRow
	Moves   []v2MoveRow
	Recent  []AcquisitionEvent
	Error   string
}

// v2ProblemNameShort is the label the "Da controllare" tables show: the
// release name up to its season/episode marker, which keeps the download
// recognisable without the long quality tail squeezing the reason column.
// Other names are cut at v2ProblemNameLimit runes. The full name stays in the
// cell tooltip.
func v2ProblemNameShort(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	if loc := v2ProblemEpisodeMarker.FindStringIndex(name); loc != nil {
		return name[:loc[1]]
	}
	runes := []rune(name)
	if len(runes) <= v2ProblemNameLimit {
		return name
	}
	return strings.TrimRight(string(runes[:v2ProblemNameLimit]), " ._-") + "…"
}

// v2ProblemEpisodeMarker matches the season/episode marker of a release name
// ("S01E01", also with separators such as "S01.E01" or "S01-E01").
var v2ProblemEpisodeMarker = regexp.MustCompile(`(?i)s\d{1,2}[ ._-]?e\d{1,3}`)

// v2ProblemNameLimit bounds the label of a name without a season/episode
// marker, so a movie release does not push the other columns out of the panel.
const v2ProblemNameLimit = 44

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
			ShortName: v2ProblemNameShort(torrent.Name),
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
		view.Moves = append(view.Moves, v2MoveRow{Hash: key, Name: name, ShortName: v2ProblemNameShort(name), Destination: state.Retry.destination, Attempts: int(state.Retry.attempts)})
		listed[key] = struct{}{}
	}
	sort.Slice(view.Moves, func(i, j int) bool { return view.Moves[i].Name < view.Moves[j].Name })

	ackID := int64(0)
	if s.db != nil {
		ackID = s.db.ProblemsAckID()
	}
	// Scan a wider slice than the panel shows: warnings of downloads that have
	// left the session are filtered below, so they must not fill the window and
	// hide the ones still worth checking.
	recent, err := s.db.AcquisitionProblems(ackID, v2ProblemsScan)
	if err != nil && view.Error == "" {
		view.Error = "Storia dei download non disponibile: " + err.Error()
	}
	for _, event := range recent {
		// Already listed above with its live state.
		if _, ok := listed[event.Hash]; ok {
			continue
		}
		// The download left the session and has nothing pending: the case is
		// closed, so its warning is history, not something to check now. It
		// disappears on its own, unlike a stalled or moving download that
		// stays in the session.
		if _, ok := session[strings.ToLower(event.Hash)]; !ok {
			continue
		}
		view.Recent = append(view.Recent, event)
		if len(view.Recent) >= v2ProblemsLimit {
			break
		}
	}
	return view
}

// V2DashboardProblems renders the "Da controllare" panel body; the dashboard
// loads it on its own and refreshes it every minute.
func V2DashboardProblems(w http.ResponseWriter, r *http.Request, s *AppState) {
	dict, eng := v2Dictionaries(s)
	v2Render(w, http.StatusOK, "v2_dashboard_problems", v2ProblemsViewFrom(s), dict, eng)
}

// V2DashboardProblemsAck marks the shown warnings as read (the "Pulisci"
// button): the panel stops listing them, and a download that produces a new
// warning shows again because its event id is above the watermark. It answers
// with the refreshed panel.
func V2DashboardProblemsAck(w http.ResponseWriter, r *http.Request, s *AppState) {
	if s != nil && s.db != nil {
		if id, err := s.db.AckProblems(); err != nil {
			logging.Warn("dashboard problems acknowledge failed", "error", err)
		} else {
			logging.Debug("dashboard problems acknowledged", "up_to_id", id)
		}
	}
	V2DashboardProblems(w, r, s)
}
