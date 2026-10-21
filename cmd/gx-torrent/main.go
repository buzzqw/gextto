// Command gx-torrent is a small BitTorrent daemon built on the gx-core engine (pure Go).
// Gextto drives it over a local REST API as its default transfer engine; the
// daemon owns its queue (slots, slow torrents, stall rotation) and keeps the
// payload safe on removal.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/buzzqw/gextto/internal/constants"
	"github.com/buzzqw/gextto/internal/settings"
)

// version may be overridden at build time with -ldflags "-X main.version=...".
// Empty means: use the daemon's own version (1.1.<gx-torrent build>), which
// counts gx-torrent's builds independently from Gextto's. The second number is
// raised by hand in gx_torrent.version_series.
var version = ""

func runtimeVersion() string {
	if version != "" {
		return version
	}
	return constants.GxTorrentVersion()
}

// displayVersion is what people see (--version, health, the settings page).
// The build is already the third number, so it equals runtimeVersion.
func displayVersion() string {
	if version != "" {
		return version
	}
	return constants.GxTorrentVersionLabel()
}

func logf(format string, args ...any) {
	log.Printf(format, args...)
}

func envOr(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

// defaultDataDir is defined per platform (defaultdir_unix.go /
// defaultdir_windows.go).

// localSetupURL is the loopback URL of the first-run wizard: a wildcard listen
// address becomes 127.0.0.1, so the browser opens the daemon on this machine.
func localSetupURL(listen string) string {
	host, port, err := net.SplitHostPort(strings.TrimSpace(listen))
	if err != nil || port == "" {
		return ""
	}
	switch host {
	case "", "0.0.0.0", "::":
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port) + setupPath
}

// resolvePortRange picks the peer port range: the flag or environment wins,
// then the standalone settings, then the built-in default.
func resolvePortRange(flagValue string, store *settings.Store) (uint16, uint16, error) {
	value := optStr(store, "peer-ports", flagValue)
	if value == "" {
		value = "6881-6891"
	}
	return parsePortRange(value)
}

// optStr returns the standalone settings value when set, otherwise the flag
// value. In managed mode store is nil, so the flag always wins.
func optStr(store *settings.Store, key, flagValue string) string {
	if store != nil {
		if v := strings.TrimSpace(store.Get(key, "")); v != "" {
			return v
		}
	}
	return strings.TrimSpace(flagValue)
}

// seedDefaultIPFilter gives a standalone daemon Gextto's IP filter list the
// first time it runs: only when settings.json never had the key and no
// -ipfilter flag was passed, so a filter the operator removed stays removed.
func seedDefaultIPFilter(store *settings.Store, flagValue string) {
	if store == nil || strings.TrimSpace(flagValue) != "" {
		return
	}
	for _, key := range store.Keys() {
		if key == "ipfilter" {
			return
		}
	}
	store.Set("ipfilter", constants.DefaultIPFilterURL)
	if err := store.Save(); err != nil {
		log.Printf("cannot save the default IP filter: %v", err)
	}
}

// stringList is a repeatable string flag.
type stringList []string

func (l *stringList) String() string { return strings.Join(*l, ";") }

func (l *stringList) Set(v string) error {
	if v = strings.TrimSpace(v); v != "" {
		*l = append(*l, v)
	}
	return nil
}

// optBool returns the settings value when set, otherwise the flag value.
func optBool(store *settings.Store, key string, flagValue bool) bool {
	if store != nil {
		if v := strings.TrimSpace(store.Get(key, "")); v != "" {
			return strings.EqualFold(v, "true")
		}
	}
	return flagValue
}

// optInt returns the settings value when set and valid, otherwise the flag value.
func optInt(store *settings.Store, key string, flagValue int) int {
	if store != nil {
		if v := strings.TrimSpace(store.Get(key, "")); v != "" {
			if n, err := strconv.Atoi(v); err == nil {
				return n
			}
		}
	}
	return flagValue
}

// parsePortRange reads "6881" or "6881-6891". "0" keeps the engine's own port per
// torrent (no shared port).
func parsePortRange(value string) (uint16, uint16, error) {
	value = strings.TrimSpace(value)
	if value == "" || value == "0" {
		return 0, 0, nil
	}
	left, right, found := strings.Cut(value, "-")
	if !found {
		right = left
	}
	begin, err1 := strconv.ParseUint(strings.TrimSpace(left), 10, 16)
	end, err2 := strconv.ParseUint(strings.TrimSpace(right), 10, 16)
	if err1 != nil || err2 != nil || begin < 1 || end < begin {
		return 0, 0, fmt.Errorf("invalid port range %q", value)
	}
	return uint16(begin), uint16(end), nil
}

// parseBootstrapNodes splits a comma/semicolon/whitespace separated list of DHT
// routers into addresses, dropping empty entries.
func parseBootstrapNodes(value string) []string {
	fields := strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == ';' || r == '\n' || r == '\r' || r == ' ' || r == '\t'
	})
	var nodes []string
	for _, field := range fields {
		if node := strings.TrimSpace(field); node != "" {
			nodes = append(nodes, node)
		}
	}
	return nodes
}

// envIntVal reads an integer env var, parsed directly into a native int so no
// narrowing conversion takes place, and clamped into [low, high].
func envIntVal(key string, fallback, low, high int) int {
	value := fallback
	if parsed, err := strconv.Atoi(strings.TrimSpace(os.Getenv(key))); err == nil {
		value = parsed
	}
	if value < low {
		value = low
	}
	if value > high {
		value = high
	}
	return value
}

func envBool(key string, fallback bool) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(key))) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	}
	return fallback
}

func loopback(listen string) bool {
	host, _, err := net.SplitHostPort(listen)
	if err != nil {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// defaultListen is the page/API address when neither settings.json nor the flag
// sets one. Standalone is a client meant to be driven from another machine (its
// web UI, *arr apps, mobile apps), so it listens on every interface and is
// reachable over the LAN. Managed keeps the loopback default: Gextto runs beside
// the daemon on this host and reaches it there.
func defaultListen(mode Mode) string {
	if mode == ModeStandalone {
		return "0.0.0.0:8890"
	}
	return "127.0.0.1:8890"
}

func main() {
	listen := flag.String("listen", envOr("GX_TORRENT_LISTEN", ""), "address of the REST API (standalone default 0.0.0.0:8890, managed 127.0.0.1:8890)")
	dataDir := flag.String("data", envOr("GX_TORRENT_DATA", defaultDataDir()), "state directory (session, links, queue state)")
	downloadDir := flag.String("download-dir", envOr("GX_TORRENT_DOWNLOAD_DIR", ""), "default save path (default <data>/downloads)")
	token := flag.String("token", envOr("GX_TORRENT_TOKEN", ""), "shared secret required in the X-Gx-Token header")
	roots := flag.String("allowed-roots", envOr("GX_TORRENT_ALLOWED_ROOTS", ""), "comma-separated directories a save path must be inside (empty = any absolute path)")
	ports := flag.String("peer-ports", envOr("GX_TORRENT_PEER_PORTS", ""), "the first free port of this range is the single peer port (TCP peers, UDP DHT); empty = 6881-6891; 0 = one port per torrent")
	listenIface := flag.String("listen-interface", envOr("GX_TORRENT_LISTEN_INTERFACE", ""), "IP or interface name for incoming peers (default all)")
	outIface := flag.String("outgoing-interface", envOr("GX_TORRENT_OUTGOING_INTERFACE", ""), "bind all outgoing traffic to this interface or IP (VPN killswitch)")
	proxyURL := flag.String("proxy", envOr("GX_TORRENT_PROXY", ""), "socks5://[user:pass@]host:port or http://host:port (disables DHT and UDP trackers)")
	encryption := flag.Int("encryption", envIntVal("GX_TORRENT_ENCRYPTION", 1, 0, 2), "0 disabled, 1 enabled, 2 forced")
	noDHT := flag.Bool("no-dht", envBool("GX_TORRENT_NO_DHT", false), "disable DHT")
	noPEX := flag.Bool("no-pex", envBool("GX_TORRENT_NO_PEX", false), "disable peer exchange")
	noUTP := flag.Bool("no-utp", envBool("GX_TORRENT_NO_UTP", false), "disable uTP (peers over TCP only)")
	noHolepunch := flag.Bool("no-holepunch", envBool("GX_TORRENT_NO_HOLEPUNCH", false), "disable BEP 55 holepunching (needs uTP)")
	noLSD := flag.Bool("no-lsd", envBool("GX_TORRENT_NO_LSD", false), "disable local service discovery")
	noUPnP := flag.Bool("no-upnp", envBool("GX_TORRENT_NO_UPNP", false), "do not open the port on the router with UPnP")
	noNATPMP := flag.Bool("no-natpmp", envBool("GX_TORRENT_NO_NATPMP", false), "do not open the port on the router with NAT-PMP")
	ipFilter := flag.String("ipfilter", envOr("GX_TORRENT_IPFILTER", ""), "IP filter file (CIDR, ranges, P2P or eMule format)")
	lang := flag.String("lang", envOr("GX_TORRENT_LANG", ""), "web page language (it, en, de, fr, es, pl; empty = English)")
	ipFilterTrackers := flag.Bool("ipfilter-trackers", envBool("GX_TORRENT_IPFILTER_TRACKERS", true), "apply the IP filter to trackers too")
	dhtBootstrap := flag.String("dht-bootstrap", envOr("GX_TORRENT_DHT_BOOTSTRAP", ""), "comma-separated DHT router addresses (empty = built-in bootstrap nodes)")
	webSourcesAllowPrivate := flag.Bool("web-sources-allow-private", envBool("GX_TORRENT_WEB_SOURCES_ALLOW_PRIVATE", false), "let web seeds and magnet xs=/as= sources reach private/LAN addresses (off: protects the LAN from URLs in untrusted torrents)")
	dhtReadOnly := flag.Bool("dht-read-only", envBool("GX_TORRENT_DHT_READ_ONLY", false), "read-only DHT (BEP 43): do not answer queries and ask not to be added to routing tables")
	insecure := flag.Bool("insecure", false, "allow a non-loopback listen address without a token")
	debug := flag.Bool("debug", os.Getenv("GX_TORRENT_DEBUG") == "1", "verbose engine logging")
	logFile := flag.String("log-file", envOr("GX_TORRENT_LOG_FILE", ""), "write the log to this file, rotated at 5 MB keeping 4 files (default stderr)")
	orphanTimeout := flag.Duration("orphan-timeout", 0, "stop when no API request arrives for this long (0 = never); Gextto sets it so a daemon it left running does not outlive it")
	fingerprint := flag.String("fingerprint", "", "opaque value reported by /api/v1/health; Gextto uses it to recognise a daemon started with the same binary and options")
	mode := flag.String("mode", envOr("GX_TORRENT_MODE", ""), "managed (driven by Gextto) or standalone; default: managed with -fingerprint, standalone otherwise")
	ipFilterSource := flag.String("ipfilter-source", envOr("GX_TORRENT_IPFILTER_SOURCE", ""), "IP filter URL or path configured in Gextto, prefilled in the web page")
	gexttoLog := flag.String("gextto-log", envOr("GX_TORRENT_GEXTTO_LOG", ""), "Gextto log file shown in the web page's Gextto log tab")
	notify := flag.String("notify-url", envOr("GX_TORRENT_NOTIFY_URL", ""), "webhook URL posted on feed match/error (empty = disabled)")
	mmapStorage := flag.Bool("mmap", envBool("GX_TORRENT_MMAP", false), "serve torrent files from a shared memory mapping (Linux, experimental): writes are page-cache memcpy and reads avoid a syscall")
	dlnaEnabled := flag.Bool("dlna", envBool("GX_TORRENT_DLNA", false), "enable the DLNA/UPnP media server for the completed media (LAN only)")
	dlnaListen := flag.String("dlna-listen", envOr("GX_TORRENT_DLNA_LISTEN", ""), "DLNA media server HTTP bind address (default :8200)")
	dlnaName := flag.String("dlna-name", envOr("GX_TORRENT_DLNA_NAME", ""), "DLNA media server friendly name (default gx-torrent)")
	dlnaFolders := stringList(dlnaSplitList(envOr("GX_TORRENT_DLNA_FOLDERS", "")))
	flag.Var(&dlnaFolders, "dlna-folder", "library folder the DLNA media server shows beside the torrents (repeatable)")
	dlnaExclude := stringList(dlnaSplitList(envOr("GX_TORRENT_DLNA_EXCLUDE", "")))
	flag.Var(&dlnaExclude, "dlna-exclude", "folder inside a DLNA library folder that is never shown (repeatable)")
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Parse()
	applyGCDefault()

	if *showVersion {
		fmt.Println(displayVersion())
		return
	}
	log.SetFlags(log.LstdFlags)
	log.SetPrefix("gx-torrent: ")
	setupLogFile(*logFile)

	data, err := filepath.Abs(*dataDir)
	if err != nil {
		log.Fatal(err)
	}
	modeValue, err := resolveMode(*mode, *fingerprint)
	if err != nil {
		log.Fatal(err)
	}
	var settingsStore *settings.Store
	if modeValue == ModeStandalone {
		store, loadErr := settings.Load(filepath.Join(data, "settings.json"))
		if loadErr != nil {
			log.Printf("cannot read settings.json: %v", loadErr)
		} else {
			settingsStore = store
			seedDefaultIPFilter(store, *ipFilter)
		}
	}
	// In standalone settings.json is authoritative: a value there wins over the
	// startup flag (the packaged unit passes fixed flags, and the settings page
	// must be able to change them). In managed mode the store is nil, so
	// Gextto's flags stay in charge. See optStr/optBool/optInt.
	tokenValue := optStr(settingsStore, "token", *token)
	// In managed mode a non-loopback listen without a token would expose the
	// API with no protection; standalone has its own login/LAN model instead.
	listenValue := optStr(settingsStore, "listen", *listen)
	if listenValue == "" {
		listenValue = defaultListen(modeValue)
	}
	if modeValue != ModeStandalone && !loopback(listenValue) && tokenValue == "" && !*insecure {
		log.Fatalf("refusing to listen on %s without a token: set -token/GX_TORRENT_TOKEN or use -insecure", listenValue)
	}
	portBegin, portEnd, err := resolvePortRange(*ports, settingsStore)
	if err != nil {
		log.Fatal(err)
	}
	downloads := optStr(settingsStore, "download-dir", *downloadDir)
	if downloads == "" {
		downloads = filepath.Join(data, "downloads")
	}
	if downloads, err = filepath.Abs(downloads); err != nil {
		log.Fatal(err)
	}
	partsDir := optStr(settingsStore, "temp-dir", "")
	notifyURLValue := optStr(settingsStore, notifyURLSettingKey, *notify)
	langValue := optStr(settingsStore, "lang", *lang)
	listenIfaceValue := optStr(settingsStore, "listen-interface", *listenIface)
	outIfaceValue := optStr(settingsStore, "outgoing-interface", *outIface)
	proxyValue := optStr(settingsStore, "proxy", *proxyURL)
	ipFilterValue := optStr(settingsStore, "ipfilter", *ipFilter)
	ipFilterTrackersValue := optBool(settingsStore, "ipfilter-trackers", *ipFilterTrackers)
	encryptionValue := optInt(settingsStore, "encryption", *encryption)
	noDHTValue := optBool(settingsStore, "no-dht", *noDHT)
	noPEXValue := optBool(settingsStore, "no-pex", *noPEX)
	noUTPValue := optBool(settingsStore, "no-utp", *noUTP)
	noHolepunchValue := optBool(settingsStore, "no-holepunch", *noHolepunch)
	noLSDValue := optBool(settingsStore, "no-lsd", *noLSD)
	noUPnPValue := optBool(settingsStore, "no-upnp", *noUPnP)
	noNATPMPValue := optBool(settingsStore, "no-natpmp", *noNATPMP)
	dhtBootstrapValue := optStr(settingsStore, "dht-bootstrap", *dhtBootstrap)
	dhtReadOnlyValue := optBool(settingsStore, "dht-read-only", *dhtReadOnly)
	webSourcesAllowPrivateValue := optBool(settingsStore, "web-sources-allow-private", *webSourcesAllowPrivate)
	dlnaEnabledValue := optBool(settingsStore, dlnaEnabledKey, *dlnaEnabled)
	dlnaListenValue := optStr(settingsStore, dlnaListenKey, *dlnaListen)
	dlnaNameValue := optStr(settingsStore, dlnaNameKey, *dlnaName)
	var allowed []string
	for _, root := range strings.Split(*roots, ",") {
		if root = strings.TrimSpace(root); root != "" {
			if !filepath.IsAbs(root) {
				log.Fatalf("allowed root %q must be absolute", root)
			}
			allowed = append(allowed, filepath.Clean(root))
		}
	}

	opts := Options{
		Listen:       listenValue,
		DataDir:      data,
		LinkDir:      filepath.Join(data, "links"),
		DownloadDir:  downloads,
		PartsDir:     partsDir,
		DBPath:       filepath.Join(data, "session.db"),
		StatePath:    filepath.Join(data, "state.json"),
		Token:        tokenValue,
		AllowedRoots: allowed,
		Network: NetworkOptions{
			PortBegin:              portBegin,
			PortEnd:                portEnd,
			ListenInterface:        listenIfaceValue,
			OutgoingInterface:      outIfaceValue,
			Proxy:                  proxyValue,
			Encryption:             encryptionValue,
			DHT:                    !noDHTValue,
			PEX:                    !noPEXValue,
			UTP:                    !noUTPValue,
			Holepunch:              !noHolepunchValue,
			LSD:                    !noLSDValue,
			UPnP:                   !noUPnPValue,
			NATPMP:                 !noNATPMPValue,
			IPFilter:               ipFilterValue,
			IPFilterTrackers:       ipFilterTrackersValue,
			DHTBootstrap:           parseBootstrapNodes(dhtBootstrapValue),
			DHTReadOnly:            dhtReadOnlyValue,
			WebSourcesAllowPrivate: webSourcesAllowPrivateValue,
		},
		Debug:          *debug,
		Mode:           modeValue,
		Settings:       settingsStore,
		Fingerprint:    *fingerprint,
		GexttoLog:      strings.TrimSpace(*gexttoLog),
		IPFilterSource: strings.TrimSpace(*ipFilterSource),
		Lang:           langValue,
		NotifyURL:      notifyURLValue,
		Mmap:           *mmapStorage,
		DLNAEnabled:    dlnaEnabledValue,
		DLNAListen:     dlnaListenValue,
		DLNAName:       dlnaNameValue,
		DLNAFolders:    dlnaFolders,
		DLNAExclude:    dlnaExclude,
		Tick:           3 * time.Second,
		ProbeWindow:    15 * time.Minute,
	}
	// Started by the Windows Service Control Manager: run the same daemon under
	// the service dispatcher instead of the interactive signal loop.
	if isWindowsService() {
		if err := runWindowsService(opts); err != nil {
			logf("service failed: %v", err)
			os.Exit(1)
		}
		return
	}
	// After an in-app update: count the new version's starts and put the
	// previous binary back if it keeps failing (update.go).
	if opts.Mode == ModeStandalone {
		if exe, err := os.Executable(); err == nil {
			if exe, err = filepath.EvalSymlinks(exe); err == nil {
				if target := updateBootGuard(opts.DataDir, exe); target != "" {
					if err := restartInto(target); err != nil {
						logf("cannot start the restored version: %v", err)
					}
				}
			}
		}
	}
	run, err := startDaemon(opts)
	if err != nil {
		log.Fatalf("cannot start: %v", err)
	}

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	watchOrphan(*orphanTimeout, signals)
	exitCode := 0
	select {
	case sig := <-signals:
		logf("received %s, shutting down", sig)
	case err := <-run.serverErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			logf("API server failed: %v", err)
			exitCode = 1
		}
	case <-run.daemon.updater.restartC():
		// An in-app update replaced the binary: stop cleanly, then start the
		// new one in this same process.
		logf("restarting into the updated version")
		run.shutdown()
		exe, err := run.daemon.updater.executable()
		if err == nil {
			err = restartInto(exe)
		}
		// Exec failed: exit with an error so a supervisor (systemd
		// Restart=on-failure) starts the new binary.
		logf("cannot restart in place: %v", err)
		os.Exit(1)
	}
	run.shutdown()
	os.Exit(exitCode)
}

// daemonRun is a running daemon: its HTTP server, run loop and shutdown handles.
type daemonRun struct {
	daemon    *Daemon
	server    *http.Server
	addr      net.Addr
	stop      chan struct{}
	loopDone  chan struct{}
	serverErr chan error
}

// Addr is the address the HTTP server is listening on (useful when the port is
// chosen by the OS, and for the service tests).
func (r *daemonRun) Addr() net.Addr { return r.addr }

// startDaemon creates the daemon, opens the listener and starts serving. The
// listener is opened before serving, so the first-run browser below never
// reaches the address ahead of the server and is not opened at all when the
// port is taken. It is shared by the interactive process and the Windows
// service.
func startDaemon(opts Options) (*daemonRun, error) {
	startPProf()
	daemon, err := newDaemon(opts)
	if err != nil {
		return nil, err
	}
	stop := make(chan struct{})
	loopDone := make(chan struct{})
	go func() {
		daemon.run(stop)
		close(loopDone)
	}()
	daemon.poke()

	listener, err := net.Listen("tcp", opts.Listen)
	if err != nil {
		close(stop)
		<-loopDone
		daemon.close()
		return nil, err
	}
	server := &http.Server{Handler: daemon.routes(), ReadHeaderTimeout: 10 * time.Second}
	serverErr := make(chan error, 1)
	boundHost, boundPort, _ := net.SplitHostPort(listener.Addr().String())
	logf("version %s listening on %s (data %s, downloads %s)", runtimeVersion(), listener.Addr(), opts.DataDir, opts.DownloadDir)
	for _, url := range webUIURLs(boundHost, boundPort) {
		logf("web UI: %s", url)
	}
	go func() {
		serverErr <- server.Serve(listener)
	}()

	// First run in standalone: open the web wizard in the browser, so the
	// installation is guided instead of typed. Best-effort: a headless server
	// (no graphical session), a Windows service session or a missing browser is
	// silently skipped, and the wizard stays reachable at the printed address.
	if opts.Mode == ModeStandalone && !daemon.setupComplete() {
		if url := localSetupURL(opts.Listen); url != "" {
			logf("first run: open %s to configure gx-torrent", url)
			go func() {
				if err := openBrowser(url); err != nil {
					logf("cannot open the browser automatically: %v (open %s by hand)", err, url)
				}
			}()
		}
	}
	if opts.Mode == ModeStandalone {
		go daemon.updateWorker(stop)
		go daemon.updateMarkHealthy(stop)
	}
	return &daemonRun{daemon: daemon, server: server, addr: listener.Addr(), stop: stop, loopDone: loopDone, serverErr: serverErr}, nil
}

// shutdown stops the HTTP server and the daemon run loop.
func (r *daemonRun) shutdown() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	_ = r.server.Shutdown(ctx)
	cancel()
	close(r.stop)
	<-r.loopDone
	r.daemon.close()
	logf("stopped")
}
