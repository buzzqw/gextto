package gextto

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// fakeMediaInfo stands in for the mediainfo binary: every call takes the next
// output and its arguments are recorded, so a test can see the parse speed
// asked for and how many reads a file needed.
func fakeMediaInfo(t *testing.T, outputs ...string) func() [][]string {
	t.Helper()
	previous := mediainfoRead
	calls := [][]string{}
	index := 0
	mediainfoRead = func(ctx context.Context, args ...string) ([]byte, error) {
		calls = append(calls, append([]string{}, args...))
		if index >= len(outputs) {
			return nil, errors.New("unexpected mediainfo read")
		}
		output := outputs[index]
		index++
		return []byte(output), nil
	}
	t.Cleanup(func() { mediainfoRead = previous })
	return func() [][]string { return calls }
}

const mediaInfoHeaderJSON = `{"media":{"track":[
	{"@type":"Video","Width":"3840","Height":"2160","Format":"HEVC","HDR_Format":"HDR10"},
	{"@type":"Audio","Format":"E-AC-3","Channels":"6","Language":"it"}]}}`

const mediaInfoSilentVideoJSON = `{"media":{"track":[
	{"@type":"Video","Width":"1920","Height":"1080","Format":"AVC"}]}}`

// The naming read stops at the container header: no whole-file pass for a
// file whose header already describes everything naming needs.
func TestReadMediaTagsAsksForTheHeaderOnly(t *testing.T) {
	calls := fakeMediaInfo(t, mediaInfoHeaderJSON)
	tags := readMediaTags("/media/example.mkv")
	if got := len(calls()); got != 1 {
		t.Fatalf("mediainfo calls = %d, want 1", got)
	}
	args := calls()[0]
	if want := "--ParseSpeed=" + mediainfoHeaderOnly; !containsArg(args, want) {
		t.Fatalf("args = %v, want %s", args, want)
	}
	assertStringPointer(t, tags.Resolution, "2160p")
	assertStringPointer(t, tags.VideoCodec, "h265")
	assertStringPointer(t, tags.AudioCodec, "EAC3")
	assertStringPointer(t, tags.Channels, "5.1")
	assertStringPointer(t, tags.Languages, "IT")
	assertStringPointer(t, tags.HDR, "HDR10")
}

// A container whose header names an audio track but says nothing about it is
// read whole: naming would otherwise lose the codec and the language.
func TestReadMediaTagsReadsTheWholeFileWhenTheHeaderHidesAudio(t *testing.T) {
	const headerOnly = `{"media":{"track":[
		{"@type":"Video","Width":"1920","Height":"1080","Format":"AVC"},
		{"@type":"Audio"}]}}`
	calls := fakeMediaInfo(t, headerOnly, mediaInfoHeaderJSON)
	tags := readMediaTags("/media/example.mkv")
	if got := len(calls()); got != 2 {
		t.Fatalf("mediainfo calls = %d, want 2 (header, then the whole file)", got)
	}
	if containsArg(calls()[1], "--ParseSpeed="+mediainfoHeaderOnly) {
		t.Fatalf("second read = %v, want the default whole-file pass", calls()[1])
	}
	assertStringPointer(t, tags.AudioCodec, "EAC3")
	assertStringPointer(t, tags.Languages, "IT")
}

// A video with no audio track at all has nothing the header could hide, so it
// is not read twice.
func TestReadMediaTagsDoesNotRepeatASilentVideo(t *testing.T) {
	calls := fakeMediaInfo(t, mediaInfoSilentVideoJSON)
	tags := readMediaTags("/media/silent.mkv")
	if got := len(calls()); got != 1 {
		t.Fatalf("mediainfo calls = %d, want 1", got)
	}
	assertStringPointer(t, tags.VideoCodec, "h264")
	assertNilString(t, tags.AudioCodec)
}

func containsArg(args []string, want string) bool {
	for _, arg := range args {
		if strings.TrimSpace(arg) == want {
			return true
		}
	}
	return false
}
