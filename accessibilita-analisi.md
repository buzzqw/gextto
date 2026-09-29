# Analisi dell'accessibilità dell'interfaccia web di Gextto

**Data dell'analisi:** 29 settembre 2026  
**Ambito:** interfaccia web in `uiweb/templates/`, `uiweb/static/` e test end-to-end in `uiweb/end2end/`  
**Riferimento:** WCAG 2.1 livello AA, con indicazioni compatibili con WCAG 2.2

## 1. Sintesi e giudizio finale

Gextto presenta una **buona base tecnica**, ma allo stato attuale **non può essere considerato pienamente accessibile né dichiarato conforme a WCAG 2.1 AA**.

Il giudizio complessivo è quindi:

> **Parzialmente accessibile: utilizzabile da molti utenti con tastiera o tecnologie assistive, ma con barriere importanti nei dialoghi modali, nei controlli dinamici, nei moduli e nel tema chiaro.**

Le criticità più rilevanti sono:

1. ordinamento delle tabelle attivabile con il mouse ma non realmente da tastiera;
2. dialoghi modali privi di gestione completa del focus;
3. diversi campi senza un nome accessibile esplicito;
4. aggiornamenti asincroni non sempre annunciati agli screen reader;
5. contrasto insufficiente per alcuni colori nel tema chiaro;
6. perdita del focus durante il polling e la sostituzione di porzioni del DOM.

Questi problemi non rendono tutta l'interfaccia inutilizzabile, ma impediscono di considerarla accessibile in senso normativo. Prima di una dichiarazione di conformità è necessario eseguire anche un test reale con tastiera, screen reader e strumenti automatici su tutte le viste.

## 2. Metodo e limiti

L'analisi è stata condotta tramite:

- revisione statica dei template HTML, del CSS e del JavaScript;
- verifica dei pattern HTML semantici, delle etichette, degli attributi ARIA e della gestione del focus;
- esame delle funzionalità responsive, del focus visibile e di `prefers-reduced-motion`;
- controllo dei token colore del tema scuro e del tema chiaro;
- revisione dei test Playwright disponibili.

Nel progetto non risulta configurato uno script di test automatizzato specifico per axe, Lighthouse, Pa11y o equivalenti: `uiweb/end2end/package.json` contiene Playwright come dipendenza, ma non uno script di accessibilità. Non è stata quindi prodotta una scansione automatica completa né una validazione con uno screen reader reale. I rilievi seguenti sono da considerare una valutazione tecnica del codice, da completare con test manuali.

## 3. Aspetti positivi già presenti

- Il documento dichiara la lingua tramite `<html lang="{{.Chrome.Lang}}">` (`uiweb/templates/ui.html:1`).
- Sono presenti landmark utili: `aside`, `nav`, `header` e `main` (`ui.html:27-50`).
- Esiste un link “Vai al contenuto” (`ui.html:25`) che porta a `#ui-page`.
- La navigazione indica la pagina corrente con `aria-current="page"` (`ui.html:40`).
- La navigazione principale ha un nome accessibile (`aria-label="Navigazione principale"`) e il gruppo Sistema espandibile espone `aria-expanded` (`ui.html:32-38`).
- Molti pulsanti con sola icona hanno sia `aria-label` sia un `title` esplicativo (`ui.html:295-301`).
- Molti campi sono racchiusi in un elemento `label`, con testo visibile associato (`pages.html:95-101`, `pages.html:332-347`).
- Il focus di link e pulsanti è considerato nel CSS (`gextto-ui.css:86`); i campi hanno uno stile distinto quando ricevono il focus (`gextto-ui.css:476-480`).
- Sono presenti layout responsive, target touch di dimensione adeguata e gestione di `prefers-reduced-motion` (`gextto-ui.css:984-1007`, `877-879`).
- Le immagini create dinamicamente ricevono un testo alternativo (`gextto-ui.js:2718-2723`, `2824-2828`, `2918-2923`, `3787-3792`).
- Toast e alcuni messaggi di stato usano `role="status"`, `role="alert"` o `aria-live` (`gextto-ui.js:354-376`).
- Le tabelle sono contenute in elementi con scorrimento orizzontale; per la tabella torrent viene mostrata anche un'indicazione (`ui.html:255-257`).

## 4. Problemi rilevati e soluzioni proposte

### A11Y-01 — Campi privi di nome accessibile esplicito

**Priorità: Alta**  
**WCAG coinvolte:** 1.3.1, 2.4.6, 3.3.2, 4.1.2

Diversi campi usano solo `placeholder` o `title` senza un'etichetta persistente e visibile/assistiva. Esempi:

- ricerca dashboard e filtro risultati (`ui.html:153-159`);
- filtri generici (`pages.html:14-16`);
- codice OAuth (`pages.html:261`, `498`, `512`);
- URL GetComics (`pages.html:296`);
- ricerca release (`pages.html:470-475`);
- filtro episodi (`pages.html:587`);
- filtro log (`ui.html:434`);
- alcuni campi generati dal JavaScript o da template generici; non tutti i percorsi condividono l'etichettatura esplicita, quindi vanno verificati nel DOM finale.

Il problema è più netto in `settings_field_grid` (`pages.html:789-800`): il testo è un semplice `<span>` e non è associato al campo tramite `label`, `for` o `aria-labelledby`.

**Soluzione:** assegnare un `id` univoco a ogni controllo e usare un `<label for="...">`. Quando l'etichetta visibile non è desiderata, usare una classe `sr-only`; non usare il placeholder come unica etichetta. Per i campi con istruzioni aggiuntive usare `aria-describedby` collegato a un testo stabile.

Esempio:

```html
<label class="sr-only" for="release-search">Cerca una release</label>
<input id="release-search" type="search" ...>
```

### A11Y-02 — Intestazioni ordinabili non raggiungibili da tastiera

**Priorità: Alta**  
**WCAG coinvolte:** 2.1.1, 2.4.3, 4.1.2

Le intestazioni con `data-sort` vengono intercettate in un listener `click` (`gextto-ui.js:454-461`), ma restano elementi `<th>` non interattivi (`ui.html:260-267`). Un utente che usa solo la tastiera non può portare il focus sull'intestazione e attivare l'ordinamento. Lo stesso problema si ripete nelle tabelle generate dinamicamente (`gextto-ui.js:1408-1411`, `1665-1693`).

**Soluzione:** inserire un vero `<button>` dentro ogni intestazione ordinabile, oppure trasformare l'intestazione in un controllo con gestione completa di tastiera. Aggiornare `aria-sort="ascending|descending|none"` e usare un testo comprensibile, non soltanto le frecce `↓` e `↑`. Le intestazioni dovrebbero inoltre avere `scope="col"`.

### A11Y-03 — Dialoghi modali senza ciclo e ripristino del focus

**Priorità: Alta**  
**WCAG coinvolte:** 2.1.1, 2.4.3, 2.4.7, 4.1.2

I dialoghi dichiarano correttamente `role="dialog"` e `aria-modal="true"` in diversi punti (`ui.html:347-387`, `gextto-ui.js:741-747`, `3976-3979`, `4051-4053`, `4196-4198`, `4571-4575`), ma l'implementazione è incompleta:

- non esiste un focus trap riutilizzabile;
- il focus non viene sempre portato nel dialogo;
- il focus dell'elemento che ha aperto il dialogo non viene ripristinato alla chiusura;
- `Escape` chiude solo alcuni dialoghi (`gextto-ui.js:3678-3685`);
- il dialogo “Aggiungi film/serie” non collega il nome visibile con `aria-labelledby` (`gextto-ui.js:3976-3983`);
- il dialogo creato per la modifica fumetti non inizializza il focus (`gextto-ui.js:1823-1831`).

Con `aria-modal="true"`, lasciare che la tabulazione raggiunga il contenuto sottostante è particolarmente problematico per gli utenti di screen reader e tastiera.

**Soluzione:** creare un componente/funzione comune `openDialog()` che:

1. assegni un id al titolo e `aria-labelledby` al dialogo;
2. memorizzi l'elemento che ha aperto il dialogo;
3. sposti il focus sul pulsante Chiudi o sul primo campo;
4. intrappoli `Tab` e `Shift+Tab` dentro il dialogo;
5. chiuda con `Escape` e clic sul backdrop quando appropriato;
6. ripristini il focus all'elemento originario.

### A11Y-04 — Perdita del focus durante aggiornamenti e polling

**Priorità: Alta**  
**WCAG coinvolte:** 2.4.3, 3.2.1, 4.1.3

La vista Download sostituisce periodicamente una porzione del DOM (`gextto-ui.js:318-334`) ogni tre secondi (`gextto-ui.js:379-387`). Anche altre viste vengono sostituite con `page.innerHTML = html` (`gextto-ui.js:339`). Se l'utente sta usando la tastiera dentro una tabella o un controllo che viene ricaricato, il nodo focalizzato viene eliminato e il focus può tornare al documento o andare in una posizione inattesa.

**Soluzione:** aggiornare solo le celle o le righe cambiate quando possibile. Se la sostituzione è necessaria, memorizzare il controllo focalizzato tramite una chiave stabile, sostituire il contenuto, quindi ripristinare il focus e la posizione. Evitare il polling aggressivo mentre un controllo della regione è attivo oppure sospenderlo durante l'interazione.

### A11Y-05 — Aggiornamenti asincroni non sempre annunciati

**Priorità: Alta**  
**WCAG coinvolte:** 1.3.1, 4.1.3

Sono presenti alcune `aria-live`, ma molti risultati dinamici non vengono annunciati:

- `data-form-message` non è una live region (`pages.html:104-109`);
- gli stati TMDB non hanno `aria-live` (`pages.html:432`, `gextto-ui.js:2763-2774`);
- i risultati e gli errori di `data-form-output` sono inseriti senza una regione con ruolo (`gextto-ui.js:3658-3670`);
- diversi stati (`data-db-status`, `data-trash-status`, `data-duplicates-status`, `data-ramdisk-status`) non hanno `aria-live` (`pages.html:136-158`, `206-213`, `239-246`);
- i risultati mostrati in `div.output` non vengono annunciati in modo consistente;
- i cambi di metriche nella topbar (`gextto-ui.js:2402-2461`) non hanno un criterio di annuncio, e non dovrebbero essere annunciati a ogni polling.

**Soluzione:** aggiungere una regione di stato persistente per ogni operazione asincrona, ad esempio `<div role="status" aria-live="polite" aria-atomic="true">`. Usare `role="alert"` solo per errori urgenti. Impostare `aria-busy="true"` sulla regione durante il caricamento e restituirla a `false` alla fine. Dopo un'operazione importante, portare il focus al titolo o al messaggio solo se necessario, senza interrompere inutilmente l'utente.

### A11Y-06 — Contrasto insufficiente nel tema chiaro

**Priorità: Medio-alta**  
**WCAG coinvolta:** 1.4.3

Nel tema chiaro i colori usati come testo per stati e badge sono troppo chiari rispetto alle superfici:

- `--accent: #12a97a` su `--surface: #edf1f7` ha un rapporto calcolato di circa **2,65:1**;
- `--warn: #b7791f` su `#edf1f7` circa **3,21:1**;
- `--err: #d6336c` su `#edf1f7` circa **4,07:1**.

Sono valori inferiori a 4,5:1 per testo normale. I colori sono usati in `.badge.ok`, `.badge.warn`, `.badge.err` (`gextto-ui.css:499-501`) e nei valori delle metriche (`372-374`). Il tema scuro risulta sensibilmente migliore per questi accostamenti.

**Soluzione:** usare varianti più scure per il testo nel tema chiaro, mantenendo eventualmente lo sfondo pastello separato. Verificare i colori finali dopo il `color-mix`, perché il contrasto va misurato sul colore realmente renderizzato. Non affidarsi al colore solo: mantenere sempre anche testo come “errore”, “in pausa”, “attiva” o “completato”, pratica già presente in molte viste.

### A11Y-07 — Indicatore di focus rimosso dalla navigazione

**Priorità: Media**  
**WCAG coinvolta:** 2.4.7

Il CSS definisce un buon outline globale (`gextto-ui.css:86`), ma lo annulla per gli elementi di navigazione:

```css
.nav-item:hover, .nav-item:focus-visible {
  ...
  outline: none;
}
```

(`gextto-ui.css:160`). Il cambio di sfondo non è sempre sufficiente come indicatore visibile, soprattutto nel tema scuro o con ingrandimento.

**Soluzione:** non usare `outline: none`; applicare un bordo/outline coerente con contrasto almeno 3:1 rispetto agli stati adiacenti. Verificare il focus anche sulla navigazione mobile e sulla barra fissa inferiore.

### A11Y-08 — Gerarchia dei titoli incoerente

**Priorità: Media**  
**WCAG coinvolta:** 1.3.1

Il titolo principale è un `<h1>` (`ui.html:54`), ma molte sezioni passano direttamente a `<h3>` (`ui.html:112`, `124`, `171`, `186`; `pages.html:4`, `41`, `85`). La pagina Manuale può inoltre inserire un altro `<h1>` dentro il contenuto (`ui.html:454-460`). Questo rende meno chiara la struttura per la navigazione tramite elenco dei titoli.

**Soluzione:** mantenere un solo `<h1>` per la vista. Usare `<h2>` per le sezioni principali dei pannelli e `<h3>` per eventuali sottosezioni. Nel Manuale, scegliere se il titolo del documento deve essere il titolo principale della pagina oppure usare un livello subordinato rispetto al titolo della shell.

### A11Y-09 — Tabelle con semantica incompleta

**Priorità: Media**  
**WCAG coinvolta:** 1.3.1

Le tabelle usano `<th>`, ma in generale non specificano `scope="col"`. Inoltre alcune colonne Azioni hanno un'intestazione vuota (`ui.html:224`, `pages.html:964` e `gextto-ui.js:4575`), mentre le tabelle generate con JavaScript costruiscono gli header senza scope (`gextto-ui.js:1408-1411`, `2249`, `3085`, `4393`).

**Soluzione:** aggiungere `scope="col"` a tutte le intestazioni, un `<caption>` descrittivo (visibile o `sr-only`) e un nome all'intestazione delle azioni, per esempio “Azioni”. Per tabelle molto complesse valutare `scope="row"` sulle celle identificative.

### A11Y-10 — Barra di progresso solo visiva

**Priorità: Media**  
**WCAG coinvolta:** 4.1.2

La progress bar è un `div` con una `span` la cui larghezza viene modificata (`pages.html:280-286`, `gextto-ui.js:3698-3707`). Il testo vicino aiuta, ma il controllo non espone `role="progressbar"` né `aria-valuemin`, `aria-valuemax` e `aria-valuenow`.

**Soluzione:** usare:

```html
<div role="progressbar" aria-valuemin="0" aria-valuemax="100"
     aria-valuenow="0" aria-label="Avanzamento operazione">
</div>
```

Aggiornare `aria-valuenow` a ogni polling e mantenere il testo esplicativo per stato, corrente, totale ed errori.

### A11Y-11 — Uso non completo del ruolo `listbox`

**Priorità: Media**  
**WCAG coinvolte:** 4.1.2, 4.1.3

Il contenitore dei risultati della ricerca impostazioni ha `role="listbox"` (`pages.html:927`), ma i figli sono link (`gextto-ui.js:2643-2648`) e non `role="option"`. Non sono implementati `aria-activedescendant`, frecce di navigazione o selezione da tastiera come richiesto dal pattern listbox.

**Soluzione:** per una semplice lista di collegamenti rimuovere `role="listbox"`. Se si vuole un vero autocomplete, implementare il pattern combobox/listbox completo, inclusi ruoli, stato attivo e tastiera.

### A11Y-12 — Errori e validazione non associati ai campi

**Priorità: Media**  
**WCAG coinvolte:** 3.3.1, 3.3.3, 4.1.3

Gli errori vengono spesso mostrati in un `<small>` o in un toast (`gextto-ui.js:1281-1312`, `1941-1987`, `3639-3674`), ma il campo non viene marcato con `aria-invalid="true"`, non viene collegato al messaggio con `aria-describedby` e il focus non viene portato al primo campo errato. Per un utente di screen reader è difficile capire quale controllo richiede correzione.

**Soluzione:** generare un id per il messaggio, impostare `aria-invalid`, collegare il messaggio al controllo e mantenere il messaggio fino alla correzione. Per gli errori di form, spostare il focus sul primo campo errato solo dopo l'invio e annunciare un riepilogo breve.

### A11Y-13 — Dipendenza eccessiva da `title` e simboli decorativi

**Priorità: Bassa-media**  
**WCAG coinvolte:** 1.3.1, 1.4.1, 2.4.6

Molte istruzioni aggiuntive sono disponibili solo tramite `title` (`ui.html:58-74`, `pages.html:50`, `808-819`). Il comportamento dei tooltip nativi non è uniforme su touch screen e non è una modalità affidabile per contenuti importanti. Emoji e simboli decorativi nella navigazione e nei link dashboard (`ui.html:34`, `174-179`) possono inoltre essere annunciati in modo rumoroso.

**Soluzione:** rendere visibili le istruzioni essenziali o associarle con `aria-describedby`; mantenere `title` solo come informazione supplementare. Applicare `aria-hidden="true"` alle emoji decorative quando il testo adiacente è già sufficiente.

## 5. Piano di correzione consigliato

### Fase 1 — blocchi di accessibilità

1. Correggere tutte le etichette e i nomi accessibili dei campi.
2. Rendere le intestazioni ordinabili veri controlli da tastiera.
3. Introdurre un gestore comune dei dialoghi con focus trap, `Escape`, `aria-labelledby` e ripristino del focus.
4. Impedire la perdita del focus durante polling e sostituzioni DOM.
5. Aggiungere live region e `aria-busy` ai risultati asincroni.

### Fase 2 — qualità semantica e visiva

1. Correggere i token di contrasto del tema chiaro.
2. Ripristinare un indicatore di focus evidente sulla navigazione.
3. Normalizzare la gerarchia dei titoli.
4. Aggiungere caption e `scope` alle tabelle.
5. Esporre correttamente le progress bar.
6. Rimuovere il `listbox` non implementato o completarne il pattern.

### Fase 3 — validazione

- Aggiungere axe-core o equivalente alla suite Playwright e uno script `npm run test:a11y`.
- Testare tutte le pagine a 100%, 200% e 400% di zoom.
- Verificare la navigazione completa con tastiera: `Tab`, `Shift+Tab`, `Enter`, `Space`, frecce, `Escape`.
- Testare almeno NVDA + Firefox e VoiceOver + Safari, se disponibili.
- Verificare tema scuro/chiaro, viewport mobile, orientamento, `prefers-reduced-motion` e assenza di JavaScript.
- Controllare manualmente ogni form, modal, tabella dinamica, polling e stato di errore.

## 6. Criteri di accettazione suggeriti

L'interfaccia potrà essere rivalutata come conforme almeno al livello AA quando:

- ogni controllo ha un nome accessibile e ogni errore è associato al campo corretto;
- ogni azione disponibile con il mouse è disponibile anche con tastiera;
- i dialoghi mantengono il focus al loro interno e lo restituiscono correttamente;
- gli aggiornamenti asincroni importanti vengono annunciati senza interrompere inutilmente l'utente;
- tutti i colori di testo rispettano il contrasto minimo, inclusi gli stati nel tema chiaro;
- le tabelle e le progress bar espongono una semantica interpretabile dalle tecnologie assistive;
- i test automatici e manuali non rilevano regressioni sulle viste desktop e mobile.

## Conclusione

Gextto non parte da una situazione priva di accorgimenti: skip link, landmark, controlli nativi, responsive design, riduzione delle animazioni e numerose etichette sono segnali positivi. Tuttavia, le carenze nei dialoghi, nei controlli dinamici e nella semantica dei campi e delle tabelle sono funzionalmente rilevanti. La classificazione corretta, allo stato dell'analisi, è pertanto **“parzialmente accessibile, non ancora conforme WCAG 2.1 AA”**.
