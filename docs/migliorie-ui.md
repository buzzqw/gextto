# Migliorie UI

Elenco ordinato delle migliorie richieste durante la revisione della nuova UI.
L’ordine numerico è quello di arrivo degli appunti.

| # | Stato | Miglioria | Note/risultato |
|---:|---|---|---|
| 1 | completato | Rimuovere dalla Dashboard la voce ridondante “Stato daemon”, già indicata dal pallino colorato in alto a destra. | Rimossa dalla griglia delle metriche. |
| 2 | completato | Rendere “Prossime uscite” come in `rextto`, evitando la resa attuale a elenco semplice. | Sostituita con elenco compatto a schede, poster, episodio e data. |
| 3 | completato | Rendere “Ultimi download” come in `rextto`. | Sostituita la tabella con elenco compatto a schede, poster, episodio, data e dimensione. |
| 4 | completato | In Scarico, mantenere i pulsanti della colonna Azioni su una sola riga. | Azioni non-wrappabili; la tabella può scorrere orizzontalmente. |
| 5 | completato | Rifare il blocco “Aggiungi torrent” come in `rextto`, su una sola riga. | Magnet, file, percorso e pulsante sono sulla stessa riga desktop; opzioni sotto. |
| 6 | completato | Verificare perché sono stati aggiunti alla lista torrent due elementi apparentemente duplicati: `I.Delitti.Del.BarLume.S07.ITA.720p.NOW.WEB-DL.x264-UBi`. | Causa: per un season pack `processed_path` è una cartella; la guardia di completamento la trattava come "copia archiviata assente" e ri-processava/ri-archiviava il pack a ogni spostamento di storage. `tevArchivedCopyPresent` ora accetta anche una cartella non vuota; test `TestTevArchivedCopyPresent`. |
| 7 | completato | Copiare da `rextto` la gestione dei limiti temporanei di download e upload. | Barra "Limite temporaneo" nella sessione: DL/UL KiB/s, minuti, Applica e Rimuovi, con i valori correnti prefillati e messaggio inline. |
| 8 | completato | Rifare la schermata per taggare i file in Scarico, copiandola da `rextto`. | Filtro per tag, selezione multipla, assegna/rimuovi tag con "Nuovo tag…", chip sotto il nome, tag per singolo torrent nel dettaglio. |
| 9 | completato | Rivedere completamente la schermata di Scarico, attualmente percepita come una copia povera di `rextto`. | Tabella con colonne ordinabili, ETA, badge NAS/seed ∞, riga origine, barre colorate; modale dettaglio a schede Generale/Tracker/Contenuto/Peers/Limiti/Storage; pulisci completati e auto-remove. |
| 10 | completato | Riorganizzare il menu Serie TV nello spirito della schermata equivalente di `rextto`. | Ricerca/aggiunta in alto, elenco da `/api/config/library` con filtro, colonne Ep./Complet./Ultimo/Stato, azioni Pausa/Attiva/Elimina; azioni serie spostate nel dettaglio. |
| 11 | completato | Riorganizzare il menu Film come in `rextto`, portando in alto le opzioni di aggiunta. | Ricerca/aggiunta in alto, elenco con filtro, colonne ordinabili Nome/Anno/Qualità/Lingua e azioni Pausa/Attiva/Elimina. |
| 12 | completato | Verificare e ripristinare in Mancanti la possibilità di avviare una ricerca, presente in `rextto`. | Aggiunta azione "Cerca" per riga (compila e invia la ricerca release) e "Ignora"; i risultati restano nel pannello con Accoda. |
| 13 | completato | Rifare completamente il menu Esplora, verificandolo e riallineandolo a `rextto`. | Aggiunti tab Di tendenza TMDB (Serie TV/Film, settimana/oggi/popolari/votati/programmazione/uscite), Calendario TMDB, ricerca TMDB e ricerca release. |
| 14 | completato | In Archivio, mantenere su una sola riga le righe con l’elenco dei file. | Titolo troncato con ellissi e tooltip; azioni non-wrappabili; tabella scorrevole. |
| 15 | completato | Rivedere la sezione Fumetti confrontandola con l’organizzazione di `rextto`. | Ordine: Aggiungi fumetto, Monitorati, grid-2 Weekly pack, grid-2 Download/Storico, Link trovati; rimossa la nota obsoleta sul legacy. |
| 16 | in lavorazione | Ricontrollare tutta l’interfaccia e riallinearla completamente a come era in `rextto`, non limitandosi a correzioni isolate. | Passaggio completo: Configurazione a righe raggruppate con salva-tutte, Integrazioni e Manutenzione in pannelli grid-2, Esplora con tendenze+calendario, dashboard con feed-sorgenti, log con filtro/follow, Scarico con dettaglio a schede. Verifica strutturale 16/16 pagine OK. Restano ritocchi cosmetici (sparkline di rete, download HTTP fusi nella tabella Scarico, pannello servizi in Salute). |
| 17 | completato | Configurazione, Integrazioni e Manutenzione: le opzioni a “blocchetti” sono poco gradevoli; copiare da `rextto`. | Configurazione a righe raggruppate con barra “Salva tutte”; Integrazioni e Manutenzione in pannelli grid-2. |
| 18 | completato | Salute: mostrare nel box dello stato anche il motivo del “degraded”; l’uptime di sistema è grande e quello di Gextto piccolo, va invertito. | Aggiunto il motivo (cartella dati/percorsi); “Uptime Gextto” grande con sistema in piccolo. |
| 19 | completato | Salute: “Stato sorgenti” e “Stato provider” troppo scarni. | Aggiunti tipo/risultati/esito/dettaglio, ricerca query e azione “Azzera” per provider. |
| 20 | completato | Manutenzione: “Porte” inutile da togliere; RAM disk con selezione e uso come `rextto`; rivedere tutta la maschera. | Rimossa Porte; RAM disk con percorsi candidati e “Usa questo percorso”; Duplicati (anteprima/pulizia), DB, trash, backup, diagnostica. |
| 21 | completato | Integrazioni: rivedere Trakt/Simkl e aggiungere Jellyfin, Plex e indexer come `rextto`. | Pannelli grid-2 Trakt/Simkl; configurazione Jellyfin/Plex (URL + chiave); editor indexer e FlareSolverr. |
| 22 | completato | Scarico: nello Storico download il “Tag NAS” è sempre vuoto. | Badge `NAS` quando esiste una copia archiviata, più il tag se presente. |
| 23 | completato | Dashboard Cerca: prima i risultati dell’archivio, poi indexer/web; dare più spazio al nome e togliere Codec. | Due fasi (archivio immediato via `/api/search/archive`, poi ricerca completa); colonne Release/Sorgente/Risoluzione/Azioni. |
| 24 | completato | `I.Delitti.Del.BarLume.S07…` e `I.Delitti.Del.BarLume.6x02…` scaricati pur essendo duplicati. | S07: upgrade reale 480p→720p. 6x02: falso upgrade per il parser che leggeva `WEBRip` come `WEB-DL`; corretto con test. |
| 25 | verificato | Confermare che Gextto usa `ffprobe`/`mediainfo` come `rextto` per analisi periodica e archivio nel DB. | Sì: `ffprobe` per MediaInfo (al completamento + backfill periodico, 60 min di default), `mediainfo` per i tag in post-processing; salvato in `media_info_json` e usato nei confronti. I film importati senza link `torrent_meta` restano senza MediaInfo (0/40). |

La coda operativa completa resta mantenuta anche in
[`ui-feedback-queue.md`](ui-feedback-queue.md).
