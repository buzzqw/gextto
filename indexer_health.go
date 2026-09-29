package gextto

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// ProwlarrIndexerHealth reads the indexer list and the failure status of a
// Prowlarr instance and returns one ready-to-render item per indexer, so the
// Sources view shows which indexers the manager currently considers failing
// instead of leaving an empty result unexplained.
func ProwlarrIndexerHealth(ctx context.Context, baseURL, apiKey string) ([]map[string]any, error) {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if base == "" {
		return nil, fmt.Errorf("prowlarr: url is required")
	}
	headers := map[string]string{}
	if key := strings.TrimSpace(apiKey); key != "" {
		headers["X-Api-Key"] = key
	}

	listBody, err := managerGet(ctx, base+"/api/v1/indexer", headers)
	if err != nil {
		return nil, err
	}
	var raw []map[string]any
	if err := json.Unmarshal([]byte(listBody), &raw); err != nil {
		return nil, fmt.Errorf("prowlarr: invalid indexer list: %w", err)
	}

	// The status endpoint only lists indexers with a failure entry; a fetch
	// failure here must not hide the indexer list.
	failures := map[string]string{}
	if statusBody, statusErr := managerGet(ctx, base+"/api/v1/indexerstatus", headers); statusErr == nil {
		var statuses []map[string]any
		if json.Unmarshal([]byte(statusBody), &statuses) == nil {
			for _, status := range statuses {
				id := managerScalar(status["indexerId"])
				if id == "" {
					continue
				}
				if disabledTill := strings.TrimSpace(managerScalar(status["disabledTill"])); disabledTill != "" {
					failures[id] = "in errore, disabilitato fino a " + disabledTill
				} else {
					failures[id] = "in errore"
				}
			}
		}
	}

	out := make([]map[string]any, 0, len(raw))
	for _, indexer := range raw {
		id := managerScalar(indexer["id"])
		name := strings.TrimSpace(managerScalar(indexer["name"]))
		if id == "" || name == "" {
			continue
		}
		enabled := true
		if value, ok := indexer["enable"].(bool); ok {
			enabled = value
		}
		item := map[string]any{
			"kind":    "prowlarr",
			"name":    name,
			"results": nil,
			"ok":      enabled,
		}
		if detail, failing := failures[id]; failing {
			item["ok"] = false
			item["error"] = detail
		} else if !enabled {
			item["error"] = "disabilitato nel manager"
		}
		out = append(out, item)
	}
	return out, nil
}

// managerGet performs a read-only GET against an indexer manager API.
func managerGet(ctx context.Context, endpoint string, headers map[string]string) (string, error) {
	requestCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	response, err := httpDo(requestCtx, defaultHTTPClient, http.MethodGet, endpoint, headers, nil, "")
	if err != nil {
		return "", err
	}
	if response == nil {
		return "", fmt.Errorf("manager: empty response")
	}
	defer response.Body.Close()
	if response.StatusCode >= 400 {
		return "", fmt.Errorf("manager: HTTP %d", response.StatusCode)
	}
	body, err := readLimitedBody(response.Body, maxAPIResponseBytes)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

// managerScalar renders a JSON scalar (string or number) as a trimmed string.
func managerScalar(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case float64:
		if typed == float64(int64(typed)) {
			return strconv.FormatInt(int64(typed), 10)
		}
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case json.Number:
		return typed.String()
	default:
		return ""
	}
}
