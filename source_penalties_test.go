package gextto

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestSourcePenaltyIsExplainedAndReversible(t *testing.T) {
	state := newTestAppState(t)
	if err := AddSourcePenalty(state.db, "Indexer One", "too many bad releases", 40, ""); err != nil {
		t.Fatal(err)
	}
	items, err := SourcePenalties(state.db)
	if err != nil || len(items) != 1 || items[0].Reason != "too many bad releases" || items[0].Points != 40 {
		t.Fatalf("penalties = %+v err = %v", items, err)
	}
	points, reason := SourcePenaltyPoints(state.db, "indexer one", time.Now())
	if points != 40 || !strings.Contains(reason, "too many bad releases") {
		t.Fatalf("points = %d reason = %q", points, reason)
	}
	if removed, err := RemoveSourcePenalty(state.db, items[0].ID); err != nil || !removed {
		t.Fatalf("remove: removed=%v err=%v", removed, err)
	}
	if points, _ := SourcePenaltyPoints(state.db, "Indexer One", time.Now()); points != 0 {
		t.Fatalf("points after undo = %d, want 0", points)
	}
}

func TestSourcePenaltyValidatesInputAndHonoursExpiry(t *testing.T) {
	state := newTestAppState(t)
	if err := AddSourcePenalty(state.db, "", "no source", 10, ""); err == nil {
		t.Fatal("a penalty without a source was accepted")
	}
	if err := AddSourcePenalty(state.db, "src", "", 10, ""); err == nil {
		t.Fatal("a penalty without a reason was accepted")
	}
	if err := AddSourcePenalty(state.db, "src", "bad points", 0, ""); err == nil {
		t.Fatal("a penalty with zero points was accepted")
	}
	past := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	if err := AddSourcePenalty(state.db, "src", "expired", 25, past); err != nil {
		t.Fatal(err)
	}
	if points, _ := SourcePenaltyPoints(state.db, "src", time.Now()); points != 0 {
		t.Fatalf("an expired penalty still applies: %d", points)
	}
}

func TestSourcePenaltyAPIRoundTrip(t *testing.T) {
	state := newTestAppState(t)
	body, _ := json.Marshal(SourcePenalty{Source: "indexer", Reason: "reliability", Points: 15})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/source-penalties", strings.NewReader(string(body)))
	SourcePenaltiesAPI(recorder, request, state)
	if recorder.Code != http.StatusOK {
		t.Fatalf("add -> %d: %s", recorder.Code, recorder.Body.String())
	}
	var created struct {
		Items []SourcePenalty `json:"items"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &created); err != nil || len(created.Items) != 1 {
		t.Fatalf("response = %s err = %v", recorder.Body.String(), err)
	}
	id := created.Items[0].ID
	recorder = httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodDelete, "/api/source-penalties/"+strconv.FormatInt(id, 10), nil)
	request.SetPathValue("id", strconv.FormatInt(id, 10))
	RemoveSourcePenaltyAPI(recorder, request, state)
	if recorder.Code != http.StatusOK {
		t.Fatalf("delete -> %d: %s", recorder.Code, recorder.Body.String())
	}
	if items, _ := SourcePenalties(state.db); len(items) != 0 {
		t.Fatalf("penalties after delete = %+v", items)
	}
}
