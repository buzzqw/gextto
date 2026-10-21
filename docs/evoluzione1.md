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

- **amuled gestito**: fatto (`amule_managed`): configurazione privata in
  `DATA_DIR/amule`, password EC generata, porte scelte e supervisione.
- **aMule remoto**: fatto (`amule_path_mappings`): la cartella di arrivo
  riportata da un core remoto viene tradotta prima dell'import.
- **Fallback eD2k**: è una scelta esplicita dell'operatore, non un punto in
  attesa. La copertura misurata sui gap reali è **7–13 %** (`measurements.md`
  §4.10); su una libreria reale il fallback ha trovato circa **10 episodi su
  100** mancanti. Basta per tenere l'opzione, non per accenderla di default: una
  installazione nuova non deve contattare eD2k se l'operatore non lo chiede.

## 5. Perimetro in una riga

Realmente aperto: **DLNA con transcoding/frame extraction**, **WebRTC**, **il gap
con gli *arr*** (import manuale, profili nominati, `/api/v3` per Jellyseerr) e i
punti dell'engine in attesa di uno sciame vero o di hardware. Il resto del
triage è chiuso.

