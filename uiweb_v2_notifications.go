package gextto

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/buzzqw/gextto/internal/logging"
)

// The Notifications page: every attempt the Notifier made, with its channels
// and outcome. The record is written in notifier.go and kept by
// notification_log.go; this page is where "the notification did not arrive"
// gets an answer.

// v2NotificationRow is one attempt as the table shows it.
type v2NotificationRow struct {
	When     string
	Event    string
	Channels string
	// Outcome is the Italian source string of the state ("inviata",
	// "fallita", "non inviata"), translated by the template.
	Outcome string
	// OutcomeClass colours the badge: ok, err or warn.
	OutcomeClass string
	Detail       string
}

// v2NotificationsView is the page: the attempts of the chosen outcome plus the
// counts of the whole log.
type v2NotificationsView struct {
	Rows       []v2NotificationRow
	Outcome    string
	Total      int
	Sent       int
	Failed     int
	Suppressed int
	// RetentionNote says how much is kept, as one translated sentence.
	RetentionNote string
}

// v2NotificationsViewFrom reads the log and applies the outcome filter.
func v2NotificationsViewFrom(s *AppState, r *http.Request) v2NotificationsView {
	outcome := strings.TrimSpace(r.FormValue("outcome"))
	days := int(notificationLogRetention.Hours() / 24)
	view := v2NotificationsView{
		Outcome:       outcome,
		RetentionNote: uiTextf(s, "Vengono conservate le ultime %d notifiche e quelle degli ultimi %d giorni.", notificationLogRows, days),
	}
	if s.db == nil {
		return view
	}
	records, err := s.db.NotificationLog(notificationLogRows, "")
	if err != nil {
		logging.Warn("the notification log could not be read", "error", err)
		return view
	}
	view.Total = len(records)
	for _, record := range records {
		switch record.Outcome {
		case notificationSent:
			view.Sent++
		case notificationFailed:
			view.Failed++
		case notificationSuppressed:
			view.Suppressed++
		}
		if outcome != "" && record.Outcome != outcome {
			continue
		}
		row := v2NotificationRow{
			When:         record.At.Format("2006-01-02 15:04:05"),
			Channels:     strings.Join(record.Channels, ", "),
			Detail:       record.Detail,
			OutcomeClass: "warn",
		}
		row.Event = record.Title
		if strings.TrimSpace(row.Event) == "" {
			row.Event = record.Event
		}
		switch record.Outcome {
		case notificationSent:
			row.Outcome, row.OutcomeClass = "inviata", "ok"
		case notificationFailed:
			row.Outcome, row.OutcomeClass = "fallita", "err"
		case notificationSuppressed:
			row.Outcome = "non inviata"
		}
		if row.Channels == "" {
			row.Channels = "—"
		}
		view.Rows = append(view.Rows, row)
	}
	return view
}

// V2NotificationsClear empties the log and reports it as a toast.
func V2NotificationsClear(w http.ResponseWriter, r *http.Request, s *AppState) {
	message := "Registro delle notifiche svuotato."
	failed := false
	if s.db == nil || s.db.ClearNotificationLog() != nil {
		message = "Il registro delle notifiche non è stato svuotato."
		failed = true
	}
	redirect := "/?view=notifications&toast=" + url.QueryEscape(message)
	if failed {
		redirect += "&toast_err=1"
	}
	if r.Header.Get("HX-Request") == "" {
		http.Redirect(w, r, redirect, http.StatusSeeOther)
		return
	}
	w.Header().Set("HX-Redirect", redirect)
	w.WriteHeader(http.StatusNoContent)
}
