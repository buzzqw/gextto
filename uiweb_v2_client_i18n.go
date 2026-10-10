package gextto

// uiweb_v2_client_i18n.go feeds the small dictionary the client script needs.
// The server-rendered HTML is translated by v2TranslateHTML, but the strings the
// browser sets at runtime (selection counter, copy feedback, folder browser,
// toasts, pull-to-refresh) never pass through it: they are looked up here and
// injected as window.__v2i18n, then read back by v2-core.js through t().

import (
	"encoding/json"
	"html/template"
	"sync"

	"github.com/buzzqw/gextto/internal/logging"
)

// v2ClientKeys are the Italian strings v2-core.js renders at runtime. They are
// the same keys used in the catalogs, so the translation is shared with the
// server-rendered UI.
var v2ClientKeys = []string{
	"Nessun risultato con questo filtro.",
	"Nessun risultato compatibile.",
	"Aggiungi",
	"Accoda questa release",
	"Accodata",
	"Copia",
	"Copia il magnet negli appunti",
	"Copiato",
	"Ricerca nell’archivio…",
	"Archivio: ",
	"Ricerca non riuscita",
	"Ricerca web non riuscita: ",
	"Caricamento…",
	"Nessuna sottocartella.",
	"Sfoglia cartelle",
	"Chiudi",
	"Su",
	"Seleziona",
	"Crea cartella",
	"Crea e usa",
	"Vai alla cartella superiore",
	"Modifica il percorso e premi Invio per navigare",
	"Usa questa cartella",
	"Crea una nuova cartella dentro quella corrente",
	"Nuova cartella",
	"Nome della nuova cartella",
	"Nome nuova cartella",
	"Nome nuova cartella:",
	"Crea la cartella e selezionala",
	"Rilevamento non supportato dal browser.",
	"Rilevamento non riuscito.",
	"Richiesta autorizzazione…",
	"Accesso ai font non autorizzato.",
	"↓ Rilascia per aggiornare",
	"selezionati · Azioni:",
	"risultati trovati",
	"ricerca RSS, indexer e web in corso…",
	"Impossibile accodare la release",
	"Impossibile leggere le cartelle",
	"Impossibile creare la cartella",
	"Richiesta avviata.",
	"Richiesta completata.",
	"Operazione non riuscita.",
	"Esegui azione",
	"Percorso corrente",
	"Inserisci un valore",
	"Seleziona un valore",
	"Tema chiaro",
	"Tema scuro",
	"Testo ",
	"Tipo di carattere",
	"Scegli un font predisposto o rileva quelli installati. La scelta vale solo in questo browser.",
	"Font dell’interfaccia",
	"Rileva font installati",
	"es. Noto Sans",
	"Applica",
	"Sistema",
	"font rilevati.",
	"⏸ Ferma scorrimento",
	"▶ Segui ultime righe",
}

// v2ClientI18nEntry memoizes the JSON dictionary of one i18n database and
// language. It is keyed by the *I18nDb so two app states in the same process
// never share it; gen detects translation edits cheaply.
type v2ClientI18nEntry struct {
	lang string
	gen  uint64
	body template.JS
}

var v2ClientI18nMemo sync.Map // *I18nDb -> v2ClientI18nEntry

// v2ClientI18nJSON renders window.__v2i18n for the active language. Unknown keys
// fall back to the Italian source, exactly like v2TranslateHTML. The result is
// cached per language and invalidated when a translation changes.
func v2ClientI18nJSON(s *AppState) template.JS {
	if s == nil || s.i18n == nil {
		return v2ClientI18nBuild(nil, nil)
	}
	lang := v2Language(s)
	gen := s.i18n.TranslationsGeneration()
	if cached, ok := v2ClientI18nMemo.Load(s.i18n); ok {
		if entry, ok := cached.(v2ClientI18nEntry); ok && entry.lang == lang && entry.gen == gen {
			return entry.body
		}
	}
	dict, eng := v2Dictionaries(s)
	body := v2ClientI18nBuild(dict, eng)
	v2ClientI18nMemo.Store(s.i18n, v2ClientI18nEntry{lang: lang, gen: gen, body: body})
	return body
}

func v2ClientI18nBuild(dict, eng map[string]string) template.JS {
	out := make(map[string]string, len(v2ClientKeys))
	for _, key := range v2ClientKeys {
		out[key] = v2TranslateText(key, dict, eng)
	}
	body, err := json.Marshal(out)
	if err != nil {
		logging.Debug("client i18n dictionary not serialized", "error", err)
		return template.JS("{}")
	}
	return template.JS(body)
}
