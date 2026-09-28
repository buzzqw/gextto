# Coda appunti UI

Documento di lavoro per raccogliere gli appunti e le correzioni comunicati
dall'utente durante la revisione della UI.

## Regole

- Gli appunti vengono aggiunti in fondo, rispettando l'ordine di arrivo.
- Ogni appunto mantiene un numero progressivo e non viene cancellato quando
  viene elaborato.
- Stati ammessi: `in coda`, `in lavorazione`, `completato`, `bloccato`.
- Un appunto può essere marcato `completato` solo dopo verifica del risultato.
- Gli appunti successivi non modificano l'ordine di lavorazione.

## Coda

| # | Stato | Appunto | Note/risultato |
|---:|---|---|---|
| 1 | completato | In Dashboard, "Stato daemon" è ridondante perché lo stato è già indicato dal pallino colorato in alto a destra. | Rimosso dalla griglia delle metriche. |
| 2 | completato | Controllare in `rextto` come viene resa la sezione "Prossime uscite" nella Dashboard: la resa attuale a elenco è poco gradevole. | Sostituita la tabella con elenco compatto a schede, poster, episodio e data, ispirato a `rextto`. |
| 3 | completato | Controllare in `rextto` come viene resa la sezione "Ultimi download" nella Dashboard: la resa attuale è poco gradevole. | Sostituita la tabella con elenco compatto a schede, poster, episodio, data e dimensione. |
| 4 | completato | In Scarico, i pulsanti della colonna Azioni vanno su due righe; devono restare su una sola riga. | Azioni rese non-wrappabili; la tabella può scorrere orizzontalmente. |
| 5 | completato | Rifare il blocco "Aggiungi torrent" come in `rextto`, disposto su una sola riga. | Magnet, file, percorso e pulsante sono sulla stessa riga desktop; le opzioni restano sotto. |
| 6 | completato | Verificare con attenzione perché sono stati aggiunti alla lista torrent due elementi con nome `I.Delitti.Del.BarLume.S07.ITA.720p.NOW.WEB-DL.x264-UBi` (apparentemente duplicati). | Causa: per un season pack `processed_path` è una cartella e la guardia di completamento la trattava come "copia archiviata assente", ri-processando/ri-archiviando il pack a ogni spostamento di storage. Ora `tevArchivedCopyPresent` accetta anche una cartella non vuota; test `TestTevArchivedCopyPresent`. |
| 7 | in coda | Copiare da `rextto` la gestione dei limiti temporanei di download e upload; la resa attuale è poco gradevole. | — |
| 8 | in coda | Rifare la schermata per taggare i file in Scarico, copiandola da `rextto`: la resa attuale è troppo povera. | — |
| 9 | in coda | Rivedere completamente la schermata di Scarico: nel complesso sembra una copia povera di quella di `rextto`. | — |
| 10 | in coda | Rivedere e riorganizzare il menu "Serie TV" nello spirito della schermata equivalente di `rextto`. | — |
| 11 | in coda | Rivedere e riorganizzare il menu "Film" come in `rextto`; le opzioni di aggiunta non devono restare in fondo alla pagina. | — |
| 12 | in coda | Ricontrollare la sezione "Mancanti": in `rextto` era disponibile anche la possibilità di avviare una ricerca. | — |
| 13 | in coda | Rifare completamente il menu "Esplora", verificandolo e riallineandolo a `rextto`. | — |
| 14 | in coda | In "Archivio", le righe con l’elenco dei file devono restare su una sola riga. | — |
| 15 | in coda | Rivedere anche la sezione "Fumetti", confrontandola con l’organizzazione di `rextto`. | — |
| 16 | in coda | Ricontrollare tutta l’interfaccia e riallinearla completamente a come era in `rextto`, non limitandosi a correzioni isolate. | Obiettivo generale della revisione completa. |
