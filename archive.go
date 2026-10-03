package gextto

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/buzzqw/gextto/internal/models"
	"github.com/buzzqw/gextto/internal/utils"
)

// Archive is the durable catalogue of every release that passed through the
// daemon, backed by its own SQLite database with an FTS5 index over titles.
type Archive struct {
	db   *sql.DB
	path string
}

// Close closes the underlying SQLite connection (see Database.Close).
func (a *Archive) Close() error {
	if a == nil || a.db == nil {
		return nil
	}
	return a.db.Close()
}

// RowCount returns the number of rows stored in the archive database.
func (a *Archive) RowCount() int64 {
	if a == nil {
		return 0
	}
	return ConnectionRowCount(a.db)
}

// ArchiveEntry is one row of the archive with the parsed release attached.
type ArchiveEntry struct {
	ID           int64           `json:"id"`
	Title        string          `json:"title"`
	Magnet       string          `json:"magnet"`
	Source       string          `json:"source"`
	QualityScore int64           `json:"quality_score"`
	AddedAt      string          `json:"added_at"`
	Release      *models.Release `json:"release,omitempty"`
}

// ArchivePage is a page of archive entries plus pagination metadata.
type ArchivePage struct {
	Items []ArchiveEntry `json:"items"`
	Total int64          `json:"total"`
	Page  int            `json:"page"`
	Pages int            `json:"pages"`
}

// archiveSchema mirrors the batch executed by `Archive::open` verbatim.
const archiveSchema = `CREATE TABLE IF NOT EXISTS archive (id INTEGER PRIMARY KEY, title TEXT NOT NULL, magnet TEXT NOT NULL UNIQUE, magnet_hash TEXT, source TEXT, quality_score INTEGER, added_at TEXT NOT NULL); CREATE INDEX IF NOT EXISTS idx_archive_title ON archive(title); CREATE INDEX IF NOT EXISTS idx_archive_added ON archive(added_at DESC); CREATE VIRTUAL TABLE IF NOT EXISTS archive_fts USING fts5(title, content='archive', content_rowid='id'); CREATE TRIGGER IF NOT EXISTS archive_fts_ai AFTER INSERT ON archive BEGIN INSERT INTO archive_fts(rowid,title) VALUES (new.id,new.title); END; CREATE TRIGGER IF NOT EXISTS archive_fts_ad AFTER DELETE ON archive BEGIN INSERT INTO archive_fts(archive_fts,rowid,title) VALUES('delete',old.id,old.title); END; CREATE TRIGGER IF NOT EXISTS archive_fts_au AFTER UPDATE OF title ON archive BEGIN INSERT INTO archive_fts(archive_fts,rowid,title) VALUES('delete',old.id,old.title); INSERT INTO archive_fts(rowid,title) VALUES(new.id,new.title); END;`

// OpenArchive opens (creating if needed) the archive database at path.
func OpenArchive(path string) (*Archive, error) {
	db, err := OpenSQLite(path)
	if err != nil {
		return nil, err
	}
	if err := HardenConnection(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	if _, err := db.Exec(archiveSchema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("archive schema: %w", err)
	}
	var archiveCount int64
	if err := db.QueryRow("SELECT COUNT(*) FROM archive").Scan(&archiveCount); err != nil {
		_ = db.Close()
		return nil, err
	}
	var indexedCount int64
	if err := db.QueryRow("SELECT COUNT(*) FROM archive_fts").Scan(&indexedCount); err != nil {
		_ = db.Close()
		return nil, err
	}
	if archiveCount != indexedCount {
		if _, err := db.Exec("INSERT INTO archive_fts(archive_fts) VALUES ('rebuild')"); err != nil {
			_ = db.Close()
			return nil, err
		}
	}
	// Ricerca case-insensitive per hash (`lower(COALESCE(magnet_hash,''))`):
	// un indice di espressione evita la scansione completa.
	if _, err := db.Exec("CREATE INDEX IF NOT EXISTS idx_archive_magnet_lower ON archive(lower(COALESCE(magnet_hash,'')));"); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &Archive{db: db, path: path}, nil
}

// SaveBatch inserts releases, ignoring magnets/URLs already archived.
func (a *Archive) SaveBatch(releases []models.Release, cfg *Config) error {
	// Spezza il batch in transazioni più piccole: un'unica transazione da
	// migliaia di righe con l'indice FTS5 generava un WAL enorme (osservato
	// ~458 MB) e una finestra di scrittura molto lunga. Con chunk da 1000
	// righe il WAL resta nell'ordine di pochi MB e un crash recupera un
	// insieme di transazioni più piccolo (l'atomicità resta per chunk).
	for start := 0; start < len(releases); start += 1000 {
		end := start + 1000
		if end > len(releases) {
			end = len(releases)
		}
		if err := a.saveChunk(releases[start:end], cfg); err != nil {
			return err
		}
		// Trunca il WAL tra i chunk: la write amplification FTS5 non si
		// accumula fino al riavvio.
		_ = a.Checkpoint()
	}
	return nil
}

// saveChunk writes one 1000-row transaction.
func (a *Archive) saveChunk(releases []models.Release, cfg *Config) error {
	tx, err := a.db.Begin()
	if err != nil {
		return err
	}
	for i := range releases {
		r := &releases[i]
		// RSS sources such as Jackett can expose only a `.torrent` URL.
		// Do not collapse all those releases into one empty unique magnet.
		source := r.Magnet
		if strings.TrimSpace(source) == "" {
			if r.TorrentURL != nil {
				source = *r.TorrentURL
			} else {
				source = ""
			}
		}
		if strings.TrimSpace(source) == "" {
			continue
		}
		var hash any
		if value, ok := utils.MagnetHash(source); ok {
			hash = value
		}
		if _, err := tx.Exec("INSERT OR IGNORE INTO archive(title,magnet,magnet_hash,source,quality_score,added_at) VALUES (?1,?2,?3,?4,?5,datetime('now'))", r.Title, source, hash, r.Source, cfg.ReleaseScore(r)); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

// QuickCheck runs `PRAGMA quick_check` (see `QuickCheck` in database.go).
func (a *Archive) QuickCheck() ([]string, error) {
	return QuickCheck(a.db)
}

// RemoveHash removes every archived listing for a rejected infohash. Indexers
// can publish the same torrent under conflicting season/title metadata, so
// leaving the rows visible would make the bad release return on every archive
// pass even after it has been blocklisted.
func (a *Archive) RemoveHash(hash string) (int, error) {
	result, err := a.db.Exec(
		"DELETE FROM archive WHERE lower(COALESCE(magnet_hash,''))=lower(?1)",
		hash,
	)
	if err != nil {
		return 0, err
	}
	removed, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	return int(removed), nil
}

// CanonicalizeTorrentURL replaces an ephemeral `.torrent` URL with its durable
// infohash magnet once the torrent has been downloaded successfully. If the
// same magnet is already archived, the obsolete URL row is simply removed.
func (a *Archive) CanonicalizeTorrentURL(torrentURL, magnet string) error {
	tx, err := a.db.Begin()
	if err != nil {
		return err
	}
	if _, err := tx.Exec(
		"DELETE FROM archive WHERE magnet=?1 AND EXISTS (SELECT 1 FROM archive WHERE magnet=?2)",
		torrentURL, magnet,
	); err != nil {
		_ = tx.Rollback()
		return err
	}
	var hash any
	if value, ok := utils.MagnetHash(magnet); ok {
		hash = value
	}
	if _, err := tx.Exec(
		"UPDATE OR IGNORE archive SET magnet=?1, magnet_hash=?2 WHERE magnet=?3",
		magnet, hash, torrentURL,
	); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

// Search returns up to 200 newest (title, magnet, source) tuples matching an
// FTS5 query. Ordering by discovery time ensures recently released episodes are
// not hidden behind older catalogue entries when a monitored title has more
// than 200 matches.
func (a *Archive) Search(query string) ([][3]string, error) {
	term := ftsQuery(query)
	if term == "" {
		return [][3]string{}, nil
	}
	rows, err := a.db.Query("SELECT archive.title,archive.magnet,COALESCE(archive.source,'archive') FROM archive JOIN archive_fts ON archive_fts.rowid=archive.id WHERE archive_fts MATCH ?1 ORDER BY archive.added_at DESC,archive.id DESC LIMIT 200", term)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	results := [][3]string{}
	for rows.Next() {
		var title, magnet, source string
		if err := rows.Scan(&title, &magnet, &source); err != nil {
			continue
		}
		results = append(results, [3]string{title, magnet, source})
	}
	return results, rows.Err()
}

// RecentEntries returns the most recent `limit` (title, magnet, source) tuples
// using the index on `added_at`: it avoids loading the whole archive (hundreds
// of thousands of rows) into memory on every feed-status computation.
func (a *Archive) RecentEntries(limit int) ([][3]string, error) {
	limit = clampInt(limit, 1, 500_000)
	rows, err := a.db.Query(
		"SELECT title,magnet,COALESCE(source,'archive') FROM archive ORDER BY added_at DESC LIMIT ?1",
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	results := [][3]string{}
	for rows.Next() {
		var title, magnet, source string
		if err := rows.Scan(&title, &magnet, &source); err != nil {
			continue
		}
		results = append(results, [3]string{title, magnet, source})
	}
	return results, rows.Err()
}

// Optimize runs VACUUM or ANALYZE (see `OptimizeConnection`).
func (a *Archive) Optimize(action string) error {
	return OptimizeConnection(a.db, action)
}

// SizeBytes returns the estimated database size in bytes.
func (a *Archive) SizeBytes() int64 {
	return ConnectionSizeBytes(a.db)
}

// Checkpoint truncates the WAL after a checkpoint (see
// `CheckpointConnection`).
func (a *Archive) Checkpoint() error {
	return CheckpointConnection(a.db)
}

// Count returns the number of archived rows.
func (a *Archive) Count() (int64, error) {
	var count int64
	if err := a.db.QueryRow("SELECT COUNT(*) FROM archive").Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

// Browse returns up to `limit` archive entries, optionally filtered by an FTS5
// query.
func (a *Archive) Browse(query string, limit int) ([]ArchiveEntry, error) {
	// FTS5 `MATCH` cannot be used inside an `OR` expression (SQLite raises
	// "unable to use function MATCH in the requested context"), so the
	// filtered and unfiltered queries are kept separate.
	term := ftsQuery(query)
	limit = clampInt(limit, 1, 500)
	if term == "" {
		rows, err := a.db.Query("SELECT id,title,magnet,COALESCE(source,'archive'),COALESCE(quality_score,0),added_at FROM archive ORDER BY added_at DESC LIMIT ?1", limit)
		if err != nil {
			return nil, err
		}
		return scanArchiveEntries(rows)
	}
	rows, err := a.db.Query("SELECT archive.id,archive.title,archive.magnet,COALESCE(archive.source,'archive'),COALESCE(archive.quality_score,0),archive.added_at FROM archive JOIN archive_fts ON archive_fts.rowid=archive.id WHERE archive_fts MATCH ?1 ORDER BY archive.added_at DESC LIMIT ?2", term, limit)
	if err != nil {
		return nil, err
	}
	return scanArchiveEntries(rows)
}

// BrowsePage returns one page of archive entries with the total count.
func (a *Archive) BrowsePage(query string, page, limit int) (*ArchivePage, error) {
	limit = clampInt(limit, 1, 500)
	if page < 1 {
		page = 1
	}
	includes, excludes := parseArchiveFilter(query)
	pageOf := func(total int64) (int, int64) {
		pages := (int(total) + limit - 1) / limit
		if pages < 1 {
			pages = 1
		}
		top := page - 1
		if top > pages-1 {
			top = pages - 1
		}
		return pages, int64(top * limit)
	}

	// Con termini positivi si usa l'indice FTS (con NOT per le esclusioni).
	if len(includes) > 0 {
		term := ftsMatchExpression(includes, excludes)
		var total int64
		if err := a.db.QueryRow(
			"SELECT COUNT(*) FROM archive JOIN archive_fts ON archive_fts.rowid=archive.id WHERE archive_fts MATCH ?1",
			term,
		).Scan(&total); err != nil {
			return nil, err
		}
		pages, offset := pageOf(total)
		rows, err := a.db.Query("SELECT archive.id,archive.title,archive.magnet,COALESCE(archive.source,'archive'),COALESCE(archive.quality_score,0),archive.added_at FROM archive JOIN archive_fts ON archive_fts.rowid=archive.id WHERE archive_fts MATCH ?1 ORDER BY archive.added_at DESC LIMIT ?2 OFFSET ?3", term, limit, offset)
		if err != nil {
			return nil, err
		}
		items, err := scanArchiveEntries(rows)
		if err != nil {
			return nil, err
		}
		return &ArchivePage{Items: items, Total: total, Page: page, Pages: pages}, nil
	}

	// Solo esclusioni (o nessun filtro): FTS5 non può iniziare con NOT, si
	// filtra la tabella con NOT LIKE.
	var whereSQL string
	var filterBinds []any
	for _, word := range excludes {
		if whereSQL == "" {
			whereSQL = " WHERE "
		} else {
			whereSQL += " AND "
		}
		filterBinds = append(filterBinds, "%"+word+"%")
		whereSQL += fmt.Sprintf("lower(title) NOT LIKE ?%d", len(filterBinds))
	}
	var total int64
	if err := a.db.QueryRow("SELECT COUNT(*) FROM archive"+whereSQL, filterBinds...).Scan(&total); err != nil {
		return nil, err
	}
	pages, offset := pageOf(total)
	filterBinds = append(filterBinds, limit, offset)
	count := len(filterBinds)
	sql := fmt.Sprintf(
		"SELECT id,title,magnet,COALESCE(source,'archive'),COALESCE(quality_score,0),added_at FROM archive%s ORDER BY added_at DESC LIMIT ?%d OFFSET ?%d",
		whereSQL, count-1, count,
	)
	rows, err := a.db.Query(sql, filterBinds...)
	if err != nil {
		return nil, err
	}
	items, err := scanArchiveEntries(rows)
	if err != nil {
		return nil, err
	}
	return &ArchivePage{Items: items, Total: total, Page: page, Pages: pages}, nil
}

// BrowseContentFilteredPage returns archive rows whose titles match the text
// query and at least one selected content filter. The regular expression and
// Unicode-script rules intentionally share titleIsContentFiltered with intake.
func (a *Archive) BrowseContentFilteredPage(query string, filters []string, page, limit int) (*ArchivePage, error) {
	if len(filters) == 0 {
		return nil, fmt.Errorf("selezionare almeno un pre-filtro")
	}
	if page < 1 {
		page = 1
	}
	limit = clampInt(limit, 1, 500)
	rows, err := a.contentFilterCandidates(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := []ArchiveEntry{}
	var total int64
	start := int64((page - 1) * limit)
	for rows.Next() {
		var entry ArchiveEntry
		if err := rows.Scan(&entry.ID, &entry.Title, &entry.Magnet, &entry.Source, &entry.QualityScore, &entry.AddedAt); err != nil {
			return nil, err
		}
		if !titleIsContentFiltered(entry.Title, filters) {
			continue
		}
		if total >= start && len(items) < limit {
			entry.Release = ParseRelease(entry.Title, entry.Magnet, "archive:"+entry.Source)
			items = append(items, entry)
		}
		total++
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	pages := int((total + int64(limit) - 1) / int64(limit))
	if pages < 1 {
		pages = 1
	}
	return &ArchivePage{Items: items, Total: total, Page: page, Pages: pages}, nil
}

// DeleteContentFiltered removes every archive row matching the text query and
// at least one selected content filter. Filters are kept explicit so bulk
// deletion cannot accidentally target the entire catalogue.
func (a *Archive) DeleteContentFiltered(query string, filters []string) (int, error) {
	if len(filters) == 0 {
		return 0, fmt.Errorf("selezionare almeno un pre-filtro")
	}
	rows, err := a.contentFilterCandidates(query)
	if err != nil {
		return 0, err
	}
	ids := []int64{}
	for rows.Next() {
		var id int64
		var title string
		var magnet, source, addedAt string
		var quality int64
		if err := rows.Scan(&id, &title, &magnet, &source, &quality, &addedAt); err != nil {
			_ = rows.Close()
			return 0, err
		}
		if titleIsContentFiltered(title, filters) {
			ids = append(ids, id)
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return 0, err
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}
	return a.DeleteIDs(ids)
}

func (a *Archive) contentFilterCandidates(query string) (*sql.Rows, error) {
	includes, excludes := parseArchiveFilter(query)
	selectSQL := "SELECT archive.id,archive.title,archive.magnet,COALESCE(archive.source,'archive'),COALESCE(archive.quality_score,0),archive.added_at FROM archive"
	if len(includes) > 0 {
		return a.db.Query(selectSQL+" JOIN archive_fts ON archive_fts.rowid=archive.id WHERE archive_fts MATCH ?1 ORDER BY archive.added_at DESC,archive.id DESC", ftsMatchExpression(includes, excludes))
	}
	if len(excludes) == 0 {
		return a.db.Query(selectSQL + " ORDER BY archive.added_at DESC,archive.id DESC")
	}
	where := make([]string, len(excludes))
	args := make([]any, len(excludes))
	for index, word := range excludes {
		where[index] = fmt.Sprintf("lower(archive.title) NOT LIKE ?%d", index+1)
		args[index] = "%" + word + "%"
	}
	return a.db.Query(selectSQL+" WHERE "+strings.Join(where, " AND ")+" ORDER BY archive.added_at DESC,archive.id DESC", args...)
}

// DeleteIDs deletes the given row ids, ignoring non-positive ids.
func (a *Archive) DeleteIDs(ids []int64) (int, error) {
	const batchSize = 500 // stay below SQLite's portable bind-variable limit
	unique := make([]int64, 0, len(ids))
	seen := make(map[int64]struct{}, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		unique = append(unique, id)
	}
	if len(unique) == 0 {
		return 0, nil
	}
	tx, err := a.db.Begin()
	if err != nil {
		return 0, err
	}
	removed := 0
	for start := 0; start < len(unique); start += batchSize {
		end := min(start+batchSize, len(unique))
		placeholders := make([]string, end-start)
		args := make([]any, end-start)
		for index, id := range unique[start:end] {
			placeholders[index] = fmt.Sprintf("?%d", index+1)
			args[index] = id
		}
		result, err := tx.Exec("DELETE FROM archive WHERE id IN ("+strings.Join(placeholders, ",")+")", args...)
		if err != nil {
			_ = tx.Rollback()
			return 0, err
		}
		count, err := result.RowsAffected()
		if err != nil {
			_ = tx.Rollback()
			return 0, err
		}
		removed += int(count)
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return removed, nil
}

// DeleteMatching removes every archive listing matching a positive title query.
// Exclusions use the same syntax as BrowsePage; at least one positive term is
// required so a typo or an exclusion-only query cannot wipe the catalogue.
func (a *Archive) DeleteMatching(query string) (int, error) {
	includes, excludes := parseArchiveFilter(query)
	if len(includes) == 0 {
		return 0, fmt.Errorf("inserire almeno una parola da cercare")
	}
	term := ftsMatchExpression(includes, excludes)
	result, err := a.db.Exec(
		"DELETE FROM archive WHERE id IN (SELECT rowid FROM archive_fts WHERE archive_fts MATCH ?1)",
		term,
	)
	if err != nil {
		return 0, err
	}
	removed, err := result.RowsAffected()
	return int(removed), err
}

// Delete removes the row matching the magnet or its infohash.
func (a *Archive) Delete(magnet string) (bool, error) {
	hash, ok := utils.MagnetHash(magnet)
	var query string
	var args []any
	if ok {
		query = "DELETE FROM archive WHERE magnet=?1 OR lower(magnet_hash)=lower(?2)"
		args = []any{magnet, hash}
	} else {
		// No usable magnet hash: match only by the exact magnet link so an
		// empty digest cannot delete unrelated rows with an empty magnet_hash.
		query = "DELETE FROM archive WHERE magnet=?1"
		args = []any{magnet}
	}
	result, err := a.db.Exec(query, args...)
	if err != nil {
		return false, err
	}
	removed, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return removed > 0, nil
}

// CleanupOlderThan deletes archive rows older than `days` days.
func (a *Archive) CleanupOlderThan(days int64) (int, error) {
	return a.CleanupOlderThanKeeping(days, 0)
}

// CleanupOlderThanKeeping deletes archive rows older than `days` days, always
// keeping at least the `keepMin` most recent rows.
func (a *Archive) CleanupOlderThanKeeping(days, keepMin int64) (int, error) {
	days = maxInt64(days, 1)
	keepMin = maxInt64(keepMin, 0)
	result, err := a.db.Exec(
		"DELETE FROM archive WHERE added_at < datetime('now', ?1) AND id NOT IN (SELECT id FROM archive ORDER BY added_at DESC LIMIT ?2)",
		fmt.Sprintf("-%d days", days), keepMin,
	)
	if err != nil {
		return 0, err
	}
	removed, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	return int(removed), nil
}

// scanArchiveEntries collects every row into an ArchiveEntry slice.
func scanArchiveEntries(rows *sql.Rows) ([]ArchiveEntry, error) {
	defer rows.Close()
	entries := []ArchiveEntry{}
	for rows.Next() {
		var entry ArchiveEntry
		if err := rows.Scan(&entry.ID, &entry.Title, &entry.Magnet, &entry.Source, &entry.QualityScore, &entry.AddedAt); err != nil {
			return nil, err
		}
		entry.Release = ParseRelease(entry.Title, entry.Magnet, "archive:"+entry.Source)
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}

// parseArchiveFilter splits the archive filter into positive and negative
// terms: `-parola` excludes, everything else (even with `+`) includes. It is a
// "facilitated regex" without real regular expressions.
func parseArchiveFilter(query string) (includes, excludes []string) {
	for _, raw := range strings.Fields(query) {
		if raw == "" {
			continue
		}
		if word, ok := strings.CutPrefix(raw, "-"); ok {
			if word != "" {
				excludes = append(excludes, asciiLower(word))
			}
		} else if word, ok := strings.CutPrefix(raw, "+"); ok {
			if word != "" {
				includes = append(includes, asciiLower(word))
			}
		} else {
			includes = append(includes, asciiLower(raw))
		}
	}
	return includes, excludes
}

// ftsMatchExpression builds an FTS5 expression where positive terms are in AND
// and exclusions in NOT. It requires at least one positive term (FTS5 does not
// accept a leading NOT).
func ftsMatchExpression(includes, excludes []string) string {
	quote := func(word string) string {
		return fmt.Sprintf("\"%s\"*", strings.ReplaceAll(word, "\"", "\"\""))
	}
	positive := make([]string, len(includes))
	for i, word := range includes {
		positive[i] = quote(word)
	}
	joined := strings.Join(positive, " AND ")
	if len(excludes) == 0 {
		return joined
	}
	negative := make([]string, len(excludes))
	for i, word := range excludes {
		negative[i] = quote(word)
	}
	return fmt.Sprintf("(%s) NOT %s", joined, strings.Join(negative, " NOT "))
}

// ftsQuery builds an FTS5 query where every whitespace-separated term is
// required. Prefix matching keeps searches useful for partial words (e.g.
// `ocea`).
func ftsQuery(query string) string {
	parts := []string{}
	for _, word := range strings.Fields(query) {
		if word == "" {
			continue
		}
		parts = append(parts, fmt.Sprintf("\"%s\"*", strings.ReplaceAll(word, "\"", "\"\"")))
	}
	return strings.Join(parts, " AND ")
}

// asciiLower mirrors the `str::to_ascii_lowercase`.
func asciiLower(value string) string {
	bytes := []byte(value)
	for i, b := range bytes {
		if b >= 'A' && b <= 'Z' {
			bytes[i] = b + ('a' - 'A')
		}
	}
	return string(bytes)
}

// clampInt mirrors the `Ord::clamp`.
func clampInt(value, low, high int) int {
	if value < low {
		return low
	}
	if value > high {
		return high
	}
	return value
}

// maxInt64 mirrors the `i64::max`.
func maxInt64(value, minimum int64) int64 {
	if value > minimum {
		return value
	}
	return minimum
}
