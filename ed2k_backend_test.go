package gextto

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/amule"
	"github.com/buzzqw/gextto/internal/emuleqt"
	"github.com/buzzqw/gextto/internal/emuleqt/emuleqttest"
)

// emuleqtTestConfig points Gextto at a fake emule-qt core.
func emuleqtTestConfig(t *testing.T) (*emuleqttest.Core, *Config) {
	t.Helper()
	core := emuleqttest.Start("secret-key")
	t.Cleanup(core.Close)
	cfgValue := DefaultConfig()
	cfg := &cfgValue
	cfg.DataDir = t.TempDir()
	cfg.Settings = map[string]string{
		"ed2k_enabled": "true", "ed2k_backend": "emuleqt",
		"emuleqt_url": core.URL(), "emuleqt_api_key": "secret-key",
		"ed2k_search_timeout_secs": "5",
	}
	ed2kInvalidateHealth()
	t.Cleanup(func() {
		ed2kState.mu.Lock()
		if ed2kState.client != nil {
			_ = ed2kState.client.Close()
		}
		ed2kState.client, ed2kState.key = nil, ""
		ed2kState.mu.Unlock()
		ed2kInvalidateHealth()
	})
	return core, cfg
}

// writePartMet writes an eMule .part.met header for hash.
func writePartMet(t *testing.T, dir string, number int, hash string) {
	t.Helper()
	raw, err := hex.DecodeString(hash)
	if err != nil || len(raw) != 16 {
		t.Fatalf("bad hash %q", hash)
	}
	header := append([]byte{0xE0, 1, 2, 3, 4}, raw...)
	header = append(header, 0, 0) // the rest of a real file
	if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("%03d.part.met", number)), header, 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestEmuleqtBackendThroughGextto: with emule-qt chosen, the download list,
// the status, a search and a queued link all go to emule-qt, mapped onto the
// same shapes aMule gives; the .part file of a download is found from the
// .part.met headers of the temporary folder.
func TestEmuleqtBackendThroughGextto(t *testing.T) {
	core, cfg := emuleqtTestConfig(t)
	temp := t.TempDir()
	incoming := t.TempDir()
	hash := "00112233445566778899aabbccddeeff"
	writePartMet(t, temp, 7, hash)
	writePartMet(t, temp, 9, "ffeeddccbbaa99887766554433221100")
	core.SetDirectories(emuleqt.Directories{Incoming: incoming, Temp: []string{temp}})
	core.SetDownloads([]emuleqt.Download{{Hash: strings.ToUpper(hash), FileName: "Show.S01E02.mkv", FileSize: 100 << 20,
		CompletedSize: 40 << 20, Datarate: 2048, Status: "ready", Sources: 12, Transferring: 3, Priority: "high"}})

	downloads := Ed2kDownloads(cfg)
	if len(downloads) != 1 {
		t.Fatalf("downloads = %+v", downloads)
	}
	d := downloads[0]
	if d.Hash != hash || d.Done != 40<<20 || d.Speed != 2048 || d.Active != 3 || d.Priority != amule.PriorityHigh || d.Status != amule.StatusReady {
		t.Fatalf("download mapped as %+v", d)
	}
	if d.PartFile() != "007.part" {
		t.Fatalf("part file = %q, want 007.part from the .part.met header", d.PartFile())
	}
	if !strings.HasPrefix(d.Link, "ed2k://|file|Show.S01E02.mkv|104857600|"+hash) {
		t.Fatalf("link = %q", d.Link)
	}
	status, err := ed2kStatus(cfg)
	if err != nil || !status.Ed2kConnected || !status.KadConnected || status.ServerName != "Server A" || !strings.HasPrefix(status.Version, "emule-qt") {
		t.Fatalf("status = %+v, %v", status, err)
	}
	prefs, err := ed2kPreferences(cfg)
	if err != nil || prefs.IncomingDir != incoming || prefs.TempDir != temp {
		t.Fatalf("preferences = %+v, %v", prefs, err)
	}
	core.SetResults([]emuleqt.SearchResult{{Hash: hash, FileName: "Show.S01E02.1080p.mkv", FileSize: 1 << 30, Sources: 8}})
	results := SearchED2K(context.Background(), cfg, "Show S01E02")
	if len(results) != 1 || results[0].Sources != 8 || len(core.Searches()) == 0 {
		t.Fatalf("search results = %+v, searches %v", results, core.Searches())
	}
	link := "ed2k://|file|Show.S01E02.mkv|104857600|" + hash + "|/"
	if err := AddEd2kLink(cfg, link); err != nil || len(core.Added()) != 1 {
		t.Fatalf("add link: %v, added %v", err, core.Added())
	}
}

// TestEmuleqtWrongKeyIsAnUnreachableCore: a refused key makes the core
// count as down, like a wrong aMule password, with emule-qt's name.
func TestEmuleqtWrongKeyIsAnUnreachableCore(t *testing.T) {
	_, cfg := emuleqtTestConfig(t)
	cfg.Settings["emuleqt_api_key"] = "wrong"
	if _, err := ed2kStatus(cfg); !errors.Is(err, emuleqt.ErrAuth) {
		t.Fatalf("status with a wrong key = %v", err)
	}
	if err := ed2kCall(cfg, func(ctx context.Context, client ed2kBackend) error { return nil }); err == nil || !strings.Contains(err.Error(), "emule-qt unreachable") {
		t.Fatalf("the next call = %v, want a fast emule-qt unreachable", err)
	}
}

func TestEd2kPartFilesByHashReadsOnlyPartMetHeaders(t *testing.T) {
	dir := t.TempDir()
	writePartMet(t, dir, 3, "0123456789abcdef0123456789abcdef")
	_ = os.WriteFile(filepath.Join(dir, "004.part.met"), []byte{0x99, 0, 0}, 0o644) // not a part.met
	_ = os.WriteFile(filepath.Join(dir, "notes.part.met"), make([]byte, 30), 0o644)
	got := ed2kPartFilesByHash([]string{dir, "", filepath.Join(dir, "missing")})
	if len(got) != 1 || got["0123456789abcdef0123456789abcdef"] != 3 {
		t.Fatalf("part files = %v", got)
	}
}

func TestEmuleqtContainerPathsBecomeHostPaths(t *testing.T) {
	b := newEmuleqtBackend("http://x", "", "/data", "/srv/gextto/emuleqt/data")
	for in, want := range map[string]string{
		"/data/incoming": "/srv/gextto/emuleqt/data/incoming", "/data": "/srv/gextto/emuleqt/data",
		"/database": "/database", "/other/file": "/other/file", "": "",
	} {
		if got := b.hostPath(in); got != want {
			t.Fatalf("hostPath(%q) = %q, want %q", in, got, want)
		}
	}
}

// fakeDocker records docker commands and plays a container and images.
type fakeDocker struct {
	mu       sync.Mutex
	commands []string
	exists   bool
	running  bool
	image    string
	latest   string
	pullTo   string
	noDocker bool
	// enforced lists the images the managed preferences were set on.
	enforced []string
}

func (f *fakeDocker) run(_ context.Context, args ...string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.commands = append(f.commands, strings.Join(args, " "))
	if f.noDocker {
		return "", errEmuleqtNoDocker
	}
	switch args[0] {
	case "inspect":
		if !f.exists {
			return "Error: No such container: " + emuleqtContainer, errors.New("exit status 1")
		}
		return fmt.Sprintf("%v %s", f.running, f.image), nil
	case "image":
		if args[1] == "inspect" {
			if f.latest == "" {
				return "", errors.New("no such image")
			}
			return f.latest, nil
		}
		return "", nil
	case "pull":
		f.latest = f.pullTo
		return "", nil
	case "run":
		f.exists, f.running, f.image = true, true, args[len(args)-1]
		return "abc", nil
	case "start":
		f.running = true
	case "stop":
		f.running = false
	case "rm":
		f.exists, f.running = false, false
	}
	return "", nil
}

func (f *fakeDocker) did(prefix string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, command := range f.commands {
		if strings.HasPrefix(command, prefix) {
			n++
		}
	}
	return n
}

func managedTestConfig(t *testing.T, docker *fakeDocker, healthy func(string) error) *Config {
	t.Helper()
	previousDocker, previousHealthy, previousEnforce := emuleqtDocker, emuleqtHealthy, emuleqtEnforce
	emuleqtDocker = docker.run
	emuleqtEnforce = func(context.Context, emuleqtManaged) error {
		docker.mu.Lock()
		docker.enforced = append(docker.enforced, docker.image)
		docker.mu.Unlock()
		return nil
	}
	emuleqtRuntime.mu.Lock()
	emuleqtRuntime.enforced = ""
	emuleqtRuntime.mu.Unlock()
	emuleqtHealthy = func(context.Context, emuleqtManaged) error {
		docker.mu.Lock()
		image := docker.image
		docker.mu.Unlock()
		return healthy(image)
	}
	t.Cleanup(func() {
		emuleqtDocker, emuleqtHealthy, emuleqtEnforce = previousDocker, previousHealthy, previousEnforce
	})
	cfgValue := DefaultConfig()
	cfg := &cfgValue
	cfg.DataDir = t.TempDir()
	cfg.Settings = map[string]string{"ed2k_enabled": "true", "ed2k_backend": "emuleqt_managed"}
	return cfg
}

// TestEmuleqtManagedPreferencesFollowWhatTheCoreKnows: the choices Gextto
// sets again after every start are sent to the core by the name it reports,
// so a setting a release dropped or renamed (emule-qt's settings change
// often) cannot make the whole change fail and leave the core with its own
// engines on; the missing ones are reported.
func TestEmuleqtManagedPreferencesFollowWhatTheCoreKnows(t *testing.T) {
	core := emuleqttest.Start("secret-key")
	t.Cleanup(core.Close)
	managed := emuleqtManaged{URL: core.URL(), APIKey: "secret-key"}
	// A core that knows the eD2k switches and the engines, but not yet the
	// hard-off switch a later release adds.
	core.SetPreferences(map[string]any{
		"networkED2K": true, "kadEnabled": true, "autoConnect": true, "reconnect": true,
		"btEnabled": true, "usenetEnabled": true, "ed3kHosterEnabled": true,
	})
	err := emuleqtEnforce(context.Background(), managed)
	if err == nil || !strings.Contains(err.Error(), "disableEd2k") {
		t.Fatalf("a setting this core does not have must be reported: %v", err)
	}
	patched := core.Patched()
	if len(patched) != 1 {
		t.Fatalf("patches = %v, want one", patched)
	}
	want := map[string]any{
		"networkED2K": true, "kadEnabled": true, "autoConnect": true, "reconnect": true,
		"btEnabled": false, "usenetEnabled": false, "ed3kHosterEnabled": false,
	}
	if len(patched[0]) != len(want) {
		t.Fatalf("sent %v, want exactly %v", patched[0], want)
	}
	for key, value := range want {
		if patched[0][key] != value {
			t.Fatalf("%s = %v, want %v (sent %v)", key, patched[0][key], value, patched[0])
		}
	}

	// A core with every one of them takes the whole set, hard-off switch off.
	core.SetPreferences(map[string]any{
		"networkED2K": true, "kadEnabled": true, "autoConnect": true, "reconnect": true,
		"disableEd2k": true, "btEnabled": true, "usenetEnabled": true, "ed3kHosterEnabled": true,
	})
	if err := emuleqtEnforce(context.Background(), managed); err != nil {
		t.Fatal(err)
	}
	patched = core.Patched()
	if len(patched) != 2 || patched[1]["disableEd2k"] != false || patched[1]["btEnabled"] != false {
		t.Fatalf("second patch = %v", patched)
	}
}

// TestEmuleqtManagedStartsUpdatesAndRollsBack: the first start pulls the
// image and runs it with Gextto's folders; a day later a new image that
// answers replaces it, and one that does not is undone.
func TestEmuleqtManagedStartsUpdatesAndRollsBack(t *testing.T) {
	docker := &fakeDocker{pullTo: "sha256:v1"}
	broken := ""
	cfg := managedTestConfig(t, docker, func(image string) error {
		if image == broken {
			return errors.New("not answering")
		}
		return nil
	})
	now := time.Now()
	if err := emuleqtEnsure(cfg, now); err != nil {
		t.Fatal(err)
	}
	if docker.image != "sha256:v1" || docker.did("pull") != 1 || docker.did("run -d --name gextto-emuleqt --network host") != 1 {
		t.Fatalf("first start: %v", docker.commands)
	}
	if strings.Join(docker.enforced, ",") != "sha256:v1" {
		t.Fatalf("managed preferences set on %v, want once on v1", docker.enforced)
	}
	if !strings.Contains(strings.Join(docker.commands, "\n"), "--log-opt max-size=10m --log-opt max-file=3") {
		t.Fatalf("the container log is not bounded: %v", docker.commands)
	}
	dir := emuleqtManagedDir(cfg)
	prefs, err := os.ReadFile(filepath.Join(dir, "config", "preferences.yml"))
	state := emuleqtLoadState(dir)
	if err != nil || state.APIKey == "" || !strings.Contains(string(prefs), `apiKey: "`+state.APIKey+`"`) ||
		!strings.Contains(string(prefs), "restApiEnabled: true") || !strings.Contains(string(prefs), `listenAddress: "127.0.0.1"`) {
		t.Fatalf("preferences.yml = %s (%v)", prefs, err)
	}
	// What emule-qt needs to join eD2k and Kad by itself, and the engines
	// Gextto does not use left off (each section as emule-qt's loader reads it).
	for _, want := range []string{
		"network:\n  port: 4662\n  udpPort: 4672\n", "  networkED2K: true\n", "  startVersion: 7\n",
		"logging:\n  verbose: false\n  kadVerboseLog: false\n", "kademlia:\n  enabled: true\n",
		"  autoConnect: true\n", "  autoUpdateServerList: true\n",
		"bittorrent:\n  enabled: false\n", "usenet:\n  enabled: false\n", "  hoster:\n    enabled: false\n",
		"ed3k:\n  enabled: true\n  udpPort: 4672\n", `incomingDir: "/data/incoming"`, `- "/data/temp"`,
	} {
		if !strings.Contains(string(prefs), want) {
			t.Fatalf("preferences.yml lacks %q:\n%s", want, prefs)
		}
	}
	// The core owns the file from now on: Gextto does not write it again.
	_ = os.WriteFile(filepath.Join(dir, "config", "preferences.yml"), []byte("changed by the core\n"), 0o600)

	// Within the day nothing is pulled.
	if err := emuleqtEnsure(cfg, now.Add(time.Hour)); err != nil || docker.did("pull") != 1 {
		t.Fatalf("pulled again within the day: %v %v", err, docker.commands)
	}
	// A day later a new image that answers takes over.
	docker.pullTo = "sha256:v2"
	if err := emuleqtEnsure(cfg, now.Add(25*time.Hour)); err != nil {
		t.Fatal(err)
	}
	state = emuleqtLoadState(dir)
	if docker.image != "sha256:v2" || state.Image != "sha256:v2" || state.Previous != "sha256:v1" {
		t.Fatalf("update: running %s, state %+v", docker.image, state)
	}
	// The next one does not answer: back to v2.
	docker.pullTo, broken = "sha256:v3", "sha256:v3"
	previousWait := emuleqtHealthWaitForTest(t, 10*time.Millisecond)
	_ = previousWait
	if err := emuleqtEnsure(cfg, now.Add(50*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if docker.image != "sha256:v2" {
		t.Fatalf("after a failed update the container runs %s, want v2 back", docker.image)
	}
	// Set once per image: v1 at the start, v2 after the update, nothing new
	// after the rollback to v2.
	if strings.Join(docker.enforced, ",") != "sha256:v1,sha256:v2" {
		t.Fatalf("managed preferences set on %v", docker.enforced)
	}
	if raw, _ := os.ReadFile(filepath.Join(dir, "config", "preferences.yml")); string(raw) != "changed by the core\n" {
		t.Fatal("Gextto rewrote the core's preferences")
	}
}

// emuleqtHealthWaitForTest shortens the wait for a new container.
func emuleqtHealthWaitForTest(t *testing.T, wait time.Duration) time.Duration {
	t.Helper()
	previous := emuleqtHealthWaitDuration
	emuleqtHealthWaitDuration = wait
	t.Cleanup(func() { emuleqtHealthWaitDuration = previous })
	return previous
}

func TestEmuleqtManagedWithoutDockerSaysSo(t *testing.T) {
	docker := &fakeDocker{noDocker: true}
	cfg := managedTestConfig(t, docker, func(string) error { return nil })
	err := emuleqtEnsure(cfg, time.Now())
	if !errors.Is(err, errEmuleqtNoDocker) || !strings.Contains(err.Error(), "Docker is not installed") {
		t.Fatalf("err = %v", err)
	}
}

// TestEmuleqtManagedStopsWhenAnotherCoreIsChosen: switching back to aMule
// stops the container Gextto started (its data stays).
func TestEmuleqtManagedStopsWhenAnotherCoreIsChosen(t *testing.T) {
	docker := &fakeDocker{pullTo: "sha256:v1"}
	cfg := managedTestConfig(t, docker, func(string) error { return nil })
	if err := emuleqtEnsure(cfg, time.Now()); err != nil {
		t.Fatal(err)
	}
	cfg.Settings["ed2k_backend"] = "amule"
	emuleqtRuntime.mu.Lock()
	emuleqtRuntime.idleAt = time.Time{}
	emuleqtRuntime.mu.Unlock()
	emuleqtManagedTick(cfg, time.Now())
	if docker.running || docker.did("stop") != 1 || docker.did("rm") != 0 {
		t.Fatalf("the container was not stopped (or was removed): %v", docker.commands)
	}
}

// TestPagesNameTheEd2kCoreInUse: with emule-qt chosen the pages say
// emule-qt where they said aMule; the settings page, where both programs
// are described, and aMule's own words (amuled) are left alone.
func TestPagesNameTheEd2kCoreInUse(t *testing.T) {
	cfgValue := DefaultConfig()
	cfg := &cfgValue
	cfg.Settings = map[string]string{"ed2k_enabled": "true", "ed2k_backend": "emuleqt_managed"}
	page := []byte("<p>Scarica con aMule</p><p>amuled</p>")
	if got := string(v2Ed2kCoreNamed(cfg, "v2_ed2k", nil, page)); got != "<p>Scarica con emule-qt</p><p>amuled</p>" {
		t.Fatalf("eD2k page = %q", got)
	}
	if got := string(v2Ed2kCoreNamed(cfg, "v2_shell", v2ShellData{Page: "settings"}, page)); got != string(page) {
		t.Fatalf("the settings page was rewritten: %q", got)
	}
	cfg.Settings["ed2k_backend"] = "amule"
	if got := string(v2Ed2kCoreNamed(cfg, "v2_ed2k", nil, page)); got != string(page) {
		t.Fatalf("with aMule the page changed: %q", got)
	}
}
