import { test, expect } from "@playwright/test";

const nav = (page: import("@playwright/test").Page, label: string) =>
  page.locator("aside .nav-item").filter({ hasText: label }).first();

test("salute e configurazione sono raggiungibili", async ({ page }) => {
  await page.goto("/");
  await expect(page.locator("h1").filter({ hasText: "Dashboard" })).toBeVisible();

  await nav(page, "Salute").click();
  await expect(page.locator("h3").filter({ hasText: "Percorsi" })).toBeVisible();
  await expect(page.locator("h3").filter({ hasText: "Dischi" })).toBeVisible();
  await expect(page.locator("h3").filter({ hasText: "Stato sorgenti" })).toBeVisible();

  await nav(page, "Configurazione").click();
  await expect(page.locator("[data-settings-search]")).toBeVisible({ timeout: 20000 });
});

test("banner modifiche non salvate in configurazione", async ({ page }) => {
  await page.goto("/");
  await nav(page, "Configurazione").click();
  const input = page.locator("form.setting-row input[data-setting-input]:not([disabled])").first();
  await expect(input).toBeVisible({ timeout: 20000 });
  await input.fill("9999");
  await expect(page.locator(".settings-savebar")).toBeVisible();
  await expect(page.locator(".settings-savebar")).toContainText("1 modifica non salvata");
  await page.getByRole("button", { name: "Ignora" }).click();
  await expect(page.locator(".settings-savebar")).toBeHidden();
});

test("il menu non ha voci duplicate", async ({ page }) => {
  await page.goto("/");
  // Su desktop il gruppo Sistema è espanso: eventuali voci "mobile" riattivate
  // per errore comparirebbero qui come duplicati.
  const ids: string[] = await page.locator("aside .nav-item:visible").evaluateAll((nodes) =>
    nodes.map((node) => node.getAttribute("data-nav")).filter((id): id is string => Boolean(id))
  );
  const counts = new Map<string, number>();
  for (const id of ids) counts.set(id, (counts.get(id) ?? 0) + 1);
  const duplicates = [...counts.entries()].filter(([, count]) => count > 1).map(([id]) => id);
  expect(duplicates).toEqual([]);
});

test("scarico espone aggiunta torrent e registrazione magnet", async ({ page }) => {
  await page.goto("/");
  await nav(page, "Scarico").click();
  await expect(page.locator("h3").filter({ hasText: "Aggiungi torrent" })).toBeVisible();
  await expect(page.locator("label.check").filter({ hasText: "Scarica subito" })).toBeVisible();
});

test("selezione torrent mantenuta durante il polling", async ({ page }) => {
  await page.route("**/ui/partial/torrents", async (route) => {
    await route.fulfill({
      contentType: "text/html; charset=utf-8",
      body: `<div class="view" data-torrents-slot>
        <input type="checkbox" data-download-select data-hash="selection-test" />
      </div>`,
    });
  });
  await page.goto("/?view=downloads");
  const checkbox = page.locator("[data-download-select]").first();
  await expect(checkbox).toBeVisible();
  await checkbox.check();
  await page.waitForTimeout(3500);
  await expect(checkbox).toBeChecked();
});

test("selettore lingua presente e selezionabile", async ({ page }) => {
  await page.goto("/");
  await expect(page.locator(".lang-select")).toBeVisible();
  await page.locator(".lang-select").selectOption("en");
  await expect(page.locator("aside .nav-item").first()).toBeVisible();
  await page.locator(".lang-select").selectOption("it");
  await expect(page.locator("aside .nav-item").filter({ hasText: "Scarico" }).first()).toBeVisible({ timeout: 15000 });
});

test("manuale utente raggiungibile e renderizzato", async ({ page }) => {
  await page.goto("/");
  await nav(page, "Manuale").click();
  // Il Markdown è convertito in HTML con titoli; il testo segue la lingua attiva.
  await expect(page.locator(".manual-body h1")).toContainText(/Manuale|Manual/i);
  expect(await page.locator(".manual-body h2").count()).toBeGreaterThan(0);
});

test("manutenzione espone la revisione rinomina cartella", async ({ page }) => {
  await page.goto("/");
  await nav(page, "Manutenzione").click();
  await expect(page.locator("h3").filter({ hasText: "Rinomina contenuto cartella" })).toBeVisible();
  await expect(page.locator("[data-folder-rename-path]")).toBeVisible();
  await expect(page.locator("[data-folder-rename-browse]")).toHaveText("Sfoglia");
});

test("ricerca impostazioni apre la tab e raggiunge il campo", async ({ page }) => {
  await page.goto("/?view=settings");
  const search = page.locator("[data-settings-search]");
  await expect(search).toBeVisible({ timeout: 20000 });
  await search.fill("proxy");
  const hit = page.locator("a.settings-result").filter({ hasText: "Proxy host" }).first();
  await expect(hit).toBeVisible();
  await hit.click();
  await expect(page.locator("h1").filter({ hasText: "Configurazione" })).toBeVisible();
  await expect(page.locator("#setting-libtorrent_proxy_host")).toBeVisible();
});

test.describe("mobile", () => {
  test.use({ viewport: { width: 390, height: 844 }, hasTouch: true });

  test("navigazione di base usabile senza overflow", async ({ page }) => {
    await page.goto("/");
    await expect(page.locator("h1")).toBeVisible();
    await nav(page, "Film").click();
    await expect(page.locator("h3").filter({ hasText: "Film monitorati" })).toBeVisible({ timeout: 20000 });
    const overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth);
    expect(overflow).toBeLessThanOrEqual(2);
  });

  test("prestazioni visibili in tile su mobile", async ({ page }) => {
    await page.goto("/");
    const performance = page.locator(".mobile-performance");
    await expect(performance).toBeVisible();
    await expect(performance.locator(".mobile-performance-tile")).toHaveCount(4);
    await expect(performance.locator('[data-metric="cpu"]')).toBeVisible();
    await expect(performance.locator('[data-metric="ram"]')).toBeVisible();
    await expect(performance.locator('[data-metric="dl"]')).toBeVisible();
    await expect(performance.locator('[data-metric="ul"]')).toBeVisible();
  });

  test("mobile mostra salute e log e raccoglie il resto in Sistema", async ({ page }) => {
    await page.goto("/");
    await expect(nav(page, "Salute")).toBeVisible();
    await expect(nav(page, "Log")).toBeVisible();
    await expect(nav(page, "Archivio")).toBeHidden();

    await page.locator("[data-nav-more]").click();
    await expect(nav(page, "Configurazione")).toBeVisible();
    await expect(page.locator(".nav-mobile-system-extra").filter({ hasText: "Esplora" })).toBeVisible();
    await expect(page.locator(".nav-mobile-system-extra").filter({ hasText: "Archivio" })).toBeVisible();
  });
});
