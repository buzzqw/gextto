package gextto

// web_handlers_torrent_diag.go adds backend-agnostic torrent diagnostics and
// selective download:
//
//   - GET  /api/torrents/{hash}/pieces          per-piece runs (anacrolix)
//   - GET  /api/torrents/{hash}/pieces/runs     alias
//   - POST /api/torrents/{hash}/selective       apply a file-selection profile
//
// Piece diagnostics are exposed only by backends that can report them; others
// answer 409 instead of fabricating data. Selective download is built on the
// existing Files + SetFilePriorities contract, so it works on every backend
// that implements them.

import (
	"net/http"
	"path/filepath"
	"strings"

	"github.com/buzzqw/gextto/internal/models"
)

// TorrentPieces implements GET /api/torrents/{hash}/pieces.
func TorrentPieces(w http.ResponseWriter, r *http.Request, s *AppState) {
	hash := pathParam(r, "hash")
	inspector, ok := s.activeEngine().(TorrentPieceInspector)
	if !ok {
		jsonError(w, http.StatusConflict, "diagnostica pezzi non disponibile per il backend attivo")
		return
	}
	runs, found, err := inspector.PieceRuns(hash)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !found {
		jsonError(w, http.StatusNotFound, "torrent not found")
		return
	}
	total := 0
	for _, run := range runs {
		total += run.End - run.Begin + 1
	}
	jsonResponse(w, map[string]any{"ok": true, "runs": runs, "pieces": total})
}

// selectiveInput is the body of POST /api/torrents/{hash}/selective.
type selectiveInput struct {
	// Profile is one of "all", "video" or "skip_extras" (default "video").
	Profile string `json:"profile"`
}

// TorrentSelective implements POST /api/torrents/{hash}/selective.
func TorrentSelective(w http.ResponseWriter, r *http.Request, s *AppState) {
	hash := pathParam(r, "hash")
	var input selectiveInput
	if !gh3DecodeOptionalJSON(w, r, &input) {
		return
	}
	profile := strings.ToLower(strings.TrimSpace(input.Profile))
	if profile == "" {
		profile = "video"
	}
	files, ok, err := s.activeEngine().Files(hash)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !ok {
		jsonError(w, http.StatusNotFound, "torrent not found")
		return
	}
	priorities := selectivePriorities(files, profile)
	if priorities == nil {
		jsonError(w, http.StatusBadRequest, "profilo sconosciuto: usa all, video o skip_extras")
		return
	}
	applied, err := s.activeEngine().SetFilePriorities(hash, priorities)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	jsonResponse(w, map[string]any{
		"ok":      true,
		"profile": profile,
		"files":   len(files),
		"applied": applied,
	})
}

// selectivePriorities turns a profile name into a priority per file.
// Normal = 1, skip = 0, main video = 6 (matches the UI's own scale).
func selectivePriorities(files []models.FileView, profile string) []int32 {
	priorities := make([]int32, len(files))
	switch profile {
	case "all":
		for index := range priorities {
			priorities[index] = 1
		}
	case "video":
		mainIndex := -1
		var mainSize int64 = -1
		for index, file := range files {
			if isSelectiveExtra(file.Path) {
				continue
			}
			if isVideoExtension(file.Path) && file.Size > mainSize {
				mainSize = file.Size
				mainIndex = index
			}
		}
		for index, file := range files {
			switch {
			case isSelectiveExtra(file.Path):
				priorities[index] = 0
			case index == mainIndex:
				priorities[index] = 6
			case isVideoExtension(file.Path) || isSelectiveSubtitle(file.Path):
				priorities[index] = 1
			default:
				priorities[index] = 0
			}
		}
	case "skip_extras":
		for index, file := range files {
			if isSelectiveExtra(file.Path) {
				priorities[index] = 0
			} else {
				priorities[index] = 1
			}
		}
	default:
		return nil
	}
	return priorities
}

var selectiveSubtitleExtensions = map[string]struct{}{
	".srt": {}, ".ass": {}, ".ssa": {}, ".sub": {}, ".vtt": {},
}

var selectiveExtraExtensions = map[string]struct{}{
	".nfo": {}, ".sfv": {}, ".txt": {}, ".url": {}, ".exe": {}, ".bat": {},
	".jpg": {}, ".jpeg": {}, ".png": {}, ".gif": {}, ".db": {},
}

func isSelectiveSubtitle(path string) bool {
	_, ok := selectiveSubtitleExtensions[strings.ToLower(filepath.Ext(path))]
	return ok
}

func isSelectiveExtra(path string) bool {
	lowered := strings.ToLower(filepath.Base(path))
	if strings.Contains(lowered, "sample") {
		return true
	}
	_, ok := selectiveExtraExtensions[strings.ToLower(filepath.Ext(path))]
	return ok
}
