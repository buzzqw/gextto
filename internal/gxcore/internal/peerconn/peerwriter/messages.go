package peerwriter

// BlockUploaded notifies the torrent that one block reached the remote peer,
// feeding the per-torrent upload accounting.
type BlockUploaded struct {
	Length uint32
}
