package peerprotocol

import "io"

// writerCounter counts the bytes flowing through an encoder into w, so
// extension messages (whose length prefix precedes the payload) can report
// their size after encoding.
type writerCounter struct {
	count  int64
	writer io.Writer
}

func newWriterCounter(w io.Writer) *writerCounter {
	return &writerCounter{writer: w}
}

func (w *writerCounter) Write(buf []byte) (int, error) {
	n, err := w.writer.Write(buf)
	w.count += int64(n)
	return n, err
}

// Count returns the bytes written so far.
func (w *writerCounter) Count() int64 {
	return w.count
}
