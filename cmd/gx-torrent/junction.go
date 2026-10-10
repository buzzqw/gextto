package main

import (
	"encoding/binary"
	"unicode/utf16"
)

// ioReparseTagMountPoint is Windows' IO_REPARSE_TAG_MOUNT_POINT (a junction).
const ioReparseTagMountPoint = 0xA0000003

// mountPointReparseData builds the REPARSE_DATA_BUFFER of a junction to
// target, an NT path such as `\??\C:\dir`. The layout is the one mklink /J
// writes and the kernel validates: the substitute name without its NUL, a NUL,
// an empty print name and its NUL; PrintNameOffset is SubstituteNameLength+2.
// It is plain byte layout, so it is tested on every platform.
func mountPointReparseData(target string) []byte {
	name := utf16.Encode([]rune(target))
	nameBytes := len(name) * 2
	const (
		headerLen = 8 // ReparseTag, ReparseDataLength, Reserved
		mountLen  = 8 // SubstituteName/PrintName offsets and lengths
	)
	pathLen := nameBytes + 2 + 2 // substitute name, NUL, empty print name, NUL
	buffer := make([]byte, headerLen+mountLen+pathLen)
	binary.LittleEndian.PutUint32(buffer[0:4], ioReparseTagMountPoint)
	binary.LittleEndian.PutUint16(buffer[4:6], uint16(mountLen+pathLen)) // ReparseDataLength
	binary.LittleEndian.PutUint16(buffer[6:8], 0)                        // Reserved
	binary.LittleEndian.PutUint16(buffer[8:10], 0)                       // SubstituteNameOffset
	binary.LittleEndian.PutUint16(buffer[10:12], uint16(nameBytes))      // SubstituteNameLength
	binary.LittleEndian.PutUint16(buffer[12:14], uint16(nameBytes+2))    // PrintNameOffset
	binary.LittleEndian.PutUint16(buffer[14:16], 0)                      // PrintNameLength
	for i, unit := range name {
		binary.LittleEndian.PutUint16(buffer[16+i*2:], unit)
	}
	return buffer
}
