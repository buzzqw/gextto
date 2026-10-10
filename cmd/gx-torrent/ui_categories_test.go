package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
)

func TestTagHelpers(t *testing.T) {
	if got := mergeTagNames([]string{"a", "b"}, []string{"b", "c", " "}); !reflect.DeepEqual(got, []string{"a", "b", "c"}) {
		t.Fatalf("mergeTagNames = %v", got)
	}
	if got := dropTagNames([]string{"a", "b", "c"}, []string{"b"}); !reflect.DeepEqual(got, []string{"a", "c"}) {
		t.Fatalf("dropTagNames = %v", got)
	}
	if got := qbitList("a, b|c\nd"); !reflect.DeepEqual(got, []string{"a", "b", "c", "d"}) {
		t.Fatalf("qbitList = %v", got)
	}
	if got := qbitList("  "); len(got) != 0 {
		t.Fatalf("qbitList(blank) = %v", got)
	}
}

func TestUICategoryRefusedInManaged(t *testing.T) {
	d := &Daemon{opts: Options{Mode: ModeManaged}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8890/ui/category",
		strings.NewReader(url.Values{"hash": {"abc"}, "category": {"movies"}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	d.handleUICategory(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if !strings.Contains(loc, "ok=0") || !strings.Contains(loc, "standalone") {
		t.Fatalf("managed must refuse: %q", loc)
	}
}

func TestCategoriesAndTagsSnapshot(t *testing.T) {
	d := standaloneTestDaemon(t, "", true)
	d.createCategory("movies", "/srv/media/movies")
	d.createCategory("tv", "/srv/media/tv")
	d.createTags([]string{"sonarr", "radarr"})

	cats := d.categoriesSnapshot()
	if cats["movies"] != "/srv/media/movies" || cats["tv"] != "/srv/media/tv" {
		t.Fatalf("categories = %v", cats)
	}
	tags := d.tagsSnapshot()
	if !reflect.DeepEqual(tags, []string{"radarr", "sonarr"}) {
		t.Fatalf("tags = %v", tags)
	}
	d.removeCategories([]string{"movies"})
	if _, ok := d.categoriesSnapshot()["movies"]; ok {
		t.Fatal("category not removed")
	}
	d.deleteTags([]string{"radarr"})
	if got := d.tagsSnapshot(); !reflect.DeepEqual(got, []string{"sonarr"}) {
		t.Fatalf("tags after delete = %v", got)
	}
}
