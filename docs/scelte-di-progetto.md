# Scelte di progetto

Decisioni di prodotto e vincoli tecnici che spiegano **perché** il codice è
fatto così. Estratto l'8-10-2026 dal vecchio piano di adozione
`SONARR_RADARR_REPLACEMENT_PLAN.md` (rimosso): qui restano solo le parti durevoli
e verificabili. Gli item ancora aperti sono nel backlog (`docs/da-fare.md`).

## Posizionamento

- Gextto è pensato per **installazioni nuove**, non come sostituto "drop-in" di
  Sonarr/Radarr: niente import dei loro database, niente migrazione in place,
  niente rollback.
- L'integrazione è **verso** i gestori di indexer (Jackett/Prowlarr), non
  duplicandoli dentro Gextto.
- **Non-goal** espliciti: compatibilità API `*arr` / `/api/v3`; quality profile e
  custom format; autenticazione multi-utente (l'esposizione remota passa dal
  reverse proxy documentato in `docs/SECURITY.md`). Le differenze si pubblicano,
  non si nascondono.
- Target: chi ha un server e un media server (Jellyfin/Plex) e vuole fare "trova,
  scarica, rinomina, archivia" con un solo demone; molto probabilmente ha **già**
  Jackett o Prowlarr per gestire gli indexer.

## Gestori di indexer (Jackett/Prowlarr)

Decisione: Gextto tratta il manager come **un'unica sorgente aggregata** e non
importa la sua lista di indexer. Il manager è l'unica fonte di verità per
indexer, categorie, credenziali, FlareSolverr e rate limit.

Misure (stessa libreria, stesso host):

| Scenario | Latenza | Risultati |
|---|---|---|
| Aggregato con un indexer rotto abilitato | 60 s (timeout) | 0 |
| Indexer sano chiamato direttamente | 0,25–2,4 s | 40–100 |
| Aggregato dopo aver disabilitato il rotto nel manager | 2–4 s | 156–240 |

Conseguenze (tutte implementate):

- un prototipo che duplicava la lista indexer è stato costruito, misurato e
  **revertito**: nessun guadagno di stabilità, solo deriva di configurazione;
- le **categorie** sono state rimosse: un aggregato serve sia serie sia film,
  quindi una categoria statica per indexer è inutile o dannosa (una query TV con
  la categoria "film" restituiva zero risultati);
- il tipo di manager si può **dichiarare** (`manager` in `config.go` e nella UI)
  invece di dedurlo da URL/porta/nome, che resta come fallback;
- la salute del manager (Prowlarr `indexerstatus`) è mostrata in Sources, così un
  indexer rotto o disabilitato è visibile invece di produrre zero risultati in
  silenzio (`indexer_health.go`);
- un **timeout per sorgente** limita una singola richiesta sotto il budget della
  ricerca, così una sorgente lenta non consuma l'intera ricerca;
- l'API di ricerca di Prowlarr **ignora** stagione/episodio ed external-id
  (misurato): non vengono inoltrati; anche `type` non ha effetto osservato;
- la lista indexer di **Jackett non è raggiungibile** con la chiave Torznab (la
  sua API di gestione risponde con un redirect al login): irrilevante, serve solo
  l'aggregato.

## FlareSolverr

- **Sessioni persistenti** per dominio, con TTL breve e distruzione, invece di un
  browser per richiesta; se le sessioni non sono supportate si ricade in modalità
  stateless.
- **Mai per i gestori di indexer**: Jackett/Prowlarr gestiscono Cloudflare al
  loro interno, quindi i loro endpoint non passano da FlareSolverr; un 403 o un
  errore di rete lì viene riportato com'è invece di sprecare 20–30 s mascherando
  la causa reale.
- Si rispetta `solution.status`: una pagina "risolta ma in errore" non viene
  interpretata come contenuto.

## Politica di selezione

Gextto **non** implementa profili di qualità, custom format o un motore
regex/score come `*arr`. Sceglie invece:

- **default globali** per risoluzione, sorgente, codec, audio, HDR, lingua e
  vincoli di dimensione;
- **override ed esclusioni per-titolo** (es. 1080p solo per una serie, 4K per un
  film);
- **spiegabilità**: l'API di spiegazione del release dice perché un release è
  stato accettato, rifiutato o preferito.

Motivo: i profili sono la parte più complessa — e più configurata male — del
modello `*arr`. Una politica unica ben spiegata più il controllo per-titolo copre
gli stessi bisogni con molta meno superficie.
