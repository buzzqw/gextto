# gx-torrent — misure di choking e seeding

Documento di lavoro per la voce "Qualità seeding/choking" della wishlist
(`docs/gx-torrent-migliorie.md`, decisione #13). Regola di fondo: **misurare
prima**, poi decidere se toccare il core di rain.

## Vincolo: non forzare l'automazione

Le misure devono riflettere il sistema **come gira davvero**. L'harness non
disattiva, per comodità, coda/slot, seed policy o scheduler di banda:

- l'harness deterministico dell'unchoker esercita l'algoritmo puro, senza
  sessione: non c'è automazione da rispettare;
- l'harness di sciame locale usa il demone con la sua **coda autogestita** e la
  sua automanagement attive;
- la campagna su sciame reale misura gx-torrent pilotato da Gextto, quindi con
  `AdjustQueue`, seed policy e limiti attivi.

Nessuna misura si fa "spegnendo le code": sarebbe una misura di laboratorio che
non rappresenta il comportamento in produzione.

## Metriche

Choking (allocazione degli slot di upload):

- quali peer vengono sbloccati e per quanto tempo;
- fairness tra peer con pari velocità;
- rotazione dell'optimistic unchoke;
- comportamento in download (premia i downloader più veloci) e in seed (premia
  gli uploader più veloci).

Seeding (trasferimento end-to-end):

- byte caricati dal seed rispetto al corpus (`Nx corpus`): quanto il seed deve
  trasmettere;
- throughput di upload del seed e di download dei peer;
- tempo di completamento;
- rapporto (ratio) e tempo a 1:1 in una campagna reale.

## 1. Harness deterministico dell'unchoker

File: `third_party/rain/internal/unchoker/sim_test.go` (gira con
`make test-rain`).

Pilota l'`Unchoker` reale con peer sintetici e velocità scriptate, e fissa le
proprietà dell'algoritmo:

- `TestUnchokerPrefersFastDownloadersWhileDownloading` — in download gli slot
  vanno ai downloader più veloci;
- `TestUnchokerPrefersFastUploadersWhenSeeding` — in seed agli uploader più
  veloci;
- `TestUnchokerFastUnchokeFillsFreeSlot` — un peer appena interessato prende
  subito uno slot libero;
- `TestUnchokerOptimisticNeverStealsRegularSlot` — l'optimistic non ruba uno
  slot regolare e ruota tra i peer;
- `TestUnchokerNeverExceedsSlots` — mai oltre il budget di slot;
- `TestUnchokerFairnessSeeding` — con uploader uguali, tutti ottengono uno slot;
- `BenchmarkTickUnchokeSeeding` — costo del tick con molti peer.

Uso: `go test github.com/cenkalti/rain/v2/internal/unchoker -v` e
`-bench TickUnchoke`.

Questo è il banco per **confrontare varianti dell'algoritmo**: le stesse
proprietà si verificano dopo una modifica, e il benchmark misura il costo. Le
soglie sono proprietà, non numeri assoluti: le differenze di throughput si
misurano con lo sciame.

## 2. Harness di sciame locale (opt-in)

File: `cmd/gx-torrent/measure_test.go`. Non gira in `make test`: serve
`GX_MEASURE=1`.

```
make measure-seeding                       # seeder + 3 leecher
GX_MEASURE_SUPERSEED=1 make measure-seeding  # confronto col super-seeding
GX_MEASURE_MESH=1 make measure-seeding       # i leecher si collegano anche tra loro
GX_MEASURE_LEECHERS=2 make measure-seeding   # sciame di 2 peer
```

Un seeder e N leecher su loopback, ognuno con un indirizzo `127.0.0.x` distinto
(rain rifiuta una seconda connessione dallo stesso IP verso un torrent). Di
default ogni leecher è collegato **solo al seeder**: si misura l'**upload del
seed** (byte e throughput), che è ciò che il super-seeding cambia. Il demone gira
con coda e automanagement normali.

Con `GX_MEASURE_MESH=1` i leecher si collegano anche tra loro (listener distinti)
e possono scambiarsi i pezzi. È però **sperimentale su un solo host**: le
connessioni multiple su loopback (bind su `127.0.0.x`, dedup per IP di rain) sono
instabili, con peer che non si connettono o non completano. Usarlo solo per una
fotografia indicativa, con pochi peer, ripetendo.

Output tipico:

```
seeder: uploaded=6291456 bytes (3.00x corpus) avg=121846 KiB/s
leecher 0: downloaded=2097152 uploaded=0 completed_in=0s avg=40615 KiB/s
...
```

Limiti noti:

- **scambio peer-to-peer non affidabile** su un host: la modalità mesh è
  sperimentale e va usata con cautela (vedi sopra). La misura affidabile è
  quella senza mesh; per lo scambio e per il confronto tra client serve una
  campagna con più macchine;
- i numeri assoluti dipendono dalla macchina e non vanno confrontati tra loro
  fuori dallo stesso run;
- un peer può impiegare fino a due tick di ritentativo (10 s ciascuno) per
  avanzare nel super-seeding: per questo la scadenza è ampia e l'harness
  riporta ciò che ha visto invece di fallire su un peer lento.

## 3. Campagna su sciame reale

Per il confronto con **libtorrent/qBittorrent** servono sciami reali. Metodo:

1. **Stesso sciame, stesso ruolo**: pubblicare un torrent noto e far girare
   gx-torrent, libtorrent e qBittorrent come seed (o come leech) nelle stesse
   condizioni; una variabile alla volta.
2. **Stesse politiche**: stessi slot di upload, limiti di banda e numero di
   peer, così il confronto è sull'algoritmo e non sulla configurazione.
3. **Stesse metriche**: byte caricati dal seed (`Nx corpus`), throughput di
   upload, tempo a 1:1, numero di seed generati; ripetere più volte e usare la
   mediana, non un singolo run.
4. **Automazione attiva**: Gextto guida gx-torrent con `AdjustQueue`, seed policy
   e scheduler accesi.

Questo è l'unico banco che può mostrare il beneficio del super-seeding (un seed
che carica ~1x invece di Nx) e la qualità del choking in uno sciame con scambio
reale tra peer.

## Risultati (prima tornata, misura locale)

Su questa macchina (Intel N97, loopback), corpus di 2 MiB.

Choking, harness deterministico (`make test-rain`):

- tutte le proprietà passano: i downloader più veloci in download, gli uploader
  più veloci in seed, `FastUnchoke` immediato, optimistic che non ruba uno slot
  regolare e ruota, budget di slot rispettato, fairness tra uploader uguali;
- costo del tick con 50 peer: ~1,1 µs, 896 B/op, 1 alloc — trascurabile.

Seeding, sciame locale **senza** mesh (seed → 3 leecher, 5 ripetizioni):

- seeding normale: completamento ~41–42 ms, seed carica **3,00x** il corpus;
- super-seeding: completamento ~51–52 ms (**~24% più lento**), seed carica
  **3,00x**.

Seeding, mesh a 2 peer (indicativo, mesh instabile):

- seeding normale: seed **1,00x** (i leecher si scambiano i pezzi);
- super-seeding: seed **1,00x**.

Lettura onesta:

- il choking di rain è **corretto e a costo trascurabile**: non emerge alcun
  problema da correggere;
- su questi sciami piccoli e veloci il super-seeding **non riduce** l'upload del
  seed rispetto al seeding normale (che già sfrutta lo scambio) e costa throughput
  e variabilità. Il vantaggio previsto da BEP 16 è per il *seeding iniziale* di
  torrent grandi con molti peer lenti, che **non è riproducibile su un host**;
- conseguenza: il super-seeding resta **opt-in**, non un default;
- il mesh a 3+ peer su un solo host non converge: la verifica dello scaling va
  fatta con la campagna su sciame reale.

Hardening applicato durante le misure: un peer interessato ma **in stallo**
(velocità a zero) ora riceve comunque il pezzo successivo al tick, invece di
aspettare il solo `not-interested`; evita che un peer resti fermo in attesa di un
pezzo che il seed non gli ha ancora offerto.

## Cosa decidere in base ai risultati

- Se il choking di rain perde in modo misurabile rispetto a libtorrent su uno
  sciame reale, si interviene su `internal/unchoker` con il banco deterministico
  come rete di sicurezza, e lo sciame come verifica.
- Se il super-seeding localmente è più lento ma su sciame reale carica meno, si
  tiene come opzione di *initial seeding* (come da BEP 16), non come default.
- Se non emerge alcuna differenza misurabile, non si tocca il core: il costo di
  manutenzione (rebase con upstream) non è giustificato.

## Riferimenti

- `docs/gx-torrent-migliorie.md` — lacune, decisioni, wishlist;
- `third_party/rain/GEXTTO.md` — inventario delle modifiche al fork;
- `docs/gx-torrent.md` — motore, limiti di banda, super-seeding.
