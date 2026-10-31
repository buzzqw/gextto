package main

// settings_tabs_test.go guards the grouping of the standalone settings page:
// every fieldset must belong to a tab panel, the tab bar must point to panels
// that exist, and the save bar must stay inside the settings form. The page is
// rendered on the server and the tabs only hide panels, so a section left
// outside a panel would show below every tab (or disappear with it) and the
// page would silently stop saving it.

import (
	"strings"
	"testing"

	"github.com/buzzqw/gextto/internal/webui/assets"
	xhtml "golang.org/x/net/html"
)

// settingsTabOrder is the tab order the page and its keyboard navigation use.
var settingsTabOrder = []string{"folders", "network", "bandwidth", "queue", "library", "interface", "system"}

func TestSettingsTabsGroupEverySection(t *testing.T) {
	doc, err := xhtml.Parse(strings.NewReader(assets.SettingsPage()))
	if err != nil {
		t.Fatalf("parse the settings page: %v", err)
	}
	attr := func(n *xhtml.Node, key string) string {
		for _, a := range n.Attr {
			if strings.EqualFold(a.Key, key) {
				return a.Val
			}
		}
		return ""
	}

	byID := map[string]*xhtml.Node{}
	tabs := map[string]*xhtml.Node{}
	var tabOrder []string
	var panels []*xhtml.Node
	fieldsets, outside := 0, 0
	var visit func(*xhtml.Node, bool)
	visit = func(n *xhtml.Node, inPanel bool) {
		if n.Type == xhtml.ElementNode {
			if id := attr(n, "id"); id != "" {
				byID[id] = n
			}
			switch {
			case n.Data == "button" && attr(n, "role") == "tab":
				name := attr(n, "data-tab")
				tabs[name] = n
				tabOrder = append(tabOrder, name)
			case n.Data == "fieldset":
				fieldsets++
				if !inPanel {
					outside++
				}
			}
			if attr(n, "role") == "tabpanel" {
				inPanel = true
				panels = append(panels, n)
			}
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			visit(child, inPanel)
		}
	}
	visit(doc, false)

	if strings.Join(tabOrder, ",") != strings.Join(settingsTabOrder, ",") {
		t.Fatalf("tabs = %v, want %v", tabOrder, settingsTabOrder)
	}
	if fieldsets == 0 || outside != 0 {
		t.Fatalf("%d fieldset(s), %d outside a tab panel: every section must belong to a tab", fieldsets, outside)
	}

	for _, name := range settingsTabOrder {
		tab := tabs[name]
		if tab == nil {
			t.Fatalf("no tab %q", name)
		}
		if strings.TrimSpace(settingsNodeText(tab)) == "" {
			t.Errorf("tab %q has no label", name)
		}
		if attr(tab, "title") == "" {
			t.Errorf("tab %q has no tooltip", name)
		}
		controls := strings.Fields(attr(tab, "aria-controls"))
		if len(controls) == 0 {
			t.Fatalf("tab %q controls no panel", name)
		}
		for _, id := range controls {
			panel := byID[id]
			if panel == nil {
				t.Errorf("tab %q controls missing panel %q", name, id)
				continue
			}
			if attr(panel, "role") != "tabpanel" || attr(panel, "aria-labelledby") != attr(tab, "id") {
				t.Errorf("panel %q is not labelled by tab %q", id, name)
			}
			// Progressive enhancement: without JavaScript every panel is
			// visible, so the markup must not hide any of them.
			for _, a := range panel.Attr {
				if strings.EqualFold(a.Key, "hidden") {
					t.Errorf("panel %q is hidden in the markup; the script hides the inactive ones", id)
				}
			}
		}
		if name == settingsTabOrder[0] {
			if attr(tab, "aria-selected") != "true" {
				t.Errorf("the first tab must be selected")
			}
		} else if attr(tab, "aria-selected") != "false" {
			t.Errorf("tab %q must start unselected", name)
		}
	}

	// The save button belongs to the settings form: it must post every field,
	// including the ones in the panels hidden by the current tab.
	savebar := byID["settings-savebar"]
	if savebar == nil {
		t.Fatal("no settings save bar")
	}
	insideForm := false
	for parent := savebar.Parent; parent != nil; parent = parent.Parent {
		if parent.Type == xhtml.ElementNode && parent.Data == "form" && attr(parent, "id") == "settings-form" {
			insideForm = true
			break
		}
	}
	if !insideForm {
		t.Error("the save bar must be inside the settings form so Save posts hidden tabs too")
	}
}

// settingsNodeText returns the text content of a node, ignoring child elements.
func settingsNodeText(n *xhtml.Node) string {
	if n == nil {
		return ""
	}
	if n.Type == xhtml.TextNode {
		return n.Data
	}
	var b strings.Builder
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		b.WriteString(settingsNodeText(child))
	}
	return b.String()
}
