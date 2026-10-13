package allocator

import (
	"github.com/buzzqw/gextto/internal/gxcore/internal/metainfo"
	"github.com/buzzqw/gextto/internal/gxcore/internal/storage"
)

// Allocator opens every file of a torrent on disk, routing deselected files
// to the parts storage. It reports which files already existed, which were
// missing, and the first error — aborting the run at that point.
type Allocator struct {
	Files       []File
	HasExisting bool
	HasMissing  bool
	Error       error

	closeC chan struct{}
	doneC  chan struct{}
}

// File is one opened torrent file.
type File struct {
	Storage storage.File
	Name    string
	Padding bool
}

// Progress reports the cumulative allocated bytes so far.
type Progress struct {
	AllocatedSize int64
}

// New prepares an allocator; run it with Run or RunSelected.
func New() *Allocator {
	return &Allocator{
		closeC: make(chan struct{}),
		doneC:  make(chan struct{}),
	}
}

// Close aborts the allocation and waits for the run to unwind.
func (a *Allocator) Close() {
	close(a.closeC)
	<-a.doneC
}

// Run allocates every file into sto.
func (a *Allocator) Run(info *metainfo.Info, sto storage.Storage, progressC chan Progress, resultC chan *Allocator) {
	a.RunSelected(info, sto, nil, nil, progressC, resultC)
}

// RunSelected allocates the files, opening the skip-marked ones (indexed
// like info.Files) in parts instead of sto.
func (a *Allocator) RunSelected(info *metainfo.Info, sto storage.Storage, skip []bool, parts storage.Storage, progressC chan Progress, resultC chan *Allocator) {
	defer close(a.doneC)
	defer func() {
		if a.Error != nil {
			for _, f := range a.Files {
				if f.Storage != nil {
					f.Storage.Close()
				}
			}
		}
		select {
		case resultC <- a:
		case <-a.closeC:
		}
	}()

	var allocatedSize int64
	a.Files = make([]File, len(info.Files))
	for i, f := range info.Files {
		sf, failed := a.openOne(i, f, sto, skip, parts)
		if failed {
			return
		}
		a.Files[i] = File{Storage: sf, Name: f.Path, Padding: f.Padding}
		allocatedSize += f.Length
		a.sendProgress(progressC, allocatedSize)
	}
}

// sendProgress reports progress unless the allocator was closed meanwhile.
func (a *Allocator) sendProgress(progressC chan Progress, size int64) {
	select {
	case progressC <- Progress{AllocatedSize: size}:
	case <-a.closeC:
	}
}

// openOne opens a single file, reporting whether it failed (a.Error holds
// the cause). Padding files need no storage; skipped files land in parts and
// never count as missing, since only their border pieces are ever written.
func (a *Allocator) openOne(i int, f metainfo.File, sto storage.Storage, skip []bool, parts storage.Storage) (storage.File, bool) {
	if f.Padding {
		return storage.NewPaddingFile(f.Length), false
	}
	target := sto
	skipped := false
	if i < len(skip) && skip[i] && parts != nil {
		target = parts
		skipped = true
	}
	sf, exists, err := target.Open(f.Path, f.Length)
	if err != nil {
		a.Error = err
		return nil, true
	}
	if exists {
		a.HasExisting = true
	} else if !skipped {
		a.HasMissing = true
	}
	return sf, false
}
