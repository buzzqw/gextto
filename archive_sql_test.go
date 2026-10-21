package gextto

import (
	"context"
	"strings"
	"testing"
)

func archiveSQLOperation(t *testing.T, id string) ArchiveSQLOperation {
	t.Helper()
	operations, err := LoadArchiveSQLOperations()
	if err != nil {
		t.Fatalf("LoadArchiveSQLOperations: %v", err)
	}
	for _, operation := range operations {
		if operation.ID == id {
			return operation
		}
	}
	t.Fatalf("operation %q not found", id)
	return ArchiveSQLOperation{}
}

func TestArchiveSQLOperationsAreSafe(t *testing.T) {
	operations, err := LoadArchiveSQLOperations()
	if err != nil {
		t.Fatalf("LoadArchiveSQLOperations: %v", err)
	}
	if len(operations) < 8 {
		t.Fatalf("loaded %d SQL operations, want at least 8", len(operations))
	}
	seen := map[string]bool{}
	for _, operation := range operations {
		if operation.ID == "" || operation.Title == "" || operation.Description == "" || operation.PreviewSQL == "" {
			t.Fatalf("incomplete operation: %+v", operation)
		}
		if seen[operation.ID] {
			t.Fatalf("duplicate operation id %q", operation.ID)
		}
		seen[operation.ID] = true
		if err := validateArchiveSQL(operation.PreviewSQL, false); err != nil {
			t.Fatalf("%s preview: %v", operation.ID, err)
		}
		if operation.Destructive {
			if err := validateArchiveSQL(operation.DeleteSQL, true); err != nil {
				t.Fatalf("%s delete: %v", operation.ID, err)
			}
		}
	}
	for _, id := range []string{
		"archive_older_than_6_months",
		"archive_language_older_than_6_months",
		"archive_not_language_older_than_6_months",
		"archive_older_than_1_year",
		"archive_empty_title",
		"archive_recent",
		"archive_duplicate_listings",
		"archive_age_distribution",
		"archive_health",
	} {
		if !seen[id] {
			t.Fatalf("missing operation %q", id)
		}
	}
	languageOperation := archiveSQLOperation(t, "archive_language_older_than_6_months")
	if !languageOperation.NeedsLanguage {
		t.Fatal("language operation should declare param=language")
	}
	if archiveSQLOperation(t, "archive_older_than_6_months").NeedsLanguage {
		t.Fatal("age-only operation should not require a language")
	}
}

func TestArchiveSQLValidationRejectsDangerousStatements(t *testing.T) {
	for _, test := range []struct {
		name     string
		query    string
		deleting bool
		wantErr  bool
	}{
		{name: "select", query: "SELECT 1", wantErr: false},
		{name: "cte select", query: "WITH x AS (SELECT 1) SELECT * FROM x", wantErr: false},
		{name: "delete", query: "DELETE FROM archive WHERE id = 1", deleting: true, wantErr: false},
		{name: "cte delete", query: "WITH x AS (SELECT 1) DELETE FROM archive WHERE id = 1", deleting: true, wantErr: false},
		{name: "preview cannot delete", query: "WITH x AS (SELECT 1) DELETE FROM archive WHERE id = 1", wantErr: true},
		{name: "delete cannot select", query: "SELECT 1", deleting: true, wantErr: true},
		{name: "multiple statements", query: "SELECT 1; DROP TABLE archive", wantErr: true},
		{name: "pragma", query: "PRAGMA journal_mode=DELETE", wantErr: true},
		{name: "attach", query: "ATTACH DATABASE 'other.db' AS other", wantErr: true},
		{name: "insert", query: "INSERT INTO archive(title) VALUES ('x')", deleting: true, wantErr: true},
		{name: "update", query: "UPDATE archive SET title='x'", deleting: true, wantErr: true},
		{name: "drop", query: "DROP TABLE archive", wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := validateArchiveSQL(test.query, test.deleting)
			if (err != nil) != test.wantErr {
				t.Fatalf("validateArchiveSQL(%q, %v) = %v, wantErr %v", test.query, test.deleting, err, test.wantErr)
			}
		})
	}
}

func TestArchiveSQLPreviewAndDelete(t *testing.T) {
	archive := openTestArchive(t)
	if _, err := archive.db.Exec(`INSERT INTO archive(title,magnet,magnet_key,source,added_at) VALUES
		('Old Show ITA 1080p','magnet:?xt=urn:btih:01',gextto_magnet_key('magnet:?xt=urn:btih:01'),'feed',datetime('now','-8 months')),
		('Old Show ENG 1080p','magnet:?xt=urn:btih:02',gextto_magnet_key('magnet:?xt=urn:btih:02'),'feed',datetime('now','-8 months')),
		('Old Show 1080p','magnet:?xt=urn:btih:03',gextto_magnet_key('magnet:?xt=urn:btih:03'),'feed',datetime('now','-8 months')),
		('New Show ITA 1080p','magnet:?xt=urn:btih:04',gextto_magnet_key('magnet:?xt=urn:btih:04'),'feed',datetime('now','-1 day'))`); err != nil {
		t.Fatalf("insert fixtures: %v", err)
	}
	ctx := context.Background()

	old := archiveSQLOperation(t, "archive_older_than_6_months")
	preview, err := archive.PreviewArchiveSQL(ctx, old, "")
	if err != nil {
		t.Fatalf("preview old: %v", err)
	}
	if preview.Total != 3 || preview.Shown != 3 || preview.Limited {
		t.Fatalf("old preview = total %d, shown %d, limited %v; want 3/3/false", preview.Total, preview.Shown, preview.Limited)
	}

	nonItalian := archiveSQLOperation(t, "archive_not_language_older_than_6_months")
	preview, err = archive.PreviewArchiveSQL(ctx, nonItalian, "ita")
	if err != nil {
		t.Fatalf("preview non-Italian: %v", err)
	}
	if preview.Total != 2 {
		t.Fatalf("non-Italian preview total = %d, want 2", preview.Total)
	}
	if count, err := archive.Count(); err != nil || count != 4 {
		t.Fatalf("preview changed the archive: count %d, %v", count, err)
	}
	removed, err := archive.DeleteArchiveSQL(ctx, nonItalian, "ita")
	if err != nil || removed != 2 {
		t.Fatalf("DeleteArchiveSQL = (%d, %v), want (2, nil)", removed, err)
	}
	if count, err := archive.Count(); err != nil || count != 2 {
		t.Fatalf("after delete count = %d, %v; want 2", count, err)
	}
	if _, err := archive.db.Exec("INSERT INTO archive_fts(archive_fts) VALUES('integrity-check')"); err != nil {
		t.Fatalf("FTS integrity after delete: %v", err)
	}
}

func TestArchiveSQLDuplicateListingsKeepsTheNewestURL(t *testing.T) {
	archive := openTestArchive(t)
	if _, err := archive.db.Exec(`INSERT INTO archive(title,magnet,magnet_key,source,added_at) VALUES
		('Same Show','http://jackett/dl/1',gextto_magnet_key('http://jackett/dl/1'),'jackett:Test',datetime('now','-2 days')),
		('same show','http://jackett/dl/2',gextto_magnet_key('http://jackett/dl/2'),'jackett:Test',datetime('now','-1 day')),
		('Same Show','http://jackett/dl/3',gextto_magnet_key('http://jackett/dl/3'),'jackett:Other',datetime('now','-2 days')),
		('Same Show','magnet:?xt=urn:btih:keep',gextto_magnet_key('magnet:?xt=urn:btih:keep'),'jackett:Test',datetime('now'))`); err != nil {
		t.Fatalf("insert fixtures: %v", err)
	}
	ctx := context.Background()
	operation := archiveSQLOperation(t, "archive_duplicate_listings")
	preview, err := archive.PreviewArchiveSQL(ctx, operation, "")
	if err != nil {
		t.Fatalf("preview duplicates: %v", err)
	}
	if preview.Total != 1 || preview.Shown != 1 {
		t.Fatalf("duplicates preview = total %d, shown %d; want 1/1", preview.Total, preview.Shown)
	}
	if len(preview.Rows) != 1 || preview.Rows[0][5] != "http://jackett/dl/1" {
		t.Fatalf("duplicates preview rows = %+v, want the older URL of the Test source", preview.Rows)
	}
	removed, err := archive.DeleteArchiveSQL(ctx, operation, "")
	if err != nil || removed != 1 {
		t.Fatalf("DeleteArchiveSQL duplicates = (%d, %v), want (1, nil)", removed, err)
	}
	if count, err := archive.Count(); err != nil || count != 3 {
		t.Fatalf("count after duplicate cleanup = %d, %v; want 3", count, err)
	}
	var kept string
	if err := archive.db.QueryRow("SELECT magnet FROM archive WHERE source='jackett:Test' AND magnet LIKE 'http%'").Scan(&kept); err != nil || kept != "http://jackett/dl/2" {
		t.Fatalf("kept URL = %q, %v; want the newest", kept, err)
	}
}

func TestArchiveSQLReadOnlyOperationCannotDelete(t *testing.T) {
	archive := openTestArchive(t)
	operation := archiveSQLOperation(t, "archive_health")
	if _, err := archive.DeleteArchiveSQL(context.Background(), operation, ""); err == nil {
		t.Fatal("read-only operation should not delete")
	}
}

func TestArchiveSQLPreviewCapsRowsAndReportsTotal(t *testing.T) {
	archive := openTestArchive(t)
	tx, err := archive.db.Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	statement, err := tx.Prepare("INSERT INTO archive(title,magnet,magnet_key,source,added_at) VALUES (?1,?2,gextto_magnet_key(?2),'feed',datetime('now','-8 months'))")
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	for index := 0; index < archiveSQLMaxRows+5; index++ {
		magnet := "magnet:?xt=urn:btih:" + strings.Repeat("a", 8) + string(rune('a'+index%26)) + string(rune('a'+(index/26)%26)) + string(rune('a'+(index/676)%26))
		if _, err := statement.Exec("Old Show "+string(rune('A'+index%26))+" ITA", magnet); err != nil {
			_ = statement.Close()
			_ = tx.Rollback()
			t.Fatalf("insert %d: %v", index, err)
		}
	}
	if err := statement.Close(); err != nil {
		t.Fatalf("close statement: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
	preview, err := archive.PreviewArchiveSQL(context.Background(), archiveSQLOperation(t, "archive_older_than_6_months"), "")
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if int(preview.Total) != archiveSQLMaxRows+5 || preview.Shown != archiveSQLMaxRows || !preview.Limited {
		t.Fatalf("preview = total %d, shown %d, limited %v; want %d/%d/true", preview.Total, preview.Shown, preview.Limited, archiveSQLMaxRows+5, archiveSQLMaxRows)
	}
}

func TestArchiveSQLOperationTextsAreTranslated(t *testing.T) {
	keys := i18nCatalogKeys(t)
	operations, err := LoadArchiveSQLOperations()
	if err != nil {
		t.Fatalf("LoadArchiveSQLOperations: %v", err)
	}
	missing := map[string]bool{}
	for _, operation := range operations {
		for _, text := range []string{operation.Title, operation.Description, operation.Category} {
			if text != "" && !keys[text] {
				missing[text] = true
			}
		}
	}
	for _, header := range []string{
		"Aggiunta il", "Titolo", "Fonte", "Fascia", "Voci", "Più vecchia",
		"Più recente", "Copie", "Release totali", "Titoli vuoti", "Magnet vuoti",
		"Magnet non validi", "Hash mancanti", "Righe FTS", "Differenza FTS",
		"Orfani FTS", "0-30 giorni", "31-90 giorni", "91-180 giorni",
		"181-365 giorni", "oltre 1 anno", "(senza fonte)",
	} {
		if !keys[header] {
			missing[header] = true
		}
	}
	if len(missing) > 0 {
		texts := make([]string, 0, len(missing))
		for text := range missing {
			texts = append(texts, text)
		}
		t.Fatalf("archive SQL texts without a catalog key: %v", texts)
	}
}

func TestArchiveSQLLanguageSelection(t *testing.T) {
	archive := openTestArchive(t)
	if _, err := archive.db.Exec(`INSERT INTO archive(title,magnet,magnet_key,source,added_at) VALUES
		('Old Show ITA 1080p','magnet:?xt=urn:btih:lang-01',gextto_magnet_key('magnet:?xt=urn:btih:lang-01'),'feed',datetime('now','-8 months')),
		('Old Show ENG 1080p','magnet:?xt=urn:btih:lang-02',gextto_magnet_key('magnet:?xt=urn:btih:lang-02'),'feed',datetime('now','-8 months')),
		('Old Show ENG 720p','magnet:?xt=urn:btih:lang-03',gextto_magnet_key('magnet:?xt=urn:btih:lang-03'),'feed',datetime('now','-8 months')),
		('Old Show FRA 1080p','magnet:?xt=urn:btih:lang-04',gextto_magnet_key('magnet:?xt=urn:btih:lang-04'),'feed',datetime('now','-8 months')),
		('Old Show 1080p','magnet:?xt=urn:btih:lang-05',gextto_magnet_key('magnet:?xt=urn:btih:lang-05'),'feed',datetime('now','-8 months'))`); err != nil {
		t.Fatalf("insert fixtures: %v", err)
	}
	ctx := context.Background()
	languageOperation := archiveSQLOperation(t, "archive_language_older_than_6_months")
	notLanguageOperation := archiveSQLOperation(t, "archive_not_language_older_than_6_months")

	for _, test := range []struct {
		language string
		want     int64
	}{
		{language: "ita", want: 1},
		{language: "eng", want: 2},
		{language: "fra", want: 1},
		{language: "", want: 1}, // default Italian
	} {
		preview, err := archive.PreviewArchiveSQL(ctx, languageOperation, test.language)
		if err != nil {
			t.Fatalf("language %q: %v", test.language, err)
		}
		if preview.Total != test.want {
			t.Fatalf("language %q preview total = %d, want %d", test.language, preview.Total, test.want)
		}
	}
	for _, test := range []struct {
		language string
		want     int64
	}{
		{language: "ita", want: 4},
		{language: "eng", want: 3},
		{language: "fra", want: 4},
	} {
		preview, err := archive.PreviewArchiveSQL(ctx, notLanguageOperation, test.language)
		if err != nil {
			t.Fatalf("not language %q: %v", test.language, err)
		}
		if preview.Total != test.want {
			t.Fatalf("not language %q preview total = %d, want %d", test.language, preview.Total, test.want)
		}
	}
	if _, err := archive.PreviewArchiveSQL(ctx, languageOperation, "xx"); err == nil {
		t.Fatal("invalid language should fail")
	}
}

func TestArchiveSQLLanguageDelete(t *testing.T) {
	archive := openTestArchive(t)
	if _, err := archive.db.Exec(`INSERT INTO archive(title,magnet,magnet_key,source,added_at) VALUES
		('Old Show ITA 1080p','magnet:?xt=urn:btih:ldel-01',gextto_magnet_key('magnet:?xt=urn:btih:ldel-01'),'feed',datetime('now','-8 months')),
		('Old Show ENG 1080p','magnet:?xt=urn:btih:ldel-02',gextto_magnet_key('magnet:?xt=urn:btih:ldel-02'),'feed',datetime('now','-8 months')),
		('Old Show ENG 720p','magnet:?xt=urn:btih:ldel-03',gextto_magnet_key('magnet:?xt=urn:btih:ldel-03'),'feed',datetime('now','-8 months'))`); err != nil {
		t.Fatalf("insert fixtures: %v", err)
	}
	removed, err := archive.DeleteArchiveSQL(context.Background(), archiveSQLOperation(t, "archive_language_older_than_6_months"), "eng")
	if err != nil || removed != 2 {
		t.Fatalf("language delete = (%d, %v), want (2, nil)", removed, err)
	}
	if count, err := archive.Count(); err != nil || count != 1 {
		t.Fatalf("after language delete count = %d, %v; want 1", count, err)
	}
}

func TestArchiveSQLLanguagesAllSelectable(t *testing.T) {
	languages := ArchiveSQLLanguages()
	if len(languages) < 10 {
		t.Fatalf("only %d languages available", len(languages))
	}
	seen := map[string]bool{}
	for _, language := range languages {
		if language.Code == "" || language.Label == "" {
			t.Fatalf("incomplete language: %+v", language)
		}
		if len(language.tokens) == 0 {
			t.Fatalf("language %q has no tokens", language.Code)
		}
		if seen[language.Code] {
			t.Fatalf("duplicate language code %q", language.Code)
		}
		seen[language.Code] = true
	}
	archive := openTestArchive(t)
	tx, err := archive.db.Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	for _, language := range languages {
		magnet := "magnet:?xt=urn:btih:all-lang-" + language.Code
		if _, err := tx.Exec(
			"INSERT INTO archive(title,magnet,magnet_key,source,added_at) VALUES (?1,?2,gextto_magnet_key(?2),'feed',datetime('now','-8 months'))",
			"Old Show "+language.tokens[0]+" 1080p", magnet,
		); err != nil {
			_ = tx.Rollback()
			t.Fatalf("insert %s: %v", language.Code, err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
	operation := archiveSQLOperation(t, "archive_language_older_than_6_months")
	for _, language := range languages {
		preview, err := archive.PreviewArchiveSQL(context.Background(), operation, language.Code)
		if err != nil {
			t.Fatalf("language %s: %v", language.Code, err)
		}
		if preview.Total != 1 {
			t.Fatalf("language %s preview total = %d, want 1", language.Code, preview.Total)
		}
	}
}

func TestArchiveSQLLanguageRawMarker(t *testing.T) {
	archive := openTestArchive(t)
	if _, err := archive.db.Exec(`INSERT INTO archive(title,magnet,magnet_key,source,added_at) VALUES
		('Old Show [IT] 1080p','magnet:?xt=urn:btih:raw-01',gextto_magnet_key('magnet:?xt=urn:btih:raw-01'),'feed',datetime('now','-8 months'))`); err != nil {
		t.Fatalf("insert fixture: %v", err)
	}
	preview, err := archive.PreviewArchiveSQL(context.Background(), archiveSQLOperation(t, "archive_language_older_than_6_months"), "ita")
	if err != nil || preview.Total != 1 {
		t.Fatalf("raw marker preview = (%d, %v), want (1, nil)", preview.Total, err)
	}
}
