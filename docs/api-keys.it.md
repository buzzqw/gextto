# Chiavi di accesso di Gextto

Gextto protegge l'interfaccia web e l'API HTTP con il controllo degli accessi
(facoltativo: la rete locale è libera per impostazione predefinita). Due
**segreti condivisi** permettono a un client senza sessione del browser di
raggiungere il daemon. Sono stringhe lunghe e casuali: tienile private e
trattale come una password di amministratore.

L'interfaccia le genera per te in *Configurazione → Accesso e servizi → Chiavi
API*: premi **Genera** per sostituire il valore con uno nuovo casuale, poi
**Copia** per metterlo negli appunti.

## Chiave API di Gextto (`auth_api_key`)

A cosa serve:

- script e automazioni che chiamano l'API HTTP di Gextto;
- il client da terminale (TUI);
- app di calendario che si iscrivono al feed iCal.

Come si usa:

- header `X-Api-Key: <chiave>`; oppure
- parametro nell'indirizzo `?apikey=<chiave>`;
- la TUI la legge dalla variabile d'ambiente `GEXTTO_API_KEY`.

Conta solo con il controllo degli accessi attivo: con il login spento
(predefinito) l'API risponde liberamente. Impostare, cambiare o svuotare la
chiave non chiude mai fuori il browser — la password continua a funzionare. La
chiave è un *segreto condiviso*: tutti i client usano la stessa, quindi
ruotarla significa aggiornare ogni client (script, TUI, iscrizione al
calendario).

## Token di gx-torrent (`gxtorrent_token`)

A cosa serve:

- la pagina web e l'API HTTP del daemon gx-torrent (`/api/v1` e la `/api/v2`
  compatibile qBittorrent), quando il daemon è raggiungibile sulla rete locale.

Come si usa: i client inviano l'header `X-Gx-Token: <token>`.

Serve solo quando il daemon ascolta oltre il loopback. Sul valore predefinito
`127.0.0.1:8890` è facoltativo; se esponi il daemon (imposta `gxtorrent_listen`
su `0.0.0.0`) imposta un token, altrimenti chiunque sulla rete può pilotarlo.
Gextto passa il token al daemon quando lo avvia, quindi **un nuovo valore vale
dopo il riavvio di gx-torrent (o di Gextto)**; l'interfaccia lo segnala quando
ne genera uno.

## Vedi anche

- [Manuale — Accesso e servizi](MANUAL.it.md#accesso-e-servizi)
- [Sicurezza](SECURITY.md)
- [API HTTP](API.md)
