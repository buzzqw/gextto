package peer

import (
	"bytes"
	"compress/zlib"
	"errors"
	"io"
)

// CompressData is the DEFLATE/zlib payload of OP_COMPRESSEDPART.
func CompressData(data []byte) ([]byte, error) {
	var buffer bytes.Buffer
	writer := zlib.NewWriter(&buffer)
	if _, err := writer.Write(data); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

// DecompressData inflates a compressed part with a hard cap, so a malicious
// peer cannot make the daemon allocate without bound.
func DecompressData(data []byte, limit int64) ([]byte, error) {
	if limit <= 0 {
		return nil, errors.New("peer: invalid decompression limit")
	}
	reader, err := zlib.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	out, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(out)) > limit {
		return nil, errors.New("peer: decompressed part is larger than the limit")
	}
	return out, nil
}
