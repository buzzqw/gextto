package main

import (
	"fmt"
	"strings"
)

// Mode is how the daemon is being run.
type Mode string

const (
	// ModeManaged: Gextto owns the daemon. The flags Gextto passes win, the
	// standalone modules stay off and the fingerprint stays stable.
	ModeManaged Mode = "managed"
	// ModeStandalone: the daemon is the product; settings.json is authoritative
	// for the options Gextto does not pass.
	ModeStandalone Mode = "standalone"
)

// resolveMode picks the mode. An explicit -mode wins; otherwise a daemon
// started by Gextto (which always passes -fingerprint) is managed, and one
// started by hand is standalone.
func resolveMode(flagValue, fingerprint string) (Mode, error) {
	switch strings.ToLower(strings.TrimSpace(flagValue)) {
	case "":
		if strings.TrimSpace(fingerprint) != "" {
			return ModeManaged, nil
		}
		return ModeStandalone, nil
	case string(ModeManaged):
		return ModeManaged, nil
	case string(ModeStandalone):
		return ModeStandalone, nil
	default:
		return "", fmt.Errorf("invalid -mode %q: use managed or standalone", flagValue)
	}
}
