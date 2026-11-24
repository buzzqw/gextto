package gextto

import (
	"errors"
	"testing"

	"github.com/buzzqw/gextto/internal/models"
)

func TestMovieUpgradeTransactionRejectsAChangedRevision(t *testing.T) {
	db := newTestDB(t)
	const id int64 = 7
	const oldHash = "0123456789012345678901234567890123456789"
	if _, err := db.db.Exec("INSERT INTO movies(id,name,year,title,quality_score,magnet_hash,magnet_link,size_bytes) VALUES (?1,'Example Movie',2024,'Example Movie',1500,?2,'magnet:?xt=urn:btih:'||?2,100)", id, oldHash); err != nil {
		t.Fatal(err)
	}
	expected, err := db.loadMovieUpgradeBackup(db.db, id)
	if err != nil {
		t.Fatal(err)
	}

	// Simulate another approval committing after the decision was made but
	// before this transaction re-read the row.
	other, err := OpenSQLite(db.path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := other.Exec("UPDATE movies SET quality_score=2100,title='Concurrent Movie' WHERE id=?1", id); err != nil {
		_ = other.Close()
		t.Fatal(err)
	}
	if err := other.Close(); err != nil {
		t.Fatal(err)
	}

	tx, err := db.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	const newHash = "fedcba9876543210fedcba9876543210fedcba98"
	err = db.applyMovieUpgradeTx(tx, newHash, "Example Movie", 2200, "magnet:?xt=urn:btih:"+newHash, expected)
	if !errors.Is(err, errMovieUpgradeConflict) {
		t.Fatalf("upgrade error = %v, want revision conflict", err)
	}
	var backups int
	if err := tx.QueryRow("SELECT COUNT(*) FROM upgrade_backup").Scan(&backups); err != nil {
		t.Fatal(err)
	}
	if backups != 0 {
		t.Fatalf("a conflicting upgrade left %d backup rows", backups)
	}
}

func TestMovieUpgradeTransactionCommitsBackupAndUpdateTogether(t *testing.T) {
	db := newTestDB(t)
	const id int64 = 8
	const oldHash = "1111111111111111111111111111111111111111"
	if _, err := db.db.Exec("INSERT INTO movies(id,name,year,title,quality_score,magnet_hash,magnet_link,size_bytes) VALUES (?1,'Example Movie',2024,'Example Movie',1500,?2,'magnet:?xt=urn:btih:'||?2,100)", id, oldHash); err != nil {
		t.Fatal(err)
	}
	expected, err := db.loadMovieUpgradeBackup(db.db, id)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := db.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	const newHash = "2222222222222222222222222222222222222222"
	newMagnet := "magnet:?xt=urn:btih:" + newHash
	if err := db.applyMovieUpgradeTx(tx, newHash, "Example Movie", 2200, newMagnet, expected); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	var hash, magnet string
	if err := db.db.QueryRow("SELECT magnet_hash,magnet_link FROM movies WHERE id=?1", id).Scan(&hash, &magnet); err != nil {
		t.Fatal(err)
	}
	if hash != newHash || magnet != newMagnet {
		t.Fatalf("movie was not upgraded: hash=%q magnet=%q", hash, magnet)
	}
	backup, err := db.loadMovieUpgradeBackup(db.db, id)
	if err != nil {
		t.Fatal(err)
	}
	if backup.MagnetHash == nil || *backup.MagnetHash != newHash {
		t.Fatalf("current row was not upgraded: %+v", backup)
	}
	var payload string
	if err := db.db.QueryRow("SELECT payload_json FROM upgrade_backup WHERE new_hash=?1", newHash).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	backups, err := decodeUpgradeBackups(payload)
	if err != nil || len(backups) != 1 || backups[0].RowID != id || backups[0].MagnetHash == nil || *backups[0].MagnetHash != oldHash {
		t.Fatalf("upgrade backup = %#v, %v", backups, err)
	}

	// The public rollback path still restores the exact pre-upgrade row.
	release := models.Release{Magnet: newMagnet}
	if err := db.RollbackRelease(&release); err != nil {
		t.Fatal(err)
	}
	if err := db.db.QueryRow("SELECT magnet_hash FROM movies WHERE id=?1", id).Scan(&hash); err != nil {
		t.Fatal(err)
	}
	if hash != oldHash {
		t.Fatalf("rollback restored hash %q, want %q", hash, oldHash)
	}
}
