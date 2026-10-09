# Harness di test Holepunch (BEP 55) — note tecniche

Documento di supporto a `scripts/holepunch-netns-test.sh`. Descrive cosa verifica
l'harness, le scelte fatte e i limiti dell'emulazione. Per il comportamento del
motore vedi `docs/gx-torrent.md`, sezione *Holepunching*.

## Scopo

Verificare end-to-end che gx-torrent apra un buco (BEP 55, `ut_holepunch`) tra due
client dietro NAT/firewall usando un peer pubblico come **relay**. Lo script è
root-only e vive interamente in network namespace dedicati: la rete dell'host non
viene toccata (né interfacce, né rotte, né firewall).

## Topologia

```
  gx-lab (pubblico)          gx-r1 (firewall)          client netns
  -----------------          ----------------          ------------
  tracker 10.0.0.1:13800
  seed    10.0.0.1:51400
  relay   10.1.0.1:51410 <-- vr1 10.0.0.2
                             var 10.10.0.1  <--- gx-a 10.10.0.2 (leecher)

  gx-lab (pubblico)          gx-r2 (firewall)          client netns
  -----------------          ----------------          ------------
  tracker 10.0.0.1:13800
  seed    10.0.0.1:51400
  relay   10.1.0.1:51410 <-- vr2 10.1.0.2
                             vbr 10.20.0.1  <--- gx-b 10.20.0.2 (seed)
```

`gx-lab` instrada tra le due subnet dei client. `gx-r1`/`gx-r2` sono **firewall
stateful** (nessun NAT): lasciano uscire, DROPPano le nuove connessioni in
entrata e accettano le risposte (`ESTABLISHED,RELATED`); rifiutano il TCP verso le
porte peer così il buco si apre su uTP.

## Cosa fa il test (sequenza)

1. Tracker HTTP minimo in Python (in `gx-lab`), interval 30 s.
2. Il **seed** carica `.torrent` e dati (è l'unica fonte completa).
3. **B** entra come leecher, scarica dal seed e **completa**, restando connesso
   al relay. Il relay è un **leecher interessato** ai dati di B.
4. Solo allora entra **A** (leecher): non raggiunge B direttamente, chiede
   l'introduzione al relay; il relay manda `connect` a entrambi e i due dialano su
   uTP. Il buco si apre da entrambi i lati.
5. Successo se compare un peer con origine **`holepunch`** (chi diala) o
   `incoming` (l'altro lato) in `/api/v1/torrents/<hash>/peers`.

## Scelte di progetto

### Firewall stateful invece di NAT
Netfilter Linux **non emula un NAT port-translating "cone"**. Il dial diretto del
leecher verso l'IP esterno del target (che precede sempre il rendezvous) arriva
all'IP WAN del router del target: conntrack crea una entry *locale* dalla quale
non riesce a dedurre una mappatura, e quella entry "occupa" la porta esterna del
peer. Quando il target riceve il `connect` e apre la sua mappatura, il SNAT
confligge (`SNAT: porta in uso`) o ripiega su un'altra porta: in entrambi i casi
il buco non si apre. Un firewall stateful, **senza traduzione di indirizzo**,
riproduce lo stesso comportamento osservabile (inbound non richiesto bloccato,
buco che si apre solo dopo il dial incrociato) e rende il test deterministico.
La traduzione di porta è un comportamento del sistema operativo, non di
gx-torrent: non è parte di ciò che l'harness deve validare.

### Seed e relay su IP diversi
rain deduplica i peer per **IP** (`connectedPeerIPs`), non per IP:porta. Due
daemon sullo stesso IP (anche con porte diverse) vengono visti come un solo peer:
il client si connette a uno e ignora l'altro. Per questo seed (`10.0.0.1`) e relay
(`10.1.0.1`) stanno su IP distinti.

### `-outgoing-interface` = IP di ascolto
Il socket del tracker non è legato all'interfaccia di ascolto: senza
`-outgoing-interface` il demone annuncerebbe al tracker il proprio IP "di
default" (per il relay, `10.0.0.1` invece di `10.1.0.1`), e i client dialerebbero
un indirizzo sbagliato. Seed e relay vengono quindi avviati con
`-outgoing-interface` uguale a `-listen-interface`.

### Ruoli: relay leecher interessato
In rain un torrent **completato non fa dial** (`handleNewPeers`/`dialAddresses`
escono se `completed`) e al completamento **scarta i peer non interessati**
(`checkCompletion`). Quindi un relay seed verrebbe scartato da B (seed) e non
potrebbe introdurre nessuno. Serve un relay **leecher** che resta interessato ai
dati di B: ha un `download_limit` basso per restare incompleto durante la prova.

### Tracker con interval lungo (30 s)
Un re-annuncio frequente rifà partire il dial diretto di A verso B mentre il
buco si sta aprendo; la connessione verrebbe attribuita al dial del tracker
(origine `tracker`) invece che all'holepunch. L'interval di 30 s lascia il dial
holepunch come unico in volo nel momento critico.

### Rilevamento del successo su entrambi i lati
Il `connect` del relay fa dialare entrambi: chi arriva primo ha origine
`holepunch`, l'altro vede la connessione come `incoming`. Il test passa se
l'origine `holepunch` compare su A **o** su B.

## Bug dello script originale (storico)

- **Un solo NAT condiviso**: A e B uscivano con lo stesso IP esterno, quindi il
  buco non poteva mai aprirsi.
- **`/api/v1/torrents/<hash>/peers` non esponeva `source`**: il controllo
  `"source":"holepunch"` non poteva scattare (il campo è stato aggiunto all'API,
  che ora rispetta i docs).
- **Ruoli sbagliati** (relay seed + B seed): il relay non poteva essere connesso
  a B.
- **`progress` nella lista è 0–100**, non 0–1: `grep '"progress":1'` era un falso
  positivo (ora `100`).
- **`grep` su log binari** non stampava nulla: serve `grep -a`.

## Uso

```bash
make gx-torrent                                   # costruisce bin/gx-torrent
sudo scripts/holepunch-netns-test.sh              # esegue la verifica
sudo scripts/holepunch-netns-test.sh --keep       # lascia la topologia per il debug
sudo scripts/holepunch-netns-test.sh --size 16    # torrent piu' grande
```

Un esempio di esecuzione riuscita (output testuale, con la sorgente `holepunch`
sul lato che diala) è in `scripts/risultato.txt`.

Variabili utili per il debug:

- `HOLEPUNCH_DEADLINE=<s>` — timeout dell'attesa del buco (default 90).
- `HOLEPUNCH_B_DEADLINE=<s>` — timeout del completamento di B (default 60).
- `HOLEPUNCH_DIAG=1` — dump delle tuple conntrack dei due router a fine prova.

Su fallimento lo script stampa le righe `holepunch` dei log
(`holepunch rendezvous …`, `holepunch connect …`), utili per capire a che punto
si è fermato il protocollo.

## Limiti noti

- Il buco è emulato con firewall stateful, non con un NAT port-translating cone
  (vedi sopra).
- Il buco si osserva come `holepunch` sul lato che diala e `incoming` sull'altro:
  è il comportamento normale, dipende da chi vince la corsa del dial.
- La deduplica dei peer per IP di rain è un limite noto; in uno swarm reale i
  peer stanno su IP diversi.

## Riferimenti

- `docs/gx-torrent.md`, sezione *Holepunching*.
- `third_party/rain/GEXTTO.md`, riga *Holepunching*.
- `scripts/holepunch-netns-test.sh`.
