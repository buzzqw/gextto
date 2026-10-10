package torrent

// limits that change without reopening the session, so a speed
// schedule or a cache retune does not drop every peer connection.

// SetSpeedLimits changes the global download and upload limits in KiB/s
// (0 = unlimited). Connected peers and web seeds use the new rate at once.
func (s *Session) SetSpeedLimits(downloadKiB, uploadKiB int64) {
	s.bucketDownload.SetRate(max(downloadKiB, 0) * 1024)
	s.bucketUpload.SetRate(max(uploadKiB, 0) * 1024)
}

// SetSpeedLimits changes this torrent's download and upload limits in KiB/s
// -1 inherits the session limit, 0 is unlimited, a positive
// value is an explicit limit. The peers already connected use the new rate.
func (t *Torrent) SetSpeedLimits(downloadKiB, uploadKiB int64) {
	t.torrent.sendCommand(func() {
		t.torrent.downloadLimitKib = downloadKiB
		t.torrent.uploadLimitKib = uploadKiB
		t.torrent.bucketDownload.SetLimitKiB(downloadKiB, t.torrent.session.bucketDownload)
		t.torrent.bucketUpload.SetLimitKiB(uploadKiB, t.torrent.session.bucketUpload)
	})
}

// SpeedLimits returns this torrent's explicit limits in KiB/s (-1 = inherit)
func (t *Torrent) SpeedLimits() (downloadKiB, uploadKiB int64) {
	type limits struct{ download, upload int64 }
	got := query(t.torrent, func() limits {
		return limits{t.torrent.downloadLimitKib, t.torrent.uploadLimitKib}
	})
	return got.download, got.upload
}

// SetMaxConnections caps the established peers of this torrent:
// 0 means unlimited, a negative value restores the session default.
func (t *Torrent) SetMaxConnections(value int) {
	t.torrent.sendCommand(func() {
		if value < 0 {
			value = 0
		}
		t.torrent.maxConnections = value
		t.torrent.enforceConnectionLimit()
	})
}

// SetMaxUploads sets the upload slots of this torrent: 0 unchokes
// every interested peer, a negative value restores the default.
func (t *Torrent) SetMaxUploads(value int) {
	t.torrent.sendCommand(func() {
		if value < 0 {
			value = 0
		}
		t.torrent.maxUploads = value
		t.torrent.applyMaxUploads()
	})
}

// MaxConnections and MaxUploads report the per-torrent caps.
func (t *Torrent) MaxConnections() int {
	return query(t.torrent, func() int { return t.torrent.maxConnections })
}

func (t *Torrent) MaxUploads() int {
	return query(t.torrent, func() int { return t.torrent.maxUploads })
}

// SetCacheSizes changes the read cache and the write buffer in bytes. Values
// <= 0 leave the current size.
func (s *Session) SetCacheSizes(readBytes, writeBytes int64) {
	if readBytes > 0 {
		s.pieceCache.SetMaxSize(readBytes)
	}
	if writeBytes > 0 {
		s.ram.SetLimit(writeBytes)
	}
}
