package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/buzzqw/gextto/internal/ed2k"
	"github.com/buzzqw/gextto/internal/ed2k/transfer/download"
	"github.com/buzzqw/gextto/internal/webui/assets"
)

const apiBodyLimit = 1 << 20

func (d *Daemon) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/health", d.withAuth(d.handleHealth))
	mux.HandleFunc("GET /api/v1/status", d.withAuth(d.handleStatus))
	mux.HandleFunc("GET /api/v1/search", d.withAuth(d.handleSearch))
	mux.HandleFunc("POST /api/v1/link", d.withAuth(d.handleLink))
	mux.HandleFunc("GET /api/v1/transfers", d.withAuth(d.handleTransfers))
	mux.HandleFunc("POST /api/v1/transfers", d.withAuth(d.handleTransfers))
	mux.HandleFunc("GET /api/v1/transfers/{id}", d.withAuth(d.handleTransfer))
	mux.HandleFunc("DELETE /api/v1/transfers/{id}", d.withAuth(d.handleTransfer))
	mux.HandleFunc("POST /api/v1/transfers/{id}/pause", d.withAuth(d.handleTransferAction))
	mux.HandleFunc("POST /api/v1/transfers/{id}/resume", d.withAuth(d.handleTransferAction))
	mux.HandleFunc("POST /api/v1/transfers/{id}/settings", d.withAuth(d.handleTransferSettings))
	mux.HandleFunc("GET /api/v1/nodes", d.withAuth(d.handleNodes))
	mux.HandleFunc("GET /api/v1/update", d.withAuth(d.handleUpdate))
	mux.HandleFunc("POST /api/v1/update/apply", d.withAuth(d.handleUpdateApply))

	mux.HandleFunc("GET /{$}", d.handleUI)
	mux.HandleFunc("GET /ui", d.handleUI)
	mux.HandleFunc("GET /favicon.ico", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/svg+xml")
		_, _ = w.Write([]byte(assets.FaviconSVG()))
	})
	mux.HandleFunc("GET /assets/theme.css", staticAsset("text/css; charset=utf-8", assets.ThemeCSS))
	mux.HandleFunc("GET /assets/common.js", staticAsset("text/javascript; charset=utf-8", assets.CommonJS))
	mux.HandleFunc("GET /assets/icons.svg", staticAsset("image/svg+xml", assets.IconsSVG))

	return d.guard(mux)
}

func staticAsset(contentType string, body func() string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Cache-Control", "no-cache")
		_, _ = w.Write([]byte(body()))
	}
}

// guard applies the standalone-only checks: a Host header that is not an IP
// or localhost is rejected (DNS rebinding), and mutating requests must not
// come from another origin.
func (d *Daemon) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if d.opts.Mode == modeStandalone {
			host, _, err := net.SplitHostPort(r.Host)
			if err != nil {
				host = r.Host
			}
			if host != "" && !strings.EqualFold(host, "localhost") && net.ParseIP(host) == nil {
				http.Error(w, "invalid host", http.StatusBadRequest)
				return
			}
			if r.Method != http.MethodGet && r.Method != http.MethodHead {
				origin := strings.TrimSpace(r.Header.Get("Origin"))
				if origin != "" {
					parsed, err := url.Parse(origin)
					if err != nil || !strings.EqualFold(parsed.Host, r.Host) {
						http.Error(w, "cross-origin request rejected", http.StatusForbidden)
						return
					}
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (d *Daemon) withAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !d.authorized(r) {
			w.Header().Set("WWW-Authenticate", `Bearer realm="gx-ed2k"`)
			writeJSON(w, http.StatusUnauthorized, map[string]any{"ok": false, "error": "unauthorized"})
			return
		}
		next(w, r)
	}
}

func (d *Daemon) authorized(r *http.Request) bool {
	token := strings.TrimSpace(d.opts.Token)
	if token == "" {
		return true
	}
	supplied := strings.TrimSpace(r.Header.Get("X-Gx-Token"))
	if supplied == "" {
		if header := strings.TrimSpace(r.Header.Get("Authorization")); strings.HasPrefix(strings.ToLower(header), "bearer ") {
			supplied = strings.TrimSpace(header[len("bearer "):])
		}
	}
	if supplied == "" {
		supplied = strings.TrimSpace(r.URL.Query().Get("token"))
	}
	if supplied == "" {
		if cookie, err := r.Cookie("gx_token"); err == nil {
			supplied = strings.TrimSpace(cookie.Value)
		}
	}
	return subtle.ConstantTimeCompare([]byte(supplied), []byte(token)) == 1
}

func (d *Daemon) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, d.healthPayload())
}

func (d *Daemon) handleStatus(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, d.statusPayload())
}

func (d *Daemon) handleNodes(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "nodes": d.nodesPayload()})
}

func (d *Daemon) handleSearch(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if query == "" || len(query) > 256 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "q must contain 1 to 256 characters"})
		return
	}
	timeout := 30 * time.Second
	if raw := strings.TrimSpace(r.URL.Query().Get("timeout_ms")); raw != "" {
		if parsed, err := time.ParseDuration(raw + "ms"); err == nil && parsed >= time.Second && parsed <= 120*time.Second {
			timeout = parsed
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()
	files, err := d.client.Search(ctx, query)
	if err != nil && len(files) == 0 {
		status := http.StatusBadGateway
		if errors.Is(err, context.DeadlineExceeded) {
			status = http.StatusGatewayTimeout
		}
		writeJSON(w, status, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"query":   query,
		"results": toAPIResults(files),
	})
}

type linkInput struct {
	Link     string `json:"link"`
	Priority string `json:"priority,omitempty"`
	LimitKB  *int   `json:"limit_kb,omitempty"`
}

func (d *Daemon) handleLink(w http.ResponseWriter, r *http.Request) {
	var input linkInput
	body, err := io.ReadAll(io.LimitReader(r.Body, apiBodyLimit))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "cannot read request"})
		return
	}
	if err := json.Unmarshal(body, &input); err != nil {
		// Fall back to a form field so the standalone page can post a plain form.
		if err := r.ParseForm(); err == nil {
			input.Link = r.PostFormValue("link")
		}
	}
	raw := strings.TrimSpace(input.Link)
	if raw == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "link is required"})
		return
	}
	file, err := ed2k.ParseFileLink(raw)
	if err == nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":       true,
			"kind":     "file",
			"file":     apiResult{Name: file.Name, SizeBytes: file.Size, Hash: file.Hash, Link: file.String(), Origin: "link"},
			"download": d.transfers != nil,
		})
		return
	}
	server, serverErr := ed2k.ParseServerLink(raw)
	if serverErr == nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "kind": "server", "server": server})
		return
	}
	writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
}

func (d *Daemon) handleTransfers(w http.ResponseWriter, r *http.Request) {
	if d.transfers == nil {
		writeJSON(w, http.StatusNotImplemented, map[string]any{"ok": false, "error": "download is disabled on this daemon"})
		return
	}
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "transfers": d.transfers.List()})
	case http.MethodPost:
		var input linkInput
		body, err := io.ReadAll(io.LimitReader(r.Body, apiBodyLimit))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "cannot read request"})
			return
		}
		if err := json.Unmarshal(body, &input); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid JSON"})
			return
		}
		file, err := ed2k.ParseFileLink(strings.TrimSpace(input.Link))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		hash, err := ed2k.ParseID(file.Hash)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		transfer, err := d.transfers.Add(download.Link{Hash: hash, Name: file.Name, Size: file.Size})
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		if input.Priority != "" && !d.transfers.SetPriority(transfer.ID, input.Priority) {
			writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid priority"})
			return
		}
		if input.LimitKB != nil && !d.transfers.SetLimitKB(transfer.ID, *input.LimitKB) {
			writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid limit_kb"})
			return
		}
		if updated, ok := d.transfers.Get(transfer.ID); ok {
			transfer = updated
		}
		writeJSON(w, http.StatusCreated, map[string]any{"ok": true, "transfer": transfer})
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"ok": false, "error": "method not allowed"})
	}
}

func (d *Daemon) handleTransfer(w http.ResponseWriter, r *http.Request) {
	if d.transfers == nil {
		writeJSON(w, http.StatusNotImplemented, map[string]any{"ok": false, "error": "download is disabled on this daemon"})
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "id is required"})
		return
	}
	switch r.Method {
	case http.MethodGet:
		transfer, ok := d.transfers.Get(id)
		if !ok {
			writeJSON(w, http.StatusNotFound, map[string]any{"ok": false, "error": "transfer not found"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "transfer": transfer})
	case http.MethodDelete:
		if !d.transfers.Remove(id) {
			writeJSON(w, http.StatusNotFound, map[string]any{"ok": false, "error": "transfer not found"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"ok": false, "error": "method not allowed"})
	}
}

// handleTransferSettings changes the priority or the per-file rate limit.
func (d *Daemon) handleTransferSettings(w http.ResponseWriter, r *http.Request) {
	if d.transfers == nil {
		writeJSON(w, http.StatusNotImplemented, map[string]any{"ok": false, "error": "download is disabled on this daemon"})
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "id is required"})
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, apiBodyLimit))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "cannot read request"})
		return
	}
	var input struct {
		Priority *string `json:"priority"`
		LimitKB  *int    `json:"limit_kb"`
	}
	if err := json.Unmarshal(body, &input); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid JSON"})
		return
	}
	if input.Priority == nil && input.LimitKB == nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "nothing to change"})
		return
	}
	if input.Priority != nil && !d.transfers.SetPriority(id, *input.Priority) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid priority"})
		return
	}
	if input.LimitKB != nil && !d.transfers.SetLimitKB(id, *input.LimitKB) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid limit_kb"})
		return
	}
	transfer, ok := d.transfers.Get(id)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{"ok": false, "error": "transfer not found"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "transfer": transfer})
}

func (d *Daemon) handleTransferAction(w http.ResponseWriter, r *http.Request) {
	if d.transfers == nil {
		writeJSON(w, http.StatusNotImplemented, map[string]any{"ok": false, "error": "download is disabled on this daemon"})
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "id is required"})
		return
	}
	var ok bool
	switch {
	case strings.HasSuffix(r.URL.Path, "/pause"):
		ok = d.transfers.Pause(id)
	case strings.HasSuffix(r.URL.Path, "/resume"):
		ok = d.transfers.Resume(id)
	default:
		writeJSON(w, http.StatusNotFound, map[string]any{"ok": false, "error": "unknown action"})
		return
	}
	if !ok {
		writeJSON(w, http.StatusConflict, map[string]any{"ok": false, "error": "transfer cannot perform this action"})
		return
	}
	transfer, _ := d.transfers.Get(id)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "transfer": transfer})
}
