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
