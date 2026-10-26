package gextto

// uiweb_v2_archive_sql.go renders the "Archive SQL maintenance" tab in
// Maintenance. The tab lists the operations bundled from scripts/sql, runs a
// preview first and only then offers the deletion of the matching rows.
// Language-aware operations use the language chosen in the panel.

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/buzzqw/gextto/internal/logging"
)

type v2ArchiveSQLOperation struct {
	ID            string
	Title         string
	Description   string
	Destructive   bool
	NeedsLanguage bool
}

type v2ArchiveSQLCategory struct {
	Title      string
	Operations []v2ArchiveSQLOperation
}

type v2ArchiveSQLLanguage struct {
	Code     string
	Label    string
	Selected bool
}

type v2ArchiveSQLView struct {
	Categories            []v2ArchiveSQLCategory
	Languages             []v2ArchiveSQLLanguage
	SelectedLanguage      string
	SelectedLanguageLabel string
	Error                 string
	DryRun                bool
}

type v2ArchiveSQLResult struct {
	ID            string
	Title         string
	Description   string
	SQL           string
	DeleteSQL     string
	Destructive   bool
	NeedsLanguage bool
	LanguageCode  string
	LanguageLabel string
	CanDelete     bool
	Limited       bool
	Total         int64
	Shown         int
	Deleted       int64
	DeletedOK     bool
	Error         string
	Columns       []string
	Rows          [][]string
	DryRun        bool
}

func v2ArchiveSQLSelectedLanguage(r *http.Request) string {
	code := strings.ToLower(strings.TrimSpace(r.FormValue("language")))
	if _, ok := archiveSQLLanguageByCode(code); !ok {
		return "ita"
	}
	return code
}

func v2ArchiveSQLLanguageLabel(code string) string {
	if language, ok := archiveSQLLanguageByCode(code); ok {
		return language.Label
	}
	return code
}

func v2ArchiveSQLViewFrom(s *AppState, r *http.Request) v2ArchiveSQLView {
	view := v2ArchiveSQLView{}
	if s != nil && s.cfg != nil {
		view.DryRun = s.cfg.DryRun
	}
	selected := v2ArchiveSQLSelectedLanguage(r)
	view.SelectedLanguage = selected
	view.SelectedLanguageLabel = v2ArchiveSQLLanguageLabel(selected)
	for _, language := range ArchiveSQLLanguages() {
		view.Languages = append(view.Languages, v2ArchiveSQLLanguage{
			Code:     language.Code,
			Label:    language.Label,
			Selected: language.Code == selected,
		})
	}
	operations, err := LoadArchiveSQLOperations()
	if err != nil {
		logging.Error("archive sql operations unavailable", "error", err)
		view.Error = "Elenco delle operazioni SQL non disponibile."
		return view
	}
	for _, operation := range operations {
		category := operation.Category
		if category == "" {
			category = "Archivio"
		}
		index := -1
		for i := range view.Categories {
			if view.Categories[i].Title == category {
				index = i
				break
			}
		}
		if index < 0 {
			view.Categories = append(view.Categories, v2ArchiveSQLCategory{Title: category})
			index = len(view.Categories) - 1
		}
		view.Categories[index].Operations = append(view.Categories[index].Operations, v2ArchiveSQLOperation{
			ID:            operation.ID,
			Title:         operation.Title,
			Description:   operation.Description,
			Destructive:   operation.Destructive,
			NeedsLanguage: operation.NeedsLanguage,
		})
	}
	return view
}

func v2ArchiveSQLOperationByID(id string) (ArchiveSQLOperation, bool) {
	operations, err := LoadArchiveSQLOperations()
	if err != nil {
		return ArchiveSQLOperation{}, false
	}
	for _, operation := range operations {
		if operation.ID == id {
			return operation, true
		}
	}
	return ArchiveSQLOperation{}, false
}

func v2ArchiveSQLResultFor(operation ArchiveSQLOperation, language string) v2ArchiveSQLResult {
	result := v2ArchiveSQLResult{
		ID:            operation.ID,
		Title:         operation.Title,
		Description:   operation.Description,
		SQL:           operation.PreviewSQL,
		DeleteSQL:     operation.DeleteSQL,
		Destructive:   operation.Destructive,
		NeedsLanguage: operation.NeedsLanguage,
	}
	if operation.NeedsLanguage {
		result.LanguageCode = language
		result.LanguageLabel = v2ArchiveSQLLanguageLabel(language)
	}
	return result
}

func v2ArchiveSQLPreviewResult(ctx context.Context, s *AppState, operation ArchiveSQLOperation, language string) v2ArchiveSQLResult {
	result := v2ArchiveSQLResultFor(operation, language)
	if s == nil || s.archive == nil {
		result.Error = "Archivio non disponibile."
		return result
	}
	previewCtx, cancel := context.WithTimeout(ctx, archiveSQLTimeout)
	defer cancel()
	preview, err := s.archive.PreviewArchiveSQL(previewCtx, operation, language)
	if err != nil {
		logging.Error(fmt.Sprintf("❌ Could not preview the archive maintenance «%s»: %v", operation.ID, err))
		result.Error = "Anteprima non riuscita."
		return result
	}
	result.Columns = preview.Columns
	result.Rows = preview.Rows
	result.Total = preview.Total
	result.Shown = preview.Shown
	result.Limited = preview.Limited
	dryRun := s != nil && s.cfg != nil && s.cfg.DryRun
	result.CanDelete = operation.Destructive && !dryRun && preview.Total > 0
	result.DryRun = dryRun
	return result
}

// V2ArchiveSQLPreview runs the read-only query of one operation and renders the
// result table. It never modifies the archive.
func V2ArchiveSQLPreview(w http.ResponseWriter, r *http.Request, s *AppState) {
	operation, ok := v2ArchiveSQLOperationByID(strings.TrimSpace(r.FormValue("op")))
	dict, eng := v2Dictionaries(s)
	if !ok {
		result := v2ArchiveSQLResult{Error: "Operazione non valida."}
		v2Render(w, http.StatusOK, "v2_archive_sql_result", result, dict, eng)
		return
	}
	language := v2ArchiveSQLSelectedLanguage(r)
	v2Render(w, http.StatusOK, "v2_archive_sql_result", v2ArchiveSQLPreviewResult(r.Context(), s, operation, language), dict, eng)
}

// V2ArchiveSQLDelete deletes the rows of a destructive operation after the
// preview. The confirmation field is required, so a bare link or a stray form
// submission cannot trigger the deletion.
func V2ArchiveSQLDelete(w http.ResponseWriter, r *http.Request, s *AppState) {
	dict, eng := v2Dictionaries(s)
	operation, ok := v2ArchiveSQLOperationByID(strings.TrimSpace(r.FormValue("op")))
	if !ok {
		result := v2ArchiveSQLResult{Error: "Operazione non valida."}
		v2Render(w, http.StatusOK, "v2_archive_sql_result", result, dict, eng)
		return
	}
	language := v2ArchiveSQLSelectedLanguage(r)
	result := v2ArchiveSQLResultFor(operation, language)
	if s == nil || s.cfg == nil || s.cfg.DryRun {
		result.Error = "La modalità prova è attiva: l'eliminazione è disabilitata."
		v2Render(w, http.StatusOK, "v2_archive_sql_result", result, dict, eng)
		return
	}
	if !operation.Destructive {
		result.Error = "Questa operazione è di sola lettura."
		v2Render(w, http.StatusOK, "v2_archive_sql_result", result, dict, eng)
		return
	}
	if r.FormValue("confirm") != "1" {
		result.Error = "Conferma richiesta per eliminare."
		v2Render(w, http.StatusOK, "v2_archive_sql_result", result, dict, eng)
		return
	}
	if s.archive == nil {
		result.Error = "Archivio non disponibile."
		v2Render(w, http.StatusOK, "v2_archive_sql_result", result, dict, eng)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), archiveSQLTimeout)
	defer cancel()
	removed, err := s.archive.DeleteArchiveSQL(ctx, operation, language)
	if err != nil {
		logging.Error("archive sql deletion failed", "operation", operation.ID, "language", language, "error", err)
		result.Error = "Eliminazione non riuscita."
		v2Render(w, http.StatusOK, "v2_archive_sql_result", result, dict, eng)
		return
	}
	// Show the archive after the deletion: usually the same rows are gone, and
	// the counter proves that the operation touched what the preview showed.
	result = v2ArchiveSQLPreviewResult(r.Context(), s, operation, language)
	result.Deleted = removed
	result.DeletedOK = true
	v2Render(w, http.StatusOK, "v2_archive_sql_result", result, dict, eng)
}
