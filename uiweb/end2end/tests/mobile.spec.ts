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
  // narrow screen the episode table fits, the title wraps and the actions of
  // the row stay in view.
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
      const cell = wrap.querySelector("td.truncate") as HTMLElement;
      const search = wrap.querySelector("td.row-actions button") as HTMLElement;
      const sourcesTitle = wrap.querySelector(".sources-table thead .sources-title") as HTMLElement;
      const style = getComputedStyle(cell);
      const result = {
        wrapScrolls: wrap.scrollWidth - wrap.clientWidth,
        whiteSpace: style.whiteSpace,
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
    expect(geometry.searchRight).toBeLessThanOrEqual(geometry.wrapRight + 1);
    // The search opened under the row keeps a readable title and its buttons:
    // the phone widths of this table must not shrink the table inside it.
    expect(geometry.sourcesTitleWidth).toBeGreaterThan(90);
    expect(geometry.sourcesActions).toBeLessThanOrEqual(geometry.wrapRight + 1);
  });
});
