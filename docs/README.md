# Documentazione Gextto

Questa cartella raccoglie la documentazione mantenuta insieme al codice. Parti
dal manuale nella lingua preferita; gli altri documenti servono a esigenze
specifiche.

| Documento | Quando usarlo |
|---|---|
| [Manuale italiano](MANUAL.it.md) | Configurazione e uso quotidiano della UI web. |
| [English manual](MANUAL.en.md) | English version of the complete user guide. |
| [Guida avanzata](ADVANCED.it.md) | NAS, permessi, proxy, backup e diagnostica. |
| [Advanced guide](ADVANCED.en.md) | English advanced-operation guide. |
| [API HTTP](API.md) | Automazioni e integrazioni che usano il daemon. |
| [Migrazione](MIGRATION.md) | Passaggio sicuro da un'installazione precedente. |
| [Sicurezza](SECURITY.md) | Esposizione di rete, segreti e segnalazione vulnerabilità. |
| [TUI](tui.md) | Client terminale e scorciatoie da tastiera. |
| [Sviluppatori](DEVELOPERS.md) | Build, test e convenzioni per contributor. |

## Rapporti tecnici

Analisi del sorgente con proposte di miglioramento, non modifiche al
comportamento del daemon:

| Rapporto | Contenuto |
|---|---|
| [Terra](../gextto-terra.md) | Esposizione di rete, budget indexer, shutdown, contratto API, router. |
| [Pro Terra](../pro-terra.md) | Sicurezza, concorrenza, lifecycle e integrità dei dati. |

> [!NOTE]
> I percorsi, le porte e i comandi qui descritti sono esempi. Verifica sempre
> utente del servizio, mount NAS e permessi nell'installazione reale.
