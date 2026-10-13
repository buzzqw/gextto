package torrent

import (
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
)

// runOnCompleteCmd fires the operator's completion hook for a finished
// torrent. The hook inherits the daemon environment plus the GX_TORRENT_*
// variables describing the torrent; failures are logged, never fatal.
func (s *Session) runOnCompleteCmd(torrent *torrent) {
	command, err := exec.LookPath(s.config.OnCompleteCmd[0])
	if err != nil {
		s.log.Errorf("error resolving completion hook command path: %s", err)
		return
	}
	cmd := exec.Command(command)
	cmd.Args = append(cmd.Args, s.config.OnCompleteCmd[1:]...)
	cmd.Env = append(os.Environ(),
		"GX_TORRENT_ADDED="+fmt.Sprint(torrent.addedAt.Unix()),
		"GX_TORRENT_DIR="+torrent.Dir(),
		"GX_TORRENT_HASH="+hex.EncodeToString(torrent.infoHash[:]),
		"GX_TORRENT_ID="+torrent.id,
		"GX_TORRENT_NAME="+torrent.name)

	s.log.Debugf("executing completion hook for torrent %s: %s", torrent.id, cmd.String())
	if err := cmd.Run(); err != nil {
		s.log.Errorf("completion hook execution failed: %s", err)
	}
}
