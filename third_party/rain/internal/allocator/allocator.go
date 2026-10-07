package allocator

import (
	"github.com/cenkalti/rain/v2/internal/metainfo"
	"github.com/cenkalti/rain/v2/internal/storage"
)

// Allocator allocates files on the disk.
type Allocator struct {
	Files       []File
	HasExisting bool
	HasMissing  bool
	Error       error

	closeC chan struct{}
	doneC  chan struct{}
}

// File on the disk.
type File struct {
	Storage storage.File
	Name    string
	Padding bool
}

// Progress about the allocation.
type Progress struct {
	AllocatedSize int64
}

// New returns a new Allocator.
func New() *Allocator {
	return &Allocator{
		closeC: make(chan struct{}),
		doneC:  make(chan struct{}),
	}
}

// Close the Allocator.
func (a *Allocator) Close() {
	close(a.closeC)
	<-a.doneC
}

// Run the Allocator.
func (a *Allocator) Run(info *metainfo.Info, sto storage.Storage, progressC chan Progress, resultC chan *Allocator) {
	a.RunSelected(info, sto, nil, nil, progressC, resultC)
}

// RunSelected allocates the files, opening the skipped ones (skip is indexed
// like info.Files) in the parts storage instead (gextto fork).
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
		var sf storage.File
		var exists bool
		if f.Padding {
			sf = storage.NewPaddingFile(f.Length)
		} else if i < len(skip) && skip[i] && parts != nil {
			// A skipped file never counts as missing: only its border pieces
			// are ever written, in the parts storage.
			sf, exists, a.Error = parts.Open(f.Path, f.Length)
			if a.Error != nil {
				return
			}
			if exists {
				a.HasExisting = true
			}
		} else {
			sf, exists, a.Error = sto.Open(f.Path, f.Length)
			if a.Error != nil {
				return
			}
			if exists {
				a.HasExisting = true
			} else {
				a.HasMissing = true
			}
		}
		a.Files[i] = File{Storage: sf, Name: f.Path, Padding: f.Padding}
		allocatedSize += f.Length
		a.sendProgress(progressC, allocatedSize)
	}
}

func (a *Allocator) sendProgress(progressC chan Progress, size int64) {
	select {
	case progressC <- Progress{AllocatedSize: size}:
	case <-a.closeC:
		return
	}
}
