package gextto

import (
	"archive/zip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestComicInfoFromTitle(t *testing.T) {
	info := comicInfoFromTitle("Poison Ivy #041 (2025)")
	if info.Series != "Poison Ivy" || info.Number != "41" || info.Year != 2025 {
		t.Fatalf("unexpected parse: %+v", info)
	}
	info = comicInfoFromTitle("Absolute Batman Vol. 1 (2025)")
	if info.Series != "Absolute Batman Vol. 1" || info.Number != "" || info.Year != 2025 {
		t.Fatalf("unexpected parse: %+v", info)
	}
}

func writeTestCBZ(t *testing.T, path string, names ...string) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	for _, name := range names {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = entry.Write([]byte("page " + name))
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	file.Close()
}

func readZipEntry(t *testing.T, path, name string) (string, []string) {
	t.Helper()
	reader, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	names := []string{}
	content := ""
	for _, file := range reader.File {
		names = append(names, file.Name)
		if file.Name == name {
			stream, _ := file.Open()
			raw, _ := io.ReadAll(stream)
			stream.Close()
			content = string(raw)
		}
	}
	return content, names
}

func TestAddComicInfoToCBZ(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Poison Ivy 041 (2025).cbz")
	writeTestCBZ(t, path, "001.jpg", "002.jpg")
	tagDownloadedComic(path, "Poison Ivy #41 (2025)")
	content, names := readZipEntry(t, path, "ComicInfo.xml")
	if len(names) != 3 || names[0] != "001.jpg" || names[1] != "002.jpg" {
		t.Fatalf("pages not preserved: %v", names)
	}
	for _, want := range []string{"<Series>Poison Ivy</Series>", "<Number>41</Number>", "<Year>2025</Year>"} {
		if !strings.Contains(content, want) {
			t.Errorf("ComicInfo.xml lacks %s:\n%s", want, content)
		}
	}
	if page, _ := readZipEntry(t, path, "002.jpg"); page != "page 002.jpg" {
		t.Fatalf("page content changed: %q", page)
	}
	// A second pass leaves an already tagged file alone.
	changed, err := addComicInfo(path, comicInfoFromTitle("Other #1"))
	if err != nil || changed {
		t.Fatalf("existing ComicInfo.xml must be kept: changed=%v err=%v", changed, err)
	}
	leftovers, _ := filepath.Glob(filepath.Join(filepath.Dir(path), ".*gextto-tag*"))
	if len(leftovers) != 0 {
		t.Fatalf("temporary files left: %v", leftovers)
	}
}

func TestAddComicInfoSkipsOtherFormats(t *testing.T) {
	dir := t.TempDir()
	cbr := filepath.Join(dir, "x.cbr")
	if err := os.WriteFile(cbr, []byte("Rar!"), 0o644); err != nil {
		t.Fatal(err)
	}
	if changed, err := addComicInfo(cbr, comicInfoFromTitle("X #1")); changed || err != nil {
		t.Fatalf("CBR must be left alone: %v %v", changed, err)
	}
	broken := filepath.Join(dir, "broken.cbz")
	if err := os.WriteFile(broken, []byte("not a zip"), 0o644); err != nil {
		t.Fatal(err)
	}
	tagDownloadedComic(broken, "X #1")
	if raw, _ := os.ReadFile(broken); string(raw) != "not a zip" {
		t.Fatal("a broken CBZ must stay untouched")
	}
}
