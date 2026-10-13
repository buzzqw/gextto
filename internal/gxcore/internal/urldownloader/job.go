package urldownloader

import (
	"github.com/buzzqw/gextto/internal/gxcore/internal/piece"
)

// downloadJob is one contiguous byte range fetched from a single file over
// HTTP. Adjacent sections of the same file merge into one job, so a torrent
// with many small files does not drown the webseed in tiny range requests.
type downloadJob struct {
	Filename   string
	RangeBegin int64
	Length     int64
	// Padding jobs are never requested: padding files do not exist on the
	// server, their bytes are always zero and are skipped in the piece
	// buffer instead.
	Padding bool
}

// createJobs plans the HTTP fetches covering pieces [begin, end).
// Consecutive sections sharing file and padding flag coalesce; zero-length
// jobs (empty files) are dropped.
func createJobs(pieces []piece.Piece, begin, end uint32) []downloadJob {
	if begin == end {
		return nil
	}
	jobs := make([]downloadJob, 0)
	var current downloadJob
	flush := func() {
		if current.Length > 0 { // do not request 0 byte files
			jobs = append(jobs, current)
		}
	}
	for i := begin; i < end; i++ {
		for j, sec := range pieces[i].Data {
			if i == 0 && j == 0 {
				current = downloadJob{
					Filename:   sec.Name,
					RangeBegin: sec.Offset,
					Length:     sec.Length,
					Padding:    sec.Padding,
				}
				continue
			}
			if sec.Name == current.Filename && sec.Padding == current.Padding {
				current.Length += sec.Length
				continue
			}
			flush()
			current = downloadJob{
				Filename:   sec.Name,
				RangeBegin: sec.Offset,
				Length:     sec.Length,
				Padding:    sec.Padding,
			}
		}
	}
	flush()
	return jobs
}
