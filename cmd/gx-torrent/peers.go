package main

// peers.go holds the peer-level policy shared by the daemon's own page and by
// the qBittorrent-compatible API: how long a banned peer stays banned.

import "time"

// banTTL is how long a peer banned by hand (the Peers tab of the detail page,
// or the compatible API's banPeers) is refused by the session.
const banTTL = 30 * time.Minute
