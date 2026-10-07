package gextto

// comicinfo.go writes a ComicInfo.xml into downloaded CBZ files, the metadata
// file that Komga, Kavita, Mylar and most tablet readers use to group issues
// by series and sort them by number. Only direct downloads are touched: a
// comic downloaded by torrent keeps seeding and its bytes must not change.
// CBR (RAR) files cannot be rewritten and are left as they are.

import (
	"archive/zip"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/buzzqw/gextto/internal/logging"
)

// comicInfo is the subset of the ComicInfo 2.0 schema Gextto can fill.
type comicInfo struct {
	XMLName   xml.Name `xml:"ComicInfo"`
	XSI       string   `xml:"xmlns:xsi,attr"`
	XSD       string   `xml:"xmlns:xsd,attr"`
	Title     string   `xml:"Title,omitempty"`
	Series    string   `xml:"Series,omitempty"`
	Number    string   `xml:"Number,omitempty"`
	Year      int      `xml:"Year,omitempty"`
	Publisher string   `xml:"Publisher,omitempty"`
	Summary   string   `xml:"Summary,omitempty"`
	Web       string   `xml:"Web,omitempty"`
	Notes     string   `xml:"Notes,omitempty"`
}

var comicYearPattern = regexp.MustCompile(`\((\d{4})\)`)

// comicInfoFromTitle reads series, issue number and year from a GetComics
// title such as "Poison Ivy #41 (2025)".
func comicInfoFromTitle(title string) comicInfo {
	title = strings.TrimSpace(title)
	info := comicInfo{Title: title, Notes: "Tagged by Gextto"}
	series := title
	if index := strings.Index(series, "#"); index >= 0 {
		series = series[:index]
	}
	series = strings.TrimSpace(comicYearPattern.ReplaceAllString(series, ""))
	series = strings.TrimRight(series, " -–:")
	info.Series = series
	if number := issueNumber(title); number != nil {
		if parsed, err := strconv.Atoi(*number); err == nil {
			info.Number = strconv.Itoa(parsed)
		}
	}
	if match := comicYearPattern.FindStringSubmatch(title); len(match) == 2 {
		info.Year, _ = strconv.Atoi(match[1])
	}
	return info
}

// addComicInfo adds ComicInfo.xml to a CBZ that does not have one yet. It
// rewrites the archive through a hidden sibling (entries are copied without
// recompressing) and replaces the file in one rename. It reports whether the
// file was changed.
func addComicInfo(path string, info comicInfo) (bool, error) {
	extension := strings.ToLower(filepath.Ext(path))
	if extension != ".cbz" {
		return false, nil
	}
	reader, err := zip.OpenReader(path)
	if err != nil {
		return false, err
	}
	defer reader.Close()
	for _, file := range reader.File {
		if strings.EqualFold(filepath.Base(file.Name), "ComicInfo.xml") {
			return false, nil
		}
	}
	info.XSI = "http://www.w3.org/2001/XMLSchema-instance"
	info.XSD = "http://www.w3.org/2001/XMLSchema"
	encoded, err := xml.MarshalIndent(info, "", "  ")
	if err != nil {
		return false, err
	}
	temporary := filepath.Join(filepath.Dir(path), fmt.Sprintf(".%s.gextto-tag-%s", filepath.Base(path), randomToken()))
	output, err := os.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return false, err
	}
	fail := func(cause error) (bool, error) {
		output.Close()
		_ = os.Remove(temporary)
		return false, cause
	}
	writer := zip.NewWriter(output)
	for _, file := range reader.File {
		if err := writer.Copy(file); err != nil {
			return fail(err)
		}
	}
	entry, err := writer.CreateHeader(&zip.FileHeader{Name: "ComicInfo.xml", Method: zip.Deflate, Modified: comicInfoModTime(path)})
	if err != nil {
		return fail(err)
	}
	if _, err := io.WriteString(entry, xml.Header+string(encoded)+"\n"); err != nil {
		return fail(err)
	}
	if err := writer.Close(); err != nil {
		return fail(err)
	}
	if err := output.Sync(); err != nil {
		return fail(err)
	}
	if err := output.Close(); err != nil {
		_ = os.Remove(temporary)
		return false, err
	}
	if err := os.Rename(temporary, path); err != nil {
		_ = os.Remove(temporary)
		return false, err
	}
	return true, nil
}

func comicInfoModTime(path string) time.Time {
	if stat, err := os.Stat(path); err == nil {
		return stat.ModTime()
	}
	return time.Now()
}

// tagDownloadedComic adds ComicInfo.xml to a comic that was just downloaded
// directly. A failure never affects the download: the file stays as it was.
func tagDownloadedComic(path, title string) {
	if strings.TrimSpace(path) == "" {
		return
	}
	changed, err := addComicInfo(path, comicInfoFromTitle(title))
	switch {
	case err != nil:
		logging.Warn(fmt.Sprintf("Could not add ComicInfo.xml to «%s»; the comic is kept as downloaded", filepath.Base(path)),
			"error", err.Error())
	case changed:
		logging.Debug("ComicInfo.xml added", "file", path, "title", title)
	}
}
