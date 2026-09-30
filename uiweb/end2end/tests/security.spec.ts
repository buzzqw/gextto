import { test, expect, type Page } from "@playwright/test";

// Payloads that must never become live DOM. Each one targets a different
// rendering context: element injection, attribute breakout, tag closing and a
// javascript: URL. The client renders API results through `innerHTML`, so this
// suite is the regression guard for the escaping rules described in the
// technical review (section "HTML dinamico e sicurezza del rendering").
const ELEMENT_INJECTION = '<img src=x onerror="window.__xss=true">';
const ATTRIBUTE_BREAKOUT = '" onmouseover="window.__xss=true" data-x="';
const TAG_BREAKOUT = "</td><script>window.__xss=true</script><td>";
const JS_URL = "javascript:window.__xss=true";

async function xssFired(page: Page): Promise<boolean> {
  return page.evaluate(() => Boolean((window as unknown as { __xss?: boolean }).__xss));
}

test("i valori dell'API sono resi come testo, non come HTML", async ({ page }) => {
  // Any dialog would only appear if a payload executed; fail loudly if so.
  let dialogs = 0;
  page.on("dialog", (dialog) => {
    dialogs += 1;
    void dialog.dismiss();
  });

  await page.route("**/api/blocklist*", async (route) => {
    await route.fulfill({
      contentType: "application/json",
      body: JSON.stringify({
        items: [
          {
            title: ELEMENT_INJECTION,
            reason: ATTRIBUTE_BREAKOUT,
            kind: TAG_BREAKOUT,
            created_at: "2026-01-01",
          },
          { title: "valore normale", reason: JS_URL, kind: "safe", created_at: "2026-01-02" },
        ],
      }),
    });
  });

  await page.goto("/?view=blocklist");

  const body = page.locator("[data-ui-body]");
  await expect(body.locator("tr").first()).toBeVisible();

  // None of the payloads may materialise as elements or event handlers.
  await expect(body.locator("img")).toHaveCount(0);
  await expect(body.locator("script")).toHaveCount(0);
  await expect(body.locator("[onmouseover]")).toHaveCount(0);
  await expect(body.locator("[onerror]")).toHaveCount(0);

  // The hostile value is displayed verbatim as text in the first cell.
  await expect(body.locator("td").first()).toHaveText(ELEMENT_INJECTION);

  expect(await xssFired(page)).toBe(false);
  expect(dialogs).toBe(0);
});

test("le virgolette non spezzano gli attributi HTML", async ({ page }) => {
  await page.route("**/api/archive*", async (route) => {
    await route.fulfill({
      contentType: "application/json",
      body: JSON.stringify({
        items: [
          {
            title: ATTRIBUTE_BREAKOUT,
            source: ATTRIBUTE_BREAKOUT,
            quality_score: 1,
            added_at: "2026-01-01",
          },
        ],
      }),
    });
  });

  await page.goto("/?view=archive");

  const body = page.locator("[data-ui-body]");
  await expect(body.locator("tr").first()).toBeVisible();

  // The quote-based breakout must not create a new attribute on the cell.
  await expect(body.locator("[onmouseover]")).toHaveCount(0);

  // `truncate` stores the raw value in the title attribute; it must survive
  // round-tripping as data, not as markup.
  const cell = body.locator("span.cell-truncate").first();
  await expect(cell).toHaveAttribute("title", ATTRIBUTE_BREAKOUT);
  await expect(cell).toHaveText(ATTRIBUTE_BREAKOUT);

  expect(await xssFired(page)).toBe(false);
});
