package gextto

import (
	"errors"
	"net/http"
	"strings"
)

type v2CrossSeedView struct {
	Result crossSeedResult
	Error  string
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
