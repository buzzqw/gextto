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

## Confronto con Sonarr/Radarr

Non esiste un "migliore" in assoluto: sono strumenti con confini diversi. Questa
è la fotografia onesta (2026-10).

### Dove Sonarr/Radarr sono oggettivamente avanti

- **Profili di qualità e custom format.** Scoring di risoluzione, sorgente, tipo
  di release e release group, con "upgrade until"/cutoff e delay profile. Gextto
  ha una politica unica più override per-titolo: più semplice, ma meno potente.
- **Gestione indexer nativa.** Definizioni pronte per centinaia di tracker,
  priorità e categorie per indexer, limiti. Gextto si appoggia a Torznab/RSS e a
  un gestore esterno (Prowlarr/Jackett).
- **Usenet.** SABnzbd/NZBGet oltre ai torrent. Gextto è torrent, HTTP e web seed
  soltanto.
- **Maturità di import e rinomina.** Multi-episodio, specials, anime a
  numerazione assoluta, daily, import manuale con UI di matching e sostituzione.
  Gextto copre molto, ma con meno anni di casi limite assorbiti.
- **Ecosistema e API.** `/api/v3` è consumata da Overseerr/Jellyseerr e da altri
  strumenti; Gextto non la espone (non-goal esplicito). Attorno a *arr esiste un
  contorno (Bazarr per i sottotitoli, ecc.) che Gextto non copre.
- **Notifiche.** Molti provider (Discord, Slack, Pushover, Gotify, ntfy, …).
  Gextto ha Telegram, webhook ed email.
- **Import di librerie esistenti.** Flusso consolidato; Gextto lo ha solo
  parziale.
- **Community, wiki, immagini Docker, cadenza di aggiornamento.** Maturità
  difficile da replicare.

### Dove Gextto è avanti (o almeno pari)

- **Tutto in uno.** Serie, film e **fumetti** (che *arr non fanno) in un solo
  demone: niente Prowlarr + qBittorrent + Mylar/Kapowarr separati.
- **Motore torrent integrato.** Nessun client esterno: `gx-torrent` in puro Go,
  libtorrent oppure qBittorrent-nox.
- **Spiegabilità.** L'API di spiegazione del release dice perché è stato
  accettato, rifiutato o preferito.
- **Ricerca anche da motori web**, non solo dagli indexer.
- **Leggerezza.** Un binario Go e SQLite; *arr girano su .NET.
- **Semplicità.** Meno superficie di configurazione, con health, backup e
  manutenzione integrati.

### Verdetto

Sonarr/Radarr restano **migliori** dove contano la potenza di selezione (profili
e custom format), gli indexer nativi, l'usenet, i casi limite di import e
l'ecosistema/API. Gextto è **migliore** su scope unico (fumetti inclusi), motore
integrato, spiegabilità, footprint e semplicità. Presentarlo quindi come
**alternativa per installazioni nuove** — senza rivendicare parità — è la
posizione corretta, non una scusa.
