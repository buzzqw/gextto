package gextto

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/buzzqw/gextto/internal/emuleqt"
	"github.com/buzzqw/gextto/internal/logging"
)

// emuleqt_managed.go runs emule-qt for Gextto, in Docker. The official image
// carries the core with every library it needs, and a project that releases
// several times a day is updated by pulling the new image and recreating the
// container, and put back by starting the previous image again — no archive
// to download, unpack and supervise by hand.
//
// The container runs on the host network (eD2k peers are told the real
// ports, IPv6 and UPnP keep working), with its configuration and data under
// Gextto's data folder. Gextto writes the core's preferences.yml before the
// first start: the REST API on 127.0.0.1 with a generated key, the GUI link
// on 127.0.0.1 too, and ports that do not collide with an aMule installed on
// the same machine. The container restarts by itself (unless-stopped), so it
// outlives a restart of Gextto, like the managed gx-torrent.

const (
	emuleqtImage     = "ghcr.io/moddermule/emuleqt"
	emuleqtImageTag  = emuleqtImage + ":latest"
	emuleqtContainer = "gextto-emuleqt"
	// emuleqtContainerData is where the image keeps incoming and temp.
	emuleqtContainerData = "/data"
	// The ports of the managed core. The P2P ports are aMule's defaults (TCP
	// 4662, UDP 4672), so a router already forwarded for aMule needs nothing
	// new; the web server (REST API) and the GUI link sit on loopback, away
	// from amuleweb (4711) and aMule's external connections (4712).
	emuleqtTCPPort = 4662
	emuleqtUDPPort = 4672
	emuleqtWebPort = 4721
	emuleqtIPCPort = 4722
	// emuleqtUpdateEvery is how often a new image is looked for;
	// emuleqtHealthWait how long a new container has to answer.
	emuleqtUpdateEvery = 24 * time.Hour
	emuleqtStateFile   = "gextto-emuleqt.json"
)

// emuleqtManaged is how Gextto reaches the managed core.
type emuleqtManaged struct {
	URL      string
	APIKey   string
	HostData string
	Dir      string
}

// emuleqtManagedState is kept in <data>/emuleqt/gextto-emuleqt.json.
type emuleqtManagedState struct {
	APIKey   string `json:"api_key"`
	IPCToken string `json:"ipc_token"`
	// Image is the image the container runs; Previous the one before the
	// last update, kept to go back to.
	Image     string    `json:"image,omitempty"`
	Previous  string    `json:"previous,omitempty"`
	CheckedAt time.Time `json:"checked_at,omitempty"`
}

// emuleqtDocker runs one docker command and returns its output. Tests
// replace it.
var emuleqtDocker = func(ctx context.Context, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
	text := strings.TrimSpace(string(out))
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return text, errEmuleqtNoDocker
		}
		if len(text) > 300 {
			text = text[len(text)-300:]
		}
		return text, fmt.Errorf("docker %s: %v: %s", args[0], err, text)
	}
	return text, nil
}

// emuleqtHealthy asks the core whether it answers; tests replace it.
var emuleqtHealthy = func(ctx context.Context, managed emuleqtManaged) error {
	client := emuleqt.New(emuleqt.Config{BaseURL: managed.URL, APIKey: managed.APIKey, Timeout: 5 * time.Second})
	if err := client.Health(ctx); err != nil {
		return err
	}
	_, err := client.Snapshot(ctx)
	return err
}

// emuleqtHealthWaitDuration is how long a new container has to answer.
var emuleqtHealthWaitDuration = 3 * time.Minute

var errEmuleqtNoDocker = errors.New("Docker is not installed: emule-qt managed by Gextto runs in Docker")

var emuleqtRuntime struct {
	mu       sync.Mutex
	busy     bool
	problem  string
	warnedAt time.Time
	idleAt   time.Time
	// amuleWarnedAt is the last warning that aMule still runs.
	amuleWarnedAt time.Time
}

func emuleqtManagedDir(cfg *Config) string { return filepath.Join(cfg.DataDir, "emuleqt") }

func emuleqtLoadState(dir string) emuleqtManagedState {
	var state emuleqtManagedState
	if raw, err := os.ReadFile(filepath.Join(dir, emuleqtStateFile)); err == nil {
		_ = json.Unmarshal(raw, &state)
	}
	return state
}

func emuleqtSaveState(dir string, state emuleqtManagedState) error {
	raw, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	tmp := filepath.Join(dir, emuleqtStateFile+".tmp")
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(dir, emuleqtStateFile))
}

func emuleqtRandomHex(bytes int) string {
	buf := make([]byte, bytes)
	_, _ = rand.Read(buf)
	return hex.EncodeToString(buf)
}

// emuleqtManagedEndpoint prepares the managed core's folders, key and
// preferences (once) and says how to reach it.
func emuleqtManagedEndpoint(cfg *Config) (emuleqtManaged, error) {
	if cfg == nil || strings.TrimSpace(cfg.DataDir) == "" {
		return emuleqtManaged{}, errors.New("the data folder is not known")
	}
	dir := emuleqtManagedDir(cfg)
	for _, sub := range []string{"config", "data/incoming", "data/temp"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			return emuleqtManaged{}, err
		}
	}
	state := emuleqtLoadState(dir)
	if state.APIKey == "" || state.IPCToken == "" {
		if state.APIKey == "" {
			state.APIKey = emuleqtRandomHex(24)
		}
		if state.IPCToken == "" {
			state.IPCToken = emuleqtRandomHex(24)
		}
		if err := emuleqtSaveState(dir, state); err != nil {
			return emuleqtManaged{}, err
		}
	}
	if err := emuleqtWritePreferences(filepath.Join(dir, "config", "preferences.yml"), state); err != nil {
		return emuleqtManaged{}, err
	}
	return emuleqtManaged{
		URL:      "http://127.0.0.1:" + strconv.Itoa(emuleqtWebPort),
		APIKey:   state.APIKey,
		HostData: filepath.Join(dir, "data"),
		Dir:      dir,
	}, nil
}

// emuleqtWritePreferences writes the core's first preferences. Once the core
// has started it owns the file and rewrites it, so an existing file is left
// alone: a change made in emule-qt stays.
func emuleqtWritePreferences(path string, state emuleqtManagedState) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	content := fmt.Sprintf(`# Written by Gextto for the emule-qt core it runs. emule-qt owns this file
# from its first start on.
general:
  promptOnExit: false

network:
  port: %d
  udpPort: %d

ed3k:
  udpPort: %d

upnp:
  enableUPnP: true

directories:
  incomingDir: "%s/incoming"
  tempDirs:
    - "%s/temp"

ipc:
  enabled: true
  port: %d
  listenAddress: "127.0.0.1"
  tokens:
    - "%s"

webserver:
  enabled: true
  port: %d
  listenAddress: "127.0.0.1"
  restApiEnabled: true
  mcpEnabled: false
  apiKey: "%s"
`, emuleqtTCPPort, emuleqtUDPPort, emuleqtUDPPort, emuleqtContainerData, emuleqtContainerData,
		emuleqtIPCPort, state.IPCToken, emuleqtWebPort, state.APIKey)
	return os.WriteFile(path, []byte(content), 0o600)
}

// emuleqtManagedProblem is the last thing that kept the managed core from
// running, "" when none.
func emuleqtManagedProblem() string {
	emuleqtRuntime.mu.Lock()
	defer emuleqtRuntime.mu.Unlock()
	return emuleqtRuntime.problem
}

func emuleqtSetProblem(err error) {
	emuleqtRuntime.mu.Lock()
	defer emuleqtRuntime.mu.Unlock()
	if err == nil {
		emuleqtRuntime.problem = ""
		return
	}
	emuleqtRuntime.problem = err.Error()
	if time.Since(emuleqtRuntime.warnedAt) >= time.Hour {
		emuleqtRuntime.warnedAt = time.Now()
		logging.Warn(fmt.Sprintf("⚠️ emule-qt could not be started: %v", err))
	}
}

// emuleqtContainerInfo is what docker inspect says of the container: whether
// it exists, runs, and from which image.
func emuleqtContainerInfo(ctx context.Context) (exists, running bool, image string, err error) {
	out, err := emuleqtDocker(ctx, "inspect", "--type", "container", "-f", "{{.State.Running}} {{.Image}}", emuleqtContainer)
	if err != nil {
		if errors.Is(err, errEmuleqtNoDocker) {
			return false, false, "", err
		}
		if strings.Contains(strings.ToLower(out), "no such") {
			return false, false, "", nil
		}
		return false, false, "", err
	}
	fields := strings.Fields(out)
	if len(fields) < 2 {
		return true, false, "", nil
	}
	return true, fields[0] == "true", fields[1], nil
}

// emuleqtImageID is the id of the image behind the latest tag, "" when it
// was never pulled.
func emuleqtImageID(ctx context.Context) string {
	out, err := emuleqtDocker(ctx, "image", "inspect", "-f", "{{.Id}}", emuleqtImageTag)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// emuleqtRun creates and starts the container from image.
func emuleqtRun(ctx context.Context, managed emuleqtManaged, image string) error {
	_, err := emuleqtDocker(ctx, "run", "-d",
		"--name", emuleqtContainer,
		"--network", "host",
		"--restart", "unless-stopped",
		"--stop-timeout", "60",
		"--label", "org.gextto.managed=true",
		"-e", "PUID="+strconv.Itoa(os.Getuid()),
		"-e", "PGID="+strconv.Itoa(os.Getgid()),
		"-v", filepath.Join(managed.Dir, "config")+":/config",
		"-v", managed.HostData+":"+emuleqtContainerData,
		image)
	return err
}

// emuleqtRemove stops the container, giving the core time to save its part
// files, and removes it. The data folders stay.
func emuleqtRemove(ctx context.Context) {
	_, _ = emuleqtDocker(ctx, "stop", "-t", "60", emuleqtContainer)
	_, _ = emuleqtDocker(ctx, "rm", "-f", emuleqtContainer)
}

// emuleqtWaitHealthy waits for the core to answer.
func emuleqtWaitHealthy(ctx context.Context, managed emuleqtManaged, wait time.Duration) error {
	deadline := time.Now().Add(wait)
	var last error
	for {
		probe, cancel := context.WithTimeout(ctx, 5*time.Second)
		last = emuleqtHealthy(probe, managed)
		cancel()
		if last == nil {
			return nil
		}
		if !time.Now().Before(deadline) || ctx.Err() != nil {
			return last
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(3 * time.Second):
		}
	}
}

// emuleqtManagedTick keeps the managed core as configured, once a minute
// from the background loop: started while it is the chosen core, stopped
// when it is not, updated once a day. Slow work (a pull, an update) runs
// in the background, one at a time.
func emuleqtManagedTick(cfg *Config, now time.Time) {
	wanted := ed2kEnabled(cfg) && ed2kBackendKind(cfg) == ed2kBackendEmuleqtManaged
	emuleqtRuntime.mu.Lock()
	if emuleqtRuntime.busy {
		emuleqtRuntime.mu.Unlock()
		return
	}
	if !wanted {
		// Not the chosen core: stop a container Gextto started, looked at
		// every ten minutes only (docker inspect is not free).
		if now.Sub(emuleqtRuntime.idleAt) < 10*time.Minute {
			emuleqtRuntime.mu.Unlock()
			return
		}
		emuleqtRuntime.idleAt = now
		emuleqtRuntime.problem = ""
		emuleqtRuntime.mu.Unlock()
		if cfg == nil || cfg.DataDir == "" {
			return
		}
		if _, err := os.Stat(filepath.Join(emuleqtManagedDir(cfg), emuleqtStateFile)); err != nil {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		if _, running, _, err := emuleqtContainerInfo(ctx); err == nil && running {
			_, _ = emuleqtDocker(ctx, "stop", "-t", "60", emuleqtContainer)
			logging.Info("⏹️ emule-qt was stopped: it is no longer the eD2k core Gextto uses")
		}
		return
	}
	emuleqtRuntime.busy = true
	emuleqtRuntime.mu.Unlock()
	emuleqtWarnAmuleRunning(cfg, now)
	go func() {
		defer func() {
			emuleqtRuntime.mu.Lock()
			emuleqtRuntime.busy = false
			emuleqtRuntime.mu.Unlock()
		}()
		emuleqtSetProblem(emuleqtEnsure(cfg, now))
	}()
}

// emuleqtEnsure brings the managed core up and applies the daily update.
func emuleqtEnsure(cfg *Config, now time.Time) error {
	managed, err := emuleqtManagedEndpoint(cfg)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	exists, running, image, err := emuleqtContainerInfo(ctx)
	if err != nil {
		return err
	}
	state := emuleqtLoadState(managed.Dir)
	if !exists {
		latest := emuleqtImageID(ctx)
		if latest == "" {
			logging.Info("⬇️ Downloading the emule-qt image: the first time it takes a few minutes")
			if _, err := emuleqtDocker(ctx, "pull", emuleqtImageTag); err != nil {
				return err
			}
			latest = emuleqtImageID(ctx)
		}
		if latest == "" {
			return errors.New("the emule-qt image is missing after the download")
		}
		if err := emuleqtRun(ctx, managed, latest); err != nil {
			return err
		}
		state.Image, state.CheckedAt = latest, now
		_ = emuleqtSaveState(managed.Dir, state)
		logging.Info(fmt.Sprintf("▶️ emule-qt started in Docker: eD2k on TCP %d and UDP %d", emuleqtTCPPort, emuleqtUDPPort))
		ed2kInvalidateHealth()
		return nil
	}
	if !running {
		if _, err := emuleqtDocker(ctx, "start", emuleqtContainer); err != nil {
			return err
		}
		logging.Info("▶️ emule-qt was not running and has been started again")
	}
	if state.Image == "" {
		state.Image = image
		_ = emuleqtSaveState(managed.Dir, state)
	}
	if !settingsBool(cfg, settingEmuleqtAutoUpdate, true) || now.Sub(state.CheckedAt) < emuleqtUpdateEvery {
		return nil
	}
	return emuleqtUpdate(ctx, managed, state, image, now)
}

// emuleqtUpdate pulls the latest image and, when it is new, moves the
// container onto it; a core that does not answer on the new image is put
// back on the one it ran before.
func emuleqtUpdate(ctx context.Context, managed emuleqtManaged, state emuleqtManagedState, current string, now time.Time) error {
	state.CheckedAt = now
	_ = emuleqtSaveState(managed.Dir, state)
	if _, err := emuleqtDocker(ctx, "pull", emuleqtImageTag); err != nil {
		return err
	}
	latest := emuleqtImageID(ctx)
	if latest == "" || latest == current {
		return nil
	}
	emuleqtRemove(ctx)
	if err := emuleqtRun(ctx, managed, latest); err == nil {
		if err := emuleqtWaitHealthy(ctx, managed, emuleqtHealthWaitDuration); err == nil {
			old := state.Previous
			state.Previous, state.Image = current, latest
			_ = emuleqtSaveState(managed.Dir, state)
			if old != "" && old != current && old != latest {
				_, _ = emuleqtDocker(ctx, "image", "rm", old)
			}
			logging.Info("⬆️ emule-qt was updated to the latest version")
			ed2kInvalidateHealth()
			return nil
		}
	}
	// The new version does not answer: back to the one that worked.
	emuleqtRemove(ctx)
	if err := emuleqtRun(ctx, managed, current); err != nil {
		return fmt.Errorf("the update failed and the previous version could not be started again: %w", err)
	}
	logging.Warn("⚠️ The new emule-qt version did not start: Gextto went back to the previous one and tries again tomorrow")
	ed2kInvalidateHealth()
	return nil
}

// emuleqtWarnAmuleRunning says, once an hour, that aMule still answers
// while emule-qt is the chosen core: the two are one or the other, and they
// want the same eD2k ports.
func emuleqtWarnAmuleRunning(cfg *Config, now time.Time) {
	emuleqtRuntime.mu.Lock()
	if now.Sub(emuleqtRuntime.amuleWarnedAt) < time.Hour {
		emuleqtRuntime.mu.Unlock()
		return
	}
	emuleqtRuntime.mu.Unlock()
	settings, err := ed2kSettingsFromConfig(cfg)
	if err != nil {
		return
	}
	conn, err := net.DialTimeout("tcp", settings.Address, time.Second)
	if err != nil {
		return
	}
	_ = conn.Close()
	emuleqtRuntime.mu.Lock()
	emuleqtRuntime.amuleWarnedAt = now
	emuleqtRuntime.mu.Unlock()
	logging.Warn(fmt.Sprintf("⚠️ aMule is still running (%s) while emule-qt is the eD2k core: stop aMule, the two use the same eD2k ports and only one can have them", settings.Address))
}

// emuleqtExternalSettings is the address and key of an emule-qt core
// Gextto does not run.
func emuleqtExternalSettings(cfg *Config) (string, string) {
	if cfg == nil {
		return "", ""
	}
	address := strings.TrimSpace(cfg.Settings[settingEmuleqtUrl])
	if address != "" && !strings.Contains(address, "://") {
		address = "http://" + address
	}
	return strings.TrimRight(address, "/"), strings.TrimSpace(cfg.Settings[settingEmuleqtApiKey])
}
