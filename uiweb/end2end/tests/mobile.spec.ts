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

  test("Scarico mostra per intero il titolo di un torrent lungo", async ({ page }) => {
    await page.goto("/?view=downloads");
    await page.waitForTimeout(400);
    // Same markup as the session card: a long name must wrap instead of ending
    // in an ellipsis, because a phone has no tooltip to reveal the rest.
    const geometry = await page.evaluate(() => {
      const table = document.createElement("table");
      table.className = "data-table torrent-table";
      table.innerHTML = `<tbody><tr><td class="torrent-card-cell"><div class="torrent-card">
        <div class="torrent-head"><span class="torrent-select"></span>
        <span class="torrent-name"><button class="torrent-open" type="button"><span class="truncate"></span></button></span>
        <span class="torrent-state badge">In scarico</span></div>
        <div class="torrent-body"></div><div class="row-actions"></div></div></td></tr></tbody>`;
      const span = table.querySelector(".torrent-open .truncate") as HTMLElement;
      span.textContent = "Serie.02x05.Il.Titolo.Della.Puntata.ITA.DLMux.x264-GRUPPO.mkv";
      document.querySelector(".content")?.append(table);
      const style = getComputedStyle(span);
      const lineHeight = parseFloat(style.lineHeight) || parseFloat(style.fontSize) * 1.2;
      const result = {
        whiteSpace: style.whiteSpace,
        clipped: span.scrollWidth - span.clientWidth,
        lines: Math.round(span.getBoundingClientRect().height / lineHeight),
      };
      table.remove();
      return result;
    });
    expect(geometry.whiteSpace).toBe("normal");
    expect(geometry.clipped).toBeLessThanOrEqual(1);
    expect(geometry.lines).toBeGreaterThan(1);
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
    expect(geometry.lines).toBeGreaterThanOrEqual(3);
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
});
