package gextto

import "net/http"

// JobsList is `GET /api/jobs`: the observable state of background jobs. The
// optional `kind` and `state` query parameters filter the result.
//
// Phase 3 of the technical review: rename-all, scan-archives and
// media-info-backfill already publish their jobs here. The endpoints are
// read-only (creation happens inside each operation); the record shape
// (id, kind, state, progress, error, result) is covered by jobs_test.go.
func JobsList(w http.ResponseWriter, r *http.Request, s *AppState) {
	if s.jobs == nil {
		jsonStatus(w, http.StatusOK, map[string]any{"jobs": []Job{}})
		return
	}
	kind := r.URL.Query().Get("kind")
	state := JobState(r.URL.Query().Get("state"))
	jsonStatus(w, http.StatusOK, map[string]any{"jobs": s.jobs.List(kind, state)})
}

// JobsGet is `GET /api/jobs/{id}`: one job snapshot.
func JobsGet(w http.ResponseWriter, r *http.Request, s *AppState) {
	if s.jobs == nil {
		jsonError(w, http.StatusNotFound, "job not found")
		return
	}
	job, ok := s.jobs.Get(pathParam(r, "id"))
	if !ok {
		jsonError(w, http.StatusNotFound, "job not found")
		return
	}
	jsonStatus(w, http.StatusOK, map[string]any{"job": job})
}

// JobsCancel is `POST /api/jobs/{id}/cancel`: asks a queued or running job to
// stop. The running function must observe its context (see JobFunc).
func JobsCancel(w http.ResponseWriter, r *http.Request, s *AppState) {
	id := pathParam(r, "id")
	if s.jobs == nil {
		jsonError(w, http.StatusNotFound, "job not found")
		return
	}
	if _, ok := s.jobs.Get(id); !ok {
		jsonError(w, http.StatusNotFound, "job not found")
		return
	}
	if !s.jobs.Cancel(id) {
		jsonError(w, http.StatusConflict, "job is not cancellable")
		return
	}
	jsonStatus(w, http.StatusAccepted, map[string]any{"ok": true})
}
