# Cosa è davvero aperto

Triage di lavoro su [`evoluzione.md`](evoluzione.md) §6 e §8, con i numeri di
[`measurements.md`](measurements.md) §4. Non è una seconda fonte di verità: lo
stato e i gate restano in `evoluzione.md`, i dati in `measurements.md`. Questo
documento serve solo a separare, tra le voci elencate là, quelle **aperte** da
quelle **in attesa di un dato**, **in attesa di un host** e **chiuse**.

Data del triage: 2026-10-21.

## 1. In attesa di un host o di hardware (engine)

Non sono «da fare»: sono implementati o quasi, e aspettano la prova.

| Voce | Cosa manca |
|---|---|
| **Choking su sciame reale** | un host con **porta in ingresso pubblica** (qui siamo dietro NAT, l'harness di seeding trova 0 peer). È l'unica cosa che può ancora giustificare modifiche a `internal/unchoker` |
| **Pipelining oltre ~162 MB/s** | sciame multi-peer su link veloce: il target 10 Gbps non è misurabile su questa macchina |
| **uTP: ACK uno per pacchetto / I/O batch** | uno `strace -c` su sessione **solo uTP sopra 8 MB/s**; oggi uTP è <1 % del tempo syscall, il TCP porta i byte |
| **Priorità file 0–7 con file misti** | misura diretta (finora solo indiretta) |
| **Richieste parallele per web seed** | un torrent servito quasi solo da web seed dove la versione parallela vince (~2,5× misurato con `curl` su archive.org) |

Tutti e cinque hanno un gate scritto: nessuno si fa «perché sarebbe meglio».

## 2. Lavoro di feature senza gate esterno

### Superficie prodotto (tre ritocchi piccoli)

- **Tabella torrent virtualizzata** oltre la finestra di 300 righe con *Show
  more*: serve solo a sessioni con più di mille torrent.
- **DLNA, cartelle libreria:** il campo dovrebbe proporre almeno la *Download
  folder* configurata, invece di farsi digitare un path.
- **Watch folders:** la sezione deve dire che i `.torrent` trovati vengono
  **scaricati**, non solo «aggiunti automaticamente».

### Engine / DLNA

- **DLNA avanzato**: transcoding, estrazione frame dal video (senza ffmpeg),
  ricerca, compatibilità Xbox/WMP (`X_MS_MediaReceiverRegistrar`). Browsing,
  Range, sottotitoli, cartelle e artwork ci sono già.
- **Publisher dei DHT feed**: oggi esiste solo il resolver; manca la firma di
  un feed (BEP 46 publisher), e il client BEP 44 in uscita non ha ancora un
  consumatore.
- **WebTorrent/WebRTC**: wishlist dichiarata, nicchia, molto lavoro. Finché non
  c'è, il WebSocket tracker vale poco: il tracker di riferimento serve ai client
  WebSocket solo peer WebRTC.

## 3. Il gap con Sonarr/Radarr

I tre punti in cui `evoluzione.md` §1 ammette che gli *arr* sono migliori,
elencati per valore/sforzo, ciascuno dopo uno studio e senza copiare codice
(Sonarr è GPLv3, Gextto EUPL-1.2).

1. **Profili nominati sul modello Gextto**: la policy globale diventa
   «Default», ogni combinazione di override diventa un profilo generato, con
   migrazione automatica dai dati per-titolo (`series.quality/language/subtitle/
   exclude`, `movies_config.*`). Riprodurre la semantica *arr* (qualità
   ordinate, cutoff, custom formats) costa molto di più: i custom formats sono
   valutati separatamente.
2. **Sottoinsieme `/api/v3` per Jellyseerr** (`system/status`,
   `qualityprofile`, `languageprofile`, `rootfolder`, `tag`, `series/lookup` +
   `POST series`, `movie/lookup` + `POST movie`, `queue`) — **dopo** profili
   nominati, root folder e tag, e testato contro un Jellyseerr reale. La
   compatibilità completa è dichiarata irrealistica.
3. **Import manuale**: abbinare un file esistente, scegliere cosa sostituisce e
   dove va. Multi-episodio, speciali e nomi daily sono già parsati e
   registrati; manca l'operazione.

È l'unico blocco «aperto» che sposta davvero l'ago del prodotto.

## 4. eD2k / aMule

- **amuled gestito**: avviarlo e supervisionarlo come qBittorrent-nox
  (config-dir, password EC generata, porte) invece di pretendere che
  l'operatore lo configuri a mano. Gate: utenti che vogliono eD2k senza setup
  manuale.
- **aMule remoto**: l'import dei completati assume lo stesso path; servirebbe un
  path mapping tipo `qbittorrent_path_mappings`.
- **Copertura del fallback**: se accendere `ed2k_fallback` di default dipende da
  un campione più grande di gap reali cercati via aMule — cioè aspetta dati.

## 5. Cosa non è aperto, anche se sembra

- **Chiusi con i dati** (`evoluzione.md` §8 ultimo paragrafo e
  `measurements.md` §4.10): DHT esterno al posto di `internal/dht`, enforcing
  del cap anti-Sybil, multi-path `get_peers`, ordinamento dei candidati del
  merge, `O_DIRECT`/`io_uring`, write-back cache (`write_cache=0`), file pool,
  benchmark UI in CI, coalescing delle scritture contigue, indici SQLite
  inutilizzati. Non si ritentano senza dati nuovi.
- **Fuori scope per scelta** (`evoluzione.md` §5): `/api/v3` completo, quality
  profile e custom formats, autenticazione multi-utente, usenet, Trakt, layout
  nel dialog di add di Gextto, cache cap su NFS.
- **Fuori scope per protocollo** ([`bep.md`](bep.md)): BEP
  8/17/18/22/26/28/30/34/35/45/50/51, I2P nativo.
- **Non è un gap, è una verifica**: l'accessibilità passa i 14 test axe, ma la
  prova manuale con screen reader è ancora dovuta — e il documento lo dichiara.

## 6. Perimetro in una riga

Realmente aperto: **tre ritocchi di UI** (tabella virtualizzata, cartella DLNA,
testo watch folders), **tre pezzi di feature** (DLNA avanzato, publisher dei
DHT feed, WebRTC), **il gap con gli *arr*** (import manuale, profili nominati,
`/api/v3` per Jellyseerr) e **la gestione di amuled**. Tutto il resto
dell'engine è in attesa di uno sciame vero o di hardware, non di codice.
