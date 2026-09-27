package gextto

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestNormalizeJSONEmptyCollections(t *testing.T) {
	value := normalizeJSON(map[string]any{
		"slice": []int(nil),
		"map":   map[string]int(nil),
		"ptr":   (*int64)(nil),
		"full":  []int{1, 2},
	})
	decoded, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("normalized type = %T", value)
	}
	if slice, ok := decoded["slice"].([]any); !ok || len(slice) != 0 {
		t.Fatalf("nil slice -> %#v", decoded["slice"])
	}
	if mapped, ok := decoded["map"].(map[string]any); !ok || len(mapped) != 0 {
		t.Fatalf("nil map -> %#v", decoded["map"])
	}
	if decoded["ptr"] != nil {
		t.Fatalf("nil pointer -> %#v", decoded["ptr"])
	}
	if full, ok := decoded["full"].([]any); !ok || len(full) != 2 {
		t.Fatalf("full slice -> %#v", decoded["full"])
	}
}

func TestJSONStatusEmitsEmptyArrays(t *testing.T) {
	recorder := httptest.NewRecorder()
	jsonResponse(recorder, map[string]any{"ok": true, "items": []int(nil), "errors": map[string]int(nil)})
	var decoded map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if items, ok := decoded["items"].([]any); !ok || len(items) != 0 {
		t.Fatalf("items = %#v", decoded["items"])
	}
	if errors, ok := decoded["errors"].(map[string]any); !ok || len(errors) != 0 {
		t.Fatalf("errors = %#v", decoded["errors"])
	}
}
