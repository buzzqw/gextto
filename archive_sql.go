package gextto

// archive_sql.go is the small engine behind the "Archive SQL maintenance"
// panel. It loads the curated .sql files from scripts/sql (embedded at build
// time), validates them and runs them against the release archive. It is not a
// general SQL console: only the bundled files are executable, a preview must be
// a single read-only SELECT/WITH statement and a deletion must be a single
// DELETE statement (possibly introduced by a WITH clause).

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/buzzqw/gextto/internal/logging"
)

//go:embed scripts/sql/*.sql
var archiveSQLFiles embed.FS

const (
	archiveSQLDir     = "scripts/sql"
	archiveSQLMaxRows = 200
	// archiveSQLTimeout bounds one preview or one delete. The expensive non-Italian
	// preview scans 600k rows in about ten seconds here; the limit leaves room for
	// a loaded or slower machine and stops a runaway query from holding the single
	// archive connection.
	archiveSQLTimeout = 120 * time.Second
)

// ArchiveSQLLanguage is one language the UI can select for the language-aware
// operations. Tokens are matched as words after the title is normalised; raw
// markers are matched against the lower-case title as they are (bracketed
// forms such as [it] cannot be distinguished from the English word "it" after
// normalisation).
type ArchiveSQLLanguage struct {
	Code  string
	Label string

	tokens []string
	raw    []string
}

var archiveSQLLanguages = []ArchiveSQLLanguage{
	{Code: "ita", Label: "Italiano", tokens: []string{"ita", "italian", "italiano", "italiana", "italia"}, raw: []string{"[it]", "(it)", "it+", "it|"}},
	{Code: "eng", Label: "English", tokens: []string{"eng", "english"}, raw: []string{"[en]", "(en)", "en+", "en|"}},
	{Code: "fra", Label: "Français", tokens: []string{"fra", "french", "francais", "français", "vff", "vostfr"}, raw: []string{"[fr]", "(fr)", "fr+", "fr|"}},
	{Code: "deu", Label: "Deutsch", tokens: []string{"ger", "deu", "german", "deutsch"}, raw: []string{"[de]", "(de)", "de+", "de|"}},
	{Code: "spa", Label: "Español", tokens: []string{"spa", "esp", "spanish", "espanol", "español"}, raw: []string{"[es]", "(es)", "es+", "es|"}},
	{Code: "por", Label: "Português", tokens: []string{"por", "portuguese", "portugues", "português", "pt"}, raw: []string{"[pt]", "(pt)", "pt+", "pt|"}},
	{Code: "rus", Label: "Русский", tokens: []string{"rus", "russian"}, raw: []string{"[ru]", "(ru)", "ru+", "ru|"}},
	{Code: "jpn", Label: "日本語", tokens: []string{"jpn", "japanese"}, raw: []string{"[ja]", "(ja)", "ja+", "ja|"}},
	{Code: "kor", Label: "한국어", tokens: []string{"kor", "korean"}, raw: []string{"[ko]", "(ko)", "ko+", "ko|"}},
	{Code: "zho", Label: "中文", tokens: []string{"chi", "zho", "chinese"}, raw: []string{"[zh]", "(zh)", "zh+", "zh|"}},
	{Code: "pol", Label: "Polski", tokens: []string{"pol", "polish"}, raw: []string{"[pl]", "(pl)", "pl+", "pl|"}},
	{Code: "nld", Label: "Nederlands", tokens: []string{"dut", "nld", "dutch"}, raw: []string{"[nl]", "(nl)", "nl+", "nl|"}},
	{Code: "swe", Label: "Svenska", tokens: []string{"swe", "swedish"}, raw: []string{"[sv]", "(sv)", "sv+", "sv|"}},
	{Code: "nor", Label: "Norsk", tokens: []string{"nor", "norwegian"}, raw: []string{"[no]", "(no)", "no+", "no|"}},
	{Code: "dan", Label: "Dansk", tokens: []string{"dan", "danish"}, raw: []string{"[da]", "(da)", "da+", "da|"}},
	{Code: "fin", Label: "Suomi", tokens: []string{"fin", "finnish"}, raw: []string{"[fi]", "(fi)", "fi+", "fi|"}},
	{Code: "tur", Label: "Türkçe", tokens: []string{"tur", "turkish"}, raw: []string{"[tr]", "(tr)", "tr+", "tr|"}},
}

// ArchiveSQLLanguages returns a copy of the languages offered by the UI.
func ArchiveSQLLanguages() []ArchiveSQLLanguage {
	return append([]ArchiveSQLLanguage(nil), archiveSQLLanguages...)
}

func archiveSQLLanguageByCode(code string) (ArchiveSQLLanguage, bool) {
	code = strings.ToLower(strings.TrimSpace(code))
	for _, language := range archiveSQLLanguages {
		if language.Code == code {
			return language, true
		}
	}
	return ArchiveSQLLanguage{}, false
}

// archiveSQLLanguageParams validates a code and turns its marker lists into
// the JSON arrays bound to :tokens and :raw. The default is Italian, which
// keeps the old "non-Italian" behaviour when the caller omits the language.
func archiveSQLLanguageParams(code string) (tokensJSON, rawJSON, normalized string, err error) {
	normalized = strings.ToLower(strings.TrimSpace(code))
	if normalized == "" {
		normalized = "ita"
	}
	language, ok := archiveSQLLanguageByCode(normalized)
	if !ok {
		return "", "", normalized, fmt.Errorf("invalid language: %s", code)
	}
	tokens, err := json.Marshal(language.tokens)
	if err != nil {
		return "", "", normalized, err
	}
	raw, err := json.Marshal(language.raw)
	if err != nil {
		return "", "", normalized, err
	}
	return string(tokens), string(raw), normalized, nil
}

// ArchiveSQLOperation is one maintenance operation: a preview query and,
// optionally, the matching delete query.
type ArchiveSQLOperation struct {
	ID            string
	Title         string
	Description   string
	Category      string
	Order         int
	Destructive   bool
	NeedsLanguage bool
	PreviewSQL    string
	DeleteSQL     string
}

// ArchiveSQLPreview is the read-only result shown before a deletion.
type ArchiveSQLPreview struct {
	Columns []string
	Rows    [][]string
	Total   int64
	Shown   int
	Limited bool
}

// LoadArchiveSQLOperations returns the operations bundled from scripts/sql.
func LoadArchiveSQLOperations() ([]ArchiveSQLOperation, error) {
	return loadArchiveSQLOperations(archiveSQLFiles, archiveSQLDir)
}

func loadArchiveSQLOperations(fsys fs.FS, dir string) ([]ArchiveSQLOperation, error) {
	matches, err := fs.Glob(fsys, dir+"/*.select.sql")
	if err != nil {
		return nil, fmt.Errorf("listing the SQL scripts: %w", err)
	}
	operations := make([]ArchiveSQLOperation, 0, len(matches))
	for _, selectPath := range matches {
		raw, err := fs.ReadFile(fsys, selectPath)
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", selectPath, err)
		}
		meta, previewSQL, err := parseArchiveSQLFile(string(raw))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", selectPath, err)
		}
		previewSQL = trimArchiveSQL(previewSQL)
		if err := validateArchiveSQL(previewSQL, false); err != nil {
			return nil, fmt.Errorf("%s: %w", selectPath, err)
		}
		id := strings.TrimSuffix(path.Base(selectPath), ".select.sql")
		operation := ArchiveSQLOperation{
			ID:            id,
			Title:         meta["title"],
			Description:   meta["description"],
			Category:      meta["category"],
			NeedsLanguage: strings.EqualFold(strings.TrimSpace(meta["param"]), "language"),
			PreviewSQL:    previewSQL,
		}
		if operation.NeedsLanguage && (!strings.Contains(previewSQL, ":tokens") || !strings.Contains(previewSQL, ":raw")) {
			return nil, fmt.Errorf("%s: an operation with param=language must use :tokens and :raw", selectPath)
		}
		if !operation.NeedsLanguage && strings.Contains(previewSQL, ":tokens") {
			return nil, fmt.Errorf("%s: :tokens requires param=language", selectPath)
		}
		if operation.Title == "" {
			operation.Title = strings.ReplaceAll(id, "_", " ")
		}
		if operation.Category == "" {
			operation.Category = "Archivio"
		}
		if order, err := strconv.Atoi(meta["order"]); err == nil {
			operation.Order = order
		}

		deletePath := dir + "/" + id + ".delete.sql"
		deleteRaw, err := fs.ReadFile(fsys, deletePath)
		switch {
		case err == nil:
			deleteSQL := trimArchiveSQL(string(deleteRaw))
			if err := validateArchiveSQL(deleteSQL, true); err != nil {
				return nil, fmt.Errorf("%s: %w", deletePath, err)
			}
			if operation.NeedsLanguage && (!strings.Contains(deleteSQL, ":tokens") || !strings.Contains(deleteSQL, ":raw")) {
				return nil, fmt.Errorf("%s: an operation with param=language must use :tokens and :raw", deletePath)
			}
			if !operation.NeedsLanguage && strings.Contains(deleteSQL, ":tokens") {
				return nil, fmt.Errorf("%s: :tokens requires param=language", deletePath)
			}
			operation.Destructive = true
			operation.DeleteSQL = deleteSQL
		case errors.Is(err, fs.ErrNotExist):
			// Read-only operation.
		default:
			return nil, fmt.Errorf("reading %s: %w", deletePath, err)
		}

		operations = append(operations, operation)
	}
	sort.Slice(operations, func(i, j int) bool {
		if operations[i].Order != operations[j].Order {
			return operations[i].Order < operations[j].Order
		}
		return operations[i].Title < operations[j].Title
	})
	return operations, nil
}

// parseArchiveSQLFile extracts the `-- gextto:key=value` header and returns
// the SQL body. Only the header is removed; ordinary SQL comments stay.
func parseArchiveSQLFile(raw string) (map[string]string, string, error) {
	meta := map[string]string{}
	lines := strings.Split(raw, "\n")
	body := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "-- gextto:") {
			key, value, ok := strings.Cut(strings.TrimPrefix(trimmed, "-- gextto:"), "=")
			if !ok {
				return nil, "", fmt.Errorf("invalid header: %s", trimmed)
			}
			meta[strings.TrimSpace(key)] = strings.TrimSpace(value)
			continue
		}
		body = append(body, line)
	}
	sqlText := strings.TrimSpace(strings.Join(body, "\n"))
	if sqlText == "" {
		return nil, "", errors.New("empty SQL script")
	}
	return meta, sqlText, nil
}

func trimArchiveSQL(query string) string {
	return strings.TrimSpace(strings.TrimRight(strings.TrimSpace(query), "; \t\r\n"))
}

// validateArchiveSQL accepts exactly one statement. Preview statements must be
// SELECT/WITH; delete statements must reach a top-level DELETE (possibly after
// a WITH). Statements that could change the schema or attach another database
// are refused even if a future bundled file contains them.
func validateArchiveSQL(query string, deleteStatement bool) error {
	query = trimArchiveSQL(query)
	if query == "" {
		return errors.New("empty SQL script")
	}
	if strings.Contains(query, ";") {
		return errors.New("only single statements are allowed, with no intermediate ';'")
	}
	keywords := sqlTopLevelKeywords(query)
	if len(keywords) == 0 {
		return errors.New("unrecognised SQL statement")
	}
	for _, keyword := range keywords {
		switch keyword {
		case "ATTACH", "DETACH", "PRAGMA", "VACUUM", "CREATE", "DROP", "ALTER", "INSERT", "UPDATE", "REPLACE":
			return fmt.Errorf("statement not allowed: %s", keyword)
		}
	}
	if !deleteStatement {
		if keywords[0] != "SELECT" && keywords[0] != "WITH" {
			return errors.New("the preview must be a SELECT or a WITH ... SELECT")
		}
		for _, keyword := range keywords {
			if keyword == "DELETE" {
				return errors.New("the preview cannot contain a DELETE")
			}
		}
		return nil
	}
	if keywords[0] != "DELETE" && keywords[0] != "WITH" {
		return errors.New("the delete must be a DELETE or a WITH ... DELETE")
	}
	for _, keyword := range keywords {
		if keyword == "DELETE" {
			return nil
		}
	}
	return errors.New("the delete does not contain a DELETE")
}

// sqlTopLevelKeywords returns the SQL words outside parentheses, strings and
// comments. It is deliberately small: the engine only needs to distinguish a
// read-only statement from a deletion, not to parse SQL.
func sqlTopLevelKeywords(query string) []string {
	keywords := []string{}
	depth := 0
	for i := 0; i < len(query); {
		switch c := query[i]; {
		case c == '\'':
			i++
			for i < len(query) {
				if query[i] == '\'' {
					if i+1 < len(query) && query[i+1] == '\'' {
						i += 2
						continue
					}
					i++
					break
				}
				i++
			}
		case c == '"':
			i++
			for i < len(query) && query[i] != '"' {
				i++
			}
			if i < len(query) {
				i++
			}
		case c == '-' && i+1 < len(query) && query[i+1] == '-':
			for i < len(query) && query[i] != '\n' {
				i++
			}
		case c == '/' && i+1 < len(query) && query[i+1] == '*':
			i += 2
			for i+1 < len(query) && !(query[i] == '*' && query[i+1] == '/') {
				i++
			}
			if i+1 < len(query) {
				i += 2
			}
		case c == '(':
			depth++
			i++
		case c == ')':
			if depth > 0 {
				depth--
			}
			i++
		case depth == 0 && isArchiveSQLWordStart(c):
			start := i
			for i < len(query) && isArchiveSQLWordPart(query[i]) {
				i++
			}
			keywords = append(keywords, strings.ToUpper(query[start:i]))
		default:
			i++
		}
	}
	return keywords
}

func isArchiveSQLWordStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func isArchiveSQLWordPart(c byte) bool {
	return isArchiveSQLWordStart(c) || (c >= '0' && c <= '9') || c == '$'
}

// PreviewArchiveSQL runs the read-only query and returns up to archiveSQLMaxRows
// rows plus the total number of matching rows.
func (a *Archive) PreviewArchiveSQL(ctx context.Context, operation ArchiveSQLOperation, language string) (ArchiveSQLPreview, error) {
	if a == nil || a.db == nil {
		return ArchiveSQLPreview{}, errors.New("archive not available")
	}
	query := trimArchiveSQL(operation.PreviewSQL)
	if err := validateArchiveSQL(query, false); err != nil {
		return ArchiveSQLPreview{}, err
	}
	queryArgs, _, err := archiveSQLQueryArgs(operation, language)
	if err != nil {
		return ArchiveSQLPreview{}, err
	}
	// One scan, not two: the window function returns the number of rows the
	// preview query would produce, while the outer LIMIT keeps only the first
	// archiveSQLMaxRows. On the 600k-row archive this halves the cost of the
	// non-Italian preview (the expensive part is the title normalisation).
	const totalColumn = "__gextto_archive_sql_total_7f1c"
	rows, err := a.db.QueryContext(
		ctx,
		"SELECT *, COUNT(*) OVER () AS "+totalColumn+" FROM ("+query+") LIMIT "+strconv.Itoa(archiveSQLMaxRows),
		queryArgs...,
	)
	if err != nil {
		return ArchiveSQLPreview{}, fmt.Errorf("preview: %w", err)
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		return ArchiveSQLPreview{}, err
	}
	if len(columns) == 0 {
		return ArchiveSQLPreview{}, errors.New("preview without columns")
	}
	// Drop the helper column from the table shown to the user.
	dataColumns := len(columns) - 1
	result := ArchiveSQLPreview{Columns: append([]string(nil), columns[:dataColumns]...), Rows: [][]string{}}
	for rows.Next() {
		values := make([]any, len(columns))
		destinations := make([]any, len(columns))
		for index := range values {
			destinations[index] = &values[index]
		}
		if err := rows.Scan(destinations...); err != nil {
			return ArchiveSQLPreview{}, fmt.Errorf("reading the preview: %w", err)
		}
		if result.Total == 0 {
			result.Total = archiveSQLInt64(values[dataColumns])
		}
		row := make([]string, dataColumns)
		for index := 0; index < dataColumns; index++ {
			row[index] = archiveSQLValue(values[index])
		}
		result.Rows = append(result.Rows, row)
	}
	if err := rows.Err(); err != nil {
		return ArchiveSQLPreview{}, fmt.Errorf("reading the preview: %w", err)
	}
	result.Shown = len(result.Rows)
	result.Limited = result.Total > int64(result.Shown)
	return result, nil
}

// DeleteArchiveSQL runs the paired DELETE in one transaction and returns the
// number of removed archive rows.
func (a *Archive) DeleteArchiveSQL(ctx context.Context, operation ArchiveSQLOperation, language string) (int64, error) {
	if a == nil || a.db == nil {
		return 0, errors.New("archive not available")
	}
	if !operation.Destructive || strings.TrimSpace(operation.DeleteSQL) == "" {
		return 0, errors.New("read-only operation")
	}
	query := trimArchiveSQL(operation.DeleteSQL)
	if err := validateArchiveSQL(query, true); err != nil {
		return 0, err
	}
	queryArgs, normalizedLanguage, err := archiveSQLQueryArgs(operation, language)
	if err != nil {
		return 0, err
	}
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	result, err := tx.ExecContext(ctx, query, queryArgs...)
	if err != nil {
		return 0, fmt.Errorf("delete: %w", err)
	}
	removed, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	// A large DELETE leaves a large WAL; truncate it before returning so the
	// maintenance operation does not leave write amplification for later.
	if err := a.Checkpoint(); err != nil {
		logging.Warn(fmt.Sprintf("⚠️ Archive maintenance «%s»: could not compact the database log afterwards: %v", operation.ID, err))
	}
	languageNote := ""
	if normalizedLanguage != "" {
		languageNote = " in " + normalizedLanguage
	}
	logging.Info(fmt.Sprintf("🧹 Archive maintenance «%s» removed %s%s", operation.ID, countLabel(removed, "release", "releases"), languageNote))
	return removed, nil
}

// archiveSQLQueryArgs binds the two marker arrays only for language-aware
// operations. Other operations receive no named arguments at all, so a
// malformed script cannot smuggle a parameter into the query.
func archiveSQLQueryArgs(operation ArchiveSQLOperation, language string) ([]any, string, error) {
	if !operation.NeedsLanguage {
		return nil, "", nil
	}
	tokens, raw, normalized, err := archiveSQLLanguageParams(language)
	if err != nil {
		return nil, normalized, err
	}
	return []any{sql.Named("tokens", tokens), sql.Named("raw", raw)}, normalized, nil
}

func archiveSQLInt64(value any) int64 {
	switch typed := value.(type) {
	case int64:
		return typed
	case int:
		return int64(typed)
	case []byte:
		parsed, _ := strconv.ParseInt(string(typed), 10, 64)
		return parsed
	case string:
		parsed, _ := strconv.ParseInt(typed, 10, 64)
		return parsed
	default:
		return 0
	}
}

func archiveSQLValue(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	case []byte:
		return string(typed)
	case int64:
		return strconv.FormatInt(typed, 10)
	case float64:
		return strconv.FormatFloat(typed, 'g', -1, 64)
	case time.Time:
		return typed.Format(time.RFC3339)
	default:
		return fmt.Sprint(typed)
	}
}
