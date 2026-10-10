// Command gx-torrent is a small BitTorrent daemon built on rain (pure Go).
// Gextto drives it over a local REST API as an alternative to the embedded
// libtorrent engine; the daemon owns its queue (slots, slow torrents, stall
// rotation) and keeps the payload safe on removal.
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
// counts gx-torrent's builds independently from Gextto's.
var version = ""

func runtimeVersion() string {
	if version != "" {
		return version
	}
	return constants.GxTorrentVersion()
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

func defaultDataDir() string {
	if dir := os.Getenv("XDG_DATA_HOME"); dir != "" {
		return filepath.Join(dir, "gx-torrent")
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".local", "share", "gx-torrent")
	}
	return "gx-torrent-data"
}

// parsePortRange reads "6881" or "6881-6891". "0" keeps rain's own port per
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

func main() {
	listen := flag.String("listen", envOr("GX_TORRENT_LISTEN", "127.0.0.1:8890"), "address of the REST API")
	dataDir := flag.String("data", envOr("GX_TORRENT_DATA", defaultDataDir()), "state directory (session, links, queue state)")
	downloadDir := flag.String("download-dir", envOr("GX_TORRENT_DOWNLOAD_DIR", ""), "default save path (default <data>/downloads)")
	token := flag.String("token", envOr("GX_TORRENT_TOKEN", ""), "shared secret required in the X-Gx-Token header")
	roots := flag.String("allowed-roots", envOr("GX_TORRENT_ALLOWED_ROOTS", ""), "comma-separated directories a save path must be inside (empty = any absolute path)")
	ports := flag.String("peer-ports", envOr("GX_TORRENT_PEER_PORTS", "6881-6891"), "the first free port of this range is the single peer port (TCP peers, UDP DHT); 0 = one port per torrent")
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
	insecure := flag.Bool("insecure", false, "allow a non-loopback listen address without a token")
	debug := flag.Bool("debug", os.Getenv("GX_TORRENT_DEBUG") == "1", "verbose rain logging")
	logFile := flag.String("log-file", envOr("GX_TORRENT_LOG_FILE", ""), "write the log to this file, rotated at 5 MB keeping 4 files (default stderr)")
	orphanTimeout := flag.Duration("orphan-timeout", 0, "stop when no API request arrives for this long (0 = never); Gextto sets it so a daemon it left running does not outlive it")
	fingerprint := flag.String("fingerprint", "", "opaque value reported by /api/v1/health; Gextto uses it to recognise a daemon started with the same binary and options")
	mode := flag.String("mode", envOr("GX_TORRENT_MODE", ""), "managed (driven by Gextto) or standalone; default: managed with -fingerprint, standalone otherwise")
	ipFilterSource := flag.String("ipfilter-source", envOr("GX_TORRENT_IPFILTER_SOURCE", ""), "IP filter URL or path configured in Gextto, prefilled in the web page")
	gexttoLog := flag.String("gextto-log", envOr("GX_TORRENT_GEXTTO_LOG", ""), "Gextto log file shown in the web page's Gextto log tab")
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println(runtimeVersion())
		return
	}
	log.SetFlags(log.LstdFlags)
	log.SetPrefix("gx-torrent: ")
	setupLogFile(*logFile)

	if !loopback(*listen) && *token == "" && !*insecure {
		log.Fatalf("refusing to listen on %s without a token: set -token/GX_TORRENT_TOKEN or use -insecure", *listen)
	}
	portBegin, portEnd, err := parsePortRange(*ports)
	if err != nil {
		log.Fatal(err)
	}
	data, err := filepath.Abs(*dataDir)
	if err != nil {
		log.Fatal(err)
	}
	modeValue, err := resolveMode(*mode, *fingerprint)
	if err != nil {
		log.Fatal(err)
	}
	pageLang := strings.TrimSpace(*lang)
	if modeValue == ModeStandalone {
		// Standalone runs from settings.json; an explicit flag still wins. In
		// managed mode the file is never read, so Gextto's flags stay in charge.
		if store, loadErr := settings.Load(filepath.Join(data, "settings.json")); loadErr != nil {
			log.Printf("cannot read settings.json: %v", loadErr)
		} else if pageLang == "" {
			pageLang = strings.TrimSpace(store.Get("lang", ""))
		}
	}
	downloads := *downloadDir
	if downloads == "" {
		downloads = filepath.Join(data, "downloads")
	}
	if downloads, err = filepath.Abs(downloads); err != nil {
		log.Fatal(err)
	}
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
		Listen:       *listen,
		DataDir:      data,
		LinkDir:      filepath.Join(data, "links"),
		DownloadDir:  downloads,
		DBPath:       filepath.Join(data, "session.db"),
		StatePath:    filepath.Join(data, "state.json"),
		Token:        *token,
		AllowedRoots: allowed,
		Network: NetworkOptions{
			PortBegin:         portBegin,
			PortEnd:           portEnd,
			ListenInterface:   *listenIface,
			OutgoingInterface: *outIface,
			Proxy:             *proxyURL,
			Encryption:        *encryption,
			DHT:               !*noDHT,
			PEX:               !*noPEX,
			UTP:               !*noUTP,
			Holepunch:         !*noHolepunch,
			LSD:               !*noLSD,
			UPnP:              !*noUPnP,
			NATPMP:            !*noNATPMP,
			IPFilter:          *ipFilter,
			IPFilterTrackers:  *ipFilterTrackers,
			DHTBootstrap:      parseBootstrapNodes(*dhtBootstrap),
		},
		Debug:          *debug,
		Mode:           modeValue,
		Fingerprint:    *fingerprint,
		GexttoLog:      strings.TrimSpace(*gexttoLog),
		IPFilterSource: strings.TrimSpace(*ipFilterSource),
		Lang:           pageLang,
		Tick:           3 * time.Second,
		ProbeWindow:    15 * time.Minute,
	}
	daemon, err := newDaemon(opts)
	if err != nil {
		log.Fatalf("cannot start: %v", err)
	}

	stop := make(chan struct{})
	loopDone := make(chan struct{})
	go func() {
		daemon.run(stop)
		close(loopDone)
	}()
	daemon.poke()

	server := &http.Server{
		Addr:              opts.Listen,
		Handler:           daemon.routes(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	serverErr := make(chan error, 1)
	go func() {
		logf("version %s listening on %s (data %s, downloads %s)", runtimeVersion(), opts.Listen, opts.DataDir, opts.DownloadDir)
		serverErr <- server.ListenAndServe()
	}()

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	watchOrphan(*orphanTimeout, signals)
	exitCode := 0
	select {
	case sig := <-signals:
		logf("received %s, shutting down", sig)
	case err := <-serverErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			logf("API server failed: %v", err)
			exitCode = 1
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	_ = server.Shutdown(ctx)
	cancel()
	close(stop)
	<-loopDone
	daemon.close()
	logf("stopped")
	os.Exit(exitCode)
}
