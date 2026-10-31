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

// handleUICategoryManage is the settings page's category editor: add, change
// the save path of, or remove a category. The qBittorrent API has the same
// operations; the page needs them too, because automatic torrent management
// (autoTMM) is useless when the category has no path to move to.
func (d *Daemon) handleUICategoryManage(w http.ResponseWriter, r *http.Request) {
	if d.opts.Mode != ModeStandalone {
		http.NotFound(w, r)
		return
	}
	if !d.uiAuthorized(w, r) || !d.uiSameOrigin(w, r) {
		return
	}
	_ = r.ParseForm()
	op := strings.TrimSpace(r.FormValue("op"))
	name := strings.TrimSpace(r.FormValue("name"))
	savePath := strings.TrimSpace(r.FormValue("savepath"))
	var err error
	switch op {
	case "add", "edit":
		if name == "" {
			err = errors.New("the category name is required")
			break
		}
		if op == "add" {
			d.createCategory(name, savePath)
		} else {
			d.editCategory(name, savePath)
		}
	case "remove":
		if name == "" {
			err = errors.New("the category name is required")
			break
		}
		d.removeCategories([]string{name})
	default:
		err = errors.New("unknown category action")
	}
	query := url.Values{}
	if err != nil {
		query.Set("ok", "0")
		query.Set("msg", "Error: "+err.Error())
	} else {
		query.Set("ok", "1")
		query.Set("msg", "Categories saved")
	}
	http.Redirect(w, r, uiSettingsPath+"?"+query.Encode()+"#categories", http.StatusSeeOther)
}

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
	tags := splitFormList(r.FormValue("tags"))
	if category != "" {
		// Auto-create the category so typing a new name in the page works.
		d.createCategory(category, d.categorySavePath(category))
	}
	err := d.setCategory(hash, category)
	if err == nil {
		err = d.setAutoManagement(hash, formBool(r, "auto_tmm"))
	}
	if err == nil {
		err = d.setTags(hash, tags)
	}
	d.uiRedirect(w, r, url.Values{"open": {hash}, "tab": {"general"}}, "Category and tags updated", err)
}
