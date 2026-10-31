package gextto

import (
	"encoding/base32"
	"encoding/hex"
	"strings"

	"github.com/buzzqw/gextto/internal/models"
	"github.com/buzzqw/gextto/internal/utils"
)

// Duplicate releases.
//
// The same torrent reaches a cycle many times: the RSS feed and the search of
// the same Jackett indexer, Jackett and Prowlarr, indexarr on top of both. A
// source that exposes only a `.torrent` link has no infohash until the file is
// downloaded, so an identity based on the link alone keeps every copy apart.
// mergeDuplicateReleases folds the copies into one release and keeps the
// others as mirrors: the acquisition uses the best copy and falls back to a
// mirror when its link fails, instead of skipping the episode.

// maxReleaseMirrors bounds the alternative copies kept per release. A popular
// release can appear in dozens of listings; a handful of fallbacks is enough.
const maxReleaseMirrors = 8

// duplicateSizeTolerance is the relative size difference still accepted for
// two copies without a common infohash. Indexers print sizes with two or three
// significant digits ("9.3 GB" for 9.32 GB): 0.6% covers that rounding, while
// two distinct uploads of the same name usually differ by more.
const duplicateSizeTolerance = 0.006

// minDuplicateTitleLength keeps very short normalized titles out of the size
// match, where a collision between unrelated releases is plausible.
const minDuplicateTitleLength = 12

// canonicalInfoHash returns the lowercase hexadecimal form of a v1 (40 hex or
// 32 base32 characters) or v2 (64 hex) infohash, or "" when the value is not
// one. Base32 and hex spell the same v1 hash: both must give one identity.
func canonicalInfoHash(raw string) string {
	value := strings.ToLower(strings.TrimSpace(raw))
	switch {
	case len(value) == 40 && isHexString(value):
		return value
	case len(value) == 64 && isHexString(value):
		return value
	case len(value) == 80 && isHexString(value):
		// Some indexers hex-encode the 40-character hash a second time
		// (Jackett issue #17045). It is not a v2 hash: it is a v1 one wearing
		// two layers of hex. Decode only when the result is itself a hash, so
		// an unrelated 80-character value is still refused.
		decoded, err := hex.DecodeString(value)
		if err == nil && len(decoded) == 40 && isHexString(string(decoded)) {
			return string(decoded)
		}
		return ""
	case len(value) == 32:
		decoded, err := base32.StdEncoding.DecodeString(strings.ToUpper(value))
		if err != nil || len(decoded) != 20 {
			return ""
		}
		return hex.EncodeToString(decoded)
	}
	return ""
}

// releaseInfoHash is the torrent identity of a release: the infohash of its
// magnet, or the one its indexer declared next to a `.torrent` link.
func releaseInfoHash(release *models.Release) string {
	if release == nil {
		return ""
	}
	if release.Magnet == "" {
		return canonicalInfoHash(release.InfoHash)
	}
	if hash, ok := utils.MagnetHash(release.Magnet); ok {
		if canonical := canonicalInfoHash(hash); canonical != "" {
			return canonical
		}
		return hash
	}
	return canonicalInfoHash(release.InfoHash)
}

// duplicateTitleKey normalizes a release name for the size match: case,
// punctuation and separators are ignored ("WEB-DL" = "WEB DL" = "web.dl"),
// every word and number is kept, so group, resolution, codec, languages,
// PROPER/REPACK and episode numbers still tell two releases apart.
func duplicateTitleKey(title string) string {
	out := make([]byte, 0, len(title))
	for i := 0; i < len(title); i++ {
		c := title[i]
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9':
			out = append(out, c)
		case c >= 'A' && c <= 'Z':
			out = append(out, c+('a'-'A'))
		}
	}
	if len(out) < minDuplicateTitleLength {
		return ""
	}
	return string(out)
}

// releaseBlockedByTitle reports whether a release with no torrent identity (a
// `.torrent` link only, no declared infohash) is already on the blocklist.
// The title goes through duplicateTitleKey, so different spellings of the same
// release ("Show.S01E01-GRP" and "Show S01E01 GRP") match. It is only a
// fallback: when the hash is known the hash decides.
func releaseBlockedByTitle(release *models.Release, titles map[string]struct{}) bool {
	if release == nil || len(titles) == 0 {
		return false
	}
	key := duplicateTitleKey(release.Title)
	if key == "" {
		return false
	}
	_, blocked := titles[key]
	return blocked
}

// duplicateSizesMatch reports whether two known sizes are the same file within
// the rounding indexers apply.
func duplicateSizesMatch(a, b int64) bool {
	if a <= 0 || b <= 0 {
		return false
	}
	diff := a - b
	if diff < 0 {
		diff = -diff
	}
	larger := max(a, b)
	return float64(diff) <= float64(larger)*duplicateSizeTolerance
}

// fuzzyDuplicateEligible is true for the releases that may be matched by name
// and size: torrents with a known size. eD2k results have their own hash.
func fuzzyDuplicateEligible(release *models.Release) bool {
	if release.Ed2kLink != "" || (release.Network != "" && release.Network != "torrent") {
		return false
	}
	return release.SizeBytes > 0
}

// preferredCopy reports whether `a` is a better copy of the same release than
// `b`: a swarm known to be alive, then more seeders, then a known infohash (it
// needs no `.torrent` download and cannot be a dead link), then a magnet.
func preferredCopy(a, b *models.Release) bool {
	if aTier, bTier := swarmTier(a), swarmTier(b); aTier != bTier {
		return aTier > bTier
	}
	if a.Seeders != b.Seeders && (a.Seeders > 0 || b.Seeders > 0) {
		return a.Seeders > b.Seeders
	}
	if a.Freeleech != b.Freeleech {
		return a.Freeleech
	}
	aMagnet, bMagnet := strings.TrimSpace(a.Magnet) != "", strings.TrimSpace(b.Magnet) != ""
	if aHash, bHash := aMagnet || a.InfoHash != "", bMagnet || b.InfoHash != ""; aHash != bHash {
		return aHash
	}
	if aMagnet != bMagnet {
		return aMagnet
	}
	return false
}

// addMirror appends a copy to the mirrors of `release`, skipping one with the
// same link and respecting maxReleaseMirrors.
func addMirror(release *models.Release, mirror models.Release) {
	if len(release.Mirrors) >= maxReleaseMirrors {
		return
	}
	link := releaseLink(&mirror)
	if link == "" || link == releaseLink(release) {
		return
	}
	for i := range release.Mirrors {
		if releaseLink(&release.Mirrors[i]) == link {
			return
		}
	}
	mirror.Mirrors = nil
	release.Mirrors = append(release.Mirrors, mirror)
}

// releaseLink is the transfer link of a release: its magnet or `.torrent` URL.
func releaseLink(release *models.Release) string {
	if magnet := strings.TrimSpace(release.Magnet); magnet != "" {
		return magnet
	}
	if release.TorrentURL != nil {
		return strings.TrimSpace(*release.TorrentURL)
	}
	return ""
}

// mergeInto folds `other` into `kept`, which stays the release of the group
// unless `other` is a better copy: then `other` takes its place and every
// former copy becomes a mirror.
func mergeInto(kept *models.Release, other models.Release) {
	// The gap pass marks a copy as a last resort after finding nothing else
	// for the episode; the same file met first in the title search must not
	// lose the mark, or the filter refuses it for its resolution.
	lastResort := kept.LastResort || other.LastResort
	defer func() { kept.LastResort = lastResort }()
	if preferredCopy(&other, kept) {
		previous := *kept
		mirrors := previous.Mirrors
		previous.Mirrors = nil
		*kept = other
		kept.Mirrors = nil
		addMirror(kept, previous)
		for _, mirror := range mirrors {
			addMirror(kept, mirror)
		}
		for _, mirror := range other.Mirrors {
			addMirror(kept, mirror)
		}
	} else {
		mirrors := other.Mirrors
		addMirror(kept, other)
		for _, mirror := range mirrors {
			addMirror(kept, mirror)
		}
	}
}

// mergeDuplicateReleases returns one release per torrent, keeping the order
// of first appearance. Copies are recognized by:
//
//   - the same identity key (infohash, listing guid, `.torrent` URL);
//   - the same infohash, from the magnet or declared by the indexer;
//   - the same normalized name and the same size within the indexers'
//     rounding, when at most one side knows its infohash. Two copies with
//     different known infohashes are never merged: they are different torrents
//     even when the name is the same.
//
// When in doubt the copies stay separate: an extra candidate costs a little
// work, a lost release costs an episode. Releases without any identity are
// kept as they are.
func mergeDuplicateReleases(releases []models.Release) []models.Release {
	if len(releases) < 2 {
		return releases
	}
	out := make([]models.Release, 0, len(releases))
	// groupHash is the infohash known for each kept release, from any of its
	// copies: a later copy with that hash joins it, a different hash never does.
	groupHash := make([]string, 0, len(releases))
	byKey := make(map[string]int, len(releases))
	byHash := make(map[string]int, len(releases))
	byTitle := make(map[string][]int, len(releases))

	findBySize := func(release *models.Release, hash, title string) int {
		best, bestDiff := -1, int64(-1)
		for _, index := range byTitle[title] {
			group := &out[index]
			if !duplicateSizesMatch(group.SizeBytes, release.SizeBytes) {
				continue
			}
			if hash != "" && groupHash[index] != "" && groupHash[index] != hash {
				continue
			}
			diff := group.SizeBytes - release.SizeBytes
			if diff < 0 {
				diff = -diff
			}
			if best < 0 || diff < bestDiff {
				best, bestDiff = index, diff
			}
		}
		return best
	}

	for i := range releases {
		release := releases[i]
		// Each identity is computed once: parsing magnets and URLs is the cost
		// of this pass.
		key, hasKey := releaseDedupKey(&release)
		hash := releaseInfoHash(&release)
		title := ""
		if fuzzyDuplicateEligible(&release) {
			title = duplicateTitleKey(release.Title)
		}
		target := -1
		if hasKey {
			if index, found := byKey[key]; found {
				target = index
			}
		}
		if target < 0 && hash != "" {
			if index, found := byHash[hash]; found {
				target = index
			}
		}
		if target < 0 && title != "" {
			target = findBySize(&release, hash, title)
		}
		if target < 0 {
			out = append(out, release)
			groupHash = append(groupHash, hash)
			target = len(out) - 1
			if title != "" {
				byTitle[title] = append(byTitle[title], target)
			}
		} else {
			mergeInto(&out[target], release)
			if groupHash[target] == "" && hash != "" {
				groupHash[target] = hash
			}
			// A `.torrent` copy kept for its link learns the hash of the
			// group, so its identity is the torrent and not the listing.
			if kept := &out[target]; kept.Magnet == "" && kept.InfoHash == "" && groupHash[target] != "" {
				kept.InfoHash = groupHash[target]
			}
		}
		if hasKey {
			if _, exists := byKey[key]; !exists {
				byKey[key] = target
			}
		}
		if hash != "" {
			if _, exists := byHash[hash]; !exists {
				byHash[hash] = target
			}
		}
	}
	return out
}

// switchToMirror replaces the transfer of `release` with its best usable
// mirror and reports whether there was one. Only the link, its source and the
// swarm counts change: name, quality and episodes stay those of the release
// the cycle already matched. `usable` can refuse a mirror (a quarantined
// link); a refused mirror is dropped.
func switchToMirror(release *models.Release, usable func(*models.Release) bool) bool {
	for len(release.Mirrors) > 0 {
		best := 0
		for i := 1; i < len(release.Mirrors); i++ {
			if preferredCopy(&release.Mirrors[i], &release.Mirrors[best]) {
				best = i
			}
		}
		mirror := release.Mirrors[best]
		release.Mirrors = append(release.Mirrors[:best:best], release.Mirrors[best+1:]...)
		if releaseLink(&mirror) == "" || (usable != nil && !usable(&mirror)) {
			continue
		}
		release.Magnet = mirror.Magnet
		release.TorrentURL = mirror.TorrentURL
		release.GUID = mirror.GUID
		release.Source = mirror.Source
		if mirror.InfoHash != "" {
			release.InfoHash = mirror.InfoHash
		}
		release.Seeders = mirror.Seeders
		release.Peers = mirror.Peers
		release.Freeleech = mirror.Freeleech
		return true
	}
	return false
}

// mirrorNotQuarantined accepts a mirror unless its `.torrent` link is in the
// dead-link quarantine (torrent_link_quarantine.go).
func mirrorNotQuarantined(quarantined map[string]string) func(*models.Release) bool {
	return func(mirror *models.Release) bool {
		if mirror.Magnet == "" && mirror.TorrentURL != nil {
			_, dead := quarantined[*mirror.TorrentURL]
			return !dead
		}
		return true
	}
}
