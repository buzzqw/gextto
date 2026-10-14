package gextto

import (
	"reflect"
	"testing"
)

func TestGxDLNAFoldersAutomatic(t *testing.T) {
	root := "/home/u/film"
	trash := "/home/u/SerieTVArchivio/ZZZ-Cestino"
	cfg := &Config{
		Settings:    map[string]string{},
		ArchiveRoot: &root,
		TrashPath:   &trash,
		Series: []SeriesConfig{
			{Name: "A", ArchivePath: "/home/u/SerieTVArchivio/A"},
			{Name: "B", ArchivePath: "/home/u/SerieTVArchivio/B/"},
			{Name: "C"}, // under the archive root: no extra folder
			{Name: "D", ArchivePath: "/home/u/film/Docs/D"}, // nested in the archive root
		},
	}
	folders, exclude := gxDLNAFolders(cfg)
	if want := []string{"/home/u/SerieTVArchivio", "/home/u/film"}; !reflect.DeepEqual(folders, want) {
		t.Fatalf("folders = %v, want %v", folders, want)
	}
	if want := []string{trash}; !reflect.DeepEqual(exclude, want) {
		t.Fatalf("exclude = %v, want %v", exclude, want)
	}
}

func TestGxDLNAFoldersExplicitWins(t *testing.T) {
	root := "/home/u/film"
	cfg := &Config{
		Settings:    map[string]string{"gxtorrent_dlna_folders": " /mnt/nas/Musica ; relative/skip ; /mnt/nas/Musica/Rock;/;/mnt/nas/Video "},
		ArchiveRoot: &root,
	}
	folders, exclude := gxDLNAFolders(cfg)
	if want := []string{"/mnt/nas/Musica", "/mnt/nas/Video"}; !reflect.DeepEqual(folders, want) {
		t.Fatalf("folders = %v, want %v (relative paths and / dropped, nested collapsed)", folders, want)
	}
	if len(exclude) != 0 {
		t.Fatalf("exclude = %v, want none", exclude)
	}
}
