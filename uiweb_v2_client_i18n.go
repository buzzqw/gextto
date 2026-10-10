package gextto

// uiweb_v2_client_i18n.go feeds the small dictionary the client script needs.
// The server-rendered HTML is translated by v2TranslateHTML, but the strings the
// browser sets at runtime (selection counter, copy feedback, folder browser,
// toasts, pull-to-refresh) never pass through it: they are looked up here and
// injected as window.__v2i18n, then read back by v2-core.js through t().

import (
	"encoding/json"
	"html/template"

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
	"⏸ Ferma scorrimento",
	"▶ Segui ultime righe",
}

// v2ClientI18nJSON renders window.__v2i18n for the active language. Unknown keys
// fall back to the Italian source, exactly like v2TranslateHTML.
func v2ClientI18nJSON(s *AppState) template.JS {
	dict, eng := v2Dictionaries(s)
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
