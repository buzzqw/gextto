import { test, expect } from "@playwright/test";

const nav = (page: import("@playwright/test").Page, label: string) =>
  page.locator("aside .nav-item").filter({ hasText: label }).first();

test("salute e simulatore punteggi sono raggiungibili", async ({ page }) => {
  await page.goto("/");
  await expect(page.locator("h1").filter({ hasText: "Dashboard" })).toBeVisible();

  await nav(page, "Salute").click();
  await expect(page.locator("h3").filter({ hasText: "Percorsi e dischi" })).toBeVisible();
  await expect(page.locator("h3").filter({ hasText: "Stato sorgenti" })).toBeVisible();
  await expect(page.locator("h3").filter({ hasText: "Ultimi errori" })).toBeVisible();

  await nav(page, "Configurazione").click();
  await page.getByRole("button", { name: "Punteggi" }).click();
  await expect(page.locator("h3").filter({ hasText: "Gruppi custom (release group)" })).toBeVisible();
  await expect(page.locator("h3").filter({ hasText: "Simulatore punteggio" })).toBeVisible();
  await expect(page.getByRole("button", { name: "Calcola" })).toBeVisible();
});

test("banner modifiche non salvate in configurazione", async ({ page }) => {
  await page.goto("/");
  await nav(page, "Configurazione").click();
  const input = page.locator("form.settings-row input").first();
  await expect(input).toBeVisible({ timeout: 20000 });
  await input.fill("9999");
  await expect(page.locator(".settings-savebar")).toBeVisible();
  await expect(page.locator(".settings-savebar")).toContainText("modifiche non salvate");
  await page.getByRole("button", { name: "Ignora" }).click();
  await expect(page.locator(".settings-savebar")).toHaveCount(0);
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
  // Il Markdown è convertito in HTML con titoli e indice interno.
  await expect(page.locator(".manual-body h1")).toContainText("Manuale utente");
  await expect(page.locator(".manual-body h2").first()).toContainText("Primo avvio");
  const anchors = await page.locator(".manual-body a[href^='#']").count();
  expect(anchors).toBeGreaterThan(0);
});

test("manutenzione espone la revisione rinomina cartella", async ({ page }) => {
  await page.goto("/");
  await nav(page, "Manutenzione").click();
  await expect(page.locator("h3").filter({ hasText: "Rinomina contenuto cartella" })).toBeVisible();
  await expect(page.locator("[data-folder-rename-path]")).toBeVisible();
  await expect(page.locator("[data-folder-rename-browse]")).toHaveText("Sfoglia");
});

test("ricerca impostazioni apre la tab e raggiunge il campo", async ({ page }) => {
  await page.goto("/");
  await expect(page.getByRole("button", { name: "Cerca impostazioni" })).toBeVisible();
  await page.getByRole("button", { name: "Cerca impostazioni" }).click();
  const search = page.locator(".settings-search-modal input[type='search']");
  await expect(search).toBeVisible();
  await search.fill("proxy");
  const hit = page.locator(".settings-search-hit").filter({ hasText: "Proxy host" }).first();
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
});
