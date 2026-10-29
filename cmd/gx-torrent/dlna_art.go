package main

// dlna_art.go gives the library videos and folders their artwork: the images
// Gextto, Kodi, Jellyfin and Emby already keep beside the media (movie-thumb.jpg,
// movie-poster.jpg, folder.jpg, poster.jpg) become the upnp:albumArtURI of the
// item or folder, so a TV shows a poster grid instead of generic icons. The
// image is scaled to the DLNA thumbnail profiles (JPEG_TN 160×160, JPEG_SM
// 640×480) with the standard library and kept in a small in-memory cache: no
// external tool and nothing written to the library.

import (
	"bytes"
	"container/list"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	_ "image/png" // decoder for .png artwork
	"net/http"
	"os"
	"path"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// dlnaArtMaxPixels bounds the source image a thumbnail is made from: a
	// crafted header claiming a gigantic image must not allocate gigabytes.
	dlnaArtMaxPixels = 40_000_000
	dlnaArtMaxFile   = 32 << 20
	// dlnaArtCacheBytes bounds the thumbnails kept in memory (~10 KB each
	// for JPEG_TN, ~50 KB for JPEG_SM).
	dlnaArtCacheBytes = 8 << 20
	dlnaArtQuality    = 85
)

// dlnaArtProfile is one DLNA image profile a thumbnail is scaled to.
type dlnaArtProfile struct {
	id   string // DLNA.ORG_PN
	w, h int
}

var (
	dlnaArtTN = dlnaArtProfile{"JPEG_TN", 160, 160}
	dlnaArtSM = dlnaArtProfile{"JPEG_SM", 640, 480}
)

// dlnaArtProfiles are offered in this order: the small thumbnail first (the
// one every renderer accepts), then the larger one for big tiles.
var dlnaArtProfiles = []dlnaArtProfile{dlnaArtTN, dlnaArtSM}

func dlnaArtProfileByQuery(q string) (dlnaArtProfile, bool) {
	for _, p := range dlnaArtProfiles {
		if strings.EqualFold(q, p.id) {
			return p, true
		}
	}
	return dlnaArtProfile{}, false
}

// dlnaArtImage reports whether a file name is an artwork image.
func dlnaArtImage(name string) bool {
	switch strings.ToLower(path.Ext(name)) {
	case ".jpg", ".jpeg", ".png":
		return true
	}
	return false
}

// Artwork names, best first. A video takes <stem><suffix>.<ext> (Kodi and
// Jellyfin naming, and a same-name image); a folder takes <name>.<ext>, and a
// video without its own image falls back to the folder's.
var (
	dlnaItemArtSuffixes = []string{"-thumb", "-poster", "-landscape", "", "-fanart", "-backdrop"}
	dlnaFolderArtNames  = []string{"folder", "poster", "cover", "thumb", "landscape", "fanart", "backdrop"}
	dlnaArtExts         = []string{".jpg", ".jpeg", ".png"}
)

// pickArt finds the first candidate stem present in images (keys lowercased).
func pickArt(images map[string]string, stems []string) string {
	for _, stem := range stems {
		for _, ext := range dlnaArtExts {
			if name, ok := images[strings.ToLower(stem+ext)]; ok {
				return name
			}
		}
	}
	return ""
}

// assignArt fills the listing's artwork (paths relative to the library
// root): the folder image, else inherited (a season folder's poster from the
// series folder), and for each video its own image or else the folder's.
func (l *dlnaDirListing) assignArt(rel, inherited string) {
	if name := pickArt(l.images, dlnaFolderArtNames); name != "" {
		l.folderArt = path.Join(rel, name)
	} else {
		l.folderArt = inherited
	}
	for _, e := range l.folder.items {
		if !e.video {
			continue
		}
		name := l.names[e.fileIndex]
		stem := strings.TrimSuffix(name, path.Ext(name))
		stems := make([]string, len(dlnaItemArtSuffixes))
		for i, s := range dlnaItemArtSuffixes {
			stems[i] = stem + s
		}
		art := l.folderArt
		if own := pickArt(l.images, stems); own != "" {
			art = path.Join(rel, own)
		}
		if art != "" {
			if l.itemArt == nil {
				l.itemArt = map[string]string{}
			}
			l.itemArt[name] = art
		}
	}
}

// dlnaSeasonDir matches a season folder name in the languages Jellyfin, Kodi
// and Gextto use ("Season 01", "Stagione 1", "S01", "Specials").
var dlnaSeasonDir = regexp.MustCompile(`(?i)^(?:season|stagione|staffel|saison|temporada|sezon|series|s)[ ._-]*0*(\d{1,3})$`)

// seasonArt is the image a season folder inherits from its series folder:
// Jellyfin's seasonNN-poster.jpg (season-specials-poster.jpg for season 0),
// else the series poster. "" when rel is not a season folder.
func (s *dlnaServer) seasonArt(n int, rel string) string {
	if rel == "" {
		return ""
	}
	base := path.Base(rel)
	season := -1
	if m := dlnaSeasonDir.FindStringSubmatch(base); m != nil {
		season, _ = strconv.Atoi(m[1])
	} else if strings.EqualFold(base, "specials") || strings.EqualFold(base, "speciali") {
		season = 0
	}
	if season < 0 {
		return ""
	}
	parent := path.Dir(rel)
	if parent == "." {
		parent = ""
	}
	listing, ok := s.readLibDir(n, parent)
	if !ok {
		return ""
	}
	tag := fmt.Sprintf("season%02d", season)
	if season == 0 {
		tag = "season-specials"
	}
	if name := pickArt(listing.images, []string{tag + "-poster", tag + "-landscape", tag + "-banner", tag}); name != "" {
		return path.Join(parent, name)
	}
	return listing.folderArt
}

// isArt reports whether name is an artwork image of the listing.
func (l *dlnaDirListing) isArt(name string) bool {
	return name != "" && l.images[strings.ToLower(name)] == name
}

// writeAlbumArt writes the albumArtURI elements for an artwork URL ("" none).
func writeAlbumArt(b *strings.Builder, artURL string) {
	if artURL == "" {
		return
	}
	for _, p := range dlnaArtProfiles {
		b.WriteString(`<upnp:albumArtURI dlna:profileID="` + p.id + `">` + xmlEscape(artURL+"?pn="+p.id) + `</upnp:albumArtURI>`)
	}
}

// dlnaArtURL is the artwork URL of a library image: /art/<n>/<escaped path>.
func dlnaArtURL(base string, n int, rel string) string {
	return strings.Replace(dlnaLibURL(base, n, rel), "/lib/", "/art/", 1)
}

// ---------------------------------------------------------------------------
// Scaling and cache
// ---------------------------------------------------------------------------

// dlnaArtCache keeps the scaled thumbnails, least recently used out first.
// A source that changed (size or modification time) is scaled again.
type dlnaArtCache struct {
	mu    sync.Mutex
	order *list.List // of *dlnaArtThumb, most recent first
	byKey map[string]*list.Element
	bytes int
	// sem bounds the concurrent decodes: a TV opening a folder asks for every
	// tile at once, and a decode holds the full-size image in memory.
	sem chan struct{}
}

type dlnaArtThumb struct {
	key  string
	size int64
	mod  time.Time
	data []byte
}

func newDLNAArtCache() *dlnaArtCache {
	return &dlnaArtCache{order: list.New(), byKey: map[string]*list.Element{}, sem: make(chan struct{}, 2)}
}

func (c *dlnaArtCache) get(key string, size int64, mod time.Time) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	el, ok := c.byKey[key]
	if !ok {
		return nil, false
	}
	t := el.Value.(*dlnaArtThumb)
	if t.size != size || !t.mod.Equal(mod) {
		c.order.Remove(el)
		delete(c.byKey, key)
		c.bytes -= len(t.data)
		return nil, false
	}
	c.order.MoveToFront(el)
	return t.data, true
}

func (c *dlnaArtCache) put(key string, size int64, mod time.Time, data []byte) {
	if len(data) > dlnaArtCacheBytes/4 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.byKey[key]; ok {
		c.bytes -= len(el.Value.(*dlnaArtThumb).data)
		c.order.Remove(el)
	}
	c.byKey[key] = c.order.PushFront(&dlnaArtThumb{key: key, size: size, mod: mod, data: data})
	c.bytes += len(data)
	for c.bytes > dlnaArtCacheBytes {
		el := c.order.Back()
		t := el.Value.(*dlnaArtThumb)
		c.order.Remove(el)
		delete(c.byKey, t.key)
		c.bytes -= len(t.data)
	}
}

// dlnaArtFit is the size of a w×h image scaled to fit the profile box,
// keeping the aspect ratio and never enlarging it.
func dlnaArtFit(w, h int, p dlnaArtProfile) (int, int) {
	if w <= p.w && h <= p.h {
		return w, h
	}
	// Scale by the tighter side: w/p.w vs h/p.h, compared without floats.
	if w*p.h >= h*p.w {
		return p.w, max(1, h*p.w/w)
	}
	return max(1, w*p.h/h), p.h
}

// dlnaMakeThumb decodes an image and returns it as a JPEG that fits the
// profile. The header is checked first so an image too large to decode is
// refused without allocating it.
func dlnaMakeThumb(data []byte, p dlnaArtProfile) ([]byte, error) {
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || int64(cfg.Width)*int64(cfg.Height) > dlnaArtMaxPixels {
		return nil, errArtTooLarge
	}
	w, h := dlnaArtFit(cfg.Width, cfg.Height, p)
	if format == "jpeg" && w == cfg.Width && h == cfg.Height {
		return data, nil // already fits: no re-encoding
	}
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	if err := jpeg.Encode(&out, dlnaScale(src, w, h), &jpeg.Options{Quality: dlnaArtQuality}); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

type artError string

func (e artError) Error() string { return string(e) }

const errArtTooLarge = artError("image too large")

// dlnaScale shrinks src to w×h by area averaging (a box filter: every source
// pixel counts once, so the thumbnail has no aliasing). The source is first
// converted to RGBA by draw.Draw, which has fast paths for the JPEG (YCbCr)
// and PNG layouts.
func dlnaScale(src image.Image, w, h int) *image.RGBA {
	if ycc, ok := src.(*image.YCbCr); ok {
		return dlnaScaleYCbCr(ycc, w, h)
	}
	sb := src.Bounds()
	rgba, ok := src.(*image.RGBA)
	if !ok || sb.Min != (image.Point{}) {
		rgba = image.NewRGBA(image.Rect(0, 0, sb.Dx(), sb.Dy()))
		draw.Draw(rgba, rgba.Bounds(), src, sb.Min, draw.Src)
	}
	sw, sh := rgba.Rect.Dx(), rgba.Rect.Dy()
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		y0, y1 := y*sh/h, max((y+1)*sh/h, y*sh/h+1)
		for x := 0; x < w; x++ {
			x0, x1 := x*sw/w, max((x+1)*sw/w, x*sw/w+1)
			var r, g, b, a, n uint32
			for sy := y0; sy < y1; sy++ {
				row := rgba.Pix[sy*rgba.Stride+x0*4 : sy*rgba.Stride+x1*4]
				for i := 0; i < len(row); i += 4 {
					r += uint32(row[i])
					g += uint32(row[i+1])
					b += uint32(row[i+2])
					a += uint32(row[i+3])
					n++
				}
			}
			o := y*dst.Stride + x*4
			dst.Pix[o], dst.Pix[o+1], dst.Pix[o+2], dst.Pix[o+3] = uint8(r/n), uint8(g/n), uint8(b/n), uint8(a/n)
		}
	}
	return dst
}

// dlnaScaleYCbCr is dlnaScale for a decoded JPEG: it averages the luma and
// chroma planes in place and converts only the thumbnail's pixels, so the
// full-size image is never copied to RGBA (8 MB for a 1080p still).
func dlnaScaleYCbCr(src *image.YCbCr, w, h int) *image.RGBA {
	sb := src.Rect
	sw, sh := sb.Dx(), sb.Dy()
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		y0, y1 := y*sh/h, max((y+1)*sh/h, y*sh/h+1)
		for x := 0; x < w; x++ {
			x0, x1 := x*sw/w, max((x+1)*sw/w, x*sw/w+1)
			var ys, cbs, crs, n uint32
			for sy := y0; sy < y1; sy++ {
				for sx := x0; sx < x1; sx++ {
					ys += uint32(src.Y[src.YOffset(sb.Min.X+sx, sb.Min.Y+sy)])
					c := src.COffset(sb.Min.X+sx, sb.Min.Y+sy)
					cbs += uint32(src.Cb[c])
					crs += uint32(src.Cr[c])
					n++
				}
			}
			r, g, b := color.YCbCrToRGB(uint8(ys/n), uint8(cbs/n), uint8(crs/n))
			o := y*dst.Stride + x*4
			dst.Pix[o], dst.Pix[o+1], dst.Pix[o+2], dst.Pix[o+3] = r, g, b, 255
		}
	}
	return dst
}

// ---------------------------------------------------------------------------
// HTTP
// ---------------------------------------------------------------------------

// handleLibArt serves the artwork of a library image (/art/<n>/<path>) or of
// a folder (/art/<n>/<dir>/), scaled to the profile in ?pn= (JPEG_TN when
// absent). Only an artwork image of a listed library directory is served:
// never another file type, never a hidden, NAS-system or excluded path.
func (s *dlnaServer) handleLibArt(w http.ResponseWriter, r *http.Request) {
	n, err := strconv.Atoi(r.PathValue("n"))
	if err != nil || n < 0 || n >= len(s.libRoots) || strconv.Itoa(n) != r.PathValue("n") {
		http.NotFound(w, r)
		return
	}
	profile := dlnaArtTN
	if q := r.URL.Query().Get("pn"); q != "" {
		var ok bool
		if profile, ok = dlnaArtProfileByQuery(q); !ok {
			http.NotFound(w, r)
			return
		}
	}
	raw := r.PathValue("path")
	folder := raw == "" || strings.HasSuffix(raw, "/")
	rel, ok := cleanLibRel(strings.TrimSuffix(raw, "/"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	imgRel := rel
	if folder {
		listing, ok := s.readLibDir(n, rel)
		if !ok || listing.folderArt == "" {
			http.NotFound(w, r)
			return
		}
		imgRel = listing.folderArt
	} else {
		dirRel := path.Dir(rel)
		if dirRel == "." {
			dirRel = ""
		}
		listing, ok := s.readLibDir(n, dirRel)
		if rel == "" || !ok || !listing.isArt(path.Base(rel)) {
			http.NotFound(w, r)
			return
		}
	}
	data, ok := s.libThumb(n, imgRel, profile)
	if !ok {
		http.NotFound(w, r)
		return
	}
	h := w.Header()
	h.Set("Content-Type", "image/jpeg")
	h.Set("Cache-Control", "max-age=300")
	h["transferMode.dlna.org"] = []string{"Interactive"}
	h["contentFeatures.dlna.org"] = []string{"DLNA.ORG_PN=" + profile.id + ";DLNA.ORG_OP=01;DLNA.ORG_CI=1;DLNA.ORG_FLAGS=00D00000000000000000000000000000"}
	http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(data))
}

// libThumb returns the scaled thumbnail of a library image, from the cache
// when its source did not change.
func (s *dlnaServer) libThumb(n int, rel string, p dlnaArtProfile) ([]byte, bool) {
	root, err := os.OpenRoot(s.libRoots[n].path)
	if err != nil {
		return nil, false
	}
	defer root.Close()
	f, err := root.Open(rel)
	if err != nil {
		return nil, false
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > dlnaArtMaxFile {
		return nil, false
	}
	key := strconv.Itoa(n) + "\x00" + rel + "\x00" + p.id
	if data, ok := s.art.get(key, info.Size(), info.ModTime()); ok {
		return data, true
	}
	s.art.sem <- struct{}{}
	defer func() { <-s.art.sem }()
	// Another request may have made it while this one waited.
	if data, ok := s.art.get(key, info.Size(), info.ModTime()); ok {
		return data, true
	}
	src := make([]byte, info.Size())
	if _, err := f.ReadAt(src, 0); err != nil {
		return nil, false
	}
	data, err := dlnaMakeThumb(src, p)
	if err != nil {
		logfWarn("DLNA: no thumbnail for %s: %v", rel, err)
		return nil, false
	}
	s.art.put(key, info.Size(), info.ModTime(), data)
	return data, true
}
