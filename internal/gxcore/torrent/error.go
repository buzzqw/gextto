package torrent

import (
	"github.com/buzzqw/gextto/internal/gxcore/internal/announcer"
)

// InputError wraps a problem with the caller's input to Session.AddTorrent
// and Session.AddURI (bad magnet, unreadable .torrent, unknown id, ...).
type InputError struct {
	err error
}

func newInputError(err error) *InputError {
	return &InputError{err: err}
}

// Error implements the error interface.
func (e *InputError) Error() string {
	return "input error: " + e.err.Error()
}

// Unwrap exposes the underlying cause for errors.Is/As.
func (e *InputError) Unwrap() error {
	return e.err
}

// AnnounceError surfaces a tracker-side failure delivered inside an
// otherwise valid announce response.
type AnnounceError struct {
	err *announcer.AnnounceError
}

// Error renders the human-readable tracker message.
func (e *AnnounceError) Error() string {
	return e.err.Message
}

// Unwrap exposes the underlying cause for errors.Is/As.
func (e *AnnounceError) Unwrap() error {
	return e.err.Err
}

// Unknown reports whether the failure is unexpected. Tracker rejections,
// network errors and DNS errors are expected; anything else is unknown.
func (e *AnnounceError) Unknown() bool {
	return e.err.Unknown
}
