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
});
