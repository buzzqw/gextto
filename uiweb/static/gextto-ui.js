// gextto-ui.js — small client for the server-rendered UI (see uiweb.go).
// It only fills authenticated partials and forwards actions to the existing
// JSON APIs; it is intentionally not a SPA.
(function () {
  "use strict";

  var page = document.getElementById("ui-page");
  if (!page) return;
  var view = page.getAttribute("data-view") || "dashboard";

  // All dialogs use the same keyboard contract: focus enters the dialog,
  // Tab stays inside it, Escape closes it, and focus returns to the opener.
  // This is deliberately small and dependency-free because the UI is served
  // without a component framework.
  var activeDialog = null;
  function dialogFocusable(dialog) {
    return Array.prototype.filter.call(dialog.querySelectorAll(
      'a[href], area[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])'
    ), function (node) {
      return !node.hidden && !!(node.offsetWidth || node.offsetHeight || node.getClientRects().length);
    });
  }
  function openAccessibleDialog(dialog, opener) {
    if (!dialog) return;
    dialog._previousDialog = activeDialog && activeDialog !== dialog ? activeDialog : null;
    dialog._previousFocus = opener && typeof opener.focus === "function" ? opener : document.activeElement;
    dialog.hidden = false;
    activeDialog = dialog;
    var target = dialogFocusable(dialog)[0] || dialog.querySelector('[role="dialog"]');
    if (target) {
      if (!target.matches("input, select, textarea, button, a, area") && !target.hasAttribute("tabindex")) target.tabIndex = -1;
      window.setTimeout(function () { target.focus(); }, 0);
    }
  }
  function closeAccessibleDialog(dialog, remove) {
    if (!dialog) return;
    dialog.hidden = true;
    if (activeDialog === dialog) activeDialog = dialog._previousDialog || null;
    if (remove || dialog._removeOnClose) dialog.remove();
    var previous = dialog._previousFocus;
    if (previous && document.contains(previous) && !previous.disabled) previous.focus();
  }
  document.addEventListener("keydown", function (event) {
    if (!activeDialog || activeDialog.hidden) return;
    if (event.key === "Escape") {
      event.preventDefault();
      closeAccessibleDialog(activeDialog);
      return;
    }
    if (event.key !== "Tab") return;
    var focusable = dialogFocusable(activeDialog);
    if (!focusable.length) {
      event.preventDefault();
      return;
    }
    var first = focusable[0];
    var last = focusable[focusable.length - 1];
    if (event.shiftKey && document.activeElement === first) {
      event.preventDefault();
      last.focus();
    } else if (!event.shiftKey && document.activeElement === last) {
      event.preventDefault();
      first.focus();
    }
  }, true);

  function request(path, method, body, signal) {
    var requestMethod = (method || "GET").toUpperCase();
    var headers = { "Content-Type": "application/json" };
    // Browsers reject a body on GET/HEAD requests. Some generic action
    // buttons intentionally use GET (for example the port check) and carry
    // an empty data-body attribute, so only encode payloads for methods that
    // can transport one.
    var requestBody = requestMethod === "GET" || requestMethod === "HEAD"
      ? undefined
      : (body === undefined || body === null ? undefined : JSON.stringify(body));
    return fetch(path, {
      method: requestMethod,
      headers: headers,
      body: requestBody,
      credentials: "same-origin",
      signal: signal
    });
  }

  function handleResponse(response) {
    var type = response.headers.get("content-type") || "";
    var isJSON = type.indexOf("application/json") >= 0;
    if (!response.ok) {
      return (isJSON ? response.json().catch(function () { return null; }) : Promise.resolve(null))
        .then(function (data) {
          var message = data && data.error ? String(data.error) : "HTTP " + response.status;
          throw new Error(message);
        });
    }
    return isJSON ? response.json() : response.text();
  }

  function api(path, method, body, signal) {
    return request(path, method, body, signal).then(function (response) {
      return handleResponse(response);
    });
  }

  // Keep polling bounded. If the daemon is busy, overlapping fetches would
  // retain their Promise/DOM closures until every request eventually finishes.
  // Action requests continue to use api() directly and are not shortened.
  var pollTimeoutMs = 10000;
  var sourceHealthTimeoutMs = 120000;
  function pollRequest(path) {
    var controller = window.AbortController ? new window.AbortController() : null;
    var timeoutMs = path.indexOf("/api/sources/health") === 0 ? sourceHealthTimeoutMs : pollTimeoutMs;
    var timeout = window.setTimeout(function () {
      if (controller) controller.abort();
    }, timeoutMs);
    var signal = controller ? controller.signal : undefined;
    return api(path, "GET", undefined, signal).then(function (value) {
      window.clearTimeout(timeout);
      return value;
    }, function (error) {
      window.clearTimeout(timeout);
      throw error;
    });
  }

  // The server-rendered shell predates the multilingual UI and contains plain
  // Italian labels. Reuse the persisted translation catalog here so the new
  // UI follows the language selected in the top bar, including labels added by
  // partials and client-side dialogs.
  var uiLanguage = document.documentElement.getAttribute("lang") || "it";
  var uiTranslations = {};
  var uiEnglishCatalog = {};
  var uiEnglishFallbacks = {
    "Cerca impostazioni": "Search settings",
    "Configurazione": "Configuration",
    "Controllo libreria e download": "Library and download control",
    "Monitora il servizio, avvia una ricerca o raggiungi subito la sezione che ti serve.": "Monitor the service, start a search, or jump to the section you need.",
    "Torrent e download HTTP fumetti nella stessa lista di lavoro": "Torrents and comic HTTP downloads in the same work list",
    "Download session": "Download session",
    "In scarico": "Downloading",
    "Nessun download nella sessione.": "No downloads in the session.",
    "Dettagli download HTTP": "HTTP download details",
    "Rimuovere questo download?": "Remove this download?",
    "Storico da conservare": "History to keep",
    "Numero massimo di elementi da conservare sia nello Storico fumetti sia nello Storico Weekly Pack (1–500).": "Maximum number of items to keep in both Comic history and Weekly Pack history (1–500).",
    "Weekly attivo": "Weekly enabled",
    "Avvia ciclo fumetti": "Start comic cycle",
    "Pianificazione settimanale": "Weekly scheduling",
    "Storico fumetti": "Comic history",
    "Storico Weekly Pack": "Weekly Pack history",
    "Motore torrent": "Torrent engine",
    "Ciclo fumetti": "Comic cycle",
    "Scarica weekly pack a partire dal": "Download Weekly Packs starting from",
    "Cerca un Weekly Pack": "Search for a Weekly Pack",
    "Cerca weekly": "Search weekly",
    "Download diretto": "Direct download",
    "File diretti": "Direct files",
    "Ricerca in corso…": "Searching…",
    "Nessun link trovato.": "No links found.",
    "Cerca il post del Weekly Pack su GetComics per la data scelta ed estrae i magnet/.torrent disponibili. Non avvia il download.": "Find the GetComics Weekly Pack post for the selected date and extract the available magnet/.torrent links. Does not start the download.",
    "Cerca il titolo, poi scarica con Download Now o aggiungi il fumetto alla libreria.": "Search for the title, then download with Download Now or add the comic to the library.",
    "Scarica da GetComics": "Download from GetComics",
    "Salva weekly": "Save weekly",
    "Avvia ciclo fumetti": "Start comic cycle",
    "Applica motore": "Apply engine",
    "Stato qBittorrent-nox": "qBittorrent-nox status",
    "Aggiungi riga": "Add row",
    "Regole tag → cartella": "Tag → folder rules",
    "Cartella": "Folder",
    "Cartella finale": "Final folder",
    "Cartella dati del motore anacrolix (stato e resume).": "anacrolix engine data folder (state and resume).",
    "Intervallo in millisecondi tra due letture dello stato dei torrent.": "Interval in milliseconds between torrent status polls.",
    "Mappatura dei percorsi tra Gextto e il motore anacrolix (locale=remoto).": "Path mapping between Gextto and the anacrolix engine (local=remote).",
    "Motore torrent attivo (libtorrent, qBittorrent-nox o anacrolix).": "Active torrent engine (libtorrent, qBittorrent-nox, or anacrolix).",
    "Porta di ascolto del motore anacrolix.": "anacrolix engine listening port.",
    "Scegli il motore dalla tendina «Motore torrent». Il binario gestito è scaricato e aggiornato da Gextto.": "Choose the engine from the “Torrent engine” list. The managed binary is downloaded and updated by Gextto.",
    "anacrolix — cartella dati": "anacrolix — data folder",
    "Dimensione massima di un singolo torrent ammesso sul RAM disk (GB).": "Maximum size of a single torrent allowed on the RAM disk (GB).",
    "I torrent che non trasferiscono dati non consumano uno slot attivo.": "Torrents that do not transfer data do not consume an active slot.",
    "Impostazioni libtorrent avanzate, una per riga nel formato chiave=valore.": "Advanced libtorrent settings, one key=value entry per line.",
    "Limite di seeding in minuti, usato solo se Seed massimo (giorni) è 0.": "Seeding limit in minutes, used only when Maximum seed time (days) is 0.",
    "Permette più connessioni dallo stesso indirizzo IP.": "Allows multiple connections from the same IP address.",
    "Più connessioni per IP": "Multiple connections per IP",
    "Programmazione velocità attiva": "Speed scheduling enabled",
    "Scarica in RAM i torrent che rientrano nella soglia; i più grandi vanno su disco.": "Download torrents within the threshold to RAM; larger torrents go to disk.",
    "Secondi di inattività dopo cui un blocco esce dalla cache.": "Idle seconds before a block leaves the cache.",
    "Spazio libero da lasciare sul RAM disk una volta completato il download (GB).": "Free space to leave on the RAM disk after the download completes (GB).",
    "Applica motore": "Apply engine",
    "Scegli una destinazione temporanea tmpfs/ramfs scrivibile: premi \"Usa questo percorso\" e Gextto la configura (creando la cartella se serve) con soglia e margine consigliati. /dev/shm viene perso al riavvio.": "Choose a writable temporary tmpfs/ramfs destination: click “Use this path” and Gextto will configure it (creating the folder if needed) with recommended threshold and margin. /dev/shm is lost on reboot.",
    "Salva Simkl": "Save Simkl",
    "Salva Trakt": "Save Trakt",
    "Jackett, Prowlarr o altri indexer Torznab. L'URL è la base (Gextto aggiunge il percorso Torznab).": "Jackett, Prowlarr, or other Torznab indexers. The URL is the base (Gextto adds the Torznab path).",
    "Scarica gli script per aprire magnet e file .torrent direttamente in Gextto.": "Download scripts to open magnet links and .torrent files directly in Gextto.",
    "Verifica indexer e sorgenti": "Check indexers and sources",
    "Associa un tag del torrent a una cartella temporanea e finale.": "Map a torrent tag to a temporary and final folder.",
    "Numero minimo di release recenti da conservare sempre in archivio.": "Minimum number of recent releases to always keep in the archive.",
    "Spazio libero minimo (GB) sulla cartella download: sotto questa soglia il ciclo non avvia download.": "Minimum free space (GB) in the download folder: below this threshold the cycle does not start downloads.",
    "Sposta gli episodi e i season pack nella cartella archivio invece di copiarli.": "Move episodes and season packs to the archive folder instead of copying them.",
    "La verifica parte solo quando la richiedi e può richiedere fino a ~1 minuto.": "The check starts only when requested and can take up to ~1 minute.",
    "Diagnostica sorgenti": "Source diagnostics",
    "Stato provider": "Provider status",
    "Salva backup": "Save backup",
    "Cartella cloud": "Cloud folder",
    "VACUUM compatta i file; ANALYZE aggiorna le statistiche SQLite.": "VACUUM compacts the files; ANALYZE updates SQLite statistics.",
    "Premi Aggiorna per verificare tutte le sorgenti; usa la ricerca per provare una query su feed, indexer e motori web.": "Click Refresh to check all sources; use the search to test a query on feeds, indexers, and web engines.",
    "Aggiungi una serie": "Add a series",
    "Aggiungi un film": "Add a movie",
    "Scrivi il titolo e cerca su TMDB, oppure usa \"Aggiungi manualmente\" per compilare i requisiti.": "Enter the title and search TMDB, or use “Add manually” to fill in the requirements.",
    "ATTIVO": "ACTIVE",
    "Rinomina anche i file già corretti": "Also rename files already in the correct format",
    "Perché non questo?": "Why not this?",
    "Chiudi": "Close",
    "Elimina tutti": "Delete all",
    "Nessun database.": "No database.",
    "Nessun download recente.": "No recent downloads.",
    "Nessun duplicato inferiore trovato.": "No lower-quality duplicates found.",
    "Nessun risultato su GetComics.": "No results on GetComics.",
    "Nessun risultato su TMDB.": "No results on TMDB.",
    "Nessun tmpfs/ramfs scrivibile trovato.": "No writable tmpfs/ramfs found.",
    "Nessun file (metadati non ancora disponibili).": "No files (metadata not available yet).",
    "Nessun peer connesso.": "No connected peers.",
    "Nessun tracker.": "No trackers.",
    "Nessuna rinomina necessaria: i file sono già nel formato corretto.": "No rename needed: files already use the correct format.",
    "Nessun risultato con questo filtro.": "No results with this filter.",
    "Seleziona almeno un torrent": "Select at least one torrent",
    "Aggiungi questa release ai download": "Add this release to downloads",
    "Tag aggiornato": "Tag updated",
    "Punteggio di qualità della release": "Release quality score",
    "Risolvi i link del post e avvia Download Now": "Resolve the post links and start Download Now",
    "Download Now non trovato per questo post": "Download Now was not found for this post",
    "Torrent non aggiunto: ": "Torrent not added: ",
    "Operazione completata": "Operation completed",
    "Azione non riuscita: ": "Action failed: ",
    "Salvataggio non riuscito: ": "Save failed: ",
    "Cambio lingua non riuscito: ": "Language change failed: ",
    "Tag assegnato a ": "Tag assigned to ",
    "Tag rimosso da ": "Tag removed from ",
    "Seleziona tutti i torrent": "Select all torrents",
    "Tag da assegnare ai torrent selezionati": "Tag to assign to selected torrents",
    "Riprendi il torrent": "Resume torrent",
    "Metti in pausa il torrent": "Pause torrent",
    "Forza la verifica dei dati": "Force data recheck",
    "Mostra i dettagli del torrent": "Show torrent details",
    "Rimuovi il torrent: scegli se mantenere o eliminare i file": "Remove torrent: choose whether to keep or delete files",
    "Metti in pausa il download HTTP": "Pause HTTP download",
    "Riprendi il download HTTP": "Resume HTTP download",
    "Mostra i dettagli del download HTTP": "Show HTTP download details",
    "Rimuovi il download HTTP": "Remove HTTP download",
    "Nuovo tag": "New tag",
    "Assegna tag": "Assign tag",
    "Rimuovi tag": "Remove tag",
    "Recheck": "Recheck",
    "Rimuovere questo torrent?": "Remove this torrent?",
    "Nessun elemento.": "No items.",
    "Caricamento…": "Loading…",
    "Nessun risultato.": "No results.",
    "Inserisci una ricerca.": "Enter a search.",
    "Salva": "Save",
    "Salva tutte": "Save all",
    "Nessuna modifica": "No changes",
    "Sì": "Yes",
    "No": "No",
    "Traduzioni": "Translations"
  };

  // German and French have bundled catalogs; these fallback objects carry
  // labels added by the redesigned UI before they reach the persisted catalog.
  var uiGermanFallbacks = {};
  var uiFrenchFallbacks = {};
  var uiFallbacks = { en: uiEnglishFallbacks, de: uiGermanFallbacks, fr: uiFrenchFallbacks };

  function uiTranslatedText(value) {
    if (!value) return value;
    var match = String(value).match(/^(\s*)([\s\S]*?)(\s*)$/);
    var core = match ? match[2] : String(value);
    var translated = uiTranslations[core] || (uiFallbacks[uiLanguage] || {})[core];
    if (!translated && (uiLanguage === "de" || uiLanguage === "fr")) translated = uiEnglishCatalog[core];
    var matchDownloads = core.match(/^(\d+) torrent · (\d+) HTTP fumetti · aggiornato (.+)$/);
    if (!translated && matchDownloads) translated = matchDownloads[1] + " torrents · " + matchDownloads[2] + " comic HTTP downloads · updated " + matchDownloads[3];
    var matchSelected = core.match(/^(\d+) selezionati$/);
    if (!translated && matchSelected) translated = matchSelected[1] + " selected";
    var matchSettings = core.match(/^(\d+) impostazioni$/);
    if (!translated && matchSettings) translated = matchSettings[1] + " settings";
    var matchOrigin = core.match(/^Perché: (.+) · Fonte: (.+)$/);
    if (!translated && matchOrigin) translated = "Reason: " + matchOrigin[1] + " · Source: " + matchOrigin[2];
    var matchDashboard = core.match(/^(\d+) in scarico · (\d+) in seed$/);
    if (!translated && matchDashboard) translated = matchDashboard[1] + " downloading · " + matchDashboard[2] + " seeding";
    var matchEngineHint = core.match(/^Scegli il motore dalla tendina «Motore torrent»\. Il binario gestito è scaricato e aggiornato da Gextto\. Binario: (.+)\.$/);
    if (!translated && matchEngineHint) translated = "Choose the engine from the “Torrent engine” list. The managed binary is downloaded and updated by Gextto. Binary: " + matchEngineHint[1] + ".";
    if (!translated) return value;
    return (match ? match[1] : "") + translated + (match ? match[3] : "");
  }

  function translateUIValue(value) {
    if (uiLanguage !== "en" && uiLanguage !== "de" && uiLanguage !== "fr") return value;
    return uiTranslatedText(value);
  }

  function translateUIJSONAttribute(node, attribute) {
    var raw = node.getAttribute(attribute);
    if (!raw) return;
    try {
      var parsed = JSON.parse(raw);
      var changed = false;
      function visit(value) {
        if (Array.isArray(value)) return value.map(visit);
        if (!value || typeof value !== "object") {
          var translated = translateUIValue(value);
          if (translated !== value) changed = true;
          return translated;
        }
        Object.keys(value).forEach(function (key) { value[key] = visit(value[key]); });
        return value;
      }
      var translatedJSON = visit(parsed);
      if (changed) node.setAttribute(attribute, JSON.stringify(translatedJSON));
    } catch (_) { /* leave malformed or non-JSON attributes untouched */ }
  }

  function translateUINode(node) {
    if (!node) return;
    if (node.nodeType === 3) {
      var originalText = node.nodeValue;
      var translatedText = translateUIValue(originalText);
      // MutationObserver watches characterData. Do not write the same value
      // back: browsers report even a no-op nodeValue assignment as a new
      // mutation, which would create an observer -> write -> observer loop.
      if (translatedText !== originalText) node.nodeValue = translatedText;
      return;
    }
    if (node.nodeType !== 1) return;
    var tag = node.tagName;
    if (tag === "SCRIPT" || tag === "STYLE" || tag === "TEXTAREA") return;
    ["title", "placeholder", "aria-label", "data-empty", "data-confirm", "data-action-label"].forEach(function (attribute) {
      if (node.hasAttribute(attribute)) node.setAttribute(attribute, translateUIValue(node.getAttribute(attribute)));
    });
    ["data-columns", "data-actions"].forEach(function (attribute) { translateUIJSONAttribute(node, attribute); });
    Array.prototype.forEach.call(node.childNodes, translateUINode);
  }

  function translateUI() {
    if (uiLanguage === "en" || uiLanguage === "de" || uiLanguage === "fr") {
      translateUINode(document.body);
      document.title = uiTranslatedText(document.title.replace(/^Gextto · /, "")) === document.title.replace(/^Gextto · /, "")
        ? document.title
        : "Gextto · " + uiTranslatedText(document.title.replace(/^Gextto · /, ""));
    }
  }

  function loadUITranslations() {
    if (uiLanguage !== "en" && uiLanguage !== "de" && uiLanguage !== "fr") return;
    api("/api/i18n?lang=" + encodeURIComponent(uiLanguage), "GET").then(function (data) {
      (data && data.items || []).forEach(function (item) {
        if (item && item.key && item.value) uiTranslations[item.key] = item.value;
      });
      var catalogReady = Promise.resolve();
      if (uiLanguage === "de" || uiLanguage === "fr") {
        catalogReady = api("/api/i18n?lang=en", "GET").then(function (english) {
          (english && english.items || []).forEach(function (item) {
            if (item && item.key && item.value) uiEnglishCatalog[item.key] = item.value;
          });
        });
      }
      return catalogReady;
    }).then(function () {
      translateUI();
      if (window.MutationObserver) {
        var observer = new MutationObserver(function (mutations) {
          mutations.forEach(function (mutation) {
            Array.prototype.forEach.call(mutation.addedNodes, translateUINode);
            if (mutation.type === "characterData") translateUINode(mutation.target);
          });
        });
        observer.observe(document.body, { childList: true, subtree: true, characterData: true });
      }
    }).catch(function () { translateUI(); });
  }

  loadUITranslations();

  var partials = {
    downloads: "/ui/partial/torrents"
  };

  var loadPromise = null;
  var loadPending = false;
  function load() {
    if (loadPromise) {
      loadPending = true;
      return loadPromise;
    }
    var url = partials[view];
    if (!url) return Promise.resolve();
    loadPromise = pollRequest(url)
      .then(function (html) {
        if (view === "downloads") {
          var currentSlot = page.querySelector("[data-torrents-slot]");
          if (currentSlot) {
            // The download partial is replaced on every poll. Preserve the
            // user's selection across that DOM replacement; otherwise the
            // checkboxes appear to clear themselves after a few seconds.
            var selectedBeforeRefresh = selectedHashes();
            var activeBeforeRefresh = document.activeElement;
            var activeHash = activeBeforeRefresh && activeBeforeRefresh.getAttribute("data-hash");
            var activeAction = activeBeforeRefresh && activeBeforeRefresh.getAttribute("data-action");
            var activeDetail = activeBeforeRefresh && activeBeforeRefresh.hasAttribute("data-torrent-detail");
            var activeSelect = activeBeforeRefresh && activeBeforeRefresh.hasAttribute("data-download-select");
            var activeSort = activeBeforeRefresh && activeBeforeRefresh.closest("[data-sort]");
            var activeSortKey = activeSort && activeSort.getAttribute("data-sort");
            var holder = document.createElement("div");
            holder.innerHTML = html;
            var incomingSlot = holder.querySelector("[data-torrents-slot]");
            if (incomingSlot) {
              var selectedSet = Object.create(null);
              selectedBeforeRefresh.forEach(function (hash) { selectedSet[hash] = true; });
              Array.prototype.forEach.call(incomingSlot.querySelectorAll("[data-download-select]"), function (node) {
                node.checked = !!selectedSet[node.getAttribute("data-hash") || ""];
              });
              currentSlot.replaceWith(incomingSlot);
              updateDownloadSelection();
              applyTorrentSort();
              applyTorrentFilter();
              var focusTarget = null;
              if (activeHash) {
                Array.prototype.some.call(incomingSlot.querySelectorAll("[data-hash]"), function (candidate) {
                  if (candidate.getAttribute("data-hash") !== activeHash) return false;
                  if (activeSelect && candidate.hasAttribute("data-download-select")) { focusTarget = candidate; return true; }
                  if (activeDetail && candidate.hasAttribute("data-torrent-detail")) { focusTarget = candidate; return true; }
                  if (activeAction && candidate.getAttribute("data-action") === activeAction) { focusTarget = candidate; return true; }
                  return false;
                });
              } else if (activeSortKey) {
                var refreshedHead = incomingSlot.querySelector('[data-sort="' + activeSortKey + '"]');
                focusTarget = refreshedHead && refreshedHead.querySelector("button") || refreshedHead;
              }
              if (focusTarget) focusTarget.focus();
            }
            return;
          }
        }
        page.innerHTML = html;
      })
      .catch(function (error) {
        page.innerHTML = '<div class="view"><div class="alert">Impossibile caricare i dati: ' +
          esc(error.message) + "</div></div>";
      })
      .then(function (value) {
        loadPromise = null;
        if (loadPending && document.body.contains(page)) {
          loadPending = false;
          window.setTimeout(load, 0);
        }
        return value;
      });
    return loadPromise;
  }

  // Inline feedback keeps the page context visible; browser alerts were easy
  // to miss on mobile and blocked the rest of the interface.
  var toastHost = null;
  function notify(message, kind) {
    if (!toastHost) {
      toastHost = document.createElement("div");
      toastHost.className = "toast-host";
      toastHost.setAttribute("aria-live", "polite");
      document.body.appendChild(toastHost);
    }
    var toast = document.createElement("div");
    toast.className = "toast " + (kind || "info");
    toast.setAttribute("role", kind === "err" ? "alert" : "status");
    var text = document.createElement("span");
    text.textContent = String(message || "");
    var close = document.createElement("button");
    close.className = "toast-close";
    close.type = "button";
    close.setAttribute("aria-label", "Chiudi messaggio");
    close.textContent = "×";
    close.addEventListener("click", function () { toast.remove(); });
    toast.appendChild(text);
    toast.appendChild(close);
    toastHost.appendChild(toast);
    window.setTimeout(function () { if (toast.parentNode) toast.remove(); }, kind === "err" ? 7000 : 3500);
  }

  var timer = null;
  function schedule() {
    if (timer) clearInterval(timer);
    var interval = view === "downloads" ? 1500 : 0;
    if (interval > 0) {
      timer = setInterval(function () {
        if (document.visibilityState === "visible") load();
      }, interval);
    }
  }

  document.addEventListener("click", function (event) {
    var bulkButton = event.target.closest("[data-download-bulk]");
    if (bulkButton) {
      var selected = Array.prototype.map.call(page.querySelectorAll("[data-download-select]:checked"), function (node) {
        return node.getAttribute("data-hash") || "";
      }).filter(Boolean);
      if (!selected.length) {
        notify("Seleziona almeno un torrent", "err");
        return;
      }
      var bulkAction = bulkButton.getAttribute("data-download-bulk");
      if (bulkAction === "remove") {
       openRemoveTorrent(selected, selected.length + " torrent selezionati", bulkButton);
        return;
      }
      var pathFor = function (hash) { return "/api/torrents/" + encodeURIComponent(hash) + "/" + bulkAction; };
      bulkButton.disabled = true;
      Promise.all(selected.map(function (hash) { return api(pathFor(hash), "POST", {}); }))
        .then(function () { load(); notify(selected.length + " torrent aggiornati", "ok"); })
        .catch(function (error) { notify("Azione bulk non riuscita: " + error.message, "err"); })
        .then(function () { bulkButton.disabled = false; });
      return;
    }
    var assignTag = event.target.closest("[data-download-assign-tag]");
    if (assignTag) {
      var tag = readAssignTag();
      var hashes = selectedHashes();
      if (!hashes.length) { notify("Seleziona almeno un torrent", "err"); return; }
      if (!tag) { notify("Scegli o inserisci un tag", "err"); return; }
      assignTag.disabled = true;
      registerDownloadTag(tag)
        .then(function () {
          return Promise.all(hashes.map(function (hash) {
            return api("/api/torrent-tags", "POST", { hash: hash, tag: tag });
          }));
        })
        .then(function () { load(); notify("Tag assegnato a " + hashes.length + " torrent", "ok"); })
        .catch(function (error) { notify("Tag non assegnato: " + error.message, "err"); })
        .then(function () { assignTag.disabled = false; });
      return;
    }
    var removeTag = event.target.closest("[data-download-remove-tag]");
    if (removeTag) {
      var hashesNoTag = selectedHashes();
      if (!hashesNoTag.length) { notify("Seleziona almeno un torrent", "err"); return; }
      removeTag.disabled = true;
      Promise.all(hashesNoTag.map(function (hash) {
        return api("/api/torrent-tags", "POST", { hash: hash, tag: "" });
      }))
        .then(function () { load(); notify("Tag rimosso da " + hashesNoTag.length + " torrent", "ok"); })
        .catch(function (error) { notify("Tag non rimosso: " + error.message, "err"); })
        .then(function () { removeTag.disabled = false; });
      return;
    }
    var tempApply = event.target.closest("[data-temp-apply]");
    if (tempApply) {
      applyTempLimits(tempApply, false);
      return;
    }
    var tempClear = event.target.closest("[data-temp-clear]");
    if (tempClear) {
      applyTempLimits(tempClear, true);
      return;
    }
    var sortHead = event.target.closest("[data-sort]");
    if (sortHead) {
      var sortTable = sortHead.closest("table");
      if (sortTable && sortTable.classList.contains("torrent-table")) {
        toggleTorrentSort(sortHead.getAttribute("data-sort") || "name");
      } else {
        toggleGenericSort(sortHead);
      }
      return;
    }
    var gapSearch = event.target.closest("[data-gap-search]");
    if (gapSearch) {
      var gapForm = page.querySelector('[data-json-form][data-render="releases"]');
      if (!gapForm) { notify("Modulo di ricerca non disponibile", "err"); return; }
      var setField = function (name, value) {
        var field = gapForm.querySelector('[name="' + name + '"]');
        if (field) field.value = value || "";
      };
      setField("series", gapSearch.getAttribute("data-series"));
      setField("season", gapSearch.getAttribute("data-season"));
      setField("episode", gapSearch.getAttribute("data-episode"));
      if (gapForm.requestSubmit) gapForm.requestSubmit();
      else gapForm.dispatchEvent(new Event("submit", { cancelable: true, bubbles: true }));
      gapForm.scrollIntoView({ behavior: "smooth", block: "center" });
      return;
    }
    var libraryToggleButton = event.target.closest("[data-library-toggle]");
    if (libraryToggleButton) {
      libraryToggleButton.disabled = true;
      libraryToggle(libraryToggleButton.getAttribute("data-library-toggle") || "series",
        libraryToggleButton.getAttribute("data-library-name") || "");
      window.setTimeout(function () { libraryToggleButton.disabled = false; }, 1200);
      return;
    }
    var libraryRemoveButton = event.target.closest("[data-library-remove]");
    if (libraryRemoveButton) {
      var removeConfirm = libraryRemoveButton.getAttribute("data-confirm");
      if (removeConfirm && !confirm(removeConfirm)) return;
      libraryRemoveButton.disabled = true;
      libraryRemove(libraryRemoveButton.getAttribute("data-library-remove") || "series",
        libraryRemoveButton.getAttribute("data-library-name") || "", libraryRemoveButton);
      return;
    }
    if (event.target.closest("[data-download-select-all]")) {
      var selectAll = event.target.closest("[data-download-select-all]");
      Array.prototype.forEach.call(page.querySelectorAll("[data-download-select]"), function (node) { node.checked = selectAll.checked; });
      updateDownloadSelection();
      return;
    }
    var exportButton = event.target.closest("[data-torrent-export]");
    if (exportButton && exportButton.getAttribute("data-torrent-export") === "magnet") {
      event.preventDefault();
      var magnetValue = exportButton.getAttribute("data-magnet") || "";
      if (magnetValue) { copyText(magnetValue).then(function () { notify("Magnet copiato", "ok"); }); }
      return;
    }
    var detailButton = event.target.closest("[data-torrent-detail]");
    if (detailButton) {
       openTorrentDetail(detailButton.getAttribute("data-hash") || "", detailButton);
      return;
    }
    var httpDetailButton = event.target.closest("[data-http-detail]");
    if (httpDetailButton) {
      openHTTPDetail(httpDetailButton);
      return;
    }
    var subdetailButton = event.target.closest("[data-torrent-subdetail]");
    if (subdetailButton) {
      loadTorrentTab(subdetailButton.getAttribute("data-torrent-subdetail") || "general", subdetailButton);
      return;
    }
    if (event.target.closest("[data-torrent-detail-close]")) {
      var panel = page.querySelector("[data-torrent-detail-panel]");
       if (panel) closeAccessibleDialog(panel);
      return;
    }
    if (event.target.matches && event.target.matches("[data-torrent-detail-panel]")) {
       closeAccessibleDialog(event.target);
      return;
    }
    var element = event.target.closest("[data-action]");
    if (!element) return;
    var action = element.getAttribute("data-action");
    var hash = element.getAttribute("data-hash") || "";

    if (action === "refresh") {
      if (partials[view]) { load(); } else { location.reload(); }
      return;
    }
    if (action === "run-cycle") {
      element.disabled = true;
      api("/api/run_now", "POST", {})
        .then(function () { if (partials[view]) { load(); } else { location.reload(); } })
        .catch(function (error) { notify("Ciclo non avviato: " + error.message, "err"); })
        .then(function () { element.disabled = false; });
      return;
    }
    if (!hash) return;

    if (action === "remove" && !confirm("Rimuovere il torrent dalla sessione? I file restano su disco.")) {
      return;
    }
    var actions = {
      pause: ["/api/torrents/" + encodeURIComponent(hash) + "/pause", "POST", {}],
      resume: ["/api/torrents/" + encodeURIComponent(hash) + "/resume", "POST", {}],
      recheck: ["/api/torrents/" + encodeURIComponent(hash) + "/recheck", "POST", {}],
      remove: ["/api/torrents/" + encodeURIComponent(hash) + "/remove", "POST", { delete_files: false }]
    };
    var entry = actions[action];
    if (!entry) return;
    element.disabled = true;
    api(entry[0], entry[1], entry[2])
      .then(load)
      .catch(function (error) { notify("Azione non riuscita: " + error.message, "err"); })
      .then(function () { element.disabled = false; });
  });

  function updateDownloadSelection() {
    var count = page.querySelectorAll("[data-download-select]:checked").length;
    var label = page.querySelector("[data-download-selected-count]");
    if (label) label.textContent = count + " selezionati";
    var selectAll = page.querySelector("[data-download-select-all]");
    var all = page.querySelectorAll("[data-download-select]");
    if (selectAll) selectAll.checked = all.length > 0 && count === all.length;
  }
  document.addEventListener("change", function (event) {
    if (event.target.closest("[data-download-select]")) updateDownloadSelection();
    var tagSelect = event.target.closest("[data-download-tag-select]");
    if (tagSelect) {
      var newTagInput = page.querySelector("[data-download-new-tag]");
      if (newTagInput) {
        newTagInput.hidden = tagSelect.value !== "__new__";
        if (tagSelect.value === "__new__") newTagInput.focus();
      }
      return;
    }
    if (event.target.closest("[data-download-tag-filter]")) {
      applyTorrentFilter();
      return;
    }
    var autoRemove = event.target.closest("[data-auto-remove-completed]");
    if (autoRemove) {
      api("/api/config/settings", "POST", { key: "auto_remove_completed", value: autoRemove.checked ? "true" : "false" })
        .then(function () { notify("Preferenza salvata", "ok"); })
        .catch(function (error) { notify("Preferenza non salvata: " + error.message, "err"); });
    }
  });

  function selectedHashes() {
    return Array.prototype.map.call(page.querySelectorAll("[data-download-select]:checked"), function (node) {
      return node.getAttribute("data-hash") || "";
    }).filter(Boolean);
  }

  function readAssignTag() {
    var select = page.querySelector("[data-download-tag-select]");
    if (!select) return "";
    if (select.value === "__new__") {
      var input = page.querySelector("[data-download-new-tag]");
      return input ? input.value.trim() : "";
    }
    return select.value.trim();
  }

  // registerDownloadTag records a new tag in the shared catalog so it also
  // appears in the filter; a failure is not fatal for the assignment itself.
  function registerDownloadTag(tag) {
    if (!tag) return Promise.resolve();
    return api("/api/download-tags", "POST", { tag: tag }).catch(function () { return null; });
  }

  function applyTempLimits(button, clear) {
    var dl = page.querySelector("[data-temp-dl]");
    var ul = page.querySelector("[data-temp-ul]");
    var minutes = page.querySelector("[data-temp-minutes]");
    var message = page.querySelector("[data-temp-message]");
    var toInt = function (node) {
      var parsed = parseInt(node && node.value ? node.value : "0", 10);
      return isNaN(parsed) || parsed < 0 ? 0 : parsed;
    };
    var body = clear ? { clear: true } : {
      download_kib: toInt(dl),
      upload_kib: toInt(ul),
      minutes: toInt(minutes)
    };
    button.disabled = true;
    api("/api/torrents/temp-limits", "POST", body)
      .then(function () {
        if (message) message.textContent = clear ? "Limite temporaneo rimosso" : "Limite temporaneo applicato";
        notify(clear ? "Limite temporaneo rimosso" : "Limite temporaneo applicato", "ok");
        load();
      })
      .catch(function (error) {
        if (message) message.textContent = error.message;
        notify("Limite non applicato: " + error.message, "err");
      })
      .then(function () { button.disabled = false; });
  }

  // ---- torrent table sorting and tag filter -------------------------------
  var torrentSort = { key: "", direction: 1 };

  function toggleTorrentSort(key) {
    if (torrentSort.key === key) {
      torrentSort.direction = -torrentSort.direction;
    } else {
      torrentSort.key = key;
      torrentSort.direction = 1;
    }
    applyTorrentSort();
  }

  function rowSortValue(row, key) {
    if (key === "name") return (row.getAttribute("data-name") || "").toLowerCase();
    var raw = row.getAttribute("data-sort-" + key) || "0";
    var number = parseFloat(raw);
    return isNaN(number) ? 0 : number;
  }

  function applyTorrentSort() {
    var body = page.querySelector(".torrent-table tbody");
    if (!body) return;
    var key = torrentSort.key;
    if (key) {
      var rows = Array.prototype.slice.call(body.querySelectorAll("tr[data-hash]"));
      rows.sort(function (left, right) {
        var a = rowSortValue(left, key);
        var b = rowSortValue(right, key);
        if (a < b) return -1 * torrentSort.direction;
        if (a > b) return 1 * torrentSort.direction;
        return 0;
      });
      rows.forEach(function (row) { body.appendChild(row); });
    }
    Array.prototype.forEach.call(page.querySelectorAll("[data-sort]"), function (head) {
      if (head.getAttribute("data-sort") === key) {
        head.setAttribute("data-sort-dir", torrentSort.direction === 1 ? "asc" : "desc");
        head.setAttribute("aria-sort", torrentSort.direction === 1 ? "ascending" : "descending");
      } else {
        head.removeAttribute("data-sort-dir");
        head.setAttribute("aria-sort", "none");
      }
    });
  }

  function applyTorrentFilter() {
    var filter = page.querySelector("[data-download-tag-filter]");
    if (!filter) return;
    var value = filter.value;
    var wanted = value.toLowerCase();
    Array.prototype.forEach.call(page.querySelectorAll(".torrent-table tbody tr[data-hash]"), function (row) {
      var tags = (row.getAttribute("data-tags") || "").toLowerCase();
      var show = true;
      if (value === "__none__") {
        show = tags.trim() === "";
      } else if (wanted) {
        show = tags.split(",").map(function (part) { return part.trim(); }).indexOf(wanted) >= 0;
      }
      row.hidden = !show;
    });
  }

  function copyText(value) {
    if (navigator.clipboard && navigator.clipboard.writeText) {
      return navigator.clipboard.writeText(value).catch(function () { /* ignore */ });
    }
    var area = document.createElement("textarea");
    area.value = value;
    area.style.position = "fixed";
    area.style.opacity = "0";
    document.body.appendChild(area);
    area.select();
    try { document.execCommand("copy"); } catch (error) { /* ignore */ }
    area.remove();
    return Promise.resolve();
  }

  function openHTTPDetail(button) {
    var row = button.closest("[data-http-download]");
    if (!row) return;
    var value = function (name, fallback) {
      var result = row.getAttribute(name) || "";
      return result || fallback || "—";
    };
    var status = value("data-http-status");
    var progress = Number(row.getAttribute("data-http-progress"));
    var downloaded = Number(row.getAttribute("data-http-downloaded")) || 0;
    var total = Number(row.getAttribute("data-http-total")) || 0;
    var speed = Number(row.getAttribute("data-http-speed")) || 0;
    var overlay = document.createElement("div");
    overlay.className = "overlay";
    overlay._removeOnClose = true;
    var modal = document.createElement("div");
    modal.className = "modal torrent-modal";
    modal.setAttribute("role", "dialog");
    modal.setAttribute("aria-modal", "true");
    modal.setAttribute("aria-labelledby", "http-detail-title");
    var head = document.createElement("div");
    head.className = "modal-head";
    var title = document.createElement("h3");
    title.id = "http-detail-title";
    title.textContent = value("data-name", "Download HTTP");
    var close = document.createElement("button");
    close.className = "btn sm";
    close.type = "button";
    close.textContent = "Chiudi";
    head.appendChild(title);
    head.appendChild(close);
    var body = document.createElement("div");
    body.className = "modal-body";
    body.appendChild(statGrid([
      ["Stato", status === "downloading" ? "In scarico" : status],
      ["Metodo", "HTTP"],
      ["Progresso", isFinite(progress) && progress >= 0 ? progress.toFixed(1) + "%" : "—"],
      ["Scaricato", humanBytes(downloaded) + (total > 0 ? " / " + humanBytes(total) : "")],
      ["Velocità", humanRate(speed)],
      ["URL", value("data-http-url")],
      ["File finale", value("data-http-destination")],
      ["File temporaneo", value("data-http-temporary")],
      ["Aggiornato", value("data-http-updated")],
      ["Errore", value("data-http-error", "")]
    ]));
    modal.appendChild(head);
    modal.appendChild(body);
    overlay.appendChild(modal);
    document.body.appendChild(overlay);
    openAccessibleDialog(overlay, button);
    function dismiss() { closeAccessibleDialog(overlay, true); }
    close.addEventListener("click", dismiss);
    overlay.addEventListener("click", function (event) {
      if (event.target === overlay) dismiss();
    });
  }

  // ---- torrent detail modal ----------------------------------------------
  var activeTorrentHash = "";

  function torrentDetailPanel() { return page.querySelector("[data-torrent-detail-panel]"); }

  // ---- torrent removal with the four rextto levels ------------------------
  function openRemoveTorrent(hashes, name, opener) {
    var panel = page.querySelector("[data-torrent-remove-panel]");
    if (!panel || !hashes || !hashes.length) return;
    panel._hashes = hashes.slice();
    var label = panel.querySelector("[data-torrent-remove-name]");
    if (label) label.textContent = name || "";
    var message = panel.querySelector("[data-torrent-remove-message]");
    if (message) message.textContent = "";
    openAccessibleDialog(panel, opener);
  }

  document.addEventListener("click", function (event) {
    var backdrop = event.target.matches && event.target.matches("[data-torrent-remove-panel]");
    if (backdrop) { closeAccessibleDialog(event.target); return; }
    var rowRemove = event.target.closest("[data-torrent-remove]");
    if (rowRemove) {
      openRemoveTorrent([rowRemove.getAttribute("data-hash") || ""].filter(Boolean), rowRemove.getAttribute("data-name") || "", rowRemove);
      return;
    }
    if (event.target.closest("[data-torrent-remove-close]")) {
      var closePanel = page.querySelector("[data-torrent-remove-panel]");
       if (closePanel) closeAccessibleDialog(closePanel);
      return;
    }
    var modeButton = event.target.closest("[data-remove-mode]");
    if (!modeButton) return;
    var panel = modeButton.closest("[data-torrent-remove-panel]");
    if (!panel) return;
    var hashes = panel._hashes || [];
    if (!hashes.length) return;
    var mode = modeButton.getAttribute("data-remove-mode");
    var deleteFiles = mode === "files" || mode === "files_blocklist";
    var blocklist = mode === "blocklist" || mode === "files_blocklist";
    if (deleteFiles && !confirm("Eliminare anche i file scaricati?")) return;
    var message = panel.querySelector("[data-torrent-remove-message]");
    modeButton.disabled = true;
    Promise.all(hashes.map(function (hash) {
      return api("/api/torrents/" + encodeURIComponent(hash) + "/remove", "POST", { delete_files: deleteFiles, blocklist: blocklist });
    })).then(function () {
      closeAccessibleDialog(panel);
      notify(hashes.length === 1 ? "Torrent rimosso" : hashes.length + " torrent rimossi", "ok");
      if (partials[view]) { load(); } else { location.reload(); }
    }).catch(function (error) {
      if (message) message.textContent = error.message;
      notify("Rimozione non riuscita: " + error.message, "err");
    }).then(function () { modeButton.disabled = false; });
  });

  function openTorrentDetail(hash, opener) {    if (!hash) return;
    activeTorrentHash = hash;
    var panel = torrentDetailPanel();
    if (!panel) return;
    openAccessibleDialog(panel, opener);
    loadTorrentTab("general");
  }

  function loadTorrentTab(tab, button) {
    var hash = activeTorrentHash;
    if (!hash) return;
    var panel = torrentDetailPanel();
    if (!panel) return;
    var output = panel.querySelector("[data-torrent-subdetail-output]");
    if (!output) return;
    Array.prototype.forEach.call(panel.querySelectorAll("[data-torrent-subdetail]"), function (tabButton) {
      var active = tabButton.getAttribute("data-torrent-subdetail") === tab;
      tabButton.classList.toggle("tab-active", active);
      tabButton.setAttribute("aria-selected", active ? "true" : "false");
    });
    if (button) button.disabled = true;
    output.hidden = false;
    output.innerHTML = '<p class="muted">Caricamento…</p>';
    var endpoint = "/api/torrents/" + encodeURIComponent(hash);
    if (tab === "trackers") endpoint += "/trackers";
    else if (tab === "files") endpoint += "/files";
    else if (tab === "peers") endpoint += "/peers";
    api(endpoint, "GET")
      .then(function (data) { renderTorrentTab(output, tab, hash, data); })
      .catch(function (error) { output.innerHTML = '<p class="alert">' + esc(error.message) + "</p>"; })
      .then(function () { if (button) button.disabled = false; });
  }

  function statGrid(pairs) {
    var wrap = document.createElement("div");
    wrap.className = "stat-grid";
    pairs.forEach(function (pair) {
      if (pair === null || pair === undefined || pair === "") return;
      var row = document.createElement("div");
      row.className = "row";
      var label = document.createElement("span");
      label.textContent = pair[0];
      var value = document.createElement("strong");
      value.textContent = pair[1];
      row.appendChild(label);
      row.appendChild(value);
      wrap.appendChild(row);
    });
    return wrap;
  }

  function accessibleDataTable(table, captionText) {
    if (!table) return;
    if (captionText && !table.querySelector("caption")) {
      var caption = document.createElement("caption");
      caption.className = "sr-only";
      caption.textContent = captionText;
      table.insertBefore(caption, table.firstChild);
    }
    Array.prototype.forEach.call(table.querySelectorAll("thead th"), function (th) {
      if (!th.hasAttribute("scope")) th.setAttribute("scope", "col");
    });
  }

  function renderTorrentTab(output, tab, hash, data) {
    output.innerHTML = "";
    if (tab === "trackers") {
      var trackers = (data && data.trackers) || [];
      var table = document.createElement("table");
      table.className = "data-table";
      table.innerHTML = "<thead><tr><th>Tracker</th><th>Tier</th><th>Esito</th></tr></thead>";
      accessibleDataTable(table, "Tracker del torrent");
      var tbody = document.createElement("tbody");
      trackers.forEach(function (tracker) {
        var tr = document.createElement("tr");
        var url = document.createElement("td");
        url.className = "truncate";
        url.textContent = String(tracker.url || "");
        var tier = document.createElement("td");
        tier.className = "numeric";
        tier.textContent = String(tracker.tier === undefined ? "—" : tracker.tier);
        var state = document.createElement("td");
        state.textContent = tracker.verified ? "verificato" : "non verificato";
        tr.appendChild(url); tr.appendChild(tier); tr.appendChild(state);
        tbody.appendChild(tr);
      });
      table.appendChild(tbody);
      output.appendChild(table);
      if (!trackers.length) output.appendChild(muted("Nessun tracker."));
      var form = document.createElement("div");
      form.className = "field span-full";
      var label = document.createElement("span");
      label.textContent = "Modifica tracker (tier|url per riga)";
      var textarea = document.createElement("textarea");
      textarea.className = "input";
      textarea.setAttribute("data-trackers-text", "");
      textarea.setAttribute("aria-label", "Modifica tracker, un tier e URL per riga");
      textarea.rows = 4;
      textarea.value = trackers.map(function (tracker) {
        return (tracker.tier === undefined ? "0" : tracker.tier) + "|" + (tracker.url || "");
      }).join("\n");
      var save = document.createElement("button");
      save.className = "btn sm primary";
      save.textContent = "Salva tracker";
      save.addEventListener("click", function () {
        var lines = textarea.value.split("\n").map(function (line) { return line.trim(); }).filter(Boolean);
        var items = lines.map(function (line) {
          var parts = line.split("|");
          return { url: (parts.length > 1 ? parts[1] : parts[0]).trim(), tier: parseInt(parts[0], 10) || 0 };
        });
        save.disabled = true;
        api("/api/torrents/" + encodeURIComponent(hash) + "/trackers", "POST", { trackers: items })
          .then(function () { notify("Tracker salvati", "ok"); loadTorrentTab("trackers"); })
          .catch(function (error) { notify("Tracker non salvati: " + error.message, "err"); })
          .then(function () { save.disabled = false; });
      });
      form.appendChild(label); form.appendChild(textarea); form.appendChild(save);
      output.appendChild(form);
      return;
    }
    if (tab === "files") {
      var files = (data && data.files) || [];
      var fileTable = document.createElement("table");
      fileTable.className = "data-table";
      fileTable.innerHTML = "<thead><tr><th>File</th><th>Dimensione</th><th>Scaricato</th><th>Priorità</th></tr></thead>";
      accessibleDataTable(fileTable, "File del torrent");
      var fileBody = document.createElement("tbody");
      var priorities = files.map(function (file) { return Number(file.priority) || 0; });
      files.forEach(function (file, index) {
        priorities[index] = Number(file.priority) || 0;
        var tr = document.createElement("tr");
        var path = document.createElement("td");
        path.className = "truncate";
        path.title = String(file.path || "");
        path.textContent = String(file.path || "");
        var size = document.createElement("td");
        size.className = "numeric";
        size.textContent = humanBytes(file.size || 0);
        var done = document.createElement("td");
        done.className = "numeric";
        done.textContent = humanBytes(file.downloaded || 0);
        var priority = document.createElement("td");
        var select = document.createElement("select");
        select.className = "input";
        select.setAttribute("aria-label", "Priorità del file " + String(file.path || index + 1));
        [["0", "Salta"], ["1", "Normale"], ["4", "Predefinita"], ["6", "Alta"], ["7", "Massima"]].forEach(function (option) {
          var node = document.createElement("option");
          node.value = option[0];
          node.textContent = option[1];
          if (Number(file.priority) === Number(option[0])) node.selected = true;
          select.appendChild(node);
        });
        select.addEventListener("change", function () {
          priorities[index] = parseInt(select.value, 10) || 0;
          api("/api/torrents/" + encodeURIComponent(hash) + "/files/priority", "POST", { priorities: priorities })
            .then(function () { notify("Priorità salvata", "ok"); })
            .catch(function (error) { notify("Priorità non salvata: " + error.message, "err"); });
        });
        priority.appendChild(select);
        tr.appendChild(path); tr.appendChild(size); tr.appendChild(done); tr.appendChild(priority);
        fileBody.appendChild(tr);
      });
      fileTable.appendChild(fileBody);
      output.appendChild(fileTable);
      if (!files.length) output.appendChild(muted("Nessun file (metadati non ancora disponibili)."));
      return;
    }
    if (tab === "peers") {
      var peers = (data && data.peers) || [];
      var peerTable = document.createElement("table");
      peerTable.className = "data-table";
      peerTable.innerHTML = "<thead><tr><th>Indirizzo</th><th>Client</th><th>↓</th><th>↑</th><th>Seed</th></tr></thead>";
      accessibleDataTable(peerTable, "Peer del torrent");
      var peerBody = document.createElement("tbody");
      peers.forEach(function (peer) {
        var tr = document.createElement("tr");
        var address = document.createElement("td");
        address.textContent = String(peer.address || "");
        var client = document.createElement("td");
        client.textContent = String(peer.client || "—");
        var down = document.createElement("td");
        down.className = "numeric";
        down.textContent = humanRate(peer.download_rate || 0);
        var up = document.createElement("td");
        up.className = "numeric";
        up.textContent = humanRate(peer.upload_rate || 0);
        var seed = document.createElement("td");
        seed.textContent = peer.seed ? "sì" : "no";
        tr.appendChild(address); tr.appendChild(client); tr.appendChild(down); tr.appendChild(up); tr.appendChild(seed);
        peerBody.appendChild(tr);
      });
      peerTable.appendChild(peerBody);
      output.appendChild(peerTable);
      if (!peers.length) output.appendChild(muted("Nessun peer connesso."));
      return;
    }
    if (tab === "limits") {
      var torrent = (data && data.torrent) || {};
      var limitForm = document.createElement("div");
      limitForm.className = "form-grid";
      limitForm.appendChild(numberField("data-limit-dl", "Download (KiB/s)", torrent.download_limit));
      limitForm.appendChild(numberField("data-limit-ul", "Upload (KiB/s)", torrent.upload_limit));
      limitForm.appendChild(numberField("data-limit-ratio", "Ratio seed", torrent.seed_ratio));
      limitForm.appendChild(numberField("data-limit-days", "Giorni seed", torrent.seed_days));
      var actions = document.createElement("div");
      actions.className = "form-actions";
      var saveLimits = document.createElement("button");
      saveLimits.className = "btn sm primary";
      saveLimits.textContent = "Salva limiti";
      saveLimits.addEventListener("click", function () {
        var value = function (name, multiplier) {
          var node = limitForm.querySelector("[" + name + "]");
          var parsed = parseFloat(node && node.value);
          if (isNaN(parsed)) parsed = -1;
          return Math.round(parsed * (multiplier || 1));
        };
        saveLimits.disabled = true;
        api("/api/torrents/" + encodeURIComponent(hash) + "/limits", "POST", {
          download_limit: value("data-limit-dl", 1024),
          upload_limit: value("data-limit-ul", 1024),
          seed_ratio: value("data-limit-ratio"),
          seed_days: value("data-limit-days")
        })
          .then(function () { notify("Limiti salvati", "ok"); })
          .catch(function (error) { notify("Limiti non salvati: " + error.message, "err"); })
          .then(function () { saveLimits.disabled = false; });
      });
      actions.appendChild(saveLimits);
      limitForm.appendChild(actions);
      output.appendChild(limitForm);
      return;
    }
    if (tab === "storage") {
      var storageForm = document.createElement("div");
      storageForm.className = "form-grid";
      storageForm.appendChild(textField("data-storage-path", "Nuovo percorso di storage", torrent_save_path(data)));
      var storageActions = document.createElement("div");
      storageActions.className = "form-actions";
      var moveButton = document.createElement("button");
      moveButton.className = "btn sm primary";
      moveButton.textContent = "Sposta storage";
      moveButton.addEventListener("click", function () {
        var node = storageForm.querySelector("[data-storage-path]");
        var path = node ? node.value.trim() : "";
        if (!path) { notify("Inserisci un percorso", "err"); return; }
        moveButton.disabled = true;
        api("/api/torrents/" + encodeURIComponent(hash) + "/storage", "POST", { path: path })
          .then(function () { notify("Spostamento avviato", "ok"); })
          .catch(function (error) { notify("Spostamento non riuscito: " + error.message, "err"); })
          .then(function () { moveButton.disabled = false; });
      });
      storageActions.appendChild(moveButton);
      storageForm.appendChild(storageActions);
      output.appendChild(storageForm);
      return;
    }
    renderTorrentGeneral(output, hash, data);
  }

  function muted(text) {
    var node = document.createElement("p");
    node.className = "muted";
    node.textContent = text;
    return node;
  }

  function numberField(attr, label, value) {
    var wrap = document.createElement("label");
    wrap.className = "field";
    var span = document.createElement("span");
    span.textContent = label;
    var input = document.createElement("input");
    input.className = "input";
    input.type = "number";
    input.setAttribute(attr, "");
    if (value !== undefined && value !== null && value !== "") input.value = String(value);
    wrap.appendChild(span); wrap.appendChild(input);
    return wrap;
  }

  function textField(attr, label, value) {
    var wrap = document.createElement("label");
    wrap.className = "field span-full";
    var span = document.createElement("span");
    span.textContent = label;
    var input = document.createElement("input");
    input.className = "input";
    input.setAttribute(attr, "");
    input.value = value || "";
    wrap.appendChild(span); wrap.appendChild(input);
    return wrap;
  }

  function torrent_save_path(data) {
    var torrent = (data && data.torrent) || {};
    return String(torrent.save_path || "");
  }

  function renderTorrentGeneral(output, hash, data) {
    var torrent = (data && data.torrent) || {};
    var magnet = (data && data.magnet) || "";
    var nameNode = torrentDetailPanel() && torrentDetailPanel().querySelector("[data-torrent-detail-name]");
    if (nameNode) nameNode.textContent = torrent.name || "Dettagli torrent";
    var exportMagnet = torrentDetailPanel() && torrentDetailPanel().querySelector('[data-torrent-export="magnet"]');
    if (exportMagnet) exportMagnet.setAttribute("data-magnet", magnet);
    var exportTorrent = torrentDetailPanel() && torrentDetailPanel().querySelector('[data-torrent-export="export.torrent"]');
    if (exportTorrent) exportTorrent.href = "/api/torrents/" + encodeURIComponent(hash) + "/export.torrent";

    output.appendChild(statGrid([
      ["Stato", String(torrent.state || "—")],
      ["Progresso", (Number(torrent.progress) || 0).toFixed(1) + "%"],
      ["Dimensione", humanBytes(torrent.total_size || 0)],
      ["Scaricato", humanBytes(torrent.total_done || 0)],
      ["↓ / ↑", humanRate(torrent.download_rate || 0) + " / " + humanRate(torrent.upload_rate || 0)],
      ["Peer / Seed", String(torrent.num_peers || 0) + " / " + String(torrent.num_seeds || 0)],
      ["Posizione coda", String(torrent.queue_position === undefined ? "—" : torrent.queue_position)],
      ["Metadata", torrent.has_metadata ? "presenti" : "in attesa"],
      ["Versione torrent", String(torrent.torrent_version || "—")],
      ["Auto-managed", torrent.auto_managed ? "sì" : "no"],
      ["Percorso", String(torrent.save_path || "—")]
    ]));

    var toolbar = document.createElement("div");
    toolbar.className = "toolbar";
    var toggle = function (label, endpoint, body, confirmText) {
      var button = document.createElement("button");
      button.className = "btn sm";
      button.textContent = label;
      button.addEventListener("click", function () {
        if (confirmText && !confirm(confirmText)) return;
        button.disabled = true;
        api(endpoint, "POST", body)
          .then(function () { notify(label + ": fatto", "ok"); loadTorrentTab("general"); })
          .catch(function (error) { notify(label + " non riuscito: " + error.message, "err"); })
          .then(function () { button.disabled = false; });
      });
      return button;
    };
    toolbar.appendChild(toggle(torrent.no_rename ? "Rinomina abilitata" : "Non rinominare", "/api/torrents/" + encodeURIComponent(hash) + "/no_rename", { value: !torrent.no_rename }));
    toolbar.appendChild(toggle("Annuncia", "/api/torrents/" + encodeURIComponent(hash) + "/reannounce", {}));
    toolbar.appendChild(toggle("Riavvia torrent", "/api/torrents/" + encodeURIComponent(hash) + "/restart", {}));
    toolbar.appendChild(toggle(torrent.super_seeding ? "Disattiva super seeding" : "Super seeding", "/api/torrents/" + encodeURIComponent(hash) + "/super-seeding", { enabled: !torrent.super_seeding }));
    toolbar.appendChild(toggle("Pin", "/api/torrents/pin", { hash: hash }));
    toolbar.appendChild(toggle("Segna come fallito", "/api/torrents/" + encodeURIComponent(hash) + "/mark_failed", {}, "Segnare questo torrent come fallito?"));
    var tagButton = document.createElement("button");
    tagButton.className = "btn sm";
    tagButton.textContent = "Tag";
    tagButton.addEventListener("click", function () {
      var entered = window.prompt("Tag del torrent (separati da virgola):", "");
      if (entered === null) return;
      api("/api/torrent-tags", "POST", { hash: hash, tag: entered.trim() })
        .then(function () { notify("Tag aggiornato", "ok"); load(); loadTorrentTab("general"); })
        .catch(function (error) { notify("Tag non aggiornato: " + error.message, "err"); });
    });
    toolbar.appendChild(tagButton);
    output.appendChild(toolbar);

    // Removal levels.
    var removeForm = document.createElement("div");
    removeForm.className = "form-grid";
    var removeWrap = document.createElement("label");
    removeWrap.className = "field";
    var removeLabel = document.createElement("span");
    removeLabel.textContent = "Rimozione";
    var removeSelect = document.createElement("select");
    removeSelect.className = "input";
    removeSelect.setAttribute("data-remove-mode", "");
    [["session", "Solo sessione"], ["files", "Sessione + file"], ["blocklist", "Sessione + blocklist"], ["files_blocklist", "File + blocklist"]].forEach(function (option) {
      var node = document.createElement("option");
      node.value = option[0];
      node.textContent = option[1];
      removeSelect.appendChild(node);
    });
    removeWrap.appendChild(removeLabel); removeWrap.appendChild(removeSelect);
    var removeActions = document.createElement("div");
    removeActions.className = "form-actions";
    var removeButton = document.createElement("button");
    removeButton.className = "btn sm danger";
    removeButton.textContent = "Rimuovi";
    removeButton.addEventListener("click", function () {
      var mode = removeSelect.value;
      var deleteFiles = mode === "files" || mode === "files_blocklist";
      var blocklist = mode === "blocklist" || mode === "files_blocklist";
      if (deleteFiles && !confirm("Eliminare anche i file scaricati?")) return;
      removeButton.disabled = true;
      api("/api/torrents/" + encodeURIComponent(hash) + "/remove", "POST", { delete_files: deleteFiles, blocklist: blocklist })
        .then(function () { notify("Torrent rimosso", "ok"); var panel = torrentDetailPanel(); if (panel) closeAccessibleDialog(panel); load(); })
        .catch(function (error) { notify("Rimozione non riuscita: " + error.message, "err"); })
        .then(function () { removeButton.disabled = false; });
    });
    removeActions.appendChild(removeButton);
    removeForm.appendChild(removeWrap); removeForm.appendChild(removeActions);
    output.appendChild(removeForm);

    // Web seeds.
    var webForm = document.createElement("div");
    webForm.className = "form-grid";
    webForm.appendChild(textField("data-web-seeds", "Web seed (URL separati da spazio)", ""));
    var webActions = document.createElement("div");
    webActions.className = "form-actions";
    var addSeed = document.createElement("button");
    addSeed.className = "btn sm";
    addSeed.textContent = "Aggiungi";
    addSeed.addEventListener("click", function () {
      var node = webForm.querySelector("[data-web-seeds]");
      var urls = (node && node.value.trim()) || "";
      if (!urls) { notify("Inserisci almeno un URL", "err"); return; }
      addSeed.disabled = true;
      api("/api/torrents/" + encodeURIComponent(hash) + "/web-seeds", "POST", { urls: urls, remove: false })
        .then(function () { notify("Web seed aggiunto", "ok"); })
        .catch(function (error) { notify("Web seed non aggiunto: " + error.message, "err"); })
        .then(function () { addSeed.disabled = false; });
    });
    var removeSeed = document.createElement("button");
    removeSeed.className = "btn sm danger";
    removeSeed.textContent = "Rimuovi";
    removeSeed.addEventListener("click", function () {
      var node = webForm.querySelector("[data-web-seeds]");
      var urls = (node && node.value.trim()) || "";
      if (!urls) { notify("Inserisci almeno un URL", "err"); return; }
      removeSeed.disabled = true;
      api("/api/torrents/" + encodeURIComponent(hash) + "/web-seeds", "POST", { urls: urls, remove: true })
        .then(function () { notify("Web seed rimosso", "ok"); })
        .catch(function (error) { notify("Web seed non rimosso: " + error.message, "err"); })
        .then(function () { removeSeed.disabled = false; });
    });
    webActions.appendChild(addSeed); webActions.appendChild(removeSeed);
    webForm.appendChild(webActions);
    output.appendChild(webForm);
  }

  document.addEventListener("visibilitychange", schedule);
  schedule();

  // ---- add/upload torrent --------------------------------------------------
  function uploadTorrent(file, form) {
    var headers = { "Content-Type": "application/octet-stream" };
    var uploadURL = "/api/upload-torrent";
    var savePath = form && form.querySelector("[data-torrent-save-path]");
    if (savePath && savePath.value.trim()) uploadURL += "?save_path=" + encodeURIComponent(savePath.value.trim());
    return file.arrayBuffer().then(function (buffer) {
      return fetch(uploadURL, {
        method: "POST", headers: headers, body: buffer, credentials: "same-origin"
      });
    }).then(function (response) {
      return handleResponse(response);
    });
  }
  Array.prototype.forEach.call(document.querySelectorAll("[data-torrent-add]"), function (form) {
    var message = form.querySelector("[data-torrent-message]");
    var upload = form.querySelector("[data-torrent-upload]");
    if (upload) upload.addEventListener("change", function () {
      var file = upload.files && upload.files[0];
      if (!file) return;
      if (message) message.textContent = "Caricamento…";
      upload.disabled = true;
      uploadTorrent(file, form).then(function () {
        if (message) message.textContent = "Torrent caricato";
        if (partials[view]) load();
      }).catch(function (error) {
        if (message) message.textContent = error.message;
        notify("Upload non riuscito: " + error.message, "err");
      }).then(function () { upload.disabled = false; });
    });
    form.addEventListener("submit", function (event) {
      event.preventDefault();
      var magnet = form.querySelector("[data-torrent-magnet]");
      var value = magnet ? magnet.value.trim() : "";
      if (!value) {
        if (message) message.textContent = "Inserisci un magnet o un URL";
        return;
      }
      var option = function (name) {
        var node = form.querySelector('[data-torrent-option="' + name + '"]');
        return !!(node && node.checked);
      };
      var savePath = form.querySelector("[data-torrent-save-path]");
      var body = {
        magnet: value,
        save_path: savePath ? savePath.value.trim() : "",
        start_paused: !option("start"),
        no_rename: option("no_rename"),
        sequential: option("sequential"),
        seed_mode: option("seed_mode"),
        queue_top: option("queue_top"),
        first_last: option("first_last"),
        stop_at_metadata: option("metadata_only"),
        preallocate: option("preallocate")
      };
      var button = form.querySelector("button[type=submit]");
      if (button) button.disabled = true;
      api("/api/send-magnet", "POST", body).then(function () {
        if (message) message.textContent = "Torrent accodato";
        if (magnet) magnet.value = "";
        if (partials[view]) load();
      }).catch(function (error) {
        if (message) message.textContent = error.message;
        notify("Torrent non aggiunto: " + error.message, "err");
      }).then(function () { if (button) button.disabled = false; });
    });
  });

  // ---- generic list pages -------------------------------------------------
  function esc(value) {
    return String(value === null || value === undefined ? "" : value)
      .replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;")
      .replace(/"/g, "&quot;");
  }
  // safeHref returns an escaped href only for http/https/magnet links, so a
  // malicious release or comic field cannot inject javascript: URLs.
  function safeHref(value) {
    var href = String(value === null || value === undefined ? "" : value).trim();
    if (!/^(https?:|magnet:)/i.test(href)) return "";
    return esc(href);
  }
  function humanBytes(value) {
    var n = Number(value);
    if (!isFinite(n) || n <= 0) return "0 B";
    var units = ["B", "KB", "MB", "GB", "TB", "PB"];
    var index = 0;
    while (n >= 1024 && index < units.length - 1) { n /= 1024; index++; }
    return (index === 0 ? n.toFixed(0) : n.toFixed(1)) + " " + units[index];
  }
  // folderLabel reduces an archived path to its destination folder: for a media
  // file it shows the containing folder, for a directory the last segment.
  function folderLabel(path) {
    var value = String(path === null || path === undefined ? "" : path).replace(/[\\/]+$/, "");
    if (!value) return "";
    var parts = value.split(/[\\/]/);
    var last = parts[parts.length - 1] || "";
    if (/\.(mkv|mp4|avi|m4v|ts|mov|wmv|flv|srt|nfo|jpg|jpeg|png|webp)$/i.test(last)) {
      return parts[parts.length - 2] || last;
    }
    return last;
  }
  function pathBaseName(path) {
    var value = String(path === null || path === undefined ? "" : path).replace(/[\\/]+$/, "");
    if (!value) return "";
    var parts = value.split(/[\\/]/);
    return parts[parts.length - 1] || value;
  }
  function sourceLabel(value) {
    var raw = String(value === null || value === undefined ? "" : value).trim();
    if (!raw) return "";
    var candidate = raw;
    try {
      var parsed = new URL(/^[a-z][a-z0-9+.-]*:\/\//i.test(raw) ? raw : "https://" + raw);
      if (parsed.hostname) {
        var labels = parsed.hostname.toLowerCase().replace(/^www\./, "").split(".").filter(Boolean);
        var compoundSuffix = labels.length >= 3 && /^(co|com|net|org)\.[a-z]{2}$/.test(labels.slice(-2).join("."));
        var providerIndex = labels.length - (compoundSuffix ? 3 : 2);
        if (providerIndex >= 0) candidate = labels[providerIndex];
      }
    } catch (_) { /* keep a non-URL source as-is */ }
    return candidate.replace(/[-_]+/g, " ").trim();
  }
  function compactDateTime(value) {
    var raw = String(value === null || value === undefined ? "" : value).trim();
    if (!raw) return "";
    // SQLite timestamps are UTC but have no timezone suffix. RFC3339 values
    // may contain nanoseconds, which Date.parse does not consistently accept.
    var normalized = raw.replace(/^(\d{4}-\d{2}-\d{2}) (\d{2}:\d{2}:\d{2})/, "$1T$2");
    normalized = normalized.replace(/\.(\d{3})\d+(?=(Z|[+-]\d{2}:?\d{2})$)/, ".$1");
    if (/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?$/.test(normalized)) normalized += "Z";
    var timestamp = Date.parse(normalized);
    if (isNaN(timestamp)) return raw;
    var date = new Date(timestamp);
    function pad(number) { return String(number).padStart(2, "0"); }
    return pad(date.getDate()) + "/" + pad(date.getMonth() + 1) + "/" + date.getFullYear() +
      " " + pad(date.getHours()) + ":" + pad(date.getMinutes());
  }
  function fmt(value, format) {    if (value === null || value === undefined || value === "") return "";
    switch (format) {
      case "bool": return value ? "Sì" : "No";
      case "bytes": return humanBytes(value);
      case "rate": return humanBytes(value) + "/s";
      case "percent": return Number(value).toFixed(1) + "%";
      case "source": return sourceLabel(value);
      case "datetime": return compactDateTime(value);
    }
    if (typeof value === "object") {
      return value.name || value.title || value.label || value.path || "—";
    }
    return String(value);
  }
  function renderTable(container) {
    var panel = container.closest(".panel");
    var tableTitleNode = panel && panel.querySelector(".panel-head h3");
    var tableTitle = tableTitleNode ? tableTitleNode.textContent : "Elenco";
    var table = panel.querySelector("table");
    var thead = panel.querySelector("[data-ui-head]");
    var tbody = panel.querySelector("[data-ui-body]");
    var count = panel.querySelector("[data-ui-count]");
    var searchInput = panel.querySelector("[data-ui-search]");
    var filterInput = panel.querySelector("[data-ui-filter]");
    var pagination = panel.querySelector("[data-ui-pagination]");
    var endpoint = container.getAttribute("data-endpoint");
    var itemsKey = container.getAttribute("data-items") || "items";
    var columns = JSON.parse(container.getAttribute("data-columns") || "[]");
    var actions = JSON.parse(container.getAttribute("data-actions") || "[]");
    var empty = container.getAttribute("data-empty") || "Nessun elemento.";
    var initial = container.getAttribute("data-initial") || "";
    var manualOnly = container.getAttribute("data-auto-load") === "false";
    var searchParam = container.getAttribute("data-search") || "";
    var pageSize = parseInt(container.getAttribute("data-page-size") || "0", 10) || 0;
    var page = 1;
    var fetching = false;
    var refetchPending = false;
    function renderPagination(current, pages, total) {
      if (!pagination || !pageSize) return;
      pagination.hidden = pages <= 1;
      if (pages <= 1) {
        pagination.innerHTML = "";
        return;
      }
      pagination.innerHTML = '<button class="btn sm" type="button" data-page-prev' + (current <= 1 ? " disabled" : "") + '>‹ Precedente</button>' +
        '<span class="page-label">Pagina ' + current + " di " + pages + " · " + total + " voci</span>" +
        '<button class="btn sm" type="button" data-page-next' + (current >= pages ? " disabled" : "") + '>Successiva ›</button>';
      var previous = pagination.querySelector("[data-page-prev]");
      var next = pagination.querySelector("[data-page-next]");
      if (previous) previous.addEventListener("click", function () { page = Math.max(1, current - 1); fetchAndRender(); });
      if (next) next.addEventListener("click", function () { page = Math.min(pages, current + 1); fetchAndRender(); });
    }
    function renderTableHead() {
      thead.innerHTML = "<tr>" + columns.map(function (column) {
        var headerAttrs = column.sortable ? ' scope="col" class="th-sort" data-sort="' + esc(column.key) + '" aria-sort="none"' : ' scope="col"';
        var headerContent = column.sortable
          ? '<button class="table-sort-button" type="button">' + esc(column.label) + "</button>"
          : esc(column.label);
        return "<th" + headerAttrs + ">" + headerContent + "</th>";
      }).join("") + (actions.length ? '<th scope="col">Azioni</th>' : "") + "</tr>";
      accessibleDataTable(table, tableTitle);
    }
    function fetchAndRender() {
      if (fetching) {
        refetchPending = true;
        return;
      }
      fetching = true;
      var url = endpoint;
      if (searchParam && searchInput && searchInput.value) {
        url += (url.indexOf("?") >= 0 ? "&" : "?") + searchParam + "=" + encodeURIComponent(searchInput.value);
      }
      if (pageSize) {
        url += (url.indexOf("?") >= 0 ? "&" : "?") + "page=" + page + "&limit=" + pageSize;
      }
      pollRequest(url).then(function (data) {
        var items = (itemsKey && data[itemsKey]) || (Array.isArray(data) ? data : []);
        if (!columns.length && items.length) {
          columns = Object.keys(items[0]).filter(function (key) {
            var value = items[0][key];
            return value === null || typeof value !== "object";
          }).slice(0, 8).map(function (key) { return { key: key, label: key }; });
        }
        var colspan = columns.length + (actions.length ? 1 : 0);
        renderTableHead();
        if (!items.length) {
          tbody.innerHTML = '<tr><td class="muted" colspan="' + colspan + '">' + esc(empty) + "</td></tr>";
        } else {
          tbody.innerHTML = items.map(function (row) {
            var cells = columns.map(function (column) {
              // Some APIs return scalar arrays (for example download tags)
              // rather than objects. An empty column key means "the item".
              var rawValue = column.key === "" ? row : row[column.key];
              var value;
              var sortValue;
              if (column.format === "episodes") {
                value = String(row.episodes_downloaded || 0) + "/" + String(row.episodes_total || 0);
                sortValue = String(Number(row.episodes_downloaded) || 0);
              } else if (column.format === "completion") {
                var totalEpisodes = Number(row.episodes_total) || 0;
                var doneEpisodes = Number(row.episodes_downloaded) || 0;
                var percent = totalEpisodes > 0 ? (doneEpisodes / totalEpisodes) * 100 : 0;
                value = percent.toFixed(0) + "%";
                sortValue = String(percent);
              } else if (column.format === "enabled") {
                value = row.enabled ? "attiva" : "in pausa";
                sortValue = row.enabled ? "1" : "0";
              } else {
                value = fmt(rawValue, column.format);
                sortValue = value;
              }
              var sortAttr = column.sortable ? ' data-value="' + esc(sortValue) + '"' : "";
              if (column.format === "nas_tag") {
                var hasNAS = row.processed_path && String(row.processed_path).trim() !== "";
                var tag = String(row.tag || "").trim();
                var html = "";
                if (hasNAS) html += '<span class="badge ok">NAS</span>';
                if (tag) html += (html ? " " : "") + '<span class="badge">' + esc(tag) + "</span>";
                return "<td" + sortAttr + ">" + (html || '<span class="muted">—</span>') + "</td>";
              }
              if (column.format === "series_status") {
                var totalEpisodes = Number(row.episodes_total) || 0;
                var doneEpisodes2 = Number(row.episodes_downloaded) || 0;
                var complete = totalEpisodes > 0 && doneEpisodes2 >= totalEpisodes;
                var rawStatus = String(row.tmdb_status || "").toLowerCase();
                var ended = rawStatus === "ended" || rawStatus === "canceled" || rawStatus === "cancelled";
                var enabled2 = row.enabled !== false;
                var statusHTML;
                if (ended && complete) statusHTML = '<span class="badge ok" title="Serie terminata e completa: tutti gli episodi disponibili sono archiviati">🏁 ✓✓</span>';
                else if (ended) statusHTML = '<span class="badge" title="Serie terminata: mancano ancora episodi">🏁 terminata</span>';
                else if (complete) statusHTML = '<span class="badge" title="Al passo: tutti gli episodi pubblicati finora sono archiviati, ma la serie non è terminata">✓ in pari</span>';
                else statusHTML = '<span class="badge ' + (enabled2 ? "ok" : "") + '">' + (enabled2 ? "attiva" : "in pausa") + "</span>";
                if ((ended || complete) && !enabled2) statusHTML += ' <span class="badge" title="Serie in pausa">in pausa</span>';
                return "<td" + sortAttr + ">" + statusHTML + "</td>";
              }
              if (column.format === "status_badge") {
                var good = row.ok !== false;
                return "<td" + sortAttr + '><span class="badge ' + (good ? "ok" : "err") + '">' + (good ? "ok" : "errore") + "</span></td>";
              }
              if (column.format === "weekly_status") {
                var sent = String(row.sent_at || "").trim() !== "";
                var hasLink = String(row.magnet || row.torrent_url || "").trim() !== "";
                var weeklyLabel = sent ? "inviato" : (hasLink ? "trovato" : "in attesa del link");
                return "<td" + sortAttr + '><span class="badge ' + (sent ? "ok" : (hasLink ? "" : "warn")) + '">' + esc(weeklyLabel) + "</span></td>";
              }
              if (column.format === "source_detail") {
                var detail = "";
                if (row.error) detail = String(row.error);
                else if (row.status !== undefined && row.status !== null) detail = "HTTP " + String(row.status);
                else if (row.results !== undefined && row.results !== null) detail = String(row.results) + " risultati";
                return "<td" + sortAttr + '><span class="cell-truncate" title="' + esc(detail) + '">' + esc(detail || "—") + "</span></td>";
              }
              if (column.format === "source") {
                return "<td" + sortAttr + '><span class="source-label" title="' + esc(String(rawValue || "")) + '">' + esc(value || "—") + "</span></td>";
              }
              if (column.format === "datetime") {
                return "<td" + sortAttr + '><span title="' + esc(String(rawValue || "")) + '">' + esc(value) + "</span></td>";
              }
              if (column.format === "truncate") {
                return "<td" + sortAttr + '><span class="cell-truncate" title="' + esc(value) + '">' + esc(value) + "</span></td>";
              }
              if (column.format === "folder") {
                var fullPath = String(rawValue || "");
                if (!fullPath) return "<td" + sortAttr + "></td>";
                return "<td" + sortAttr + '><span title="' + esc(fullPath) + '">' + esc(folderLabel(fullPath)) + "</span></td>";
              }
              if (column.format === "series_link") {
                return "<td" + sortAttr + '><a href="/?view=series&amp;series=' + encodeURIComponent(row[column.key]) + '" title="Apri il dettaglio della serie">' + esc(value) + "</a></td>";
              }
              if (column.format === "movie_link") {
                return "<td" + sortAttr + '><a href="/?view=movies&amp;movie=' + encodeURIComponent(row.id) + '" title="Apri il dettaglio del film">' + esc(value) + "</a></td>";
              }
              if (column.format === "url" || column.format === "getcomics") {
                if (!value) return "<td" + sortAttr + "></td>";
                var href = String(value);
                if (column.format === "getcomics" && href.charAt(0) === "/") {
                  href = "https://getcomics.org" + href;
                }
                href = safeHref(href);
                if (!href) return "<td" + sortAttr + "></td>";
                return "<td" + sortAttr + '><a href="' + href + '" target="_blank" rel="noopener">apri</a></td>';
              }
              return "<td" + sortAttr + ">" + esc(value) + "</td>";
            }).join("");
            var actionsHtml = "";
            if (actions.length) {
              actionsHtml = '<td class="row-actions">' + actions.map(function (action) {
                if (action.kind === "comic-edit") {
                  return '<button class="btn sm" data-comic-edit' +
                    ' data-comic-title="' + esc(row.title || "") + '"' +
                    ' data-comic-tag-url="' + esc(row.tag_url || "") + '"' +
                    ' data-comic-post-url="' + esc(row.post_url || "") + '"' +
                    ' data-comic-from-date="' + esc(row.from_date || "") + '"' +
                    ' data-comic-save-path="' + esc(row.save_path || "") + '">' +
                    esc(action.label || "Modifica") + "</button>";
                }
                if (action.kind === "release-explain") {
                  var explainRelease = row.release && typeof row.release === "object" ? row.release : {
                    title: row.title || "",
                    magnet: row.magnet || "",
                    source: row.source || ""
                  };
                  return '<button class="btn sm" data-release-explain="' + esc(JSON.stringify(explainRelease)) +
                    '" title="Mostra perché questa release viene accettata o scartata">' + esc(action.label || "Perché non questo?") + "</button>";
                }
                if (action.kind === "comic-weekly-force") {
                  var weeklyMagnet = String(row.magnet || "").trim();
                  var weeklyTorrent = String(row.torrent_url || "").trim();
                  var weeklyURL = weeklyMagnet || weeklyTorrent;
                  var weeklyMethod = weeklyMagnet ? "magnet" : "torrent";
                  var weeklyBody = JSON.stringify({
                    url: weeklyURL,
                    method: weeklyMethod,
                    title: "Weekly Pack " + String(row.pack_date || ""),
                    post_url: "",
                    save_path: ""
                  });
                  return '<button class="btn sm" data-api="/api/comics/download" data-method="POST" data-body="' +
                    esc(weeklyBody) + '"' + (weeklyURL ? "" : " disabled") +
                    ' title="Scarica di nuovo questo Weekly Pack">' + esc(action.label || "Forza") + "</button>";
                }
                if (action.kind === "gap-search") {
                  return '<button class="btn sm ' + (action.class || "") + '" data-gap-search data-series="' + esc(row.series) +
                    '" data-season="' + esc(row.season) + '" data-episode="' + esc(row.episode) + '">' + esc(action.label) + "</button>";
                }
                if (action.kind === "library-toggle" || action.kind === "library-remove") {
                  var isRemove = action.kind === "library-remove";
                  var scope = itemsKey === "series" ? "series" : "movies";
                  var buttonClass = action.class || (isRemove ? "danger" : "");
                  var buttonLabel = isRemove ? action.label : (row.enabled ? "Pausa" : "Attiva");
                  var confirmAttr = action.confirm ? ' data-confirm="' + esc(action.confirm) + '"' : "";
                  return '<button class="btn sm ' + buttonClass + '" data-library-' + (isRemove ? "remove" : "toggle") +
                    '="' + scope + '" data-library-name="' + esc(row.name) + '"' + confirmAttr + ">" + esc(buttonLabel) + "</button>";
                }
                var path = action.path.replace(/\{([a-z_]+)\}/g, function (_, key) {
                  return encodeURIComponent(row[key]);
                });
                var body = (action.body || "{}").replace(/\{([a-z_]+)\}/g, function (_, key) {
                  var encoded = JSON.stringify(row[key] == null ? "" : row[key]);
                  return encoded.slice(1, -1);
                });
                var confirmAttr = action.confirm ? ' data-confirm="' + esc(action.confirm) + '"' : "";
                return '<button class="btn sm ' + (action.class || "") + '" data-api="' + esc(path) +
                  '" data-method="' + esc(action.method || "POST") + '" data-body="' + esc(body) +
                  '"' + confirmAttr + ">" + esc(action.label) + "</button>";
              }).join(" ") + "</td>";
            }
            return "<tr>" + cells + actionsHtml + "</tr>";
          }).join("");
        }
        var total = pageSize && typeof data.total === "number" ? data.total : items.length;
        if (count) count.textContent = total + " voci";
        if (pageSize) renderPagination(Number(data.page) || page, Number(data.pages) || 1, total);
        applyTableFilter(panel);
      }).catch(function (error) {
        var message = error && error.name === "AbortError" ? "Verifica sorgenti scaduta: riprova con Aggiorna." : error.message;
        tbody.innerHTML = '<tr><td class="alert">' + esc(message) + "</td></tr>";
      }).then(function () {
        fetching = false;
        if (refetchPending && document.body.contains(container)) {
          refetchPending = false;
          window.setTimeout(fetchAndRender, 0);
        }
      });
    }
    container._refetch = fetchAndRender;
    var refresh = panel.querySelector("[data-ui-refresh]");
    if (refresh) refresh.addEventListener("click", fetchAndRender);
    if (searchInput) searchInput.addEventListener("keydown", function (event) {
      if (event.key === "Enter") { event.preventDefault(); page = 1; fetchAndRender(); }
    });
    if (filterInput) filterInput.addEventListener("input", function () { applyTableFilter(panel); });
    if (manualOnly) {
      renderTableHead();
      tbody.innerHTML = '<tr><td class="muted" colspan="' + (columns.length + (actions.length ? 1 : 0)) + '">' + esc(initial || "Premi Aggiorna per caricare i dati.") + "</td></tr>";
    } else {
      fetchAndRender();
    }
    if (endpoint === "/api/comics/downloads") {
      var pollTimer = window.setInterval(function () {
        // The page content can be replaced by navigation. Stop polling a
        // detached table so each visit does not add another timer.
        if (!document.body.contains(container)) {
          window.clearInterval(pollTimer);
          return;
        }
        fetchAndRender();
      }, 1500);
    }
  }
  Array.prototype.forEach.call(document.querySelectorAll("[data-ui-table]"), renderTable);

  function refreshComicsDownloadTables() {
    Array.prototype.forEach.call(document.querySelectorAll('[data-ui-table][data-endpoint="/api/comics/downloads"]'), function (table) {
      if (table._refetch) table._refetch();
    });
  }

  function applyTableFilter(panel) {
    var input = panel.querySelector("[data-ui-filter]");
    if (!input) return;
    var query = (input.value || "").trim().toLowerCase();
    Array.prototype.forEach.call(panel.querySelectorAll("[data-ui-body] tr"), function (row) {
      if (!query) { row.hidden = false; return; }
      row.hidden = (row.textContent || "").toLowerCase().indexOf(query) < 0;
    });
  }

  // ---- library toggle/remove (read-modify-write of /api/config/library) ---
  function libraryRead() {
    return api("/api/config/library", "GET").then(function (data) {
      return { series: data.series || [], movies: data.movies || [] };
    });
  }

  function librarySave(library) {
    return api("/api/config/library", "POST", { series: library.series, movies: library.movies });
  }

  function refetchTableFor(element) {
    var panel = element.closest(".panel");
    var container = panel && panel.querySelector("[data-ui-table]");
    if (container && container._refetch) { container._refetch(); return; }
    if (partials[view]) { load(); return; }
    location.reload();
  }

  function libraryToggle(scope, name) {
    libraryRead().then(function (library) {
      var list = library[scope] || [];
      var found = false;
      list.forEach(function (item) {
        if (item.name === name) { item.enabled = !item.enabled; found = true; }
      });
      if (!found) throw new Error("elemento non trovato");
      library[scope] = list;
      return librarySave(library);
    }).then(function () { notify("Stato aggiornato", "ok"); })
      .catch(function (error) { notify("Modifica non riuscita: " + error.message, "err"); });
  }

  function libraryRemove(scope, name, element) {
    libraryRead().then(function (library) {
      var list = (library[scope] || []).filter(function (item) { return item.name !== name; });
      if (list.length === (library[scope] || []).length) throw new Error("elemento non trovato");
      library[scope] = list;
      return librarySave(library);
    }).then(function () {
      notify("Elemento eliminato", "ok");
      if (element) refetchTableFor(element);
    }).catch(function (error) { notify("Eliminazione non riuscita: " + error.message, "err"); });
  }

  function toggleGenericSort(head) {
    var table = head.closest("table");
    var body = table && table.querySelector("tbody");
    if (!body) return;
    var index = Array.prototype.indexOf.call(head.parentNode.children, head);
    var key = head.getAttribute("data-sort") || String(index);
    if (genericSort.table === table && genericSort.key === key) {
      genericSort.direction = -genericSort.direction;
    } else {
      genericSort.table = table;
      genericSort.key = key;
      genericSort.direction = 1;
    }
    var rows = Array.prototype.slice.call(body.querySelectorAll("tr"));
    rows.sort(function (left, right) {
      var ca = left.children[index];
      var cb = right.children[index];
      var va = ca ? (ca.getAttribute("data-value") || ca.textContent || "") : "";
      var vb = cb ? (cb.getAttribute("data-value") || cb.textContent || "") : "";
      var na = parseFloat(va);
      var nb = parseFloat(vb);
      if (!isNaN(na) && !isNaN(nb)) return (na - nb) * genericSort.direction;
      return String(va).localeCompare(String(vb)) * genericSort.direction;
    });
    rows.forEach(function (row) { body.appendChild(row); });
    Array.prototype.forEach.call(table.querySelectorAll("th[data-sort]"), function (th) {
      if (th === head) {
        th.setAttribute("data-sort-dir", genericSort.direction === 1 ? "asc" : "desc");
        th.setAttribute("aria-sort", genericSort.direction === 1 ? "ascending" : "descending");
      } else {
        th.removeAttribute("data-sort-dir");
        th.setAttribute("aria-sort", "none");
      }
    });
  }
  var genericSort = { table: null, key: "", direction: 1 };

  // ---- movie detail edit form ---------------------------------------------
  Array.prototype.forEach.call(document.querySelectorAll("[data-movie-id]"), function (form) {
    form.addEventListener("submit", function (event) {
      event.preventDefault();
      var id = form.getAttribute("data-movie-id");
      var body = {};
      Array.prototype.forEach.call(form.querySelectorAll("input[name]"), function (input) {
        body[input.name] = input.value;
      });
      var message = form.querySelector("small");
      api("/api/movies/" + encodeURIComponent(id), "POST", body).then(function () {
        if (message) message.textContent = "Salvato";
      }).catch(function (error) {
        notify("Salvataggio non riuscito: " + error.message, "err");
      });
    });
  });

  // ---- i18n editor --------------------------------------------------------
  var i18nEditor = document.querySelector("[data-i18n-editor]");
  if (i18nEditor) {
    var langSelect = i18nEditor.querySelector("[data-i18n-language]");
    var i18nBody = i18nEditor.querySelector("[data-i18n-body]");
    var i18nExport = i18nEditor.querySelector("[data-i18n-export]");
    var i18nMessage = i18nEditor.querySelector("[data-i18n-message]");
    function loadI18n() {
      if (i18nExport) i18nExport.href = "/api/i18n/export/" + encodeURIComponent(langSelect.value);
      api("/api/i18n?lang=" + encodeURIComponent(langSelect.value), "GET").then(function (data) {
        var items = (data && data.items) || [];
        if (!items.length) {
          i18nBody.innerHTML = '<tr><td class="muted" colspan="3">Nessuna traduzione.</td></tr>';
          return;
        }
        i18nBody.innerHTML = items.map(function (item) {
          return '<tr><td class="truncate">' + esc(item.key) + "</td>" +
            '<td><input data-i18n-key="' + esc(item.key) + '" value="' + esc(item.value) + '" /></td>' +
            '<td><button class="btn sm" data-i18n-save="' + esc(item.key) + '">Salva</button></td></tr>';
        }).join("");
      }).catch(function (error) { if (i18nMessage) i18nMessage.textContent = error.message; });
    }
    api("/api/i18n/active", "GET").then(function (data) {
      if (data && data.lang) langSelect.value = data.lang;
      loadI18n();
    }).catch(function () { loadI18n(); });
    langSelect.addEventListener("change", function () {
      api("/api/i18n/language", "POST", { lang: langSelect.value }).then(function () { loadI18n(); })
        .catch(function (error) { notify("Cambio lingua non riuscito: " + error.message, "err"); });
    });
    i18nBody.addEventListener("click", function (event) {
      var button = event.target.closest("[data-i18n-save]");
      if (!button) return;
      var key = button.getAttribute("data-i18n-save");
      var input = i18nBody.querySelector('[data-i18n-key="' + key.replace(/(["\\])/g, "\\$1") + '"]');
      button.disabled = true;
      api("/api/i18n", "POST", { lang: langSelect.value, key: key, value: input ? input.value : "" })
        .then(function () { if (i18nMessage) i18nMessage.textContent = "Salvato"; })
        .catch(function (error) { notify("Salvataggio non riuscito: " + error.message, "err"); })
        .then(function () { button.disabled = false; });
    });
    var i18nImport = i18nEditor.querySelector("[data-i18n-import]");
    if (i18nImport) i18nImport.addEventListener("click", function () {
      var area = i18nEditor.querySelector("[data-i18n-yaml]");
      i18nImport.disabled = true;
      api("/api/i18n/import/" + encodeURIComponent(langSelect.value), "POST", { yaml: area ? area.value : "" })
        .then(function () { if (i18nMessage) i18nMessage.textContent = "Importato"; loadI18n(); })
        .catch(function (error) { notify("Import non riuscito: " + error.message, "err"); })
        .then(function () { i18nImport.disabled = false; });
    });
    var i18nDelete = i18nEditor.querySelector("[data-i18n-delete]");
    if (i18nDelete) i18nDelete.addEventListener("click", function () {
      if (!confirm("Eliminare tutte le traduzioni della lingua " + langSelect.value + "?")) return;
      api("/api/i18n/" + encodeURIComponent(langSelect.value), "DELETE", {}).then(function () { loadI18n(); })
        .catch(function (error) { notify("Eliminazione non riuscita: " + error.message, "err"); });
    });
  }

  // ---- comics: GetComics link finder --------------------------------------
  var comicsDownload = document.querySelector("[data-comics-download]");
  if (comicsDownload) {
    var comicsInput = comicsDownload.querySelector("[data-comics-post]");
    var comicsMessage = comicsDownload.querySelector("[data-comics-message]");
    var comicsResults = comicsDownload.querySelector("[data-comics-results]");
    var comicsLinks = comicsDownload.querySelector("[data-comics-links]");
    var comicGroups = [
      { key: "download_now", label: "Download diretto", method: "download_now" },
      { key: "direct", label: "File diretti", method: "direct" },
      { key: "torrents", label: "Torrent", method: "torrent" },
      { key: "magnets", label: "Magnet", method: "magnet" },
      { key: "mega", label: "Mega", method: "mega" }
    ];
    comicsLinks.addEventListener("click", function () {
      var postURL = comicsInput.value.trim();
      if (!postURL) { notify("Inserisci l'URL del post GetComics", "err"); return; }
      comicsLinks.disabled = true;
      if (comicsMessage) comicsMessage.textContent = "Ricerca in corso…";
      api("/api/comics/links", "POST", { url: postURL }).then(function (data) {
        var links = (data && data.links) || {};
        var html = comicGroups.map(function (group) {
          var values = links[group.key] || [];
          if (!values.length) return "";
          return '<div class="field span-full" style="margin-top:10px"><span>' + esc(group.label) + "</span></div>" +
            '<div class="toolbar">' + values.map(function (value, index) {
              var body = JSON.stringify({ url: value, method: group.method, title: "Comic " + group.label + " " + (index + 1), post_url: postURL });
              return '<button class="btn sm" data-api="/api/comics/download" data-method="POST" data-body="' + esc(body) + '" title="' + esc(value) + '">Scarica</button>';
            }).join(" ") + "</div>";
        }).join("");
        comicsResults.innerHTML = html || '<p class="muted">Nessun link trovato.</p>';
        if (comicsMessage) comicsMessage.textContent = "";
      }).catch(function (error) {
        if (comicsMessage) comicsMessage.textContent = error.message;
      }).then(function () { comicsLinks.disabled = false; });
    });
  }

  // ---- monitored comic editor ---------------------------------------------
  function openComicEditor(button) {
    var overlay = document.createElement("div");
    overlay.className = "overlay";
    overlay._removeOnClose = true;
    overlay.innerHTML = '<div class="modal" role="dialog" aria-modal="true" aria-labelledby="comic-edit-title">' +
      '<div class="modal-head"><h3 id="comic-edit-title">Modifica fumetto monitorato</h3><button class="btn sm" type="button" data-comic-edit-close>Chiudi</button></div>' +
      '<div class="modal-body"><form class="form-grid" data-comic-edit-form>' +
      '<label class="field"><span>Titolo</span><input class="input" name="title" readonly /></label>' +
      '<label class="field"><span>Data inizio</span><input class="input" type="date" name="from_date" /></label>' +
      '<label class="field span-full"><span>Percorso archivio</span><input class="input" name="save_path" aria-label="Percorso archivio" placeholder="cartella fumetti predefinita" /></label>' +
      '<div class="form-actions"><button class="btn primary" type="submit">Salva</button><small class="muted" data-comic-edit-message role="status" aria-live="polite" aria-atomic="true"></small></div>' +
      '</form></div></div>';
    document.body.appendChild(overlay);
    var form = overlay.querySelector("[data-comic-edit-form]");
    form.elements.title.value = button.getAttribute("data-comic-title") || "";
    form.elements.from_date.value = button.getAttribute("data-comic-from-date") || "";
    form.elements.save_path.value = button.getAttribute("data-comic-save-path") || "";
    openAccessibleDialog(overlay, button);
    function close() { closeAccessibleDialog(overlay, true); }
    overlay.addEventListener("click", function (event) {
      if (event.target === overlay || event.target.closest("[data-comic-edit-close]")) close();
    });
    form.addEventListener("submit", function (event) {
      event.preventDefault();
      var submit = form.querySelector("button[type=submit]");
      var message = form.querySelector("[data-comic-edit-message]");
      submit.disabled = true;
      api("/api/comics", "POST", {
        title: form.elements.title.value,
        tag_url: button.getAttribute("data-comic-tag-url") || "",
        post_url: button.getAttribute("data-comic-post-url") || "",
        from_date: form.elements.from_date.value,
        save_path: form.elements.save_path.value
      }).then(function () {
        close();
        notify("Fumetto aggiornato", "ok");
        var container = button.closest(".panel") && button.closest(".panel").querySelector("[data-ui-table]");
        if (container && container._refetch) container._refetch();
      }).catch(function (error) {
        message.textContent = error.message;
        submit.disabled = false;
      });
    });
  }

  document.addEventListener("click", function (event) {
    var button = event.target.closest("[data-comic-edit]");
    if (button) openComicEditor(button);
  });

  // ---- generic actions ----------------------------------------------------
  document.addEventListener("click", function (event) {
    var element = event.target.closest("[data-api]");
    if (!element) return;
    var confirmMessage = element.getAttribute("data-confirm");
    if (confirmMessage && !confirm(confirmMessage)) return;
    var body = {};
    try { body = JSON.parse(element.getAttribute("data-body") || "{}"); } catch (error) { body = {}; }
    element.disabled = true;
    api(element.getAttribute("data-api"), element.getAttribute("data-method") || "POST", body)
      .then(function (data) {
        refreshComicsDownloadTables();
        var container = element.closest(".panel") &&
          element.closest(".panel").querySelector("[data-ui-table]");
        if (container && container._refetch) { container._refetch(); return; }
        if (partials[view]) { load(); return; }
        var message = "Operazione completata";
        if (data && typeof data === "object" && data.message) message = String(data.message);
        notify(message, "ok");
        window.setTimeout(function () { location.reload(); }, 1400);
      })
      .catch(function (error) { notify("Azione non riuscita: " + error.message, "err"); })
      .then(function () { element.disabled = false; });
  });

  // ---- settings forms -----------------------------------------------------
  var backupFtpTest = document.querySelector("[data-backup-test-ftp]");
  if (backupFtpTest) {
    var backupFtpForm = backupFtpTest.closest("form");
    var backupFtpStatus = backupFtpForm && backupFtpForm.querySelector("[data-backup-ftp-status]");
    var backupFtpValue = function (name) {
      var input = backupFtpForm && backupFtpForm.querySelector('[name="' + name + '"]');
      return input ? String(input.value || "").trim() : "";
    };
    backupFtpTest.addEventListener("click", function () {
      backupFtpTest.disabled = true;
      if (backupFtpStatus) backupFtpStatus.textContent = "Test FTP in corso…";
      api("/api/backup/test-ftp", "POST", {
        host: backupFtpValue("backup_ftp_host"),
        user: backupFtpValue("backup_ftp_user"),
        password: backupFtpValue("backup_ftp_password"),
        path: backupFtpValue("backup_ftp_path")
      }).then(function (data) {
        var message = data && data.ok
          ? "FTP OK: connessione, login, trasferimento e rimozione completati"
          : "FTP non riuscito: " + String((data && data.error) || "verifica fallita");
        if (backupFtpStatus) backupFtpStatus.textContent = message;
        notify(message, data && data.ok ? "ok" : "err");
      }).catch(function (error) {
        if (backupFtpStatus) backupFtpStatus.textContent = "Test FTP non riuscito: " + error.message;
        notify("Test FTP non riuscito: " + error.message, "err");
      }).then(function () { backupFtpTest.disabled = false; });
    });
  }

  var dirtySettings = {};

  function settingValueOf(input) {
    if (!input) return "";
    if (input.getAttribute("data-tag-json") !== null) {
      var items = (input.value || "").split("\n").map(function (line) { return line.trim(); })
        .filter(function (line) { return line !== ""; });
      return input.getAttribute("data-tag-json") === "true" ? JSON.stringify(items) : items.join(", ");
    }
    return input.value;
  }

  function updateSettingsSavebar() {
    var bar = document.querySelector("[data-settings-savebar]");
    if (!bar) return;
    var keys = Object.keys(dirtySettings);
    if (!keys.length) { bar.hidden = true; return; }
    bar.hidden = false;
    var label = bar.querySelector("[data-settings-dirty-count]");
    if (label) label.textContent = keys.length === 1 ? "1 modifica non salvata" : keys.length + " modifiche non salvate";
  }

  function markSettingDirty(form, input) {
    var key = form.getAttribute("data-setting-key");
    if (!key) return;
    var original = form.getAttribute("data-original-value");
    if (original === null) {
      form.setAttribute("data-original-value", input.value);
      original = input.value;
    }
    var secret = input.type === "password";
    var current = input.value;
    if (secret && current === "") {
      delete dirtySettings[key];
    } else if (!secret && current === original) {
      delete dirtySettings[key];
    } else {
      dirtySettings[key] = { form: form, input: input };
    }
    updateSettingsSavebar();
  }

  document.addEventListener("input", function (event) {
    var input = event.target.closest("[data-setting-input]");
    if (!input) return;
    var form = input.closest("[data-setting-key]");
    if (form) markSettingDirty(form, input);
  });
  document.addEventListener("change", function (event) {
    var input = event.target.closest("[data-setting-input]");
    if (!input) return;
    var form = input.closest("[data-setting-key]");
    if (form) markSettingDirty(form, input);
  });

  function saveSettingForm(form, input) {
    var key = form.getAttribute("data-setting-key");
    var status = form.querySelector("[data-setting-status]");
    var secret = input.type === "password";
    var value = settingValueOf(input);
    if (secret && value === "") {
      if (status) status.textContent = "Inserisci un valore";
      return Promise.reject(new Error("inserisci un valore"));
    }
    return api("/api/config/settings", "POST", { key: key, value: value }).then(function () {
      if (status) status.textContent = "Salvato";
      if (secret) {
        input.value = "";
      } else {
        form.setAttribute("data-original-value", input.value);
      }
      delete dirtySettings[key];
      updateSettingsSavebar();
    });
  }

  Array.prototype.forEach.call(document.querySelectorAll("[data-setting-key]"), function (form) {
    var input = form.querySelector("[data-setting-input]");
    if (input) form.setAttribute("data-original-value", input.value);
    form.addEventListener("submit", function (event) {
      event.preventDefault();
      var currentInput = form.querySelector("[data-setting-input]");
      if (!currentInput) return; // structured value, edited from its dedicated section
      var button = form.querySelector("button[type=submit]");
      if (button) button.disabled = true;
      saveSettingForm(form, currentInput).then(function () {
        if (form.getAttribute("data-setting-key") === "libtorrent_auto_optimize") {
          // Re-render the tab so the managed queue/cache fields switch to Auto.
          location.reload();
          return;
        }
        if (!button) return;
        button.textContent = "Salvato";
        setTimeout(function () { button.textContent = "Salva"; button.disabled = false; }, 1200);
      }).catch(function (error) {
        if (button) button.disabled = false;
        var status = form.querySelector("[data-setting-status]");
        if (status) status.textContent = "Errore";
        if (error && error.message && error.message !== "inserisci un valore") {
          notify("Salvataggio non riuscito: " + error.message, "err");
        }
      });
    });
  });

  document.addEventListener("click", function (event) {
    var saveAll = event.target.closest("[data-settings-save-all]");
    if (saveAll) {
      var keys = Object.keys(dirtySettings);
      if (!keys.length) { updateSettingsSavebar(); return; }
      var reloadAfter = keys.indexOf("libtorrent_auto_optimize") !== -1;
      saveAll.disabled = true;
      Promise.all(keys.map(function (key) {
        var entry = dirtySettings[key];
        if (!entry) return Promise.resolve();
        return saveSettingForm(entry.form, entry.input);
      })).then(function () {
        notify("Impostazioni salvate", "ok");
      }).catch(function (error) {
        notify("Salvataggio non riuscito: " + error.message, "err");
      }).then(function () {
        saveAll.disabled = false;
        if (reloadAfter) location.reload();
      });
      return;
    }
    if (event.target.closest("[data-settings-discard]")) {
      Object.keys(dirtySettings).forEach(function (key) {
        var entry = dirtySettings[key];
        if (!entry) return;
        var original = entry.form.getAttribute("data-original-value");
        entry.input.value = original === null ? "" : original;
        var status = entry.form.querySelector("[data-setting-status]");
        if (status) status.textContent = "";
      });
      dirtySettings = {};
      updateSettingsSavebar();
    }
  });

  // Selecting the qBittorrent engine installs/refreshes the managed binary and
  // fills the optimal qBittorrent options, then reloads the tab.
  var backendSelect = document.querySelector("#input-torrent_backend");
  if (backendSelect) {
    // Sane starting values for the anacrolix engine (which has no managed
    // binary): applied automatically when it is selected.
    var ANACROLIX_SANE = {
      anacrolix_listen_port: "6881",
      anacrolix_tcp: "true",
      anacrolix_utp: "true",
      anacrolix_dht: "true",
      anacrolix_pex: "true",
      anacrolix_trackers: "true",
      anacrolix_upnp: "true",
      anacrolix_apply_ip_filter: "true",
      anacrolix_max_conns_per_torrent: "50",
      anacrolix_download_limit_kib: "0",
      anacrolix_upload_limit_kib: "0",
      anacrolix_piece_hashers: "2",
      anacrolix_max_unverified_mb: "64",
      anacrolix_proxy_type: "0",
      anacrolix_proxy_port: "0"
    };
    backendSelect.addEventListener("change", function () {
      var form = backendSelect.closest("[data-setting-key]");
      var status = form ? form.querySelector("[data-setting-status]") : null;
      if (backendSelect.value === "qbittorrent") {
        if (status) status.textContent = "Ottimizzazione qBittorrent…";
        notify("Ottimizzazione qBittorrent in corso… (download del binario gestito)", "info");
        api("/api/config/settings", "POST", { key: "torrent_backend", value: "qbittorrent" })
          .then(function () { return api("/api/torrent-backend/qbittorrent/update", "POST", {}); })
          .then(function (data) {
            var message = (data && data.message) || "qBittorrent scaricato e installato";
            if (status) status.textContent = message;
            notify(message, "ok");
            window.setTimeout(function () { location.reload(); }, 1500);
          })
          .catch(function (error) {
            if (status) status.textContent = "Errore";
            notify("Ottimizzazione qBittorrent non riuscita: " + error.message, "err");
          });
        return;
      }
      if (backendSelect.value === "anacrolix") {
        if (status) status.textContent = "Applico valori sani anacrolix…";
        notify("Applico valori sani ad anacrolix…", "info");
        var saves = [api("/api/config/settings", "POST", { key: "torrent_backend", value: "anacrolix" })];
        Object.keys(ANACROLIX_SANE).forEach(function (key) {
          saves.push(api("/api/config/settings", "POST", { key: key, value: ANACROLIX_SANE[key] }));
        });
        Promise.all(saves).then(function () {
          if (status) status.textContent = "Valori sani applicati";
          notify("anacrolix impostato con valori sani; riavvia per applicarlo", "ok");
          window.setTimeout(function () { location.reload(); }, 900);
        }).catch(function (error) {
          if (status) status.textContent = "Errore";
          notify("Configurazione anacrolix non riuscita: " + error.message, "err");
        });
      }
    });
  }

  // ---- checkbox groups (Motori web, Filtri contenuto) ---------------------
  Array.prototype.forEach.call(document.querySelectorAll("[data-checkbox-group]"), function (group) {
    var key = group.getAttribute("data-key");
    var status = group.querySelector("[data-checkbox-status]");
    var row = group.querySelector(".checkbox-row");
    function selected() {
      return Array.prototype.filter.call(group.querySelectorAll("[data-checkbox-option]"), function (box) {
        return box.checked;
      }).map(function (box) { return box.value; });
    }
    function save(message) {
      return api("/api/config/settings", "POST", { key: key, value: JSON.stringify(selected()) }).then(function () {
        if (status) status.textContent = message || "Salvato";
      }).catch(function (error) {
        if (status) status.textContent = "Errore";
        notify("Salvataggio non riuscito: " + error.message, "err");
      });
    }
    group.addEventListener("change", function (event) {
      if (!event.target.closest("[data-checkbox-option]")) return;
      save("Salvato");
    });
    var add = group.querySelector("[data-checkbox-add]");
    var custom = group.querySelector("[data-checkbox-custom]");
    if (add && custom) {
      add.addEventListener("click", function () {
        var value = (custom.value || "").trim();
        if (!value) return;
        var exists = Array.prototype.some.call(group.querySelectorAll("[data-checkbox-option]"), function (box) {
          return box.value.toLowerCase() === value.toLowerCase();
        });
        if (!exists && row) {
          var label = document.createElement("label");
          label.className = "check";
          label.title = value;
          var input = document.createElement("input");
          input.type = "checkbox";
          input.setAttribute("data-checkbox-option", "");
          input.value = value;
          input.checked = true;
          var span = document.createElement("span");
          span.textContent = value;
          label.appendChild(input);
          label.appendChild(document.createTextNode(" "));
          label.appendChild(span);
          row.appendChild(label);
        }
        custom.value = "";
        save("Filtro aggiunto");
      });
    }

    var testButton = group.querySelector("[data-checkbox-test]");
    if (testButton) {
      var queryInput = group.querySelector("[data-checkbox-query]");
      var testStatus = group.querySelector("[data-checkbox-test-status]");
      var testOutput = group.querySelector("[data-checkbox-test-output]");
      var testKind = group.getAttribute("data-test-kind") || "";
      var labelFor = {};
      Array.prototype.forEach.call(group.querySelectorAll("[data-checkbox-option]"), function (box) {
        var span = box.parentNode ? box.parentNode.querySelector("span") : null;
        if (span) labelFor[box.value] = span.textContent;
      });
      testButton.addEventListener("click", function () {
        var term = (queryInput && queryInput.value || "").trim();
        if (!term) { if (testStatus) testStatus.textContent = "Inserisci un termine"; return; }
        testButton.disabled = true;
        if (testStatus) testStatus.textContent = "Verifica in corso…";
        if (testOutput) testOutput.hidden = true;
        var healthUrl = "/api/sources/health?q=" + encodeURIComponent(term);
        if (testKind) healthUrl += "&kind=" + encodeURIComponent(testKind);
        api(healthUrl, "GET").then(function (data) {
          var items = (data && data.items) || [];
          if (testKind) items = items.filter(function (item) { return item.kind === testKind; });
          var ok = items.filter(function (item) { return item.ok; });
          var ko = items.filter(function (item) { return !item.ok; });
          var name = function (item) { return labelFor[item.name] || item.name; };
          var parts = [ok.length + "/" + items.length + " ok"];
          if (ok.length) parts.push(ok.map(name).join(", "));
          if (testStatus) testStatus.textContent = parts.join(": ");
          if (testOutput && ko.length) {
            testOutput.hidden = false;
            testOutput.textContent = ko.map(function (item) {
              return name(item) + ": " + (item.error || "nessun risultato");
            }).join("\n");
          }
        }).catch(function (error) {
          if (testStatus) testStatus.textContent = "Errore: " + error.message;
        }).then(function () { testButton.disabled = false; });
      });
    }
  });

  // ---- rename composition -------------------------------------------------
  var renameEditor = document.querySelector("[data-rename-editor]");
  if (renameEditor) {
    var renameFormat = renameEditor.querySelector("[data-rename-format]");
    var renameTemplate = renameEditor.querySelector("[data-rename-template]");
    var renamePreview = renameEditor.querySelector("[data-rename-preview]");
    var renameStatus = renameEditor.querySelector("[data-rename-status]");
    var renameSave = renameEditor.querySelector("[data-rename-save]");
    var renameSample = { series: "Nome Serie", title: "Titolo Episodio", res: "1080p", codec: "x265", audio: "DDP5.1", hdr: "HDR10", lang: "ita", channels: "5.1", source: "WEB-DL", group: "GRP" };
    var renameRender = function () {
      var format = renameFormat ? renameFormat.value : "base";
      var tpl = renameTemplate ? renameTemplate.value : "";
      var s = renameSample;
      var stem;
      if (format === "standard") {
        stem = s.series + " - S01E02 - " + s.title + " [" + s.res + "][" + s.codec + "]";
      } else if (format === "full" || format === "completo") {
        stem = s.series + " - S01E02 - " + s.title + " [" + s.res + "][" + s.audio + "][" + s.hdr + "][" + s.codec + "][" + s.lang + "]";
      } else if (format === "custom") {
        stem = tpl
          .split("{Serie}").join(s.series).split("{Stagione}").join("S01").split("{Episodio}").join("E02")
          .split("{Titolo}").join(s.title).split("{Source}").join(s.source).split("{Sorgente}").join(s.source)
          .split("{Gruppo}").join(s.group).split("{Risoluzione}").join(s.res).split("{VideoCodec}").join(s.codec)
          .split("{AudioCodec}").join(s.audio).split("{Audio}").join(s.audio).split("{Canali}").join(s.channels)
          .split("{HDR}").join(s.hdr).split("{Lingue}").join(s.lang);
      } else {
        stem = s.series + " - S01E02 - " + s.title;
      }
      if (renamePreview) renamePreview.textContent = stem + ".mkv";
    };
    if (renameFormat) renameFormat.addEventListener("change", renameRender);
    if (renameTemplate) renameTemplate.addEventListener("input", renameRender);
    Array.prototype.forEach.call(renameEditor.querySelectorAll("[data-rename-token]"), function (button) {
      button.addEventListener("click", function () {
        if (!renameTemplate) return;
        renameTemplate.value += button.getAttribute("data-rename-token");
        renameRender();
      });
    });
    if (renameSave) {
      renameSave.addEventListener("click", function () {
        renameSave.disabled = true;
        if (renameStatus) renameStatus.textContent = "Salvataggio…";
        api("/api/config/settings", "POST", { key: "rename_format", value: renameFormat ? renameFormat.value : "base" })
          .then(function () { return api("/api/config/settings", "POST", { key: "rename_template", value: renameTemplate ? renameTemplate.value : "" }); })
          .then(function () { if (renameStatus) renameStatus.textContent = "Rinomina salvata"; })
          .catch(function (error) {
            if (renameStatus) renameStatus.textContent = "Errore";
            notify("Salvataggio rinomina non riuscito: " + error.message, "err");
          })
          .then(function () { renameSave.disabled = false; });
      });
    }
    renameRender();
  }

  // ---- explore (search) ---------------------------------------------------
  function renderSearch(form) {
    var panel = form.closest(".panel");
    var thead = panel.querySelector("[data-ui-head]");
    var tbody = panel.querySelector("[data-ui-body]");
    var count = panel.querySelector("[data-ui-count]");
    var status = panel.querySelector("[data-release-status]");
    var filterInput = panel.querySelector("[data-release-filter]");
    var input = form.querySelector("input");
    var endpoint = form.getAttribute("data-endpoint");
    var resultsKey = form.getAttribute("data-results") || "results";
    var addPath = form.getAttribute("data-add");
    var searchToken = 0;
    var archiveEndpoint = form.getAttribute("data-archive") || "/api/search/archive";
    thead.innerHTML = "<tr><th scope=\"col\" class=\"th-sort\" data-release-sort=\"title\" aria-sort=\"none\" title=\"Nome del file — clicca per ordinare\"><button class=\"table-sort-button\" type=\"button\">Release</button></th><th scope=\"col\" title=\"Sorgente/indexer\">Sorgente</th><th scope=\"col\" class=\"th-sort\" data-release-sort=\"score\" aria-sort=\"none\" title=\"Punteggio di qualità — clicca per ordinare\"><button class=\"table-sort-button\" type=\"button\">Punteggio</button></th><th scope=\"col\" title=\"Azioni\">Azioni</th></tr>";
    var table = thead.closest("table");
    if (table) table.classList.add("release-table");
    if (filterInput) {
      filterInput.addEventListener("input", function () {
        renderReleaseTable({ container: tbody, filterInput: filterInput, countNode: count, items: form._items || [], addPath: addPath });
      });
    }
    var renderItems = function (items) {
      form._items = items;
      renderReleaseTable({ container: tbody, filterInput: filterInput, countNode: count, items: items, addPath: addPath });
    };
    form.addEventListener("submit", function (event) {
      event.preventDefault();
      var query = (input.value || "").trim();
      if (!query) return;
      var token = ++searchToken;
      tbody.innerHTML = '<tr><td class="muted" colspan="4">Ricerca nell\'archivio…</td></tr>';
      if (count) count.textContent = "…";
      // Phase 1: the local archive answers immediately.
      api(archiveEndpoint, "POST", { query: query }).then(function (data) {
        if (token !== searchToken) return;
        var archiveItems = (data && data.results) || [];
        renderItems(archiveItems);
        if (status) status.textContent = "Archivio: " + archiveItems.length + " · ricerca su indexer e motori web…";
      }).catch(function () { /* the full search reports errors */ });
      // Phase 2: indexer + web engines (may take up to 90 s); the archive
      // results are already visible and are merged when the full set arrives.
      api(endpoint, "POST", { query: query }).then(function (data) {
        if (token !== searchToken) return;
        var items = (data && data[resultsKey]) || [];
        renderItems(items);
        if (status) status.textContent = "";
      }).catch(function (error) {
        if (token !== searchToken) return;
        tbody.innerHTML = '<tr><td class="alert" colspan="4">' + esc(error.message) + "</td></tr>";
        if (status) status.textContent = "";
      });
    });
  }
  Array.prototype.forEach.call(document.querySelectorAll("[data-ui-search-post]"), renderSearch);

  // ---- shell: navigation, theme, font scale, language, live metrics --------
  document.addEventListener("click", function (event) {
    var nav = event.target.closest("[data-nav]");
    if (nav) {
      location.href = "/?view=" + encodeURIComponent(nav.getAttribute("data-nav"));
      return;
    }
    var navMore = event.target.closest("[data-nav-more]");
    if (navMore) {
      var group = navMore.closest(".nav-group");
      if (group) {
        group.classList.toggle("open");
        var icon = navMore.querySelector("[data-nav-more-icon]");
        var open = group.classList.contains("open");
        if (icon) icon.textContent = open ? "▲" : "▾";
        navMore.setAttribute("aria-expanded", open ? "true" : "false");
      }
      return;
    }
    var navSearch = event.target.closest("[data-nav-search]");
    if (navSearch) {
      location.href = "/?view=settings";
      return;
    }
    var font = event.target.closest("[data-font]");
    if (font) {
      setFontScale(readFontScale() + parseInt(font.getAttribute("data-font"), 10));
      return;
    }
    var theme = event.target.closest("[data-theme-toggle]");
    if (theme) {
      setTheme(document.documentElement.getAttribute("data-theme") === "light" ? "dark" : "light");
      return;
    }
  });

  // On a phone the navigation is a horizontal scroller (and on very small
  // screens it is docked at the bottom). Keep the current destination visible
  // after a page navigation instead of making the user hunt for it.
  (function revealActiveNavigation() {
    var active = document.querySelector(".sidebar .nav-item.active");
    if (!active || !window.matchMedia || window.matchMedia("(min-width: 900px)").matches) return;
    window.requestAnimationFrame(function () {
      active.scrollIntoView({ block: "nearest", inline: "center" });
    });
  }());

  // Cycle buttons with a domain (Dashboard quick actions).
  document.addEventListener("click", function (event) {
    var button = event.target.closest("[data-cycle]");
    if (!button) return;
    var domain = button.getAttribute("data-cycle") || "full";
    var path = domain === "full" ? "/api/run_now" : "/api/run_now?domain=" + encodeURIComponent(domain);
    button.disabled = true;
    api(path, "POST", {}).then(function () {
      button.textContent = "Avviato";
    }).catch(function (error) {
      notify("Ciclo non avviato: " + error.message, "err");
    }).then(function () { button.disabled = false; });
  });

  function storageGet(key) {
    try { return localStorage.getItem(key); } catch (error) { return null; }
  }
  function storageSet(key, value) {
    try { localStorage.setItem(key, value); } catch (error) { /* ignore */ }
  }
  function readFontScale() {
    var value = parseInt(storageGet("gextto_font_scale") || "100", 10);
    if (!isFinite(value)) value = 100;
    return Math.min(140, Math.max(85, value));
  }
  function setFontScale(percent) {
    percent = Math.min(140, Math.max(85, percent));
    // The root font size drives every rem-based size. Base 17px (not 16) so
    // the whole interface reads one step larger by default; the stored scale
    // then multiplies it.
    document.documentElement.style.fontSize = (17 * percent / 100) + "px";
    var label = document.querySelector("[data-font-label]");
    if (label) label.textContent = "Testo " + percent + "%";
    storageSet("gextto_font_scale", String(percent));
  }
  function setTheme(mode) {
    document.documentElement.setAttribute("data-theme", mode);
    var button = document.querySelector("[data-theme-toggle]");
    if (button) button.textContent = mode === "light" ? "Tema scuro" : "Tema chiaro";
    storageSet("gextto_theme", mode);
  }
  setFontScale(readFontScale());
  setTheme(storageGet("gextto_theme") === "light" ? "light" : "dark");
  var langSelect = document.querySelector("[data-lang]");
  if (langSelect) langSelect.addEventListener("change", function () {
    api("/api/i18n/active", "POST", { lang: langSelect.value })
      .then(function () { location.reload(); })
      .catch(function (error) { notify("Cambio lingua non riuscito: " + error.message, "err"); });
  });

  function humanRate(value) {
    var n = Number(value);
    if (!isFinite(n) || n <= 0) return "0 B/s";
    return humanBytes(n) + "/s";
  }
  function humanDuration(seconds) {
    var total = Math.max(0, Math.floor(Number(seconds) || 0));
    var hours = Math.floor(total / 3600);
    var minutes = Math.floor((total % 3600) / 60);
    var secs = total % 60;
    if (hours > 0) return hours + "h " + minutes + "m";
    if (minutes > 0) return minutes + "m " + secs + "s";
    return secs + "s";
  }
  function setMetric(name, value) {
    var node = document.querySelector('[data-metric="' + name + '"]');
    if (node) node.textContent = value;
  }
  var shellTorrentDownload = 0;
  var shellHTTPDownload = 0;
  function refreshShellDownloadMetric() {
    setMetric("dl", humanRate(shellTorrentDownload + shellHTTPDownload));
  }
  function setNavDownloadCount(count) {
    var node = document.querySelector('[data-nav="downloads"] [data-nav-count]');
    if (!node) return;
    node.textContent = String(count);
    node.hidden = count <= 0;
  }
  var shellMetricsInFlight = false;
  function refreshShellMetrics() {
    if (shellMetricsInFlight) return;
    shellMetricsInFlight = true;
    pollRequest("/api/process-metrics").then(function (data) {
      var cpu = data && data.process_cpu_percent;
      setMetric("cpu", cpu == null ? "—" : Number(cpu).toFixed(1) + "%");
      if (data && data.resident_bytes != null) setMetric("ram", humanBytes(data.resident_bytes));
    }).catch(function () { /* keep the previous value */ });
    pollRequest("/api/torrents").then(function (items) {
      var list = Array.isArray(items) ? items : [];
      var dl = 0, ul = 0, peers = 0, seeds = 0;
      list.forEach(function (item) {
        dl += Number(item.download_rate) || 0;
        ul += Number(item.upload_rate) || 0;
        peers += Number(item.num_peers) || 0;
        seeds += Number(item.num_seeds) || 0;
      });
      shellTorrentDownload = dl;
      refreshShellDownloadMetric();
      setMetric("ul", humanRate(ul));
      setMetric("count", String(list.length));
      setMetric("peers", peers + "/" + seeds);
      pollRequest("/api/comics/downloads").then(function (downloads) {
        var items = Array.isArray(downloads) ? downloads : [];
        shellHTTPDownload = items.reduce(function (total, item) {
          return total + (Number(item.speed_bytes) || 0);
        }, 0);
        var activeHTTP = items.filter(function (item) { return item.status === "downloading"; }).length;
        setNavDownloadCount(list.length + activeHTTP);
        refreshShellDownloadMetric();
      }).catch(function () { refreshShellDownloadMetric(); });
    }).catch(function () { /* keep the previous value */ });
    pollRequest("/api/status").then(function (data) {
      if (!data || !data.next_cycle_at) { setMetric("cycle", "—"); return; }
      var remaining = (Date.parse(data.next_cycle_at) - Date.now()) / 1000;
      setMetric("cycle", humanDuration(remaining));
    }).catch(function () { /* keep the previous value */ }).then(function () {
      shellMetricsInFlight = false;
    });
  }
  refreshShellMetrics();
  setInterval(function () {
    if (document.visibilityState === "visible") refreshShellMetrics();
  }, 4000);

  // ---- sources editor (feeds) ---------------------------------------------
  // Feeds live in the `url` setting and are edited as one URL per line.
  var sourcesEditor = document.querySelector("[data-sources-editor]");
  if (sourcesEditor) {
    var feedsInput = sourcesEditor.querySelector("[data-sources-feeds]");
    var sourcesMessage = sourcesEditor.querySelector("[data-sources-message]");
    api("/api/config", "GET").then(function (config) {
      config = config || {};
      if (feedsInput) feedsInput.value = (config.feed_urls || []).join("\n");
    }).catch(function (error) { if (sourcesMessage) sourcesMessage.textContent = error.message; });
    var saveSources = sourcesEditor.querySelector("[data-sources-save]");
    if (saveSources) saveSources.addEventListener("click", function () {
      var feeds = ((feedsInput && feedsInput.value) || "").split("\n").map(function (line) {
        return line.trim();
      }).filter(function (line) { return line !== ""; });
      saveSources.disabled = true;
      api("/api/config/settings", "POST", { key: "url", value: JSON.stringify(feeds) }).then(function () {
        if (sourcesMessage) sourcesMessage.textContent = "Feed salvati";
      }).catch(function (error) {
        notify("Salvataggio non riuscito: " + error.message, "err");
      }).then(function () { saveSources.disabled = false; });
    });
  }

  // ---- structured list editors --------------------------------------------
  // Real form rows, never raw JSON: indexers, tag-to-folder rules, event hooks,
  // watched folders and source filters.
  function listFieldValue(field) {
    var kind = field.getAttribute("data-kind");
    if (kind === "bool") return field.value === "true";
    if (kind === "number") return Number(field.value || 0);
    if (kind === "select") return field.value;
    if (kind === "tags") {
      return (field.value || "").split(",").map(function (part) { return part.trim(); })
        .filter(function (part) { return part !== ""; });
    }
    return field.value;
  }
  function fillListRow(row, item) {
    Array.prototype.forEach.call(row.querySelectorAll("[data-field]"), function (field) {
      var name = field.getAttribute("data-field");
      var value = item ? item[name] : undefined;
      var kind = field.getAttribute("data-kind");
      if (kind === "bool") {
        field.value = (value === false || value === "false") ? "false" : "true";
      } else if (kind === "select") {
        field.value = (value === undefined || value === null) ? "" : String(value);
      } else if (kind === "tags") {
        field.value = Array.isArray(value) ? value.join(", ") : (value || "");
      } else {
        field.value = (value === undefined || value === null) ? "" : value;
      }
    });
  }
  Array.prototype.forEach.call(document.querySelectorAll("[data-list-editor]"), function (editor) {
    var rowsBox = editor.querySelector("[data-list-rows]");
    var template = editor.querySelector("[data-list-row]");
    var message = editor.querySelector("[data-list-message]");
    var unwrap = editor.getAttribute("data-unwrap") || "";
    var wrap = editor.getAttribute("data-wrap") || "";
    var postKey = editor.getAttribute("data-post-key") || "";
    var testEndpoint = editor.getAttribute("data-test-endpoint") || "";
    function rowValues(row) {
      var item = {};
      Array.prototype.forEach.call(row.querySelectorAll("[data-field]"), function (field) {
        item[field.getAttribute("data-field")] = listFieldValue(field);
      });
      return item;
    }
    function addRow(item) {
      var clone = template.content.firstElementChild.cloneNode(true);
      fillListRow(clone, item || {});
      var remove = clone.querySelector("[data-list-remove]");
      if (remove) remove.addEventListener("click", function () { clone.remove(); });
      var testButton = clone.querySelector("[data-list-test]");
      if (testButton && testEndpoint) {
        testButton.addEventListener("click", function () {
          var result = clone.querySelector("[data-list-test-result]");
          testButton.disabled = true;
          if (result) result.textContent = "Test in corso…";
          api(testEndpoint, "POST", rowValues(clone)).then(function (data) {
            var ok = !!(data && data.ok === true);
            var detail = ok ? "OK" : ("Errore: " + String((data && data.error) || "non riuscito"));
            if (result) result.textContent = detail;
            notify(ok ? "Indexer OK" : ("Test indexer: " + detail), ok ? "ok" : "err");
          }).catch(function (error) {
            if (result) result.textContent = "Errore: " + error.message;
            notify("Test indexer: " + error.message, "err");
          }).then(function () { testButton.disabled = false; });
        });
      }
      rowsBox.appendChild(clone);
      return clone;
    }
    api(editor.getAttribute("data-get"), "GET").then(function (data) {
      var payload = (unwrap && data && data[unwrap] !== undefined) ? data[unwrap] : data;
      var items = Array.isArray(payload) ? payload : [];
      if (!items.length) { addRow({}); return; }
      items.forEach(function (item) { addRow(item); });
    }).catch(function (error) {
      if (message) message.textContent = error.message;
      addRow({});
    });
    var addButton = editor.querySelector("[data-list-add]");
    if (addButton) addButton.addEventListener("click", function () { addRow({}); });
    var saveButton = editor.querySelector("[data-list-save]");
    if (saveButton) saveButton.addEventListener("click", function () {
      var items = Array.prototype.map.call(rowsBox.children, function (row) {
        var item = {};
        Array.prototype.forEach.call(row.querySelectorAll("[data-field]"), function (field) {
          item[field.getAttribute("data-field")] = listFieldValue(field);
        });
        return item;
      });
      var encoded = JSON.stringify(items);
      var request;
      if (postKey) {
        request = api(editor.getAttribute("data-post"), "POST", { key: postKey, value: encoded });
      } else if (wrap) {
        var body = {};
        body[wrap] = items;
        request = api(editor.getAttribute("data-post"), "POST", body);
      } else {
        request = api(editor.getAttribute("data-post"), "POST", items);
      }
      saveButton.disabled = true;
      request.then(function () {
        if (message) message.textContent = "Salvato";
      }).catch(function (error) {
        notify("Salvataggio non riuscito: " + error.message, "err");
      }).then(function () { saveButton.disabled = false; });
    });
  });

  // ---- series edit form (structured fields, no JSON) ----------------------
  Array.prototype.forEach.call(document.querySelectorAll("[data-series-edit]"), function (form) {
    var name = form.getAttribute("data-series-name");
    var message = form.querySelector("[data-form-message]");
    form.addEventListener("submit", function (event) {
      event.preventDefault();
      api("/api/config/library", "GET").then(function (library) {
        var series = (library && library.series) || [];
        var found = false;
        series = series.map(function (item) {
          if (item.name !== name) return item;
          found = true;
          item.seasons = form.querySelector("[name=seasons]").value;
          item.quality = form.querySelector("[name=quality]").value;
          item.language = form.querySelector("[name=language]").value;
          item.archive_path = form.querySelector("[name=archive_path]").value;
          item.exclude = form.querySelector("[name=exclude]").value;
          var subtitle = form.querySelector("[name=subtitle]");
          if (subtitle) item.subtitle = subtitle.value;
          var tvdb = form.querySelector("[name=tvdb_id]");
          if (tvdb) item.tvdb_id = tvdb.value;
          var aliases = form.querySelector("[name=aliases]");
          if (aliases) {
            item.aliases = aliases.value.split(",").map(function (part) { return part.trim(); }).filter(Boolean);
          }
          return item;
        });
        if (!found) { notify("Serie non trovata", "err"); return; }
        return api("/api/config/series", "POST", series).then(function () {
          if (message) message.textContent = "Salvata";
        });
      }).catch(function (error) {
        notify("Salvataggio non riuscito: " + error.message, "err");
      });
    });
  });

  // ---- settings search ----------------------------------------------------
  var settingsView = document.querySelector("[data-settings-index]");
  if (settingsView) {
    var settingsIndex = [];
    try { settingsIndex = JSON.parse(settingsView.getAttribute("data-settings-index") || "[]"); }
    catch (error) { settingsIndex = []; }
    var searchInput = settingsView.querySelector("[data-settings-search]");
    var tabSelect = settingsView.querySelector("[data-settings-tab-select]");
    var resultsBox = settingsView.querySelector("[data-settings-results]");
    var settingsBody = settingsView.querySelector("[data-settings-body]");
    var settingsByKey = Object.create(null);
    settingsIndex.forEach(function (entry) {
      if (entry && entry.key) settingsByKey[String(entry.key).toLowerCase()] = entry;
    });
    function settingsSearchText(key, label) {
      var entry = settingsByKey[String(key || "").toLowerCase()];
      return [key, label, entry && entry.terms].filter(Boolean).join(" ").toLowerCase();
    }
    function settingsMatches(entry, query) {
      var text = settingsSearchText(entry && entry.key, entry && entry.label);
      return query.split(/\s+/).filter(Boolean).every(function (part) {
        return text.indexOf(part) >= 0;
      });
    }
    if (tabSelect) tabSelect.addEventListener("change", function () {
      location.href = "/?view=settings&tab=" + encodeURIComponent(tabSelect.value);
    });
    if (searchInput) searchInput.addEventListener("input", function () {
      var query = (searchInput.value || "").trim().toLowerCase();
      var cards = settingsBody ? settingsBody.querySelectorAll("[data-setting-key]") : [];
      Array.prototype.forEach.call(cards, function (card) {
        var key = (card.getAttribute("data-setting-key") || "").toLowerCase();
        var labelNode = card.querySelector(".setting-label");
        var label = (labelNode && labelNode.textContent || "").toLowerCase();
        var match = query === "" || settingsMatches({ key: key, label: label }, query);
        card.style.display = match ? "" : "none";
      });
      if (!resultsBox) return;
      if (query.length < 2) {
        resultsBox.hidden = true;
        resultsBox.innerHTML = "";
        return;
      }
      var matches = settingsIndex.filter(function (entry) {
        return settingsMatches(entry, query);
      }).slice(0, 30);
      resultsBox.hidden = false;
      if (!matches.length) {
        resultsBox.innerHTML = '<p class="muted">Nessuna impostazione trovata.</p>';
        return;
      }
      resultsBox.innerHTML = matches.map(function (entry) {
        var href = "/?view=settings&amp;tab=" + encodeURIComponent(entry.tab);
        if (entry.key) href += "#setting-" + encodeURIComponent(entry.key);
        return '<a class="settings-result" href="' + href + '">' + esc(entry.label) +
          (entry.key ? " <code>" + esc(entry.key) + "</code>" : "") + "</a>";
      }).join("");
    });
    if (location.hash && location.hash.indexOf("#setting-") === 0) {
      var target = document.getElementById(location.hash.slice(1));
      if (target) {
        var field = target.querySelector("[data-setting-input]");
        if (field) field.focus();
      }
    }
  }

  // ---- readable rendering (never JSON) ------------------------------------
  function renderReadable(container, value) {
    container.innerHTML = "";
    function makeNode(item) {
      if (item === null || item === undefined) return document.createTextNode("—");
      if (typeof item === "boolean") return document.createTextNode(item ? "Sì" : "No");
      if (typeof item === "number") return document.createTextNode(String(item));
      if (typeof item === "string") {
        if (/^https?:\/\//i.test(item)) {
          var link = document.createElement("a");
          link.href = item;
          link.target = "_blank";
          link.rel = "noopener";
          link.textContent = item;
          return link;
        }
        return document.createTextNode(item);
      }
      if (Array.isArray(item)) {
        var list = document.createElement("ul");
        list.className = "kv-list";
        item.forEach(function (entry) {
          var li = document.createElement("li");
          li.appendChild(makeNode(entry));
          list.appendChild(li);
        });
        return list;
      }
      var dl = document.createElement("dl");
      dl.className = "kv";
      Object.keys(item).forEach(function (key) {
        var dt = document.createElement("dt");
        dt.textContent = key;
        var dd = document.createElement("dd");
        dd.appendChild(makeNode(item[key]));
        dl.appendChild(dt);
        dl.appendChild(dd);
      });
      return dl;
    }
    container.appendChild(makeNode(value));
  }

  function renderCalendarList(container, items, limit) {
    var max = typeof limit === "number" ? limit : 6;
    container.innerHTML = "";
    if (!items.length) {
      container.innerHTML = '<p class="muted">Nessuna uscita in programma.</p>';
      return;
    }
    items.slice(0, max).forEach(function (item) {
      var episode = item && item.episode || {};
      var row = document.createElement("div");
      row.className = "list-item";
      var poster = document.createElement("div");
      poster.className = "list-poster placeholder";
      poster.textContent = "N/D";
      var posterURL = item && typeof item.poster === "string" ? item.poster : "";
      if (/^https?:\/\//i.test(posterURL)) {
        var image = document.createElement("img");
        image.className = "list-poster";
        image.src = posterURL;
        image.alt = String(item.series || "Serie");
        image.loading = "lazy";
        poster = image;
      }
      var text = document.createElement("div");
      var title = document.createElement("strong");
      title.textContent = String(item.series || "Serie");
      var detail = document.createElement("small");
      detail.textContent = "S" + String(episode.season_number || "—") +
        "E" + String(episode.episode_number || "—") + " · " + String(episode.air_date || "—");
      text.appendChild(title);
      text.appendChild(detail);
      row.appendChild(poster);
      row.appendChild(text);
      container.appendChild(row);
    });
  }

  var calendarList = document.querySelector("[data-dashboard-calendar-list]");
  if (calendarList) {
    api("/api/calendar", "GET").then(function (data) {
      renderCalendarList(calendarList, data && Array.isArray(data.items) ? data.items : []);
    }).catch(function (error) {
      calendarList.innerHTML = '<p class="muted">' + esc(error.message) + "</p>";
    });
  }

  var discoverCalendar = document.querySelector("[data-discover-calendar]");
  if (discoverCalendar) {
    api("/api/calendar", "GET").then(function (data) {
      renderCalendarList(discoverCalendar, data && Array.isArray(data.items) ? data.items : [], 24);
    }).catch(function (error) {
      discoverCalendar.innerHTML = '<p class="muted">' + esc(error.message) + "</p>";
    });
  }

  // Esplora: TMDB discovery with a kind toggle and the trending/category modes.
  var discoverPanel = document.querySelector("[data-discover]");
  if (discoverPanel) {
    var discoverState = { kind: "series", mode: "trending", window: "week" };
    var discoverResults = discoverPanel.querySelector("[data-discover-results]");
    var discoverStatus = discoverPanel.querySelector("[data-discover-status]");
    var runDiscover = function () {
      discoverResults.innerHTML = '<p class="muted">Caricamento…</p>';
      if (discoverStatus) discoverStatus.textContent = "";
      api("/api/tmdb/discover", "POST", { kind: discoverState.kind, mode: discoverState.mode, window: discoverState.window })
        .then(function (data) {
          var items = data.items || [];
          if (discoverStatus) discoverStatus.textContent = items.length + " risultati";
          renderDiscoverResults(discoverResults, items, discoverState.kind);
        })
        .catch(function (error) {
          discoverResults.innerHTML = '<p class="alert">' + esc(error.message) + "</p>";
        });
    };
    Array.prototype.forEach.call(discoverPanel.querySelectorAll("[data-discover-kind]"), function (button) {
      button.addEventListener("click", function () {
        discoverState.kind = button.getAttribute("data-discover-kind");
        Array.prototype.forEach.call(discoverPanel.querySelectorAll("[data-discover-kind]"), function (other) {
          other.classList.toggle("primary", other === button);
        });
        runDiscover();
      });
    });
    Array.prototype.forEach.call(discoverPanel.querySelectorAll("[data-discover-mode]"), function (button) {
      button.addEventListener("click", function () {
        discoverState.mode = button.getAttribute("data-discover-mode");
        discoverState.window = button.getAttribute("data-discover-window") || "week";
        runDiscover();
      });
    });
    runDiscover();
  }

  function tmdbPosterURL(item) {
    if (item.poster && /^https?:\/\//i.test(item.poster)) return item.poster;
    if (item.poster_path) return "https://image.tmdb.org/t/p/w200" + item.poster_path;
    return "";
  }

  // Shared rextto-style TMDB cards: poster, title, id/year/vote, overview and
  // an "Aggiungi alla libreria" button that opens the completion modal.
  function renderTmdbCards(container, items, kind) {
    container.innerHTML = "";
    if (!items.length) {
      container.innerHTML = '<p class="muted">Nessun risultato su TMDB.</p>';
      return;
    }
    var grid = document.createElement("div");
    grid.className = "tmdb-grid";
    items.forEach(function (item) {
      var isTVDB = String(item.external || "tmdb") === "tvdb";
      var title = String(item.name || item.title || "—");
      var year = String(item.first_air_date || item.release_date || "").slice(0, 4);
      var id = isTVDB ? String(item.tvdb_id || "") : String(item.id || item.tmdb_id || "");
      var poster = tmdbPosterURL(item);

      var card = document.createElement("div");
      card.className = "tmdb-card";

      var posterBox = document.createElement("div");
      posterBox.className = "tmdb-poster";
      if (poster && safeHref(poster)) {
        var image = document.createElement("img");
        image.src = safeHref(poster);
        image.alt = title;
        image.loading = "lazy";
        posterBox.appendChild(image);
      } else {
        posterBox.className = "tmdb-poster placeholder";
        posterBox.textContent = "N/D";
      }
      card.appendChild(posterBox);

      var body = document.createElement("div");
      body.className = "tmdb-card-body";
      var strong = document.createElement("strong");
      if (!isTVDB && id) {
        var link = document.createElement("a");
        link.href = "https://www.themoviedb.org/" + (kind === "movie" ? "movie/" : "tv/") + encodeURIComponent(id);
        link.target = "_blank";
        link.rel = "noopener";
        link.textContent = title;
        strong.appendChild(link);
      } else {
        strong.textContent = title;
      }
      body.appendChild(strong);

      var meta = document.createElement("small");
      meta.textContent = (isTVDB ? "TVDB " : "TMDB ") + (id || "—") + (year ? " · " + year : "") +
        (item.vote_average ? " · ★ " + item.vote_average : "");
      body.appendChild(meta);

      if (item.in_library) {
        var already = document.createElement("span");
        already.className = "badge ok tmdb-in-library";
        already.textContent = "Già in lista";
        already.title = "Questo titolo è già presente nella libreria";
        body.appendChild(already);
      }

      if (item.overview) {
        var overview = String(item.overview);
        var text = document.createElement("p");
        text.className = "tmdb-overview";
        text.textContent = overview.length > 180 ? overview.slice(0, 180) + "…" : overview;
        body.appendChild(text);
      }

      var add = document.createElement("button");
      add.className = "btn sm primary";
      add.textContent = item.in_library ? "Già in lista" : "Aggiungi alla libreria";
      if (item.in_library) add.disabled = true;
      add.addEventListener("click", function () {
        openAddModal(kind, {
          name: title,
          year: year,
          tmdb_id: isTVDB ? "" : id,
          tvdb_id: isTVDB ? id : ""
        });
      });
      body.appendChild(add);

      card.appendChild(body);
      grid.appendChild(card);
    });
    container.appendChild(grid);
  }

  function renderDiscoverResults(container, items, kind) {
    renderTmdbCards(container, items, kind);
  }

  function renderRecentList(container, items) {
    container.innerHTML = "";
    if (!items.length) {
      container.innerHTML = '<p class="muted">Nessun download recente.</p>';
      return;
    }
    items.slice(0, 6).forEach(function (item) {
      var isSeries = item && item.kind === "series";
      var name = String(item && item.name || "—");
      var label = isSeries
        ? name + " S" + String(item.season || "—") + "E" + String(item.episode || "—")
        : name;
      var detail = isSeries
        ? "S" + String(item.season || "—") + "E" + String(item.episode || "—") + " · "
        : "";
      detail += String(item && item.downloaded_at || "—") + " · " + humanBytes(item && item.size_bytes);
      var row = document.createElement("div");
      row.className = "list-item";
      var poster = document.createElement("div");
      poster.className = "list-poster placeholder";
      poster.textContent = "N/D";
      var posterURL = item && typeof item.poster === "string" ? item.poster : "";
      if (/^https?:\/\//i.test(posterURL)) {
        var image = document.createElement("img");
        image.className = "list-poster";
        image.src = posterURL;
        image.alt = name;
        image.loading = "lazy";
        poster = image;
      }
      var text = document.createElement("div");
      var title = document.createElement("strong");
      title.textContent = label;
      var small = document.createElement("small");
      small.textContent = detail;
      text.appendChild(title);
      text.appendChild(small);
      row.appendChild(poster);
      row.appendChild(text);
      container.appendChild(row);
    });
  }

  var recentList = document.querySelector("[data-dashboard-recent-list]");
  if (recentList) {
    api("/api/recent-downloads", "GET").then(function (data) {
      renderRecentList(recentList, data && Array.isArray(data.items) ? data.items : []);
    }).catch(function (error) {
      recentList.innerHTML = '<p class="muted">' + esc(error.message) + "</p>";
    });
  }

  // Dashboard "Ultimi trovati nelle sorgenti": flattens the feed matches so a
  // release can be queued straight from the dashboard, like the classic UI.
  var feedPanel = document.querySelector("[data-dashboard-feed]");
  if (feedPanel) {
    var feedBody = feedPanel.querySelector("[data-dashboard-feed-body]");
    var feedStatus = feedPanel.querySelector("[data-dashboard-feed-status]");
    var loadFeed = function () {
      feedBody.innerHTML = '<tr><td class="muted" colspan="5">Caricamento…</td></tr>';
      api("/api/feed/status", "GET").then(function (data) {
        var rows = [];
        (data.items || []).forEach(function (item) {
          (item.matches || []).forEach(function (match) {
            rows.push({ name: item.name, kind: item.kind, title: match.title, source: match.source, magnet: match.magnet });
          });
        });
        if (feedStatus) feedStatus.textContent = rows.length + " release";
        if (!rows.length) {
          feedBody.innerHTML = '<tr><td class="muted" colspan="5">Nessuna release trovata.</td></tr>';
          return;
        }
        feedBody.innerHTML = "";
        rows.slice(0, 40).forEach(function (row) {
          var tr = document.createElement("tr");
          var nameCell = document.createElement("td");
          nameCell.textContent = String(row.name || "—");
          var kindCell = document.createElement("td");
          kindCell.textContent = row.kind === "movie" ? "Film" : "Serie TV";
          var releaseCell = document.createElement("td");
          var release = document.createElement("span");
          release.className = "cell-truncate";
          release.title = String(row.title || "");
          release.textContent = String(row.title || "");
          releaseCell.appendChild(release);
          var sourceCell = document.createElement("td");
          sourceCell.textContent = String(row.source || "—");
          var actionCell = document.createElement("td");
          var add = document.createElement("button");
          add.className = "btn sm primary";
          add.textContent = "Accoda";
          add.addEventListener("click", function () {
            add.disabled = true;
            api("/api/archive/add", "POST", { title: row.title, magnet: row.magnet, source: row.source })
              .then(function () { add.textContent = "Accodato"; })
              .catch(function (error) { notify("Accoda non riuscito: " + error.message, "err"); add.disabled = false; });
          });
          actionCell.appendChild(add);
          tr.appendChild(nameCell); tr.appendChild(kindCell); tr.appendChild(releaseCell);
          tr.appendChild(sourceCell); tr.appendChild(actionCell);
          feedBody.appendChild(tr);
        });
      }).catch(function (error) {
        feedBody.innerHTML = '<tr><td class="alert" colspan="5">' + esc(error.message) + "</td></tr>";
      });
    };
    var feedButton = feedPanel.querySelector("[data-dashboard-feed-load]");
    if (feedButton) feedButton.addEventListener("click", loadFeed);
  }

  // ---- logs: filter, level highlighting, follow ---------------------------
  var logsView = document.querySelector("[data-logs-view]");
  if (logsView) {
    var logsPanel = logsView.closest("[data-logs]");
    var logsFilter = logsPanel.querySelector("[data-logs-filter]");
    var logsLines = logsPanel.querySelector("[data-logs-lines]");
    var logsCount = logsPanel.querySelector("[data-logs-count]");
    var logsFollowButton = logsPanel.querySelector("[data-logs-follow]");
    var logsCache = [];
    var logsFollow = true;
    var logsTimer = null;
    var logsRefreshInFlight = false;

    var highlightLogLine = function (line) {
      return esc(line)
        .replace(/\bERROR\b/g, '<span class="hl-err">ERROR</span>')
        .replace(/\b(WARN|WARNING)\b/g, '<span class="hl-warn">$1</span>')
        .replace(/\b(completed|completato|archived|archiviato|moved|approved|approvato)\b/gi, '<span class="hl-ok">$1</span>')
        .replace(/\b(indexer|feed|source|sorgente|scraping|engine)\b/gi, '<span class="hl-src">$1</span>')
        .replace(/\b(upgrade|score|punteggio)\b/gi, '<span class="hl-score">$1</span>')
        .replace(/\b(filter|filtered|rejected|scartato|skipped|blocklist|stalled)\b/gi, '<span class="hl-filter">$1</span>');
    };

    var renderLogs = function () {
      var query = (logsFilter && logsFilter.value || "").trim().toLowerCase();
      var lines = logsCache.filter(function (line) {
        return !query || line.toLowerCase().indexOf(query) >= 0;
      });
      logsView.innerHTML = lines.map(function (line) {
        return '<span class="log-line">' + highlightLogLine(line) + "</span>";
      }).join("");
      if (logsCount) logsCount.textContent = String(lines.length);
      if (logsFollow) logsView.scrollTop = logsView.scrollHeight;
    };

    var refreshLogs = function () {
      if (logsRefreshInFlight) return;
      logsRefreshInFlight = true;
      var limit = logsLines ? logsLines.value : "500";
      pollRequest("/api/logs?limit=" + encodeURIComponent(limit)).then(function (data) {
        logsCache = Array.isArray(data.items) ? data.items : [];
        renderLogs();
      }).catch(function () { /* keep the previous view on a transient error */ }).then(function () {
        logsRefreshInFlight = false;
      });
    };

    var scheduleLogs = function () {
      if (logsTimer) clearInterval(logsTimer);
      if (!logsFollow) return;
      logsTimer = setInterval(function () {
        if (document.visibilityState === "visible") refreshLogs();
      }, 5000);
    };

    if (logsFilter) logsFilter.addEventListener("input", renderLogs);
    if (logsLines) logsLines.addEventListener("change", refreshLogs);
    var logsRefreshButton = logsPanel.querySelector("[data-logs-refresh]");
    if (logsRefreshButton) logsRefreshButton.addEventListener("click", refreshLogs);
    if (logsFollowButton) logsFollowButton.addEventListener("click", function () {
      logsFollow = !logsFollow;
      logsFollowButton.textContent = logsFollow ? "⏸ Ferma scorrimento" : "▶ Riprendi";
      scheduleLogs();
      if (logsFollow) renderLogs();
    });
    document.addEventListener("visibilitychange", scheduleLogs);
    refreshLogs();
    scheduleLogs();
  }

  // ---- maintenance: duplicate cleanup -------------------------------------
  var duplicatesPanel = document.querySelector("[data-duplicates]");
  if (duplicatesPanel) {
    var dupResults = duplicatesPanel.querySelector("[data-duplicates-results]");
    var dupStatus = duplicatesPanel.querySelector("[data-duplicates-status]");
    var renderDuplicates = function (items) {
      dupResults.innerHTML = "";
      if (!items.length) { dupResults.innerHTML = '<p class="muted">Nessun duplicato inferiore trovato.</p>'; return; }
      var table = document.createElement("table");
      table.className = "data-table";
      table.innerHTML = "<thead><tr><th>Serie</th><th>Stagione</th><th>Episodio</th><th>File</th><th>Risoluzione</th></tr></thead>";
      accessibleDataTable(table, "Duplicati video");
      var tbody = document.createElement("tbody");
      items.forEach(function (item) {
        var tr = document.createElement("tr");
        var series = document.createElement("td"); series.textContent = String(item.series || "");
        var season = document.createElement("td"); season.className = "numeric"; season.textContent = String(item.season || 0);
        var episode = document.createElement("td"); episode.className = "numeric"; episode.textContent = String(item.episode || 0);
        var fileCell = document.createElement("td");
        var file = document.createElement("span"); file.className = "cell-truncate"; file.title = String(item.path || ""); file.textContent = folderLabel(item.path || "");
        fileCell.appendChild(file);
        var resolution = document.createElement("td"); resolution.textContent = String(item.resolution_rank) + " (migliore " + String(item.best_rank) + ")";
        tr.appendChild(series); tr.appendChild(season); tr.appendChild(episode); tr.appendChild(fileCell); tr.appendChild(resolution);
        tbody.appendChild(tr);
      });
      table.appendChild(tbody);
      dupResults.appendChild(table);
    };
    var runDuplicates = function (execute) {
      if (execute && !confirm("Spostare nel cestino le copie inferiori?")) return;
      if (dupStatus) dupStatus.textContent = "Analisi…";
      api("/api/maintenance/clean-duplicates", "POST", { execute: execute }).then(function (data) {
        if (execute) {
          if (dupStatus) dupStatus.textContent = "";
          dupResults.innerHTML = '<p class="muted">Rimossi ' + String(data.removed || 0) + ' file.</p>';
          notify("Pulizia duplicati completata", "ok");
          return;
        }
        var items = data.items || [];
        if (dupStatus) dupStatus.textContent = items.length + " file inferiori";
        renderDuplicates(items);
      }).catch(function (error) {
        if (dupStatus) dupStatus.textContent = "";
        dupResults.innerHTML = '<p class="alert">' + esc(error.message) + "</p>";
      });
    };
    var dupPreview = duplicatesPanel.querySelector("[data-duplicates-preview]");
    if (dupPreview) dupPreview.addEventListener("click", function () { runDuplicates(false); });
    var dupClean = duplicatesPanel.querySelector("[data-duplicates-clean]");
    if (dupClean) dupClean.addEventListener("click", function () { runDuplicates(true); });
  }

  // ---- maintenance: review-based folder rename ----------------------------
  var folderRenamePanel = document.querySelector("[data-folder-rename]");
  if (folderRenamePanel) {
    var folderRenamePath = folderRenamePanel.querySelector("[data-folder-rename-path]");
    var folderRenameScan = folderRenamePanel.querySelector("[data-folder-rename-scan]");
    var folderRenameBrowse = folderRenamePanel.querySelector("[data-folder-rename-browse]");
    var folderRenameActions = folderRenamePanel.querySelector("[data-folder-rename-actions]");
    var folderRenameResults = folderRenamePanel.querySelector("[data-folder-rename-results]");
    var folderRenameStatus = folderRenamePanel.querySelector("[data-folder-rename-status]");
    var folderRenameCount = folderRenamePanel.querySelector("[data-folder-rename-count]");
    var folderRenameState = { path: "", items: [] };

    var folderRenameStatusLabel = function (item) {
      if (item._applied) return "Rinominato";
      if (item._rejected) return "Rifiutato";
      if (item.conflict) return "Conflitto";
      if (item.status === "unmatched") return "Non identificato";
      if (item.status === "proposta") return "Proposta forte";
      return "Da verificare";
    };

    var folderRenameUpdateCount = function () {
      var selected = folderRenamePanel.querySelectorAll("[data-folder-rename-check]:checked").length;
      var available = folderRenamePanel.querySelectorAll("[data-folder-rename-check]:not(:disabled)").length;
      if (folderRenameCount) folderRenameCount.textContent = selected + " selezionate · " + available + " proposte";
    };

    var folderRenameRender = function () {
      folderRenameResults.innerHTML = "";
      if (!folderRenameState.items.length) {
        folderRenameResults.hidden = true;
        folderRenameActions.hidden = true;
        return;
      }
      var table = document.createElement("table");
      table.className = "data-table folder-rename-table";
      var head = document.createElement("thead");
      head.innerHTML = "<tr><th>Accetta</th><th>File</th><th>Tipo</th><th>Rilevato</th><th>Confronto TMDB/TVDB</th><th>Nuovo nome</th><th>Stato</th><th>Azioni</th></tr>";
      table.appendChild(head);
      accessibleDataTable(table, "Anteprima rinomina file");
      var body = document.createElement("tbody");
      folderRenameState.items.forEach(function (item, index) {
        var row = document.createElement("tr");
        row.setAttribute("data-folder-rename-row", String(index));

        var selectCell = document.createElement("td");
        var check = document.createElement("input");
        check.type = "checkbox";
        check.setAttribute("data-folder-rename-check", "");
        check.checked = !item._rejected && !!item.target && !item.conflict;
        check.disabled = !item.target || item.status === "unmatched" || !!item.conflict || !!item._applied;
        check.addEventListener("change", function () {
          item._rejected = !check.checked;
          folderRenameUpdateCount();
        });
        selectCell.appendChild(check);
        row.appendChild(selectCell);

        var fileCell = document.createElement("td");
        var file = document.createElement("span");
        file.className = "cell-truncate";
        file.title = String(item.source || "");
        file.textContent = String(item.relative || item.source || "");
        fileCell.appendChild(file);
        row.appendChild(fileCell);

        var kind = document.createElement("td");
        kind.textContent = item.kind === "series" ? "Serie" : item.kind === "movie" ? "Film" : "—";
        row.appendChild(kind);
        var detected = document.createElement("td");
        detected.textContent = String(item.detected || "—") + (item.season ? " · S" + String(item.season).padStart(2, "0") + "E" + String(item.episode).padStart(2, "0") : "");
        row.appendChild(detected);

        var match = document.createElement("td");
        if (item.candidates && item.candidates.length) {
          var candidateSelect = document.createElement("select");
          candidateSelect.className = "input compact";
          candidateSelect.setAttribute("data-folder-rename-candidate", "");
          item.candidates.forEach(function (candidate, candidateIndex) {
            var option = document.createElement("option");
            option.value = String(candidateIndex);
            option.textContent = String(candidate.provider || "") .toUpperCase() + " · " + String(candidate.title || "") + (candidate.year ? " (" + candidate.year + ")" : "");
            candidateSelect.appendChild(option);
          });
          candidateSelect.addEventListener("change", function () {
            var candidate = item.candidates[Number(candidateSelect.value)];
            if (!candidate) return;
            item.target = candidate.target || "";
            target.title = String(item.target || "");
            target.textContent = pathBaseName(item.target) || "—";
            item._rejected = false;
            check.disabled = !item.target || !!item.conflict;
            check.checked = !check.disabled;
            folderRenameUpdateCount();
          });
          match.appendChild(candidateSelect);
        } else {
          match.textContent = item.reason || "Nessuna proposta";
        }
        row.appendChild(match);

        var targetCell = document.createElement("td");
        var target = document.createElement("span");
        target.className = "cell-truncate";
        target.title = String(item.target || "");
        target.textContent = pathBaseName(item.target) || "—";
        targetCell.appendChild(target);
        row.appendChild(targetCell);

        var stateCell = document.createElement("td");
        stateCell.textContent = folderRenameStatusLabel(item) + (item.reason && item.status !== "unmatched" ? " · " + item.reason : "");
        row.appendChild(stateCell);

        var actions = document.createElement("td");
        actions.className = "row-actions";
        var accept = document.createElement("button");
        accept.className = "btn sm";
        accept.type = "button";
        accept.textContent = "Accetta";
        accept.disabled = check.disabled;
        accept.addEventListener("click", function () { check.checked = true; item._rejected = false; folderRenameUpdateCount(); });
        actions.appendChild(accept);
        var reject = document.createElement("button");
        reject.className = "btn sm";
        reject.type = "button";
        reject.textContent = "Rifiuta";
        reject.addEventListener("click", function () { check.checked = false; item._rejected = true; folderRenameUpdateCount(); });
        actions.appendChild(reject);
        row.appendChild(actions);
        body.appendChild(row);
      });
      table.appendChild(body);
      folderRenameResults.appendChild(table);
      folderRenameResults.hidden = false;
      folderRenameActions.hidden = false;
      folderRenameUpdateCount();
    };

    var folderRenameApply = function () {
      var selected = [];
      Array.prototype.forEach.call(folderRenamePanel.querySelectorAll("[data-folder-rename-check]:checked"), function (check) {
        var row = check.closest("[data-folder-rename-row]");
        var index = row ? Number(row.getAttribute("data-folder-rename-row")) : -1;
        if (index >= 0 && folderRenameState.items[index]) selected.push(folderRenameState.items[index]);
      });
      if (!selected.length) { notify("Seleziona almeno una proposta", "err"); return; }
      var applyButton = folderRenamePanel.querySelector("[data-folder-rename-apply]");
      if (applyButton) applyButton.disabled = true;
      if (folderRenameStatus) folderRenameStatus.textContent = "Rinomina in corso…";
      api("/api/maintenance/rename-folder/apply", "POST", {
        path: folderRenameState.path,
        items: selected.map(function (item) { return { source: item.source, target: item.target }; })
      }).then(function (data) {
        var successful = {};
        (data.items || []).forEach(function (result) {
          if (result.ok) successful[result.source] = true;
        });
        folderRenameState.items = folderRenameState.items.filter(function (item) { return !successful[item.source]; });
        if (folderRenameStatus) folderRenameStatus.textContent = "";
        folderRenameRender();
        notify("Rinominati " + String(data.renamed || 0) + " file", "ok");
      }).catch(function (error) {
        if (folderRenameStatus) folderRenameStatus.textContent = "";
        notify("Rinomina cartella non riuscita: " + error.message, "err");
      }).then(function () { if (applyButton) applyButton.disabled = false; });
    };

    folderRenameScan.addEventListener("click", function () {
      var path = String(folderRenamePath.value || "").trim();
      if (!path) { notify("Inserisci il percorso di una cartella", "err"); return; }
      folderRenameState = { path: path, items: [] };
      folderRenameScan.disabled = true;
      if (folderRenameStatus) folderRenameStatus.textContent = "Scansione e confronto con TMDB/TVDB…";
      api("/api/maintenance/rename-folder/scan", "POST", { path: path }).then(function (data) {
        folderRenameState.path = data.path || path;
        folderRenameState.items = (data.items || []).map(function (item) { item._rejected = false; return item; });
        if (folderRenameStatus) folderRenameStatus.textContent = folderRenameState.items.length + " file analizzati";
        folderRenameRender();
      }).catch(function (error) {
        if (folderRenameStatus) folderRenameStatus.textContent = "";
        folderRenameResults.innerHTML = '<p class="alert">' + esc(error.message) + "</p>";
        folderRenameResults.hidden = false;
      }).then(function () { folderRenameScan.disabled = false; });
    });
    if (folderRenameBrowse) folderRenameBrowse.addEventListener("click", function () {
      openBrowseModal(folderRenamePath);
    });
    folderRenamePanel.querySelector("[data-folder-rename-accept-all]").addEventListener("click", function () {
      folderRenameState.items.forEach(function (item) { if (item.target && !item.conflict) item._rejected = false; });
      folderRenameRender();
    });
    folderRenamePanel.querySelector("[data-folder-rename-apply]").addEventListener("click", folderRenameApply);
  }

  // ---- maintenance: RAM disk control --------------------------------------
  var ramdiskPanel = document.querySelector("[data-ramdisk]");
  if (ramdiskPanel) {
    var ramdiskPaths = ramdiskPanel.querySelector("[data-ramdisk-paths]");
    var ramdiskStatus = ramdiskPanel.querySelector("[data-ramdisk-status]");
    var ramdiskMessage = ramdiskPanel.querySelector("[data-ramdisk-message]");
    var loadRamdisk = function () {
      api("/api/ramdisk", "GET").then(function (data) {
        if (ramdiskStatus) ramdiskStatus.textContent = data.configured ? "in uso: " + data.configured : "non configurato";
        var paths = data.paths || [];
        ramdiskPaths.innerHTML = "";
        if (!paths.length) { ramdiskPaths.innerHTML = '<p class="muted">Nessun tmpfs/ramfs scrivibile trovato.</p>'; return; }
        var table = document.createElement("table");
        table.className = "data-table";
        table.innerHTML = "<thead><tr><th>Percorso</th><th>Filesystem</th><th>Liberi</th><th>Totali</th><th></th></tr></thead>";
        accessibleDataTable(table, "Percorsi RAM disk");
        var tbody = document.createElement("tbody");
        paths.forEach(function (item) {
          var tr = document.createElement("tr");
          var path = document.createElement("td"); path.textContent = String(item.path || "");
          var filesystem = document.createElement("td"); filesystem.textContent = String(item.filesystem || "");
          var free = document.createElement("td"); free.className = "numeric"; free.textContent = humanBytes(item.free_bytes || 0);
          var total = document.createElement("td"); total.className = "numeric"; total.textContent = humanBytes(item.total_bytes || 0);
          var action = document.createElement("td"); action.className = "row-actions";
          if (item.configured) {
            action.innerHTML = '<span class="badge ok">in uso</span>';
          } else if (item.writable) {
            var use = document.createElement("button");
            use.className = "btn sm primary";
            use.textContent = "Usa questo percorso";
            use.addEventListener("click", function () {
              use.disabled = true;
              // /dev/shm is a shared tmpfs: create a dedicated subfolder there
              // silently, then select it. Other paths are selected as-is.
              var isDevShm = String(item.path) === "/dev/shm";
              var endpoint = isDevShm ? "/api/ramdisk/create" : "/api/ramdisk/select";
              var body = isDevShm ? { path: "/dev/shm/gextto" } : { path: item.path };
              api(endpoint, "POST", body).then(function (res) {
                var recommended = (res && res.recommended) || {};
                var chosen = isDevShm ? "/dev/shm/gextto" : item.path;
                if (ramdiskMessage) {
                  ramdiskMessage.textContent = "Selezionato " + chosen +
                    (recommended.threshold_gb ? " · soglia " + recommended.threshold_gb + " GB, margine " + recommended.margin_gb + " GB" : "") +
                    (isDevShm ? " (cartella creata se assente)" : "");
                }
                notify("RAM disk selezionato", "ok");
                loadRamdisk();
              }).catch(function (error) { notify("Selezione non riuscita: " + error.message, "err"); use.disabled = false; });
            });
            action.appendChild(use);
          } else {
            action.innerHTML = '<span class="muted">non scrivibile</span>';
          }
          tr.appendChild(path); tr.appendChild(filesystem); tr.appendChild(free); tr.appendChild(total); tr.appendChild(action);
          tbody.appendChild(tr);
        });
        table.appendChild(tbody);
        ramdiskPaths.appendChild(table);
      }).catch(function (error) {
        ramdiskPaths.innerHTML = '<p class="alert">' + esc(error.message) + "</p>";
      });
    };
    var ramdiskRefresh = ramdiskPanel.querySelector("[data-ramdisk-refresh]");
    if (ramdiskRefresh) ramdiskRefresh.addEventListener("click", loadRamdisk);
    loadRamdisk();
  }

  // ---- release results (search, gaps, comic result panels) ----------------
  // Shared renderer: Release | Sorgente | Risoluzione | Codec | Azioni, with a
  // local text filter and the "Perché non questo?" decision explanation, like
  // rextto. `filterInput`/`countNode` are optional.
  function renderReleaseTable(opts) {
    var tbody = opts.container;
    if (!tbody) return;
    var items = opts.items || [];
    var filterInput = opts.filterInput;
    var query = (filterInput && filterInput.value || "").trim().toLowerCase();
    var terms = query ? query.split(/\s+/).filter(Boolean) : [];
    var visible = terms.length ? items.filter(function (release) {
      var quality = release.quality || {};
      var text = (String(release.title || "") + " " + String(release.source || "") +
        " " + String(quality.resolution || "") + " " + String(quality.codec || "")).toLowerCase();
      return terms.every(function (term) {
        if (term.charAt(0) === "-") return text.indexOf(term.slice(1)) < 0;
        return text.indexOf(term) >= 0;
      });
    }) : items;
    if (opts.countNode) {
      opts.countNode.textContent = terms.length ? visible.length + "/" + items.length + " risultati" : items.length + " risultati";
    }
    if (!visible.length) {
      tbody.innerHTML = '<tr><td class="muted" colspan="4">' + esc(terms.length ? "Nessun risultato con questo filtro." : "Nessun risultato.") + "</td></tr>";
      return;
    }
    tbody.innerHTML = "";
    visible.forEach(function (release) {
      var row = document.createElement("tr");
      row.setAttribute("data-release-row", "1");
      row.setAttribute("data-title", String(release.title || ""));
      row.setAttribute("data-score", String(Number(release.score) || 0));

      var titleCell = document.createElement("td");
      titleCell.className = "release-title";
      titleCell.textContent = String(release.title || "—");
      titleCell.title = String(release.title || "");

      var sourceCell = document.createElement("td");
      sourceCell.className = "release-source";
      sourceCell.textContent = String(release.source || "—");
      sourceCell.title = String(release.source || "");

      var scoreCell = document.createElement("td");
      scoreCell.className = "release-score numeric";
      scoreCell.textContent = String(Number(release.score) || 0);
      scoreCell.title = "Punteggio di qualità della release";

      var actionsCell = document.createElement("td");
      actionsCell.className = "row-actions";
      var explain = document.createElement("button");
      explain.className = "btn sm";
      explain.textContent = "Perché non questo?";
      explain.title = "Mostra perché questa release viene accettata o scartata";
       explain.addEventListener("click", function () { showExplain(release, explain); });
      var add = document.createElement("button");
      add.className = "btn sm primary";
      add.textContent = "Accoda";
      add.title = "Aggiungi questa release ai download";
      add.addEventListener("click", function () {
        add.disabled = true;
        api(opts.addPath || "/api/search/add", "POST", { release: release })
          .then(function () { add.textContent = "Accodata"; })
          .catch(function (error) { notify("Non accodata: " + error.message, "err"); add.disabled = false; });
      });
      actionsCell.appendChild(explain);
      actionsCell.appendChild(add);

      row.appendChild(titleCell);
      row.appendChild(sourceCell);
      row.appendChild(scoreCell);
      row.appendChild(actionsCell);
      tbody.appendChild(row);
    });
    if (tbody._releaseSort && tbody._releaseSort.key) sortReleaseRows(tbody, tbody._releaseSort);
  }

  function sortReleaseRows(tbody, state) {
    var rows = Array.prototype.slice.call(tbody.querySelectorAll("tr[data-release-row]"));
    rows.sort(function (left, right) {
      if (state.key === "score") {
        return ((Number(left.getAttribute("data-score")) || 0) - (Number(right.getAttribute("data-score")) || 0)) * state.dir;
      }
      return String(left.getAttribute("data-title") || "").localeCompare(String(right.getAttribute("data-title") || "")) * state.dir;
    });
    rows.forEach(function (row) { tbody.appendChild(row); });
  }

  document.addEventListener("click", function (event) {
    var explainButton = event.target.closest("[data-release-explain]");
    if (explainButton) {
      try {
        showExplain(JSON.parse(explainButton.getAttribute("data-release-explain") || "{}"), explainButton);
      } catch (_) {
        notify("Impossibile leggere la release", "err");
      }
      return;
    }
    var head = event.target.closest("[data-release-sort]");
    if (!head) return;
    var table = head.closest("table");
    var tbody = table && table.querySelector("tbody");
    if (!tbody) return;
    var key = head.getAttribute("data-release-sort");
    var state = tbody._releaseSort || { key: "", dir: 1 };
    if (state.key === key) {
      state.dir = -state.dir;
    } else {
      state.key = key;
      state.dir = 1;
    }
    tbody._releaseSort = state;
    sortReleaseRows(tbody, state);
    Array.prototype.forEach.call(table.querySelectorAll("[data-release-sort]"), function (other) {
      if (other === head) {
        other.setAttribute("data-sort-dir", state.dir === 1 ? "asc" : "desc");
        other.setAttribute("aria-sort", state.dir === 1 ? "ascending" : "descending");
      } else {
        other.removeAttribute("data-sort-dir");
        other.setAttribute("aria-sort", "none");
      }
    });
  });

  function renderReleaseResults(container, items) {
    container.innerHTML = "";
    var table = document.createElement("table");
    table.className = "data-table release-table";
    table.innerHTML = "<thead><tr><th scope=\"col\" class=\"th-sort\" data-release-sort=\"title\" aria-sort=\"none\" title=\"Nome del file — clicca per ordinare\"><button class=\"table-sort-button\" type=\"button\">Release</button></th><th scope=\"col\">Sorgente</th><th scope=\"col\" class=\"th-sort\" data-release-sort=\"score\" aria-sort=\"none\" title=\"Punteggio di qualità — clicca per ordinare\"><button class=\"table-sort-button\" type=\"button\">Punteggio</button></th><th scope=\"col\">Azioni</th></tr></thead>";
    accessibleDataTable(table, "Risultati release");
    var tbody = document.createElement("tbody");
    table.appendChild(tbody);
    container.appendChild(table);
    renderReleaseTable({ container: tbody, items: items, addPath: "/api/search/add" });
  }

  // ---- decision explanation overlay ("Perché non questo?") ----------------
  function showExplain(release, opener) {
    var overlay = document.getElementById("explain-overlay");
    if (!overlay) {
      overlay = document.createElement("div");
      overlay.id = "explain-overlay";
      overlay.className = "overlay";
      overlay.innerHTML = '<div class="modal" role="dialog" aria-modal="true" aria-labelledby="explain-title">' +
        '<div class="modal-head"><h3 id="explain-title">Perché non questo?</h3><button class="btn sm" type="button" data-explain-close>Chiudi</button></div>' +
        '<div class="modal-body" data-explain-body></div></div>';
      overlay.addEventListener("click", function (event) {
        if (event.target === overlay || event.target.closest("[data-explain-close]")) closeAccessibleDialog(overlay);
      });
      document.addEventListener("keydown", function (event) {
        if (event.key === "Escape" && !overlay.hidden) closeAccessibleDialog(overlay);
      });
      document.body.appendChild(overlay);
    }
    openAccessibleDialog(overlay, opener);
    var body = overlay.querySelector("[data-explain-body]");
    body.innerHTML = '<p class="muted">Analisi della release…</p>';
    api("/api/search/explain", "POST", { release: release }).then(function (data) {
      renderDecisionTrace(body, data.trace || {});
    }).catch(function (error) {
      body.innerHTML = '<p class="alert">' + esc(error.message) + "</p>";
    });
  }

  function renderDecisionTrace(container, trace) {
    container.innerHTML = "";
    var verdict = String(trace.decision || "unknown").toLowerCase();
    var banner = document.createElement("p");
    banner.className = "badge " + (verdict === "approved" || verdict === "accept" ? "ok" : (verdict === "rejected" ? "err" : "warn"));
    banner.textContent = verdict === "approved" || verdict === "accept" ? "Accettata" : (verdict === "rejected" ? "Rifiutata" : (trace.decision || "—"));
    container.appendChild(banner);

    var title = document.createElement("h4");
    title.textContent = String(trace.candidate || "");
    container.appendChild(title);
    if (trace.reason) {
      var reason = document.createElement("p");
      reason.className = "muted";
      reason.textContent = "Motivo: " + String(trace.reason);
      container.appendChild(reason);
    }

    var summary = document.createElement("div");
    summary.className = "stat-grid";
    [["Punteggio", String(trace.score === undefined ? "—" : trace.score)],
     ["Obiettivo", String(trace.target || "—")]].forEach(function (pair) {
      var row = document.createElement("div");
      row.className = "row";
      var label = document.createElement("span");
      label.textContent = pair[0];
      var value = document.createElement("strong");
      value.textContent = pair[1];
      row.appendChild(label); row.appendChild(value);
      summary.appendChild(row);
    });
    container.appendChild(summary);

    var steps = trace.steps || [];
    if (steps.length) {
      var heading = document.createElement("div");
      heading.className = "field span-full";
      heading.innerHTML = "<span>Controlli</span>";
      container.appendChild(heading);
      var table = document.createElement("table");
      table.className = "data-table";
      table.innerHTML = "<thead><tr><th>Regola</th><th>Esito</th><th>Dettaglio</th></tr></thead>";
      accessibleDataTable(table, "Controlli punteggio");
      var tbody = document.createElement("tbody");
      steps.forEach(function (step) {
        var row = document.createElement("tr");
        var rule = document.createElement("td"); rule.textContent = String(step.rule || "");
        var result = document.createElement("td");
        result.innerHTML = '<span class="badge ' + (step.result === "ok" ? "ok" : (step.result === "fail" ? "err" : "warn")) + '">' + esc(String(step.result || "")) + "</span>";
        var detail = document.createElement("td"); detail.textContent = String(step.detail || "");
        row.appendChild(rule); row.appendChild(result); row.appendChild(detail);
        tbody.appendChild(row);
      });
      table.appendChild(tbody);
      container.appendChild(table);
    }

    var components = trace.score_components || [];
    if (components.length) {
      var compHeading = document.createElement("div");
      compHeading.className = "field span-full";
      compHeading.innerHTML = "<span>Punteggio</span>";
      container.appendChild(compHeading);
      var compTable = document.createElement("table");
      compTable.className = "data-table";
      compTable.innerHTML = "<thead><tr><th>Voce</th><th>Valore</th></tr></thead>";
      var compBody = document.createElement("tbody");
      components.forEach(function (component) {
        var row = document.createElement("tr");
        var label = document.createElement("td"); label.textContent = String(component.label || component.key || "");
        var value = document.createElement("td"); value.className = "numeric"; value.textContent = String(component.value === undefined ? "" : component.value);
        row.appendChild(label); row.appendChild(value);
        compBody.appendChild(row);
      });
      compTable.appendChild(compBody);
      container.appendChild(compTable);
    }

    if (trace.comparison && Object.keys(trace.comparison).length) {
      var cmpHeading = document.createElement("div");
      cmpHeading.className = "field span-full";
      cmpHeading.innerHTML = "<span>Confronto con l'archivio</span>";
      container.appendChild(cmpHeading);
      var cmp = document.createElement("div");
      cmp.className = "output";
      renderReadable(cmp, trace.comparison);
      container.appendChild(cmp);
    }
  }

  function renderTmdbResults(container, items, form) {
    var kindField = form ? form.querySelector("[name=kind]") : null;
    var kind = kindField ? kindField.value : "series";
    renderTmdbCards(container, items, kind);
  }

  // ---- generic JSON forms (section forms) ---------------------------------
  Array.prototype.forEach.call(document.querySelectorAll("[data-json-form]"), function (form) {
    form.addEventListener("submit", function (event) {
      event.preventDefault();
      var body = {};
      Array.prototype.forEach.call(form.querySelectorAll("[name]"), function (field) {
        var value = field.value;
        if (field.type === "number") value = Number(value);
        // boolField renders a select for APIs whose JSON contract uses real
        // booleans (weekly/comics and provider settings), not string values.
        if (value === "true") value = true;
        if (value === "false") value = false;
        body[field.name] = value;
      });
      var button = form.querySelector("button[type=submit]");
      var message = form.querySelector("[data-form-message]");
      var output = form.querySelector("[data-form-output]");
      if (button) button.disabled = true;
      var wrap = form.getAttribute("data-wrap");
      var payload = wrap ? (function () { var value = {}; value[wrap] = body; return value; }()) : body;
      api(form.getAttribute("data-endpoint"), form.getAttribute("data-method") || "POST", payload).then(function (data) {
        if (message) message.textContent = "Fatto";
        if (output && data !== undefined && data !== null && data !== "") {
          output.hidden = false;
          if (form.getAttribute("data-render") === "tmdb" && data && Array.isArray(data.items)) {
            renderTmdbResults(output, data.items, form);
          } else if (form.getAttribute("data-render") === "releases" && data && Array.isArray(data.results)) {
            renderReleaseResults(output, data.results);
          } else if (form.getAttribute("data-render") === "comics" && data && Array.isArray(data.items)) {
            renderComicsResults(output, data.items);
          } else {
            renderReadable(output, data);
          }
        }
      }).catch(function (error) {
        notify("Operazione non riuscita: " + error.message, "err");
      }).then(function () { if (button) button.disabled = false; });
    });
  });

  // Close the torrent overlays with Escape.
  document.addEventListener("keydown", function (event) {
    if (event.key !== "Escape") return;
    var panel = page.querySelector("[data-torrent-detail-panel]");
    if (panel && !panel.hidden) closeAccessibleDialog(panel);
    var removePanel = page.querySelector("[data-torrent-remove-panel]");
    if (removePanel && !removePanel.hidden) closeAccessibleDialog(removePanel);
  });

  // ---- progress polling ---------------------------------------------------
  Array.prototype.forEach.call(document.querySelectorAll("[data-progress][data-endpoint]"), function (node) {    var endpoint = node.getAttribute("data-endpoint");
    var panel = node.closest(".panel");
    var bar = panel.querySelector("[data-progress-bar]");
    var text = panel.querySelector("[data-progress-text]");
    var inFlight = false;
    function poll() {
      if (inFlight) return;
      inFlight = true;
      pollRequest(endpoint).then(function (data) {
        var progress = data && data.progress ? data.progress : data;
        if (!progress) return;
        var total = Number(progress.total) || 0;
         var current = Number(progress.current) || 0;
         var pct = total > 0 ? Math.min(100, Math.round(current / total * 100)) : (progress.running ? 0 : 100);
         if (bar) {
           bar.style.width = pct + "%";
           var progressBar = bar.closest('[role="progressbar"]') || bar;
           progressBar.setAttribute("aria-valuenow", String(pct));
           progressBar.setAttribute("aria-valuetext", pct + "%");
           progressBar.setAttribute("aria-busy", progress.running ? "true" : "false");
         }
         if (node) node.setAttribute("aria-busy", progress.running ? "true" : "false");
        if (text) {
          text.textContent = (progress.running ? "in corso" : "inattivo") +
            (progress.series ? " · " + progress.series : "") + " · " + current + "/" + total +
            (progress.errors ? " · errori " + progress.errors : "") +
            (progress.message ? " · " + progress.message : "");
        }
      }).catch(function () { /* keep the previous value */ }).then(function () {
        inFlight = false;
      });
    }
    poll();
    setInterval(poll, 3000);
  });

  // ---- OAuth / PIN flows (Trakt, Simkl) -----------------------------------
  document.addEventListener("click", function (event) {
    var start = event.target.closest("[data-oauth-start]");
    if (start) {
      var panel = start.closest(".panel");
      var output = panel.querySelector("[data-oauth-output]");
      start.disabled = true;
      api(start.getAttribute("data-oauth-start"), "POST", {})
        .then(function (data) { if (output) renderReadable(output, data); })
        .catch(function (error) { if (output) output.textContent = error.message; })
        .then(function () { start.disabled = false; });
      return;
    }
    var poll = event.target.closest("[data-oauth-poll]");
    if (poll) {
      var panel2 = poll.closest(".panel");
      var output2 = panel2.querySelector("[data-oauth-output]");
      var input = panel2.querySelector("[data-oauth-code]");
      var code = input ? input.value.trim() : "";
      if (!code) { notify("Inserisci il codice di accesso", "err"); return; }
      poll.disabled = true;
      api(poll.getAttribute("data-oauth-poll"), "POST", { code: code })
        .then(function (data) { if (output2) renderReadable(output2, data); })
        .catch(function (error) { if (output2) output2.textContent = error.message; })
        .then(function () { poll.disabled = false; });
      return;
    }
  });

  // ---- series detail hero + episode filter --------------------------------
  var seriesHero = document.querySelector("[data-series-hero]");
  if (seriesHero) {
    var heroSeriesName = seriesHero.getAttribute("data-series-name") || "";
    if (heroSeriesName) {
      api("/api/series/" + heroSeriesName + "/info", "GET").then(function (data) {
        renderSeriesHero(seriesHero, (data && data.info) || {});
      }).catch(function () { /* keep the server-rendered header */ });
    }
  }
  var seriesFilter = document.querySelector("[data-series-filter]");
  if (seriesFilter) {
    seriesFilter.addEventListener("input", function () {
      var query = (seriesFilter.value || "").trim().toLowerCase();
      Array.prototype.forEach.call(document.querySelectorAll(".series-episodes tbody tr"), function (row) {
        row.hidden = query !== "" && (row.textContent || "").toLowerCase().indexOf(query) < 0;
      });
    });
  }

  function heroPersonLink(person) {
    var label = esc(String(person && person.name || ""));
    if (!label) return "";
    var url = person && (person.url || person.profile_url);
    if (url && safeHref(url)) {
      return '<a href="' + esc(safeHref(url)) + '" target="_blank" rel="noopener">' + label + "</a>";
    }
    return '<a href="https://www.themoviedb.org/search?query=' + encodeURIComponent(String(person.name || "")) + '" target="_blank" rel="noopener">' + label + "</a>";
  }

  function heroCastHTML(cast) {
    if (!Array.isArray(cast) || !cast.length) return "";
    return "<small>Cast: " + cast.slice(0, 12).map(function (person) {
      var link = heroPersonLink(person);
      return person && person.character ? link + " (" + esc(String(person.character)) + ")" : link;
    }).join(", ") + "</small>";
  }

  function setHeroPoster(hero, url, name) {
    var slot = hero.querySelector("[data-hero-poster]");
    if (!slot || !url || !safeHref(url)) return;
    var image = document.createElement("img");
    image.className = "series-poster";
    image.loading = "lazy";
    image.alt = String(name || "");
    image.src = safeHref(url);
    slot.replaceWith(image);
  }

  function renderSeriesHero(hero, info) {
    var name = String(info.name || hero.getAttribute("data-name") || "");
    setHeroPoster(hero, String(info.poster || ""), name);
    var nameSlot = hero.querySelector("[data-hero-name]");
    if (nameSlot && info.name) nameSlot.textContent = String(info.name);
    var yearSlot = hero.querySelector("[data-hero-year]");
    if (yearSlot && info.year) yearSlot.textContent = String(info.year);
    var metaSlot = hero.querySelector("[data-hero-meta]");
    if (metaSlot) {
      var badges = [];
      if (info.network) badges.push(String(info.network));
      if (info.country) badges.push(String(info.country));
      if (info.vote) badges.push("★ " + String(info.vote));
      if (info.seasons) badges.push(String(info.seasons) + " stagioni");
      if (info.last_air_date) badges.push("ultima " + String(info.last_air_date));
      if (info.status) badges.push(String(info.status));
      var next = info.next_episode;
      if (next && next.name) {
        badges.push("Prossima: S" + String(next.season_number || "—") + "E" +
          String(next.episode_number || "—") + " · " + String(next.air_date || "—"));
      }
      metaSlot.innerHTML = badges.map(function (badge) {
        return '<span class="badge">' + esc(badge) + "</span>";
      }).join(" ");
      var heroTotal = Number(hero.getAttribute("data-total")) || 0;
      var heroDone = Number(hero.getAttribute("data-downloaded")) || 0;
      var heroComplete = heroTotal > 0 && heroDone >= heroTotal;
      var heroStatus = String(info.status || "").toLowerCase();
      var heroEnded = heroStatus.indexOf("ended") === 0 || heroStatus.indexOf("cancel") === 0;
      var flag = "";
      if (heroEnded && heroComplete) flag = '<span class="badge ok" title="Serie terminata e completa: tutti gli episodi disponibili sono archiviati">🏁 ✓✓</span>';
      else if (heroEnded) flag = '<span class="badge" title="Serie terminata: mancano ancora episodi">🏁 terminata</span>';
      else if (heroComplete) flag = '<span class="badge" title="Al passo: tutti gli episodi pubblicati finora sono archiviati, ma la serie non è terminata">✓ in pari</span>';
      if (flag) metaSlot.innerHTML = flag + (metaSlot.innerHTML ? " " + metaSlot.innerHTML : "");
    }
    var overviewSlot = hero.querySelector("[data-hero-overview]");
    if (overviewSlot && info.overview) overviewSlot.textContent = String(info.overview);
    var genresSlot = hero.querySelector("[data-hero-genres]");
    if (genresSlot && Array.isArray(info.genres) && info.genres.length) {
      genresSlot.innerHTML = info.genres.map(function (genre) {
        return '<span class="badge">' + esc(String(genre)) + "</span>";
      }).join(" ");
    }
    var castSlot = hero.querySelector("[data-hero-cast]");
    if (castSlot) castSlot.innerHTML = heroCastHTML(info.cast);
    var linksSlot = hero.querySelector("[data-hero-links]");
    if (linksSlot) {
      var links = "";
      if (info.tmdb_id) links += '<a class="btn sm" href="https://www.themoviedb.org/tv/' + encodeURIComponent(String(info.tmdb_id)) + '" target="_blank" rel="noopener">TMDB</a>';
      if (info.tvdb_url && safeHref(info.tvdb_url)) links += '<a class="btn sm" href="' + esc(safeHref(info.tvdb_url)) + '" target="_blank" rel="noopener">TVDB</a>';
      linksSlot.innerHTML = links;
    }
  }

  // Series season enable/disable toggles.
  document.addEventListener("click", function (event) {
    var toggle = event.target.closest("[data-season-toggle]");
    if (!toggle) return;
    var hero = toggle.closest("[data-series-hero]");
    if (!hero) return;
    var ignored = toggle.getAttribute("data-season-ignored") === "true";
    var season = parseInt(toggle.getAttribute("data-season-toggle"), 10);
    toggle.disabled = true;
    api("/api/series/" + (hero.getAttribute("data-series-name") || "") + "/toggle-season", "POST", { season: season, enabled: ignored })
      .then(function () {
        var nowIgnored = !ignored;
        toggle.setAttribute("data-season-ignored", nowIgnored ? "true" : "false");
        toggle.classList.toggle("warn", nowIgnored);
        toggle.classList.toggle("primary", !nowIgnored);
        notify(nowIgnored ? ("Stagione " + season + " disattivata") : ("Stagione " + season + " attivata"), "ok");
        toggle.disabled = false;
      })
      .catch(function (error) { notify("Stagione non aggiornata: " + error.message, "err"); toggle.disabled = false; });
  });

  // ---- movie detail hero --------------------------------------------------
  var movieHero = document.querySelector("[data-movie-hero]");
  if (movieHero) {
    var movieID = movieHero.getAttribute("data-movie-id") || "";
    if (movieID) {
      api("/api/movies/" + encodeURIComponent(movieID), "GET").then(function (data) {
        renderMovieHero(movieHero, data || {});
      }).catch(function () { /* keep the server-rendered header */ });
    }
  }

  function renderMovieHero(hero, data) {
    var movie = data.movie || {};
    var meta = data.metadata || {};
    var name = String(meta.title || movie.name || hero.getAttribute("data-name") || "");
    var posterPath = String(meta.poster_path || movie.poster_path || "");
    var poster = posterPath && posterPath.indexOf("http") === 0
      ? posterPath
      : (posterPath ? "https://image.tmdb.org/t/p/w300" + posterPath : "");
    setHeroPoster(hero, poster, name);
    var nameSlot = hero.querySelector("[data-hero-name]");
    if (nameSlot && name) nameSlot.textContent = name;
    var yearSlot = hero.querySelector("[data-hero-year]");
    if (yearSlot && movie.year) yearSlot.textContent = String(movie.year);
    var metaSlot = hero.querySelector("[data-hero-meta]");
    if (metaSlot) {
      var badges = [];
      if (meta.release_date) badges.push("uscita " + String(meta.release_date));
      metaSlot.innerHTML = badges.map(function (badge) {
        return '<span class="badge">' + esc(badge) + "</span>";
      }).join(" ");
    }
    var overviewSlot = hero.querySelector("[data-hero-overview]");
    if (overviewSlot && (meta.overview || movie.overview)) overviewSlot.textContent = String(meta.overview || movie.overview);
    var castSlot = hero.querySelector("[data-hero-cast]");
    if (castSlot) castSlot.innerHTML = heroCastHTML(data.cast);
    var linksSlot = hero.querySelector("[data-hero-links]");
    if (linksSlot) {
      var links = "";
      if (movie.tmdb_id) links += '<a class="btn sm" href="https://www.themoviedb.org/movie/' + encodeURIComponent(String(movie.tmdb_id)) + '" target="_blank" rel="noopener">TMDB</a>';
      if (movie.tvdb_id) links += '<a class="btn sm" href="https://thetvdb.com/dereferrer/movie/' + encodeURIComponent(String(movie.tvdb_id)) + '" target="_blank" rel="noopener">TVDB</a>';
      linksSlot.innerHTML = links;
    }
  }

  // ---- add-to-library completion modal (rextto-style) ---------------------
  var addModalOptions = {
    quality: [["", "Qualsiasi"], ["720p", "720p"], ["720p+", "720p+"], ["1080p", "1080p"], ["1080p+", "1080p+"], ["2160p", "2160p 4K"], ["2160p+", "2160p+ 4K+"]],
    language: [["", "Predefinita (ita)"], ["ita", "Italiano"], ["eng", "Inglese"], ["ita,eng", "Italiano + Inglese"], ["multi", "Multi"], ["any", "Qualsiasi"]]
  };

  function addModalField(label, name, value, options, hint, browse) {
    var wrap = document.createElement("label");
    wrap.className = "field";
    if (hint) wrap.setAttribute("title", hint);
    var span = document.createElement("span");
    span.textContent = label;
    var control;
    if (options) {
      control = document.createElement("select");
      control.className = "input";
      options.forEach(function (option) {
        var node = document.createElement("option");
        node.value = option[0];
        node.textContent = option[1];
        if (option[0] === value) node.selected = true;
        control.appendChild(node);
      });
    } else {
      control = document.createElement("input");
      control.className = "input";
      control.value = value || "";
    }
    control.setAttribute("name", name);
    wrap.appendChild(span);
    if (browse && !options) {
      var row = document.createElement("span");
      row.className = "field-row";
      row.appendChild(control);
      var browseButton = document.createElement("button");
      browseButton.className = "btn sm";
      browseButton.type = "button";
      browseButton.textContent = "Sfoglia";
      browseButton.title = "Sfoglia le cartelle e crea una nuova se serve";
      browseButton.addEventListener("click", function () { openBrowseModal(control); });
      row.appendChild(browseButton);
      wrap.appendChild(row);
    } else {
      wrap.appendChild(control);
    }
    return wrap;
  }

  function openAddModal(kind, prefill) {
    prefill = prefill || {};
    var opener = document.activeElement;
    var overlay = document.getElementById("add-overlay");
    if (!overlay) {
      overlay = document.createElement("div");
      overlay.id = "add-overlay";
      overlay.className = "overlay";
      document.body.appendChild(overlay);
    }
    overlay.innerHTML = "";
    overlay.onclick = function (event) { if (event.target === overlay) closeAccessibleDialog(overlay); };

    var modal = document.createElement("div");
    modal.className = "modal";
    modal.setAttribute("role", "dialog");
    modal.setAttribute("aria-modal", "true");
    modal.setAttribute("aria-labelledby", "add-modal-title");
    var head = document.createElement("div");
    head.className = "modal-head";
    var title = document.createElement("h3");
    title.id = "add-modal-title";
    title.textContent = kind === "movie" ? "Aggiungi film" : "Aggiungi serie";
    var close = document.createElement("button");
    close.className = "btn sm";
    close.type = "button";
    close.textContent = "Chiudi";
    close.addEventListener("click", function () { closeAccessibleDialog(overlay); });
    head.appendChild(title);
    head.appendChild(close);
    modal.appendChild(head);

    var body = document.createElement("div");
    body.className = "modal-body";
    var form = document.createElement("div");
    form.className = "form-grid";
    form.appendChild(addModalField("Titolo", "name", prefill.name || "", null, "Titolo come deve comparire nella libreria."));
    form.appendChild(addModalField("Anno", "year", prefill.year || "", null, "Anno di uscita/messa in onda (usato anche per i metadati)."));
    form.appendChild(addModalField("TMDB ID", "tmdb_id", prefill.tmdb_id || "", null, "ID TMDB: permette di recuperare poster e metadati."));
    form.appendChild(addModalField("TVDB ID", "tvdb_id", prefill.tvdb_id || "", null, "ID TVDB alternativo (opzionale)."));
    form.appendChild(addModalField("Qualità richiesta", "quality", "", addModalOptions.quality, "Risoluzione minima accettata. 720p accetta 720p e superiori; 1080p da Full HD in su; 2160p+ 4K+ solo 4K. Il '+' non cambia la soglia (come in rextto)."));
    form.appendChild(addModalField("Lingue", "language", "", addModalOptions.language, "Lingua audio richiesta. Per più lingue scegli Italiano + Inglese (ita,eng)."));
    form.appendChild(addModalField("Sottotitoli", "subtitle", "", null, "Sottotitoli richiesti (es. ita,eng); vuoto = nessun requisito."));
    if (kind !== "movie") {
      form.appendChild(addModalField("Stagioni", "seasons", "1+", null, "Stagioni da monitorare: es. 1-5, 3+ oppure * per tutte."));
      form.appendChild(addModalField("Alias", "aliases", "", null, "Altri nomi con cui possono apparire le release, separati da virgola."));
      form.appendChild(addModalField("Percorso NAS", "archive_path", "", null, "Cartella di destinazione sul NAS dove archiviare i file.", true));
    }
    form.appendChild(addModalField("Esclusioni", "exclude", "", null, "Parole che escludono una release (es. cam, ts)."));

    var actions = document.createElement("div");
    actions.className = "form-actions";
    var confirm = document.createElement("button");
    confirm.className = "btn primary";
    confirm.type = "button";
    confirm.textContent = "Conferma";
    var message = document.createElement("small");
    message.className = "muted";
    message.id = "add-modal-message";
    message.setAttribute("role", "alert");
    message.setAttribute("aria-live", "assertive");
    var titleField = form.querySelector('[name="name"]');
    if (titleField) titleField.setAttribute("aria-describedby", message.id);
    confirm.addEventListener("click", function () {
      var payload = { kind: kind };
      Array.prototype.forEach.call(form.querySelectorAll("[name]"), function (field) {
        payload[field.getAttribute("name")] = field.value;
      });
      if (!String(payload.name || "").trim()) {
        var nameField = form.querySelector('[name="name"]');
        if (nameField) {
          nameField.setAttribute("aria-invalid", "true");
          nameField.focus();
        }
        message.textContent = "Inserisci il titolo";
        return;
      }
      var validName = form.querySelector('[name="name"]');
      if (validName) validName.removeAttribute("aria-invalid");
      confirm.disabled = true;
      api("/api/tmdb/add", "POST", payload).then(function () {
        closeAccessibleDialog(overlay);
        notify("Aggiunto alla libreria", "ok");
        if (partials[view]) { load(); return; }
        var container = page.querySelector("[data-ui-table]");
        if (container && container._refetch) container._refetch();
      }).catch(function (error) { message.textContent = error.message; confirm.disabled = false; });
    });
    actions.appendChild(confirm);
    actions.appendChild(message);
    form.appendChild(actions);
    body.appendChild(form);
    modal.appendChild(body);
    overlay.appendChild(modal);
    openAccessibleDialog(overlay, opener);
    var first = form.querySelector("[name=name]");
    if (first) first.focus();
  }

  // ---- folder browser (NAS path picker with mkdir, rextto-style) ----------
  function openBrowseModal(input) {
    var overlay = document.getElementById("browse-overlay");
    if (!overlay) {
      overlay = document.createElement("div");
      overlay.id = "browse-overlay";
      overlay.className = "overlay";
      overlay.innerHTML =
        '<div class="modal path-modal" role="dialog" aria-modal="true" aria-labelledby="browse-title">' +
        '<div class="modal-head"><h3 id="browse-title">Sfoglia cartelle</h3><button class="btn sm" type="button" data-browse-close>Chiudi</button></div>' +
        '<div class="modal-body">' +
        '<div class="toolbar" style="margin-bottom:10px">' +
        '<button class="btn sm" type="button" data-browse-up title="Vai alla cartella superiore">↑ Su</button>' +
        '<input class="input mono" type="text" data-browse-path-input aria-label="Percorso corrente" title="Percorso corrente: modificalo e premi Invio per navigare" />' +
        '<button class="btn sm primary" type="button" data-browse-select title="Usa questa cartella">Seleziona</button>' +
        '<button class="btn sm" type="button" data-browse-create-prompt title="Crea una nuova cartella dentro quella corrente">Crea cartella</button>' +
        "</div>" +
        '<div class="path-list" data-browse-list></div>' +
        '<div class="toolbar" style="margin-top:10px">' +
        '<input class="input" data-browse-new aria-label="Nome nuova cartella" placeholder="Nuova cartella" title="Nome della nuova cartella da creare nella cartella corrente" />' +
        '<button class="btn sm primary" type="button" data-browse-create title="Crea la cartella e selezionala">Crea e usa</button>' +
        "</div>" +
        '<small class="muted" data-browse-message aria-live="polite"></small>' +
        "</div></div>";
      document.body.appendChild(overlay);
      overlay.addEventListener("click", function (event) {
        if (event.target === overlay || event.target.closest("[data-browse-close]")) closeAccessibleDialog(overlay);
      });
      overlay.addEventListener("keydown", function (event) {
        if (event.key === "Enter" && event.target.matches("[data-browse-path-input]")) {
          event.preventDefault();
          overlay._current = String(event.target.value || "").trim();
          loadBrowseModal(overlay);
        }
      });
    }
    openAccessibleDialog(overlay, input);
    overlay._target = input;
    overlay._current = String(input && input.value || "").trim();
    loadBrowseModal(overlay);
  }

  function loadBrowseModal(overlay) {
    var list = overlay.querySelector("[data-browse-list]");
    var pathInput = overlay.querySelector("[data-browse-path-input]");
    var message = overlay.querySelector("[data-browse-message]");
    if (message) message.textContent = "";
    list.innerHTML = '<p class="muted">Caricamento…</p>';
    api("/api/browse_dir?path=" + encodeURIComponent(overlay._current || ""), "GET").then(function (data) {
      overlay._current = String(data.path || overlay._current || "");
      overlay._parent = data.parent || "";
      if (pathInput) pathInput.value = overlay._current;
      var dirs = data.dirs || [];
      list.innerHTML = "";
      if (!dirs.length) {
        list.innerHTML = '<p class="muted">Nessuna sottocartella.</p>';
        return;
      }
      dirs.forEach(function (dir) {
        var button = document.createElement("button");
        button.className = "path-item";
        button.type = "button";
        button.title = dir;
        var span = document.createElement("span");
        span.className = "mono truncate";
        span.textContent = dir;
        button.appendChild(span);
        button.addEventListener("click", function () { overlay._current = dir; loadBrowseModal(overlay); });
        list.appendChild(button);
      });
    }).catch(function (error) {
      list.innerHTML = '<p class="alert">' + esc(error.message) + "</p>";
    });
  }

  function browseCreate(overlay, name) {
    name = String(name || "").trim();
    var message = overlay.querySelector("[data-browse-message]");
    if (!name) {
      if (message) message.textContent = "Inserisci il nome della cartella";
      return;
    }
    var target = (overlay._current || "/").replace(/\/+$/, "") + "/" + name;
    api("/api/mkdir", "POST", { path: target }).then(function () {
      if (overlay._target) overlay._target.value = target;
      closeAccessibleDialog(overlay);
      notify("Cartella creata: " + target, "ok");
    }).catch(function (error) { if (message) message.textContent = error.message; });
  }

  document.addEventListener("click", function (event) {
    var browseButton = event.target.closest("[data-browse-for]");
    if (browseButton) {
      var scope = browseButton.closest("form") || browseButton.closest(".form-grid");
      var input = scope ? scope.querySelector('[name="' + browseButton.getAttribute("data-browse-for") + '"]') : null;
      if (input) openBrowseModal(input);
      return;
    }
    var overlay = document.getElementById("browse-overlay");
    if (!overlay || overlay.hidden) return;
    if (event.target.closest("[data-browse-up]")) {
      overlay._current = overlay._parent || overlay._current;
      loadBrowseModal(overlay);
      return;
    }
    if (event.target.closest("[data-browse-select]")) {
      if (overlay._target) overlay._target.value = overlay._current;
      closeAccessibleDialog(overlay);
      return;
    }
    if (event.target.closest("[data-browse-create-prompt]")) {
      var entered = window.prompt("Nome nuova cartella:", "");
      if (entered === null) return;
      browseCreate(overlay, entered);
      return;
    }
    if (event.target.closest("[data-browse-create]")) {
      var nameInput = overlay.querySelector("[data-browse-new]");
      browseCreate(overlay, nameInput ? nameInput.value : "");
      return;
    }
  });

  // Open the completion modal from the "Aggiungi manualmente" button, prefilling
  // the title with whatever is typed in the search box.
  document.addEventListener("click", function (event) {
    var manual = event.target.closest("[data-open-add]");
    if (!manual) return;
    var scope = manual.closest("form") || manual.closest(".panel");
    var queryInput = scope ? scope.querySelector('input[name="query"]') : null;
    var name = queryInput ? queryInput.value.trim() : "";
    openAddModal(manual.getAttribute("data-open-add") || "series", { name: name });
  });

  // ---- rename preview modal (series) --------------------------------------
  function renameFileName(path) {
    var parts = String(path || "").split(/[\\/]/);
    return parts[parts.length - 1] || path || "";
  }

  function showRenamePreview(previewURL, executeURL) {
    var overlay = document.getElementById("rename-overlay");
    if (!overlay) {
      overlay = document.createElement("div");
      overlay.id = "rename-overlay";
      overlay.className = "overlay";
      document.body.appendChild(overlay);
      overlay.addEventListener("click", function (event) {
        if (event.target === overlay || event.target.closest("[data-rename-close]")) closeAccessibleDialog(overlay);
      });
    }
    overlay.innerHTML = '<div class="modal" role="dialog" aria-modal="true" aria-labelledby="rename-title">' +
      '<div class="modal-head"><h3 id="rename-title">Anteprima rinomina</h3><button class="btn sm" type="button" data-rename-close>Chiudi</button></div>' +
      '<div class="modal-body" data-rename-body><p class="muted">Analisi in corso…</p></div></div>';
    openAccessibleDialog(overlay, document.activeElement);
    var body = overlay.querySelector("[data-rename-body]");
    api(previewURL, "POST", {}).then(function (data) {
      renderRenamePreview(body, data || {}, executeURL, overlay);
    }).catch(function (error) {
      body.innerHTML = '<p class="alert">' + esc(error.message) + "</p>";
    });
  }

  function renderRenamePreview(body, data, executeURL, overlay) {
    body.innerHTML = "";
    var items = Array.isArray(data.items) ? data.items : [];

    var summary = document.createElement("div");
    summary.className = "series-badges";
    [
      ["Da rinominare", items.length],
      ["Già corretti", Number(data.already_ok_count) || 0],
      ["File presenti", (Number(data.with_path) || 0) + "/" + (Number(data.episodes) || 0)],
      ["Scartati", Number(data.discarded_count) || 0],
      ["Errori", Number(data.error_count) || 0],
    ].forEach(function (pair) {
      var badge = document.createElement("span");
      badge.className = "badge" + (pair[0] === "Errori" && pair[1] > 0 ? " err" : "");
      badge.textContent = pair[0] + ": " + pair[1];
      summary.appendChild(badge);
    });
    body.appendChild(summary);

    if (!items.length) {
      var none = document.createElement("p");
      none.className = "muted";
      none.textContent = "Nessuna rinomina necessaria: i file sono già nel formato corretto.";
      body.appendChild(none);
    } else {
      var table = document.createElement("table");
      table.className = "data-table";
      table.innerHTML = "<thead><tr><th>Ep.</th><th>Vecchio nome</th><th>Nuovo nome / esito</th></tr></thead>";
      accessibleDataTable(table, "Anteprima rinomina");
      var tbody = document.createElement("tbody");
      items.forEach(function (item) {
        var row = document.createElement("tr");
        var ep = document.createElement("td");
        ep.className = "numeric";
        ep.textContent = "S" + String(item.season || "—") + "E" + String(item.episode || "—");
        var fromCell = document.createElement("td");
        fromCell.className = "truncate";
        fromCell.title = String(item.from || "");
        fromCell.textContent = renameFileName(item.from);
        var toCell = document.createElement("td");
        if (item.error) {
          toCell.innerHTML = '<span class="badge err">errore</span> ' + esc(String(item.error));
        } else if (item.discarded) {
          toCell.innerHTML = '<span class="badge warn">scartato</span>';
        } else {
          toCell.className = "truncate";
          toCell.title = String(item.to || "");
          toCell.textContent = renameFileName(item.to);
        }
        row.appendChild(ep);
        row.appendChild(fromCell);
        row.appendChild(toCell);
        tbody.appendChild(row);
      });
      table.appendChild(tbody);
      var wrap = document.createElement("div");
      wrap.className = "table-wrap";
      wrap.appendChild(table);
      body.appendChild(wrap);
    }

    var actions = document.createElement("div");
    actions.className = "form-actions";
    var execute = document.createElement("button");
    execute.className = "btn sm primary";
    execute.textContent = "Esegui rinomina";
    execute.disabled = items.length === 0;
    var force = document.createElement("button");
    force.className = "btn sm danger";
    force.textContent = "Forza rinomina";
    force.title = "Rinomina anche i file già corretti";
    var message = document.createElement("small");
    message.className = "muted";

    var runRename = function (forceFlag) {
      execute.disabled = true;
      force.disabled = true;
      message.textContent = "Rinomina in corso…";
      api(executeURL, "POST", { force: forceFlag }).then(function (result) {
        var renamed = Number(result && result.renamed_count) || 0;
        var discarded = Number(result && result.discarded_count) || 0;
        message.textContent = "Fatto: " + renamed + " rinominati, " + discarded + " scartati";
        notify("Rinomina: " + renamed + " file aggiornati", "ok");
      }).catch(function (error) {
        message.textContent = error.message;
        notify("Rinomina non riuscita: " + error.message, "err");
      }).then(function () {
        execute.disabled = false;
        force.disabled = false;
      });
    };
    execute.addEventListener("click", function () { runRename(false); });
    force.addEventListener("click", function () {
      if (!confirm("Forzare la rinomina? Verranno toccati anche i file già corretti.")) return;
      runRename(true);
    });
    actions.appendChild(execute);
    actions.appendChild(force);
    actions.appendChild(message);
    body.appendChild(actions);
  }

  document.addEventListener("click", function (event) {
    var preview = event.target.closest("[data-rename-preview]");
    if (!preview) return;
    showRenamePreview(preview.getAttribute("data-rename-preview"), preview.getAttribute("data-rename-execute"));
  });

  // ---- language / subtitle helpers ---------------------------------------
  document.addEventListener("change", function (event) {
    var preset = event.target.closest("[data-preset-for]");
    if (!preset || !preset.value) return;
    var scope = preset.closest("form") || preset.closest(".form-grid") || preset.closest(".panel");
    var input = scope ? scope.querySelector('[name="' + preset.getAttribute("data-preset-for") + '"]') : null;
    if (input) input.value = preset.value;
  });

  (function initLanguageRequirements() {
    Array.prototype.forEach.call(document.querySelectorAll("[data-lang-requirements]"), function (box) {
      var rows = Array.prototype.slice.call(box.querySelectorAll(".lang-row"));
      var hidden = box.parentNode ? box.parentNode.querySelector("[data-lang-hidden]") : null;
      var value = String(box.getAttribute("data-value") || "").trim();
      var entries = [];
      if (value.charAt(0) === "[") {
        try {
          JSON.parse(value).forEach(function (entry) {
            if (entry && entry.language) entries.push({ language: String(entry.language), required: entry.required !== false });
          });
        } catch (error) { entries = []; }
      } else if (value) {
        value.split(/[,\+]/).forEach(function (part) {
          if (part.trim()) entries.push({ language: part.trim().toLowerCase(), required: true });
        });
      }
      var sync = function () {
        var out = [];
        rows.forEach(function (row) {
          var select = row.querySelector("[data-lang-select]");
          var check = row.querySelector("[data-lang-required]");
          if (check) {
            check.disabled = !(select && select.value);
            if (!select || !select.value) check.checked = false;
          }
          if (select && select.value) out.push({ language: select.value, required: check ? check.checked : true });
        });
        if (hidden) hidden.value = out.length ? JSON.stringify(out) : "";
      };
      rows.forEach(function (row, index) {
        var select = row.querySelector("[data-lang-select]");
        var check = row.querySelector("[data-lang-required]");
        var entry = entries[index];
        if (entry) {
          if (select) select.value = entry.language;
          if (check) check.checked = entry.required;
        }
        if (select) select.addEventListener("change", sync);
        if (check) check.addEventListener("change", sync);
      });
      sync();
    });
  })();

  // ---- integrations: sources check ----------------------------------------
  document.addEventListener("click", function (event) {
    var check = event.target.closest("[data-sources-check]");
    if (!check) return;
    var button = check;
    var panel = button.closest(".panel");
    var output = panel.querySelector("[data-sources-output]");
    var message = panel.querySelector("[data-sources-message]");
    button.disabled = true;
    if (message) message.textContent = "Verifica in corso…";
    output.innerHTML = '<p class="muted">Attendere…</p>';
    var promise = api("/api/sources/health", "GET");
    promise.then(function (data) {
      if (message) message.textContent = "";
      output.innerHTML = "";
      var items = (data && data.items) || [];
      if (!items.length) {
        output.innerHTML = '<p class="muted">Nessuna sorgente da verificare.</p>';
        return;
      }
        var table = document.createElement("table");
        table.className = "data-table";
        table.innerHTML = "<thead><tr><th>Tipo</th><th>Nome</th><th>Esito</th><th>Risultati</th><th>Dettaglio</th></tr></thead>";
        accessibleDataTable(table, "Stato sorgenti");
        var tbody = document.createElement("tbody");
        items.forEach(function (item) {
          var tr = document.createElement("tr");
          var ok = item.ok !== false;
          var detail = item.error ? String(item.error) : (item.results !== undefined && item.results !== null ? String(item.results) + " risultati" : "");
          tr.innerHTML = "<td>" + esc(String(item.kind || "")) + "</td>" +
            "<td class='truncate'>" + esc(String(item.name || "")) + "</td>" +
            "<td><span class='badge " + (ok ? "ok" : "err") + "'>" + (ok ? "ok" : "errore") + "</span></td>" +
            "<td class='numeric'>" + esc(String(item.results === undefined || item.results === null ? "—" : item.results)) + "</td>" +
            "<td class='muted truncate'>" + esc(detail) + "</td>";
          tbody.appendChild(tr);
        });
        table.appendChild(tbody);
        output.appendChild(table);
    }).catch(function (error) {
      if (message) message.textContent = error.message;
      output.innerHTML = '<p class="alert">' + esc(error.message) + "</p>";
    }).then(function () { button.disabled = false; });
  });

  // Series header actions: Cerca mancanti / Scansiona archivio / Aggiorna da TMDB.
  document.addEventListener("click", function (event) {
    var button = event.target.closest("[data-series-action]");
    if (!button) return;
    var label = button.getAttribute("data-action-label") || "Operazione";
    button.disabled = true;
    api(button.getAttribute("data-series-action"), "POST", {}).then(function (data) {
      if (data && data.metadata_available === false) {
        notify(label + ": metadati episodi non disponibili. Premi prima “Aggiorna da TMDB”.", "err");
        button.disabled = false;
        return;
      }
      var extra = "";
      if (data && data.searched !== undefined) extra = " · " + data.searched + " episodi verificati";
      else if (data && data.updated !== undefined) extra = " · " + data.updated + " file aggiornati";
      else if (data && data.air_dates_updated !== undefined) extra = " · " + data.air_dates_updated + " date aggiornate";
      else if (data && Array.isArray(data.results)) extra = " · " + data.results.length + " risultati";
      notify(label + ": completato" + extra, "ok");
      window.setTimeout(function () { location.reload(); }, 700);
    }).catch(function (error) {
      notify(label + " non riuscito: " + error.message, "err");
      button.disabled = false;
    });
  });

  // ---- maintenance: database optimization --------------------------------
  var dbPanel = document.querySelector("[data-db-optimize]");
  if (dbPanel) {
    var dbFiles = dbPanel.querySelector("[data-db-files]");
    var dbResult = dbPanel.querySelector("[data-db-result]");
    var dbStatus = dbPanel.querySelector("[data-db-status]");
    var loadDB = function () {
      api("/api/db/info", "GET").then(function (data) {
        var files = (data && data.files) || [];
        dbFiles.innerHTML = "";
        if (!files.length) { dbFiles.innerHTML = '<p class="muted">Nessun database.</p>'; return; }
        files.forEach(function (file) {
          var row = document.createElement("div");
          row.className = "list-item";
          var name = document.createElement("span");
          name.className = "mono truncate";
          name.textContent = String(file.name || "");
          name.title = String(file.path || file.name || "");
          var size = document.createElement("strong");
          size.textContent = humanBytes(file.size_bytes || 0);
          row.appendChild(name);
          row.appendChild(size);
          dbFiles.appendChild(row);
        });
      }).catch(function (error) { dbFiles.innerHTML = '<p class="alert">' + esc(error.message) + "</p>"; });
    };
    var runDB = function (action, button) {
      button.disabled = true;
      if (dbStatus) dbStatus.textContent = action.toUpperCase() + " in corso…";
      api("/api/db/action", "POST", { action: action }).then(function (data) {
        if (dbStatus) dbStatus.textContent = "";
        var before = (data && data.before) || {};
        var after = (data && data.after) || {};
        dbResult.hidden = false;
        dbResult.innerHTML = "";
        [["Operazione", String((data && data.action) || action)],
         ["Dimensione prima", humanBytes(before.size_bytes || 0)],
         ["Dimensione dopo", humanBytes(after.size_bytes || 0)],
         ["Spazio liberato", humanBytes(Math.max(0, (before.size_bytes || 0) - (after.size_bytes || 0)))],
         ["Righe prima", String(before.rows || 0)],
         ["Righe dopo", String(after.rows || 0)]].forEach(function (pair) {
          var row = document.createElement("div");
          row.className = "row";
          var label = document.createElement("span");
          label.textContent = pair[0];
          var value = document.createElement("strong");
          value.textContent = pair[1];
          row.appendChild(label);
          row.appendChild(value);
          dbResult.appendChild(row);
        });
        notify(action.toUpperCase() + ": fatto", "ok");
        loadDB();
      }).catch(function (error) {
        if (dbStatus) dbStatus.textContent = error.message;
        notify(action.toUpperCase() + " non riuscito: " + error.message, "err");
      }).then(function () { button.disabled = false; });
    };
    Array.prototype.forEach.call(dbPanel.querySelectorAll("[data-db-action]"), function (button) {
      button.addEventListener("click", function () { runDB(button.getAttribute("data-db-action"), button); });
    });
    var dbRefresh = dbPanel.querySelector("[data-db-refresh]");
    if (dbRefresh) dbRefresh.addEventListener("click", loadDB);
    loadDB();
  }

  // ---- maintenance: trash (button opens a modal with the list) ------------
  var trashPanel = document.querySelector("[data-trash]");
  if (trashPanel) {
    var trashStatus = trashPanel.querySelector("[data-trash-status]");
    var loadTrash = function () {
      api("/api/trash", "GET").then(function (data) {
        if (trashStatus) trashStatus.textContent = (Number(data && data.count) || 0) + " elementi · " + humanBytes((data && data.total_bytes) || 0);
      }).catch(function (error) { if (trashStatus) trashStatus.textContent = error.message; });
    };
    var renderTrashModal = function (overlay) {
      var body = overlay.querySelector("[data-trash-body]");
      var count = overlay.querySelector("[data-trash-count]");
      var message = overlay.querySelector("[data-trash-message]");
      api("/api/trash", "GET").then(function (data) {
        var items = (data && data.items) || [];
        if (count) count.textContent = items.length + " elementi · " + humanBytes((data && data.total_bytes) || 0);
        body.innerHTML = "";
        if (!items.length) { body.innerHTML = '<tr><td class="muted" colspan="3">Cestino vuoto.</td></tr>'; return; }
        items.forEach(function (item) {
          var tr = document.createElement("tr");
          var nameCell = document.createElement("td");
          nameCell.className = "truncate";
          nameCell.title = String(item.path || item.name || "");
          nameCell.textContent = String(item.name || "");
          var sizeCell = document.createElement("td");
          sizeCell.className = "numeric";
          sizeCell.textContent = humanBytes(item.size_bytes || 0);
          var actionCell = document.createElement("td");
          actionCell.className = "row-actions";
          var del = document.createElement("button");
          del.className = "btn sm danger";
          del.textContent = "Elimina";
          del.title = "Elimina definitivamente questo elemento";
          del.addEventListener("click", function () {
            if (!confirm("Eliminare definitivamente " + item.name + "?")) return;
            del.disabled = true;
            api("/api/trash/delete", "POST", { names: [item.name] })
              .then(function () { renderTrashModal(overlay); loadTrash(); })
              .catch(function (error) { if (message) message.textContent = error.message; del.disabled = false; });
          });
          actionCell.appendChild(del);
          tr.appendChild(nameCell);
          tr.appendChild(sizeCell);
          tr.appendChild(actionCell);
          body.appendChild(tr);
        });
      }).catch(function (error) { body.innerHTML = '<tr><td class="alert" colspan="3">' + esc(error.message) + "</td></tr>"; });
    };
    var openTrash = function () {
      var overlay = document.getElementById("trash-overlay");
      if (!overlay) {
        overlay = document.createElement("div");
        overlay.id = "trash-overlay";
        overlay.className = "overlay";
        document.body.appendChild(overlay);
        overlay.addEventListener("click", function (event) {
          if (event.target === overlay || event.target.closest("[data-trash-close]")) closeAccessibleDialog(overlay);
        });
      }
      overlay.innerHTML = '<div class="modal trash-modal" role="dialog" aria-modal="true" aria-labelledby="trash-title">' +
        '<div class="modal-head"><h3 id="trash-title">Cestino</h3><button class="btn sm" type="button" data-trash-close>Chiudi</button></div>' +
        '<div class="modal-body">' +
        '<div class="toolbar"><span class="muted" data-trash-count></span><button class="btn sm danger" type="button" data-trash-all title="Elimina tutti gli elementi">Elimina tutti</button><button class="btn sm" type="button" data-trash-reload title="Ricarica">Aggiorna</button><small class="muted" data-trash-message aria-live="polite"></small></div>' +
        '<div class="table-wrap"><table class="data-table trash-table"><caption class="sr-only">Elementi nel cestino</caption><thead><tr><th scope="col">Nome</th><th scope="col">Dimensione</th><th scope="col">Azioni</th></tr></thead><tbody data-trash-body><tr><td class="muted">Caricamento…</td></tr></tbody></table></div>' +
        '</div></div>';
      openAccessibleDialog(overlay, trashOpenButton);
      renderTrashModal(overlay);
    };
    var trashOpenButton = trashPanel.querySelector("[data-trash-open]");
    if (trashOpenButton) trashOpenButton.addEventListener("click", openTrash);
    var trashEmptyButton = trashPanel.querySelector("[data-trash-empty]");
    if (trashEmptyButton) trashEmptyButton.addEventListener("click", function () {
      if (!confirm("Eliminare tutti gli elementi del cestino?")) return;
      trashEmptyButton.disabled = true;
      api("/api/maintenance/clean-trash", "POST", {})
        .then(function () { notify("Cestino svuotato", "ok"); loadTrash(); })
        .catch(function (error) { notify("Pulizia cestino non riuscita: " + error.message, "err"); })
        .then(function () { trashEmptyButton.disabled = false; });
    });
    var trashRefreshButton = trashPanel.querySelector("[data-trash-refresh]");
    if (trashRefreshButton) trashRefreshButton.addEventListener("click", loadTrash);
    document.addEventListener("click", function (event) {
      var overlay = document.getElementById("trash-overlay");
      if (!overlay || overlay.hidden) return;
      if (event.target.closest("[data-trash-reload]")) { renderTrashModal(overlay); return; }
      if (event.target.closest("[data-trash-all]")) {
        if (!confirm("Eliminare tutti gli elementi del cestino?")) return;
        api("/api/trash", "GET").then(function (data) {
          var names = ((data && data.items) || []).map(function (item) { return item.name; });
          if (!names.length) { notify("Cestino già vuoto", "info"); return null; }
          return api("/api/trash/delete", "POST", { names: names });
        }).then(function (result) {
          if (result === null) return;
          renderTrashModal(overlay);
          loadTrash();
          notify("Elementi eliminati", "ok");
        }).catch(function (error) { notify("Eliminazione non riuscita: " + error.message, "err"); });
        return;
      }
    });
    loadTrash();
  }

  // ---- comics explore results (Download Now / Seleziona) ------------------
  function renderComicsResults(container, items) {
    container.innerHTML = "";
    container.classList.add("comics-results");
    if (!items.length) { container.innerHTML = '<p class="muted">Nessun risultato su GetComics.</p>'; return; }
    var table = document.createElement("table");
    table.className = "data-table";
    table.innerHTML = "<thead><tr><th>Risultato GetComics</th><th>Data</th><th>Azioni</th></tr></thead>";
    accessibleDataTable(table, "Risultati GetComics");
    var tbody = document.createElement("tbody");
    items.forEach(function (item) {
      var row = document.createElement("tr");
      var titleCell = document.createElement("td");
      if (item.url) {
        var link = document.createElement("a");
        link.href = safeHref(item.url);
        link.target = "_blank";
        link.rel = "noopener";
        link.textContent = String(item.title || item.url);
        titleCell.appendChild(link);
      } else {
        titleCell.textContent = String(item.title || "—");
      }
      var dateCell = document.createElement("td");
      dateCell.textContent = String(item.date || "—");
      var actionsCell = document.createElement("td");
      actionsCell.className = "row-actions";
      var download = document.createElement("button");
      download.className = "btn sm primary";
      download.textContent = "Scarica";
      download.title = "Risolvi i link del post e avvia Download Now";
      download.addEventListener("click", function () {
        if (!item.url) { notify("Post senza URL", "err"); return; }
        download.disabled = true;
        api("/api/comics/links", "POST", { url: item.url }).then(function (data) {
          var links = (data && data.links) || {};
          var url = (links.download_now || [])[0] || (links.direct || [])[0];
          if (!url) throw new Error("Download Now non trovato per questo post");
          return api("/api/comics/download", "POST", {
            url: url,
            method: "direct",
            title: "Comic " + String(item.title || "download"),
            post_url: item.url,
            save_path: ""
          });
        }).then(function () {
          download.textContent = "Avviato";
          notify("Download avviato", "ok");
          refreshComicsDownloadTables();
        }).catch(function (error) {
          notify("Download non avviato: " + error.message, "err");
          download.disabled = false;
        });
      });
      var select = document.createElement("button");
      select.className = "btn sm";
      select.textContent = "Seleziona";
      select.title = "Aggiungi questo fumetto alla libreria monitorata";
      select.addEventListener("click", function () { renderComicSelect(container, item); });
      actionsCell.appendChild(download);
      actionsCell.appendChild(select);
      row.appendChild(titleCell);
      row.appendChild(dateCell);
      row.appendChild(actionsCell);
      tbody.appendChild(row);
    });
    table.appendChild(tbody);
    container.appendChild(table);
  }

  function renderComicSelect(container, item) {
    var box = document.createElement("div");
    box.className = "card editor-card";
    var heading = document.createElement("h3");
    heading.textContent = "Fumetto selezionato";
    box.appendChild(heading);
    var name = document.createElement("p");
    name.className = "muted";
    name.textContent = String(item.title || "");
    box.appendChild(name);
    var pathField = document.createElement("label");
    pathField.className = "field";
    var pathLabel = document.createElement("span");
    pathLabel.textContent = "Percorso archivio (opzionale)";
    var pathInput = document.createElement("input");
    pathInput.className = "input";
    pathInput.placeholder = "cartella fumetti predefinita";
    pathField.appendChild(pathLabel);
    pathField.appendChild(pathInput);
    box.appendChild(pathField);
    var actions = document.createElement("div");
    actions.className = "form-actions";
    var add = document.createElement("button");
    add.className = "btn sm primary";
    add.textContent = "Aggiungi fumetto selezionato";
    add.addEventListener("click", function () {
      add.disabled = true;
      api("/api/comics", "POST", {
        title: String(item.title || ""),
        tag_url: String(item.tag_url || item.url || ""),
        post_url: String(item.url || ""),
        cover_url: String(item.cover_url || ""),
        publisher: String(item.publisher || ""),
        description: String(item.description || ""),
        from_date: String(item.date || ""),
        save_path: pathInput.value.trim()
      }).then(function () {
        notify("Fumetto aggiunto", "ok");
        box.remove();
      }).catch(function (error) { notify("Aggiunta non riuscita: " + error.message, "err"); add.disabled = false; });
    });
    var cancel = document.createElement("button");
    cancel.className = "btn sm";
    cancel.textContent = "Annulla";
    cancel.addEventListener("click", function () { box.remove(); });
    actions.appendChild(add);
    actions.appendChild(cancel);
    box.appendChild(actions);
    container.appendChild(box);
    pathInput.focus();
  }
})();
