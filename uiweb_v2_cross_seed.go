package gextto

import (
	"errors"
	"net/http"
	"strings"

	"github.com/buzzqw/gextto/internal/utils"
)

type v2CrossSeedView struct {
	Result crossSeedResult
	Error  string
	Status string
}

// V2CrossSeedDiscover renders the read-only cross-seed result in the modal used
// by the series and movie pages. The JSON API remains available to scripts.
func V2CrossSeedDiscover(w http.ResponseWriter, r *http.Request, s *AppState) {
	view := v2CrossSeedView{}
	hash := strings.TrimSpace(r.FormValue("hash"))
	result, err := discoverCrossSeed(r.Context(), s.db, latestConfig(s), hash)
	switch {
	case err == nil:
		view.Result = result
	case errors.Is(err, errCrossSeedTargetNotFound):
		view.Error = "Nessun torrent completato con questo infohash."
	case canonicalInfoHash(hash) == "":
		view.Error = "Infohash non valido."
	case strings.Contains(err.Error(), "no enabled indexer"):
		view.Error = "Nessun indexer abilitato configurato."
	default:
		view.Error = "Ricerca cross-seed non riuscita."
	}
	v2Render(w, http.StatusOK, "v2_cross_seed_modal", view, s)
}

// V2CrossSeedSeed runs the explicit seeding action from the cross-seed modal.
// The link of the chosen copy is kept server-side: only the source and title
// travel back from the row the operator clicked.
func V2CrossSeedSeed(w http.ResponseWriter, r *http.Request, s *AppState) {
	view := v2CrossSeedView{}
	confirmed := strings.EqualFold(strings.TrimSpace(r.FormValue("confirmed")), "on") ||
		strings.EqualFold(strings.TrimSpace(r.FormValue("confirmed")), "true")
	result, err := crossSeedStart(r.Context(), s,
		strings.TrimSpace(r.FormValue("hash")),
		strings.TrimSpace(r.FormValue("source")),
		strings.TrimSpace(r.FormValue("title")), confirmed)
	switch {
	case err == nil:
		switch result["status"] {
		case "seeding":
			view.Status = "Questa copia sta seminando dalla copia già in libreria: non è stato scaricato nulla."
		case "already_seeding":
			view.Status = "Questa copia sta già seminando."
		default:
			view.Status = "Il torrent è rimasto in pausa: i file locali non corrispondono a questo torrent."
		}
	case errors.Is(err, errCrossSeedHnRConfirmation):
		view.Error = "Conferma il rischio di hit-and-run per seminare questa copia."
	case errors.Is(err, errCrossSeedTargetNotFound):
		view.Error = "Nessun torrent completato con questo infohash."
	default:
		view.Error = "Impossibile seminare questa copia: " + utils.RedactURLSecrets(err.Error())
	}
	if target, targetErr := crossSeedTargetFor(s.db, r.FormValue("hash")); targetErr == nil {
		view.Result.Target = target
	}
	v2Render(w, http.StatusOK, "v2_cross_seed_modal", view, s)
}
