package torrent

import (
	"archive/tar"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/buzzqw/gextto/internal/gxcore/internal/resumer/boltdbresumer"
)

// handleMoveTorrent receives a torrent moved from another session over HTTP.
// The request is a multipart body with three parts, in order:
// "id" (torrent id), "metadata" (JSON resume spec), "data" (tar of the files).
func (h *rpcHandler) handleMoveTorrent(w http.ResponseWriter, r *http.Request) {
	provider, ok := h.session.storage.(*fileStorageProvider)
	if !ok {
		h.failMove(w, errors.New("session is not using file storage"), http.StatusInternalServerError)
		return
	}

	port, err := h.session.getPort()
	if err != nil {
		h.failMove(w, err, http.StatusInternalServerError)
		return
	}
	// The port is consumed only on success; a failed transfer gives it back.
	succeeded := false
	defer func() {
		if !succeeded {
			h.session.releasePort(port)
		}
	}()

	mr, err := r.MultipartReader()
	if err != nil {
		h.failMove(w, err, http.StatusInternalServerError)
		return
	}

	id, err := readMoveID(mr)
	if err != nil {
		h.failMove(w, err, moveErrorCode(err))
		return
	}
	if err := h.dropDuplicateTorrent(id); err != nil {
		h.failMove(w, err, http.StatusInternalServerError)
		return
	}

	spec, err := readMoveSpec(mr, port)
	if err != nil {
		h.failMove(w, err, moveErrorCode(err))
		return
	}

	if err := readMoveData(mr, provider.getDataDir(id), h.session.config.FilePermissions); err != nil {
		h.failMove(w, err, moveErrorCode(err))
		return
	}

	if err := h.session.resumer.Write(id, spec); err != nil {
		h.failMove(w, err, http.StatusInternalServerError)
		return
	}
	t, started, err := h.session.loadExistingTorrent(id)
	if err != nil {
		h.failMove(w, err, http.StatusInternalServerError)
		return
	}
	if started {
		if err := t.Start(); err != nil {
			h.failMove(w, err, http.StatusInternalServerError)
			return
		}
	}
	succeeded = true
}

// failMove reports the failure to the sender. Server-side failures are also
// logged; client-side protocol violations are not, matching the original
// handler's logging behavior.
func (h *rpcHandler) failMove(w http.ResponseWriter, err error, code int) {
	if code != http.StatusBadRequest {
		h.session.log.Error(err)
	}
	http.Error(w, err.Error(), code)
}

// moveProtocolError marks client-side violations of the transfer protocol
// (wrong part order, missing fields). Anything else is a server failure.
type moveProtocolError struct {
	err error
}

func (e *moveProtocolError) Error() string {
	return e.err.Error()
}

// moveErrorCode maps a step failure to its HTTP status.
func moveErrorCode(err error) int {
	var proto *moveProtocolError
	if errors.As(err, &proto) {
		return http.StatusBadRequest
	}
	return http.StatusInternalServerError
}

// nextMovePart advances the multipart reader and checks the part name.
func nextMovePart(mr *multipart.Reader, want string) (*multipart.Part, error) {
	p, err := mr.NextPart()
	if err != nil {
		return nil, err
	}
	if p.FormName() != want {
		return nil, &moveProtocolError{err: fmt.Errorf("%s expected in multipart form", want)}
	}
	return p, nil
}

// readMoveID reads the "id" part: the torrent id being transferred.
func readMoveID(mr *multipart.Reader) (string, error) {
	// case "id":
	p, err := nextMovePart(mr, "id")
	if err != nil {
		return "", err
	}
	b, err := io.ReadAll(p)
	if err != nil {
		return "", err
	}
	if len(b) == 0 {
		return "", &moveProtocolError{err: errors.New("id required")}
	}
	return string(b), nil
}

// dropDuplicateTorrent removes the local torrent when the sender pushes an
// id we already have, so the incoming transfer starts from a clean slate.
func (h *rpcHandler) dropDuplicateTorrent(id string) error {
	if _, ok := h.session.torrents[id]; !ok {
		return nil
	}
	h.session.log.Warningln("duplicate torrent id, removing existing one:", id)
	t, err := h.session.removeTorrentFromClient(id)
	if err != nil {
		return err
	}
	return h.session.stopAndRemoveData(t, false)
}

// readMoveSpec decodes the "metadata" part into a resume spec bound to port.
func readMoveSpec(mr *multipart.Reader, port int) (*boltdbresumer.Spec, error) {
	// case "metadata":
	p, err := nextMovePart(mr, "metadata")
	if err != nil {
		return nil, err
	}
	var s boltdbresumer.Spec
	if err := json.NewDecoder(p).Decode(&s); err != nil {
		return nil, err
	}
	s.Port = port
	return &s, nil
}

// readMoveData unpacks the "data" tar part into the torrent's data dir.
func readMoveData(mr *multipart.Reader, dir string, perm fs.FileMode) error {
	// case "data":
	p, err := nextMovePart(mr, "data")
	if err != nil {
		return err
	}
	return readData(p, dir, perm)
}

func readData(r io.Reader, dir string, perm fs.FileMode) error {
	dir = filepath.Clean(dir)
	tr := tar.NewReader(r)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		name := filepath.Join(dir, hdr.Name)
		// Reject entries that escape the destination directory (zip/tar slip).
		// filepath.Join cleans ".." segments, so a malicious header name
		// would otherwise resolve to a path outside dir. Legitimate archives
		// produced by generateTar only contain regular files nested under dir,
		// so every valid entry has the dir+separator prefix.
		if !strings.HasPrefix(name, dir+string(os.PathSeparator)) {
			return fmt.Errorf("tar entry %q escapes destination directory", hdr.Name)
		}
		if err := os.MkdirAll(filepath.Dir(name), os.ModeDir|perm); err != nil {
			return err
		}
		if err := writeFile(name, tr); err != nil {
			return err
		}
	}
	return nil
}

// writeFile creates name and copies r into it, syncing before close. The file
// handle is closed on every path so a failed entry does not leak a descriptor.
func writeFile(name string, r io.Reader) error {
	f, err := os.Create(name)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := io.Copy(f, r); err != nil { // nolint: gosec
		return err
	}
	return f.Sync()
}
