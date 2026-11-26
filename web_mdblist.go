package gextto

import (
	"net/http"

	"github.com/buzzqw/gextto/internal/utils"
)

// MDbListStatus implements GET /api/mdblist/status.
func MDbListStatus(w http.ResponseWriter, r *http.Request, s *AppState) {
	cfg := latestConfig(s)
	jsonResponse(w, map[string]any{
		"ok":         true,
		"configured": settingsOr(cfg, mdblistAPIKeyKey, "") != "",
		"list_set":   settingsOr(cfg, mdblistListIDKey, "") != "",
	})
}

// MDbListSettings implements GET and POST /api/mdblist/settings.
func MDbListSettings(w http.ResponseWriter, r *http.Request, s *AppState) {
	if r.Method == http.MethodGet {
		cfg := latestConfig(s)
		jsonResponse(w, map[string]any{
			"ok": true,
			"values": map[string]string{
				mdblistAPIKeyKey: "", // a secret is never echoed back
				mdblistListIDKey: settingsOr(cfg, mdblistListIDKey, ""),
			},
		})
		return
	}
	var input SettingsPatch
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	status, body := saveProviderSettings(s, input, []string{mdblistAPIKeyKey, mdblistListIDKey})
	if status != http.StatusOK {
		message, _ := body["error"].(string)
		jsonError(w, status, message)
		return
	}
	jsonResponse(w, body)
}

// MDbListTest implements POST /api/mdblist/test: it reads the available lists
// with the saved key without changing the configuration or the library.
func MDbListTest(w http.ResponseWriter, r *http.Request, s *AppState) {
	client := (&MDbListClient{}).FromSettings(latestConfig(s).Settings)
	if !client.Configured() {
		jsonError(w, http.StatusConflict, "MDbList is not configured")
		return
	}
	lists, err := client.Lists(r.Context())
	if err != nil {
		jsonError(w, http.StatusBadGateway, utils.RedactURLSecrets(err.Error()))
		return
	}
	jsonResponse(w, map[string]any{"ok": true, "lists": lists})
}
