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
| 16 | in coda | Ricontrollare tutta l’interfaccia e riallinearla completamente a come era in `rextto`, non limitandosi a correzioni isolate. | Obiettivo generale della revisione completa. |

La coda operativa completa resta mantenuta anche in
[`ui-feedback-queue.md`](ui-feedback-queue.md).
