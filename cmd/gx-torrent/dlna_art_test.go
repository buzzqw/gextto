package main

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// artImage encodes a w×h image of one colour as JPEG or PNG.
func artImage(t testing.TB, w, h int, asPNG bool) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = 200, 40, 40, 255
	}
	var buf bytes.Buffer
	var err error
	if asPNG {
		err = png.Encode(&buf, img)
	} else {
		err = jpeg.Encode(&buf, img, nil)
	}
	if err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// dlnaArtFixture builds a library laid out the way Jellyfin/Kodi save artwork:
//
//	Film/movie.mkv + movie-poster.jpg + movie-fanart.jpg
//	Film/other.mkv                      (no image, no folder image)
//	Show/folder.jpg, Show/season02-poster.png
//	Show/ep.mkv + ep-thumb.jpg
//	Show/Season 02/s02e01.mkv           (season poster from the series)
//	Show/Season 03/s03e01.mkv           (no season poster: series folder.jpg)
//	Show/stray.jpg                      (an image is servable, a txt is not)
func dlnaArtFixture(t *testing.T) (*dlnaServer, string) {
	t.Helper()
	lib := filepath.Join(t.TempDir(), "lib")
	write := func(rel string, data []byte) {
		p := filepath.Join(lib, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("Film/movie.mkv", []byte("video"))
	write("Film/movie-poster.jpg", artImage(t, 600, 900, false))
	write("Film/movie-fanart.jpg", artImage(t, 1920, 1080, false))
	write("Film/other.mkv", []byte("video"))
	write("Show/folder.jpg", artImage(t, 100, 150, false))
	write("Show/season02-poster.png", artImage(t, 400, 600, true))
	write("Show/ep.mkv", []byte("video"))
	write("Show/ep-thumb.jpg", artImage(t, 1280, 720, false))
	write("Show/Season 02/s02e01.mkv", []byte("video"))
	write("Show/Season 03/s03e01.mkv", []byte("video"))
	write("Show/stray.jpg", artImage(t, 50, 50, false))
	write("Show/notes.txt", []byte("x"))
	write("Show/@eaDir/ep.mkv/SYNOVIDEO_VIDEO_SCREENSHOT.jpg", artImage(t, 50, 50, false))
	srv := dlnaTestServer()
	srv.libRoots = dlnaLibRoots([]string{lib})
	return srv, lib
}

func TestDLNAArtInDIDL(t *testing.T) {
	srv, _ := dlnaArtFixture(t)
	art := func(rel string) string {
		return `<upnp:albumArtURI dlna:profileID="JPEG_TN">http://h:8200/art/0/` + rel + `?pn=JPEG_TN</upnp:albumArtURI>` +
			`<upnp:albumArtURI dlna:profileID="JPEG_SM">http://h:8200/art/0/` + rel + `?pn=JPEG_SM</upnp:albumArtURI>`
	}
	cases := []struct {
		id      string
		want    []string
		notWant []string
	}{
		// The poster wins over the fanart; a video without image has none
		// (Film has no folder image).
		{"lib:0/Film", []string{`<dc:title>movie</dc:title><upnp:class>object.item.videoItem</upnp:class>` + art("Film/movie-poster.jpg")},
			[]string{"fanart", "other-"}},
		// A subfolder always offers its folder URL; the episode its thumb.
		{"lib:0", []string{`<dc:title>Show</dc:title><upnp:class>object.container.storageFolder</upnp:class>` + art("Show/")}, nil},
		{"lib:0/Show", []string{art("Show/ep-thumb.jpg"), art("Show/Season%2002/")}, nil},
		// The season folder takes Jellyfin's season poster from the series
		// folder; without one, the series folder image.
		{"lib:0/Show/Season 02", []string{art("Show/season02-poster.png")}, nil},
		{"lib:0/Show/Season 03", []string{art("Show/folder.jpg")}, nil},
	}
	for _, c := range cases {
		didl, _ := browseIDs(t, srv, c.id)
		for _, w := range c.want {
			if !strings.Contains(didl, w) {
				t.Errorf("%s: missing %s\n%s", c.id, w, didl)
			}
		}
		for _, nw := range c.notWant {
			if strings.Contains(didl, nw) {
				t.Errorf("%s: must not contain %q\n%s", c.id, nw, didl)
			}
		}
	}
	// The other movie has no albumArtURI at all.
	didl, _, _, _ := srv.browseObject(srv.library(), "lib:0/Film/other.mkv", "BrowseMetadata", 0, 0, "http://h:8200")
	if strings.Contains(didl, "albumArtURI") {
		t.Fatalf("a video without artwork got one: %s", didl)
	}
}

func TestDLNAArtServed(t *testing.T) {
	srv, _ := dlnaArtFixture(t)
	ts := httptest.NewServer(srv.handler())
	defer ts.Close()

	get := func(u string) (*http.Response, []byte) {
		t.Helper()
		resp, err := http.Get(ts.URL + u)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		return resp, body
	}
	for _, c := range []struct {
		url      string
		maxW     int
		maxH     int
		wantW    int
		wantH    int
		profile  string
		wantSame bool // already fits: the original bytes
	}{
		{"/art/0/Film/movie-poster.jpg?pn=JPEG_TN", 160, 160, 106, 160, "JPEG_TN", false},
		{"/art/0/Film/movie-poster.jpg?pn=JPEG_SM", 640, 480, 320, 480, "JPEG_SM", false},
		{"/art/0/Film/movie-poster.jpg", 160, 160, 106, 160, "JPEG_TN", false},
		{"/art/0/Show/ep-thumb.jpg?pn=JPEG_SM", 640, 480, 640, 360, "JPEG_SM", false},
		{"/art/0/Show/season02-poster.png?pn=JPEG_TN", 160, 160, 106, 160, "JPEG_TN", false}, // PNG becomes JPEG
		{"/art/0/Show/?pn=JPEG_SM", 640, 480, 100, 150, "JPEG_SM", true},                     // folder.jpg already fits
		{"/art/0/Show/Season%2002/", 160, 160, 106, 160, "JPEG_TN", false},
	} {
		resp, body := get(c.url)
		if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "image/jpeg" ||
			!strings.Contains(resp.Header.Get("contentFeatures.dlna.org"), "DLNA.ORG_PN="+c.profile) {
			t.Fatalf("%s: %d %v", c.url, resp.StatusCode, resp.Header)
		}
		cfg, format, err := image.DecodeConfig(bytes.NewReader(body))
		if err != nil || format != "jpeg" || cfg.Width != c.wantW || cfg.Height != c.wantH {
			t.Fatalf("%s: %s %dx%d %v, want jpeg %dx%d", c.url, format, cfg.Width, cfg.Height, err, c.wantW, c.wantH)
		}
		if c.wantSame && len(body) != len(artImage(t, 100, 150, false)) {
			t.Errorf("%s: an image that fits must be served as is", c.url)
		}
	}
	// The colour survives the scaling (area averaging, no black borders).
	_, body := get("/art/0/Film/movie-poster.jpg?pn=JPEG_TN")
	img, _ := jpeg.Decode(bytes.NewReader(body))
	r, g, b, _ := img.At(50, 80).RGBA()
	if c := (color.RGBA{uint8(r >> 8), uint8(g >> 8), uint8(b >> 8), 0}); c.R < 180 || c.G > 70 || c.B > 70 {
		t.Fatalf("thumbnail colour %v, want about (200,40,40)", c)
	}
	// Not artwork, not in the library, or not existing: 404.
	for _, u := range []string{
		"/art/0/Show/notes.txt", "/art/0/Film/movie.mkv", "/art/0/../lib/Show/folder.jpg",
		"/art/0/Show/@eaDir/ep.mkv/SYNOVIDEO_VIDEO_SCREENSHOT.jpg", "/art/0/Film/", // Film has no folder image
		"/art/0/Show/missing.jpg", "/art/1/Show/folder.jpg", "/art/0/Show/folder.jpg?pn=PNG_LRG",
	} {
		if resp, _ := get(u); resp.StatusCode != http.StatusNotFound {
			t.Errorf("%s: %d, want 404", u, resp.StatusCode)
		}
	}
	// A loose image of a library folder is servable.
	if resp, _ := get("/art/0/Show/stray.jpg"); resp.StatusCode != 200 {
		t.Errorf("stray image: %d", resp.StatusCode)
	}
}

func TestDLNAArtRefusesHugeImages(t *testing.T) {
	// A PNG whose header claims 20000×20000 pixels: refused from the header,
	// before decoding.
	small := artImage(t, 1, 1, true)
	big := append([]byte(nil), small...)
	// IHDR width/height at bytes 16..23, then the chunk CRC (type+data) at 29.
	copy(big[16:24], []byte{0, 0, 0x4e, 0x20, 0, 0, 0x4e, 0x20})
	binary.BigEndian.PutUint32(big[29:33], crc32.ChecksumIEEE(big[12:29]))
	if _, err := dlnaMakeThumb(big, dlnaArtTN); err != errArtTooLarge {
		t.Fatalf("huge image: err %v, want %v", err, errArtTooLarge)
	}
}

func TestDLNAArtFitAndCache(t *testing.T) {
	for _, c := range []struct{ w, h, ww, wh int }{
		{1920, 1080, 160, 90}, {600, 900, 106, 160}, {100, 50, 100, 50}, {10000, 1, 160, 1},
	} {
		if w, h := dlnaArtFit(c.w, c.h, dlnaArtTN); w != c.ww || h != c.wh {
			t.Errorf("fit %dx%d = %dx%d, want %dx%d", c.w, c.h, w, h, c.ww, c.wh)
		}
	}
	testTime := time.Unix(1700000000, 0)
	c := newDLNAArtCache()
	chunk := make([]byte, dlnaArtCacheBytes/8)
	for i := range 20 {
		c.put(string(rune('a'+i)), 1, testTime, chunk)
	}
	if c.bytes > dlnaArtCacheBytes || len(c.byKey) != 8 {
		t.Fatalf("cache over budget: %d bytes, %d entries", c.bytes, len(c.byKey))
	}
	if _, ok := c.get("t", 1, testTime); !ok {
		t.Fatal("the most recent entry was evicted")
	}
	if _, ok := c.get("t", 2, testTime); ok {
		t.Fatal("a changed source must not hit the cache")
	}
}

func BenchmarkDLNAArtThumb(b *testing.B) {
	src := artImage(b, 1920, 1080, false)
	b.ReportAllocs()
	for b.Loop() {
		if _, err := dlnaMakeThumb(src, dlnaArtSM); err != nil {
			b.Fatal(err)
		}
	}
}
