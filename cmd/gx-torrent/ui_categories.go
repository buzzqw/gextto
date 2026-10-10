package main

// ui_categories.go is the standalone page's category/tag editor. In managed
// mode the fields are not rendered and the handler refuses: Gextto owns the
// library layout.

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
)

func (d *Daemon) handleUICategory(w http.ResponseWriter, r *http.Request) {
	if !d.uiAuthorized(w, r) || !d.uiSameOrigin(w, r) {
		return
	}
	hash := strings.ToLower(strings.TrimSpace(r.FormValue("hash")))
	if d.opts.Mode != ModeStandalone {
		d.uiRedirect(w, r, nil, "", errors.New("categories are a standalone feature"))
		return
	}
	category := strings.TrimSpace(r.FormValue("category"))
	tags := qbitList(r.FormValue("tags"))
	if category != "" {
		// Auto-create the category so typing a new name in the page works.
		d.createCategory(category, d.categorySavePath(category))
	}
	err := d.setCategory(hash, category)
	if err == nil {
		err = d.setTags(hash, tags)
	}
	d.uiRedirect(w, r, url.Values{"open": {hash}, "tab": {"general"}}, "Category and tags updated", err)
}
