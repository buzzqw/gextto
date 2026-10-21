package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDHTFeedPublishParams(t *testing.T) {
	// The page sends JSON: the selected hashes must reach the publisher.
	jsonReq := httptest.NewRequest(http.MethodPost, "/ui/dht-feed/publish", strings.NewReader(`{"name":"night","hashes":["aabbccddeeff00112233445566778899aabbccdd"]}`))
	jsonReq.Header.Set("Content-Type", "application/json")
	name, hashes, err := dhtFeedPublishParams(httptest.NewRecorder(), jsonReq)
	if err != nil || name != "night" || len(hashes) != 1 {
		t.Fatalf("json params = %q %v %v", name, hashes, err)
	}

	// The API GET form keeps the historical query parameters.
	getReq := httptest.NewRequest(http.MethodGet, "/api/v1/dht-feed/publish?name=web&hash=aa", nil)
	name, hashes, err = dhtFeedPublishParams(httptest.NewRecorder(), getReq)
	if err != nil || name != "web" || len(hashes) != 1 || hashes[0] != "aa" {
		t.Fatalf("get params = %q %v %v", name, hashes, err)
	}

	// A form post still works for scripts.
	formReq := httptest.NewRequest(http.MethodPost, "/ui/dht-feed/publish", strings.NewReader("name=form&hashes=aa,bb"))
	formReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	name, hashes, err = dhtFeedPublishParams(httptest.NewRecorder(), formReq)
	if err != nil || name != "form" || len(hashes) != 2 {
		t.Fatalf("form params = %q %v %v", name, hashes, err)
	}

	bad := httptest.NewRequest(http.MethodPost, "/api/v1/dht-feed/publish", strings.NewReader("{"))
	bad.Header.Set("Content-Type", "application/json")
	if _, _, err := dhtFeedPublishParams(httptest.NewRecorder(), bad); err == nil {
		t.Fatal("invalid JSON must fail")
	}
}

func TestDHTFeedItemsRejectsBadHash(t *testing.T) {
	if _, err := dhtFeedItemsFromTorrents(nil, []string{"not-hex"}); err == nil {
		t.Fatal("a non-hex infohash must be refused")
	}
	if _, err := dhtFeedItemsFromTorrents(nil, nil); err == nil {
		t.Fatal("an empty session must not produce an empty feed")
	}
}
