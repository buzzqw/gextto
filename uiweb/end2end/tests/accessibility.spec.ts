import { test, expect } from "@playwright/test";
import AxeBuilder from "@axe-core/playwright";

const scan = async (page: import("@playwright/test").Page, path: string) => {
  await page.goto(path);
  await page.waitForLoadState("domcontentloaded");
  const results = await new AxeBuilder({ page })
    .withTags(["wcag2a", "wcag2aa"])
    .analyze();
  if (results.violations.length) {
    throw new Error(results.violations.map((violation) =>
      `${violation.id}: ${violation.nodes.map((node) => node.html).join(" | ")}`
    ).join("\n"));
  }
};

test.describe("accessibilità automatizzata", () => {
  for (const path of ["/", "/?view=downloads", "/?view=settings", "/?view=maintenance", "/?view=health", "/?view=logs"]) {
    test(`nessuna violazione axe su ${path}`, async ({ page }) => {
      await scan(page, path);
    });
  }

  test("i controlli di ricerca e lingua hanno un nome accessibile", async ({ page }) => {
    await page.goto("/");
    const unnamed = await page.locator("input:not([type=hidden]), select, textarea").evaluateAll((controls) =>
      controls.filter((control) => {
        const labelled = control.getAttribute("aria-label") || control.getAttribute("aria-labelledby");
        return !labelled && !(control as HTMLInputElement).labels?.length;
      }).map((control) => control.outerHTML)
    );
    expect(unnamed).toEqual([]);
  });

  test("la ricerca impostazioni comprende sinonimi comuni", async ({ page }) => {
    await page.goto("/?view=settings");
    const search = page.locator("[data-settings-search]");
    await search.fill("memo");
    await expect(page.locator("[data-settings-results] .settings-result").first()).toBeVisible();
  });

  test("i dialoghi mantengono il focus e lo restituiscono all'apertura", async ({ page }) => {
    await page.goto("/?view=movies");
    const opener = page.getByRole("button", { name: "Aggiungi manualmente" });
    await opener.click();
    const dialog = page.getByRole("dialog");
    await expect(dialog).toBeVisible();
    await expect.poll(() => page.evaluate(() => !!document.activeElement?.closest('[role="dialog"]'))).toBe(true);

    await page.keyboard.press("Tab");
    await expect.poll(() => page.evaluate(() => !!document.activeElement?.closest('[role="dialog"]'))).toBe(true);
    await page.keyboard.press("Escape");
    await expect(dialog).toBeHidden();
    await expect(opener).toBeFocused();
  });

  test("il polling non perde il focus sul controllo del torrent", async ({ page }) => {
    await page.route("**/ui/partial/torrents", async (route) => {
      await route.fulfill({
        contentType: "text/html; charset=utf-8",
        body: `<div class="view" data-torrents-slot>
          <button type="button" data-action="pause" data-hash="focus-test">Pausa</button>
        </div>`,
      });
    });
    await page.goto("/?view=downloads");
    const pause = page.locator('[data-action="pause"][data-hash="focus-test"]');
    await pause.focus();
    await page.waitForTimeout(3500);
    await expect(pause).toBeFocused();
  });

  test("le intestazioni ordinabili funzionano da tastiera e aggiornano aria-sort", async ({ page }) => {
    await page.route("**/api/search/archive", async (route) => {
      await route.fulfill({
        contentType: "application/json",
        body: JSON.stringify({ results: [{ title: "Test release", source: "test", score: 10 }] }),
      });
    });
    await page.route("**/api/search", async (route) => {
      await route.fulfill({
        contentType: "application/json",
        body: JSON.stringify({ results: [{ title: "Test release", source: "test", score: 10 }] }),
      });
    });
    await page.goto("/?view=search");
    const input = page.locator("form[data-ui-search-post] input[type=search]");
    await input.fill("test");
    await input.press("Enter");
    const header = page.locator("th[data-release-sort=title]");
    await expect(header).toBeVisible();
    await header.locator("button").press("Enter");
    await expect(header).toHaveAttribute("aria-sort", "ascending");
    await header.locator("button").press("Enter");
    await expect(header).toHaveAttribute("aria-sort", "descending");
  });

  test("il layout non richiede scorrimento orizzontale a 320px", async ({ page }) => {
    await page.setViewportSize({ width: 320, height: 800 });
    await page.goto("/");
    const overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth);
    expect(overflow).toBeLessThanOrEqual(2);
  });
});
