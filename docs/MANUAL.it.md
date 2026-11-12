# Manuale utente di Gextto

Questo manuale spiega **come si usa Gextto**: dall'installazione al primo
download, dalle serie che segui ogni settimana alla manutenzione della
libreria. È ordinato per compiti — *aggiungere una serie*, *capire perché un
episodio non arriva*, *fare un backup* — e non per pagine dell'interfaccia.
L'elenco completo di ogni impostazione, campo e pulsante è nelle appendici in
fondo.

La UI è disponibile in italiano, inglese, tedesco, francese, spagnolo e
polacco; qui sono usate le etichette italiane. Per quelle inglesi c'è
[MANUAL.en.md](MANUAL.en.md), che fa anche da manuale per tedesco, francese,
spagnolo e polacco. La voce **Manuale** dell'interfaccia apre questo documento
online, nella lingua scelta; **Changelog** apre le novità. Nessuno dei due è
dentro il programma: si aggiornano senza aggiornare Gextto.

> [!IMPORTANT]
> Inizia in **modalità prova** (dry-run): Gextto cerca e decide, ma non scarica
> nulla. Verifica percorsi, sorgenti e un titolo di prova prima di passare ai
> download veri. L'interfaccia web è amministrativa ed è aperta per
> impostazione predefinita: tienila in una rete fidata oppure, per l'accesso da
> fuori, attiva il login e usa HTTPS (vedi [Accesso e sicurezza](#20-accesso-e-sicurezza)).

## Indice

**Parte I — Primi passi**

1. [Cos'è Gextto e come lavora](#1-cosè-gextto-e-come-lavora)
2. [Installare Gextto](#2-installare-gextto)
3. [La configurazione iniziale](#3-la-configurazione-iniziale)
4. [Il primo download, in prova e poi davvero](#4-il-primo-download-in-prova-e-poi-davvero)
5. [Orientarsi nell'interfaccia](#5-orientarsi-nellinterfaccia)

**Parte II — Uso quotidiano**

6. [Seguire una serie TV](#6-seguire-una-serie-tv)
7. [Seguire un film](#7-seguire-un-film)
8. [Cercare e scaricare a mano](#8-cercare-e-scaricare-a-mano)
9. [Seguire i download](#9-seguire-i-download)
10. [Dove finiscono i file](#10-dove-finiscono-i-file)
11. [Fumetti](#11-fumetti)
12. [eD2k e Kad con aMule](#12-ed2k-e-kad-con-amule)

**Parte III — Configurare**

13. [Sorgenti: feed e indexer](#13-sorgenti-feed-e-indexer)
14. [Qualità, punteggi e upgrade](#14-qualità-punteggi-e-upgrade)
15. [Il motore torrent](#15-il-motore-torrent)
16. [Velocità e rete](#16-velocità-e-rete)
17. [Libreria, NAS e spazio](#17-libreria-nas-e-spazio)
18. [Notifiche](#18-notifiche)
19. [Integrazioni con altri programmi](#19-integrazioni-con-altri-programmi)
20. [Accesso e sicurezza](#20-accesso-e-sicurezza)

**Parte IV — Tenere tutto in ordine**

21. [Controllare che tutto funzioni](#21-controllare-che-tutto-funzioni)
22. [Curare la libreria](#22-curare-la-libreria)
23. [Backup e ripristino](#23-backup-e-ripristino)
24. [Aggiornare Gextto](#24-aggiornare-gextto)
25. [Usare Gextto dal terminale (TUI)](#25-usare-gextto-dal-terminale-tui)

**Parte V — Quando qualcosa non va**

26. [Risoluzione dei problemi](#26-risoluzione-dei-problemi)
27. [Glossario](#27-glossario)

**Appendici — Riferimento**

- [A. Configurazione, voce per voce](#appendice-a-riferimento--configurazione)
- [B. Integrazioni](#appendice-b-riferimento--integrazioni)
- [C. Manutenzione](#appendice-c-riferimento--manutenzione)

---

# Parte I — Primi passi

## 1. Cos'è Gextto e come lavora

Gextto è un **servizio unico** che gira su un computer sempre acceso (un mini
PC, un server, un NAS): tu gli dici quali serie e quali film ti interessano, e
lui li cerca, sceglie la versione migliore secondo le tue regole, la scarica, le
dà un nome ordinato e la mette nella tua libreria. Si comanda dal browser
(`http://<host>:5000`) o dal terminale. Non servono altri componenti: il motore
torrent, **gx-torrent**, è incluso e Gextto lo avvia da solo.

### Il percorso di un episodio

Ogni download segue sempre la stessa strada. Conoscerla aiuta a capire dove
guardare quando qualcosa non va.

1. **Titolo seguito.** Aggiungi una serie o un film alla libreria, con un
   profilo di qualità (risoluzione, lingua, sottotitoli, audio).
2. **Ciclo di ricerca.** A intervalli regolari Gextto legge i feed e interroga
   gli indexer (Jackett, Prowlarr, indexarr…) per i titoli che segui. Ogni
   risultato è una **release**.
3. **Scelta.** Ogni release passa una serie di controlli: è il titolo giusto?
   rispetta qualità e lingua? è già in libreria una copia uguale o migliore?
   Tra quelle valide vince quella con il **punteggio** più alto.
4. **Download.** La release scelta va al motore torrent (o ad aMule, per
   eD2k). La pagina **Scarico** mostra l'avanzamento.
5. **Arrivo in libreria.** A download finito Gextto rinomina il file secondo il
   tuo modello e lo copia, sposta o collega nella cartella della serie o del
   film. Se in libreria c'era una versione peggiore, quella va nel **cestino**.
6. **Seed.** Il torrent continua a condividere finché non raggiunge i limiti
   di seed, poi esce dalla sessione.

### Le parole che userai

| Parola | Cosa vuol dire |
|---|---|
| **Release** | un risultato trovato da un feed, un indexer o un motore web |
| **Ciclo** | una passata automatica di ricerca e scelta |
| **Modalità prova** (dry-run) | Gextto cerca e decide, ma non avvia nessun download |
| **Sessione torrent** | i torrent che il motore sta ancora gestendo (in download o in seed) |
| **Libreria** | le cartelle finali, ordinate, con serie e film (spesso su un NAS) |
| **Archivio** | il catalogo locale delle release viste nei feed, consultabile e ricercabile |
| **Cestino** | dove finiscono i file sostituiti o rimossi, prima della cancellazione definitiva |
| **Upgrade** | la sostituzione di un file in libreria con una versione migliore |

Tieni distinti questi quattro insiemi: **sessione torrent**, **libreria**,
**archivio** e **cestino**. Una pulizia in uno non tocca gli altri: togliere un
torrent dalla sessione non cancella il file dalla libreria, e svuotare l'archivio
non cancella nessun video.

## 2. Installare Gextto

Servono Linux con systemd, su x86_64 o aarch64. Gextto è scritto in Go puro e
non compila nulla sul server.

### Con l'installer (consigliato)

```bash
curl -fsSL https://raw.githubusercontent.com/buzzqw/gextto/main/install.sh | sudo bash
```

L'installer scarica l'ultima versione pubblicata, ne verifica il checksum
SHA-256 e la installa:

- il programma in `/opt/gextto`, i dati in `/var/lib/gextto`;
- il servizio `gextto.service`, avviato subito;
- i pacchetti di base con i nomi giusti per Debian, Ubuntu, Fedora, openSUSE e
  Arch e, se la distribuzione li fornisce, `ffmpeg`, `mediainfo` e
  `mkvtoolnix` (servono per leggere la qualità reale dei file, controllarne
  l'integrità e pulire le tracce audio). `--no-media-tools` li salta.

Il servizio gira con **l'utente che esegue l'installer**, così può scrivere
nelle cartelle dei tuoi media. Per un account separato usa `--user NOME`, oppure
`--user gextto` per l'account di sistema isolato e senza login (con
`--media-group` per dargli accesso ai media di altri utenti).

Altre opzioni utili:

| Opzione | Effetto |
|---|---|
| `--channel stable` / `--release <tag>` | installa la versione stabile o una release precisa invece dell'ultima |
| `--local-archive <file>` | installa da un archivio già scaricato, senza rete |
| `--no-media-tools` | non installa ffmpeg, mediainfo e mkvtoolnix |
| `GEXTTO_REPO`, `GEXTTO_RELEASE` | scelgono un altro repository o un'altra release |

Un file facoltativo `/etc/gextto/gextto.env`, se esiste, viene caricato come
variabili d'ambiente del servizio.

### Senza root, da un checkout

```bash
make build
GEXTTO_DATA_DIR="$HOME/gextto-data" \
  GEXTTO_LISTEN=127.0.0.1:5000 \
  scripts/install-user-service.sh
systemctl --user status gextto.service
```

È un servizio systemd **dell'utente**. Se deve restare acceso anche quando
esci dalla sessione, esegui `loginctl enable-linger "$USER"`.

### Con Docker

Gextto, gx-torrent, Jackett, Prowlarr, FlareSolverr e aMule insieme in un'unica
composizione: vedi [Docker: stack completo](DOCKER.it.md).

### Verificare che giri

Apri `http://<host>:5000`. Se la pagina non si apre:

```bash
systemctl status gextto            # installazione di sistema
systemctl --user status gextto     # servizio dell'utente
/opt/gextto/gexttod --version      # versione installata
```

Gextto accetta **una sola istanza per cartella dati**: se un altro Gextto usa
già la stessa cartella, o la porta 5000 è occupata, il nuovo processo si ferma
subito e lo scrive nel log, senza toccare i database.

## 3. La configurazione iniziale

La prima volta che apri l'interfaccia parte la **Configurazione iniziale**, in
cinque passi. Puoi saltarla e riaprirla quando vuoi da `/?view=setup`; non
compare più su un'installazione che ha già titoli o archivio.

1. **Accesso.** Scegli una password. Puoi lasciare libero l'accesso dalla rete
   di casa e chiederla solo da fuori.
2. **Cartelle.** Indica libreria, download, cartella temporanea e cestino.
   **Sfoglia** apre le cartelle del server (e ne crea di nuove); **Verifica**
   controlla, con l'utente del servizio, che ogni cartella esista e sia
   scrivibile, e mostra il tipo di disco (NAS compreso), lo spazio libero e se
   sta sullo stesso disco della libreria.
3. **Fonti.** Un indexer (Prowlarr, Jackett o indexarr) e la chiave **TMDB**,
   consigliata, per titoli, locandine ed episodi. In alternativa va bene la
   chiave **TVDB**, con il PIN se lo hai.
4. **Primo titolo.** Cerca una serie o un film e aggiungilo.
5. **Attiva.** Accendi il ciclo automatico e scegli se uscire dalla modalità
   prova (**Scarica davvero**). Per il primo giro lasciala accesa.

### Le cartelle e il loro ruolo

| Cartella | A cosa serve | Note |
|---|---|---|
| **Download** | i dati dei torrent in sessione | non cancellarla a mano mentre un torrent è attivo |
| **Temporanea** | dati incompleti e metadati | deve essere scrivibile; può stare su un disco veloce |
| **Libreria** | i file finali, ordinati per serie e film | di solito sul NAS |
| **Cestino** | i file sostituiti o rimossi | si svuota secondo la retention |
| **Cartelle osservate** | `.torrent` e `.magnet` da importare | facoltative |

Due consigli che evitano la maggior parte dei problemi:

- **Prova i permessi con l'utente del servizio**, non con il tuo. Una cartella
  che vedi dalla tua shell può non essere visibile al servizio systemd.
- **Metti download e libreria sullo stesso filesystem**, se puoi: Gextto potrà
  usare gli hardlink, e un file in seed entrerà in libreria subito, senza
  occupare spazio due volte (vedi [Dove finiscono i file](#10-dove-finiscono-i-file)).

## 4. Il primo download, in prova e poi davvero

Fai un solo cambiamento alla volta: se modifichi insieme percorsi, qualità e
sorgenti, un problema diventa difficile da riprodurre.

1. **Controlla le cartelle** in *Configurazione → Archivio e spazio* e in
   *Configurazione → Libreria e rinomina*.
2. **Aggiungi una sola sorgente** in *Configurazione → Sorgenti* e premi
   **Verifica**. Aggiungi le altre solo quando questa funziona (vedi
   [Sorgenti](#13-sorgenti-feed-e-indexer)).
3. **Aggiungi un titolo di prova**: una serie con una sola stagione o un film.
4. **Avvia un ciclo** dalla Dashboard (*Ricerca automatica*, solo il dominio
   che ti interessa: Serie, Film o Fumetti).
5. **Guarda il Log**: devi vedere il ciclo, le sorgenti interrogate e, per ogni
   release, perché è stata accettata o scartata.
6. **Fai una ricerca manuale** dello stesso titolo e apri **Perché non questa?**
   su un risultato (vedi [Cercare e scaricare a mano](#8-cercare-e-scaricare-a-mano)).
7. **Passa ai download veri**: riapri l'ultimo passo della configurazione
   iniziale (`/?view=setup&step=5`; il badge **DRY-RUN** in alto ti ricorda che
   sei in prova), spunta **Scarica davvero** e premi **Completa**. Ha effetto
   subito, senza riavviare; dalla TUI lo stesso interruttore è in
   *Impostazioni*. Un Gextto avviato con `--dry-run` dalla riga di comando resta
   invece in prova fino al riavvio.

### Checklist prima dei download veri

- [ ] `http://<host>:5000` si apre;
- [ ] **Salute** non segnala problemi di permessi o di spazio;
- [ ] la cartella temporanea è scrivibile;
- [ ] la libreria (o il NAS) è montata e scrivibile dall'utente del servizio;
- [ ] almeno una sorgente risponde a **Verifica**;
- [ ] una ricerca manuale restituisce release sensate;
- [ ] il ciclo in prova non ha prodotto errori inattesi.

## 5. Orientarsi nell'interfaccia

### Il menu

| Gruppo | Pagine | A cosa servono |
|---|---|---|
| Panoramica | **Dashboard** | lo stato di tutto a colpo d'occhio, la ricerca, i cicli |
| Download | **Scarico**, **Log** | i trasferimenti in corso; il log in diretta |
| Libreria | **Serie TV**, **Film** | i titoli che segui, i loro episodi e i mancanti |
| Scoperta | **Esplora**, **Archivio**, **Fumetti**, **eD2k** | trovare titoli nuovi, consultare le release viste, fumetti, aMule |
| Sistema | **Configurazione**, **Integrazioni**, **Manutenzione**, **Salute**, **Blocklist**, **Manuale**, **Changelog** | impostazioni, servizi collegati, pulizie, diagnosi |

### La barra in alto

- **Aggiungi torrent**, presente in ogni pagina: apre il modulo per un magnet,
  un URL o un file `.torrent` (vedi [Aggiungere un torrent o un magnet](#aggiungere-un-torrent-o-un-magnet)).
- **Aggiorna**: ricarica subito i dati della pagina.
- Le **velocità** di download e upload (torrent più aMule) e **CPU e RAM per
  processo**: Gextto, gx-torrent e, se attivo e locale, aMule. Si aggiornano
  ogni 5 secondi.
- **Tema**, dimensione del testo e **Font**. Il dialogo **Tema** ha i temi
  pronti (Scuro, Chiaro, Crema, Nord, Dracula, Solarized, Gruvbox, Alto
  contrasto e altri, in versione chiara e scura); **Personalizza** crea un tema
  tuo da quattro colori (sfondo, pannelli, testo, accento) e avvisa se il
  contrasto è troppo basso. I temi salvati sono sul server e li ritrovi da ogni
  dispositivo; il tema attivo, il testo e il font restano una scelta del
  singolo browser.
- La **lingua** dell'interfaccia.
- **Dona**, in basso a destra (sul telefono è nel menu *Altro*), e
  **Aggiornamento disponibile** quando c'è una versione nuova.

### Dal telefono

Sotto i 900 pixel di larghezza l'interfaccia si compatta: menu scorrevole in
alto, metriche su due colonne, finestre a tutta larghezza, tabelle che scorrono
dentro il loro riquadro. Nella pagina **Scarico** il nome di un torrent va a capo
per intero invece di fermarsi ai primi caratteri: sul telefono non c'è il
suggerimento del mouse che mostra il resto. Ogni azione resta raggiungibile col
tocco. Tema, dimensione del testo, **Font**, lingua e **Dona** sono nel menu
*Altro*, così la barra in alto tiene solo il titolo, **＋** e **Aggiorna**. Nelle
liste **Serie TV** e **Film** ogni titolo è una scheda: nome e stato su una
riga, **Pausa** ed **Elimina** affiancati sotto. Nella **Dashboard**, quando
negli ultimi 30 minuti non si è trasferito nulla, al posto del grafico della
banda c'è una riga che lo dice. Le pagine viaggiano compresse e gli script restano nella cache
del browser, quindi l'interfaccia resta usabile anche su una rete lenta.

### Accessibilità

L'interfaccia si usa da tastiera e con le tecnologie assistive: i controlli
hanno nomi accessibili; i dialoghi gestiscono il focus, `Tab`, `Esc` e il
ritorno al controllo che li ha aperti; le intestazioni ordinabili si attivano da
tastiera e annunciano l'ordinamento; aggiornamenti, errori e notifiche usano
regioni live; il layout regge viewport stretti, ingrandimento e reflow a 320
pixel. Le verifiche automatiche (axe-core e Playwright) si lanciano dal
checkout con `cd uiweb/end2end && npm ci && npm run test:a11y`. Non sono una
dichiarazione di conformità WCAG: per quella serve anche una verifica manuale.
Dettagli in [ACCESSIBILITY.md](ACCESSIBILITY.md).

---

# Parte II — Uso quotidiano

## 6. Seguire una serie TV

### Aggiungere una serie

1. Apri **Esplora**, cerca il titolo e scegli il risultato giusto: controlla
   anno, rete e locandina, perché esistono spesso titoli omonimi. In
   alternativa usa **Serie TV → Aggiungi**.
2. Scegli il **Profilo di qualità**. È il profilo a dare alla serie qualità,
   audio minimo, lingua, sottotitoli, esclusioni e «Permetti upgrade»: il modulo
   non ha altri campi per questi valori, così non possono contraddirsi. I
   profili si preparano in *Configurazione → Qualità e upgrade* (vedi
   [Profili di qualità](#profili-di-qualità)).
3. Aggiungi, se servono, **Esclusioni in più** solo per questa serie: parole
   che, se compaiono nel nome della release, la scartano. Si sommano a quelle
   del profilo.
4. Scegli le **stagioni** da seguire. Gli intervalli sono ammessi.
5. Controlla la **cartella**: è già proposta come *cartella della libreria +
   titolo*. Con «Crea cartella» attivo viene creata all'aggiunta; togli la
   spunta per scriverla a mano. «Sottocartelle per stagione» mette gli episodi
   in *Stagione 01*, *Stagione 02*…
6. Salva e apri la scheda della serie.

Nel nome della cartella e dei file Gextto toglie i caratteri che Windows, exFAT
e le condivisioni SMB rifiutano (`\ / : * ? " < > |`) e il punto o lo spazio
finale: «Titolo: Sottotitolo» diventa «Titolo Sottotitolo». La libreria resta
così leggibile da qualunque computer.

> [!TIP]
> Lascia attive anche le stagioni non ancora uscite: il calendario e la ricerca
> dei mancanti continueranno a funzionare quando arriveranno.

### La scheda della serie

In testa trovi locandina, trama, cast (collegato alle schede TMDB o TVDB), anno,
rete, stato, completezza e il percorso in libreria. Le azioni:

| Azione | Quando usarla |
|---|---|
| **Cerca mancanti** | per cercare subito tutti gli episodi mancanti, senza aspettare il ciclo |
| **Scansiona archivio** | hai copiato a mano dei file nella cartella della serie e vuoi che Gextto li riconosca |
| **Aggiorna da TMDB** (o TVDB) | sono usciti episodi nuovi, o titoli e date sono cambiati |
| **Anteprima rinomina** | vedi i nomi *Vecchio → Nuovo* prima di rinominare; **Forza rinomina** ricalcola anche i file che sembrano già corretti |
| **Modifica** | cambi nome, stagioni, cartella, alias, ID, profilo |
| **Storia dei download** | ricostruisci cosa è successo a ogni episodio |

I campi della scheda più importanti:

| Campo | Effetto |
|---|---|
| **Nome** | il titolo mostrato in libreria. Cambiandolo restano collegati episodi, storia, ricerca dei mancanti ed esclusioni della serie; la cartella dell'archivio non viene spostata, quindi i file restano dove sono |
| **Profilo di qualità** | applica i valori di un profilo alla serie. È preselezionato il profilo che corrisponde ai valori attuali; se nessuno corrisponde compare **Valori attuali**, che salvando non cambia nulla |
| **Stagioni** | quali stagioni sono seguite |
| **Alias** | altri nomi con cui la serie compare nelle release |
| **Esclusioni** | parole che scartano una release |
| **ID TMDB / ID TVDB** | identificano la serie nei due servizi; un ID sbagliato si corregge qui. Sono indipendenti: salvarne uno non cancella l'altro |
| **Stato della serie** | *Automatico* lascia decidere alle fonti (vedi sotto); *Terminata* o *In corso* vale più di qualsiasi fonte, quando sai qualcosa che le fonti non hanno ancora registrato |

**Audio minimo** e **Permetti upgrade** non sono nella scheda: arrivano dal
profilo e si cambiano con [Modifica selezionati](#modificare-più-titoli-insieme).

### Quando una serie è terminata

Gextto segna una serie come terminata (🏁) mettendo insieme quattro segnali, dal
più forte:

1. **la tua scelta** in *Stato della serie*, che vale più di tutto il resto;
2. **una prossima puntata con la data**: se TMDB (o TVDB) annuncia un episodio
   da oggi in poi, la serie è **▶ in corso**, qualunque cosa dica lo stato;
3. **lo stato di TMDB** (o di TVDB senza la chiave TMDB): *Ended* o *Canceled*
   la segnano come terminata;
4. **TVmaze**, una seconda opinione gratuita e senza chiave, che spesso registra
   prima le cancellazioni. Se TVmaze dice che la serie è finita mentre TMDB la
   dà ancora in corso, Gextto la segna come terminata; se TVmaze aspetta la
   decisione sul rinnovo e nessuna puntata è annunciata, mostra **⏳ rinnovo
   incerto**.

Quando TMDB e TVmaze danno risposte opposte, la scheda mostra anche **⚠ fonti in
disaccordo**: decidi tu con *Stato della serie*. Passando sopra l'etichetta vedi
chi l'ha decisa. I segnali si aggiornano una volta al giorno, insieme ai
metadati, o subito con **Aggiorna da TMDB**. A TVmaze vanno gli identificativi
TVDB e IMDb della serie (e il nome, quando TVmaze è la fonte perché mancano le
chiavi); si spegne con *Usa TVmaze per le serie*, in *Impostazioni → Accesso e
servizi*.

È solo un'etichetta: una serie terminata continua a essere cercata come le
altre, perché le puntate mancanti possono sempre uscire. Per smettere di
cercarla, mettila in pausa.

### Gli episodi

Gli episodi sono raggruppati per stagione. Su ognuno puoi:

- **🔍 cercarlo**: la ricerca si apre in un riquadro sotto la puntata e dice a
  che punto è (quante sorgenti sta interrogando, quante hanno risposto, quando
  passa a eD2k);
- **📜 vederne la storia**: tutti gli eventi di quel download (vedi
  [Un episodio non è arrivato](#un-episodio-non-è-arrivato-dove-guardare));
- copiare il magnet, **ignorarlo** (non verrà più cercato; riattivandolo torna
  candidato), forzarlo, eliminarlo;
- **↻ Riscarica**: sposta il file attuale nel cestino e rimette la puntata in
  cerca, anche quando normalmente non risulterebbe mancante (per esempio perché
  la stagione è coperta da un pack o ci sono già episodi successivi).

### Gli episodi mancanti

La scheda **Mancanti** di *Serie TV* raccoglie tutte le puntate già andate in
onda che non sono in libreria, raggruppate per serie e per stagione:

- **un clic su una puntata** la cerca, prima nell'archivio locale e poi online,
  e mostra i risultati con **Scarica**;
- **Cerca mancanti** cerca tutte le puntate mancanti di una serie in background:
  la scheda mostra l'avanzamento, puoi annullare, e alla fine elenca i
  risultati con **Scarica**. Non scarica nulla da sola, e la lista resta aperta
  dopo ogni download: puoi prenderne più di uno senza rifare la ricerca;
- **Filtra per serie** restringe l'elenco.

Non sono mancanti le puntate non ancora andate in onda, né quelle che hanno già
un download in corso (anche aggiunto a mano o da una cartella osservata), né le
puntate di una stagione il cui pack sta scaricando. Se il download finisce in
errore, la puntata torna tra i mancanti.

Se per una puntata non esiste nessuna copia nel profilo, l'elenco propone
l'unica copia nella lingua richiesta con il badge **🆘 ultima risorsa**: è quella
che Gextto prenderebbe da solo, e l'upgrade la sostituirà appena esce una copia
nel profilo.

Non devi fare tutto a mano: nei cicli automatici Gextto cerca i mancanti da
solo, prima nell'archivio locale e poi online, e — se lo attivi — chiede a eD2k
una volta per stagione quando i torrent non trovano nulla. **Cerca mancanti**
serve solo a non aspettare il ciclo successivo.

### Season pack ed episodi singoli

- Un **season pack** può riempire molti episodi, ma il confronto con la
  libreria resta episodio per episodio: un pack non sostituisce un episodio che
  hai già in qualità uguale o migliore.
- A parità di punteggio tra un episodio singolo e un pack che lo contiene,
  vince la release che copre più episodi; poi un REMUX; poi quella con seed
  noti.
- Gextto non riscarica un episodio vecchio se hai già quelli successivi, a meno
  che sia un buco riconosciuto, un upgrade vero o una tua richiesta esplicita
  (↻ Riscarica).
- Con il **controllo dei pack** attivo (*Configurazione → Seed e completamento →
  Controllo dei pack*, spento di default) Gextto legge l'elenco dei file del
  pack appena arriva: se il pack non aggiunge nulla lo scarta, se solo alcuni
  file servono scarica soltanto quelli. Un pack che arriva da un magnet, e di
  cui quindi l'elenco non si conosce ancora, viene aggiunto fermo: si
  scaricano solo i metadati, pochi KB, e il download parte (con la sua riga nel
  log e la sua notifica) solo se dentro c'è qualcosa che serve. Altrimenti il
  log dice cosa conteneva e il pack sparisce senza aver scaricato nulla. Vale
  solo per i pack scelti dal ciclo: un pack che avvii tu a mano non viene mai
  scartato.
- Se un download resta **fermo a 0 B/s o in attesa dei metadati** oltre il
  tempo impostato in *Configurazione → Qualità e upgrade → Download bloccati*,
  la ricerca può avviare un'alternativa della stessa puntata (o della stagione)
  senza togliere quello bloccato: se si riprende, la libreria terrà comunque la
  copia migliore.
- Se un pack viene **abbandonato** perché fermo, gli episodi già completi
  entrano comunque in libreria e solo quelli mancanti vengono cercati di nuovo
  (nel log: `🛟 … already complete, added to the library before giving up`).

## 7. Seguire un film

### Aggiungere un film

1. Cerca il film in **Esplora** e controlla anno e titolo originale.
2. Aggiungilo e scegli il **Profilo di qualità**; aggiungi, se serve,
   esclusioni del solo film.
3. Apri la scheda e premi **Cerca subito** per vedere i risultati senza
   aspettare il ciclo.
4. Prima di accodare una release al limite, apri **Perché non questa?**.

Il film è riconosciuto da titolo e anno, e la punteggiatura non conta: una
release si riconosce allo stesso modo con o senza i due punti. Se il nome di una
voce è contenuto in quello di un'altra, vince la più specifica. Non aggiungere
due volte lo stesso film scritto in modi diversi: correggi i metadati di quello
che hai già.

Contano anche i **titoli alternativi** che TMDB conosce per il film — quello
internazionale e quelli usati in altri paesi: una release pubblicata con un altro
nome arriva lo stesso, e anche le corrispondenze in archivio della scheda vengono
cercate con quei titoli. Le ricerche sugli indexer usano i nomi che servono: il
nome mostrato, il titolo originale (la lingua del film), il titolo nella lingua
scelta per il film e quello inglese. Gli altri titoli che TMDB elenca non
vengono interrogati uno per uno — moltiplicherebbero le richieste — ma restano
validi per riconoscere le release e per la ricerca in archivio. I titoli di
lavorazione sono esclusi, perché a volte appartengono a un film diverso.

Il confronto è quello di Sonarr e Radarr: conta il titolo **intero**, non una
parola trovata dovunque nel nome. Una release si riconosce quando il suo nome
**si apre** con il titolo del film o con uno dei suoi titoli alternativi, parola
per parola, senza badare ad accenti, maiuscole e punteggiatura; il tag che un
indexer mette davanti al titolo (una lingua come «ITA», una risoluzione, un
gruppo tra parentesi quadre) viene saltato. Così un nome di personaggio di una
parola sola — che TMDB elenca tra i titoli alternativi — non basta più a far
prendere la release di un altro film. Gli articoli iniziali non contano, e a
parità di titolo vince il nome del film rispetto a un titolo alternativo. Le
**corrispondenze in archivio** della scheda mostrano solo le release che
Gextto riconoscerebbe davvero.

### La scheda del film

Mostra locandina, trama, cast, i **migliori risultati** trovati nelle sorgenti
e le **corrispondenze in archivio**, ognuna con **Perché non questa?**. Sotto la
trama la riga **Titoli alternativi (TMDB)** apre l'elenco dei nomi con cui il
film è uscito all'estero: sono quelli che Gextto usa per riconoscere le release
e per cercare in archivio, e restano salvati con il film. Se il film è stato
aggiunto a mano e TMDB non è ancora stato interrogato, la riga dice **non
ancora letti**: **Aggiorna TMDB** li legge subito, insieme ai metadati. Dalla
scheda puoi modificare il film, riscaricarlo o cercarlo subito.

Nell'editor:

- **Lingua** accetta un codice o più codici separati da virgola (`ita,eng`), anche
  oltre le voci dell'elenco;
- **Sottotitoli** con **Sottotitoli obbligatori**: *Sì* scarta una release senza
  quei sottotitoli, *No* li tratta solo come preferenza;
- **Permetti upgrade** e **audio minimo** si cambiano con *Modifica
  selezionati*.

La pagina **Film** ha due schede, **Monitorati** e **Scaricati**, con colonne
ordinabili. Nella scheda *Monitorati*, accanto a *attiva* o *in pausa*, un **✓**
segnala che una versione del film è già stata scaricata: passando il mouse sul
segno compare il percorso del file in libreria, oppure il titolo archiviato
quando lo spostamento non è registrato.

### Elenco o locandine

Sopra le tabelle di *Serie TV* e *Film* il selettore **Elenco / Locandine**
cambia la vista: *Locandine* mostra una card per titolo con copertina, qualità,
lingua e stato. La scelta vale per entrambe le pagine ed è ricordata.

### Modificare più titoli insieme

Per cambiare gli stessi valori su molti titoli — per esempio portare dieci film
a `2160p` o mettere in pausa più serie — usa la vista *Elenco*:

1. **Spunta le righe.** La casella nell'intestazione spunta tutte le righe
   visibili: combinala con *Filtra l'elenco…* per prendere, per esempio, tutti i
   titoli in `720p`.
2. Premi **Modifica selezionati**. Spunta solo i campi da cambiare (qualità,
   audio minimo, lingua, sottotitoli, esclusioni) e scrivi il nuovo valore. Un
   campo spuntato e vuoto viene svuotato; un campo non spuntato resta com'è.
   *Permetti upgrade* e *Stato* cambiano solo se scegli un valore diverso da
   «Lascia com'è».
3. **Parti da un profilo** copia nei campi i valori di un profilo, tranne le
   esclusioni, che restano di ogni titolo (spuntale solo se vuoi sostituirle).
4. Premi **Applica ai titoli selezionati**.

Stagioni, cartella, alias e gli altri valori dei titoli non vengono toccati. I
requisiti obbligatori di un film (sottotitoli, lingue richieste) seguono il
valore che qualificano: cambiando lingua o sottotitoli si aggiorna anche il
requisito.

## 8. Cercare e scaricare a mano

Il ciclo automatico fa quasi tutto da solo. La ricerca manuale serve a vedere
**cosa c'è in giro** prima di cambiare una configurazione, a scegliere una
release precisa o a scaricare qualcosa che non segui.

### Dove si cerca

| Da dove | Cosa cerca |
|---|---|
| **Dashboard**, riquadro di ricerca | archivio locale, indexer, motori web; eD2k se spunti **eD2k** |
| **Scarico**, *Cerca una release* | lo stesso, senza lasciare la pagina dei download |
| **Esplora** | titoli su TMDB (tendenze, popolari, prossime uscite) da aggiungere; ricerca generica di release |
| **Archivio** | le release viste nei feed, all'istante; online solo se spunti *Cerca anche nel web* |
| 🔍 su un episodio, **Cerca subito** su un film | quel titolo preciso |

### Leggere i risultati

1. Scrivi un titolo (o una query tecnica) e attendi che le sorgenti rispondano;
   quelle lente vengono segnalate al timeout.
2. Confronta titolo, sorgente, qualità, dimensione e seed/peer.
3. Restringi con **Filtra i risultati già caricati**: lavora sui risultati
   ricevuti e non rifà la ricerca.
4. Apri **Perché non questa?** sui risultati interessanti.
5. Premi **Accoda** (o **Scarica**) quando sei convinto: i risultati restano
   nell'elenco, così puoi aggiungerne altri senza rifare la ricerca.

La ricerca manuale mostra anche release che il ciclo **non** sceglierebbe,
proprio per permetterti di ispezionarle: vederla non significa che verrebbe
scaricata.

### Dove finisce un download aggiunto a mano

| Da dove lo aggiungi | Dove finisce |
|---|---|
| Ricerca libera: **Dashboard**, **Scarico** (*Cerca una release*), **Esplora**, un link eD2k incollato, la TUI | resta nella **cartella di trasferimento**, con il suo nome: non va in libreria, non viene rinominato e non conta come episodio o film presente. Viene condiviso secondo i limiti di seed e poi fermato; lo togli tu dalla sessione |
| **Scarica** sui risultati di un titolo che segui, dalla sua pagina (🔍 su un episodio mancante, le corrispondenze dell'archivio di un film), **Accoda** sui titoli seguiti della Dashboard | va in **libreria**, accanto agli altri episodi o film, come un download automatico |

Nella colonna del motivo un download della prima riga si legge *✋ aggiunto a
mano, resta in trasferimento*.

### «Perché non questa?»

È una diagnosi in sola lettura di una release: non accoda nulla e non cambia
niente. Mostra l'esito, il punteggio con le sue componenti e ogni controllo:

- **Superata** — il controllo è passato;
- **Bloccante** — basta da solo a scartare la release;
- **Informativa** — dipende dal resto del ciclo (altri candidati, ritardo,
  spazio libero, buchi da riempire).

In fondo c'è il confronto con la libreria: primo download, possibile upgrade o
copia non migliore di quella che hai. I controlli seguono quest'ordine: titolo
seguito, blocklist e duplicati, filtri globali e della sorgente, sanità della
release (sottotitoli impressi, dimensione assurda), qualità/lingua/sottotitoli/
esclusioni del titolo, confronto con libreria e download attivi. Il punteggio
sceglie solo tra le release che hanno superato tutti i controlli.

### Aggiungere un torrent o un magnet

**Aggiungi torrent**, nella barra in alto di ogni pagina, accetta un magnet, un
URL `.torrent` o un file `.torrent`. Puoi indicare la cartella di salvataggio,
**Scarica subito** (fuori coda) e **Non rinominare**. Con gx-torrent ci sono
anche:

- **Salta verifica**: aggiunge il torrent su file che hai già, senza verificarli
  prima; va subito in seed e ogni pezzo viene controllato la prima volta che un
  peer lo chiede. Usalo solo per dati che sai completi;
- download **sequenziale** e **prima/ultima parte** dei file;
- **preallocazione** dello spazio, disattivabile per il singolo torrent.

Un magnet con `so=` scarica solo i file indicati; con `ws=` aggiunge web seed.
Un indirizzo che risponde con un magnet viene aggiunto come magnet.

Puoi anche copiare file `.torrent` o `.magnet` in una **cartella osservata**
(*Configurazione → Sorgenti → Cartelle osservate*): Gextto aspetta che il file
smetta di cambiare, lo aggiunge e poi lo rimuove o lo rinomina `.imported`.

### L'Archivio

**Archivio** è il catalogo di tutte le release viste nei feed, anche per titoli
che non segui. La ricerca parte da sola mentre scrivi (da tre caratteri; per
quelli più corti premi **Invio**) ed è immediata. `-parola` esclude i titoli che
la contengono. Lo stesso torrent compare una volta sola. Dalle righe puoi
accodare (⬇️), aprire **Perché non questa?** (🔍), copiare il magnet (🧲) o
eliminare la riga (🗑️).

- **Cerca anche nel web** estende la ricerca a indexer e motori online: è più
  lenta e parte solo quando premi **Cerca**.
- **Cerca anche su eD2k** (con aMule attivo) aggiunge sotto i risultati di
  eD2k.
- La scheda **Visti dai feed** raggruppa le release per titolo, con il numero di
  versioni e la migliore risoluzione e punteggio: espandi un gruppo per
  accodarne una.

Le release di un **feed tolto da Sorgenti** (o commentato con `#` nella lista)
restano nel catalogo, come quelle di un indexer spento: i loro collegamenti
`.torrent` non vengono più aperti, quindi Gextto non riporta più l'errore di un
feed che non c'è più. Rimettendo il feed tornano utilizzabili da sole; le righe
archiviate prima di questa versione si comportano come prima.

### Link eD2k

In **Esplora** e nella pagina **eD2k** puoi incollare uno o più link `ed2k://`
(anche mescolati a testo): Gextto li verifica, toglie i doppioni e li passa ad
aMule con **Aggiungi tutti** o uno per uno.

## 9. Seguire i download

La pagina **Scarico** mostra tutto quello che si sta trasferendo: torrent,
download HTTP dei fumetti e file eD2k di aMule (con il badge **eD2k**), in
un'unica lista che si aggiorna ogni 5 secondi.

### Leggere la lista

Ogni riga mostra nome, stato, progresso, velocità, tempo stimato, peer e
rapporto. Lo **sciame** (🌐 seeder / leecher) è quello riportato da tracker e DHT:
con **0 leecher** non c'è nessuno a cui caricare. Ordini con **Ordina per** (per
tutta la lista insieme), filtri per nome e per tag, e scegli uno stato con i
pulsanti sopra l'elenco (*Tutti i torrent*, *In scarico*, *In seed*, *In pausa*,
*Completati*, *In attesa di seed*, *Errore*), ognuno con il suo conteggio; uno
stato vuoto non compare. Nella testa del pannello, accanto ai conteggi, ↓ e ↑
dicono quanto il motore ha scaricato e inviato da quando è partito.

| Stato | Cosa significa | Cosa fare |
|---|---|---|
| In coda | registrato ma non ancora partito | aspettare: lo avvierà la coda |
| Download | sta trasferendo | controllare velocità e peer |
| Stalled | nessun byte in più da troppo tempo; messo in pausa | aspettare il nuovo tentativo automatico |
| Seeding | completato, sta condividendo | lasciarlo finire secondo i limiti di seed |
| Errore | il motore ha segnalato un errore | leggere il motivo prima di rimuoverlo |
| Archiviato | il file è in libreria | nulla; non cancellare la sorgente mentre è in seed |
| 📌 pin | forzato fuori coda | **Sblocca pin** nella sua scheda per rimetterlo in coda |

### Agire sui download

- **Su una riga**: pausa/riprendi, ricontrolla, rimuovi.
- **Su più righe**: spunta i torrent e usa la barra di selezione: **Pausa**,
  **Riprendi**, **Ricontrolla**, **Tag** (assegna, crea o togli un tag),
  **Rimuovi dalla sessione**.
- **Strumenti**: **Pulisci completati** (toglie i torrent che hanno finito il
  seed), **File orfani…**, **Aggiorna ogni 5 s** e il collegamento a
  **Rimozione a fine seed**.

**Rimuovi** toglie il torrent dalla sessione e può chiedere se cancellare anche
i dati. Togliere un torrent dalla sessione **non** toglie il file dalla
libreria. Un torrent con seed infinito non viene mai tolto da *Pulisci
completati*.

### Il dettaglio di un torrent

Clic sul nome: il dettaglio si apre sopra lo *Storico download*, così la lista
resta visibile. Le schede:

| Scheda | Cosa trovi |
|---|---|
| **Generale** | dati del torrent, copia magnet, **Esporta .torrent**, riannuncia, pin, riavvia, segna come fallito, **Super seeding**, web seed |
| **Tracker** | aggiungi tracker (`tier|url` per riga) o rimuovine uno |
| **Contenuto** | i file, con la **priorità** (Salta, Normale, Alta, Massima) e, con gx-torrent, **▶** per lo streaming |
| **Peers** | i peer connessi |
| **Pezzi** | la mappa dei pezzi: verde scaricato, giallo in corso, grigio mancante. Con gx-torrent sotto c'è la **disponibilità tra i peer**: più la barra è piena, più peer connessi hanno i pezzi che ti mancano; il rosso indica pezzi che nessun peer connesso ha. Con qBittorrent c'è la sola mappa, e i pezzi dei file saltati risultano mancanti |
| **Velocità** | il grafico di download e upload di quel torrent negli ultimi 30 minuti, con media e picco. Gextto registra la velocità solo dei torrent che stanno trasferendo, quindi il grafico c'è già quando apri la scheda |
| **Limiti** | rapporto, giorni di seed e banda di quel torrent: **-1** usa il valore globale, **0** toglie il limite |
| **Storage** | **Sposta storage** in un'altra cartella |
| **Storia** | ogni evento di quel download, dalla scelta all'arrivo in libreria |

Passando il mouse su un valore o su un pulsante compare la spiegazione. Se
sposti lo storage in una cartella che **contiene già i dati**, Gextto li
ricontrolla e li riusa invece di riscaricarli. Ogni salvataggio mostra un
avviso in alto che dice cosa è stato salvato; se qualcosa non riesce l'avviso è
rosso, riporta il motivo e la scheda resta com'era, con i valori inseriti.

### Streaming e media server

Con gx-torrent ogni file ha **▶**: apre lo **streaming HTTP**, e i pezzi della
parte che stai guardando vengono scaricati per primi. Puoi iniziare a guardare
prima che il download finisca.

Lo stesso motore fa da **media server DLNA/UPnP** (*Configurazione → Motore
torrent → server DLNA/UPnP*, spento di default): TV e console sulla rete di casa
vedono i media completati e le cartelle della libreria, con locandine e
miniature già presenti (nel formato di Jellyfin, Kodi ed Emby) e i sottotitoli
`.srt`/`.vtt` con lo stesso nome del video. Non ha autenticazione, quindi
risponde solo ai dispositivi della rete locale.

### La banda

Il pulsante **Banda**, in alto a destra nella pagina, mostra cosa limita la
velocità in questo momento e permette un'**eccezione** temporanea alla regola
impostata in *Configurazione → Velocità e rete*:

- **Auto** — Gextto misura la linea e si adatta;
- **Limita download** / **Limita upload** — un valore dall'elenco, o **Senza
  limite**;
- **Durata** — 1, 3, 6, 12 ore o **Finché non la tolgo**;
- **Valori personalizzati…** — download e upload in KiB/s e durata in minuti;
- **Togli l'eccezione** — torna subito alla regola.

L'icona dice cosa vale: ⚡ un'eccezione (con i minuti rimasti), ⏰ una fascia
della programmazione, altrimenti la regola. Con eD2k attivo **aMule segue gli
stessi limiti**. Vedi anche [Velocità e rete](#16-velocità-e-rete).

### Torrent fermi (stalled)

Se un torrent non aggiunge byte per un certo tempo diventa **stalled**: Gextto
lo mette in pausa, così non occupa uno slot della coda, e ogni tanto lo riprende
e lo riannuncia. Se non riparte entro il limite finale, lo abbandona e cerca
un'altra release. Avere peer connessi senza ricevere dati non azzera il timer.

I tempi sono in *Configurazione → Seed e completamento → Torrent bloccati*:

| Impostazione | Predefinito |
|---|---|
| Considera bloccato dopo | 60 minuti |
| Riprova i torrent bloccati ogni | 60 minuti |
| Rimuovi i torrent bloccati dopo | 14 giorni (`0` = mai) |

Nel log, in inglese: `is stuck at` (con il motivo), `is still stuck` a ogni
tentativo, `is downloading again` quando riparte, `Gave up on` quando viene
abbandonato. La Dashboard elenca i download fermi in **Da controllare**, con il
prossimo tentativo e quando verranno abbandonati.

### Limiti di seed: regole per sorgente e seed inattivo

Quando un torrent smette di condividere lo decide Gextto, con lo stesso criterio
per gx-torrent e qBittorrent. Per ogni limite (rapporto, tempo, inattività) vale
il primo livello che lo imposta:

1. il limite del **singolo torrent** (scheda **Limiti**);
2. la prima **regola di seed** che corrisponde (*Configurazione → Seed e
   completamento → Regole di seed*);
3. il limite **globale**.

Una **regola di seed** indica una **sorgente** (basta una parte del nome
dell'indexer, senza distinguere maiuscole: `mircrew` vale anche per `mircrew
indexerr`) e/o una **categoria** (Serie TV, Film, Fumetti, Altro). Così le regole
di un tracker privato valgono solo per i suoi torrent. Un campo vuoto usa il
globale. Un torrent arrivato dalla ricerca aggregata di Jackett non porta il
nome del tracker: per lui valgono categoria o globale.

**Seed infinito.** `0` vuol dire infinito, per il rapporto come per il tempo, e
**basta che uno dei due sia infinito** perché il seed non finisca mai: il torrent
non viene fermato nemmeno per inattività e non viene spostato in libreria (ci
entra una copia o un hardlink). Per fermare il seed, rapporto **e** tempo devono
essere entrambi maggiori di zero. Vale anche per eD2k.

**Ferma il seed inattivo dopo** ferma un torrent che non invia dati da quei
minuti. Conta solo il tempo in seed attivo (non in coda né in pausa) e riparte a
ogni riavvio di Gextto: un riavvio può solo allungare il seed.

A fine seed il torrent segue il suo percorso: archiviazione in libreria e, se
attiva, rimozione dalla sessione.

### Lo storico

**Storico download**, in fondo alla pagina, elenca i download conclusi: nome
(con il badge **NAS** quando il file è archiviato), tipo, stagione ed episodio,
regola di cartella, punteggio, percorso in libreria e data.

## 10. Dove finiscono i file

### Cosa succede a download finito

Gextto riconosce il contenuto, lo rinomina con il tuo modello e lo mette nella
cartella della serie o del film. Il comportamento dipende dalla forma del
torrent e dalla presenza di una libreria configurata:

| Contenuto | Con libreria configurata | Senza libreria |
|---|---|---|
| Un episodio, come file singolo | archiviato e rinominato secondo le regole della serie | resta nella cartella download |
| Un episodio dentro una cartella | il video entra in libreria rinominato; la cartella resta per il seed e poi va nel **cestino** | la cartella resta intatta in Download |
| Un season pack | i video degli episodi (riconosciuti da `S01E02`, `1x02`…) entrano in libreria; il pack resta per il seed e poi va nel **cestino** | il pack resta intatto in Download |

Sottotitoli, NFO e immagini del pack restano nella cartella sorgente. Una
cartella sorgente non viene mai spostata o cancellata mentre è in seed, e va
nel cestino solo se il cestino è configurato e la rimozione a fine seed è
attiva; senza cestino i file sorgente non vengono mai cancellati.

### Copia, spostamento o hardlink

- **Sposta** (*Sposta gli episodi/pack in archivio*): a fine seed il file viene
  spostato in libreria.
- **Hardlink**: finché il file è in seed, Gextto prova a creare in libreria un
  **secondo nome per gli stessi dati**. Il file compare in libreria subito e non
  occupa spazio due volte. Cancellare uno dei due nomi non tocca l'altro: i dati
  spariscono solo quando li cancelli entrambi.
- **Copia**: se l'hardlink non è possibile, Gextto copia e lo scrive una volta
  nel log con il motivo.

L'hardlink funziona solo se download e libreria sono sullo **stesso
filesystem**. Due cartelle dello stesso NAS montate **separatamente** (per
esempio `nas:/volume1/Download` e `nas:/volume1/Video` come due mount) non
bastano: monta una radice comune (`nas:/volume1`). Se usi programmi che
modificano i video **sul posto** (alcuni tagger, `mkvpropedit`), spegni l'opzione
*Configurazione → Seed e completamento → Hardlink invece della copia durante il
seed*: cambierebbero anche il file in seed.

### Upgrade

Quando arriva una versione migliore di un file che hai già, la nuova entra in
libreria e la vecchia va nel **cestino**. Una versione uguale o peggiore viene
scartata (nel log: `discarded as inferior`). Per congelare un titolo imposta
**Permetti upgrade** su *No* con *Modifica selezionati*.

### Il nome dei file

Con **Rinomina episodi** attivo, il nome si sceglie in *Configurazione →
Libreria e rinomina → Composizione del nome*, con quattro formati e
un'anteprima:

| Formato | Episodi | Film |
|---|---|---|
| **Base** (predefinito) | serie, stagione, episodio e titolo | `Titolo (Anno)` |
| **Standard** | più risoluzione e codec | `Titolo (Anno) [risoluzione][codec]` |
| **Completo** | più audio, HDR e lingua | `Titolo (Anno) [risoluzione][audio canali][HDR][codec]` |
| **Personalizzato** | il tuo modello, con i token `{Serie}`, `{Stagione}`, `{Episodio}`, `{Titolo}`, `{Source}`, `{Gruppo}`, `{Risoluzione}`, `{VideoCodec}`, `{Audio}`, `{Canali}`, `{HDR}`, `{Lingue}` | il modello dei film (sotto) |

Con **Personalizzato** i film hanno un modello tutto loro, nel campo *Template
personalizzato per i film*: i token sono `{Titolo}`, `{Anno}`, `{Source}`,
`{Gruppo}`, `{Risoluzione}`, `{VideoCodec}`, `{Audio}`, `{AudioCodec}`,
`{Canali}`, `{HDR}` e `{Lingue}`, con i loro pulsanti e la loro anteprima. Il
predefinito, `{Titolo} ({Anno}) [{Risoluzione}]`, è il nome che i film avevano
già, quindi una libreria già rinominata conserva i suoi nomi: se personalizzi
il modello degli episodi con i tag di qualità, aggiungili anche a quello dei
film, che non li prende da solo. Un token senza
valore (un HDR sconosciuto, un anno mancante) sparisce insieme alle sue
parentesi.

I tag tecnici non vengono presi solo dal nome della release: prima di
rinominare Gextto legge il file con `mediainfo` (o, se manca, con `ffprobe`) e
usa risoluzione, codec, audio, canali, HDR/Dolby Vision e lingue reali. Un tag
già presente nel nome resta se il file non lo smentisce. Un file con più episodi
contigui prende l'intervallo (`S01E01-E02`). Senza titolo dell'episodio (nessuna
chiave TMDB o TVDB) il nome usa «Episodio N».

### Importare un file che hai già

Per un video che Gextto non ha scaricato — un episodio recuperato a mano, una
vecchia stagione, un film da un altro disco — usa **Manutenzione → Importa
file**:

1. scrivi il percorso o premi **Sfoglia** (un clic su un video lo sceglie,
   **Seleziona** prende la cartella intera);
2. premi **Analizza**: Gextto propone serie o film, stagione, episodio e
   cartella di destinazione;
3. correggi se serve e premi **Importa ora**.

Il file viene rinominato, spostato in libreria e registrato, e Jellyfin o Plex
vengono aggiornati. Se esiste già un file con lo stesso nome l'import si ferma:
spunta **Sostituisci il file esistente** per mandare il vecchio nel cestino.

Se invece hai copiato file direttamente nella cartella di una serie, basta
**Scansiona archivio** dalla scheda della serie.

## 11. Fumetti

1. In **Fumetti → Aggiungi fumetto** scrivi il titolo e premi **Trova**.
2. Scegli il risultato esatto di GetComics e conferma. Gextto salva quel post e
   lo usa per il download, senza confonderlo con un albo omonimo.
3. Un titolo con il numero (per esempio «Titolo #41») segue la serie **da quel
   numero in avanti**: i numeri successivi vengono scaricati, quelli precedenti,
   le altre serie e le raccolte no.

La pagina ha anche la lista dei fumetti seguiti, l'estrazione dei link da un
post, le impostazioni dei **weekly pack** e lo storico con reinvia, elimina e
forza. Un weekly pack si scarica una volta sola; uno trovato prima che fossero
pubblicati i link viene ricontrollato a ogni ciclo e accodato appena il link
compare.

I download HTTP diretti compaiono in **Scarico** come gli altri. Nei file **CBZ**
scaricati direttamente Gextto aggiunge un `ComicInfo.xml` con serie, numero e
anno, che Komga, Kavita e i lettori usano per ordinare gli albi; un file che lo
ha già, i fumetti da torrent (sono in seed) e i CBR non vengono toccati. Un albo
che arriva come RAR viene salvato `.cbr`.

## 12. eD2k e Kad con aMule

Gextto può usare anche la rete **eD2k/Kad**, tramite **aMule**: `amuled` fa il
lavoro di rete, Gextto lo comanda con le **connessioni esterne (EC)**, lo stesso
protocollo di `amulecmd`. È facoltativo e spento di default; con aMule spento i
torrent non cambiano.

### Collegare aMule

1. Installa aMule (`amule-daemon` su Debian/Ubuntu) e abilita le connessioni
   esterne in `amule.conf`:

   ```ini
   [ExternalConnect]
   AcceptExternalConnections=1
   ECAddress=127.0.0.1
   ECPort=4712
   ECPassword=<md5 della password>
   ```

   `ECPassword` è l'MD5 della password: `printf '%s' 'segreta' | md5sum`.
2. Avvia `amuled`.
3. In **Configurazione → eD2k e Kad** accendi **eD2k e Kad tramite aMule** e
   inserisci indirizzo, porta e password **in chiaro**.
4. Apri sul router le porte TCP e UDP di aMule: avrai un **ID alto** e un Kad non
   dietro firewall, cioè più fonti.

Gextto deve vedere la **cartella di arrivo** di aMule allo stesso percorso (lo fa
già se girano sulla stessa macchina): è da lì che importa i file completati.
Usa una cartella dedicata, perché aMule condivide tutto quello che contiene. Con
aMule in **Docker** monta la cartella dei download allo stesso percorso
dell'host, imposta lì `INCOMING_DIR` e `TEMP_DIR` e, per l'UPnP, usa
`network_mode: host`.

### La pagina eD2k

- **aMule** — versione, server, **ID alto/basso**, stato di Kad, velocità, coda
  di upload, byte scambiati; i pulsanti **Connetti**, **Disconnetti**,
  **Avvia/Ferma Kad**, **Lascia il server**. Se aMule non risponde, la pagina
  dice cosa controllare.
- **Server eD2k** — la lista dei server, con **Connetti**; da qui scarichi anche
  un nuovo `server.met` o un `nodes.dat`.
- **Cerca su eD2k e Kad** — scegli la rete e cerca; ogni risultato ha
  **Scarica**, e **già noto** segnala un file che aMule ha già. Il filtro sopra i
  risultati (parole, fonti minime, dimensione minima, nascondi già noti) non
  rifà la ricerca.
- **Incolla link eD2k** — uno o più link `ed2k://`, poi **Scarica con aMule**.
- **Log di aMule**, **Cartelle di aMule** (con **Crea le cartelle** se mancano) e
  **File condivisi** (byte caricati, richieste, client in coda, link da copiare;
  **Ricarica file condivisi** dopo modifiche a mano).

### Cosa succede a un download eD2k

I download eD2k compaiono in **Scarico** con il badge **eD2k**, con pausa,
riprendi e 🗑️ (su un download in corso lo annulla; su uno completato lo toglie
solo dall'elenco di aMule). **ⓘ** apre il dettaglio, con fonti, hash, link e
**priorità in aMule**.

Quando aMule completa un video, Gextto aspetta che il file sia stabile nella
cartella di arrivo, poi lo tratta come un torrent completato: stesso controllo
di qualità, rinomina, libreria, MediaInfo, notifica. Con *Sposta gli
episodi/pack in archivio* attiva il file viene spostato (e aMule smette di
condividerlo); spenta, entra con hardlink o copia e resta condiviso. Un file che
non è un video resta nella cartella di arrivo. Con seed infinito il file viene
copiato e resta condiviso, con il badge «seed ∞».

### eD2k come ripiego

Con **Usa eD2k quando i torrent non trovano nulla** attivo, la ricerca dei
mancanti chiede a eD2k/Kad quando torrent e archivio non hanno risultati: una
sola interrogazione per stagione, che dura quanto **Durata della ricerca eD2k**.
I risultati compaiono con la sorgente **eD2k/Kad** e **Scarica** li passa ad
aMule.

La guida completa — installazione, Docker, problemi comuni — è in
[amule.it.md](amule.it.md).

---

# Parte III — Configurare

La pagina **Configurazione** è divisa in quattro aree: **Cosa cercare**
(Generale, Sorgenti, eD2k e Kad, Qualità e upgrade), **Come scaricare** (Motore
torrent, Velocità e rete, Code e prestazioni, Seed e completamento), **Dove
salvare** (Libreria e rinomina, Archivio e spazio) e **Sistema** (Manutenzione
automatica, Notifiche, Accesso e servizi, Diagnostica e aggiornamenti).

Come si usa:

- le sezioni sono nella colonna a sinistra (dal telefono, nella tendina
  *Sezione*); la casella di ricerca trova un'impostazione per nome o per chiave
  tecnica;
- ogni riga mostra nome, descrizione, unità e valori speciali (per esempio «0 =
  mai»); le opzioni che dipendono da un'altra **compaiono solo quando servono**;
- un valore cambiato e non salvato è segnato **non salvata** e compare la barra
  *Modifiche non salvate* con **Salva tutto** e **Annulla modifiche**; uscire
  dalla pagina con modifiche in sospeso chiede conferma;
- **↺ Predefinito** rimette il valore predefinito; **Solo modificate** mostra
  solo quello che hai cambiato;
- le impostazioni che il motore torrent attivo non usa sono raccolte in **Non
  usate dal motore attivo**; quelle rare in **Per esperti**.

Questa parte spiega le scelte che contano. Ogni impostazione, una per una, è
nell'[Appendice A](#appendice-a-riferimento--configurazione).

## 13. Sorgenti: feed e indexer

Gextto trova le release in tre modi: **feed RSS** (letti a ogni ciclo, riempiono
l'archivio locale), **indexer Torznab** (interrogati per i titoli che segui) e
**motori web**. Tutto si imposta in *Configurazione → Sorgenti*.

### Aggiungere Jackett o Prowlarr

1. In Jackett (o Prowlarr) abilita almeno un indexer e copia la **API key**.
2. In Gextto premi **+ Jackett** (o aggiungi una riga alla lista *Indexer
   Torznab* e scegli il tipo).
3. Inserisci **solo l'indirizzo base**, per esempio `http://jackett:9117` o
   `http://prowlarr:9696`, e la API key. Non aggiungere il percorso Torznab:
   Gextto lo costruisce da sé.
4. Salva e premi **Verifica**.
5. Controlla *Salute → Stato API*.

Gextto legge l'elenco degli indexer dal manager e li interroga **uno per uno**,
così ognuno ha il suo timeout e la sua salute e il più lento non blocca gli
altri. Se l'elenco non è leggibile, torna alla ricerca aggregata. Nei risultati
la sorgente compare come `jackett:NomeTracker`.

Un indexer **spento** non viene interrogato, e i collegamenti `.torrent` delle
sue release già raccolte non vengono più aperti: le release restano nel catalogo
dell'archivio — nessun dato viene cancellato — ma Gextto non prova a scaricarle
e non riporta più l'errore del servizio spento. Riaccendendo l'indexer tornano
utilizzabili.

### Aggiungere indexarr

Uguale, con tipo **indexarr** e indirizzo base `http://indexarr:8788`. indexarr
si interroga in una volta sola: unisce da sé i doppioni delle sue fonti e dice
da quale sito arriva ogni risultato. Le sue fonti eD2k restituiscono link
`ed2k://`, che Gextto passa ad aMule invece che al motore torrent. Quando un
download si completa, Gextto lo comunica a indexarr, che nella sua pagina
Download segna la riga come **scaricato**. Non c'è nulla da configurare.

### FlareSolverr

I siti protetti da Cloudflare richiedono un **FlareSolverr** funzionante:
inserisci l'URL in *Configurazione → Sorgenti* e premi il pulsante di test. Con
un proxy configurato, FlareSolverr non viene usato come ripiego, perché
scaricherebbe fuori dal tunnel.

### Raggiungibile non vuol dire funzionante

Un indexer può rispondere ma rifiutare la richiesta (API key sbagliata, nessun
indexer abilitato in Jackett). Per questo *Salute* distingue:

| Stato | Significato | Cosa fare |
|---|---|---|
| **OK** | risponde e accetta le richieste | nulla |
| **Errore API** | risponde, ma rifiuta API key o configurazione | ricopia la API key, controlla gli indexer abilitati e l'URL base. Riavviare Gextto non serve |
| **Non raggiungibile** | nessuna risposta entro il timeout | controlla DNS, container, porta, firewall |

Dietro un manager la tabella mostra anche ogni suo indexer, così un tracker in
errore o in pausa si vede anche se il manager risponde.

### Backoff: quando una sorgente fallisce

Una sorgente che continua a fallire viene messa in **backoff**: per un po'
Gextto non la interroga, con pause sempre più lunghe. Se un indexer risponde
`429 Too Many Requests` con `Retry-After`, la pausa rispetta almeno il tempo
chiesto. Lo stato è in *Salute → Stato provider*, con livello, scadenza e
ultimo errore. Dopo aver corretto la causa premi **Azzera** su quella
sorgente; non usarlo per nascondere una API key sbagliata, o il backoff
ricomincerà.

Gextto avvisa da sé (log e notifica) quando una sorgente fallisce in tre delle
ultime ricerche, quando i link `.torrent` di una sorgente falliscono su due
release diverse nello stesso ciclo (di solito un login scaduto) e quando tutto
torna regolare.

### Copie della stessa release

Lo stesso torrent arriva spesso da più fonti. Gextto le unisce in una sola
release quando hanno lo stesso infohash, oppure — se una fonte non lo dà —
quando nome e dimensione coincidono. Nel dubbio non unisce: gruppo, risoluzione,
lingua, PROPER o episodio diversi restano separati. Della release usa la copia
migliore e tiene le altre come riserva: se un link `.torrent` non funziona,
prova subito la stessa release da un'altra fonte.

## 14. Qualità, punteggi e upgrade

### Come Gextto sceglie

1. **Filtri.** Una release che non rispetta i requisiti del titolo (qualità,
   lingua, sottotitoli obbligatori, esclusioni) o i controlli di sanità viene
   scartata.
2. **Punteggio.** Tra le release rimaste vince quella con il punteggio più alto.
   Il punteggio somma i pesi di risoluzione, sorgente, codec, audio, HDR/Dolby
   Vision, correzioni (PROPER, REPACK) e gruppo.
3. **Confronto con la libreria.** Se hai già il file, la nuova release lo
   sostituisce solo se è un **miglioramento reale** di qualità.

I controlli di sanità sono automatici e non si configurano: sottotitoli impressi
nel video (`HC`) e dimensioni assurde per la risoluzione vengono rifiutati. Gli
scarti si vedono nel log a livello `DEBUG`.

### Profili di qualità

Un profilo è un **modello con un nome**: qualità, audio minimo, lingua,
sottotitoli, esclusioni e «Permetti upgrade». Si gestiscono in *Configurazione
→ Qualità e upgrade → Profili di qualità*.

- **Qualità** si sceglie da un elenco (360p … 2160p, un minimo con «+»,
  l'intervallo `720p-1080p`, oppure *Qualsiasi*).
- **Lingua** e **Sottotitoli** propongono le scelte comuni (`ita`, `eng`,
  `ita,eng`, `multi`, `any`) ma accettano anche altri codici (`fre`,
  `ita,eng,fre`). Vuota vale la lingua predefinita di Gextto.
- Finché non salvi la lista, Gextto genera da sé un profilo per ogni
  combinazione già presente in libreria; il primo è **Default**. Due profili non
  possono avere lo stesso nome.

**I profili sono modelli, non regole.** Quando scegli un profilo per un titolo,
i suoi valori vengono **copiati** nel titolo. Cambiare il profilo dopo non
cambia i titoli già creati: per aggiornarne molti insieme usa [Modifica
selezionati](#modificare-più-titoli-insieme). La lista dei profili è anche
quella che vede Jellyseerr; ogni profilo conserva il suo numero anche se lo
rinomini, perché Jellyseerr ricorda il numero.

### Provare prima di cambiare

In cima a *Qualità e upgrade*:

- **Esempi con i pesi attuali** — i punteggi di alcune release tipiche;
- **Impatto sulla libreria → Verifica ora** — quanti episodi in libreria
  verrebbero sostituiti con le regole attuali. Usalo dopo aver cambiato pesi o
  soglie: un numero alto vuol dire che i prossimi cicli riscaricheranno molto;
- **Prova una release** — incolla un nome di release: vedi il punteggio voce per
  voce e se sostituirebbe la copia in libreria, con il motivo.

Nessuno dei tre scarica nulla.

### I pesi

Sotto ci sono i pesi, in schede chiuse: **Video** (4K, Full HD, HD, SD, codec,
sorgente, HDR/DV), **Audio** (Lossless, DTS/DD+, AC3, AAC/Opus, MP3, canali,
Atmos), **Correzioni release** e **Gruppi custom** (un bonus o una penalità per
un release group, scritto come il tag finale della release, per esempio `TBK`).
Più alto vuol dire più preferito. Dopo un cambio dei pesi i punteggi della
libreria si ricalcolano da soli.

La lingua non è un bonus: è un requisito del titolo, quindi puoi preferire
l'inglese quanto l'italiano. I sottotitoli preferiti danno un piccolo bonus. Una
release **freeleech** riceve un bonus a parte solo nella scelta tra candidati,
mai per un upgrade.

### Quando un file viene sostituito

| Impostazione | Effetto |
|---|---|
| **Differenza minima score per upgrade** | quanto deve essere migliore la nuova release. Conta solo la qualità: dimensione, sottotitoli e salute dello sciame non bastano |
| **Smetti di migliorare oltre questo punteggio** | sopra questo punteggio il file non viene più sostituito, salvo un REPACK o PROPER |
| **Sostituisci le versioni già archiviate** | attiva gli upgrade dei file già in libreria |

Prima di sostituire un file Gextto legge con `ffprobe` sia il nuovo sia quello in
libreria: se il nome prometteva più di quanto c'è (un «2160p» ingrandito da un
1080p, un «HDR» senza HDR) il download viene scartato e messo in blocklist
(nel log: `Upgrade not confirmed by MediaInfo`). Un file in libreria che non
dichiara sorgente o gruppo non viene considerato peggiore senza prove.

### Ritardo prima di scaricare

In *Configurazione → Generale → Ritardo prima di scaricare* puoi far aspettare
Gextto qualche minuto dopo aver trovato una release, per dare tempo a una
versione migliore di comparire. **Ignora il ritardo da questo punteggio** fa
partire subito le release già ottime. I titoli in attesa sono nel riquadro
**Coda titoli** di *Salute*.

## 15. Il motore torrent

Gextto gestisce sempre database, coda, punteggi, rinomina e libreria; il
**motore** fa solo il trasferimento. Si sceglie in *Configurazione → Motore
torrent*, e la pagina **Scarico** e le automazioni sono identiche con entrambi.
L'intestazione dell'interfaccia mostra il motore in uso con la sua versione.

### gx-torrent (predefinito)

Il motore incluso, in Go puro. Gextto lo avvia e lo sorveglia in un processo
separato; non c'è nulla da installare.

- **Resta acceso quando Gextto si riavvia** (per esempio per un aggiornamento):
  i trasferimenti non si interrompono. Viene riavviato solo se è cambiato il suo
  programma o le sue opzioni, mai durante uno spostamento di file. Se Gextto
  resta spento, si ferma da solo dopo 15 minuti.
- Ha una **pagina web** propria all'indirizzo di ascolto (predefinito
  `127.0.0.1:8890`, solo da questo computer): magnet, pausa, verifica, coda,
  filtro IP, log. Per aprirla in rete usa `0.0.0.0:8890` **e un token**.
- La **cache disco** si regola da sola in base alla memoria libera e al tipo di
  disco. Il **filtro IP** da URL si aggiorna una volta al giorno.
- Con un **proxy** configurato passano dal tunnel anche feed, indexer e
  download dei `.torrent`; i servizi in rete locale restano diretti.
- Supporta streaming, DLNA, download sequenziale, torrent v2 e i magnet con
  `so=`, `ws=` e `dht=`.
- In **Configurazione → Motore torrent → gx-torrent** ci sono tre opzioni native
  di qBittorrent: **ricontrolla i dati al completamento** (verifica i file una
  volta prima di seminarli), **modalità anonima** (nasconde la versione del
  client a peer e tracker pubblici, non l'IP) e **annuncia a tutti i tracker del
  tier** (interroga insieme tutti i tracker di un tier). Le prime due sono
  spente di default, la terza è attiva. Dopo averle cambiate, riavvia Gextto
  per applicarle al demone gestito.

Se il motore non riesce a restare in piedi (6 avvii anomali in 10 minuti),
Gextto passa da solo a qBittorrent-nox e, come ultima risorsa, a uno stato di
recupero con i trasferimenti fermi e l'interfaccia raggiungibile. Un
aggiornamento di gx-torrent che non parte viene prima riportato alla versione
precedente. Ricevi una notifica in ogni caso.

### qBittorrent-nox

Gextto può pilotare un **qBittorrent-nox** (dalla 4.6 alla 5.3) tramite la sua
Web API: imposta URL, utente, password, categoria e tag. Se i due programmi
vedono le cartelle con percorsi diversi (Docker, NAS, altra macchina) servono le
**mappature percorsi** `locale=remoto`: **Proponi** le legge da qBittorrent.
Se un percorso non è traducibile l'attivazione viene rifiutata, così un file non
può finire nella cartella sbagliata.

In **modalità gestita** Gextto scarica qBittorrent-nox, lo installa accanto a
sé, lo avvia e lo ferma col servizio, lo aggiorna e lo riavvia se cade. I passi
completi sono in [qbittorrent-nox.md](qbittorrent-nox.md).

### Cambiare motore

Un torrent appartiene a un solo motore alla volta: il passaggio è una
migrazione, non due client sugli stessi dati. Il riquadro del motore ha un
**test** di raggiungibilità e percorsi: usalo prima di passare. Lo stato del
motore attivo (coda, porta, DHT, cifratura, proxy, cache, I/O disco) è in
*Salute → Motore torrent*.

### Su una macchina piccola

Gextto gira bene su un mini PC o un NAS. A riposo non si nota; durante un
trasferimento il costo è del motore, che restituisce la memoria a download
finito. Se le risorse sono poche:

- attiva la **coda dinamica** e *Non contare i torrent fermi negli slot
  attivi* (*Configurazione → Code e prestazioni*);
- non impostare un limite di cache: il valore automatico è quasi sempre il
  migliore.

## 16. Velocità e rete

### La regola della velocità

In *Configurazione → Velocità e rete*, **Come si decide la velocità** offre tre
regole:

- **Auto** — Gextto misura la linea (il ping verso server pubblici) e alza o
  abbassa la velocità per non saturarla. Se conosci la velocità reale della tua
  linea, scrivila in *Auto — velocità massima linea*: Auto trova il punto giusto
  più in fretta.
- **Limiti fissi** — un tetto per il download e uno per l'upload.
- **Programmazione oraria** — una riga per giorno, con *Non attiva*, *Limite*
  (i tuoi valori) o *Auto*, più ora di inizio e fine. **Fuori dalle fasce
  orarie: Auto** decide cosa vale fuori dalle fasce.

I peer della rete di casa non sono limitati, a meno che tu non attivi *Applica i
limiti anche ai peer della rete locale*. Per un'eccezione temporanea usa il
pulsante **Banda** in Scarico (vedi [La banda](#la-banda)).

### Porte, VPN, cifratura

- **Porte**: apri sul router la porta di ascolto; UPnP e NAT-PMP possono farlo
  da soli. **Testa porte** controlla solo che la porta si apra su questo
  computer, non l'inoltro sul router.
- **Interfaccia VPN (killswitch)**: vincola il traffico torrent a una scheda di
  rete (`tun0`, `wg0`); se la VPN cade, il traffico si ferma invece di uscire
  in chiaro. Si applica dopo il riavvio.
- **Cifratura e filtro IP**: in *Velocità e rete → Cifratura e filtro IP*.

### RAM disk

Per risparmiare scritture su un disco lento puoi far lavorare i dati incompleti
in RAM (*Configurazione → Code e prestazioni → RAM disk*). Gextto elenca i
`tmpfs` disponibili e, se non ce n'è uno, crea `/dev/shm/gextto`. Il contenuto
si perde al riavvio della macchina e la cartella va ricreata.

## 17. Libreria, NAS e spazio

### Una libreria su NAS

1. Monta il NAS **prima** che Gextto parta (per esempio con un'unità systemd o
   `fstab`).
2. Dai lettura e scrittura all'utente del servizio.
3. Imposta uno **spazio libero minimo** prudente (*Archivio e spazio → Spazio
   su disco*): sotto quella soglia Gextto non avvia download nuovi.
4. Se hai copiato file esistenti, esegui **Scansiona archivio**.
5. Dopo il primo download controlla il percorso effettivo nello *Storico
   download*.

Se il NAS non è montato, **non** sostituire il percorso con una cartella locale
per farlo funzionare: i file finirebbero nel posto sbagliato. Correggi il mount
e lascia aspettare il download: gli spostamenti falliti vengono ritentati dopo
1, 2, 4, 8, 16 e 32 minuti. Un NAS smontato non cancella nulla: un mount point
vuoto viene trattato come volume assente e gli episodi restano registrati.

### Cestino

Il **cestino** (*Archivio e spazio → Cestino*) riceve i file sostituiti da un
upgrade, i pack a fine seed e i duplicati. Viene svuotato secondo la retention;
**Pulisci trash** in Manutenzione lo svuota subito. Senza cestino configurato
Gextto non cancella mai file sorgente da solo.

### Lingua e metadati

- **Lingua predefinita** (*Libreria e rinomina*): la lingua che serie e film
  ricevono se non ne indichi un'altra.
- **Metadati**: TMDB è la fonte predefinita di titoli, episodi e locandine;
  TVDB si usa quando c'è solo la sua chiave. Le chiavi si inseriscono in
  *Accesso e servizi → Servizi metadati*.
- **Rimuovi le tracce audio in altre lingue**: dopo l'importazione tiene solo
  l'audio nella lingua scelta, senza ricodificare, e controlla la copia prima
  di sostituire il file. Servono `ffprobe`, `ffmpeg` e `mkvmerge`.

## 18. Notifiche

Gextto può avvisarti su **Telegram**, **e-mail**, **webhook**, **Discord**,
**Slack**, **Gotify**, **ntfy**, **Pushover**, **Apprise** o con un tuo
**programma**. Si configura in *Configurazione → Notifiche*.

### Configurare un canale

1. Compila i campi del canale (un campo vuoto spegne il canale).
2. Nel gruppo **Eventi** scegli cosa ricevere.
3. Premi **Invia notifica di test**: usa tutti i canali configurati e mostra
   subito un errore.
4. Prova un completamento vero solo con un download non importante.

### Quali eventi

| Gruppo | Cosa comprende |
|---|---|
| **Notifiche dei download** | un download parte, viene scartato (un pack che non contiene nulla di utile), si completa o riempie un episodio mancante |
| **Notifiche degli errori** | un download fallisce, un torrent o un fumetto va in errore, il servizio si ferma per errore |
| **Notifiche di salute e motore** | problemi di salute, del ciclo, degli indexer, dei login, del motore torrent, e il ritorno alla normalità |
| **Notifiche di manutenzione** | riavvii, backup completati, pulizia delle tracce audio |

Le notifiche di completamento riportano dimensione, durata e velocità media. Per
un season pack distinguono gli episodi **nuovi** da quelli che hanno
**sostituito** una versione peggiore. **Anti-spam — intervallo minimo** evita di
ricevere più volte lo stesso evento; **Lingua dei messaggi** sceglie la lingua,
indipendente da quella dell'interfaccia.

Gextto avvisa anche di riavvii (e di arresti anomali, al riavvio successivo:
un processo morto non può notificare la propria caduta), errori fatali,
problemi di gx-torrent e del passaggio a un altro motore.

### Webhook: quale URL e quale formato

Il canale **Webhook** manda gli eventi a un servizio esterno. Scegli il
**Formato**, incolla l'**URL** e metti la credenziale in **Webhook token**, non
nell'URL.

| Servizio | Webhook URL | Formato | Credenziale |
|---|---|---|---|
| Endpoint tuo | il tuo URL | **Gextto (JSON firmato)** | «Webhook secret» firma il corpo (HMAC SHA-256, header `x-gextto-signature`) |
| Discord | URL del webhook del canale | **Discord** | nessuna |
| Slack | URL del webhook in ingresso | **Slack** | nessuna |
| ntfy | `https://ntfy.sh/<argomento>` | **ntfy** | «Webhook token» = token Bearer, se l'argomento è protetto |
| Gotify | `https://gotify.esempio/message` | **Gotify** | «Webhook token» = application token |
| Pushover | `https://api.pushover.net/1/messages.json` | **Pushover** | «Webhook token» = application token, «Webhook user» = user key |

Ogni servizio ha anche **campi dedicati** (Discord, Slack, Gotify, ntfy,
Pushover, Apprise, Programma), così più canali possono restare accesi insieme.
Ogni canale riceve lo stesso evento, e uno lento non trattiene gli altri. Il
**Programma** riceve i dati nelle variabili `GEXTTO_*` e gira senza shell.

## 19. Integrazioni con altri programmi

Nessuna integrazione è necessaria per scaricare e archiviare. Attivale una alla
volta e usa sempre il pulsante di test. Campi e azioni sono
nell'[Appendice B](#appendice-b-riferimento--integrazioni).

### Jellyseerr (al posto di Sonarr e Radarr)

Gextto risponde alle richieste di Jellyseerr come farebbero Sonarr e Radarr:

1. in Jellyseerr aggiungi un server Sonarr e uno Radarr con l'indirizzo di
   Gextto;
2. come chiave usa la **Chiave API** di Gextto (*Configurazione → Accesso e
   servizi*);
3. scegli profilo di qualità e cartella dagli elenchi che Gextto espone.

Una serie richiesta viene seguita solo per le stagioni chieste; se la segui già,
le stagioni si aggiungono. Un film già presente non viene duplicato. La
«ricerca immediata» di Jellyseerr non è gestita: il titolo viene cercato dal
ciclo normale. Non è una compatibilità completa con Sonarr e Radarr: le
richieste non gestite rispondono 404.

### Jellyfin e Plex

Inserisci l'URL del server (raggiungibile dal computer di Gextto) e un token:
Gextto chiede un aggiornamento della libreria quando arriva un file nuovo.

### Simkl, calendario, hook

- **Simkl**: completa il flusso PIN, verifica l'account e poi importa la
  watchlist.
- **Calendario iCal**: le prossime uscite in un calendario che puoi aggiungere
  al telefono.
- **Hook eventi**: un programma lanciato su eventi come `download_started`,
  `torrent_completed`, `season_pack_completed`, `torrent_error`. I campi
  accettano segnaposto (`{title}`, `{path}`, `{series}`, `{episode}`…) e
  ricevono le variabili `GEXTTO_*`. Gira senza shell (niente pipe né `&&`: usa
  uno script), con un timeout predefinito di 60 secondi e al massimo quattro
  hook insieme. Prova prima con un programma innocuo che scrive un log.
- **Metriche ed eventi** di gx-torrent: `GET /api/v1/metrics` per Prometheus e
  `GET /api/v1/events` per Home Assistant (vedi [API.md](API.md)).

## 20. Accesso e sicurezza

L'interfaccia è un **pannello di amministrazione**: chi la apre può cambiare
tutto. Per impostazione predefinita è aperta, perché Gextto è pensato per una
rete di casa fidata.

- **Solo in casa**: lasciala così, o limita la porta 5000 con il firewall.
- **Da fuori**: in *Configurazione → Accesso e servizi* attiva **Richiedi
  l'accesso (login)** e imposta una password. Con **Nessun login dalla rete
  locale** (predefinito) in casa non serve. Metti Gextto dietro un reverse proxy
  HTTPS o una VPN.
- **Script, TUI, calendario**: usano la **Chiave API** (header `X-Api-Key` o
  `?apikey=`). **Genera** crea un valore casuale, **Copia** lo copia.
- **gx-torrent in rete**: se apri la sua pagina in LAN, imposta il suo **token**.

Indipendentemente dal login, Gextto rifiuta le modifiche che il browser dichiara
provenire da un altro sito: una pagina malevola non può riconfigurarlo
attraverso il tuo browser. Il modello completo è in [SECURITY.md](SECURITY.md);
le chiavi in [Chiavi di accesso](api-keys.it.md).

---

# Parte IV — Tenere tutto in ordine

## 21. Controllare che tutto funzioni

### La Dashboard

È la pagina da guardare ogni giorno. Dall'alto:

- **In sessione** — torrent in scarico, fermi e in seed, la banda degli ultimi
  30 giorni e il **grafico del traffico** degli ultimi 30 minuti (download e
  upload, con il picco e il limite in vigore; con eD2k, aMule a linee
  tratteggiate);
- **Da controllare** — download fermi (con motivo e prossimo tentativo),
  spostamenti in libreria in sospeso, avvisi ed errori. Un avviso resta finché il
  download è in sessione e sparisce da solo quando esce senza nulla in sospeso;
  **Pulisci** li segna come letti. Se è tutto a posto dice «Niente da
  controllare»;
- **Ricerca manuale** e **Ricerca automatica** — la prossima esecuzione,
  l'esito dell'ultimo ciclo e i pulsanti per avviarne uno (tutto, o solo Serie,
  Film, Fumetti);
- **Operazioni in background** — rinomine, scansioni, MediaInfo, controllo
  integrità, ricerche dei mancanti: con la percentuale e **Annulla**;
- **Prossime uscite**, **ultimi download**, **attività recente** e **ultimi
  trovati**.

Il badge **DRY-RUN** o **ATTIVO** in alto dice se Gextto scarica davvero.

### Salute

*Salute* (🩺) risponde a «funziona tutto?»:

- stato di processo, sistema e servizio, permessi delle cartelle, dischi;
- **Stato API** e **Stato provider**: ogni indexer con il suo stato e i backoff
  (vedi [Sorgenti](#raggiungibile-non-vuol-dire-funzionante));
- **Ricerche**: gli ultimi cicli, con durata, release esaminate, download
  avviati, errori e sorgenti che non hanno risposto;
- **Coda titoli**: i titoli trattenuti dal ritardo, e da quanto;
- **Import falliti**: quanti al giorno, negli ultimi 7 giorni (un numero che
  cresce indica un disco o un NAS che rifiuta gli spostamenti);
- **Motore torrent** e, con eD2k attivo, **eD2k e Kad (aMule)**.

### Il log

**Log** mostra il log in diretta, con filtro, numero di righe e segui/pausa. Le
righe sono in inglese, in frasi leggibili. Ogni ciclo stampa un **SOURCE
REPORT** con l'esito di ogni sorgente e le decisioni prese. Il file è in
`<dati>/gextto.log` (ruotato a 5 MB); quello del motore è in
`<dati>/gx-torrent/gx-torrent.log`, consultabile anche da *Manutenzione → Log
gx-torrent*.

Una riga identica ripetuta a raffica — per esempio un file che il disco rifiuta
centinaia di volte in un secondo — non riempie più il log: si vede la prima
copia, le altre vengono contate e ogni mezz'ora un riepilogo dice quante sono
state; passato un minuto la riga può ricomparire, così un problema che continua
non resta nascosto. Se il motore esce con un errore mentre Gextto lo sta
sostituendo (un aggiornamento o un riavvio), Gextto scrive una riga che dice
dove sta il dettaglio: il file `<dati>/gx-torrent/gx-torrent.crash.log`.

### La storia di un download

Ogni riga del log che riguarda un download finisce con un **ID di
acquisizione** (`acq: 7f3a2c`), lo stesso dalla scelta della release all'arrivo
in libreria. Scrivilo nel filtro del log per vedere solo quel download. Le
stesse righe sono conservate per 180 giorni come **storia**: scheda *Storia* nel
dettaglio del torrent, 📜 su ogni episodio, *Storia dei download* nella scheda
della serie. Restano leggibili anche dopo la rotazione del log e la rimozione
del torrent.

### Grafici e attività

**Grafici** mostra CPU, RAM, rete, disco e consumo giornaliero; **Attività** gli
eventi recenti.

## 22. Curare la libreria

Tutte queste azioni sono in **Manutenzione** (elenco completo
nell'[Appendice C](#appendice-c-riferimento--manutenzione)). Le operazioni lunghe
girano in background: puoi continuare a usare Gextto e annullarle.

> [!TIP]
> Usa sempre prima l'**anteprima**, dove c'è. Le operazioni che spostano file
> mandano nel cestino, non cancellano.

### Rinominare

- **Anteprima rinomina** su una serie, o **Rinomina tutto** per la libreria
  intera: prima vedi *Vecchio → Nuovo*, poi applichi.
- **Rinomina contenuto cartella**: scegli una cartella qualsiasi; Gextto
  riconosce serie e film dai nomi, li confronta con TMDB/TVDB e propone i nomi
  nuovi. Puoi scegliere un risultato alternativo, accettare o rifiutare ogni
  proposta e poi **Applica selezionate**. I conflitti restano invariati;
  sottotitoli, immagini e NFO seguono il nuovo nome.
- **Ripristina sorgente nei nomi**: rimette il token `[WEB-DL]`, `[BluRay]`…
  nei nomi che l'hanno perso, ricavandolo dalla release originale. Se non è
  noto, il file non cambia; se nella cartella esiste già un file con il nuovo
  nome, l'episodio viene saltato e conta come errore, senza sovrascrivere nulla.

### Duplicati

*Anteprima duplicati* e *Pulisci duplicati* trovano i video chiaramente
peggiori rimasti accanto alla versione migliore nella stessa cartella (il
vecchio 480p accanto al nuovo 1080p) e li spostano nel cestino. A pari
risoluzione tiene la versione nella lingua preferita. I file dei torrent in
sessione sono protetti.

### Qualità reale e integrità dei file

- **Aggiorna MediaInfo** legge con `ffprobe` risoluzione, codec, HDR, audio e
  lingue dei file in libreria. Questi dati servono a confrontare gli upgrade
  con il file com'è davvero, non con il suo nome. I file nuovi vengono letti a
  download finito; per gli altri c'è anche il backfill automatico.
- **Controllo integrità file** (*Configurazione → Manutenzione automatica*)
  decodifica i video un po' alla volta, in background, e trova i file troncati
  o danneggiati. Il pannello **Stato del controllo** elenca i file danneggiati
  o illeggibili con il motivo; **Riscarica** accanto a una puntata la toglie
  dalla libreria, mette in blocklist quella release e ne cerca una diversa. Con
  *riscarica i file danneggiati* attivo lo fa da solo. Servono `ffprobe` e
  `ffmpeg`.

### Manutenzione notturna

In *Configurazione → Manutenzione automatica → Manutenzione notturna* scegli
una finestra (predefinita 01:00–07:00) per i lavori pesanti: ottimizzazione e
controllo dei database, rapporto sui file spariti dal disco, pulizia
dell'audio dei file già in libreria (se la attivi), controllo integrità e
MediaInfo. Ogni lavoro parte solo quando Gextto è inattivo, uno alla volta. Il
pannello mostra cosa sta girando o perché aspetta; **Esegui ora** fa partire
subito un passaggio.

### Pulizie del database

- **Housekeeping** tiene ordinato il database (statistiche dei cicli, errori
  vecchi, backoff scaduti, `VACUUM` quando serve). **Non** tocca la libreria, i
  download né l'Archivio.
- **Pulizia archivio release** (*Manutenzione automatica*) toglie dal catalogo
  le release vecchie, se la attivi. Non tocca file né download.
- **Manutenzione SQL dell'archivio** (in *Manutenzione*) esegue operazioni
  pronte sul catalogo: release più vecchie di 6 mesi o di 1 anno, in una lingua
  o senza, titoli vuoti, duplicati. Prima mostra l'anteprima con il numero di
  righe, poi **Elimina definitivamente**. Fai un backup prima di una pulizia
  grande.

### Cosa non confondere

| Insieme | Cosa contiene | Cosa lo pulisce |
|---|---|---|
| **Sessione torrent** | i torrent nel motore | *Rimuovi*, *Pulisci completati*, fine seed |
| **Libreria** | i tuoi video | *Pulisci duplicati*, upgrade (verso il cestino) |
| **Cestino** | i file tolti dalla libreria | retention, *Pulisci trash* (definitivo) |
| **Archivio** | il catalogo delle release viste | *Pulizia archivio release*, Manutenzione SQL |
| **Storico download** | l'elenco dei download conclusi | retention dello storico |

## 23. Backup e ripristino

### Cosa contiene un backup

Uno ZIP `gextto-backup-<data>_<ora>.zip` in `<dati>/backups` con:

- i quattro database: serie, episodi, film e download (`gextto_series.db`),
  impostazioni e indexer (`gextto_config.db`), archivio dei feed
  (`gextto_archive.db`), fumetti (`gextto_comics.db`);
- gli altri file di configurazione della cartella dati, compreso lo stato di
  gx-torrent;
- le cartelle esterne che elenchi in *File e cartelle da includere*.

**Non** contiene i video, la sessione torrent, il filtro IP, le cache, i log, il
cestino né `/etc/gextto/gextto.env`: tienine una copia tu.

### Programmare i backup

In *Manutenzione → Backup* scegli la retention e quando farli (a mano, ogni N
ore o a un'ora fissa). Puoi mandarli anche su **FTP** (con **Test FTP**, che
dice quale passo fallisce), in una cartella sincronizzata o su Telegram. Il
pulsante della Dashboard fa un backup immediato.

### Ripristinare

1. Installa Gextto con le stesse opzioni di prima, oppure ferma il servizio:
   `sudo systemctl stop gextto`.
2. Metti da parte la cartella dati attuale:
   `sudo mv /var/lib/gextto /var/lib/gextto.old`.
3. Estrai il backup e assegnalo all'utente del servizio:

   ```bash
   sudo install -d /var/lib/gextto
   sudo unzip /percorso/gextto-backup-<data>.zip -d /var/lib/gextto
   sudo chown -R <utente>:<gruppo> /var/lib/gextto
   ```

4. Rimetti `/etc/gextto/gextto.env` se lo avevi, poi
   `sudo systemctl start gextto`.
5. Apri **Salute**. Con gx-torrent i download in corso vengono riaggiunti dai
   magnet salvati, e i dati già scaricati vengono ricontrollati e riusati.

Un ripristino non sposta i video. Se nel frattempo la libreria ha cambiato
posto, correggi i percorsi o esegui *Scansiona archivio* prima di riattivare le
ricerche.

## 24. Aggiornare Gextto

### Dall'interfaccia

*Manutenzione → Aggiornamenti* mostra la versione installata, il canale,
l'ultima versione pubblicata e le novità. Gextto controlla da solo ogni 6 ore
(*Intervallo controllo aggiornamenti*); **Verifica aggiornamenti** controlla
subito. Quando c'è una versione nuova:

1. premi **Aggiorna**: Gextto fa un backup dei database e chiede l'aggiornamento
   al servizio di sistema `gextto-update`;
2. aspetta: Gextto finisce le copie verso la libreria, si riavvia e il pannello
   lo segnala da solo;
3. al primo accesso un popup **Gextto aggiornato** mostra le novità.

Se la nuova versione non parte, torna da sola quella precedente. In fondo alla
pagina trovi la versione esatta: il terzo numero è la build (`1.2.2065`), con il
commit da cui è compilata.

### Dalla riga di comando

```bash
sudo /opt/gextto/gexttod --update
```

Scarica l'archivio, ne verifica il checksum (obbligatorio), sostituisce
programma e motore e riavvia il servizio, tenendo le versioni precedenti
(`gexttod.prev`, `gx-torrent.prev`) e rimettendole se le nuove non partono. Dati
e configurazione non vengono toccati, e nemmeno l'unità systemd: le tue
personalizzazioni restano. Opzioni: `--channel stable`, `--release <tag>`,
`--archive <file>` (senza rete), `--force`, `--no-restart`.

Su un'installazione da sorgenti: `git pull` e `make build`.

### Altri comandi

| Comando | Effetto |
|---|---|
| `gexttod --version` | versione, build e canale installati |
| `gexttod --help` | riepilogo d'uso |
| `gexttod --config <file>` | usa un file di configurazione diverso |
| `gexttod --dry-run` | avvia in modalità prova fino al riavvio |
| `gexttod tui` | apre l'interfaccia da terminale |

## 25. Usare Gextto dal terminale (TUI)

La TUI è un'interfaccia a schede per il terminale, comoda via SSH anche dal
telefono. Usa solo le API del servizio, quindi funziona anche da un altro
computer.

```bash
/opt/gextto/gexttod tui
/opt/gextto/gexttod tui --url http://host:5000 --lang it
GEXTTO_URL=http://host:5000 GEXTTO_API_KEY=<chiave> /opt/gextto/gexttod tui
```

Le schede sono Stato, Torrent, Log, Salute, Archivio, Mancanti, Blocklist,
Libreria e Manutenzione.

| Tasto | Azione |
|---|---|
| `1`–`9`, `Tab` | cambia scheda |
| `r` | aggiorna |
| `?` | aiuto |
| `/` | filtra (nel Log) |
| `f` | segui o ferma il log |
| `Invio` | apre il dettaglio |
| `p` | pausa o riprendi |
| `y` | copia il link |
| `d` | toglie un download eD2k da aMule |

Su un terminale stretto (sotto le 60 colonne) l'interfaccia si compatta. La
guida completa è in [tui.md](tui.md).

---

# Parte V — Quando qualcosa non va

## 26. Risoluzione dei problemi

### Un episodio non è arrivato: dove guardare

Parti dalla scheda della serie e premi **📜** sulla puntata:

| Cosa vedi | Cosa vuol dire | Cosa fare |
|---|---|---|
| *Nessun evento* | Gextto non l'ha mai scaricata | 🔍 sulla puntata e **Perché non questa?**: la release non c'è nelle sorgenti, o è stata scartata da filtri, qualità, lingua o ritardo? Controlla anche *Salute* |
| *📥 Downloading* e poi niente | il download è in corso o fermo | apri il torrent in *Scarico*: «⏸️ is stuck» vuol dire pochi utenti che condividono; «❌ Gave up» che è stato abbandonato e verrà cercata un'altra versione |
| *🎉 downloaded* ma nessun 📁 | scaricato ma non arrivato in libreria | cerca «still cannot be moved» o «Could not move»: di solito NAS non raggiungibile, permessi o spazio. I tentativi riprendono da soli |
| *discarded as inferior* | in libreria c'era già una versione almeno altrettanto buona | nulla: è il comportamento previsto |
| *📁 added* / *♻️ updated* | è in libreria | se il nome non è quello atteso, vedi sotto *Un file non viene rinominato* |

Per tutti i dettagli apri *Mostra nel log* dal torrent: il log si filtra su quel
solo download.

### Non scarica nulla

1. Il badge in alto dice **DRY-RUN**? Sei in modalità prova (vedi
   [capitolo 4](#4-il-primo-download-in-prova-e-poi-davvero)).
2. Il *Ciclo automatico* è acceso (*Configurazione → Generale*)?
3. La serie o il film sono attivi, e le stagioni sono seguite?
4. Le sorgenti sono **OK** in *Salute*?
5. Una ricerca manuale trova release? Se sì, apri **Perché non questa?**: di
   solito è un filtro di qualità o lingua troppo stretto.
6. C'è abbastanza spazio libero rispetto al minimo impostato?

### Una release è visibile ma non viene scelta

Apri **Perché non questa?** e guarda i controlli **Bloccanti**: titolo seguito,
filtri, sanità, qualità/lingua, confronto con la libreria. Se passano tutti,
guarda quelli **Informativi**: un altro candidato migliore, il ritardo, lo
spazio libero o i buchi da riempire possono cambiare la scelta.

### Una sorgente non risponde o dà «Errore API»

- Controlla *Salute → Stato API* e premi **Verifica** sulla sorgente.
- Jackett: URL **base** (`http://host:9117`), API key ricopiata, almeno un
  indexer abilitato. Non aggiungere il percorso
  `/api/v2.0/indexers/all/results/torznab/api`.
- indexarr: URL base (`http://host:8788`) e tipo **indexarr**.
- Siti con Cloudflare: serve FlareSolverr funzionante.
- Dopo aver corretto, **Azzera** il backoff della sorgente.

### Un torrent è fermo (stalled)

È in pausa di proposito, fuori dalla coda attiva: verrà ripreso al prossimo
tentativo. Puoi anche **Riprendi** o **Riavvia** a mano. Guarda lo sciame nella
riga: con 0 seeder probabilmente non ripartirà, e Gextto cercherà un'altra
versione. I tempi sono in [Torrent fermi](#torrent-fermi-stalled).

### Il download è completo ma non è in libreria

Guarda il log per spostamento, permessi e spazio. Gli errori temporanei (NAS
non raggiungibile, disco pieno, timeout) vengono ritentati dopo 1, 2, 4, 8, 16 e
32 minuti; solo dopo l'ultimo il torrent va in errore. Uno spostamento
interrotto da un riavvio riprende da solo circa un minuto dopo. Non cancellare
i file sorgente finché il percorso in libreria non compare nello storico.

### Un file è sul NAS ma Gextto lo considera mancante

Controlla nome del file (deve contenere stagione ed episodio, per esempio
`S01E02`), percorso della serie e permessi, poi premi **Scansiona archivio**.

### Una puntata in libreria non si riscarica

Usa **↻ Riscarica** sulla puntata: sposta il file nel cestino e la rimette in
cerca anche se la stagione è coperta da un pack o hai già episodi successivi.
Uno speciale `SxxE00` non copre la stagione.

### Un file non viene rinominato

Controlla le impostazioni di *Libreria e rinomina* e la chiave TMDB (o TVDB e il
suo PIN): senza metadati il titolo dell'episodio diventa «Episodio N». Conviene
avere `mediainfo` installato.

### Una cartella osservata non importa il file

Il file deve avere estensione `.torrent` o `.magnet`. Gextto aspetta che
dimensione e data restino stabili, poi ritenta da solo gli errori. Controlla le
righe del log sulla cartella osservata.

### L'interfaccia non si apre

- Controlla il servizio: `systemctl status gextto` (o `systemctl --user status
  gextto`).
- Se il log dice che la cartella dati è già in uso o la porta è occupata, c'è
  un'altra istanza: fermala e riavvia.
- Dopo un riavvio Gextto può impiegare circa un minuto per essere pronto.

### Il backup FTP fallisce

Usa **Test FTP**: dice quale passo fallisce (connessione, login, percorso,
upload, rimozione) e lo scrive nel log.

### Dove sono i log

- Gextto: `<dati>/gextto.log` (ruotato a 5 MB × 4), oppure la pagina **Log**.
- gx-torrent: `<dati>/gx-torrent/gx-torrent.log`, oppure *Manutenzione → Log
  gx-torrent*.
- aMule: *eD2k → Log di aMule*.
- Aggiornamenti: `/var/log/gextto-update.log`.

## 27. Glossario

| Termine | Significato |
|---|---|
| **Alias** | un altro nome con cui una serie compare nelle release |
| **Archivio** | il catalogo locale delle release viste nei feed |
| **Backoff** | la pausa crescente delle richieste a una sorgente che fallisce |
| **Blocklist** | le release che non verranno più scaricate |
| **Cestino** | dove vanno i file tolti dalla libreria, prima della cancellazione |
| **Ciclo** | una passata automatica di ricerca e scelta |
| **Dry-run** | la modalità prova: cerca e decide, ma non scarica |
| **Gap** | un episodio mancante in mezzo a quelli che hai |
| **Gap forzato** | la richiesta esplicita di riscaricare una puntata (↻ Riscarica) |
| **Hardlink** | un secondo nome per gli stessi dati, senza occupare spazio due volte |
| **Indexer** | un servizio che cerca release su uno o più siti (Jackett, Prowlarr, indexarr) |
| **Libreria** | le cartelle finali con serie e film |
| **Placeholder** | la riga temporanea di un download non ancora completato |
| **Profilo di qualità** | un modello di qualità, lingua, sottotitoli ed esclusioni |
| **Release** | un risultato trovato da un feed, un indexer o un motore web |
| **Season pack** | un torrent con un'intera stagione |
| **Seed** | la condivisione di un torrent dopo il completamento |
| **Sessione torrent** | i torrent ancora gestiti dal motore |
| **Stalled** | un torrent che non riceve dati da troppo tempo |
| **Upgrade** | la sostituzione di un file con una versione migliore |

---

## Appendice A. Riferimento — Configurazione

La pagina è divisa in quattro aree, ognuna con le sue sezioni: **Cosa cercare**
(Generale, Sorgenti, Qualità e upgrade), **Come scaricare** (Motore torrent,
Velocità e rete, Code e prestazioni, Seed e completamento), **Dove salvare**
(Libreria e rinomina, Archivio e spazio) e **Sistema** (Manutenzione automatica,
Notifiche, Accesso e servizi, Diagnostica e aggiornamenti). Su computer le sezioni
sono nella colonna a sinistra, con il numero di impostazioni; su telefono si
scelgono dalla tendina *Sezione*. L'indirizzo della pagina segue la sezione
aperta, quindi si può salvare nei preferiti. I link alle vecchie schede
(*Acquisizione*, *Avanzate*, *Traduzioni*) aprono la sezione che ne ha preso il
contenuto (*Traduzioni* apre Diagnostica e aggiornamenti). Entrando in Configurazione la barra dei menu principale si nasconde; il
pulsante **Menu** in alto a sinistra la fa ricomparire sopra il contenuto (Esc
per richiuderla).

Quando una sezione raccoglie più gruppi di impostazioni (per esempio **Ciclo di
ricerca** ed **Episodi mancanti** in *Manutenzione automatica*), il titolo del
gruppo resta **agganciato in alto** mentre scorri, con una barretta colorata
d'accento.

Sotto il titolo della sezione c'è l'**indice dei gruppi** (chip cliccabili) per
saltare direttamente a un gruppo. La casella **Solo modificate**, accanto a
*Mostra chiavi tecniche*, nasconde tutte le impostazioni rimaste al valore
predefinito: comoda per rivedere solo ciò che hai cambiato. Le righe si
evidenziano al passaggio del mouse.

Come si legge una riga:

- **Nome e descrizione** a sinistra; il controllo al centro con l'**unità**
  (secondi, minuti, GB, KiB/s…) e, sotto, il significato dei **valori
  speciali** (per esempio «0 = mai»).
- Le opzioni sì/no sono **interruttori**; gli orari usano il selettore
  dell'ora. In **Velocità e rete**, **Come si decide la velocità** sceglie la
  regola: *Auto* (Gextto misura la linea e si adatta), *Limiti fissi* (i due
  limiti globali) o *Programmazione oraria*. Con la programmazione compaiono la
  tabella con una riga per giorno, dove per ogni giorno c'è **una scelta** tra
  *Non attiva*, *Limite* (i tuoi valori, unici a usare i campi di velocità) e
  *Auto*, più inizio e fine, e **Fuori dalle fasce orarie: Auto**, che decide se
  fuori dalle fasce vale Auto o i limiti fissi. Le
  vecchie impostazioni a fascia unica non compaiono più nella pagina: finché la
  tabella non viene salvata ne mostra i valori e restano in uso.
- Le opzioni che dipendono da un interruttore o da una scelta (per esempio i
  campi della programmazione velocità o del RAM disk) **compaiono solo quando
  servono**, anche prima di salvare.
- Un valore diverso dal predefinito mostra **modificato**, il valore
  *Predefinito* e il pulsante **↺ Predefinito**, che rimette il valore
  predefinito nel campo (va poi salvato).
- La chiave tecnica (es. `refresh_interval`) si vede con **Mostra chiavi
  tecniche**; la ricerca la trova comunque.
- Le impostazioni che il motore torrent attivo non usa sono raccolte, chiuse,
  in **Non usate dal motore attivo** (o nel pannello del motore non attivo):
  restano salvate e tornano attive cambiando motore. Con qBittorrent-nox ci
  finiscono anche le opzioni di rete, protocollo, coda, cache e RAM disk che
  legge solo gx-torrent: qBittorrent usa la propria configurazione. Le opzioni
  rare sono nel pannello chiuso **Per esperti**.

Ogni riga si salva con il suo **Salva**. Un campo cambiato e non ancora salvato
è segnato **non salvata** e compare la barra in basso *Modifiche non salvate*,
con **Salva tutto** e **Annulla modifiche**. Cambiare sezione o lasciare la
pagina con modifiche in sospeso chiede conferma. Gli editor (feed, filtri,
regole, hook) hanno il loro pulsante *Salva* e mostrano anch'essi **non
salvata** finché non li salvi.

### Cosa cercare

#### Generale

*Quando e quanto spesso Gextto cerca nuove release e gli episodi mancanti.*

**Ciclo di ricerca**

| Impostazione | Cosa fa |
|---|---|
| Ciclo automatico (ricerca e download) | Interruttore generale del ciclo: quando è spento Gextto non cerca e non scarica da solo. Acceso di default; con la prova (dry-run) attiva cerca senza scaricare. |
| Intervallo della ricerca automatica | Intervallo tra le ricerche automatiche di serie e film, in secondi (21600 = 6 ore). *(Unità: secondi)* |
| Ricerca titoli online nel ciclo | Cerca i titoli su indexer durante il ciclo: auto (solo se non ci sono feed configurati), yes (sempre), no (mai, usa solo feed e archivio locale). |
| Età massima delle release | Ignora le release più vecchie di N giorni (0 = nessun limite). Le release senza data sono considerate pubblicate oggi. *(Unità: giorni; 0 = nessun limite)* |

**Episodi mancanti**

| Impostazione | Cosa fa |
|---|---|
| Cerca gli episodi mancanti | Attiva il riempimento dei buchi (episodi mancanti) dalle release disponibili. |
| Episodi mancanti per serie a ogni ciclo | Numero massimo di gap da cercare per serie in un ciclo (0 = illimitato). *(0 = illimitato)* Compare solo con «Cerca gli episodi mancanti» attivo. |
| Intervallo della ricerca approfondita | Ogni quante ore fare una ricerca live mirata sugli indexer per i gap. *(Unità: ore)* Compare solo con «Cerca gli episodi mancanti» attivo. |
| Ricerche approfondite per ciclo | Numero massimo di ricerche live per ciclo. Vengono prima gli episodi mai cercati online, un episodio per serie a rotazione (ogni giro riparte dalla serie dopo l'ultima servita), poi quelli cercati da più tempo. Quando i torrent non danno nulla, aMule risponde una volta per stagione: una sola ricerca riempie tutte le puntate mancanti di quella stagione, come fa **Cerca mancanti**. Compare solo con «Cerca gli episodi mancanti» attivo. |
| Ultima risorsa: accetta una risoluzione fuori profilo | Attivo per default. Se per un episodio mancante non esiste nessuna copia nel profilo qualità, Gextto prende quella nella lingua richiesta anche con una risoluzione fuori profilo o sconosciuta; vale anche per le copie trovate su eD2k, che spesso non dichiarano la risoluzione nel nome. Lingua, sottotitoli, audio ed esclusioni valgono sempre. Il download porta il badge «🆘 ultima risorsa» in *Scarico*, il log e la notifica lo dicono, e l'upgrade lo sostituisce appena esce una copia nel profilo. Compare solo con «Cerca gli episodi mancanti» attivo. |

**Ritardo prima di scaricare**

| Impostazione | Cosa fa |
|---|---|
| Ritardo per le serie | Ritarda l'avvio dei download delle serie di questo numero di minuti. 0 avvia subito. *(Unità: minuti; 0 = nessuno)* |
| Ritardo per i film | Ritarda l'avvio dei download dei film di questo numero di minuti. 0 avvia subito. *(Unità: minuti; 0 = nessuno)* |
| Ignora il ritardo da questo punteggio | Se una release raggiunge almeno questo punteggio, ignora il delay configurato. *(0 = mai)* |

#### Sorgenti

*Da dove arrivano le release: feed RSS, motori web, filtri e cartelle osservate.*

**Feed e blacklist**

| Impostazione | Cosa fa |
|---|---|
| Blacklist (una parola per riga) | Parole vietate, una per riga: le release che le contengono vengono scartate. |
| Pagine feed da leggere | Quante pagine di elenco leggere per ogni feed (3 è un buon compromesso). |
| Disabilita i feed dopo giorni di errori | Dopo quanti giorni senza un successo il feed viene disabilitato automaticamente con un `#` davanti alla riga. Scatta solo se un altro feed ha funzionato nello stesso giorno; `0` disattiva la regola. |

**Editor**

| Impostazione | Cosa fa |
|---|---|
| Feed RSS | Lista dei feed RSS da leggere a ogni ciclo (una riga per URL). Metti `#` all'inizio di una riga per escluderla senza cancellarla: `# https://…` o `#https://…`. |
| Motori web | Motori di ricerca web usati dal gap-filling quando feed e indexer non trovano nulla. |
| Filtri contenuto esclusi | Le release che contengono queste parole o script (es. [porno]) vengono escluse. |
| Filtri per sorgente | Parole chiave da accettare o scartare per una singola sorgente, con attivazione per riga. |
| Cartelle osservate | I .torrent e i .magnet trovati nelle cartelle indicate vengono **scaricati** e aggiunti automaticamente alla sessione (ricorsiva, elimina dopo). |

Sotto l'editor c'è il riquadro **Feed disabilitati**: elenca le righe con `#` e dice se sono state disabilitate a mano o automaticamente, con la data e i giorni di errore. L'andamento completo — stato, ultimo successo, ultimo errore, ultimi risultati e tentativi/successi/fallimenti/release prese su 1, 7 e 30 giorni — è nella pagina **Salute → Attività dei feed**, con il riepilogo delle ultime 24 ore. Il pannello distingue i feed che condividono host e percorso tramite i parametri (per esempio `filter=u=…` di ext.to), maschera le credenziali e, finché non c'è ancora un tentativo, dice «in attesa del primo ciclo». Per riattivare un feed basta togliere il `#` e salvare.

Più feed possono puntare allo stesso sito (le liste degli uploader di ext.to o TorrentGalaxy). Gextto li legge distanziati, non tutti insieme, e quando il sito risponde «troppe richieste» lo lascia in pace: la pausa parte da dieci minuti e cresce ogni volta che succede di nuovo, fino a sei ore, mentre gli altri feed dello stesso sito aspettano un ciclo successivo. La pausa finisce da sola; non serve disabilitare i feed.

#### eD2k e Kad

*Collegamento ad aMule, ricerca su Kad e sui server eD2k, ripiego automatico e download fermi.*

eD2k e Kad passano da **aMule** (vedi [eD2k e Kad con aMule](#12-ed2k-e-kad-con-amule)):
Gextto comanda un `amuled` già avviato tramite le connessioni esterne (EC). È
spento per default: senza il tuo consenso Gextto non contatta la rete.

| Impostazione | Cosa fa |
|---|---|
| eD2k e Kad tramite aMule | Collega Gextto ad aMule: ricerca, link `ed2k://`, download in Scarico e pagina eD2k. Spento di default. |
| aMule — indirizzo | Macchina su cui gira `amuled`, di solito `127.0.0.1`. Gextto deve vedere la cartella di arrivo di aMule allo stesso percorso. |
| aMule — porta connessioni esterne (EC) | `ECPort` di `amule.conf`, 4712 di default. |
| aMule — password connessioni esterne (EC) | La password in chiaro; aMule conserva il suo MD5 in `ECPassword`. Non visualizzata. |
| Cartelle di aMule | La cartella di download (incoming) e quella temporanea sono mostrate in sola lettura: appartengono ad aMule e si cambiano da aMule/amuled. Dopo la modifica riavvia Gextto (oppure riapri questa pagina) perché usi il nuovo percorso. |
| Rete delle ricerche eD2k | **Kad** (default, non serve un server), **Kad + server eD2k** (entrambe le ricerche, risultati uniti), **Server eD2k (globale)** (tutti i server) o **Server eD2k (connesso)**. Vale per la ricerca manuale; il fallback automatico cerca sempre su Kad e sui server. La stessa scelta è nella pagina eD2k. |
| Usa eD2k quando i torrent non trovano nulla | Secondo interruttore, separato: quando la ricerca torrent di episodio e stagione non ha prodotto nulla, Gextto cerca lo stesso episodio su eD2k/Kad (con il nome della serie e tutti gli alias). Vale sia per il passaggio approfondito del gap filling sia per **Cerca mancanti** e per la ricerca del singolo episodio. Spento di default. |
| Fonti minime per il fallback | Soglia di fonti eD2k per accettare automaticamente un risultato. La ricerca manuale mostra tutto. *(Unità: fonti; 0 = nessun minimo)* |
| Durata della ricerca eD2k | Per quanti secondi aMule raccoglie i risultati prima che Gextto li mostri. *(Unità: secondi)* |
| Rinuncia a un download eD2k fermo dopo | Un download eD2k scelto da Gextto che non riceve dati per questi giorni viene annullato in aMule, la release va in blocklist e l'episodio torna da cercare. I download aggiunti a mano e quelli in pausa restano. *(Unità: giorni; 0 = mai)* |

Il fallback automatico accetta solo un risultato che corrisponde esattamente
all'episodio, ha una qualità riconosciuta, una dimensione plausibile e almeno le
fonti minime. In tutti gli altri casi il risultato resta visibile nella ricerca
manuale e il buco non si chiude: meglio un episodio mancante che un file
sbagliato in libreria. Se aMule non è raggiungibile, i torrent continuano senza
alcun effetto.

Nella pagina **Sorgenti** la voce **eD2k e Kad** mostra la spia della
connessione: **aperta** (connesso a un server con ID alto), **limitata**
(firewall/NAT: ID basso o solo Kad), **non connessa**, **aMule non
raggiungibile** oppure **sconosciuta**. Se è limitata, apri sul router le porte
TCP e UDP di aMule.

#### Qualità e upgrade

*Come vengono confrontate le release e quando una versione migliore sostituisce quella in libreria.*

La pagina parte da ciò che serve più spesso e lascia i pesi per ultimi:

1. **Upgrade e sostituzione** — le regole che decidono quando una release
   migliore sostituisce quella in libreria (tabella qui sotto).
2. **Esempi con i pesi attuali** — punteggi di riferimento (1080p WEB-DL,
   1080p H.265 DD+, 2160p Dolby Vision) e soglia di stop.
   **Impatto sulla libreria** — **Verifica ora** ripassa le release già viste
   nei feed con le regole e i pesi attuali e dice quanti episodi archiviati
   verrebbero sostituiti, con alcuni esempi. Usala dopo aver cambiato pesi o
   soglie: un numero alto vuol dire che i prossimi cicli riscaricherebbero molto.
   Non scarica nulla; le serie con «Permetti upgrade» su No non contano.
3. **Prova una release** — incolla il nome di una release: Gextto mostra il
   punteggio voce per voce, la copia già in libreria con il suo punteggio e se
   la sostituirebbe, con il motivo. Non scarica nulla.
4. **Profili di qualità** — i modelli di qualità, lingua, sottotitoli ed
   esclusioni (vedi *Profili di qualità*); la lista è quella esposta a
   Jellyseerr.
5. **Video**, **Audio**, **Correzioni release** — i pesi, in schede chiuse:
   aprile solo se vuoi cambiare le preferenze. Per ognuno: **più alto = più
   preferito**.
6. **Gruppi custom**.

Il pannello **Gruppi custom** aggiunge un bonus o una penalità a un release group
(il nome deve corrispondere al tag finale della release, es. TBK; maiuscole e
minuscole non contano). I punteggi della libreria vengono ricalcolati da soli
quando cambi i pesi o quando un aggiornamento di Gextto cambia le regole di
calcolo, così una modifica non viene scambiata per un upgrade;
*Manutenzione → Ricalcola punteggi* resta disponibile per forzarlo.

Un upgrade **per punteggio** richiede un miglioramento reale della qualità:
risoluzione, sorgente, codec, audio, HDR/DV, correzioni e gruppo devono valere
almeno la differenza minima in più. Dimensione del file, sottotitoli e salute
dello swarm aiutano a scegliere tra release equivalenti, ma da soli non fanno
mai riscaricare un episodio o un film della stessa qualità. La stessa regola
vale per i season pack e dopo il download: un file della stessa qualità non
sostituisce la copia archiviata solo perché ha un punteggio più alto. Se la
copia archiviata non dichiara la sorgente o il gruppo (per esempio un file
importato o rinominato con il solo titolo), quei campi non vengono accreditati
alla release trovata: senza la prova che la copia sia peggiore non viene
sostituita. `ffprobe` completa risoluzione, codec, audio e canali quando il nome
li ha persi.

Anche la regola che preferisce una sorgente WEB-DL a un WEBRip o un HDTV segue i
pesi: vale solo finché il peso **Sorgente** del WEB-DL è più alto. Se dai a
WEB-DL e WEBRip lo stesso peso, per Gextto sono la stessa qualità e un WEB-DL non
sostituisce più un WEBRip (restano validi HDR, risoluzione, REPACK/PROPER e il
punteggio).

**Verifica MediaInfo prima di entrare in libreria.** Quando un download completo
deve sostituire un episodio o un film già archiviato, Gextto legge con `ffprobe`
il file nuovo, ancora nella cartella di download, e la copia in libreria, prima
di spostare, collegare o copiare qualsiasi cosa. Risoluzione, codec video, audio,
canali e HDR10/HLG vengono presi dai flussi reali, non dal nome. Se il nome
prometteva più di quanto c'è (un «2160p» che è un 1080p ingrandito, un «HDR» senza
HDR, un «DDP 5.1» che è AAC stereo) e la qualità reale non è un upgrade, il
download viene scartato e messo in blocklist e la copia in libreria resta com'è;
il log lo spiega con «Upgrade not confirmed by MediaInfo». Sorgente, Dolby Vision
e REPACK/PROPER restano quelli del nome, perché `ffprobe` non li distingue in modo
affidabile. Senza `ffprobe`, o con la copia archiviata irraggiungibile, vale la
decisione presa prima del download. I pack di stagione non passano da questa
verifica.

**Upgrade e sostituzione**

| Impostazione | Cosa fa |
|---|---|
| Differenza minima score per upgrade | Differenza minima di punteggio per sostituire un file con un upgrade migliore. Conta solo la qualità: dimensione, sottotitoli e salute dello swarm non bastano a superarla. |
| Smetti di migliorare oltre questo punteggio | Tetto agli upgrade: quando il file in libreria ha almeno questo punteggio non viene più sostituito da release migliori; resta accettato solo un REPACK o PROPER, che corregge una release difettosa. Riferimenti con i punteggi predefiniti: 1080p WEB-DL H.264 ≈ 1280, 1080p WEB-DL H.265 DD+ ≈ 1480, 2160p WEB-DL ≈ 2480. Si somma alla differenza minima: la prima dice *di quanto* deve migliorare, questa *fino a dove*. Per fermare gli upgrade di un solo titolo imposta «Permetti upgrade» su No con *Modifica selezionati* (o nel profilo con cui lo aggiungi). «Perché non questa?» indica quando una release è scartata per questa soglia. *(0 = migliora sempre: nessun tetto)* |
| Sostituisci le versioni già archiviate | Sostituisce versioni inferiori già archiviate con upgrade migliori. |
| Differenza minima score per cleanup | Differenza minima di punteggio per sostituire un file esistente con uno migliore (cleanup). |

**Download bloccati**

| Impostazione | Cosa fa |
|---|---|
| Prova un'alternativa dopo (stallo) | Se un download è fermo da almeno questi minuti — a 0 B/s oppure ancora in attesa del suo elenco di file (metadati, tipico dei magnet morti) — la ricerca può avviare una versione alternativa della stessa puntata o film. Il download fermo resta in sessione e può riprendersi; la deduplica dell'archivio terrà poi la copia migliore. *(0 = mai)* |
| Punteggio in meno per l'alternativa | Quanti punti in meno sono accettati per l'alternativa rispetto alla release bloccata: parte solo un'alternativa adeguata, mai una versione scadente. Il download bloccato non viene rimosso. *(0 = disattivato)* |

### Come scaricare

#### Motore torrent

*Quale motore scarica i torrent e come Gextto lo raggiunge.*

**Motore torrent**

| Impostazione | Cosa fa |
|---|---|
| Motore torrent | Motore torrent attivo (gx-torrent o qBittorrent-nox). |

**gx-torrent**

| Impostazione | Cosa fa |
|---|---|
| gx-torrent — URL Web API | URL dell'API di gx-torrent (es. http://127.0.0.1:8890). Gextto lo usa per pilotare il demone. |
| gx-torrent — indirizzo di ascolto (LAN) | Indirizzo di ascolto del demone gestito. Predefinito `127.0.0.1:8890`: pagina web e API restano solo su questo server. Per esporle in LAN usa `0.0.0.0:8890` e imposta `gxtorrent_token` (senza token Gextto avvia il demone in `-insecure`). La porta viene allineata a quella dell'URL. |
| gx-torrent — gestione automatica (cache e coda) | Attiva l'autogestione di gx-torrent: coda dinamica e cache adattiva (dimensionata su memoria disponibile, download/seed attivi e tipo di storage). Predefinito attivo. Disattivalo per fissare a mano cache e slot. |
| gx-torrent — proxy (socks5:// o http://) | Proxy per peer, tracker HTTP, web seed, lista del filtro IP, feed RSS, indexer Torznab e download di .torrent; localhost e rete locale restano diretti. Con un proxy DHT e tracker UDP vengono spenti e FlareSolverr non viene usato come ripiego (non visualizzato). |
| gx-torrent — identità del client annunciata | Cosa vedono peer e tracker: `gx-core` con la versione reale (default) oppure un preset qBittorrent, Transmission, Rain o anacrolix/torrent. Cambia solo l'identità annunciata, non il protocollo: alcuni tracker privati accettano solo client in whitelist, controlla le loro regole. |
| gx-torrent — versione del client annunciata | Versione usata con il preset; vuoto usa quella predefinita del preset. |
| gx-torrent — ricontrolla i dati al completamento | Verifica una volta i file di un download completato, prima di seminarlo: un disco che restituisce un blocco errato viene scoperto subito invece di essere seminato per ore. Costa una lettura completa dei dati. Vale per i download completati dopo l'attivazione. |
| gx-torrent — modalità anonima | Nasconde la versione del client ai peer e ai tracker pubblici: il peer id la perde, l'handshake non invia la versione e i tracker pubblici non ricevono lo User-Agent. I tracker privati mantengono l'agente configurato. Non nasconde l'indirizzo IP: per quello serve una VPN. |
| gx-torrent — annuncia a tutti i tracker del tier | Contatta insieme tutti i tracker di un tier con più tracker e unisce i peer, come fa qBittorrent (predefinito attivo). Disattivalo per contattare un tracker per tier e passare al successivo in caso di errore. |
| gx-torrent — timeout richieste | Timeout in secondi delle richieste HTTP verso gx-torrent. *(Unità: secondi)* |
| gx-torrent — intervallo polling | Intervallo minimo in millisecondi tra due letture dello stato dei torrent (la coda la gestisce il demone). *(Unità: ms)* |
| gx-torrent — server DLNA/UPnP | Abilita il media server DLNA/UPnP del demone: i media **completati** dei torrent sono visibili a TV e console sulla rete locale. Solo rete locale, senza autenticazione; disattivato di default. Funziona con il motore gx-torrent. |
| gx-torrent — DLNA: indirizzo HTTP | Indirizzo e porta di ascolto del media server (es. `:8200` per tutte le interfacce, `192.168.1.10:8200` per una sola). Vuoto = `:8200`. La discovery usa UDP 1900 sulla stessa interfaccia. |
| gx-torrent — DLNA: nome del server | Nome mostrato ai lettori DLNA sulla rete. Vuoto = `gx-torrent`. |
| gx-torrent — DLNA: cartelle della libreria | Cartelle (es. l'archivio sul NAS) mostrate dal media server accanto ai torrent, navigabili per cartelle; separale con `;`. Vuoto = automatico: la cartella archivio e le cartelle che contengono gli archivi delle serie. Sola lettura; il cestino, le cartelle nascoste e quelle di sistema del NAS (`@eaDir`, `#recycle`) sono escluse. |

**qBittorrent-nox**

| Impostazione | Cosa fa |
|---|---|
| qBittorrent-nox — scaricato e aggiornato da Gextto | Gextto scarica da sé l'ultima release di qBittorrent-nox, la installa nella cartella dell'applicazione (accanto a gexttod), la avvia e la ferma con il servizio e la aggiorna (con backup e rollback). Il motore in uso però si sceglie dalla voce «Motore torrent»: questa opzione non lo cambia. |
| qBittorrent-nox — URL Web API | URL dell'interfaccia Web di qBittorrent-nox (es. http://127.0.0.1:8080). |
| qBittorrent-nox — utente | Utente dell'interfaccia Web di qBittorrent-nox. |
| qBittorrent-nox — password | Password dell'interfaccia Web di qBittorrent-nox (non visualizzata). |
| qBittorrent-nox — categoria | Categoria applicata ai torrent aggiunti a qBittorrent-nox. |
| qBittorrent-nox — tag | Tag applicato ai torrent aggiunti a qBittorrent-nox. |
| qBittorrent-nox — timeout richieste | Timeout in secondi delle richieste HTTP verso qBittorrent-nox. *(Unità: secondi)* |
| qBittorrent-nox — intervallo polling | Intervallo in millisecondi tra due letture dello stato dei torrent. *(Unità: ms)* |
| qBittorrent-nox — mappatura percorsi | Serve solo se qBittorrent vede le cartelle con percorsi diversi (Docker, NAS, altro host): una riga per percorso, forma locale=remoto. **Proponi** legge da qBittorrent le sue cartelle di download e temporanea e compila le righe; controllale e premi **Salva**. Stessa macchina e stesse cartelle — o modalità gestita —: lascia pure vuoto. |

**Azioni**

| Impostazione | Cosa fa |
|---|---|
| Applica motore | Verifica il motore configurato e indica se serve un riavvio (pannello *Motore torrent*). |
| Installa / Ottimizza qBittorrent-nox, Stato qBittorrent-nox, Test connessione | Nel pannello *qBittorrent-nox*: installa o aggiorna il binario gestito, mostra dove è installato e se c'è un aggiornamento, verifica la connessione. |

#### Velocità e rete

*Limiti di banda, fasce orarie, porte e protocolli della sessione torrent.*

**Limiti di velocità**

| Impostazione | Cosa fa |
|---|---|
| Come si decide la velocità | La regola della velocità: **Auto** (Gextto misura la linea e si adatta, senza tetto), **Limiti fissi** (i due limiti qui sotto) o **Programmazione oraria** (fasce per giorno, ognuna con limiti o Auto). In Scarico, dal menu **Banda**, puoi sempre fare un'eccezione temporanea, che vince sulla regola. Non è un'impostazione a sé: accende o spegne «Programmazione velocità attiva» e «Fuori dalle fasce orarie: Auto». |
| Limite download globale | Limite globale di download in KiB/s (0 = illimitato). *(Unità: KiB/s; 0 = illimitato)* Compare con «Limiti fissi» e «Programmazione oraria». |
| Limite upload globale | Limite globale di upload in KiB/s (0 = illimitato). *(Unità: KiB/s; 0 = illimitato)* Compare con «Limiti fissi» e «Programmazione oraria». |
| Programmazione velocità attiva | Si imposta da «Come si decide la velocità» e non compare nella pagina; resta disponibile dalle API. |
| Fuori dalle fasce orarie: Auto | Con la programmazione oraria: fuori dalle fasce la velocità è Auto invece dei limiti fissi. Compare solo con «Programmazione oraria». |
| Programmazione — per giorno | Una scelta per giorno: **Non attiva** (nessuna fascia), **Limite** (fascia con i tuoi valori di download e upload) o **Auto** (fascia in cui Gextto misura la linea e si adatta). Solo «Limite» abilita i due campi di velocità; gli orari restano sempre attivi. Una fascia oltre la mezzanotte appartiene al giorno in cui inizia. *(un giorno «Limite» ha i suoi valori (0 = illimitato); «Auto» lascia che Gextto misuri la linea e si adatti; una fascia oltre la mezzanotte appartiene al giorno in cui inizia)* Compare solo con «Programmazione velocità attiva» attivo. |
| Programmazione — ora inizio | Ora di inizio della programmazione (HH:MM). Non compare nella pagina: è sostituita dalla tabella «Programmazione — per giorno», che ne mostra il valore finché non viene salvata. |
| Programmazione — ora fine | Ora di fine della programmazione (HH:MM). Non compare nella pagina: è sostituita dalla tabella «Programmazione — per giorno», che ne mostra il valore finché non viene salvata. |
| Programmazione — giorni | Giorni attivi: 0=Lun … 6=Dom (es. 0,1,2,3,4). *(nessun giorno = mai attiva)* Non compare nella pagina: è sostituita dalla tabella «Programmazione — per giorno», che ne mostra il valore finché non viene salvata. |
| Programmazione — download | Limite di download in KiB/s durante la programmazione. *(Unità: KiB/s; 0 = illimitato)* Non compare nella pagina: è sostituita dalla tabella «Programmazione — per giorno», che ne mostra il valore finché non viene salvata. |
| Programmazione — upload | Limite di upload in KiB/s durante la programmazione. *(Unità: KiB/s; 0 = illimitato)* Non compare nella pagina: è sostituita dalla tabella «Programmazione — per giorno», che ne mostra il valore finché non viene salvata. |
| Auto rispetta la latenza | Con la velocità su Auto, Gextto misura il ping verso Google e Cloudflare ogni 2 secondi mentre trasferisce, ogni 30 secondi a linea ferma (8.8.8.8, 8.8.4.4, 1.1.1.1, 1.0.0.1; si usa il tempo più basso). Finché il ping è buono alza download e upload; quando sale taglia il 15% e scende fino a 128/64 KiB/s. Ricorda la velocità alla quale il ping è peggiorato e si ferma appena sotto, riprovando a superarla una volta al minuto. Se il ping resta buono anche a piena velocità, il limite viene tolto; quando resta solo il traffico di fondo (DHT), il limite cade. La prima volta che applica un limite mentre sta scaricando e il ping è salito lo scrive subito nel log; un controllo che serve solo a misurare la linea, o che avviene con il solo seed, resta nel dettaglio (debug) e non compare nel log normale. Poi scrive al massimo una riga al minuto, solo se la velocità cambia almeno del 10% e l'andamento lo conferma. Con una linea asimmetrica l'upload conta come carico già a un decimo della capacità (tra 32 e 256 KiB/s) e viene limitato prima che saturi; se comincia a caricare durante il controllo, il limite parte subito. Il ping misurato compare accanto alla velocità **↓ Download** nella barra in alto. Attiva per default. |
| Auto — cosa misura la latenza | `both` (default): il ping decide, i contatori del router fanno da diagnostica e da freno. `ping`: solo ping, il router non viene letto. `router`: vecchio comportamento, si cede solo agli altri dispositivi visti dal router. In `both` e `router`, quando il router vede un altro dispositivo attivo, Auto **non prende altro spazio**: i limiti restano dove sono e non scendono. È così che rispetta una chiamata o un gioco, che non fanno coda e quindi non muovono il ping; in `ping` questo non è possibile per costruzione, perché il router non viene letto. |
| Auto — host per misurare la latenza | Host (o host:porta) a cui misurare la latenza con una connessione TCP, solo mentre Auto è attivo: ogni 2 secondi mentre Gextto trasferisce, ogni 30 secondi a linea ferma. Vuoto = Google (8.8.8.8, 8.8.4.4) e Cloudflare (1.1.1.1, 1.0.0.1), porta 443; si usa il tempo più basso, così un server lontano o lento non falsa la misura. |
| Auto — velocità massima linea, download | Massima velocità reale della linea in download, in KiB/s (es. 9000 per 9 MB/s). 0 = non nota: la scopre Auto. Se la indichi, Auto non sale oltre e trova il punto giusto più in fretta. |
| Auto — velocità massima linea, upload | Massima velocità reale della linea in upload, in KiB/s (es. 800). 0 = non nota: la scopre Auto. Se la indichi, Auto non sale oltre e trova il punto giusto più in fretta. |
| Applica i limiti anche ai peer della rete locale | Spento (default, consigliato): i peer della tua rete di casa o d'ufficio (indirizzi privati 10.x, 172.16–31.x, 192.168.x, link-local, loopback e IPv6 locali fc00::/7) scaricano e caricano alla massima velocità, come in qBittorrent e libtorrent: i limiti di velocità servono per la linea Internet. Acceso: anche loro rispettano i limiti globali e del torrent. Con un proxy configurato tutti i peer rispettano i limiti (dietro un proxy gx-torrent non può sapere dove si trova un peer). Solo gx-torrent (qBittorrent ha la sua opzione). Cambiarla riavvia gx-torrent. |

**Porte e interfacce**

| Impostazione | Cosa fa |
|---|---|
| Porta minima | Porta minima della sessione torrent (richiede il riavvio del servizio). |
| Porta massima | Porta massima della sessione torrent (richiede il riavvio del servizio). |
| Interfacce listen | Indica dove il motore accetta connessioni: 0.0.0.0:6881-6891 per tutte le interfacce, 127.0.0.1:6881 solo in locale, oppure wg0:6881/tun0:6881 per una VPN. Il valore proposto va bene nella maggior parte dei casi. |
| Interfaccia uscente | Killswitch VPN: interfaccia usata per tutto il traffico BitTorrent in uscita. |
| Test porte | Verifica che la porta peer sia in ascolto e che il router la inoltri (il messaggio dice "aperta", "in ascolto ma non inoltrata" o "chiusa"). Disponibile con il motore **gx-torrent**. |

**Ricerca peer e tracker**

| Impostazione | Cosa fa |
|---|---|
| DHT | Abilita la rete DHT per trovare peer senza tracker. |
| PEX | Peer Exchange: scambio peer con altri client. |
| LSD | Local Service Discovery: trova peer nella rete locale. |
| UPnP | Apre le porte del router automaticamente con UPnP. |
| NAT-PMP | Apre le porte del router automaticamente con NAT-PMP (prova anche PCP, RFC 6887). |
| uTP | Abilita il protocollo uTP (UDP) oltre a TCP. |
| Holepunching (BEP 55) | Apre una connessione diretta ai peer dietro NAT tramite un peer che fa da relè (BEP 55). Richiede uTP. |
| Nodi bootstrap DHT | Nodi DHT iniziali (host:porta separati da virgola). |

**Cifratura e filtro IP**

| Impostazione | Cosa fa |
|---|---|
| Cifratura | Politica di cifratura: 0 disabilitata, 1 abilitata, 2 forzata. |
| Applica IP filter | Applica il filtro IP anche ai tracker HTTP. È spento per impostazione predefinita: la lista predefinita contiene gli intervalli di Cloudflare, quindi i tracker ospitati lì (molti tracker privati) resterebbero irraggiungibili e nella scheda *Tracker* comparirebbe «tracker IP is blocked». Il filtro sui peer resta sempre attivo. |
| IP filter (file/URL) | File locale o URL della lista IP da bloccare. Il valore predefinito è la lista eMule Security (`https://upd.emule-security.org/ipfilter.gz`), in formato P2P e aggiornata ogni giorno. |

**Azioni**

| Impostazione | Cosa fa |
|---|---|
| Filtro IP → Carica / aggiorna ora | Scarica la lista dall'URL configurato (o usa il file locale) e la applica subito al motore attivo, gx-torrent compreso. |

#### Code e prestazioni

*Quanti torrent restano attivi, connessioni, RAM disk e cache.*

**Gestione automatica**

| Impostazione | Cosa fa |
|---|---|
| Adatta i valori nel tempo | Ritocca nel tempo i valori scelti dal sistema (per esempio lo spazio libero minimo) quando cambiano le condizioni. Non tocca mai i valori impostati a mano; spegnilo per avere valori fissi. |

**Coda e slot**

| Impostazione | Cosa fa |
|---|---|
| Auto-gestione dinamica coda e risorse | Regola automaticamente quanti torrent sono attivi in base al carico. |
| Slot download dinamici minimi | Numero minimo di download dinamici. La coda cambia al massimo di uno per volta. Compare solo con «Auto-gestione dinamica coda e risorse» attivo. |
| Slot download dinamici massimi | Numero massimo di download dinamici. Servono campioni consecutivi coerenti prima di aumentare la coda. Compare solo con «Auto-gestione dinamica coda e risorse» attivo. |
| Non contare i torrent fermi negli slot attivi | I torrent che non trasferiscono dati non consumano uno slot attivo. |
| Download attivi | Valore base dei download attivi; con la coda dinamica viene adattato a runtime. |
| Seed attivi | Valore base dei seed attivi; con la coda dinamica scende a 1 quando ci sono download in coda. |
| Limite torrent attivi | Valore base del limite di torrent attivi; con la coda dinamica diventa max(base, download + seed + 2). |

**Modalità di download**

| Impostazione | Cosa fa |
|---|---|
| Download sequenziale | Scarica i file in ordine sequenziale invece che a pezzi sparsi. |
| Prima/ultima parte dei file | Scarica per primi l'inizio e la fine di ogni file in tutti i nuovi torrent, poi prosegue normalmente: utile per guardare un video mentre scarica. Vale con ogni motore; i torrent già in corso non cambiano. |
| Prealloca lo spazio su disco | Riserva subito tutto lo spazio su disco prima di iniziare il download. |

**Connessioni**

| Impostazione | Cosa fa |
|---|---|
| Connessioni automatiche (in base alla macchina) | Attivo (predefinito): Gextto lascia decidere al motore quante connessioni tenere per ogni torrent in base a core e RAM della macchina, e riduce quelle dei torrent che non progrediscono da un paio di minuti pur avendo molti peer. Con molti torrent attivi insieme il motore ha anche un totale di connessioni per tutta la macchina (75 per core), diviso tra i torrent con la precedenza a quelli che stanno scaricando; un torrent da solo ha sempre tutte le sue connessioni. Il valore qui sotto resta il tetto massimo. Spegnilo per usare un numero fisso. Solo gx-torrent. |
| Connessioni max per torrent | Tetto alle connessioni per singolo torrent (-1 = illimitato, predefinito). Con le connessioni automatiche attive è il massimo che il motore può usare; con l'automatico spento è il limite fisso. Vale per ogni torrent che non ha un limite proprio (scheda *Limiti* del dettaglio) e si applica subito, anche ai torrent in corso; solo gx-torrent. |

**RAM disk**

| Impostazione | Cosa fa |
|---|---|
| Usa il RAM disk | Scarica in RAM i torrent che rientrano nella soglia; i più grandi vanno su disco. |
| Cartella RAM disk | RAM disk da usare per i download in corso, se disponibile. Compare solo con «Usa il RAM disk» attivo. |
| Dimensione massima per torrent | Dimensione massima di un singolo torrent ammesso sul RAM disk (GB). *(Unità: GB)* Compare solo con «Usa il RAM disk» attivo. |
| Margine libero da mantenere | Spazio libero da lasciare sul RAM disk una volta completato il download (GB). *(Unità: GB)* Compare solo con «Usa il RAM disk» attivo. |
| Spazio minimo libero | Spazio minimo libero in byte richiesto per usare il RAM disk. 0 = usa il margine configurato. *(Unità: byte; 0 = usa il margine)* Compare solo con «Usa il RAM disk» attivo. |

La scelta avviene il più presto possibile: un torrent la cui dimensione è già
nota e supera la soglia va subito su disco; uno con dimensione **ancora
sconosciuta** (tipico dei magnet) resta su disco finché non arrivano i metadati,
poi viene spostato sul RAM disk **solo se** la dimensione reale rientra nella
soglia. Un file troppo grande non tocca mai il RAM disk. La regola vale per tutti
i download — automatici e aggiunti a mano — e per ogni motore che usa il RAM
disk; qBittorrent, che non lo supporta, scarica sempre su disco.

Gextto verifica anche la memoria disponibile della macchina: un `tmpfs` sta in
RAM, quindi un disco da 6 GB può risultare quasi vuoto mentre il sistema ha poca
memoria libera. Prima di iniziare, lo spazio usabile del RAM disk è limitato
dalla memoria disponibile (`MemAvailable`): il download va sul RAM disk solo se
ci stanno sia lo spazio del `tmpfs` sia la memoria del sistema, altrimenti resta
su disco come i file troppo grandi. Se la memoria non è leggibile vale il solo
spazio del `tmpfs`, come prima.

**Per esperti**

| Impostazione | Cosa fa |
|---|---|
| Cache disco | Dimensione della cache disco in blocchi (-1 = automatico). *(Unità: blocchi; -1 = auto)* |
| Scadenza cache | Secondi di inattività dopo cui un blocco esce dalla cache. *(Unità: secondi)* |

**Azioni**

| Impostazione | Cosa fa |
|---|---|
| Ottimizza | Calcola cache e buffer in base alla RAM. |
| Ottimizza per il sistema | Propone i valori adatti a questo hardware (memoria, dischi, condivisione di rete, RAM disk), una riga per modifica con il motivo; nulla viene scritto finché non applichi le righe selezionate. Un valore salvato a mano non viene mai riproposto. |

#### Seed e completamento

*Quanto restare in seed, cosa fare a download finito, come controllare i pack in download e come gestire i torrent bloccati.*

**Seed**

| Impostazione | Cosa fa |
|---|---|
| Seed ratio globale | Rapporto upload/download dopo cui fermare il seeding. 0 = infinito: basta questo perché il seed non finisca mai e il file non venga spostato, anche con un limite di tempo. *(0 = infinito)* |
| Seed massimo | Limite principale di seeding in giorni; se maggiore di 0 prevale sul limite in minuti. Con giorni e minuti a 0 il tempo è infinito. *(Unità: giorni; 0 = usa il limite in minuti)* |
| Seed massimo (fallback) | Limite di seeding in minuti, usato solo se Seed massimo (giorni) è 0. Con giorni e minuti a 0 il tempo è infinito: il seed non finisce mai e il file non viene spostato. *(Unità: minuti)* |
| Ferma il seed inattivo dopo | Ferma un torrent in seed che non invia dati da questo numero di minuti (conta solo il tempo in seed attivo). Come gli altri limiti: a fine seed segue l'archiviazione o la rimozione. Con qBittorrent il valore viene inviato anche come `inactiveSeedingTimeLimit`. *(Unità: minuti; 0 = disattivato)* |

**Editor**

| Impostazione | Cosa fa |
|---|---|
| Regole di seed | Limiti di seed (ratio, giorni, inattività) per sorgente/indexer e categoria; vale la prima regola che corrisponde, un campo vuoto usa il globale, 0 toglie il limite. |

**A download completato**

| Impostazione | Cosa fa |
|---|---|
| Elimina i completati dopo il seed | Attivo: a fine seed il torrent completato viene tolto dalla sessione (come «Pulisci completati»). Spento: a fine seed il torrent resta nell'elenco come Completato e lo rimuovi tu con «Pulisci completati». Non influisce su dove vengono spostati i file. |
| Sposta gli episodi/pack in archivio (non copiare) | Attivo: la sorgente scaricata viene spostata in libreria e rimossa dalla cartella download (per i torrent al termine del seed; per eD2k al completamento, perché il seeding è gestito da aMule). Spento: la sorgente viene copiata o collegata e mantenuta per la condivisione. |
| Hardlink invece della copia durante il seed | Attivo (predefinito): un file che resta in seed entra in libreria come hardlink, senza occupare spazio due volte; se download e libreria sono su filesystem diversi si copia. Vedi «Hardlink al posto della copia». |

**Controllo dei pack**

| Impostazione | Cosa fa |
|---|---|
| Ispeziona i pack in download e scarta quelli inutili | Attivo: appena il motore conosce la lista dei file, Gextto controlla se almeno un episodio serve davvero (manca dalla libreria oppure è un vero upgrade). Se nessuno serve, il torrent viene rimosso con i dati parziali e messo in blocklist, così non viene più scaricato. Spento (predefinito): il pack viene scaricato per intero e deciso solo a download finito. |
| Nei pack utili scarica solo i file che servono | Attivo (con l'ispezione): quando il pack contiene solo alcuni episodi utili, il motore scarica soltanto quei file e salta gli altri (priorità 0); sottotitoli e file senza numero di episodio restano attivi. Spento: il pack utile viene scaricato per intero. |

**Torrent bloccati**

| Impostazione | Cosa fa |
|---|---|
| Considera bloccato dopo | Dopo questi minuti senza avanzamento il torrent viene considerato stalled. *(Unità: minuti)* |
| Riprova i torrent bloccati ogni | Intervallo tra i tentativi di reannounce dei torrent stalled. *(Unità: minuti)* |
| Rimuovi i torrent bloccati dopo | Dopo questo periodo senza progresso il torrent viene rimosso automaticamente. Imposta 0 per disattivare completamente la rimozione automatica per stallo. *(Unità: minuti; 0 = mai)* |
| Rimuovi i torrent senza seeder dopo | Stallo con zero seeder (dead swarm): rimosso dopo questo periodo, più breve, e la puntata viene ricercata di nuovo subito. Imposta 0 per usare la soglia generale. *(Unità: minuti; 0 = usa la soglia generale)* |
| Riammetti una release senza seeder dopo | Una release con lo swarm vuoto — abbandonata durante il download o rifiutata prima di partire perché nessuno la condivideva — resta in blocklist per questo periodo, poi torna fra i candidati: così un 4K che nel frattempo ha trovato seeder può essere scaricato come upgrade, e il log non ripete la stessa riga a ogni ricerca. Una release che rientra e viene rifiutata di nuovo resta via quattro volte tanto, e la terza volta dodici volte, fino a tre mesi; il conteggio viene dimenticato dopo sei mesi, e un download che va a buon fine lo azzera. La copia già in libreria resta nel frattempo. Imposta 0 per tenerla bloccata per sempre. *(Unità: giorni; 0 = per sempre)* |

### Dove salvare

#### Libreria e rinomina

*Come vengono nominati e organizzati i file in libreria.*

**Rinomina**

| Impostazione | Cosa fa |
|---|---|
| Rinomina episodi | Rinomina i file scaricati usando i metadati TMDB (o TVDB senza chiave TMDB). |
| Film come file singoli (spiana le cartelle) | Se Sì, un film arrivato dentro una cartella torrent viene spostato nella cartella film come file singolo, portando con sé sottotitoli e artwork. Se No, resta nella sua cartella. |
| Verifica dei file rinominati ogni | Ogni quante ore verificare che i file archiviati/rinominati siano ancora presenti. *(Unità: ore)* |
| Rimuovi le tracce audio in altre lingue | Se Sì, dopo l'importazione Gextto tiene solo le tracce audio nella lingua scelta e toglie le altre, senza ricodificare il video: qualità, HDR e sincronizzazione restano quelli del file originale. Il file viene toccato solo se c'è più di una traccia audio e almeno una è dichiarata nella lingua scelta. Prima di sostituirlo Gextto controlla la copia: stesse tracce nello stesso ordine, con codec, lingua e flag invariati (un sottotitolo spento resta spento), stessa durata, stessa sincronizzazione, e ogni pacchetto di video, audio tenuto e sottotitoli identico all'originale, byte per byte. Per gli MKV prova con mkvmerge e, se la copia non supera il controllo, la rifà con ffmpeg; se nessuna delle due lo supera il file resta com'è e il log lo dice. Se la traccia predefinita era fra quelle tolte, diventa predefinita la traccia nella lingua scelta. La copia temporanea si scrive nella stessa cartella: se lo spazio libero non basta il file resta com'è. Sulle condivisioni di rete (NFS, SMB) i permessi non vengono toccati: li decide la condivisione. Vengono puliti i file importati mentre l'opzione è accesa; quelli in attesa o in lavorazione quando Gextto si ferma vengono puliti dopo il riavvio. Se il file in libreria è un hardlink di un torrent ancora in seed, la copia pulita è un file nuovo: finché dura il seed lo spazio è occupato due volte. L'opzione si attiva solo se ffprobe, ffmpeg e mkvmerge sono installati; altrimenti la riga resta bloccata con un avviso. Per ogni file pulito arriva una notifica sui canali configurati (Telegram, webhook, email) con file, tracce rimosse e spazio risparmiato. |
| Lingua audio da tenere | Scegli dalla lista (italiano, inglese, francese…) o scrivi il codice a 2-3 lettere, per esempio ita o eng. Le varianti dello stesso codice valgono uguale: it, ita, it-IT e Italian per l'italiano, pl e pol per il polacco, e così per tutte le lingue della lista. Le tracce dichiarate in un'altra lingua vengono tolte; per quelle senza lingua decide «Tracce audio senza lingua». |
| Se ci sono più tracce nella lingua scelta | Tenerle tutte, solo la migliore (più canali e bitrate) o solo la prima. |
| Tracce audio senza lingua | Cosa fare con le tracce audio che non dichiarano una lingua (tag assente, und, mul…), che spesso sono l'audio originale. **Tienile** (predefinito): restano nel file accanto a quelle nella lingua scelta. **Toglile**: vengono tolte insieme alle altre lingue. **Non toccare il file**: un file che ne ha anche una sola resta com'è. |

**Lingue dei metadati**

| Impostazione | Cosa fa |
|---|---|
| Lingua predefinita | Lingua preferita di default per serie e film (es. ita, eng). |
| Lingua TVDB | Lingua preferita per i metadati TVDB (es. ita, eng). |
| Lingua TMDB | Lingua usata per i metadati TMDB (es. it-IT, en-US). |

**Editor**

| Impostazione | Cosa fa |
|---|---|
| Composizione del nome | Editor del template con token e anteprima per comporre il nome dei file. |
| Regole tag → cartella | Associa un tag del torrent a una cartella temporanea e a una cartella finale. |

#### Archivio e spazio

*Cartelle di lavoro, cestino e spazio minimo su disco.*

**Cartelle**

| Impostazione | Cosa fa |
|---|---|
| Cartella archivio | Cartella di archivio predefinita per i contenuti senza percorso dedicato. |
| Cartella download | Cartella di download predefinita per tutti i motori. |
| Cartella temporanea | Cartella temporanea per i download in corso. |
| Copia i file .torrent in | Copia qui i file .torrent dei download (vuoto = nessuna copia). Gextto tiene una propria copia interna del `.torrent` finché il torrent è nella sessione e la elimina quando lo rimuovi o lo archivi; questa cartella non viene toccata. |

**Cestino**

| Impostazione | Cosa fa |
|---|---|
| Cartella cestino | Cartella dove vengono spostati i file sostituiti/duplicati (se lasciata vuota usa la sottocartella trash nella cartella dati). |
| Cosa fare con i file sostituiti | Cosa fare con i file sostituiti: sposta nel trash o elimina. |
| Conservazione nel cestino | Giorni di conservazione per le pulizie non forzate; 0 elimina tutto il contenuto del cestino. Le azioni manuali della UI svuotano sempre subito il cestino. *(Unità: giorni; 0 = svuota tutto)* |

**Spazio su disco**

| Impostazione | Cosa fa |
|---|---|
| Spazio libero minimo per scaricare | Spazio libero minimo (GB) sulla cartella download: sotto questa soglia il ciclo non avvia download e gx-torrent mette in pausa quelli in corso, che ripartono da soli quando lo spazio torna. *(Unità: GB; 0 = nessun controllo del ciclo, gx-torrent mantiene il suo default di 1 GiB)* |

### Sistema

#### Manutenzione automatica

*Pulizie periodiche dei dati tecnici, analisi MediaInfo e file orfani.*

**Housekeeping**

| Impostazione | Cosa fa |
|---|---|
| Housekeeping periodico attivo | Attiva la pulizia periodica dei dati tecnici e dello storico. |
| Housekeeping — intervallo | Intervallo tra due housekeeping automatici, in ore. *(Unità: ore)* Compare solo con «Housekeeping periodico attivo» attivo. |
| Housekeeping — statistiche cicli di ricerca conservate | Numero di statistiche dei cicli di ricerca da conservare. Compare solo con «Housekeeping periodico attivo» attivo. |
| Housekeeping — visti nel feed | Elimina le righe storiche delle release viste nei feed più vecchie di N giorni. *(Unità: giorni; 0 = mai)* Compare solo con «Housekeeping periodico attivo» attivo. |
| Housekeeping — storico download | Elimina dallo Storico download le righe dei torrent rimossi più vecchie di N giorni. *(Unità: giorni; 0 = conserva sempre)* Compare solo con «Housekeeping periodico attivo» attivo. |
| Housekeeping — schede errore | Elimina le schede dei torrent in errore più vecchie di N giorni (minimo 1). *(Unità: giorni)* Compare solo con «Housekeeping periodico attivo» attivo. |
| Housekeeping — log ricerche gap | Elimina il log delle ricerche degli episodi mancanti più vecchio di N giorni. *(Unità: giorni; 0 = mai)* Compare solo con «Housekeeping periodico attivo» attivo. |
| Housekeeping — backup upgrade | Elimina i backup dei file sostituiti dagli upgrade più vecchi di N giorni. *(Unità: giorni; 0 = mai)* Compare solo con «Housekeeping periodico attivo» attivo. |

**Pulizia archivio release**

| Impostazione | Cosa fa |
|---|---|
| Pulizia automatica archivio | Abilita la pulizia automatica dell'archivio a ogni housekeeping: elimina solo le righe del catalogo release più vecchie dell'età massima. File scaricati, libreria, storico download e torrent attivi non vengono toccati. |
| Archivio — età massima | Età massima delle release in archivio, in giorni (0 = nessun limite). *(Unità: giorni; 0 = nessun limite)* Compare solo con «Pulizia automatica archivio» attivo. |
| Archivio — mantieni almeno N voci | Numero minimo di release recenti da conservare sempre, anche se più vecchie dell'età massima. Compare solo con «Pulizia automatica archivio» attivo. |

**Manutenzione notturna**

| Impostazione | Cosa fa |
|---|---|
| Finestra notturna — inizio | Ora di inizio (HH:MM, predefinita 01:00) della finestra in cui girano i lavori pesanti: un lavoro alla volta e solo quando Gextto è inattivo, cioè nessun download trasferisce più di 100 KB/s, nessun download viene archiviato, il controllo della libreria e il controllo integrità sono fermi e il carico del sistema è basso. Il seed non conta: continua tutta la notte. |
| Finestra notturna — fine | Ora di fine (HH:MM, predefinita 07:00). Dopo non parte nessun lavoro nuovo; quello in corso viene completato. Se la fine è prima dell'inizio la finestra passa la mezzanotte; inizio e fine uguali vogliono dire tutto il giorno. |
| Controllo integrità e analisi MediaInfo solo nella finestra | Sì (predefinito): il controllo integrità e l'analisi MediaInfo girano al loro intervallo ma solo dentro la finestra e con Gextto inattivo. No: girano al loro intervallo a qualsiasi ora, come prima. I comandi manuali (**Controlla ora**, **Aggiorna MediaInfo**) partono sempre subito. |
| Manutenzione del database ogni notte | Sì (predefinito): una volta per notte i database vengono ottimizzati (`PRAGMA optimize`) e compattati quando hanno almeno il 20% o 64 MB di spazio libero; una volta alla settimana un controllo completo (`integrity_check`) legge ogni pagina e ogni indice, e se trova un problema arriva subito una notifica, senza aspettare il riepilogo. |
| Pulisci l'audio dei file già in libreria | Applica **Rimuovi le tracce audio in altre lingue**, con la stessa lingua e le stesse scelte, ai file già archiviati: uno alla volta, nella finestra, con gli stessi controlli della pulizia dopo l'importazione. Ogni file viene esaminato una sola volta (Gextto ne ricorda dimensione e data); un file ancora in seed tramite hardlink aspetta la fine del seed, così la copia pulita non occupa spazio due volte, e un file scritto nell'ultima ora aspetta la notte dopo. Un file che non supera il controllo per un motivo passeggero (uno strumento interrotto, il disco che non risponde) resta com'è e viene riprovato dopo una settimana; un file che non si può pulire senza cambiarne il contenuto — la verifica delle tracce non torna — viene ricordato e non viene più riprovato, a meno che il file cambi (dimensione o data). Spento di default; si attiva solo con la pulizia audio accesa. |

**Manutenzione notturna — pannello**

Sotto le opzioni, nello stesso gruppo, il pannello **Stato della manutenzione notturna** mostra la finestra, cosa sta girando o perché aspetta, e l'esito di ogni lavoro.

| Voce | Cosa fa |
|---|---|
| Esegui ora | Esegue subito un passaggio della manutenzione, anche fuori dalla finestra; aspetta comunque che Gextto sia inattivo. |
| Database | Ultima ottimizzazione, dimensione totale dei database e spazio recuperato, ultimo controllo di integrità e i problemi trovati. |
| Pulizia audio della libreria | File controllati sul totale, file puliti, spazio risparmiato, file lasciati com'erano e l'ultimo controllo non superato. |
| Libreria | L'ultimo passaggio del controllo della libreria (file corretti, rinominati, doppioni nel cestino, errori: il controllo gira da solo ogni *Verifica dei file rinominati ogni* ore) e il rapporto notturno dei file che il database conosce ma che non sono più sul disco. Una cartella che non si legge, per esempio un NAS non montato, è indicata a parte e non conta come file mancanti; sono elencate solo le stagioni che la serie segue ancora, quindi i file di una stagione tolta dall'intervallo o di una serie disattivata non vengono segnalati. Se un download di quella serie è in archiviazione o la pulizia audio della libreria sta riscrivendo un suo file, il controllo salta la serie per quel passaggio e la riga nel log dice quale delle due operazioni la sta tenendo occupata; la serie viene ripresa al controllo successivo. |
| Segna come mancante | Toglie dall'archivio la puntata il cui file non c'è più, così la ricerca la scarica di nuovo. Non fa niente se il file è tornato o se la cartella non si legge. |

Quando la finestra si chiude, il log riassume la notte in una riga; arriva anche una notifica sui canali configurati, del gruppo manutenzione, se la notte ha trovato problemi nei database, file mancanti, file puliti o file lasciati com'erano, oppure ha recuperato almeno 64 MB.

**Analisi MediaInfo**

| Impostazione | Cosa fa |
|---|---|
| Backfill MediaInfo automatico | Analizza periodicamente con ffprobe i file già presenti che non hanno ancora MediaInfo. |
| Backfill MediaInfo — intervallo | Minuti tra due passaggi del backfill MediaInfo. *(Unità: minuti)* Compare solo con «Backfill MediaInfo automatico» attivo. |
| Backfill MediaInfo — file per volta | Numero massimo di file analizzati in ogni passaggio MediaInfo. Compare solo con «Backfill MediaInfo automatico» attivo. |
| Controllo integrità file automatico | Verifica periodicamente con `ffprobe` e `ffmpeg` i file della libreria, un file alla volta, per trovare quelli danneggiati. |
| Controllo integrità — intervallo | Minuti tra due passaggi del controllo. Compare solo con «Controllo integrità file automatico» attivo. |
| Controllo integrità — file per volta | Numero massimo di file verificati in ogni passaggio. |
| Controllo integrità — controllo veloce dei file 4K | Per i file 4K legge tutto il file ma decodifica solo i fotogrammi chiave: il controllo è molto più veloce e trova comunque i file troncati o illeggibili, ma può non vedere un piccolo danno tra un fotogramma chiave e l'altro. È attivo di default. |
| Controllo integrità — riscarica i file danneggiati | Appena il controllo trova un file danneggiato mette la release in blocklist, sposta il file fuori dalla libreria e cerca subito una release diversa per la stessa puntata o lo stesso film. È spento di default: senza, la sostituzione resta manuale, con **Riscarica**. |

**Controllo integrità file — pannello**

Sotto le opzioni, nello stesso gruppo, il pannello mostra lo stato e i file trovati.

| Voce | Cosa fa |
|---|---|
| Avvia controllo / Riprendi controllo | Avvia la passata o la riprende dal primo file non ancora verificato; i file già controllati e non cambiati vengono saltati. Funziona anche con il controllo automatico spento. |
| Interrompi | Ferma il controllo dopo il file in corso; la posizione resta salvata. |
| Riavvia da capo | Ricomincia dal primo file, cancella i risultati precedenti e ricontrolla tutta la libreria. |
| Danneggiato / Illeggibile | Il file non è stato decodificato, oppure non è stato possibile leggerlo entro il tempo previsto: un illeggibile non è dato per rotto e viene ritentato automaticamente in un passaggio successivo. |
| Riscarica | Sposta il file danneggiato fuori dalla libreria, mette la vecchia release in blocklist e cerca la stessa puntata scegliendo una release diversa. Con **Controllo integrità — riscarica i file danneggiati** attivo lo fa Gextto da solo, appena il file è dato per danneggiato. |

**File orfani**

| Impostazione | Cosa fa |
|---|---|
| Sposta nel cestino i dati orfani della cartella temporanea | Ogni 30 minuti sposta nel cestino file e cartelle della cartella temporanea dei download che non appartengono a nessun torrent in lista e non sono cambiati da almeno il numero di giorni indicato. Non cancella nulla: restano nel cestino finché non lo svuoti. |
| Dati orfani — fermi da almeno | Da quanti giorni un elemento della cartella temporanea deve essere fermo, senza torrent in lista, per essere spostato nel cestino. *(Unità: giorni)* Compare solo con «Sposta nel cestino i dati orfani della cartella temporanea» attivo. |

#### Notifiche

*Telegram, email, webhook, Discord, Slack, Gotify, ntfy, Pushover, Apprise e programmi da eseguire sugli eventi. «Invia notifica di test» manda un messaggio su tutti i canali configurati.*

**Telegram**

| Impostazione | Cosa fa |
|---|---|
| Telegram attivo | Invia le notifiche su Telegram. |
| Telegram bot token | Token del bot Telegram (da @BotFather). Compare solo con «Telegram attivo» attivo. |
| Telegram chat ID | ID della chat/canale dove inviare le notifiche. Compare solo con «Telegram attivo» attivo. |

**Email**

| Impostazione | Cosa fa |
|---|---|
| Email attiva | Invia le notifiche via email. |
| SMTP | Server SMTP nel formato host:porta (es. smtp.gmail.com:587). Compare solo con «Email attiva» attivo. |
| Email mittente | Indirizzo mittente delle email di notifica. Compare solo con «Email attiva» attivo. |
| Email destinatario | Destinatari delle email (separati da virgola). Compare solo con «Email attiva» attivo. |
| Password email | Password/app-password SMTP (non visualizzata). Compare solo con «Email attiva» attivo. |

**Webhook**

| Impostazione | Cosa fa |
|---|---|
| Webhook URL | URL del webhook a cui inviare gli eventi. |
| Webhook secret | Segreto HMAC per firmare le richieste al webhook. |
| Formato webhook | Formato del payload inviato all'URL del webhook (Gextto, Discord, Slack, ntfy, Gotify, Pushover). |
| Webhook token | Credenziale del provider (ntfy, Gotify, Pushover); non visualizzata. Non serve per Gextto, Discord e Slack. |
| Webhook user (Pushover) | Solo Pushover: la user key. Non serve per gli altri formati. |

**Discord**

| Impostazione | Cosa fa |
|---|---|
| Discord webhook URL | URL del webhook del canale Discord. Vuoto non usa Discord. |

**Slack**

| Impostazione | Cosa fa |
|---|---|
| Slack webhook URL | URL del webhook in ingresso di Slack. Vuoto non usa Slack. |

**Gotify**

| Impostazione | Cosa fa |
|---|---|
| Gotify URL | Indirizzo del server Gotify (es. https://gotify.esempio); le notifiche vengono inviate a `/message`. |
| Gotify token | Token dell'applicazione Gotify, necessario insieme all'URL. |

**ntfy**

| Impostazione | Cosa fa |
|---|---|
| ntfy URL | Indirizzo del server ntfy (es. https://ntfy.sh). |
| ntfy topic | Argomento su cui pubblicare le notifiche. |
| ntfy token | Token Bearer, solo se l'argomento è protetto. |

**Pushover**

| Impostazione | Cosa fa |
|---|---|
| Pushover token | Token dell'applicazione Pushover. |
| Pushover user key | User o group key del tuo account Pushover. |

**Apprise**

| Impostazione | Cosa fa |
|---|---|
| Apprise URL | Indirizzo di un server Apprise API (es. http://192.168.1.10:8000); le notifiche vengono inviate a `/notify`. |
| Apprise key | Chiave opzionale di una configurazione Apprise; vuota usa quella predefinita. |

**Programma**

| Impostazione | Cosa fa |
|---|---|
| Programma | Eseguibile lanciato su ogni evento, con i dati nelle variabili d'ambiente `GEXTTO_*`. |
| Argomenti del programma | Argomenti passati al programma, separati da spazi; le virgolette tengono insieme quelli con spazi. |

**Eventi**

| Impostazione | Cosa fa |
|---|---|
| Notifiche dei download | Avvisa quando un download parte, viene scartato, si completa o riempie un episodio mancante. |
| Notifiche degli errori | Avvisa quando un download fallisce o un torrent o fumetto va in errore. |
| Notifiche di salute e motore | Avvisa dei problemi di salute, del ciclo di ricerca, degli indexer, del login ai provider e del motore torrent. |
| Notifiche di manutenzione | Avvisa dei riavvii, dei backup completati e della pulizia delle tracce audio. |

**Anti-spam**

| Impostazione | Cosa fa |
|---|---|
| Anti-spam — intervallo minimo | Entro questo numero di minuti lo stesso evento non viene inviato di nuovo. `0` disattiva l'anti-spam; il massimo è `10080` (sette giorni). |

**Lingua dei messaggi**

| Impostazione | Cosa fa |
|---|---|
| Lingua dei messaggi | Lingua delle notifiche; vuota segue la lingua dell'interfaccia. |

**Editor**

| Impostazione | Cosa fa |
|---|---|
| Hook eventi | Esegue un programma su determinati eventi (nome, eventi, programma, argomenti, timeout). |

#### Accesso e servizi

*Login, chiavi API e credenziali dei servizi esterni.*

Gextto è pensato per una LAN fidata: l'accesso è **libero per impostazione
predefinita**. Se lo raggiungi da fuori casa (reverse proxy, port forwarding,
VPN) puoi chiedere il login a chi non è in rete locale.

**Login**

| Impostazione | Cosa fa |
|---|---|
| Richiedi l'accesso (login) | Spento (predefinito): nessun controllo. Acceso: chi non è in rete locale deve fare il login o usare la chiave API. Finché non imposti una password o una chiave resta tutto aperto (e il log lo segnala). |
| Nessun login dalla rete locale | Acceso (predefinito): da 127.0.0.1, 192.168.x.x, 10.x.x.x, 172.16–31.x.x e dagli indirizzi IPv6 locali non serve il login. Dietro un reverse proxy conta l'indirizzo reale del client inoltrato dal proxy (`X-Forwarded-For`, `X-Real-IP`, `Forwarded`), quindi chi arriva da Internet attraverso il proxy deve comunque autenticarsi. Compare solo con «Richiedi l'accesso (login)» attivo. |
| Utente | Nome utente per il login (predefinito `admin`). Compare solo con «Richiedi l'accesso (login)» attivo. |
| Password | Salvata solo come hash bcrypt; cambiarla chiude tutte le sessioni aperte. Lascia il campo vuoto per non modificarla. Compare solo con «Richiedi l'accesso (login)» attivo. |

**Scritture da un'altra origine.** Indipendentemente dal login, Gextto rifiuta
le richieste di modifica che il browser dichiara provenire da un altro sito
(`Origin`/`Referer`, `Sec-Fetch-Site: cross-site`): una pagina malevola non può
riconfigurare Gextto (impostazioni, hook, azioni sui torrent) usando il browser
di chi ha l'interfaccia aperta. Le letture (GET) e i client senza browser
(curl, la TUI) non sono toccati.

**Chiavi API**

| Impostazione | Cosa fa |
|---|---|
| Chiave API (script, TUI, calendario) | Per gli accessi senza browser: header `X-Api-Key: <chiave>` oppure `?apikey=<chiave>` nell'indirizzo. La TUI la legge dalla variabile `GEXTTO_API_KEY`. |
| gx-torrent — token di accesso (pagina e API in LAN) | Nella scheda **Accesso**: segreto condiviso richiesto dalla pagina e dall'API; obbligatorio se il demone ascolta in rete (non visualizzato). |

I due segreti si generano dalla riga stessa: **Genera** crea un valore casuale
lungo e lo salva, **Copia** lo mette negli appunti. Per il token di gx-torrent
serve poi riavviare il demone perché venga applicato. Cosa sono e come si usano
è spiegato in [Chiavi di accesso](api-keys.it.md).

**Servizi metadati**

| Impostazione | Cosa fa |
|---|---|
| TMDB API key (consigliata) | Chiave API TMDB, gratuita: fonte principale di titoli, poster e metadati. Se c'è, ha la precedenza su TVDB. |
| TVDB API key (alternativa a TMDB) | Chiave API v4 di TheTVDB: senza la chiave TMDB fornisce tutti i metadati. |
| TVDB PIN abbonato | PIN dell'abbonamento TheTVDB, richiesto al login solo dalle chiavi «user-supported». |
| Usa TVmaze per le serie | Una volta al giorno chiede a TVmaze (gratuito, senza chiave) lo stato di ogni serie, come seconda opinione dopo TMDB o TVDB. Senza chiavi TMDB e TVDB diventa la fonte delle serie: stagioni, episodi, date, locandine e stato, con titoli e trame in inglese e senza film. A TVmaze vanno gli identificativi TVDB e IMDb della serie e, quando fa da fonte, il suo nome. |

#### Metadati: TMDB, TVDB o TVmaze

Gextto usa i metadati per sapere quanti episodi ha ogni stagione (episodi
mancanti, pack di stagione, completezza), lo stato della serie, il calendario,
i titoli degli episodi nei nomi dei file, le locandine e le schede di serie e
film. Basta una delle due chiavi:

- **TMDB (consigliato)** — chiave gratuita su themoviedb.org → Impostazioni →
  API. Se è impostata, Gextto usa sempre TMDB, anche quando c'è la chiave TVDB.
- **TVDB (alternativa)** — chiave v4 da thetvdb.com → Dashboard → API keys. Senza
  la chiave TMDB fornisce tutte le funzioni elencate sopra, tranne le tendenze e
  i popolari di **Esplora**, che esistono solo su TMDB. Le chiavi di tipo
  «user-supported» richiedono un abbonamento TheTVDB a pagamento: inserisci il
  suo PIN in *TVDB PIN abbonato*. I titoli degli episodi seguono la *Lingua
  TVDB* (es. `ita`); se un episodio non è tradotto resta il titolo originale.

Senza nessuna delle due chiavi Gextto passa da solo a **TVmaze**, gratuito e
senza chiave: stagioni, episodi e date (quindi episodi mancanti, pack e
calendario), la prossima puntata, le locandine e lo stato delle serie. I limiti:
titoli degli episodi e trame sono in inglese, la ricerca per nome trova bene il
titolo originale ma non sempre quello italiano (se la serie ha un ID TVDB,
Gextto la cerca con quello), e non ci sono film né le liste di **Esplora**.
Anche TVmaze si può spegnere con *Usa TVmaze per le serie*: senza nessuna fonte
Gextto scarica comunque, ma gli episodi mancanti si fermano all'ultimo già
scaricato e i file prendono titoli generici («Episodio N»). TMDB e TVDB usano ID diversi: una serie aggiunta
con TVDB conserva l'ID TVDB, e se in seguito imposti la chiave TMDB Gextto
ritrova la serie su TMDB dal nome.

La sessione del browser dura 30 giorni; `/logout` la chiude. Dopo cinque
password sbagliate dallo stesso indirizzo i tentativi vengono bloccati per un
minuto e ogni errore è annotato nel log. Se resti chiuso fuori, avvia Gextto con
la variabile d'ambiente `GEXTTO_AUTH_DISABLE=1`: il controllo è spento finché la
variabile è presente e puoi correggere le impostazioni. Per l'accesso da fuori
casa usa comunque HTTPS (reverse proxy con certificato o VPN), altrimenti la
password viaggia in chiaro.


#### Diagnostica e aggiornamenti

*Log dettagliati e controllo degli aggiornamenti.*

**Diagnostica**

| Impostazione | Cosa fa |
|---|---|
| Debug (log dettagliati) | Attiva log dettagliati e diagnostiche periodiche per il debug. |

**Aggiornamenti**

| Impostazione | Cosa fa |
|---|---|
| Controlla gli aggiornamenti | Scarica periodicamente da GitHub le informazioni sull'ultima build (ogni quante ore lo decide «Intervallo controllo aggiornamenti») e, se ce n'è una nuova, mostra «Aggiornamento disponibile». Non invia dati e non aggiorna da solo: l'aggiornamento parte solo da Manutenzione. |
| Intervallo controllo aggiornamenti | Ogni quante ore Gextto controlla se è uscita una nuova build (da 1 a 720, default 6). Il controllo scarica solo un piccolo file da GitHub; «Verifica aggiornamenti» in Manutenzione controlla subito. |

## Appendice B. Riferimento — Integrazioni

La pagina è divisa in gruppi — **Servizi** (Simkl), **Media server** (Jellyfin,
Plex), **Sorgenti** (indexer, FlareSolverr, verifica sorgenti) e
**Collegamenti** (calendario iCal, handler del browser) — con il titolo del
gruppo agganciato in alto mentre scorri.

### Simkl

| Campo / azione | Cosa fa |
|---|---|
| ID client | ID client dell'app Simkl. |
| Giorni calendario | Quanti giorni avanti mostrare nel calendario Simkl. |
| Stato watchlist | Stato assegnato alle serie importate: Da guardare, In visione o Completato. |
| Segna come visto | Segna come visti su Simkl gli episodi scaricati. |
| Avvia accesso / Conferma | Avvia il flusso PIN e conferma il codice mostrato da Simkl. |
| Revoca | Revoca l'accesso e rimuove il token salvato. |
| Importa watchlist | Importa le serie della watchlist Simkl nella libreria. |
| Watchlist / Calendario | Tabelle di sola lettura con la watchlist e le prossime uscite. |

### Jellyfin

| Campo / azione | Cosa fa |
|---|---|
| Jellyfin URL | URL del server Jellyfin (es. `http://127.0.0.1:8096`). |
| Jellyfin API key | API key generata in Jellyfin → Dashboard → API Keys. |
| Jellyfin — mappatura percorsi | Solo se Jellyfin vede la libreria con percorsi diversi (es. Docker): una riga per cartella, `percorso_gextto=percorso_jellyfin`. |
| Test connessione | Verifica che Jellyfin risponda. |
| Aggiorna libreria | Chiede a Jellyfin di aggiornare tutta la libreria. |

### Plex

| Campo / azione | Cosa fa |
|---|---|
| Plex URL | URL del server Plex (es. `http://127.0.0.1:32400`). |
| Plex token | Token `X-Plex-Token` per accedere alla libreria. |
| Plex — mappatura percorsi | Solo se Plex vede la libreria con percorsi diversi (es. Docker): una riga per cartella, `percorso_gextto=percorso_plex`. |
| Test connessione | Verifica che Plex risponda. |
| Aggiorna libreria | Chiede a Plex di aggiornare tutta la libreria. |

Dopo ogni importazione Gextto chiede a Jellyfin e Plex di rileggere **solo la
cartella cambiata** (la stagione o il film appena arrivati), non tutta la
libreria: sul NAS si evita una scansione completa per ogni episodio e il file
compare in pochi secondi. Per Plex la cartella deve stare dentro una delle
cartelle di una sua libreria. Se la richiesta mirata non è possibile (percorso
non riconosciuto, server che la rifiuta) Gextto chiede l'aggiornamento completo
come prima. Se il server gira in Docker e vede i file sotto un altro percorso,
compila la mappatura percorsi.

### Calendario iCal

`/feed/calendar.ics` è un calendario a cui iscriversi da Thunderbird, Google
Calendar, Apple Calendar o dal telefono (link in *Integrazioni → Calendario
iCal*). Le serie arrivano in italiano spesso mesi o anni dopo la prima messa in
onda, quindi il calendario distingue tre cose:

| Voce | Cosa indica |
|---|---|
| 📥 Serie S01E04 | episodio o film **arrivato in libreria**, nel giorno in cui Gextto l'ha scaricato (ultimi 30 giorni). È l'unica data che dice quando è davvero disponibile. |
| 📺 Serie S02E03 · Titolo | **prima messa in onda originale** (TMDB, o TVDB) della stagione in corso, dall'ultima settimana ai prossimi due mesi. Non è la data della versione italiana; ✓ se l'episodio è già in libreria. |
| 🎬 Film | uscita del film monitorato **in Italia** (TMDB: digitale, poi home video, poi cinema; il paese segue la lingua TMDB, es. `it-IT`). Se per l'Italia non c'è ancora una data compare quella originale, indicata come «(uscita originale)». Con la sola chiave TVDB compare la prima uscita del film, sempre come «(uscita originale)». |

Serve la chiave TMDB oppure quella TVDB; il calendario si aggiorna al massimo ogni 30 minuti. Con
l'accesso protetto (vedi *Accesso*) aggiungi `?apikey=<chiave>` all'indirizzo.

### Indexer Torznab

| Campo / azione | Cosa fa |
|---|---|
| Nome | Etichetta dell'indexer (es. `jackett` / `prowlarr` / `indexarr`). |
| URL base | URL base del servizio; Gextto aggiunge il percorso Torznab. |
| API key | Chiave API dell'indexer. |
| Tipo | Rilevato automaticamente, Prowlarr, Jackett o indexarr. |
| Attivo | Abilita o disabilita l'indexer. |
| Verifica | Testa l'indexer (per Jackett e indexarr usa `t=caps`). |

Un indexer Torznab **diretto** (non gestito da Jackett, Prowlarr o indexarr), per esempio il servizio
`mircrew-indexer` sulla stessa macchina o in LAN, si aggiunge qui con l'indirizzo
base (es. `http://192.168.1.10:9118`) o con quello che finisce in `/api`: Gextto
usa `<base>/api`, la stessa regola di gx-torrent, quindi l'indirizzo da scrivere
è identico nei due. Le ricerche automatiche hanno un budget di **100 secondi per titolo**: un
indexer dietro Prowlarr può attendere fino a **90 secondi** (Prowlarr serve una
ricerca alla volta per indexer), un indexer diretto o dietro Jackett fino a **45**,
indexarr fino a **90** (una ricerca per titolo su tutto il manager). Una
lane la cui coda supererebbe il budget viene saltata senza contare come errore,
così un tracker lento non ferma il ciclo. Se un indexer su un indirizzo locale o di rete privata chiude la
connessione o non risponde, Gextto **non** ritenta via FlareSolverr (Cloudflare
non protegge un servizio in LAN): l'errore resta visibile così com'è. Per
`mircrew-indexer` *Salute → Stato provider* mostra se il servizio risponde e se
il suo login al forum MirCrew è riuscito; se non lo è, rifai il login dalla web
UI del servizio.

Gextto legge dal manager l'elenco degli indexer e le loro capacità (le *caps*
Torznab; per Prowlarr l'elenco dei suoi indexer, riletto ogni 10 minuti) e invia
solo i parametri che il singolo indexer dichiara. Per esempio l'aggregato «all» di Jackett non
accetta l'ID TMDB nelle ricerche di serie e film, e gli indexer pubblici di
Prowlarr non cercano per ID: in questi casi la ricerca usa titolo, stagione ed
episodio invece di fallire o tornare vuota. Se un indexer rifiuta comunque un
ID, la ricerca viene ripetuta subito senza; l'errore mostrato in *Salute* riporta
la spiegazione dell'indexer (per esempio «HTTP 400: Torznab 100: Invalid API
Key») invece del solo codice HTTP.

### FlareSolverr

| Campo / azione | Cosa fa |
|---|---|
| URL | URL del servizio FlareSolverr usato per superare Cloudflare. |
| Test FlareSolverr | Verifica che FlareSolverr risponda. |

### Hook eventi (Configurazione → Notifiche)

| Campo / azione | Cosa fa |
|---|---|
| Nome | Etichetta dell'hook. |
| Attivo | Abilita o disabilita l'hook. |
| Eventi | Eventi che attivano l'hook (vuoto = tutti). |
| Programma | Eseguibile da lanciare (senza shell). |
| Argomenti | Argomenti con segnaposto `{title}`, `{hash}`, `{path}`, `{series}`, `{episode}`; ogni segnaposto resta un solo argomento anche con spazi. Gli stessi valori sono variabili `GEXTTO_*`. |
| Timeout (s) | Timeout in secondi (default 60, massimo 24 ore; `0` = default). |

### Handler del browser e verifica sorgenti

| Voce | Cosa fa |
|---|---|
| Gestore magnet / torrent | Scarica gli script per aprire magnet e file `.torrent` direttamente in Gextto. |
| Magnet / Torrent `.desktop` | Versioni `.desktop` per l'integrazione con il desktop Linux. |
| `install.sh` | Scarica lo script di installazione. |
| Verifica sorgenti / Aggiorna | Controlla feed, indexer e motori web; con una ricerca misura anche i risultati, senza cambiare le impostazioni. |

### Estensione del browser (client qBittorrent)

Gextto risponde a un sottoinsieme dell'API di qBittorrent (`/api/v2`) proprio
sull'indirizzo dell'interfaccia, così un'estensione scritta per qBittorrent —
per esempio *Torrent Control* per Firefox — manda magnet e file `.torrent` a
Gextto: il torrent entra in coda e lo scarica il motore attivo (gx-core per
default), come se fosse stato aggiunto dall'interfaccia.

Nell'estensione si configura un server con questi valori:

| Campo | Valore |
|---|---|
| Client BitTorrent | qBittorrent (versione corrente) |
| Indirizzo del server | `http://<indirizzo-di-gextto>:5000/` (la porta dell'interfaccia, non quella di gx-torrent) |
| Utente / Password | quelli dell'accesso protetto, se è attivo; altrimenti si possono lasciare vuoti |
| Cartella di scaricamento | vuota per usare la cartella configurata, oppure un percorso accettato dal motore |
| Etichetta / Categoria | vuota: categorie, tag e layout del contenuto diversi da *Original* sono rifiutati con un errore esplicito, perché in Gextto cambierebbero i tag della coda e le cartelle su disco |

Quando l'accesso protetto è attivo, l'estensione fa il login con le stesse
credenziali dell'interfaccia e riceve un cookie di sessione valido solo per
`/api/v2`: il resto dell'interfaccia resta protetto.

## Appendice C. Riferimento — Manutenzione

### Azioni rapide

| Azione | Cosa fa |
|---|---|
| Backup ora | Crea subito uno snapshot di backup dei database. |
| Pulisci trash | Svuota completamente il cestino (azione forzata: `trash_retention_days` non si applica). |
| Ricalcola punteggi | Ricalcola lo score delle release archiviate con i pesi attuali. |
| Scansiona archivi | Rilegge le cartelle archivio e aggiorna la libreria. |
| Aggiorna MediaInfo | Analizza con `ffprobe` i file archiviati senza MediaInfo. |
| Rinomina tutto | Rinomina tutti i file archiviati secondo il formato configurato. |
| Housekeeping | Pulizia dei dati tecnici e dello storico, senza toccare la libreria. |
| Riavvia servizio | Riavvia il daemon Gextto. |

### Rinomina contenuto cartella

| Voce | Cosa fa |
|---|---|
| Cartella / Sfoglia | Inserisci o scegli dal server la cartella da analizzare. |
| Scansiona e proponi | Analizza ricorsivamente i video, riconosce serie/film con TMDB/TVDB e propone i nuovi nomi (solo anteprima), con il formato scelto per episodi e film. I tag degli episodi vengono dal nome del file, quelli dei film dal file stesso (MediaInfo); un film che segui prende il suo ID TMDB, altrimenti conta anche l'anno nel nome. |
| Scelta risultato per riga | Seleziona un risultato TMDB/TVDB alternativo. |
| Accetta / rifiuta | Approva o scarta la singola proposta. |
| Accetta tutte le proposte | Approva in blocco le proposte. |
| Applica selezionate | Rinomina i file selezionati realmente presenti e con destinazione valida. |
| Progresso rinomina | Avanzamento dell'operazione in background. |

### Libreria

| Voce | Cosa fa |
|---|---|
| Duplicati video — Anteprima duplicati | Elenca i duplicati inferiori (risoluzione più bassa) senza eliminare nulla. |
| Duplicati video — Pulisci duplicati | Sposta nel cestino le copie inferiori trovate. |
| Ottimizzazione database — VACUUM / ANALYZE | Compatta i database e aggiorna le statistiche del query planner. |
| Ottimizzazione database — Aggiorna dimensioni | Rilegge dimensioni e stato dei file database. |
| Ottimizzazione database — Verifica integrità e indici | Controlla integrità, chiavi esterne e indici (compreso FTS dell'archivio). |
| RAM disk | Mostra i `tmpfs`/`ramfs` scrivibili, permette di sceglierne uno, crearne uno dedicato e aggiornare l'elenco. |

### Pulizie

| Voce | Cosa fa |
|---|---|
| Cestino — Apri cestino | Mostra gli elementi nel cestino per eliminarli singolarmente. |
| Cestino — Svuota cestino | Elimina tutti gli elementi del cestino e mostra un avviso con i file rimossi e lo spazio liberato. |
| Pulizia database — Cicli da conservare | Numero di statistiche dei cicli da mantenere. |
| Pulizia database — Giorni errori | Conserva le schede dei torrent in errore per almeno N giorni prima di rimuoverle. |

### Log

| Voce | Cosa fa |
|---|---|
| Log gx-torrent | Ultime righe del log del motore torrent (`gx-torrent/gx-torrent.log`), con filtro testo, solo avvisi/errori, numero di righe e scelta tra file attivo e rotazioni. |

### Backup

| Voce | Cosa fa |
|---|---|
| Backup ora | Crea subito un archivio di backup, senza aspettare l'intervallo o l'orario programmato. Lo stesso comando è tra le Azioni rapide. Mentre lavora mostra un avviso e resta disattivato. |
| Salva impostazioni | Salva le impostazioni di backup della sezione (conservazione, programmazione, FTP, cartella cloud, percorsi extra, Telegram): non crea nessun archivio. |
| Backup da conservare | Numero di file ZIP locali da conservare nella cartella `backups`. |
| Intervallo (ore) | Intervallo del backup automatico in ore (0 disattiva l'intervallo). |
| Orario (HH:MM) | Orario locale giornaliero del backup automatico; se impostato ha la precedenza sull'intervallo. |
| FTP host | Host/indirizzo del server FTP (vuoto = nessun invio FTP). |
| FTP utente | Utente del server FTP. |
| FTP password | Password FTP (non visualizzata; vuoto = mantiene quella salvata). |
| FTP percorso | Cartella remota di destinazione dello ZIP. |
| Cartella cloud | Percorso locale di una cartella già sincronizzata da un servizio cloud. |
| File e cartelle da includere | Percorsi assoluti di file o cartelle da aggiungere allo ZIP (uno per riga). Finiscono nella cartella `extra/` dell'archivio, con il loro percorso originale; i percorsi che non esistono più vengono saltati. |
| Invia su Telegram | Invia una copia su Telegram usando il bot configurato. |
| Test FTP | Verifica connessione, login, percorso remoto, upload di prova e rimozione. |
| Backup disponibili / Verifica | Tabella degli snapshot con etichetta, nome, dimensione e data; il pulsante Verifica controlla l'archivio, avvisa mentre lavora e conferma quando lo ZIP è integro. |
