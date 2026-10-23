package gextto

// log_readability_test.go guards the rule that the log is written for the
// person running Gextto: INFO, WARN and ERROR lines are sentences, and the
// structured arguments carry values only, never developer identifiers.
//
// It reads the source, not the running log, so a new call site is checked
// before it can reach a user. DEBUG is deliberately exempt: it is the place
// for hashes, peer counters and internal state.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// logSnakeCase finds developer identifiers such as downloading_metadata or
// releases_examined. Normal words, versions and release names have no
// underscore.
var logSnakeCase = regexp.MustCompile(`\b[a-z][a-z0-9]*_[a-z0-9_]+\b`)

// logEmbeddedField finds a structured field written into the message instead
// of passed as an argument: " · released: 3".
var logEmbeddedField = regexp.MustCompile(` · [A-Za-z_][A-Za-z0-9_]*: `)

// logItalianText finds an Italian sentence in a message. Titles and release
// names arrive as arguments and keep their language; the sentence around them
// is English.
var logItalianText = regexp.MustCompile(`(?i)\b(il|una|della|delle|degli|nella|nelle|negli|sono|hanno|essere|fatto|fatta|più|anche|questo|questa|questi|queste|dalla|dagli|dalle|senza|dopo|sempre|errore|errori|impossibile|nessun|nessuna|recupero|salvataggio|scaricato|scaricata|scaricare|titolo|titoli|episodio|episodi|cartella|cartelle|percorso|percorsi|giorno|giorni|minuti|secondi|avviso|avvisi|attenzione|fallito|fallita|mancante|mancanti|trovato|trovata|letto|letta|scritto|scritta|spostato|spostata|rimosso|rimossa|chiuso|chiusa|aperto|aperta|controllo|verifica|inizio|configurazione|impostazione|valore|valori|elenco|riga|righe|nuovo|nuova|vecchio|vecchia|primo|ultimo|assente|salto|disponibile|riuscito|riuscita|provato|tentativo|attesa|pronto|pronta|caricato|caricata|salvato|salvata|eliminato|eliminata|aggiunto|aggiunta|creato|creata|perché|perche|cioè|cioe|già|gia|può|puo|deve|devono|viene|vengono)\b`)
var logItalianLetters = regexp.MustCompile(`[àèéìòùÀÈÉÌÒÙ]`)

func TestLogCallsAreReadable(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	checked := 0
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok || !isUserLogCall(call.Fun) || len(call.Args) == 0 {
				return true
			}
			checked++
			line := fset.Position(call.Pos()).Line
			if message, ok := logMessageLiteral(call.Args[0]); ok {
				if found := logSnakeCase.FindString(message); found != "" {
					t.Errorf("%s:%d: message %q contains the developer identifier %q; write it in words (details belong in DEBUG)",
						name, line, message, found)
				}
				if found := logEmbeddedField.FindString(message); found != "" {
					t.Errorf("%s:%d: message %q embeds the field %q; pass the value as a logging argument",
						name, line, message, found)
				}
				if found := logItalianText.FindString(message); found != "" {
					t.Errorf("%s:%d: message %q contains the Italian word %q; the log is always in English",
						name, line, message, found)
				}
				if found := logItalianLetters.FindString(message); found != "" {
					t.Errorf("%s:%d: message %q contains the non-English character %q",
						name, line, message, found)
				}
			}
			return true
		})
	}
	if checked < 100 {
		t.Fatalf("only %d logging calls checked: the guard is not reading the source", checked)
	}
}

// isUserLogCall reports whether fun is logging.Info, logging.Warn or
// logging.Error. Debug is the technical log and is not checked.
func isUserLogCall(fun ast.Expr) bool {
	selector, ok := fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := selector.X.(*ast.Ident)
	if !ok || pkg.Name != "logging" {
		return false
	}
	switch selector.Sel.Name {
	case "Info", "Warn", "Error":
		return true
	}
	return false
}

// logMessageLiteral returns the constant part of a log message: a string
// literal, a fmt.Sprintf format, or a concatenation of literals.
func logMessageLiteral(expr ast.Expr) (string, bool) {
	switch value := expr.(type) {
	case *ast.BasicLit:
		return logStringLiteral(value)
	case *ast.BinaryExpr:
		if value.Op != token.ADD {
			return "", false
		}
		left, leftOK := logMessageLiteral(value.X)
		right, rightOK := logMessageLiteral(value.Y)
		if leftOK && rightOK {
			return left + right, true
		}
	case *ast.CallExpr:
		selector, ok := value.Fun.(*ast.SelectorExpr)
		if !ok || len(value.Args) == 0 {
			return "", false
		}
		pkg, ok := selector.X.(*ast.Ident)
		if ok && pkg.Name == "fmt" && selector.Sel.Name == "Sprintf" {
			return logMessageLiteral(value.Args[0])
		}
	}
	return "", false
}

func logStringLiteral(expr ast.Expr) (string, bool) {
	literal, ok := expr.(*ast.BasicLit)
	if !ok || literal.Kind != token.STRING {
		return "", false
	}
	value, err := strconv.Unquote(literal.Value)
	return value, err == nil
}
