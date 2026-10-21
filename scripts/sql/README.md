# Comandi SQL di manutenzione

Script pronti all'uso per l'**Archivio release** di Gextto, la tabella
`archive` nel database `gextto_archive.db`. Sono anche la sorgente del pannello
**Manutenzione → tab Manutenzione SQL dell'archivio**: la UI usa una copia
inclusa nel binario, mostra le operazioni, esegue prima l'anteprima e solo dopo
propone l'eliminazione.

> **Attenzione**: i file `*.delete.sql` cancellano righe in modo definitivo.
> Controlla sempre l'anteprima prima di eseguirli.

## Dove sta il database

Il percorso dipende da `GEXTTO_DATA_DIR`:

| Installazione | Percorso |
|---|---|
| Servizio di questa macchina | `/home/andres/gextto-data/gextto_archive.db` |
| Default del codice (`GEXTTO_DATA_DIR` non impostata) | `data/gextto_archive.db` |
| Installer di sistema | in genere `/var/lib/gextto/gextto_archive.db` |

Controlla il valore reale con:

```bash
systemctl --user show gextto -p Environment
```

Negli esempi qui sotto usa `$DB` per il percorso:

```bash
DB=/home/andres/gextto-data/gextto_archive.db
```

## Come funziona il pannello

In **Manutenzione** il tab **Manutenzione SQL dell'archivio** contiene il
pannello delle operazioni:

1. scegli la **lingua per le operazioni** dal selettore in cima (vale per le
   operazioni che analizzano o eliminano una lingua);
2. scegli un'operazione e premi **Anteprima** (o **Esegui** per le operazioni
   di sola lettura);
3. mentre la query è in corso compare un indicatore; al termine il pannello
   mostra il numero di righe corrispondenti e le prime 200 e scorre al
   risultato;
4. per le operazioni che modificano l'archivio compare **Elimina
   definitivamente**, con una conferma;
5. dopo l'eliminazione il pannello mostra quante righe sono state rimosse e
   rilegge l'archivio.

Non è una console SQL libera: la UI esegue solo i file inclusi in questa
cartella. Le anteprime devono essere una sola `SELECT`/`WITH ... SELECT`; le
eliminazioni una sola `DELETE` (anche introdotta da `WITH`). I comandi che
cambiano lo schema o allegano altri database sono rifiutati.

## Formato dei file

Ogni operazione è una coppia di file:

- `nome.select.sql` — obbligatorio, una sola query di lettura;
- `nome.delete.sql` — opzionale, una sola `DELETE`.

Se il file `.delete.sql` manca, l'operazione è di sola lettura. Il pannello
mostra il titolo, la descrizione e la categoria presi dall'intestazione del
file `.select.sql`:

```sql
-- gextto:title=Release più vecchie di 6 mesi
-- gextto:description=Mostra e cancella le release entrate in archivio da più di 6 mesi.
-- gextto:category=Età
-- gextto:order=10
SELECT ...
```

`order` decide l'ordine delle operazioni; la categoria crea i gruppi nel
pannello. Le anteprime non devono contenere `LIMIT`: il pannello applica un
limite di 200 righe e calcola il totale prima del limite.

Le operazioni in lingua aggiungono `param=language` e usano i parametri
`:tokens` e `:raw`, riempiti dal motore con i marcatori della lingua scelta:

```sql
-- gextto:param=language
...
EXISTS (SELECT 1 FROM json_each(:tokens) m WHERE t LIKE '% ' || lower(m.value) || ' %')
OR EXISTS (SELECT 1 FROM json_each(:raw) m WHERE raw LIKE '%' || lower(m.value) || '%')
```

Il file `.delete.sql` della stessa operazione deve usare gli stessi parametri.

I file sono inclusi nel binario con `go:embed`: per aggiungere o modificare
un'operazione serve una build di Gextto.

## Operazioni incluse

| Operazione | Tipo | Cosa fa |
|---|---|---|
| Release più vecchie di 6 mesi | Elimina | Release con `added_at` oltre 6 mesi |
| Release più vecchie di 1 anno | Elimina | Release con `added_at` oltre 1 anno |
| Release nella lingua selezionata più vecchie di 6 mesi | Elimina | Release con il marcatore della lingua scelta |
| Release non nella lingua selezionata più vecchie di 6 mesi | Elimina | Release senza il marcatore della lingua scelta |
| Release nella lingua selezionata più vecchie di 1 anno | Elimina | Come sopra, oltre 1 anno |
| Release non nella lingua selezionata più vecchie di 1 anno | Elimina | Come sopra, oltre 1 anno |
| Titoli vuoti | Elimina | Righe con titolo vuoto |
| Distribuzione per età | Sola lettura | Conteggio per fascia di età |
| Release per fonte | Sola lettura | Conteggio per fonte, con la più vecchia e la più recente |
| Stato dell'archivio | Sola lettura | Controlli su titoli, magnet, hash e indice full-text |
| Titoli duplicati | Sola lettura | Titoli ripetuti e numero di copie |

## Come eseguire una select da terminale

```bash
sqlite3 -header -column "$DB" < scripts/sql/archive_older_than_6_months.select.sql
```

Per le operazioni in lingua il file usa `:tokens` e `:raw`: vanno impostati
prima di leggere lo script. Esempio per l'italiano:

```bash
sqlite3 "$DB" \
  ".parameter set :tokens '[\"ita\",\"italian\",\"italiano\",\"italiana\",\"italia\"]'" \
  ".parameter set :raw '[\"[it]\",\"(it)\",\"it+\",\"it|\"]'" \
  ".read scripts/sql/archive_language_older_than_6_months.select.sql"
```

Le operazioni in lingua usano `json_each`: da terminale serve SQLite 3.38 o
successivo. La UI usa la versione inclusa in Gextto.

Per limitare l'output, aggiungi `LIMIT 100` in fondo alla query in una copia
del file: il pannello della UI applica già il limite di 200 righe.

Per esportare in CSV:

```bash
sqlite3 -csv "$DB" < scripts/sql/archive_older_than_6_months.select.sql > vecchi-6-mesi.csv
```

## Come viene riconosciuta la lingua

La tabella `archive` **non ha una colonna lingua**: il titolo della release è
l'unica informazione disponibile. Il motore costruisce i marcatori della lingua
scelta e li passa allo script come due array JSON:

- `:tokens` contiene le parole cercate come token dopo la normalizzazione dei
  separatori (`. _ - [ ] ( ) { } + / |` → spazi);
- `:raw` contiene i marcatori cercati direttamente nel titolo minuscolo, perché
  forme come `[it]` non sono distinguibili dalla parola inglese “it” dopo la
  normalizzazione.

La lingua selezionata è considerata presente se almeno un token o un marcatore
compare nel titolo. Le lingue disponibili nel selettore sono:

| Codice | Lingua | Esempi di marcatori |
|---|---|---|
| `ita` | Italiano | `ita`, `italian`, `italiano`, `italiana`, `italia`, `[it]` |
| `eng` | English | `eng`, `english`, `[en]` |
| `fra` | Français | `fra`, `french`, `francais`, `vff`, `vostfr`, `[fr]` |
| `deu` | Deutsch | `ger`, `deu`, `german`, `deutsch`, `[de]` |
| `spa` | Español | `spa`, `esp`, `spanish`, `espanol`, `español`, `[es]` |
| `por` | Português | `por`, `portuguese`, `portugues`, `português`, `pt`, `[pt]` |
| `rus` | Русский | `rus`, `russian`, `[ru]` |
| `jpn` | 日本語 | `jpn`, `japanese`, `[ja]` |
| `kor` | 한국어 | `kor`, `korean`, `[ko]` |
| `zho` | 中文 | `chi`, `zho`, `chinese`, `[zh]` |
| `pol` | Polski | `pol`, `polish`, `[pl]` |
| `nld` | Nederlands | `dut`, `nld`, `dutch`, `[nl]` |
| `swe` | Svenska | `swe`, `swedish`, `[sv]` |
| `nor` | Norsk | `nor`, `norwegian`, `[no]` |
| `dan` | Dansk | `dan`, `danish`, `[da]` |
| `fin` | Suomi | `fin`, `finnish`, `[fi]` |
| `tur` | Türkçe | `tur`, `turkish`, `[tr]` |

Le release **senza** il marcatore della lingua scelta rientrano nell'elenco
“non nella lingua selezionata”, comprese quelle che non dichiarano la lingua
nel titolo. Per questo conviene sempre guardare l'anteprima prima di
cancellare.

La regola è volutamente più semplice di quella del parser Go: SQLite non ha le
espressioni regolari usate da `ParseQuality`. Se in futuro la tabella acquisirà
una colonna lingua, gli script andranno aggiornati per usarla.

## Prima di cancellare

1. **Fai un backup** del database:

   ```bash
   sqlite3 "$DB" ".backup '/percorso/gextto_archive-$(date +%F).db'"
   ```

2. **Esegui prima l'anteprima** dal pannello o la select da terminale.

3. **Ferma il servizio** se la cancellazione è grande:

   ```bash
   systemctl --user stop gextto
   ```

   Puoi anche lasciarlo attivo: il pannello usa una transazione e un timeout,
   ma su decine di migliaia di righe è più prevedibile fermarlo.

4. Esegui l'eliminazione dal pannello oppure da terminale:

   ```bash
   sqlite3 "$DB" < scripts/sql/archive_older_than_6_months.delete.sql
   ```

5. Se avevi fermato il servizio, riavvialo:

   ```bash
   systemctl --user start gextto
   ```

## Dopo la cancellazione

Il `DELETE` libera spazio logico ma **non riduce subito il file** su disco.
Per compattarlo (serve spazio libero pari alla dimensione del database):

```bash
sqlite3 "$DB" "VACUUM;"
```

Verifica che l'indice full-text sia coerente:

```bash
sqlite3 "$DB" "INSERT INTO archive_fts(archive_fts) VALUES('integrity-check');"
```

Il comando non stampa nulla se è tutto a posto.

## Note

- La finestra "1 anno" usa `datetime('now', '-1 year')`: se l'archivio ha
  meno di un anno, l'operazione non trova righe.
- La finestra "6 mesi" usa `datetime('now', '-6 months')`: è una data di
  calendario, non 180 giorni fissi.
- La cancellazione usa i trigger già presenti (`archive_fts_ad`), quindi
  l'indice FTS5 resta allineato senza comandi aggiuntivi.
- Questi comandi lavorano **solo** sulla tabella `archive` di
  `gextto_archive.db`: non cancellano i file già scaricati nella libreria e non
  toccano `gextto_series.db` o `gextto_config.db`.
