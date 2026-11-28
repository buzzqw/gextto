import { test, expect } from "@playwright/test";

// Phone layout: every page must stay inside the screen (no sideways scrolling
// of the whole document, which also means nothing is clipped out of reach) and
// the pages that put a form above the list must show the list first.
test.describe("telefono", () => {
  test.use({ viewport: { width: 390, height: 844 }, isMobile: true, hasTouch: true });

  const views = [
    "dashboard",
    "downloads",
    "logs",
    "series",
    "movies",
    "search",
    "archive",
    "comics",
    "ed2k",
    "settings",
    "integrations",
    "maintenance",
    "health",
  ];

  for (const view of views) {
    test(`${view} non scorre di lato`, async ({ page }) => {
      await page.goto(`/?view=${view}`);
      await page.waitForTimeout(500);
      const overflow = await page.evaluate(
        () => document.documentElement.scrollWidth - window.innerWidth,
      );
      expect(overflow).toBeLessThanOrEqual(1);
    });
  }

  test("Scarico: il titolo lungo si ferma a due righe, con i pulsanti accanto alla barra", async ({ page }) => {
    await page.goto("/?view=downloads");
    await page.waitForTimeout(400);
    // A downloading card is two or three lines, not a tall stack of text: the
    // name stops at two lines with an ellipsis (the whole title opens with the
    // detail, one tap on the name) and the buttons sit beside the progress bar
    // instead of taking a row of their own.
    const geometry = await page.evaluate(() => {
      const table = document.createElement("table");
      table.className = "data-table torrent-table";
      table.innerHTML = `<tbody><tr><td class="torrent-card-cell"><div class="torrent-card">
        <div class="torrent-head"><span class="torrent-select"></span>
        <span class="torrent-name"><button class="torrent-open" type="button"><span class="truncate"></span></button>
        <span class="badge">eD2k</span></span>
        <span class="torrent-state badge">In coda</span></div>
        <div class="torrent-body">
          <div class="torrent-progress"><div class="progress"><span style="--pct:42%"></span></div><small class="muted">42% · 0 B / 1.5 GB</small></div>
          <div class="torrent-metrics"><span class="torrent-metric">↓ 0 B/s</span><span class="torrent-metric">↑ 0 B</span></div>
        </div>
        <div class="row-actions"><button class="btn sm icon-action" type="button">▶</button><details class="v2-menu" data-v2-menu><summary class="btn sm icon-action">⋯</summary><div class="v2-menu-panel v2-menu-end"></div></details></div>
        </div></td></tr></tbody>`;
      document.querySelector(".content")?.append(table);
      const card = table.querySelector(".torrent-card") as HTMLElement;
      const span = table.querySelector(".torrent-open .truncate") as HTMLElement;
      span.textContent = "Serie.02x05.Il.Titolo.Della.Puntata.ITA.DLMux.x264-GRUPPO.mkv";
      const style = getComputedStyle(span);
      const lineHeight = parseFloat(style.lineHeight) || parseFloat(style.fontSize) * 1.2;
      const name = span.getBoundingClientRect();
      const progress = (table.querySelector(".torrent-progress") as HTMLElement).getBoundingClientRect();
      const actions = (table.querySelector(".row-actions") as HTMLElement).getBoundingClientRect();
      const metrics = (table.querySelector(".torrent-metrics") as HTMLElement).getBoundingClientRect();
      const result = {
        clamp: style.webkitLineClamp,
        lines: Math.round(name.height / lineHeight),
        actionsBesideProgress: actions.top >= progress.top - 1 && actions.top < progress.top + 12 && actions.right > progress.right,
        metricsBelow: metrics.top >= Math.max(progress.bottom, actions.bottom) - 1,
        cardHeight: Math.round(card.getBoundingClientRect().height),
        nameHeight: Math.round(name.height),
      };
      table.remove();
      return result;
    });
    expect(geometry.clamp).toBe("2");
    expect(geometry.lines).toBe(2);
    expect(geometry.actionsBesideProgress).toBe(true);
    expect(geometry.metricsBelow).toBe(true);
    // The card is the two lines of the name, the row of progress and buttons,
    // and one line of numbers: not a line for each of them.
    expect(geometry.cardHeight).toBeLessThan(geometry.nameHeight + 120);
  });

  test("una riga in seed mette i numeri sotto il nome, non di fianco", async ({ page }) => {
    await page.goto("/?view=downloads");
    await page.waitForTimeout(400);
    // A settled row is one line on a desktop and two on a phone: the size and
    // the numbers that still move take their own line under the name instead
    // of squeezing it, so a long name still wraps and nothing runs off screen.
    const geometry = await page.evaluate(() => {
      const table = document.createElement("table");
      table.className = "data-table torrent-table";
      table.innerHTML = `<tbody><tr><td class="torrent-card-cell"><div class="torrent-card is-settled">
        <div class="torrent-head"><span class="torrent-select"><input type="checkbox"></span>
        <span class="torrent-name"><button class="torrent-open" type="button"><span class="truncate"></span></button>
        <span class="badge warn">seed ∞</span>
        <span class="torrent-facts"><span class="torrent-fact">1.4 GB</span><span class="torrent-fact">↑ 0 B/s</span><span class="torrent-fact">👥 0 / 0</span><span class="torrent-fact">🌐 1 / 0</span><span class="torrent-fact">⇅ 1.00</span></span>
        </span><span class="torrent-state badge ok">In seed</span></div>
        <div class="row-actions"></div></div></td></tr></tbody>`;
      const name = table.querySelector(".torrent-open .truncate") as HTMLElement;
      name.textContent = "Serie.02x05.Il.Titolo.Della.Puntata.ITA.DLMux.x264-GRUPPO.mkv";
      document.querySelector(".content")?.append(table);
      const card = table.querySelector(".torrent-card") as HTMLElement;
      const button = table.querySelector(".torrent-open") as HTMLElement;
      const facts = table.querySelector(".torrent-facts") as HTMLElement;
      const result = {
        overflow: card.scrollWidth - card.clientWidth,
        factsBelowName: facts.getBoundingClientRect().top >= button.getBoundingClientRect().bottom - 1,
        nameShare: button.getBoundingClientRect().width / card.getBoundingClientRect().width,
      };
      table.remove();
      return result;
    });
    expect(geometry.overflow).toBeLessThanOrEqual(1);
    expect(geometry.factsBelowName).toBe(true);
    expect(geometry.nameShare).toBeGreaterThan(0.6);
  });

  test("Serie TV e Film mostrano l'elenco prima del modulo di aggiunta", async ({ page }) => {
    for (const [view, panel] of [
      ["series", "#v2-table-panel-series-t0"],
      ["movies", "#v2-table-panel-movies-t0"],
    ]) {
      await page.goto(`/?view=${view}`);
      await page.waitForTimeout(400);
      const table = await page.locator(panel).boundingBox();
      const form = await page.locator('form[hx-post="/section/form"] input[name="query"]').boundingBox();
      if (!table || !form) throw new Error(`${view}: geometria non disponibile`);
      expect(table.y).toBeLessThan(form.y);
    }
  });

  test("Esplora apre con le ricerche, non col calendario", async ({ page }) => {
    await page.goto("/?view=search");
    await page.waitForTimeout(400);
    const searchPanel = await page.locator('form[hx-post="/tmdb/search"]').boundingBox();
    const calendarPanel = await page.locator('[hx-get="/tmdb/calendar"]').boundingBox();
    if (!searchPanel || !calendarPanel) throw new Error("geometria non disponibile");
    expect(searchPanel.y).toBeLessThan(calendarPanel.y);
  });

  // A phone has no tooltip to reveal a truncated name, and a sideways scroll
  // inside the table hides the button that sits at the end of every row: on a
  // narrow screen an episode is a block, the name of the file takes the whole
  // width and the actions of the row stay in view.
  test("gli episodi di una serie mostrano le azioni anche col titolo lungo", async ({ page }) => {
    await page.goto("/?view=series");
    await page.waitForTimeout(300);
    const geometry = await page.evaluate(() => {
      const wrap = document.createElement("div");
      wrap.className = "table-wrap";
      wrap.innerHTML = `<table class="data-table series-episodes"><thead><tr>
        <th scope="col">S</th><th scope="col">E</th><th scope="col">Titolo</th>
        <th scope="col">Stato</th><th scope="col">Qualità</th><th scope="col">Dimensione</th>
        <th scope="col">Azioni</th></tr></thead><tbody><tr>
        <td class="numeric">3</td><td class="numeric">2</td>
        <td class="truncate">Serie - S03E02 - Il Titolo Della Puntata - [WEB-DL][1080p][h264][Ddp 5.1][IT+EN]</td>
        <td>archiviato</td><td class="numeric">90</td><td class="numeric">4.3 GB</td>
        <td class="row-actions"><button class="btn sm icon-action primary" type="button">cerca</button><button class="btn sm icon-action" type="button">forza</button><details class="v2-menu"><summary class="btn sm icon-action">⋯</summary></details></td>
        </tr><tr class="v2-ep-search-row"><td colspan="7"><section class="v2-ep-search">
        <div class="v2-ep-search-body"><div class="table-wrap"><table class="data-table sources-table">
        <thead><tr><th class="sources-title">Titolo</th><th class="sources-source">Sorgente</th><th class="sources-score">Punteggio</th><th class="sources-size">Dimensione</th><th class="sources-actions">Azioni</th></tr></thead>
        <tbody><tr><td class="sources-title">Serie - S03E02 Il Dono degli Uomini 1080p H264 ITA ENG AAC SUB ITA NUENG.mkv</td>
        <td class="sources-source">bt4g</td><td class="numeric sources-score">1090</td><td class="numeric sources-size">—</td>
        <td class="row-actions sources-actions"><button class="btn sm" type="button">Copia magnet</button><button class="btn sm primary" type="button">Scarica</button></td></tr></tbody>
        </table></div></div></section></td></tr></tbody></table>`;
      document.querySelector(".content")?.append(wrap);
      const row = wrap.querySelector("tbody > tr") as HTMLElement;
      const cell = wrap.querySelector("td.truncate") as HTMLElement;
      const status = row.children[3] as HTMLElement;
      const search = wrap.querySelector("td.row-actions button") as HTMLElement;
      const sourcesTitle = wrap.querySelector(".sources-table thead .sources-title") as HTMLElement;
      const style = getComputedStyle(cell);
      const lineHeight = parseFloat(style.lineHeight) || parseFloat(style.fontSize) * 1.3;
      const result = {
        wrapScrolls: wrap.scrollWidth - wrap.clientWidth,
        whiteSpace: style.whiteSpace,
        wrapWidth: wrap.getBoundingClientRect().width,
        titleWidth: cell.getBoundingClientRect().width,
        titleClipped: cell.scrollWidth - cell.clientWidth,
        rowHeight: row.getBoundingClientRect().height,
        statusWidth: status.getBoundingClientRect().width,
        statusLines: Math.round(status.getBoundingClientRect().height / lineHeight),
        searchRight: search.getBoundingClientRect().right,
        wrapRight: wrap.getBoundingClientRect().right,
        sourcesTitleWidth: sourcesTitle.getBoundingClientRect().width,
        sourcesActions: (wrap.querySelector(".sources-table .sources-actions") as HTMLElement).getBoundingClientRect().right,
      };
      wrap.remove();
      return result;
    });
    expect(geometry.wrapScrolls).toBeLessThanOrEqual(1);
    expect(geometry.whiteSpace).toBe("normal");
    // The name of the file is the reason the row exists: it gets the width of
    // the block, not a 27% column where it fell one word per line.
    expect(geometry.titleWidth).toBeGreaterThanOrEqual(geometry.wrapWidth - 24);
    expect(geometry.titleClipped).toBeLessThanOrEqual(1);
    expect(geometry.searchRight).toBeLessThanOrEqual(geometry.wrapRight + 1);
    // A block, not a tower: one release name used to make the row 400+ tall.
    expect(geometry.rowHeight).toBeLessThan(260);
    expect(geometry.statusLines).toBeLessThanOrEqual(2);
    expect(geometry.statusWidth).toBeGreaterThan(60);
    // The search opened under the row keeps a readable title and its buttons:
    // the phone widths of this table must not shrink the table inside it.
    expect(geometry.sourcesTitleWidth).toBeGreaterThan(90);
    expect(geometry.sourcesActions).toBeLessThanOrEqual(geometry.wrapRight + 1);
  });

  // The story of a download: one block per event, not a four-column table
  // thousands of pixels wide with a message column one character across.
  test("la storia dei download si legge a blocchi", async ({ page }) => {
    await page.goto("/?view=series");
    await page.waitForTimeout(300);
    const geometry = await page.evaluate(() => {
      const wrap = document.createElement("div");
      wrap.className = "table-wrap";
      wrap.innerHTML = `<table class="data-table acquisition-events"><thead><tr>
        <th scope="col">Quando</th><th scope="col">Download</th><th scope="col">Evento</th><th scope="col">Dettagli</th>
        </tr></thead><tbody><tr class="acq-info">
        <td class="nowrap acq-when">2026-11-19 09:27:27<br /><small class="muted"><span>dal</span> 2026-11-19 09:20:11</small></td>
        <td class="truncate acq-what"><code>2aa9b9</code> S03E04</td>
        <td class="acq-event">Scaricato: il file è stato spostato in libreria dopo la verifica dell'integrità e la rinomina</td>
        <td class="truncate acq-details">acq: 2aa9b9 · downloaded bytes: 4294967296 · destination: /home/andres/SerieTVAchivio/Serie/Stagione 3/episodio.mkv</td>
        </tr></tbody></table>`;
      document.querySelector(".content")?.append(wrap);
      const row = wrap.querySelector("tbody > tr") as HTMLElement;
      const what = wrap.querySelector("td.acq-what") as HTMLElement;
      const event = wrap.querySelector("td.acq-event") as HTMLElement;
      const details = wrap.querySelector("td.acq-details") as HTMLElement;
      const result = {
        wrapScrolls: wrap.scrollWidth - wrap.clientWidth,
        rowHeight: row.getBoundingClientRect().height,
        eventWidth: event.getBoundingClientRect().width,
        eventWhiteSpace: getComputedStyle(event).whiteSpace,
        detailsWhiteSpace: getComputedStyle(details).whiteSpace,
        detailsClipped: details.scrollWidth - details.clientWidth,
        whatRight: what.getBoundingClientRect().right,
        wrapRight: wrap.getBoundingClientRect().right,
      };
      wrap.remove();
      return result;
    });
    expect(geometry.wrapScrolls).toBeLessThanOrEqual(1);
    expect(geometry.eventWidth).toBeGreaterThan(200);
    expect(geometry.eventWhiteSpace).toBe("normal");
    expect(geometry.detailsWhiteSpace).toBe("normal");
    expect(geometry.detailsClipped).toBeLessThanOrEqual(1);
    expect(geometry.whatRight).toBeLessThanOrEqual(geometry.wrapRight + 1);
    expect(geometry.rowHeight).toBeLessThan(260);
  });

  // The detail of one series has no list panel: the rule that puts the list
  // before the "add" form must not drag the toolbar (search the missing
  // episodes, scan the archive, …) under every panel of the page.
  test("il dettaglio di una serie tiene le azioni in cima", async ({ page }) => {
    await page.goto("/?view=series");
    await page.waitForTimeout(300);
    const order = await page.evaluate(() => {
      const panel = document.getElementById("v2-table-panel-series-t0");
      panel?.remove();
      const pageRoot = document.getElementById("v2-page") as HTMLElement;
      const view = document.createElement("div");
      view.className = "view";
      view.innerHTML = `<div class="toolbar"><button class="btn sm">Cerca mancanti</button></div>
        <div class="panel"><div class="panel-body">Episodi</div></div>`;
      pageRoot.append(view);
      const toolbar = view.querySelector(".toolbar") as HTMLElement;
      const body = view.querySelector(".panel") as HTMLElement;
      const result = { toolbarY: toolbar.getBoundingClientRect().top, panelY: body.getBoundingClientRect().top };
      view.remove();
      return result;
    });
    expect(order.toolbarY).toBeLessThan(order.panelY);
  });

  // A result of TMDB carries a long name: its card must fit the screen and the
  // name must wrap, because the page clips whatever goes past the right edge.
  test("i risultati TMDB stanno dentro lo schermo", async ({ page }) => {
    await page.goto("/?view=series");
    await page.waitForTimeout(300);
    const geometry = await page.evaluate(() => {
      const content = document.querySelector(".content") as HTMLElement;
      const grid = document.createElement("div");
      grid.className = "tmdb-grid";
      grid.innerHTML = `<div class="tmdb-card"><div class="tmdb-poster placeholder">N/D</div>
        <div class="tmdb-card-body"><a class="tmdb-result-title truncate" href="#">Il Signore degli Anelli: Gli Anelli del Potere — La serie completa</a>
        <small>TMDB 84773 · 2022 · ★ 7.2</small><p class="tmdb-overview">Migliaia di anni prima degli eventi raccontati.</p>
        <button class="btn sm primary" type="button">Aggiungi alla libreria</button></div></div>`;
      content.append(grid);
      const card = grid.querySelector(".tmdb-card") as HTMLElement;
      const title = grid.querySelector(".tmdb-result-title") as HTMLElement;
      const result = {
        contentWidth: content.getBoundingClientRect().width,
        cardWidth: card.getBoundingClientRect().width,
        cardRight: card.getBoundingClientRect().right,
        contentRight: content.getBoundingClientRect().right,
        titleWhiteSpace: getComputedStyle(title).whiteSpace,
        titleClipped: title.scrollWidth - title.clientWidth,
        titleLines: Math.round(title.getBoundingClientRect().height / (parseFloat(getComputedStyle(title).lineHeight) || 18)),
      };
      grid.remove();
      return result;
    });
    expect(geometry.cardWidth).toBeLessThanOrEqual(geometry.contentWidth + 1);
    expect(geometry.cardRight).toBeLessThanOrEqual(geometry.contentRight + 1);
    expect(geometry.titleWhiteSpace).toBe("normal");
    expect(geometry.titleClipped).toBeLessThanOrEqual(1);
    expect(geometry.titleLines).toBeGreaterThan(1);
  });

  // A poster carries the name of the series; on a phone nothing reveals the
  // part cut after two lines, so the title wraps instead of ending in "…".
  test("il titolo di una locandina non viene tagliato", async ({ page }) => {
    await page.goto("/?view=series&layout=cards");
    await page.waitForTimeout(300);
    const geometry = await page.evaluate(() => {
      const wall = document.createElement("div");
      wall.className = "library-cards";
      wall.innerHTML = `<article class="library-card"><a class="library-card-poster placeholder" href="#">▤</a>
        <div class="library-card-body"><strong class="library-card-title"><a href="#">Il Signore degli Anelli Gli Anelli del Potere</a></strong>
        <div class="card-meta"><span class="card-meta-item">1+ · 6/8 · 75%</span></div></div></article>`;
      document.querySelector(".content")?.append(wall);
      const link = wall.querySelector(".library-card-title a") as HTMLElement;
      const style = getComputedStyle(link);
      const result = {
        clipped: link.scrollHeight - link.clientHeight,
        lines: Math.round(link.getBoundingClientRect().height / (parseFloat(style.lineHeight) || 18)),
        text: link.textContent?.length || 0,
      };
      wall.remove();
      return result;
    });
    expect(geometry.clipped).toBeLessThanOrEqual(1);
    // It wraps instead of stopping on one line; how many lines depends on the
    // width of the font (Geist needs two at this width).
    expect(geometry.lines).toBeGreaterThanOrEqual(2);
  });

  // A warning of the health page is a whole error line: on a phone it wraps
  // instead of stopping at the edge of the panel.
  test("un avviso lungo si legge per intero", async ({ page }) => {
    await page.goto("/?view=health");
    await page.waitForTimeout(300);
    const geometry = await page.evaluate(() => {
      const list = document.createElement("ul");
      list.className = "plain-list";
      list.innerHTML = `<li class="truncate">2026-11-19 12:07:02: Failed to open file (.nfs00000000134d06c900000004) while reading the library of the archive</li>`;
      document.querySelector(".content")?.append(list);
      const item = list.querySelector("li") as HTMLElement;
      const style = getComputedStyle(item);
      const result = { whiteSpace: style.whiteSpace, clipped: item.scrollWidth - item.clientWidth, height: item.getBoundingClientRect().height };
      list.remove();
      return result;
    });
    expect(geometry.whiteSpace).toBe("normal");
    expect(geometry.clipped).toBeLessThanOrEqual(1);
    expect(geometry.height).toBeGreaterThan(20);
  });

  // A release name is one long token ("Serie.02x05.Il.Titolo..."): in the
  // "Scarico recente" table it wraps in its cell instead of being cut in the
  // middle of the name with no way to read the rest.
  test("il nome di una release recente va a capo nella cella", async ({ page }) => {
    await page.goto("/?view=dashboard");
    await page.waitForTimeout(300);
    const geometry = await page.evaluate(() => {
      const wrap = document.createElement("div");
      wrap.className = "table-wrap";
      const table = document.createElement("table");
      table.className = "data-table dashboard-recent-table";
      table.innerHTML = `<thead><tr><th>Nome</th><th>Tipo</th><th>Punteggio</th><th>Stato</th><th>Destinazione</th><th>Concluso</th></tr></thead>
        <tbody><tr><td class="truncate">Serie.02x05.Il.Titolo.Della.Puntata.ITA.DLMux.x264-GRUPPO.mkv</td><td>Film</td><td>1364</td><td>✓</td><td>—</td><td>ieri</td></tr></tbody>`;
      wrap.append(table);
      document.querySelector(".content")?.append(wrap);
      const cell = table.querySelector("td.truncate") as HTMLElement;
      const result = { clipped: cell.scrollWidth - cell.clientWidth, wrap: getComputedStyle(cell).overflowWrap };
      wrap.remove();
      return result;
    });
    expect(geometry.wrap).toBe("anywhere");
    expect(geometry.clipped).toBeLessThanOrEqual(1);
  });

  // The default of a setting can be a URL, one unbreakable token: it wraps
  // instead of running past the edge of the screen.
  test("il predefinito lungo di un'impostazione resta nello schermo", async ({ page }) => {
    await page.goto("/?view=settings&tab=network");
    const line = page.locator("#v2-setting-torrent_tracker_list_url .setting-default");
    await expect(line).toBeVisible({ timeout: 20000 });
    const box = await line.boundingBox();
    expect(box).not.toBeNull();
    expect(box!.x + box!.width).toBeLessThanOrEqual(391);
  });

  // A name shown through a .truncate span sits inline in a paragraph: the span
  // must really truncate (an inline box cannot clip), not run past the edge.
  test("uno span truncate truncia davvero, invece di uscire dal riquadro", async ({ page }) => {
    await page.goto("/?view=health");
    await page.waitForTimeout(300);
    const geometry = await page.evaluate(() => {
      const p = document.createElement("p");
      p.className = "muted";
      p.innerHTML = `<span>Ultimo controllo non superato:</span> <span class="truncate"></span>`;
      const span = p.querySelector(".truncate") as HTMLElement;
      span.textContent = "Serie 2x05 Il Titolo Della Puntata [WEB-DL 1080p] - copia in biblioteca non corrispondente.mkv";
      document.querySelector(".content")?.append(p);
      const result = {
        ellipsis: getComputedStyle(span).textOverflow,
        right: Math.round(span.getBoundingClientRect().right),
        vw: window.innerWidth,
      };
      p.remove();
      return result;
    });
    expect(geometry.ellipsis).toBe("ellipsis");
    expect(geometry.right).toBeLessThanOrEqual(geometry.vw + 1);
  });

  // An alert can hold an address (a feed URL): one token, which wraps instead
  // of opening the panel that carries it.
  test("un indirizzo dentro un avviso va a capo", async ({ page }) => {
    await page.goto("/?view=health");
    await page.waitForTimeout(300);
    const geometry = await page.evaluate(() => {
      const alert = document.createElement("div");
      alert.className = "alert";
      alert.innerHTML = `<ul><li><strong>torrentgalaxy.one/get-posts/user:MIRCrew</strong></li></ul>`;
      document.querySelector(".content")?.append(alert);
      const item = alert.querySelector("li") as HTMLElement;
      const result = { clipped: item.scrollWidth - item.clientWidth, wrap: getComputedStyle(item).overflowWrap };
      alert.remove();
      return result;
    });
    expect(geometry.wrap).toBe("anywhere");
    expect(geometry.clipped).toBeLessThanOrEqual(1);
  });

  // On a 320 px phone the log toolbar stacks: the two selects side by side
  // asked for more width than the screen has.
  test("a 320 pixel la barra del log sta dentro lo schermo", async ({ page }) => {
    await page.setViewportSize({ width: 320, height: 700 });
    await page.goto("/?view=logs");
    await page.waitForTimeout(400);
    const geometry = await page.evaluate(() => {
      const toolbar = document.querySelector(".logs-toolbar") as HTMLElement;
      return { sw: toolbar.scrollWidth, cw: toolbar.clientWidth };
    });
    expect(geometry.sw).toBeLessThanOrEqual(geometry.cw + 1);
  });

  // The mount path of a disk is one token: it wraps instead of pushing the
  // free-space figure past the edge of a narrow screen.
  test("i dischi di Salute tengono lo spazio libero dentro lo schermo", async ({ page }) => {
    await page.setViewportSize({ width: 320, height: 700 });
    await page.goto("/?view=health");
    await page.waitForTimeout(400);
    const worst = await page.evaluate(() => {
      let max = 0;
      for (const row of document.querySelectorAll(".panel-body .row")) {
        const el = row as HTMLElement;
        max = Math.max(max, el.scrollWidth - el.clientWidth);
      }
      return max;
    });
    expect(worst).toBeLessThanOrEqual(1);
  });
});
