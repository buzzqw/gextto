package gextto

// dependencies.go reports which external services a cycle can contact. The
// report is about configuration, not reachability: it never calls a provider
// and never returns a key or a URL that could carry one.

import (
	"net/http"
	"strings"
)

type declaredDependency struct {
	Name       string `json:"name"`
	Purpose    string `json:"purpose"`
	Configured bool   `json:"configured"`
	Optional   bool   `json:"optional"`
}

type dependencyReport struct {
	OK        bool                 `json:"ok"`
	Providers []declaredDependency `json:"providers"`
	Note      string               `json:"note"`
}

func declaredDependencies(cfg *Config) dependencyReport {
	if cfg == nil {
		return dependencyReport{OK: false, Note: "configuration unavailable"}
	}
	has := func(key string) bool { return strings.TrimSpace(settingsOr(cfg, key, "")) != "" }
	providers := []declaredDependency{
		{Name: "TMDB", Purpose: "titles, episodes, artwork and movie metadata", Configured: cfg.TmdbAPIKey != nil && strings.TrimSpace(*cfg.TmdbAPIKey) != "", Optional: true},
		{Name: "TheTVDB", Purpose: "alternate episode and series identifiers", Configured: cfg.TvdbAPIKey() != nil && strings.TrimSpace(*cfg.TvdbAPIKey()) != "", Optional: true},
		{Name: "TVmaze", Purpose: "series status and schedule checks", Configured: true, Optional: true},
		{Name: "Simkl", Purpose: "watchlist import and watched state", Configured: has("simkl_client_id"), Optional: true},
		{Name: "MDbList", Purpose: "watchlist and custom list import", Configured: has(mdblistAPIKeyKey), Optional: true},
		{Name: "FlareSolverr", Purpose: "indexer challenges", Configured: cfg.FlaresolverrURL != nil && strings.TrimSpace(*cfg.FlaresolverrURL) != "", Optional: true},
		{Name: "Subtitles provider", Purpose: "subtitle search and download", Configured: has(subtitleSettingAPIKey), Optional: true},
	}
	return dependencyReport{
		OK: true, Providers: providers,
		Note: "Configured means a value is saved; it does not mean the service answered. No API key, token or full URL is included in this report.",
	}
}

// DependenciesAPI implements GET /api/health/dependencies.
func DependenciesAPI(w http.ResponseWriter, r *http.Request, s *AppState) {
	jsonResponse(w, declaredDependencies(latestConfig(s)))
}
