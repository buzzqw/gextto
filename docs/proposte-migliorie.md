# Proposte di migliorie

Aggiornato: 2026-10-07. Stato: **tutte approvate e implementate** (commit su
`main` indicati per punto).

Confronto con i programmi simili per uso e tradizione: Sonarr e Radarr (serie e
film), Mylar3 e Kapowarr (fumetti), Bazarr (sottotitoli), Prowlarr (indexer).
Per ogni punto: la proposta, la decisione del proprietario e cosa è stato fatto.

| # | Proposta | Decisione | Commit |
|---|---|---|---|
| 1 | Hardlink invece della copia durante il seed | sì, con la garanzia che una cancellazione in buona fede non perda nulla | `20b28eb` |
| 2 | Aggiornamento mirato di Jellyfin e Plex | sì | `1bfaad4` |
| 3 | Soglia "upgrade until" | sì, in aggiunta alla differenza minima esistente | `95f6f97` |
| 4 | Autenticazione facoltativa | sì, spenta di default e con la rete locale esente di default | `7dae752` |
| 5 | Calendario iCal | sì, tenendo conto che in Italia le serie arrivano molto dopo la messa in onda | `e78bcc2`, `a919fcd` |
| 6 | `ComicInfo.xml` nei fumetti | sì | `6a7f739` |
| 7 | Anime con numerazione assoluta | sì | `f4a9f8f` |
| 8 | Mancanti: escludere gli episodi in download | sì | `e815180` |
| 9 | Badge dei download attivi nella barra | sì | `893bc94`, `42db4b2` |
| 9.1 | Traduzioni dei nuovi elementi in tutte le lingue | richiesta del proprietario | `698efbf` |
| 10 | Aggiornamento della documentazione | richiesta del proprietario | commit `docs:` finale |

---

## 1. Hardlink invece della copia durante il seed

- **Prima**: un file che restava in seed veniva copiato in libreria: per tutta
  la durata del seed occupava lo spazio due volte.
- **Ora** (`hardlink.go`, impostazione `hardlink_seeding`, attiva di default):
  episodi in cartella, season pack ed episodi in modalità copia entrano in
  libreria come hardlink. Il link viene creato con un nome nascosto e poi
  rinominato, quindi il file del torrent non viene mai toccato. Se il link non
  è possibile (filesystem diversi, RAM disk, condivisione senza hardlink) si
  copia come prima e il log lo dice una volta per coppia di dischi.
- **Sicurezza dei dati** (domanda del proprietario): i due nomi sono alla pari,
  non c'è un "originale" e un "collegamento" come con i link simbolici.
  Cancellare per errore il file in trasferimento lascia intatto quello in
  archivio (si interrompe solo il seed); i dati spariscono solo cancellando
  entrambi. Verificato con un test che cancella il nome nel download e rilegge
  il file in libreria.
- I film non sono coinvolti: vengono già spostati con il torrent (nessuna copia).

## 2. Aggiornamento mirato di Jellyfin e Plex

- **Ora** (`media_refresh.go`): dopo un import Jellyfin riceve
  `/Library/Media/Updated` con la cartella e Plex
  `/library/sections/{id}/refresh?path=` per la sezione che la contiene. Le
  richieste che arrivano mentre un aggiornamento è in corso vengono unite.
  Cartella sconosciuta, nessuna sezione Plex corrispondente o rifiuto del server:
  aggiornamento completo come prima.
- Mappature facoltative `jellyfin_path_mappings` / `plex_path_mappings` per i
  server in Docker. I pulsanti manuali "Aggiorna libreria" restano completi.

## 3. Soglia "upgrade until"

- **Ora** (`upgrade_until.go`, impostazione `upgrade_until_score`, 0 = spenta):
  quando la copia in libreria ha almeno quel punteggio non viene più sostituita;
  restano accettati REPACK e PROPER. Vale per ciclo, ricerca mancanti, season
  pack e film. «Perché non questa?» indica quando la release è scartata per la
  soglia. Riferimenti: 1080p WEB-DL H.264 ≈ 1280, 1080p WEB-DL H.265 DD+ ≈ 1480,
  2160p ≈ 2480.
- La differenza minima (`upgrade_min_score_diff`) resta: dice *di quanto* deve
  migliorare, la soglia *fino a dove*. Il blocco per singolo titolo resta
  «Niente upgrade», ora anche nella scheda serie della UI web.

## 4. Autenticazione facoltativa

- **Ora** (`auth.go`, scheda *Configurazione → Accesso*): spenta di default.
  Accesa: chi non è in rete locale fa il login (password bcrypt, cookie firmato
  di 30 giorni, una nuova password chiude tutte le sessioni) o usa la chiave API
  (`X-Api-Key` o `?apikey=`; la TUI la prende da `GEXTTO_API_KEY`).
- Rete locale esente di default (richiesta del proprietario). Dietro un reverse
  proxy contano gli indirizzi inoltrati (`X-Forwarded-For`, `X-Real-IP`,
  `Forwarded`), così chi arriva da Internet tramite un proxy locale deve
  autenticarsi.
- Cinque password sbagliate bloccano l'indirizzo per un minuto. Senza password
  né chiave il controllo resta aperto (con avviso nel log).
  `GEXTTO_AUTH_DISABLE=1` per rientrare se si resta chiusi fuori.

## 5. Calendario iCal

- **Ora** (`calendar_ics.go`, `GET /feed/calendar.ics`, link in Integrazioni).
  Richiesta del proprietario: in Italia le serie arrivano mesi o anni dopo la
  messa in onda, quindi il calendario distingue:
  - 📥 **arrivati in libreria** negli ultimi 30 giorni, nel giorno dell'arrivo;
  - 📺 **messa in onda originale** (TMDB) della stagione in corso, dichiarata
    come tale (✓ se già in libreria);
  - 🎬 **uscita italiana dei film** (TMDB, digitale → home video → cinema),
    con ripiego dichiarato sulla data originale.
- Anche il riquadro "Prossime uscite" della Dashboard ora dice che si tratta
  della messa in onda originale.

## 6. Fumetti: `ComicInfo.xml`

- **Ora** (`comicinfo.go`): nei CBZ scaricati direttamente (HTTP, Mega) viene
  aggiunto `ComicInfo.xml` con serie, numero e anno presi dal titolo GetComics.
  Non vengono toccati: file che lo hanno già, CBR, fumetti via torrent (sono in
  seed). Un errore non compromette mai il download.

## 7. Anime con numerazione assoluta

- **Ora** (`anime.go`, parser, casella *Anime (numerazione assoluta)* nella
  scheda serie, web e TUI): titoli come `[SubsPlease] One Piece - 1071 (1080p)`,
  `One Piece Ep 1071 SUB ITA`, `One.Piece.1071.SUB.ITA` vengono convertiti in
  stagione/episodio con le stagioni TMDB (senza TMDB: stagione 1). Anche
  `S01E1071` di alcuni indexer è letto come assoluto. La ricerca dei mancanti
  cerca anche il numero assoluto, online e nell'archivio locale.
- Le serie non marcate come anime non sono mai interessate.

## 8. Mancanti senza gli episodi già in download

- **Ora**: gli episodi con un torrent attivo (anche aggiunto a mano, da
  cartella osservata o dal telefono) e le stagioni con un season pack in corso
  non sono più mancanti, né nel ciclo né nella scheda serie. Un download in
  errore li fa tornare mancanti.

## 9. Badge dei download in corso

- **Ora**: la voce "Scarico" mostra i download in corso (torrent non finiti e
  download HTTP attivi, non quelli in seed), aggiornati ogni 5 secondi; sul
  telefono è un piccolo contatore sull'icona.

## Escluse perché già presenti o tolte di proposito

Webhook firmato HMAC, event hook, alias dei titoli, blocklist, ritardi per
serie e film (delay profile), pack di stagione, sottotitoli nel punteggio,
Discover/Popular da TMDB: già presenti. Integrazione Trakt: rimossa di
proposito (`TestTraktIntegrationRoutesRemoved`).
