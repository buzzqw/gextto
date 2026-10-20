package gextto

import (
	"path/filepath"
	"testing"
)

// TestDropRetiredEd2kKeys removes the settings of the earlier eD2k
// integrations (extto's aMule service, gx-ed2k) and keeps the current aMule
// connection details.
func TestDropRetiredEd2kKeys(t *testing.T) {
	conn, err := OpenConfigDB(filepath.Join(t.TempDir(), "gextto_config.db"))
	if err != nil {
		t.Fatalf("OpenConfigDB: %v", err)
	}
	defer conn.Close()
	if err := sqlExec(conn, "CREATE TABLE IF NOT EXISTS settings (key TEXT PRIMARY KEY, value TEXT NOT NULL);"); err != nil {
		t.Fatal(err)
	}
	seed := map[string]string{
		"amule_host": "192.168.1.161", "amule_port": "4712", "amule_password": "secret",
		"amule_enabled": "no", "amule_config_dir": "/old", "amule_incoming": "/in", "amule_tcp_port": "4662",
		"ed2k_url": "http://127.0.0.1:8899", "ed2k_token": "t", "ed2k_upnp": "true",
		"ed2k_enabled": "true", "ed2k_fallback": "false",
	}
	cfg := &Config{Settings: map[string]string{}}
	for key, value := range seed {
		if err := sqlExec(conn, "INSERT INTO settings(key,value) VALUES (?1,?2)", key, value); err != nil {
			t.Fatal(err)
		}
		cfg.Settings[key] = value
	}
	if err := dropRetiredEd2kKeys(conn, cfg); err != nil {
		t.Fatal(err)
	}
	for _, kept := range []string{"amule_host", "amule_port", "amule_password", "ed2k_enabled", "ed2k_fallback"} {
		if cfg.Settings[kept] != seed[kept] {
			t.Fatalf("%s was not kept", kept)
		}
	}
	var count int
	if err := conn.QueryRow("SELECT COUNT(*) FROM settings").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 5 || len(cfg.Settings) != 5 {
		t.Fatalf("rows left: db %d, config %d", count, len(cfg.Settings))
	}
}
