package gextto

// settings_keys_test.go keeps the settings names honest: every row of the page
// has a generated constant (settings_keys_gen.go), and every key the code reads
// as a literal is a key Gextto knows. A typo cannot compile when it goes
// through a constant, and what still goes through a literal fails here instead
// of silently returning the default.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"sort"
	"strings"
	"testing"
)

// settingLegacyKeys are read as a fallback or to migrate an older name: the
// page writes the current key, and these keep an old installation working.
var settingLegacyKeys = map[string]bool{
	"api_token":                 true,
	"content_filter":            true,
	"gap_research_hours":        true,
	"max_age_days":              true,
	"source_filters":            true,
	"title_search_in_cycle":     true,
	"torrent_stall_timeout_min": true,
}

// settingInternalKeys are bookkeeping markers Gextto writes for itself: no row
// on the page, no API writer, only Gextto reads them back.
var settingInternalKeys = map[string]bool{
	"_migrated_series":           true,
	"_score_weights_fingerprint": true,
	"gap_deep_cursor":            true,
	"last_deep_gap_fill_ts":      true,
}

// settingPanelKeys are edited by a panel that saves its own JSON through
// SaveSetting: the generic form cannot show them.
var settingPanelKeys = map[string]bool{
	"download_tags":   true,
	"event_hooks":     true,
	"tag_dir_rules":   true,
	"v3_tags":         true,
	"watched_folders": true,
}

// TestSettingsKeyConstantsMatchTheIndex fails when a row is added to (or
// removed from) the settings page without regenerating the constants.
func TestSettingsKeyConstantsMatchTheIndex(t *testing.T) {
	want := SettingsIndexKeys()
	if len(settingKeyNames) != len(want) {
		t.Fatalf("settings_keys_gen.go has %d keys, the index has %d: run `make settings-keys`",
			len(settingKeyNames), len(want))
	}
	for i, key := range want {
		if settingKeyNames[i] != key {
			t.Fatalf("constants key %d is %q, the index says %q: run `make settings-keys`",
				i, settingKeyNames[i], key)
		}
	}
}

// TestSettingsLiteralsAreKnownKeys scans the package for a settings key read
// or saved through a literal and fails on one Gextto does not know: a typo
// would otherwise return the default without a word.
func TestSettingsLiteralsAreKnownKeys(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	unknown := make(map[string]string)
	fset := token.NewFileSet()
	check := func(key, file string) {
		if settingKeyKnown(key) {
			return
		}
		if _, seen := unknown[key]; !seen {
			unknown[key] = file
		}
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		if name == "settings_keys_gen.go" {
			continue
		}
		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			switch node := n.(type) {
			case *ast.IndexExpr:
				sel, ok := node.X.(*ast.SelectorExpr)
				if !ok || sel.Sel.Name != "Settings" {
					return true
				}
				if lit, ok := node.Index.(*ast.BasicLit); ok && lit.Kind == token.STRING {
					check(strings.Trim(lit.Value, `"`), name)
				}
			case *ast.CallExpr:
				// SaveSetting(dataDir, "key", value) writes without the
				// allowlist; the panel keys still have to be real.
				if fun, ok := node.Fun.(*ast.Ident); !ok || fun.Name != "SaveSetting" || len(node.Args) < 2 {
					return true
				}
				if lit, ok := node.Args[1].(*ast.BasicLit); ok && lit.Kind == token.STRING {
					check(strings.Trim(lit.Value, `"`), name)
				}
			}
			return true
		})
	}
	if len(unknown) == 0 {
		return
	}
	keys := make([]string, 0, len(unknown))
	for key := range unknown {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		t.Errorf("%s reads settings key %q, which is not in the settings index, the API-only list, a writable prefix or the internal lists", unknown[key], key)
	}
}

// settingReadHelpers take the key as their second argument.
var settingReadHelpers = map[string]bool{
	"settingsOr": true, "settingsBool": true, "settingsNonEmpty": true, "settingOrDefault": true,
	"settingsParseUint": true, "settingsParseInt": true, "settingListContains": true,
	"settingsText": true, "settingsOrNil": true, "settingsBoolString": true,
}

// TestIndexedSettingsAreReadThroughConstants fails on a literal key that has a
// generated constant: a key on the settings page is read by its constant, so a
// typo stops the build instead of returning the default.
func TestIndexedSettingsAreReadThroughConstants(t *testing.T) {
	fset := token.NewFileSet()
	gen, err := parser.ParseFile(fset, "settings_keys_gen.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	constFor := map[string]string{}
	ast.Inspect(gen, func(n ast.Node) bool {
		if spec, ok := n.(*ast.ValueSpec); ok && len(spec.Names) == 1 && len(spec.Values) == 1 {
			if lit, ok := spec.Values[0].(*ast.BasicLit); ok && lit.Kind == token.STRING {
				constFor[strings.Trim(lit.Value, `"`)] = spec.Names[0].Name
			}
		}
		return true
	})
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") || name == "settings_keys_gen.go" {
			continue
		}
		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		report := func(node ast.Node, lit *ast.BasicLit) {
			key := strings.Trim(lit.Value, `"`)
			if constant, ok := constFor[key]; ok {
				t.Errorf("%s: settings key %q read as a literal; use %s", fset.Position(node.Pos()), key, constant)
			}
		}
		ast.Inspect(file, func(n ast.Node) bool {
			switch node := n.(type) {
			case *ast.IndexExpr:
				if sel, ok := node.X.(*ast.SelectorExpr); ok && sel.Sel.Name == "Settings" {
					if lit, ok := node.Index.(*ast.BasicLit); ok && lit.Kind == token.STRING {
						report(node, lit)
					}
				}
			case *ast.CallExpr:
				if fun, ok := node.Fun.(*ast.Ident); ok && settingReadHelpers[fun.Name] && len(node.Args) >= 2 {
					if lit, ok := node.Args[1].(*ast.BasicLit); ok && lit.Kind == token.STRING {
						report(node, lit)
					}
				}
			}
			return true
		})
	}
}

// settingKeyKnown is the whole surface: the page, the writable keys and the
// keys that only Gextto itself reads.
func settingKeyKnown(key string) bool {
	if settingKeyAllowed(key) || settingNeverWritable[key] {
		return true
	}
	return settingLegacyKeys[key] || settingInternalKeys[key] || settingPanelKeys[key]
}

// Every literal that goes through a constant is covered by the compiler: this
// guards the other direction, a constant that no longer exists in the index.
func TestSettingsConstantsPointAtRealKeys(t *testing.T) {
	indexed := make(map[string]bool, len(settingKeyNames))
	for _, key := range settingKeyNames {
		indexed[key] = true
	}
	for _, key := range settingKeyNames {
		if !uiSettingIndexed[key] {
			t.Fatalf("generated key %q is not on the settings page: run `make settings-keys`", key)
		}
	}
}
