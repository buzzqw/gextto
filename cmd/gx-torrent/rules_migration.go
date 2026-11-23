package main

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/buzzqw/gextto/internal/rss"
	"github.com/buzzqw/gextto/internal/settings"
)

// rulesMergedKey records that the one-time conversion of the old RSS rules has
// run. Its value is the number of rules that were switched off, kept until the
// operator dismisses the notice; "0" means the notice is gone.
const rulesMergedKey = "rules-merged"

// migrateLegacyRules runs once, at standalone startup. Ordered rules used to be
// a second acquisition system beside the titles of Series & movies; now they
// live in an advanced section of that page. Existing enabled rules are switched
// off so that nothing keeps downloading by a path the operator can no longer
// see in the normal flow, and the page says what happened. The rules themselves
// are kept, so they can be inspected, deleted or deliberately re-enabled.
func migrateLegacyRules(store *settings.Store) int {
	if store == nil || store.Get(rulesMergedKey, "") != "" {
		return 0
	}
	disabled := 0
	raw := strings.TrimSpace(store.Get(rulesSettingKey, ""))
	if raw != "" && raw != "[]" {
		var rules []rss.Rule
		if err := json.Unmarshal([]byte(raw), &rules); err == nil {
			changed := false
			for i := range rules {
				if !rss.RuleEnabled(rules[i]) {
					continue
				}
				off := false
				rules[i].Enabled = &off
				disabled++
				changed = true
			}
			if changed {
				if encoded, err := json.Marshal(rules); err == nil {
					store.Set(rulesSettingKey, string(encoded))
				}
			}
		} else {
			logfWarn("the saved rules could not be read: they were left untouched")
		}
	}
	store.Set(rulesMergedKey, strconv.Itoa(disabled))
	if err := store.Save(); err != nil {
		logfError("cannot save the rules migration: %v", err)
	}
	return disabled
}

// rulesNoticeCount is how many switched-off rules the page still has to report.
func (d *Daemon) rulesNoticeCount() int {
	if d.opts.Settings == nil {
		return 0
	}
	n, _ := strconv.Atoi(d.opts.Settings.Get(rulesMergedKey, "0"))
	if n < 0 {
		return 0
	}
	return n
}

// handleUIRulesNotice dismisses the one-time notice about switched-off rules.
func (d *Daemon) handleUIRulesNotice(w http.ResponseWriter, r *http.Request) {
	if d.opts.Mode != ModeStandalone || !d.uiAuthorized(w, r) || !d.uiSameOrigin(w, r) {
		http.NotFound(w, r)
		return
	}
	if d.opts.Settings != nil {
		d.opts.Settings.Set(rulesMergedKey, "0")
		if err := d.opts.Settings.Save(); err != nil {
			logfError("cannot save the rules notice: %v", err)
		}
	}
	http.Redirect(w, r, "/ui/wanted", http.StatusSeeOther)
}
