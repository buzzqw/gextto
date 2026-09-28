package gextto

import (
	"os"
	"os/exec"
	"time"

	"github.com/buzzqw/gextto/internal/logging"
)

// requestServiceActionLater schedules a systemd action on the gextto service
// shortly after the caller returns. The HTTP handler has its own response-then-
// restart path; this helper is used by the qBittorrent watchdog so a failed
// managed runtime can switch back to libtorrent with a clean restart.
func requestServiceActionLater(action string) {
	verb := "restart"
	switch action {
	case "start":
		verb = "start"
	case "stop":
		verb = "stop"
	}
	go func() {
		time.Sleep(1500 * time.Millisecond)
		if scope, scopeName := gh6_serviceScope("gextto.service"); scopeName == "user" {
			if err := exec.Command("systemctl", append(append([]string{}, scope...), verb, "gextto.service")...).Run(); err != nil {
				logging.Error("riavvio del servizio gextto non riuscito", "scope", "user", "error", err)
			}
			return
		}
		const restartHelper = "/usr/local/bin/gextto-restart"
		if _, err := os.Stat(restartHelper); err != nil {
			logging.Error("helper di riavvio non installato: impossibile applicare il cambio di motore", "helper", restartHelper)
			return
		}
		if err := exec.Command("sudo", "-n", restartHelper, verb).Run(); err != nil {
			logging.Error("riavvio del servizio gextto non riuscito", "error", err)
		}
	}()
}
