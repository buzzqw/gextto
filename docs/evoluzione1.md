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

### Chiusi in questo ciclo

- **Tabella torrent virtualizzata** oltre le 500 righe: il client tiene solo le
  righe vicine alla vista, filtri e ordinamento lavorano su tutta la sessione.
- **DLNA**: ricerca per titolo (`Search`), `X_MS_MediaReceiverRegistrar` per
  Xbox/WMP e pulsante **Cartella download** accanto alle cartelle della
  libreria.
- **Watch folders**: il testo dice che i `.torrent` trovati vengono scaricati.
- **Publisher dei DHT feed**: firma BEP 46 e put BEP 44, con chiave e sequenza
  conservate; il resolver e il client BEP 44 avevano già il resto.

### Ancora aperto

- **DLNA avanzato**: transcoding ed estrazione frame dal video. Servono un
  decoder video (ffmpeg o equivalente), fuori dal perimetro pure-Go del daemon.
- **WebTorrent/WebRTC**: wishlist dichiarata, nicchia, molto lavoro. Finché non
  c'è, il WebSocket tracker vale poco: il tracker di riferimento serve ai client
  WebSocket solo peer WebRTC.

## 3. Il gap con Sonarr/Radarr

Fatto in questo ciclo, dopo uno studio e senza copiare codice (Sonarr è GPLv3,
Gextto EUPL-1.2):

1. **Profili nominati**: Configurazione → Sorgenti → *Profili di qualità*; ogni
   combinazione di override già presente nella libreria diventa un profilo
   generato, il primo è **Default**. La semantica *arr* (qualità ordinate,
   cutoff, custom formats) resta fuori per scelta.
2. **Sottoinsieme `/api/v3` per Jellyseerr**: `system/status`,
   `qualityprofile`, `languageprofile`, `rootfolder`, `tag`, `series/lookup` +
   `POST series`, `movie/lookup` + `POST movie`, `queue`. La compatibilità
   completa non è dichiarata; la prova contro un Jellyseerr reale resta da fare.
3. **Import manuale**: pagina **Importa file**, con anteprima, correzione di
   serie/film/stagione/episodio, scelta della destinazione e sostituzione
   esplicita della copia esistente; i file multi-episodio sono registrati per
   ogni episodio.

## 4. eD2k / aMule

- **amuled gestito**: fatto (`amule_managed`): configurazione privata in
  `DATA_DIR/amule`, password EC generata, porte scelte e supervisione.
- **aMule remoto**: fatto (`amule_path_mappings`): la cartella di arrivo
  riportata da un core remoto viene tradotta prima dell'import.
- **Copertura del fallback**: se accendere `ed2k_fallback` di default dipende da
  un campione più grande di gap reali cercati via aMule — cioè aspetta dati.

## 5. Perimetro in una riga

Realmente aperto: **DLNA con transcoding/frame extraction**, **WebRTC**, la
prova dell'API v3 contro un Jellyseerr reale e i punti dell'engine in attesa di
uno sciame vero o di hardware. Il resto del triage è chiuso.

