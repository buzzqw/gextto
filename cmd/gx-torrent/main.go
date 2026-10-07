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

	clog "github.com/cenkalti/log"
)

// version is overridden at build time with -ldflags "-X main.version=...".
var version = "dev"

func logf(format string, args ...any) {
	log.Printf(format, args...)
}

// rainLogHandler keeps rain's own log to warnings, or everything with -debug.
func rainLogHandler(debug bool) clog.Handler {
	handler := clog.NewFileHandler(os.Stderr)
	if debug {
		handler.SetLevel(clog.DEBUG)
	} else {
		handler.SetLevel(clog.WARNING)
	}
	return handler
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

func parsePortRange(value string) (uint16, uint16, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, 0, nil
	}
	left, right, found := strings.Cut(value, "-")
	if !found {
		return 0, 0, fmt.Errorf("port range must look like 20000-30000")
	}
	begin, err1 := strconv.ParseUint(strings.TrimSpace(left), 10, 16)
	end, err2 := strconv.ParseUint(strings.TrimSpace(right), 10, 16)
	if err1 != nil || err2 != nil || begin < 1024 || end <= begin {
		return 0, 0, fmt.Errorf("invalid port range %q", value)
	}
	return uint16(begin), uint16(end), nil
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
	ports := flag.String("peer-ports", envOr("GX_TORRENT_PEER_PORTS", ""), "peer port range, e.g. 50000-50100 (default 20000-30000)")
	noDHT := flag.Bool("no-dht", os.Getenv("GX_TORRENT_NO_DHT") == "1", "disable DHT")
	insecure := flag.Bool("insecure", false, "allow a non-loopback listen address without a token")
	debug := flag.Bool("debug", os.Getenv("GX_TORRENT_DEBUG") == "1", "verbose rain logging")
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println(version)
		return
	}
	log.SetFlags(log.LstdFlags)
	log.SetPrefix("gx-torrent: ")

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
		PortBegin:    portBegin,
		PortEnd:      portEnd,
		DHT:          !*noDHT,
		Debug:        *debug,
		Tick:         3 * time.Second,
		ProbeWindow:  15 * time.Minute,
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
		logf("version %s listening on %s (data %s, downloads %s)", version, opts.Listen, opts.DataDir, opts.DownloadDir)
		serverErr <- server.ListenAndServe()
	}()

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
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
