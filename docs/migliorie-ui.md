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
| 7 | in coda | Copiare da `rextto` la gestione dei limiti temporanei di download e upload. | — |
| 8 | in coda | Rifare la schermata per taggare i file in Scarico, copiandola da `rextto`. | — |
| 9 | in coda | Rivedere completamente la schermata di Scarico, attualmente percepita come una copia povera di `rextto`. | — |
| 10 | in coda | Riorganizzare il menu Serie TV nello spirito della schermata equivalente di `rextto`. | — |
| 11 | in coda | Riorganizzare il menu Film come in `rextto`, portando in alto le opzioni di aggiunta. | — |
| 12 | in coda | Verificare e ripristinare in Mancanti la possibilità di avviare una ricerca, presente in `rextto`. | — |
| 13 | in coda | Rifare completamente il menu Esplora, verificandolo e riallineandolo a `rextto`. | — |
| 14 | in coda | In Archivio, mantenere su una sola riga le righe con l’elenco dei file. | — |
| 15 | in coda | Rivedere la sezione Fumetti confrontandola con l’organizzazione di `rextto`. | — |
| 16 | in coda | Ricontrollare tutta l’interfaccia e riallinearla completamente a come era in `rextto`, non limitandosi a correzioni isolate. | Obiettivo generale della revisione completa. |

La coda operativa completa resta mantenuta anche in
[`ui-feedback-queue.md`](ui-feedback-queue.md).
