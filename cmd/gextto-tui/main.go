// Command gextto-tui is the terminal client for a running Gextto daemon.
//
// It is a separate binary from gexttod because the daemon is Unix-only, while
// the TUI needs just a terminal and the daemon's HTTP API. This client builds
// on Linux, macOS and Windows; the daemon keeps running on its own machine.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/buzzqw/gextto/internal/tui"
)

func main() {
	url := flag.String("url", tui.DefaultURL(), "URL of the running Gextto daemon (or GEXTTO_URL)")
	lang := flag.String("lang", "", "interface language: it, en, de, fr, es, pl (or GEXTTO_LANG)")
	flag.Parse()

	if err := tui.Run(context.Background(), tui.Options{URL: *url, Lang: *lang}); err != nil {
		fmt.Fprintln(os.Stderr, "gextto-tui:", err)
		os.Exit(1)
	}
}
