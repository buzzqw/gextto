// Command gx-ed2k is Gextto's native eD2k/Kad daemon.
//
// By default it is a search client: it bootstraps Kad, searches keywords on
// Kad and on the configured eD2k servers, and answers over the same /api/v1
// shape as gx-torrent. With -download it also runs the transfer plane and
// accepts ed2k:// links on /api/v1/transfers. Upload, credits, LowID callback
// and obfuscation are not implemented yet, so health reports "download": false
// unless the flag is set.
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
	"github.com/buzzqw/gextto/internal/ed2k"
	"github.com/buzzqw/gextto/internal/ed2k/transfer/download"
	"github.com/buzzqw/gextto/internal/ed2k/transfer/upload"
	"github.com/buzzqw/gextto/internal/messages"
)

// version and commit are stamped by the build.
var (
	version = ""
	commit  = ""
)

func runtimeVersion() string {
	if version != "" {
		return version
	}
	return constants.GxEd2kVersion()
}

func envOr(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func defaultDataDir() string {
	if value := strings.TrimSpace(os.Getenv("XDG_DATA_HOME")); value != "" {
		return filepath.Join(value, "gx-ed2k")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "gx-ed2k-data"
	}
	return filepath.Join(home, ".local", "share", "gx-ed2k")
}

func main() {
	listen := flag.String("listen", envOr("GX_ED2K_LISTEN", ""), "HTTP listen address (default 127.0.0.1:8899 managed, 0.0.0.0:8899 standalone)")
	dataDir := flag.String("data", envOr("GX_ED2K_DATA", defaultDataDir()), "state directory")
	token := flag.String("token", envOr("GX_ED2K_TOKEN", ""), "API token required outside loopback")
	insecure := flag.Bool("insecure", false, "allow a non-loopback listen without a token")
	mode := flag.String("mode", envOr("GX_ED2K_MODE", ""), "managed or standalone")
	kadListen := flag.String("kad-listen", envOr("GX_ED2K_KAD_LISTEN", ""), "Kad UDP listen address (default 0.0.0.0:4672)")
	tcpPort := flag.Int("tcp-port", envInt("GX_ED2K_TCP_PORT", 4662), "TCP port advertised to the network")
	nodesURL := flag.String("nodes-url", envOr("GX_ED2K_NODES_URL", "https://upd.emule-security.org/nodes.dat"), "nodes.dat URL used to bootstrap Kad")
	serverListURL := flag.String("serverlist-url", envOr("GX_ED2K_SERVERLIST_URL", "https://upd.emule-security.org/server.met"), "server.met URL used when -servers is empty")
	servers := flag.String("servers", envOr("GX_ED2K_SERVERS", ""), "comma-separated eD2k servers (host:port)")
	upnp := flag.Bool("upnp", envBool("GX_ED2K_UPNP", true), "ask the router to forward the Kad UDP port with UPnP")
	searchTimeout := flag.Duration("search-timeout", 20*time.Second, "whole search budget")
	downloadEnabled := flag.Bool("download", envBool("GX_ED2K_DOWNLOAD", false), "enable the eD2k transfer plane")
	downloadDir := flag.String("download-dir", envOr("GX_ED2K_DOWNLOAD_DIR", ""), "directory for .part files and completed downloads")
	lang := flag.String("lang", messages.Language(), "UI language (it, en, de, fr, es, pl)")
	fingerprint := flag.String("fingerprint", "", "configuration fingerprint (managed mode)")
	logFile := flag.String("log-file", "", "write the log to this file instead of stderr")
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println(runtimeVersion())
		return
	}
	if *logFile != "" {
		if err := os.MkdirAll(filepath.Dir(*logFile), 0o755); err != nil {
			log.Fatalf("gx-ed2k: log dir: %v", err)
		}
		file, err := os.OpenFile(*logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			log.Fatalf("gx-ed2k: open log: %v", err)
		}
		defer file.Close()
		log.SetOutput(file)
	}
	if *dataDir == "" {
		log.Fatal("gx-ed2k: -data is required")
	}
	if err := os.MkdirAll(*dataDir, 0o755); err != nil {
		log.Fatalf("gx-ed2k: data dir: %v", err)
	}

	modeValue, err := resolveMode(*mode, *fingerprint)
	if err != nil {
		log.Fatalf("gx-ed2k: %v", err)
	}
	listenValue := strings.TrimSpace(*listen)
	if listenValue == "" {
		if modeValue == modeStandalone {
			listenValue = "0.0.0.0:8899"
		} else {
			listenValue = "127.0.0.1:8899"
		}
	}
	if modeValue != modeStandalone && !isLoopback(listenValue) && strings.TrimSpace(*token) == "" && !*insecure {
		log.Fatalf("gx-ed2k: refusing to listen on %s without a token (pass -token or -insecure)", listenValue)
	}
	if *tcpPort < 1 || *tcpPort > 65535 {
		log.Fatalf("gx-ed2k: -tcp-port must be between 1 and 65535")
	}
	if *searchTimeout < 2*time.Second || *searchTimeout > 120*time.Second {
		log.Fatalf("gx-ed2k: -search-timeout must be between 2s and 120s")
	}
	if *downloadDir == "" {
		*downloadDir = filepath.Join(*dataDir, "incoming")
	}
	if *downloadEnabled {
		if err := os.MkdirAll(*downloadDir, 0o755); err != nil {
			log.Fatalf("gx-ed2k: download dir: %v", err)
		}
	}

	userHash, _ := ed2k.RandomID()
	var uploader *upload.Server
	if *downloadEnabled {
		uploader = upload.New(2)
		uploader.ClientName = "gx-ed2k"
		uploader.UserHash = userHash
		uploader.TCPPort = uint16(*tcpPort)
		if err := uploader.IndexDir(*downloadDir); err != nil {
			log.Printf("gx-ed2k: upload index: %v", err)
		}
	}
	var clientHandler func(net.Conn)
	if uploader != nil {
		clientHandler = uploader.Handle
	}
	client, err := ed2k.New(ed2k.Config{
		DataDir:       *dataDir,
		UDPListen:     *kadListen,
		TCPPort:       *tcpPort,
		NodesURL:      *nodesURL,
		ServerListURL: *serverListURL,
		Servers:       ed2k.NormalizeServerList(*servers),
		SearchTimeout: *searchTimeout,
		UPnP:          *upnp,
		ClientHandler: clientHandler,
	})
	if err != nil {
		log.Fatalf("gx-ed2k: %v", err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	daemon := newDaemon(daemonOptions{
		DataDir:     *dataDir,
		Mode:        modeValue,
		Token:       *token,
		Fingerprint: *fingerprint,
		Lang:        *lang,
		Commit:      commit,
	}, client)
	var manager *download.Manager
	if *downloadEnabled {
		manager = download.NewManager(ctx, download.ManagerConfig{
			Fetch: download.Config{
				TempDir:    *downloadDir,
				ClientName: "gx-ed2k",
				TCPPort:    uint16(*tcpPort),
				UserHash:   userHash,
			},
			OnComplete: func(result download.Result) {
				if uploader != nil {
					if err := uploader.AddFile(result.Path); err != nil {
						log.Printf("gx-ed2k: share completed file: %v", err)
					}
				}
			},
		}, client)
		daemon.transfers = manager
		log.Printf("gx-ed2k: transfer plane enabled, downloads in %s", *downloadDir)
	}

	if err := client.Start(ctx); err != nil {
		// Kad is optional: the server search still works when UDP is blocked.
		log.Printf("gx-ed2k: Kad start: %v", err)
		daemon.setStartupError(err)
	}

	server := &http.Server{
		Addr:              listenValue,
		Handler:           daemon.routes(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		log.Printf("gx-ed2k %s listening on %s (mode %s, data %s)", runtimeVersion(), listenValue, modeValue, *dataDir)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("gx-ed2k: http: %v", err)
			stop()
		}
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = server.Shutdown(shutdownCtx)
	if manager != nil {
		manager.Close()
	}
	_ = client.Close()
}

func envInt(name string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return value
}

// envBool reads a boolean env var, accepting the spellings the rest of the
// project uses (true/false, yes/no, 1/0).
func envBool(name string, fallback bool) bool {
	raw := strings.ToLower(strings.TrimSpace(os.Getenv(name)))
	switch raw {
	case "":
		return fallback
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	}
	return fallback
}

func isLoopback(listen string) bool {
	host, _, err := net.SplitHostPort(listen)
	if err != nil {
		return false
	}
	if host == "" {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
