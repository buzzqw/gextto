// Package assets bundles gx-torrent's web UI files into the binary: the page
// template, the token prompt page and the site icon. Moving them out of Go
// string literals keeps ui.go about behaviour, not markup.
//
// The files are embedded at build time (embed.FS) and read lazily, on first
// use: a managed daemon that never serves the page pays nothing for them beyond
// the bytes on disk, which Linux loads only when they are accessed.
package assets

import "embed"

//go:embed ui.tmpl token.html favicon.svg
var files embed.FS

// UITemplate is the html/template source of the daemon page: the page body plus
// the "addopts", "fragments", "cards", "live" and "detail" definitions.
func UITemplate() string { return mustRead("ui.tmpl") }

// TokenPage is the standalone page that asks for the configured token.
func TokenPage() string { return mustRead("token.html") }

// FaviconSVG is the site icon, inlined in the pages and served at /favicon.ico.
func FaviconSVG() string { return mustRead("favicon.svg") }

func mustRead(name string) string {
	data, err := files.ReadFile(name)
	if err != nil {
		// The files are embedded at build time: this can only fail if the
		// embed directive and the file names drift apart, which is a build
		// error, not a runtime condition.
		panic("webui/assets: " + err.Error())
	}
	return string(data)
}
