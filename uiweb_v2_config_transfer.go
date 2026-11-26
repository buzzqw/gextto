package gextto

import (
	"fmt"
	"net/http"

	"github.com/buzzqw/gextto/internal/utils"
)

type v2ConfigTransferView struct {
	Message  string
	Error    string
	Warnings []string
}

// V2ConfigImport previews or applies a configuration document pasted in the
// Maintenance page. The API endpoints remain available for scripts; this is
// the same validation and the same apply path, with a human-readable answer.
func V2ConfigImport(w http.ResponseWriter, r *http.Request, s *AppState) {
	view := v2ConfigTransferView{}
	cfg := latestConfig(s)
	plan, err := parseConfigImport([]byte(r.FormValue("document")), cfg, cfg.Indexers)
	if err != nil {
		view.Error = utils.RedactURLSecrets(err.Error())
		v2Render(w, http.StatusOK, "v2_config_transfer_result", view, s)
		return
	}
	view.Warnings = plan.warnings
	if r.FormValue("step") == "apply_import" {
		if err := applyConfigImportPlan(s, plan); err != nil {
			view.Error = utils.RedactURLSecrets(err.Error())
		} else {
			view.Message = fmt.Sprintf("Configurazione importata: %d sezioni applicate.", len(plan.settings))
		}
	} else {
		view.Message = fmt.Sprintf("Anteprima: %d sezioni pronte, nessuna modifica scritta.", len(plan.settings))
	}
	v2Render(w, http.StatusOK, "v2_config_transfer_result", view, s)
}
