package torrent

import (
	"io/fs"
	"path/filepath"

	"github.com/buzzqw/gextto/internal/gxcore/internal/storage"
	"github.com/buzzqw/gextto/internal/gxcore/internal/storage/filestorage"
)

type fileStorageProvider struct {
	DataDir                  string
	DataDirIncludesTorrentID bool
	FilePermissions          fs.FileMode
	// Preallocate reserves the full size of new files (gextto fork).
	Preallocate bool
}

func newFileStorageProvider(cfg *Config) *fileStorageProvider {
	return &fileStorageProvider{
		DataDir:                  cfg.DataDir,
		DataDirIncludesTorrentID: cfg.DataDirIncludesTorrentID,
		FilePermissions:          cfg.FilePermissions,
		Preallocate:              cfg.Preallocate,
	}
}

func (p *fileStorageProvider) GetStorage(torrentID string) (storage.Storage, error) {
	sto, err := filestorage.New(p.getDataDir(torrentID), p.FilePermissions)
	if err != nil {
		return nil, err
	}
	sto.Preallocate = p.Preallocate
	return sto, nil
}

func (p *fileStorageProvider) getDataDir(torrentID string) string {
	if p.DataDirIncludesTorrentID {
		return filepath.Join(p.DataDir, torrentID)
	}
	return p.DataDir
}
