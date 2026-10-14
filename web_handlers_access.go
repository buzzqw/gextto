package gextto

// Access secrets: the Gextto API key and the gx-torrent token. Both are shared
// secrets the operator must paste into a script, the TUI, a calendar app or the
// daemon configuration; the UI offers a button that generates a fresh random
// value so a weak, hand-typed key is never used.

import (
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"strings"

	"github.com/buzzqw/gextto/internal/logging"
)

// generatableAccessKeys are the settings the UI can fill with a fresh random
// value. They are access secrets, not ordinary configuration.
var generatableAccessKeys = map[string]bool{
	"auth_api_key":    true,
	"gxtorrent_token": true,
}

// GenerateAccessKey implements POST /api/access/generate-key: it creates a
// strong random value for one of the access secrets, stores it and returns it
// once, so the operator can copy it. The gx-torrent token is passed to the
// daemon at launch, so a new value applies at the next gx-torrent restart.
func GenerateAccessKey(w http.ResponseWriter, r *http.Request, s *AppState) {
	var input struct {
		Key string `json:"key"`
	}
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	key := strings.TrimSpace(input.Key)
	if !generatableAccessKeys[key] {
		jsonError(w, http.StatusBadRequest, "questa chiave non si genera automaticamente")
		return
	}
	value, err := randomAccessKey()
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "generazione della chiave non riuscita")
		return
	}
	if err := SaveSetting(s.cfg.DataDir, key, value); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	logging.Info("access key generated", "key", key)
	jsonResponse(w, map[string]any{
		"ok":               true,
		"key":              key,
		"value":            value,
		"restart_required": key == "gxtorrent_token",
	})
}

// randomAccessKey returns a URL-safe random secret: 32 random bytes encoded as
// 43 base64 characters, safe in a header, a query string or a config file.
func randomAccessKey() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
