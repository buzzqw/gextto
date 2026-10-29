// Package boltdbresumer persists torrent resume records in BoltDB: one
// sub-bucket per torrent id, one key per field. Key names, text formats and
// version semantics are the on-disk contract — existing session.db files
// must keep loading, including pre-version records and the old []string
// tracker shape.
package boltdbresumer

import (
	"encoding/json"
	"fmt"
	"runtime/debug"
	"strconv"
	"time"

	"go.etcd.io/bbolt"
)

// LatestVersion bumps on every backwards-incompatible reinterpretation of
// stored records.
const LatestVersion = 3

// Keys names every persisted field.
var Keys = struct {
	InfoHash          []byte
	Port              []byte
	Name              []byte
	Trackers          []byte
	URLList           []byte
	FixedPeers        []byte
	Dest              []byte
	Info              []byte
	Bitfield          []byte
	PieceLayers       []byte
	AddedAt           []byte
	BytesDownloaded   []byte
	BytesUploaded     []byte
	BytesWasted       []byte
	SeededFor         []byte
	Started           []byte
	StopAfterDownload []byte
	StopAfterMetadata []byte
	CompleteCmdRun    []byte
	Sequential        []byte
	FirstLast         []byte
	SuperSeeding      []byte
	SeedMode          []byte
	FilePaths         []byte
	DisplayName       []byte
	ContentLayout     []byte
	Version           []byte
}{
	InfoHash:          []byte("info_hash"),
	Port:              []byte("port"),
	Name:              []byte("name"),
	Trackers:          []byte("trackers"),
	URLList:           []byte("url_list"),
	FixedPeers:        []byte("fixed_peers"),
	Dest:              []byte("dest"),
	Info:              []byte("info"),
	Bitfield:          []byte("bitfield"),
	PieceLayers:       []byte("piece_layers"),
	AddedAt:           []byte("added_at"),
	BytesDownloaded:   []byte("bytes_downloaded"),
	BytesUploaded:     []byte("bytes_uploaded"),
	BytesWasted:       []byte("bytes_wasted"),
	SeededFor:         []byte("seeded_for"),
	Started:           []byte("started"),
	StopAfterDownload: []byte("stop_after_download"),
	StopAfterMetadata: []byte("stop_after_metadata"),
	CompleteCmdRun:    []byte("complete_cmd_run"),
	Sequential:        []byte("sequential"),
	FirstLast:         []byte("first_last"),
	SuperSeeding:      []byte("super_seeding"),
	SeedMode:          []byte("seed_mode"),
	FilePaths:         []byte("file_paths"),
	DisplayName:       []byte("display_name"),
	ContentLayout:     []byte("content_layout"),
	Version:           []byte("version"),
}

// Resumer reads and writes resume records in a BoltDB bucket.
type Resumer struct {
	db     *bbolt.DB
	bucket []byte
}

// New opens the bucket, creating it on first use.
func New(db *bbolt.DB, bucket []byte) (*Resumer, error) {
	err := db.Update(func(tx *bbolt.Tx) error {
		_, err2 := tx.CreateBucketIfNotExists(bucket)
		return err2
	})
	if err != nil {
		return nil, err
	}
	return &Resumer{
		db:     db,
		bucket: bucket,
	}, nil
}

// Write stores the full spec of torrentID. An explicit spec.Version sticks;
// zero means "current".
func (r *Resumer) Write(torrentID string, spec *Spec) error {
	trackers, err := json.Marshal(spec.Trackers)
	if err != nil {
		return err
	}
	urlList, err := json.Marshal(spec.URLList)
	if err != nil {
		return err
	}
	fixedPeers, err := json.Marshal(spec.FixedPeers)
	if err != nil {
		return err
	}
	filePaths, err := json.Marshal(spec.FilePaths)
	if err != nil {
		return err
	}
	version := LatestVersion
	if spec.Version != 0 {
		version = spec.Version
	}
	return r.db.Update(func(tx *bbolt.Tx) error {
		b, err := tx.Bucket(r.bucket).CreateBucketIfNotExists([]byte(torrentID))
		if err != nil {
			return err
		}
		_ = b.Put(Keys.InfoHash, spec.InfoHash)
		_ = b.Put(Keys.Port, []byte(strconv.Itoa(spec.Port)))
		_ = b.Put(Keys.Name, []byte(spec.Name))
		_ = b.Put(Keys.Trackers, trackers)
		_ = b.Put(Keys.URLList, urlList)
		_ = b.Put(Keys.FixedPeers, fixedPeers)
		_ = b.Put(Keys.Info, spec.Info)
		_ = b.Put(Keys.PieceLayers, spec.PieceLayers)
		_ = b.Put(Keys.Bitfield, spec.Bitfield)
		_ = b.Put(Keys.AddedAt, []byte(spec.AddedAt.Format(time.RFC3339)))
		_ = b.Put(Keys.BytesDownloaded, []byte(strconv.FormatInt(spec.BytesDownloaded, 10)))
		_ = b.Put(Keys.BytesUploaded, []byte(strconv.FormatInt(spec.BytesUploaded, 10)))
		_ = b.Put(Keys.BytesWasted, []byte(strconv.FormatInt(spec.BytesWasted, 10)))
		_ = b.Put(Keys.SeededFor, []byte(spec.SeededFor.String()))
		_ = b.Put(Keys.Started, []byte(strconv.FormatBool(spec.Started)))
		_ = b.Put(Keys.StopAfterDownload, []byte(strconv.FormatBool(spec.StopAfterDownload)))
		_ = b.Put(Keys.StopAfterMetadata, []byte(strconv.FormatBool(spec.StopAfterMetadata)))
		_ = b.Put(Keys.CompleteCmdRun, []byte(strconv.FormatBool(spec.CompleteCmdRun)))
		_ = b.Put(Keys.Sequential, []byte(strconv.FormatBool(spec.Sequential)))
		_ = b.Put(Keys.FirstLast, []byte(strconv.FormatBool(spec.FirstLast)))
		_ = b.Put(Keys.SuperSeeding, []byte(strconv.FormatBool(spec.SuperSeeding)))
		_ = b.Put(Keys.SeedMode, []byte(strconv.FormatBool(spec.SeedMode)))
		_ = b.Put(Keys.FilePaths, filePaths)
		_ = b.Put(Keys.DisplayName, []byte(spec.DisplayName))
		_ = b.Put(Keys.ContentLayout, []byte(spec.ContentLayout))
		_ = b.Put(Keys.Version, []byte(strconv.Itoa(version)))
		return nil
	})
}

// update applies fn to one torrent's sub-bucket, skipping missing torrents.
func (r *Resumer) update(torrentID string, fn func(b *bbolt.Bucket) error) error {
	return r.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket(r.bucket).Bucket([]byte(torrentID))
		if b == nil {
			return nil
		}
		return fn(b)
	})
}

// WriteInfo replaces only the info dict.
func (r *Resumer) WriteInfo(torrentID string, value []byte) error {
	return r.update(torrentID, func(b *bbolt.Bucket) error {
		return b.Put(Keys.Info, value)
	})
}

// WritePieceLayers replaces only the bencoded BEP 52 piece layers,
// persisted once a magnet fetches them from peers.
func (r *Resumer) WritePieceLayers(torrentID string, value []byte) error {
	return r.update(torrentID, func(b *bbolt.Bucket) error {
		return b.Put(Keys.PieceLayers, value)
	})
}

// WriteBitfield replaces only the bitfield.
func (r *Resumer) WriteBitfield(torrentID string, value []byte) error {
	return r.update(torrentID, func(b *bbolt.Bucket) error {
		return b.Put(Keys.Bitfield, value)
	})
}

// WriteStarted replaces only the started flag.
func (r *Resumer) WriteStarted(torrentID string, value bool) error {
	return r.update(torrentID, func(b *bbolt.Bucket) error {
		return b.Put(Keys.Started, []byte(strconv.FormatBool(value)))
	})
}

// WriteFilePaths replaces only the file-path override list (nil/empty clears it).
func (r *Resumer) WriteFilePaths(torrentID string, paths []string) error {
	raw, err := json.Marshal(paths)
	if err != nil {
		return err
	}
	return r.update(torrentID, func(b *bbolt.Bucket) error {
		return b.Put(Keys.FilePaths, raw)
	})
}

// WriteDisplayName replaces only the display-name override (a qBittorrent
// torrent rename). The original name stays under Keys.Name.
func (r *Resumer) WriteDisplayName(torrentID, name string) error {
	return r.update(torrentID, func(b *bbolt.Bucket) error {
		return b.Put(Keys.DisplayName, []byte(name))
	})
}

// HandleStopAfterDownload clears started and stop-after-download together.
func (r *Resumer) HandleStopAfterDownload(torrentID string) error {
	return r.update(torrentID, func(b *bbolt.Bucket) error {
		if err := b.Put(Keys.Started, []byte(strconv.FormatBool(false))); err != nil {
			return err
		}
		return b.Put(Keys.StopAfterDownload, []byte(strconv.FormatBool(false)))
	})
}

// HandleStopAfterMetadata clears started and stop-after-metadata together.
func (r *Resumer) HandleStopAfterMetadata(torrentID string) error {
	return r.update(torrentID, func(b *bbolt.Bucket) error {
		if err := b.Put(Keys.Started, []byte(strconv.FormatBool(false))); err != nil {
			return err
		}
		return b.Put(Keys.StopAfterMetadata, []byte(strconv.FormatBool(false)))
	})
}

// WriteCompleteCmdRun marks the completion hook as fired.
func (r *Resumer) WriteCompleteCmdRun(torrentID string) error {
	return r.update(torrentID, func(b *bbolt.Bucket) error {
		return b.Put(Keys.CompleteCmdRun, []byte(strconv.FormatBool(true)))
	})
}

// WriteSuperSeeding replaces only the super-seeding flag.
func (r *Resumer) WriteSuperSeeding(torrentID string, value bool) error {
	return r.update(torrentID, func(b *bbolt.Bucket) error {
		return b.Put(Keys.SuperSeeding, []byte(strconv.FormatBool(value)))
	})
}

// WriteSeedMode replaces only the seed-mode flag.
func (r *Resumer) WriteSeedMode(torrentID string, value bool) error {
	return r.update(torrentID, func(b *bbolt.Bucket) error {
		return b.Put(Keys.SeedMode, []byte(strconv.FormatBool(value)))
	})
}

// Read loads one torrent's spec. Bolt values die with the transaction, so
// every byte field is copied out. A storage fault (SIGBUS on a broken
// mapping) becomes an error instead of a crash.
func (r *Resumer) Read(torrentID string) (spec *Spec, err error) {
	defer debug.SetPanicOnFault(debug.SetPanicOnFault(true))
	defer func() {
		if rec := recover(); rec != nil {
			err = fmt.Errorf("cannot read torrent %q from db: %s", torrentID, rec)
		}
	}()
	err = r.db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket(r.bucket).Bucket([]byte(torrentID))
		if b == nil {
			return fmt.Errorf("bucket not found: %q", torrentID)
		}
		if b.Get(Keys.InfoHash) == nil {
			return fmt.Errorf("key not found: %q", string(Keys.InfoHash))
		}
		spec = new(Spec)
		spec.InfoHash = copied(b, Keys.InfoHash)
		var err error
		if spec.Port, err = requiredInt(b, Keys.Port); err != nil {
			return err
		}
		if v := b.Get(Keys.Name); v != nil {
			spec.Name = string(v)
		}
		if v := b.Get(Keys.DisplayName); v != nil {
			spec.DisplayName = string(v)
		}
		if v := b.Get(Keys.ContentLayout); v != nil {
			spec.ContentLayout = string(v)
		}
		if err := readTrackers(b, spec); err != nil {
			return err
		}
		if v := b.Get(Keys.URLList); v != nil {
			if err := json.Unmarshal(v, &spec.URLList); err != nil {
				return err
			}
		}
		if v := b.Get(Keys.FixedPeers); v != nil {
			if err := json.Unmarshal(v, &spec.FixedPeers); err != nil {
				return err
			}
		}
		spec.Info = copied(b, Keys.Info)
		spec.PieceLayers = copied(b, Keys.PieceLayers)
		spec.Bitfield = copied(b, Keys.Bitfield)
		if v := b.Get(Keys.FilePaths); v != nil {
			if err := json.Unmarshal(v, &spec.FilePaths); err != nil {
				return err
			}
		}
		if v := b.Get(Keys.AddedAt); v != nil {
			if spec.AddedAt, err = time.Parse(time.RFC3339, string(v)); err != nil {
				return err
			}
		}
		for _, num := range []struct {
			key []byte
			dst *int64
		}{
			{Keys.BytesDownloaded, &spec.BytesDownloaded},
			{Keys.BytesUploaded, &spec.BytesUploaded},
			{Keys.BytesWasted, &spec.BytesWasted},
		} {
			if v := b.Get(num.key); v != nil {
				if *num.dst, err = strconv.ParseInt(string(v), 10, 64); err != nil {
					return err
				}
			}
		}
		if v := b.Get(Keys.SeededFor); v != nil {
			if spec.SeededFor, err = time.ParseDuration(string(v)); err != nil {
				return err
			}
		}
		for _, flag := range []struct {
			key []byte
			dst *bool
		}{
			{Keys.Started, &spec.Started},
			{Keys.StopAfterDownload, &spec.StopAfterDownload},
			{Keys.StopAfterMetadata, &spec.StopAfterMetadata},
			{Keys.CompleteCmdRun, &spec.CompleteCmdRun},
			{Keys.Sequential, &spec.Sequential},
			{Keys.FirstLast, &spec.FirstLast},
			{Keys.SuperSeeding, &spec.SuperSeeding},
			{Keys.SeedMode, &spec.SeedMode},
		} {
			if v := b.Get(flag.key); v != nil {
				if *flag.dst, err = strconv.ParseBool(string(v)); err != nil {
					return err
				}
			}
		}
		if v := b.Get(Keys.Version); v != nil {
			if spec.Version, err = strconv.Atoi(string(v)); err != nil {
				return err
			}
		} else {
			// The version key arrived in version 2: older records read as v1.
			spec.Version = 1
		}
		return nil
	})
	return
}

// copied returns a copy of the value under key, or nil when absent. The copy
// is mandatory: Bolt reuses value memory across transactions.
func copied(b *bbolt.Bucket, key []byte) []byte {
	v := b.Get(key)
	if v == nil {
		return nil
	}
	out := make([]byte, len(v))
	copy(out, v)
	return out
}

// requiredInt parses a mandatory integer field.
func requiredInt(b *bbolt.Bucket, key []byte) (int, error) {
	return strconv.Atoi(string(b.Get(key)))
}

// readTrackers decodes the tier list, migrating the legacy flat []string
// shape into tiers and persisting the migration in place.
func readTrackers(b *bbolt.Bucket, spec *Spec) error {
	v := b.Get(Keys.Trackers)
	if v == nil {
		return nil
	}
	if err := json.Unmarshal(v, &spec.Trackers); err == nil {
		return nil
	}
	var flat []string
	if err := json.Unmarshal(v, &flat); err != nil {
		return err
	}
	spec.Trackers = make([][]string, len(flat))
	for i, t := range flat {
		spec.Trackers[i] = []string{t}
	}
	migrated, err := json.Marshal(spec.Trackers)
	if err != nil {
		return err
	}
	return b.Put(Keys.Trackers, migrated)
}
