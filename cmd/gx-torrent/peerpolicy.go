package main

import (
	"html"
	"strconv"
	"strings"
	"time"

	"github.com/buzzqw/gextto/internal/gxcore/torrent"
)

// Session-wide peer policies read at runtime from settings.json, with the
// start-up flag as fallback: the trading share of a limited upload and the
// client filter. They are applied live by applyPeerPoliciesLocked, called
// from the speed-limit tick, so a change on the page bites within seconds.

const (
	// uploadTradingShareKey is the percent of a limited upload that goes
	// first to trading peers (0 = off).
	uploadTradingShareKey = "upload-trading-share"
	// clientFilterKey turns the client filter on; clientFilterListKey holds
	// its rules, one per line or comma-separated.
	clientFilterKey     = "client-filter"
	clientFilterListKey = "client-filter-list"
)

// uploadTradingShare returns the trading share in percent, 0 when off.
func (d *Daemon) uploadTradingShare() int {
	share := d.settingInt(uploadTradingShareKey, d.opts.Network.UploadTradingShare)
	if share < 1 || share > 99 {
		return 0
	}
	return share
}

// clientFilterEnabled reports whether refused clients are filtered.
func (d *Daemon) clientFilterEnabled() bool {
	return d.settingBool(clientFilterKey, d.opts.Network.ClientFilter)
}

// clientFilterList returns the rules of the client filter: the saved list,
// the start-up one, or the suggested default.
func (d *Daemon) clientFilterList() []string {
	raw := d.opts.Network.ClientFilterList
	if d.opts.Settings != nil {
		if v := d.opts.Settings.Get(clientFilterListKey, ""); strings.TrimSpace(v) != "" {
			raw = v
		}
	}
	if strings.TrimSpace(raw) == "" {
		return append([]string(nil), torrent.DefaultClientFilter...)
	}
	return splitRuleList(raw)
}

// splitRuleList splits a list written one per line or comma-separated.
func splitRuleList(raw string) []string {
	var out []string
	for _, field := range strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == '\n' || r == '\r' || r == ';' }) {
		if field = strings.TrimSpace(field); field != "" {
			out = append(out, field)
		}
	}
	return out
}

// effectiveClientFilter is the list the engine applies: empty when off.
func (d *Daemon) effectiveClientFilter() []string {
	if !d.clientFilterEnabled() {
		return nil
	}
	return d.clientFilterList()
}

// applyPeerPoliciesLocked pushes the live peer policies to the session when
// they changed.
func (d *Daemon) applyPeerPoliciesLocked() {
	if d.session == nil {
		return
	}
	if share := d.uploadTradingShare(); share != d.appliedTradingShare {
		d.session.SetUploadTradingShare(share)
		d.appliedTradingShare = share
	}
	filter := strings.Join(d.effectiveClientFilter(), "\n")
	if filter != d.appliedClientFilter || !d.clientFilterApplied {
		d.session.SetClientFilter(splitRuleList(filter))
		d.appliedClientFilter = filter
		d.clientFilterApplied = true
	}
}

// peerPolicyStatus is the status line under the peer-policy settings: the
// tracker list and the country database, escaped for the page.
func (d *Daemon) peerPolicyStatus(lang string) string {
	var parts []string
	if d.trackerListURL() != "" {
		report := d.dhtReport(time.Now()).TrackerList
		switch {
		case report.Error != "":
			parts = append(parts, uiTr(lang, "Tracker list")+": "+html.EscapeString(report.Error))
		case report.FetchedAt != nil:
			parts = append(parts, uiTr(lang, "Tracker list")+": "+strconv.Itoa(report.Trackers)+" "+uiTr(lang, "trackers")+" · "+report.FetchedAt.Format("02/01 15:04"))
		default:
			parts = append(parts, uiTr(lang, "Tracker list")+": "+uiTr(lang, "waiting for the first download"))
		}
	}
	if path := d.geoIPFile(); path != "" {
		d.peerCountry("0.0.0.0")
		d.geo.mu.Lock()
		loaded, failure := d.geo.db, d.geo.err
		d.geo.mu.Unlock()
		switch {
		case failure != "":
			parts = append(parts, uiTr(lang, "Country database")+": "+html.EscapeString(failure))
		case loaded != nil:
			parts = append(parts, uiTr(lang, "Country database")+": "+strconv.Itoa(len(loaded.v4)+len(loaded.v6))+" "+uiTr(lang, "address ranges"))
		}
	}
	d.mu.Lock()
	session := d.session
	d.mu.Unlock()
	if session != nil && d.clientFilterEnabled() {
		parts = append(parts, uiTr(lang, "Clients refused")+": "+strconv.FormatInt(session.ClientFilterRefused(), 10))
	}
	return strings.Join(parts, " · ")
}
