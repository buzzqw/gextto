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
	if got := splitFormList("a, b|c\nd"); !reflect.DeepEqual(got, []string{"a", "b", "c", "d"}) {
		t.Fatalf("splitFormList = %v", got)
	}
	if got := splitFormList("  "); len(got) != 0 {
		t.Fatalf("splitFormList(blank) = %v", got)
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

func TestUICategoryManageAddEditRemove(t *testing.T) {
	d := standaloneTestDaemon(t, "", true)
	d.opts.Settings.Set(setupCompleteKey, "true")
	post := func(values url.Values) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8890/ui/category-manage", strings.NewReader(values.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		d.handleUICategoryManage(rec, req)
		return rec
	}
	if rec := post(url.Values{"op": {"add"}, "name": {"Movies"}, "savepath": {"/srv/media/movies"}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("add status = %d", rec.Code)
	}
	if got := d.categoriesSnapshot()["Movies"]; got != "/srv/media/movies" {
		t.Fatalf("added category path = %q", got)
	}
	if rec := post(url.Values{"op": {"edit"}, "name": {"Movies"}, "savepath": {"/srv/media/films"}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("edit status = %d", rec.Code)
	}
	if got := d.categoriesSnapshot()["Movies"]; got != "/srv/media/films" {
		t.Fatalf("edited category path = %q", got)
	}
	// The settings page shows the category and its path in an editable row.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8890/ui/settings", nil)
	d.renderSettings(rec, req, "", false)
	if body := rec.Body.String(); !strings.Contains(body, `value="Movies"`) || !strings.Contains(body, `value="/srv/media/films"`) {
		t.Fatalf("settings page does not show the category: %s", body)
	}
	if rec := post(url.Values{"op": {"remove"}, "name": {"Movies"}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("remove status = %d", rec.Code)
	}
	if _, ok := d.categoriesSnapshot()["Movies"]; ok {
		t.Fatal("category not removed")
	}
	// A blank name is refused instead of creating an unnamed category.
	if rec := post(url.Values{"op": {"add"}, "name": {"  "}}); !strings.Contains(rec.Header().Get("Location"), "ok=0") {
		t.Fatalf("blank name must be refused: %q", rec.Header().Get("Location"))
	}
}

func TestUICategoryManageOnlyStandalone(t *testing.T) {
	d := &Daemon{opts: Options{Mode: ModeManaged}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8890/ui/category-manage", strings.NewReader("op=add&name=Movies"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	d.handleUICategoryManage(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("managed status = %d, want 404", rec.Code)
	}
}

func TestCategoryEditorOnlyStandalone(t *testing.T) {
	render := func(standalone bool) string {
		d := &Daemon{}
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8890/ui/detail?tab=general", nil)
		d.renderUI(rec, req, "detail", uiDetailData{Hash: "h", Name: "n", Tab: "general", Standalone: standalone})
		return rec.Body.String()
	}
	if strings.Contains(render(false), `name="category"`) {
		t.Fatal("managed detail must not show the category editor")
	}
	if !strings.Contains(render(true), `name="category"`) {
		t.Fatal("standalone detail must show the category editor")
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
