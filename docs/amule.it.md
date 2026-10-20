# eD2k e Kad con aMule

Gextto raggiunge le reti **eD2k** e **Kad** tramite **aMule**. `amuled` fa il
lavoro di rete — server, Kad, fonti, code, crediti, trasferimenti — e Gextto lo
comanda con il protocollo delle **connessioni esterne (EC)**, lo stesso di
`amulecmd`, `amuleweb` e della GUI remota: cerca, manda i link `ed2k://`,
elenca e controlla i download in **Scarico**, mostra la pagina **eD2k** e
importa in libreria i file completati.

L'integrazione è **facoltativa e spenta di default** (`ed2k_enabled`). Gextto
non include un client eD2k: aMule lo installi, lo configuri e lo avvii tu. Con
aMule spento o non raggiungibile le funzioni eD2k sono semplicemente
indisponibili e i torrent non cambiano.

- [Requisiti](#requisiti)
- [Installare aMule](#installare-amule)
- [Attivarlo in Gextto](#attivarlo-in-gextto)
- [Come si usa](#come-si-usa)
- [Problemi comuni](#problemi-comuni)
- [Riferimento](#riferimento)

## Requisiti

- **aMule con `amuled`**, in esecuzione e raggiungibile da Gextto: sulla stessa
  macchina (il caso normale) o su un'altra macchina della rete.
- **Connessioni esterne abilitate** in aMule, con una password (vedi sotto).
- **La cartella di arrivo visibile allo stesso percorso** sulla macchina dove
  gira Gextto, se i file completati devono essere importati in libreria. Quando
  i due girano sulla stessa macchina è automatico.
- **Le porte TCP e UDP di aMule aperte** sul router per un ID alto e un Kad non
  dietro firewall, cioè più fonti. Con le porte predefinite sono TCP 4662 e
  UDP 4672 (Kad e eD2k condividono la porta UDP).

## Installare aMule

Installa aMule dalla tua distribuzione. Su Debian e Ubuntu il pacchetto è
`amule-daemon`:

```bash
sudo apt install amule-daemon
```

Sulle altre distribuzioni installa il pacchetto di aMule che contiene `amuled`.
Avvialo come lo fornisce la distribuzione (di solito c'è un servizio), oppure
lancia `amuled` tu, come l'utente che possiede la cartella di configurazione di
aMule. Se non usi un `--config-dir` diverso, quella cartella è `~/.aMule` e il
file da modificare è `~/.aMule/amule.conf`.

> [!NOTE]
> aMule riscrive `amule.conf` quando esce. Modifica il file con `amuled`
> fermo, poi riavvialo, altrimenti le modifiche si perdono.

### Abilitare le connessioni esterne

In `amule.conf`:

```ini
[ExternalConnect]
AcceptExternalConnections=1
ECAddress=127.0.0.1
ECPort=4712
ECPassword=<md5 della password>
```

- `ECPassword` è il **digest MD5 esadecimale** della password, non la password:
  `printf '%s' 'segreta' | md5sum`. In Gextto la password si inserisce **in
  chiaro** (`amule_password`) ed è il client a calcolarne l'hash al login.
- `ECAddress` è l'indirizzo su cui aMule si mette in ascolto: `127.0.0.1`
  accetta connessioni solo dalla stessa macchina. Se Gextto gira su un altro
  host, usa l'interfaccia (o `0.0.0.0`) e limita la porta 4712 col firewall.
- `ECPort` è la porta a cui si collega Gextto (4712 di default); deve coincidere
  con **aMule — porta connessioni esterne (EC)** nelle impostazioni di Gextto.

### Docker

Con l'immagine `ngosang/amule` (o qualunque container con aMule):

- monta la condivisione dei download **allo stesso percorso** dentro il
  container e imposta `INCOMING_DIR` e `TEMP_DIR` su quei percorsi, così la
  cartella che aMule riporta è valida anche sull'host;
- usa una **cartella di arrivo dedicata**, perché aMule condivide tutto quello
  che contiene;
- per far aprire ad aMule le porte sul router con l'UPnP, avvia il container con
  `network_mode: host` e imposta `UPnPEnabled=1` in `amule.conf`. In modalità
  bridge aMule chiederebbe al router di inoltrare le porte all'indirizzo del
  container, e non funziona;
- pubblica la porta EC (4712) e imposta `ECAddress=0.0.0.0` se Gextto è fuori
  dal container.

### Su un disco di rete

Imposta `AllocateFullFile=0` in `amule.conf`: preallocare un file parziale di
diversi GB su un NAS può bloccare `amuled` (e le sue risposte EC) per decine di
secondi. Gextto regge un aMule lento — un'aggiunta che va in timeout, o che
aMule rifiuta perché il file è già in elenco, viene verificata nella coda dei
download e conta come fatta se il file c'è — ma disattivare la preallocazione
evita del tutto l'attesa.

## Attivarlo in Gextto

**Configurazione → Fonti → eD2k e Kad**:

1. accendi **eD2k e Kad tramite aMule**;
2. inserisci **indirizzo** (di solito `127.0.0.1`), **porta EC** (4712) e la
   **password in chiaro**;
3. salva, poi apri la pagina **eD2k** (**Scoperta → eD2k**) e premi
   **Connetti** per entrare in eD2k e avviare Kad.

La pagina **Sorgenti** mostra la spia di eD2k e Kad: **aperta** (connesso a un
server con ID alto), **limitata (firewall/NAT)** (ID basso, o solo Kad), **non
connessa**, **aMule non raggiungibile** oppure **stato sconosciuto** (non ancora
letto). Se la spia dice *limitata*, apri sul router le porte TCP e UDP di aMule.

Quando aMule non risponde, la pagina eD2k lo dice, spiega cosa controllare e
offre **Riprova**. Nient'altro in Gextto smette di funzionare.

Quando `amuled` gira sulla macchina di Gextto, la barra in alto mostra il badge
**aMule** con CPU e RAM del demone, aggiornato insieme agli altri valori. Con
aMule su un altro host il badge non c'è: quei dati non sono disponibili tramite
il protocollo EC.

## Come si usa

La pagina eD2k ha tre schede: **aMule e ricerca** (stato, server, ricerca, link
incollati), **File condivisi** e **Log di aMule**.

### Ricerca

Tre punti, tutti attraverso aMule:

- **Ricerca manuale** (la casella di ricerca di **Scarico** e la pagina di
  ricerca): spunta **eD2k** per aggiungere ai torrent i risultati eD2k/Kad.
- **Pagina eD2k → Cerca su eD2k e Kad**: scegli la rete e cerca; aMule
  raccoglie i risultati per qualche secondo.
- **Archivio**: spunta **Cerca anche su eD2k** per avere un blocco di risultati
  eD2k sotto le release archiviate.

La rete è quella scelta in **Rete delle ricerche eD2k**: **Kad** (default: rete
distribuita, non serve un server), **Kad + server eD2k** (entrambe le ricerche,
risultati uniti per hash), **Server eD2k (globale)** (tutti i server della lista
di aMule) o **Server eD2k (connesso)** (solo quello a cui aMule è connesso). Il
ripiego automatico cerca sempre Kad e i server insieme.

Ogni risultato mostra nome, dimensione, hash MD4 e fonti (in trasferimento /
totali), viene interpretato come un nome di torrent (serie, episodio, qualità) e
ha **Scarica**; **già noto** segnala un file che aMule ha già. La pagina eD2k
elenca anche i server di aMule con utenti, file e ping, e da lì puoi scaricare un
nuovo `server.met` o un `nodes.dat` per Kad da un indirizzo.

### Incollare link eD2k

Nella **pagina eD2k** o nella riga di ricerca di **Scarico** (il campo sotto la
casella di ricerca, **Apri link eD2k**), incolla uno o più link `ed2k://`, anche
mescolati a testo: Gextto li verifica, li mostra come risultati e **Scarica con
aMule** li mette tutti in coda. Un link a un file che non è un video (un pdf, un
programma, un archivio) è solo un download di aMule e resta nella cartella di
arrivo, come un torrent aggiunto a mano.

### Download e completamenti

La coda di aMule compare in **Scarico** accanto ai torrent e ai download HTTP,
con progressione, velocità, fonti (in trasferimento / totali), pausa / ripresa /
rimozione e il badge **eD2k**. ⓘ apre il dettaglio: stato, fonti, ultimo dato
ricevuto, hash, il link `ed2k://` da copiare e la **priorità in aMule** (bassa,
normale, alta, automatica). I download contano nella dashboard e nel contatore
del menu, compaiono nell'elenco della TUI e il loro traffico è disegnato nel
grafico della dashboard come linee tratteggiate, mentre la barra in alto somma
torrent e aMule.

Quando aMule completa un video, Gextto solleva lo stesso evento di
completamento di un torrent, così rinomina, import in libreria, controllo con
MediaInfo, notifica e aggiornamento dei media server sono condivisi. Il
controllo di qualità gira prima che qualcosa venga scritto in libreria, film
compresi: se c'è già un file uguale o migliore il download viene scartato
(spostato nel Cestino, o eliminato secondo l'azione di pulizia) e mai copiato.

aMule 2.3 tiene il file finito nella coda dei download; aMule 3.x lo toglie
subito, quindi Gextto lo ritrova tra i file condivisi, anche dopo un riavvio. Gli
episodi eD2k entrano in libreria sempre con **hardlink o copia** (mai spostati),
così aMule continua a condividere l'originale dalla cartella di arrivo;
l'hardlink non occupa spazio in più quando le due cartelle sono sullo stesso
filesystem.

Le notifiche usano gli stessi eventi dei torrent: **Download avviato** quando è
Gextto a scegliere un risultato (compreso il ripiego automatico eD2k),
**Download completato** con titolo, percorso in libreria, dimensione, durata e
velocità media (e la release sostituita quando ne ha rimpiazzata una peggiore),
**Download fallito** quando un download eD2k automatico si ferma per i giorni
configurati, **Errore torrent** quando l'import fallisce. Un file scartato
perché inferiore produce solo una riga di log, e un file che non è un video non
viene registrato come release: se aMule lo tiene in coda arriva la semplice
notifica "manuale" di completamento, con aMule 3.x (che lo toglie subito)
nessuna notifica.

### Banda

aMule segue i limiti di velocità di Gextto (regola, eccezione, programmazione,
Auto): vengono inviati quando cambiano e rimandati ogni 10 minuti. La pagina
eD2k mostra i limiti che aMule ha in quel momento.

### Ripiego automatico

Secondo interruttore, separato (**Usa eD2k quando i torrent non trovano
nulla**, spento di default): nel passaggio approfondito del gap filling, solo
quando le ricerche torrent di episodio e stagione non hanno prodotto nulla,
Gextto cerca Kad e i server eD2k (ricerca globale) con il nome configurato e
tutti gli alias, e avvia un risultato solo con l'episodio esatto, una qualità
riconosciuta, un file video e almeno le fonti minime impostate in **Fonti minime
per il fallback**.

### Download che si fermano

Un download eD2k avviato da Gextto stesso che non riceve dati per tutti i giorni
indicati in **Rinuncia a un download eD2k fermo dopo** (7, 0 = mai, misurati
sull'ora di "ultimo dato ricevuto" di aMule, quindi i riavvii non la azzerano)
viene annullato in aMule, la release va in blocklist e il buco torna da cercare.
I download aggiunti a mano e quelli in pausa restano.

### File condivisi e caricamenti

La scheda **File condivisi** elenca quello che aMule
condivide con i byte caricati nella sessione e in totale, le richieste
soddisfatte, i client in coda per ogni file e la priorità di upload, più il link
`ed2k://` da copiare. Sotto, **Chi sta scaricando da te** elenca i client nella
coda di upload con software, file, velocità e posizione in coda. Il pannello
mostra anche la velocità di upload e la lunghezza della coda, e le cartelle
condivise in sola lettura: quella di arrivo più le eventuali altre configurate
in aMule.

**Ricarica file condivisi** chiede ad aMule di rileggere le cartelle, come il
pulsante di ricarica della sua interfaccia: i file aggiunti o tolti a mano
compaiono o spariscono. Gextto non modifica mai la configurazione di
condivisione di aMule.

La scheda **Log di aMule** mostra le ultime righe del log di `amuled`, con
**Aggiorna** per rileggerle.

## Problemi comuni

| Sintomo | Cosa controllare |
|---|---|
| **aMule non raggiungibile** sulla pagina eD2k o sulla spia in Sorgenti | `amuled` è in esecuzione; `AcceptExternalConnections=1`; `ECPort` e indirizzo coincidono con le impostazioni di Gextto; la porta 4712 è aperta nel firewall se aMule è su un'altra macchina; `ECAddress` non è `127.0.0.1` quando Gextto si collega da altrove. |
| **Password sbagliata** (la pagina riporta `authentication failed`) | In Gextto va la password **in chiaro**; `ECPassword` in `amule.conf` è il suo MD5. Dopo averla cambiata, riavvia `amuled`. |
| **ID basso** (badge nella pagina eD2k) | La porta TCP di aMule (4662 di default) non è raggiungibile da fuori: aprila sul router, oppure abilita l'UPnP in aMule. Meno fonti, code più lente. |
| **Kad dietro firewall** | La porta UDP di aMule (4672 di default) non è raggiungibile: aprila sul router. Kad funziona ma trova meno fonti. |
| **La ricerca non trova nulla** | la rete scelta (per Kad basta che Kad sia connesso); una durata di ricerca troppo breve; un Kad dietro firewall. Kad e i server indicizzano file diversi, quindi prova **Kad + server eD2k**. |
| **Il download resta in coda** | è normale su eD2k: aMule aspetta il proprio turno nelle code delle fonti. Controlla le fonti e l'ID; con l'ID basso le code sono lunghe. |
| **I file si completano in aMule ma non arrivano in libreria** | la cartella di arrivo non è visibile allo stesso percorso da Gextto, o non è leggibile; il file non è un video (resta nella cartella di arrivo per scelta); aMule stava ancora preallocando (`AllocateFullFile=0` sui dischi di rete). |
| **La pagina eD2k dice che l'integrazione è spenta** | l'interruttore è spento, o le impostazioni non sono state salvate. La pagina ha il link diretto all'impostazione. |

## Riferimento

### Impostazioni

| Chiave | Significato |
|---|---|
| `ed2k_enabled` | interruttore principale (spento) |
| `amule_host` | host di `amuled` (`127.0.0.1`) |
| `amule_port` | porta EC (`4712`) |
| `amule_password` | password EC, in chiaro (segreta) |
| `ed2k_search_network` | `kad` (default), `both` (Kad + server eD2k), `global` o `local` |
| `ed2k_search_timeout_secs` | per quanti secondi aMule raccoglie i risultati (5–120 s, default 20) |
| `ed2k_fallback` | ripiego automatico nel gap filling (spento) |
| `ed2k_min_sources` | fonti minime del ripiego (3) |
| `ed2k_stall_days` | rinuncia a un download automatico fermo da tanti giorni (7, 0 = mai) |

Le impostazioni delle integrazioni precedenti (le opzioni `amule_*` di servizio
di extto, `ed2k_url`, `ed2k_token`, `ed2k_upnp`, … di gx-ed2k) vengono cancellate
al caricamento; `amule_host`, `amule_port` e `amule_password` restano. Le API
sono documentate in [`API.md`](API.md) (`/api/ed2k/status`,
`/api/ed2k/downloads`, `/api/ed2k/limits`, `/api/ed2k/log`).

### Il client EC (`internal/amule`)

- Framing: `flags:uint32 BE | length:uint32 BE | payload`. Il payload è
  `opcode:uint8 | tagCount:uint16 | tags`; un tag è `name<<1|hasChildren:uint16
  | type:uint8 | length:uint32 | [childCount | children] | data`. La lunghezza
  di un tag conta i suoi dati più ogni figlio con un header fisso di 7 byte
  (+2 quando il figlio ha figli), indipendentemente dalla codifica dei numeri.
- Il client non annuncia mai `EC_TAG_CAN_ZLIB` né `EC_TAG_CAN_UTF8_NUMBERS`,
  così amuled risponde con pacchetti semplici; il decoder accetta comunque
  payload zlib e numeri codificati in UTF-8.
- Login (protocollo `0x0204`, aMule 2.3.x): `EC_OP_AUTH_REQ` →
  `EC_OP_AUTH_SALT` → `EC_OP_AUTH_PASSWD` con
  `MD5(hex(MD5(password)) + hex(MD5(uppercase hex of the salt)))` →
  `EC_OP_AUTH_OK`.
- Una sola connessione, richieste serializzate; un errore di trasporto la
  chiude e la richiesta viene ritentata una volta con un login nuovo, così un
  riavvio di amuled è invisibile.
- Operazioni: stato (`EC_OP_STAT_REQ` con lo stato della connessione), lista
  server, connetti/disconnetti, connetti/disconnetti server, `server.met` /
  `nodes.dat` da URL, avvio/stop Kad, coda dei download, aggiunta link,
  pausa/ripresa/eliminazione, pulizia dei completati, file condivisi, ricerca
  (avvio, risultati, progressione, stop), preferenze (cartelle di arrivo e
  temporanea, limiti di banda) e il log.
- aMule tiene **una sola** lista di risultati: una nuova ricerca sostituisce la
  precedente. Gextto serializza le ricerche (`ed2kSearchMu`), così le query
  degli alias del gap filler vanno una dopo l'altra. Una ricerca Kad finisce
  quando il core riporta `0xfffe` e il numero di risultati è stabile; una
  ricerca sui server viene letta fino allo scadere della durata configurata.

### Lato Gextto

| File | Ruolo |
|---|---|
| `ed2ksearch.go` | impostazioni, client condiviso con backoff veloce mentre amuled è giù, stato in cache, ricerca e ripiego del gap filling, `GET /api/ed2k/status` |
| `ed2k_downloads.go` | lista dei download, aggiunta/pausa/ripresa/rimozione, la sessione che trasforma i file completati in eventi `torrent_finished`, la configurazione dell'import in copia |
| `uiweb_v2_ed2k.go` | la pagina eD2k (stato, server, Kad, ricerca, link incollati) |
| `web_background.go` | unisce gli eventi di completamento di aMule al ciclo eventi dei torrent, ognuno gestito dalla propria sessione |

### Test

- `internal/amule`: codec (round trip, regola della lunghezza dei tag, numeri
  UTF-8, zlib, input malformato), risposta di login, client contro il core finto
  `internal/amule/amuletest`.
- `TestLiveCore` gira contro un amuled vero quando `GEXTTO_AMULE_EC=host:port` e
  `GEXTTO_AMULE_PASSWORD` sono impostate: stato, preferenze, server,
  aggiunta / pausa / ripresa / eliminazione di un link, ricerca Kad, limiti, log
  e un rifiuto con password sbagliata. Nelle esecuzioni normali viene saltato.
- Gextto: `ed2ksearch_test.go` (ricerca, rifiuto, gate del ripiego, password
  sbagliata, alias, aggiunta link e azioni, eventi di completamento, import in
  copia) contro il core finto.

## Storia

Gextto ha inizialmente distribuito un proprio demone eD2k/Kad, `gx-ed2k`. Lo
strato di protocollo funzionava, ma lo scheduler dei download (fonti, code,
richieste UDP ripetute) non ha mai raggiunto il livello di un vero client eMule, e la
copertura Kad misurata sugli episodi mancanti era del 7–13% (vedi
[`measurements.md`](measurements.md)). È stato ritirato in favore di aMule; il
codice resta nella storia git, al commit `9f4e2b9`.
