package torrent

import (
	"reflect"
	"testing"

	"github.com/buzzqw/gextto/internal/gxcore/internal/metainfo"
)

func TestParseContentLayout(t *testing.T) {
	for in, want := range map[string]string{
		"": ContentLayoutOriginal, "original": ContentLayoutOriginal,
		"Subfolder": ContentLayoutSubfolder, "create_subfolder": ContentLayoutSubfolder,
		"NOSUBFOLDER": ContentLayoutNoSubfolder, "dont_create_subfolder": ContentLayoutNoSubfolder,
	} {
		if got, ok := ParseContentLayout(in); !ok || got != want {
			t.Errorf("ParseContentLayout(%q) = %q, %v", in, got, ok)
		}
	}
	if _, ok := ParseContentLayout("flat"); ok {
		t.Error("an unknown layout must be refused")
	}
}

func TestLayoutPaths(t *testing.T) {
	multi := &metainfo.Info{Name: "Show", Files: []metainfo.File{
		{Path: "Show/a.mkv", Length: 1},
		{Path: "Show/.pad/0", Length: 8, Padding: true},
		{Path: "Show/sub/b.srt", Length: 1},
	}}
	single := &metainfo.Info{Name: "film.mkv", Files: []metainfo.File{{Path: "film.mkv", Length: 1}}}

	if got := layoutPaths(multi, ContentLayoutNoSubfolder); !reflect.DeepEqual(got, []string{"a.mkv", ".pad/0", "sub/b.srt"}) {
		t.Fatalf("NoSubfolder multi = %v", got)
	}
	if got := layoutPaths(single, ContentLayoutSubfolder); !reflect.DeepEqual(got, []string{"film/film.mkv"}) {
		t.Fatalf("Subfolder single = %v", got)
	}
	// Layouts that change nothing return nil: the paths stay the metainfo's.
	for _, c := range []struct {
		info   *metainfo.Info
		layout string
	}{
		{multi, ContentLayoutOriginal},
		{multi, ContentLayoutSubfolder},
		{single, ContentLayoutNoSubfolder},
		{single, ""},
	} {
		if got := layoutPaths(c.info, c.layout); got != nil {
			t.Errorf("%s on %s = %v, want nil", c.layout, c.info.Name, got)
		}
	}
	noExt := &metainfo.Info{Name: "README", Files: []metainfo.File{{Path: "README", Length: 1}}}
	if got := layoutPaths(noExt, ContentLayoutSubfolder); !reflect.DeepEqual(got, []string{"README/README"}) {
		t.Fatalf("Subfolder without extension = %v", got)
	}
}

func TestApplyContentLayoutRecordsThePaths(t *testing.T) {
	tor := &torrent{id: "x", session: &Session{}, contentLayout: ContentLayoutNoSubfolder}
	info := &metainfo.Info{Name: "Pack", Files: []metainfo.File{
		{Path: "Pack/e1.mkv", Length: 1},
		{Path: "Pack/e2.mkv", Length: 1},
	}}
	if !tor.applyContentLayout(info) {
		t.Fatal("the layout must apply to a multi-file magnet")
	}
	if info.Files[0].Path != "e1.mkv" || !reflect.DeepEqual(tor.filePaths, []string{"e1.mkv", "e2.mkv"}) {
		t.Fatalf("files = %+v, filePaths = %v", info.Files, tor.filePaths)
	}
	tor2 := &torrent{id: "y", session: &Session{}}
	if tor2.applyContentLayout(info) {
		t.Fatal("no layout must change nothing")
	}
}
