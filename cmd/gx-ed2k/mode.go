package main

import (
	"fmt"
	"strings"
)

// Mode is how gx-ed2k is running. Managed means Gextto started it with a
// fingerprint and owns its lifecycle; standalone means it is its own product,
// with a web page and no token when it listens on loopback.
type Mode string

const (
	modeManaged    Mode = "managed"
	modeStandalone Mode = "standalone"
)

// resolveMode mirrors gx-torrent: an explicit flag wins, otherwise the
// presence of a fingerprint (only Gextto sends one) means managed.
func resolveMode(flagValue, fingerprint string) (Mode, error) {
	switch strings.ToLower(strings.TrimSpace(flagValue)) {
	case "":
		if strings.TrimSpace(fingerprint) != "" {
			return modeManaged, nil
		}
		return modeStandalone, nil
	case string(modeManaged):
		return modeManaged, nil
	case string(modeStandalone):
		return modeStandalone, nil
	default:
		return "", fmt.Errorf("invalid -mode %q: use managed or standalone", flagValue)
	}
}
