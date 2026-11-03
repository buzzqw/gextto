// audio_cleanup_packets.go is the strongest check of the audio cleanup: it
// reads every packet of the source and of the cleaned copy and proves that each
// kept stream carries the same bytes at the same times. Matching headers can
// hide a damaged copy (a muxer that rewrites codec headers, drops a subtitle
// event or shifts one track); identical packets cannot.

package gextto

import (
	"bufio"
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"math"
	"os/exec"
	"strconv"
	"strings"
)

// audioCleanupPacketTolerance is the timestamp difference accepted between a
// source packet and its copy, relative to the start of each stream. Matroska
// stores milliseconds, so 10ms is rounding, not a shift.
const audioCleanupPacketTolerance = 0.010

// audioCleanupPacket is one packet as the check needs it: its time and the
// MD5 of its payload.
type audioCleanupPacket struct {
	time    float64
	hasTime bool
	hash    [16]byte
}

// audioCleanupPacketLine is one parsed line of ffprobe -show_packets.
type audioCleanupPacketLine struct {
	stream int
	packet audioCleanupPacket
}

// parseAudioCleanupPacketLine reads a compact ffprobe line such as
// "stream_index=1|pts_time=0.021|dts_time=0.021|data_hash=MD5:…". A line
// without a stream index or a hash is not a packet.
func parseAudioCleanupPacketLine(line string) (audioCleanupPacketLine, bool) {
	var parsed audioCleanupPacketLine
	haveStream, haveHash := false, false
	pts, dts := "", ""
	for _, field := range strings.Split(strings.TrimSpace(line), "|") {
		key, value, ok := strings.Cut(field, "=")
		if !ok {
			continue
		}
		switch key {
		case "stream_index":
			index, err := strconv.Atoi(value)
			if err != nil {
				return parsed, false
			}
			parsed.stream = index
			haveStream = true
		case "pts_time":
			pts = value
		case "dts_time":
			dts = value
		case "data_hash":
			digest, err := hex.DecodeString(strings.TrimPrefix(value, "MD5:"))
			if err != nil || len(digest) != len(parsed.packet.hash) {
				return parsed, false
			}
			copy(parsed.packet.hash[:], digest)
			haveHash = true
		}
	}
	if !haveStream || !haveHash {
		return parsed, false
	}
	for _, value := range []string{pts, dts} {
		if seconds, err := strconv.ParseFloat(value, 64); err == nil && !math.IsNaN(seconds) {
			parsed.packet.time = seconds
			parsed.packet.hasTime = true
			break
		}
	}
	return parsed, true
}

// readAudioCleanupPackets streams the packets of path to visit, in file order.
// It demuxes only: nothing is decoded.
func readAudioCleanupPackets(ctx context.Context, path string, visit func(audioCleanupPacketLine) error) error {
	command := exec.CommandContext(ctx, "ffprobe",
		"-v", "error",
		"-protocol_whitelist", "file,pipe",
		"-show_packets", "-show_data_hash", "md5",
		"-show_entries", "packet=stream_index,pts_time,dts_time,data_hash",
		"-of", "compact=p=0",
		path,
	)
	stdout, err := command.StdoutPipe()
	if err != nil {
		return err
	}
	var stderr bytes.Buffer
	command.Stderr = &stderr
	if err := command.Start(); err != nil {
		return err
	}
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	var visitErr error
	for scanner.Scan() {
		parsed, ok := parseAudioCleanupPacketLine(scanner.Text())
		if !ok {
			continue
		}
		if visitErr = visit(parsed); visitErr != nil {
			break
		}
	}
	if visitErr != nil {
		// Stop reading a file that already failed the check.
		_ = command.Process.Kill()
		_ = command.Wait()
		return visitErr
	}
	if err := scanner.Err(); err != nil {
		_ = command.Process.Kill()
		_ = command.Wait()
		return err
	}
	if err := command.Wait(); err != nil {
		detail := audioCleanupLastLine(stderr.String())
		if detail == "" {
			detail = err.Error()
		}
		return fmt.Errorf("cannot read the packets of the file: %s", detail)
	}
	return nil
}

// audioCleanupStreamPairs maps each kept source stream to its copy: the n-th
// stream of a type in the source is the n-th of that type in the output, the
// order the header check already proved.
func audioCleanupStreamPairs(plan AudioCleanupPlan, output MediaInfo, matroska bool) (map[int]int, map[int]string, error) {
	expected := audioCleanupStreamsByType(audioCleanupMappedStreams(plan, matroska), matroska)
	actual := audioCleanupStreamsByType(output.Streams, matroska)
	pairs := make(map[int]int)
	kinds := make(map[int]string)
	for _, kind := range []string{"video", "audio", "subtitle"} {
		want, got := expected[kind], actual[kind]
		if len(want) != len(got) {
			return nil, nil, fmt.Errorf("the remux kept %d %s stream(s) instead of %d", len(got), kind, len(want))
		}
		for position := range want {
			pairs[got[position].Index] = want[position].Index
			kinds[want[position].Index] = kind
		}
	}
	return pairs, kinds, nil
}

// verifyAudioCleanupPackets compares every packet of every kept stream: same
// count, same payload, same time relative to the start of the stream, and the
// same distance of each audio and subtitle stream from the first video packet.
func verifyAudioCleanupPackets(ctx context.Context, sourcePath, outputPath string, source, output MediaInfo, plan AudioCleanupPlan, matroska bool) error {
	pairs, kinds, err := audioCleanupStreamPairs(plan, output, matroska)
	if err != nil {
		return err
	}
	if len(pairs) == 0 {
		return nil
	}
	firstVideo := -1
	for _, stream := range audioCleanupMappedStreams(plan, matroska) {
		if stream.Type == "video" {
			firstVideo = stream.Index
			break
		}
	}

	sourcePackets := make(map[int][]audioCleanupPacket, len(kinds))
	if err := readAudioCleanupPackets(ctx, sourcePath, func(line audioCleanupPacketLine) error {
		if _, kept := kinds[line.stream]; kept {
			sourcePackets[line.stream] = append(sourcePackets[line.stream], line.packet)
		}
		return nil
	}); err != nil {
		return err
	}

	seen := make(map[int]int, len(kinds))
	base := make(map[int]float64, len(kinds))
	if err := readAudioCleanupPackets(ctx, outputPath, func(line audioCleanupPacketLine) error {
		original, ok := pairs[line.stream]
		if !ok {
			return nil
		}
		position := seen[original]
		seen[original] = position + 1
		packets := sourcePackets[original]
		if position >= len(packets) {
			return fmt.Errorf("%s stream %d has more packets than the source", kinds[original], original)
		}
		want := packets[position]
		if want.hash != line.packet.hash {
			return fmt.Errorf("%s stream %d: packet %d differs from the source", kinds[original], original, position)
		}
		if position == 0 {
			if line.packet.hasTime {
				base[original] = line.packet.time
			}
			return nil
		}
		first := packets[0]
		start, haveStart := base[original]
		if !want.hasTime || !line.packet.hasTime || !first.hasTime || !haveStart {
			return nil
		}
		if delta := math.Abs((want.time - first.time) - (line.packet.time - start)); delta > audioCleanupPacketTolerance {
			return fmt.Errorf("%s stream %d: packet %d moved by %.0f ms", kinds[original], original, position, delta*1000)
		}
		return nil
	}); err != nil {
		return err
	}

	for original := range kinds {
		if seen[original] != len(sourcePackets[original]) {
			return fmt.Errorf("%s stream %d has %d packets instead of %d", kinds[original], original, seen[original], len(sourcePackets[original]))
		}
	}

	// The distance from the first video packet is what a viewer perceives as
	// sync: it must be the same in the copy.
	if firstVideo < 0 || len(sourcePackets[firstVideo]) == 0 || !sourcePackets[firstVideo][0].hasTime {
		return nil
	}
	videoStart, haveVideo := base[firstVideo]
	if !haveVideo {
		return nil
	}
	for original, kind := range kinds {
		if kind == "video" || len(sourcePackets[original]) == 0 || !sourcePackets[original][0].hasTime {
			continue
		}
		start, ok := base[original]
		if !ok {
			continue
		}
		before := sourcePackets[original][0].time - sourcePackets[firstVideo][0].time
		after := start - videoStart
		if math.Abs(before-after) > audioCleanupPacketTolerance {
			return fmt.Errorf("%s stream %d: the distance from the video moved from %.3fs to %.3fs", kind, original, before, after)
		}
	}
	return nil
}
