# Implementazione e Integrazione di gx-torrent in Gextto

Questo documento descrive in dettaglio l'architettura e lo stato dell'implementazione di `gx-torrent`, il demone alternativo (scritto in puro Go tramite la libreria `cenkalti/rain`) utilizzato da Gextto per lo scaricamento dei file. L'obiettivo dell'integrazione è raggiungere un'equiparazione delle funzionalità con il motore integrato basato su `libtorrent`.

Il demone agisce in un processo separato ed espone una REST API (su porta 8890 di default) che Gextto interroga per ottenere la lista dei file, impartire azioni (pause, resume) e iniettare la configurazione.

## Gestione dei 7 Punti Critici

L'elenco sottostante chiarisce come sono state affrontate le esigenze funzionali primarie e i "desiderata" richiesti, bypassando laddove necessario le rigide limitazioni strutturali della libreria `cenkalti/rain`.

### 1. Limiti di Banda e Schedulazione (Implementato)
- **Problema:** `cenkalti/rain` non espone setter a runtime per ricalibrare i limiti di banda in volo (necessita di passare una `Config` alla creazione della sessione).
- **Soluzione:** È stato creato un endpoint `POST /api/v1/config` sul demone `gx-torrent` che accetta dinamicamente i nuovi limiti di velocità (Download/Upload). Al ricevimento, il demone aggiorna la sua struttura e riavvia *gracefully* in memoria la sessione interna in una frazione di secondo. Nessun dato viene perso e le connessioni si ristabiliscono immediatamente.
- **Supporto Gextto:** Gextto invia in modo trasparente questi parametri richiamando `SetGlobalSpeedLimits`.

### 2. Percorsi di Salvataggio per Singolo Torrent (Implementato)
- **Problema:** La libreria `rain` salva globalmente tutto all'interno di un'unica `DataDir` (es. `./downloads/[ID_TORRENT]`), ignorando le richieste di posizionamento specifico per singolo download.
- **Soluzione (Symlink Injection):** Quando a `gx-torrent` viene richiesto di scaricare un magnet o un .torrent indicando una specifica `destination`, il demone *prima* dell'allocazione vera e propria genera un Link Simbolico (Symlink) nella cartella locale (`./downloads/[ID]`) che punta dritto alla `destination` voluta. `rain` risolve il link in trasparenza e scrive in modo nativo sul disco o sulla partizione di destinazione, permettendo al contempo di mantenere attivi i seed senza successive operazioni di spostamento.

### 3. Opzioni di Rete (Desiderata)
- Al momento non vi è un mapping esteso per le interfacce di rete e vincoli IP (Outgoing/Listen interfaces) specifici come in `libtorrent`. `gx-torrent` eredita il bind generico sul sistema.

### 4. Opzioni di Privacy (Desiderata)
- Anche in questo caso non vi è una gestione fine di anonimato/cifratura o proxing. `cenkalti/rain` applica un handshake cifrato standard ma non possiede un equivalente completo della gestione *strict-encryption* di libtorrent.

### 5. Code di Download e Connessioni (Implementato e Vincolante)
- **Problema:** Manca un concetto di coda asincrona integrato nel motore, in grado di sospendere attivamente i torrent se il numero limite viene superato.
- **Soluzione (Arbitraggio Gextto):** Proprio come accade per l'integrazione di Gextto sopra `qBittorrent-nox`, anche qui è stato popolato il metodo `AdjustQueue()`. L'orchestratore di Gextto polla continuamente la API di `gx-torrent` valutando i torrent che riportano lo stato "downloading". Mettendo in correlazione il valore massimo stabilito dall'utente per "Download attivi", l'engine di Gextto sospende proattivamente i torrent in eccesso (passando allo stato in pausa per simulare una coda) e li riprende dinamicamente man mano che gli slot si liberano.

### 6. Storage e Allocazione Disco (Limitato all'Engine)
- Il demone usa il default di allocazione e gestione sparse files del sistema operativo host. Le cache su disco e la preallocazione aggressiva configurabili dal pannello "libtorrent" in Gextto si applicano dove supportate.

### 7. Seeding, Stalled e Ratio di Condivisione (Integrazione di Ritorno)
- Anche le dinamiche di raggiungimento *seed_ratio* ed espulsione degli sciami bloccati (stalled) sono governate dall'orchestratore Gextto, che confronta l'Upload Rate, il tempo di scaricamento e l'Upload Complessivo esposti da `gx-torrent` nelle statistiche (`/api/v1/torrents`).

## Aggiornamenti dell'Interfaccia Utente

- La voce nel menu delle Impostazioni utilizza l'etichetta amichevole **gx-torrent (demone alternativo)** (invece di "esterno").
- I metadati relativi a **Categoria** e **Tag** sono stati ripristinati per `gx-torrent` in modo da armonizzarsi con le funzioni preesistenti del menu di "Scarico" di Gextto.
- Le impostazioni globali quali i Limiti Connessione e i Slot Download Attivi rimangono visibili (nella sezione *libtorrent*, che agisce da ombrello globale) e sono instradate a questo motore. L'URL della Web API per comunicare col demone non richiede configurazione manuale: il sistema inietta tacitamente e autonomamente il valore predefinito `http://127.0.0.1:8890`.
