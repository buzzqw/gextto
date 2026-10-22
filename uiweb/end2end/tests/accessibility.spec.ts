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
  for (const path of ["/", "/?view=downloads", "/?view=import", "/?view=bulk", "/?view=settings", "/?view=maintenance", "/?view=maintenance&tab=archive-sql", "/?view=integrations", "/?view=health", "/?view=logs", "/?view=ed2k"]) {
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
    const search = page.locator("input.settings-search");
    await search.fill("memo");
    await expect(page.locator("#v2-settings-search a").first()).toBeVisible();
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
    await page.route("**/downloads/table", async (route) => {
      await route.fulfill({
        contentType: "text/html; charset=utf-8",
        body: `<div class="view" data-torrents-slot>
          <button type="button" data-action="pause" data-hash="focus-test">Pausa</button>
        </div>`,
      });
    });
    await page.clock.install();
    await page.goto("/?view=downloads");
    // The first poll brings the test button into the table; focus it and let
    // the next poll swap the table again. Fast-forwarding avoids two 5 s waits.
    await page.clock.fastForward(5000);
    const pause = page.locator('[data-action="pause"][data-hash="focus-test"]');
    await pause.focus();
    await page.clock.fastForward(5000);
    await expect(pause).toBeFocused();
  });

  test("la ricerca release è utilizzabile da tastiera", async ({ page }) => {
    await page.route("**/search", async (route) => {
      await route.fulfill({
        contentType: "text/html; charset=utf-8",
        // The form swaps inside #v2-search-results, so the response is the
        // content of the container, not a new container.
        body: `<p>Risultati per test</p><table class="data-table"><tbody><tr><td>Test release</td></tr></tbody></table>`,
      });
    });
    await page.goto("/?view=search");
    const input = page.locator("form.search-row input[type=search]");
    await input.fill("test");
    await input.press("Enter");
    await expect(page.locator("#v2-search-results")).toContainText("Test release");
  });

  test("il layout non richiede scorrimento orizzontale a 320px", async ({ page }) => {
    await page.setViewportSize({ width: 320, height: 800 });
    await page.goto("/");
    const overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth);
    expect(overflow).toBeLessThanOrEqual(2);
  });

  test("i riquadri di log scorrevoli sono accessibili da tastiera e hanno un ruolo regione", async ({ page }) => {
    await page.goto("/?view=logs");
    const logView = page.locator("#v2-logs-view");
    await expect(logView).toBeVisible();
    await expect(logView).toHaveAttribute("tabindex", "0");
    await expect(logView).toHaveAttribute("role", "region");
    await logView.focus();
    await expect(logView).toBeFocused();

    await page.goto("/?view=maintenance");
    const gxLogView = page.locator("#v2-gx-log-view");
    await expect(gxLogView).toBeVisible();
    await expect(gxLogView).toHaveAttribute("tabindex", "0");
    await expect(gxLogView).toHaveAttribute("role", "region");
    await gxLogView.focus();
    await expect(gxLogView).toBeFocused();
  });
});
