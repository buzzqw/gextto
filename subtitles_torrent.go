package gextto

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/buzzqw/gextto/internal/models"
	subtitleprovider "github.com/buzzqw/gextto/internal/subtitles"
)

// subtitleTorrentSource is the part of the torrent engine needed to resolve a
// subtitle target. The client chooses an index in this list, never a path.
type subtitleTorrentSource interface {
	List() []models.TorrentView
	Files(hash string) ([]models.FileView, bool, error)
}

// resolveSubtitleTorrentTarget resolves one video inside an active torrent.
// Incomplete files are valid search targets: their identity and release name
// can still find useful candidates, but no partial OSHash or size is sent to the
// provider as if it described the finished video.
func resolveSubtitleTorrentTarget(ctx context.Context, cfg *Config, db *Database, torrents subtitleTorrentSource, rawHash string, fileIndex int) (subtitleTarget, error) {
	if cfg == nil || db == nil || torrents == nil {
		return subtitleTarget{}, errors.New("configuration, database or torrent engine unavailable")
	}
	hash := canonicalInfoHash(strings.TrimSpace(rawHash))
	if hash == "" {
		return subtitleTarget{}, errors.New("invalid torrent hash")
	}
	if fileIndex < 0 {
		return subtitleTarget{}, errors.New("invalid video file index")
	}
	var view models.TorrentView
	found := false
	for _, candidate := range torrents.List() {
		if canonicalInfoHash(candidate.Hash) != hash {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(candidate.Network), "ed2k") {
			return subtitleTarget{}, errors.New("subtitles are not available for eD2K downloads")
		}
		view, found = candidate, true
		break
	}
	if !found {
		return subtitleTarget{}, errors.New("torrent not found in the active session")
	}
	meta, err := db.TorrentMeta(hash)
	if err != nil {
		return subtitleTarget{}, err
	}
	if meta != nil {
		if releaseHash := releaseInfoHash(&meta.Release); releaseHash != "" && releaseHash != hash {
			return subtitleTarget{}, errors.New("stored release metadata belongs to another torrent")
		}
	}
	files, ok, err := torrents.Files(hash)
	if err != nil {
		return subtitleTarget{}, err
	}
	if !ok {
		return subtitleTarget{}, errors.New("the torrent engine cannot list this torrent's files")
	}
	if fileIndex >= len(files) {
		return subtitleTarget{}, errors.New("video file index is outside the torrent")
	}
	file := files[fileIndex]
	if file.Priority == 0 {
		return subtitleTarget{}, errors.New("the selected file is excluded from the download")
	}
	if !subtitleVideoExtension(file.Path) {
		return subtitleTarget{}, errors.New("the selected file is not a supported video")
	}
	if isSampleFile(file.Path) {
		return subtitleTarget{}, errors.New("the selected file is a sample, not a video to subtitle")
	}
	complete := file.Size > 0 && file.Downloaded >= file.Size
	videoPath, exists, err := subtitleTorrentVideoPath(view, file, complete)
	if err != nil {
		return subtitleTarget{}, err
	}
	if complete && !exists {
		return subtitleTarget{}, errors.New("the completed video file is not present on disk")
	}
	identity := subtitleTarget{Label: filepath.Base(file.Path), VideoPath: videoPath, ReferenceName: strings.TrimSpace(view.Name)}
	if meta != nil && !tevNeedsManualIdentity(meta) {
		if err := subtitleTargetFromRelease(cfg, &identity, &meta.Release, file.Path); err != nil {
			return subtitleTarget{}, err
		}
	} else {
		if err := subtitleTargetFromNames(cfg, &identity, view.Name, file.Path); err != nil {
			return subtitleTarget{}, err
		}
	}
	identity.TorrentHash = hash
	identity.FileIndex = &fileIndex
	identity.FileComplete = complete
	if complete {
		identity.SizeBytes = file.Size
		if hash, size, err := subtitleprovider.HashFile(videoPath); err == nil {
			identity.OSHash, identity.SizeBytes = hash, size
		}
		if probe, err := probeFileWithinContext(ctx, videoPath, 15*time.Second); err == nil {
			identity.RuntimeSeconds = probe.RuntimeSeconds
		}
	}
	return identity, nil
}

// subtitleTorrentVideoPath joins an engine-reported relative file to SavePath
// through os.Root. ContentPath is deliberately not used: it may already include
// the torrent directory and is not a base for FileView.Path.
func subtitleTorrentVideoPath(view models.TorrentView, file models.FileView, complete bool) (string, bool, error) {
	savePath := strings.TrimSpace(view.SavePath)
	if savePath == "" || !filepath.IsAbs(savePath) {
		return "", false, errors.New("the torrent save path is not absolute")
	}
	raw := strings.TrimSpace(file.Path)
	if raw == "" || strings.ContainsRune(raw, 0) || strings.Contains(raw, `\`) {
		return "", false, errors.New("the torrent file path is invalid")
	}
	relative := filepath.FromSlash(raw)
	if !filepath.IsLocal(relative) || hasParentComponent(relative) {
		return "", false, errors.New("the torrent file path leaves the download folder")
	}
	fullPath := filepath.Join(savePath, relative)
	if !pathStartsWith(fullPath, savePath) {
		return "", false, errors.New("the torrent file path leaves the download folder")
	}
	root, err := os.OpenRoot(savePath)
	if err != nil {
		return "", false, err
	}
	defer root.Close()
	info, err := root.Lstat(relative)
	if os.IsNotExist(err) {
		if complete {
			return "", false, errors.New("the completed video file is not present on disk")
		}
		return fullPath, false, nil
	}
	if err != nil {
		return "", false, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return "", false, errors.New("the subtitle target is not a regular video file")
	}
	if complete && info.Size() != file.Size {
		return "", false, errors.New("the video size on disk differs from the torrent")
	}
	return fullPath, true, nil
}

func subtitleTargetFromNames(cfg *Config, target *subtitleTarget, torrentName, fileName string) error {
	magnet := "magnet:?xt=urn:btih:" + strings.Repeat("0", 40)
	release := ParseRelease(strings.TrimSpace(torrentName), magnet, manualSource)
	if !subtitleReleaseUsableForSearch(cfg, release) {
		release = manualImportParse(fileName)
	}
	if !subtitleReleaseUsableForSearch(cfg, release) {
		return errors.New("the series or film could not be recognised from the torrent or selected file name")
	}
	return subtitleTargetFromRelease(cfg, target, release, fileName)
}

func subtitleReleaseUsableForSearch(cfg *Config, release *models.Release) bool {
	if release == nil {
		return false
	}
	switch release.Kind {
	case "series":
		return release.Series != nil && strings.TrimSpace(*release.Series) != "" && release.Season != nil && (release.Episode != nil || release.IsPack)
	case "movie":
		return strings.TrimSpace(release.Title) != "" && (release.Year != nil || cfg.FindMovieMatchForRelease(release, true) != nil)
	default:
		return false
	}
}

// subtitleTargetFromRelease copies a structured identity into target. For a
// pack, the selected file supplies the real episode; a single-episode release
// must agree with the file so the wrong video cannot silently get its subtitles.
func subtitleTargetFromRelease(cfg *Config, target *subtitleTarget, release *models.Release, fileName string) error {
	if release == nil {
		return errors.New("release identity is unavailable")
	}
	corrected := *release
	release = &corrected
	fileRelease := manualImportParse(fileName)
	if release.Kind == "series" {
		if fileRelease != nil && fileRelease.Kind == "series" && fileRelease.Series != nil && fileRelease.Season != nil {
			if release.Series == nil || !SeriesNamesMatch(*release.Series, *fileRelease.Series) {
				return errors.New("the selected file belongs to another series")
			}
			if release.Season == nil || *release.Season != *fileRelease.Season {
				return errors.New("the selected file belongs to another season")
			}
			if fileRelease.Episode == nil || len(fileRelease.EpisodeRange) > 1 {
				return errors.New("the selected file does not identify one episode")
			}
			episode := *fileRelease.Episode
			release.Episode = &episode
		}
		if fileRelease != nil && fileRelease.Kind == "movie" {
			return errors.New("the selected file does not look like an episode")
		}
		if release.Series == nil || release.Season == nil || release.Episode == nil {
			return errors.New("the selected file does not identify one episode")
		}
		season, episode := *release.Season, *release.Episode
		if season <= 0 || episode < 0 || episode > 10000 {
			return errors.New("the episode identity is outside the supported range")
		}
		series := cfg.FindSeriesMatch(*release.Series, &season)
		title, tmdbID, preference := strings.TrimSpace(*release.Series), strings.TrimSpace(release.TmdbID), ""
		if series != nil {
			title, tmdbID, preference = series.Name, series.TmdbID, series.Subtitle
		}
		if len(title) > 300 {
			return errors.New("the series title is too long")
		}
		target.Kind, target.Series, target.Season, target.Episode = "episode", title, season, episode
		target.Title, target.TMDBID, target.SubtitlePreference = title, tmdbID, preference
		target.Label = fmt.Sprintf("%s S%02dE%02d", title, season, episode)
		return nil
	}
	if release.Kind != "movie" {
		return errors.New("the release is neither a series episode nor a film")
	}
	if fileRelease != nil && fileRelease.Kind == "series" {
		return errors.New("the selected file belongs to a series, not a film")
	}
	if fileRelease != nil && fileRelease.Kind == "movie" && fileRelease.Year != nil && release.Year != nil && absInt64(*fileRelease.Year-*release.Year) > 1 {
		return errors.New("the selected file belongs to another film year")
	}
	movie := cfg.FindMovieMatchForRelease(release, true)
	title, tmdbID, preference := strings.TrimSpace(release.Title), strings.TrimSpace(release.TmdbID), ""
	year := 0
	if release.Year != nil && *release.Year >= 1000 && *release.Year <= 9999 {
		year = int(*release.Year)
	}
	movieID := int64(0)
	if movie != nil {
		title, tmdbID, preference, movieID = movie.Name, movie.TmdbID, movie.Subtitle, movie.ID
		year, _ = parsePositiveYear(movie.Year)
	}
	if title == "" || len(title) > 300 {
		return errors.New("the film title is missing or too long")
	}
	if year == 0 && movie == nil && tmdbID == "" {
		return errors.New("the film year or external id could not be recognised")
	}
	target.Kind, target.MovieID, target.Title, target.Year = "movie", movieID, title, year
	target.TMDBID, target.SubtitlePreference = tmdbID, preference
	target.Label = title
	if year > 0 {
		target.Label = fmt.Sprintf("%s (%d)", title, year)
	}
	return nil
}
