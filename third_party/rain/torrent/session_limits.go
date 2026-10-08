package torrent

// gextto fork: limits that change without reopening the session, so a speed
// schedule or a cache retune does not drop every peer connection.

// SetSpeedLimits changes the global download and upload limits in KiB/s
// (0 = unlimited). Connected peers and web seeds use the new rate at once.
func (s *Session) SetSpeedLimits(downloadKiB, uploadKiB int64) {
	s.bucketDownload.SetRate(max(downloadKiB, 0) * 1024)
	s.bucketUpload.SetRate(max(uploadKiB, 0) * 1024)
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
