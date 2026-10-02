import { test, expect } from "@playwright/test";

const navViews: Record<string, string> = {
  "Salute": "health",
  "Configurazione": "settings",
  "Scarico": "downloads",
  "Film": "movies",
  "Archivio": "archive",
  "Manutenzione": "maintenance",
  "Manuale": "manual",
  "Log": "logs",
};
const nav = (page: import("@playwright/test").Page, label: string) =>
  page.locator(`aside .nav-item:not(.nav-search):not(.nav-mobile-system-extra)[href="/?view=${navViews[label]}"]`);

test("salute e configurazione sono raggiungibili", async ({ page }) => {
  await page.goto("/");
  await expect(page.locator("h1").filter({ hasText: "Dashboard" })).toBeVisible();

  await nav(page, "Salute").click();
  await expect(page.locator("h3").filter({ hasText: "Percorsi" })).toBeVisible();
  await expect(page.locator("h3").filter({ hasText: "Dischi" })).toBeVisible();
  await expect(page.locator("h3").filter({ hasText: "Stato sorgenti" })).toBeVisible();

  await nav(page, "Configurazione").click();
  await expect(page.locator("input.settings-search")).toBeVisible({ timeout: 20000 });
});

test("una singola impostazione può essere salvata", async ({ page }) => {
  await page.route("**/v2/settings/save", async (route) => {
    const key = new URL(route.request().url()).searchParams.get("key") || "refresh_interval";
    await route.fulfill({
      contentType: "text/html; charset=utf-8",
      body: `<form class="setting-row" data-setting-key="${key}"><small class="setting-status"><span class="htx-status ok">Salvato</span></small></form>`,
    });
  });
  await page.goto("/");
  await nav(page, "Configurazione").click();
  const form = page.locator("form.setting-row").filter({ has: page.locator('input[name="value"]') }).first();
  const input = form.locator('input[name="value"]');
  await expect(input).toBeVisible({ timeout: 20000 });
  await input.fill("17");
  await form.getByRole("button", { name: "Salva" }).click();
  await expect(page.locator('[data-setting-key] .htx-status')).toHaveText("Salvato");
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

test("la selezione torrent aggiorna il riepilogo delle azioni", async ({ page }) => {
  await page.route("**/v2/downloads/table", async (route) => {
    await route.fulfill({
      contentType: "text/html; charset=utf-8",
      body: `<div id="v2-torrents-wrap"><form id="v2-torrent-form">
        <span data-v2-selected-count>0 selezionati · Azioni:</span>
        <input type="checkbox" data-v2-select value="selection-test" aria-label="Seleziona test" />
      </form></div>
      <div class="view" data-torrents-slot>
      </div>`,
    });
  });
  await page.goto("/?view=downloads");
  const checkbox = page.locator("[data-v2-select]").first();
  if (!(await checkbox.isVisible())) {
    await page.locator("#v2-torrent-form button[type=submit]").first().click();
  }
  await expect(checkbox).toBeVisible();
  await checkbox.check();
  await expect(page.locator("[data-v2-selected-count]")).toContainText("1 selezionati");
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
  await expect(page.getByRole("textbox", { name: "Cartella", exact: true })).toBeVisible();
  await expect(page.getByRole("button", { name: "Scansiona e proponi" })).toBeVisible();
});

test("ricerca impostazioni apre la tab e raggiunge il campo", async ({ page }) => {
  await page.goto("/?view=settings");
  const search = page.locator("input.settings-search");
  await expect(search).toBeVisible({ timeout: 20000 });
  await search.fill("ram disk");
  const hit = page.locator("#v2-settings-search a").filter({ hasText: "Cartella RAM disk" }).first();
  await expect(hit).toBeVisible();
  await hit.click();
  await expect(page.locator("h1").filter({ hasText: "Configurazione" })).toBeVisible();
  await expect(page.locator("#input-libtorrent_ramdisk_dir")).toBeVisible();
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
    await expect(performance).toContainText("CPU");
    await expect(performance).toContainText("RAM");
    await expect(performance).toContainText("download");
    await expect(performance).toContainText("upload");
  });

  test("mobile mostra salute e log e raccoglie il resto in Sistema", async ({ page }) => {
    await page.goto("/");
    await expect(nav(page, "Salute")).toBeVisible();
    await expect(nav(page, "Log")).toBeVisible();
    await expect(page.locator('aside .nav-mobile-hidden[href="/?view=archive"]')).toBeHidden();

    await page.locator("[data-mobile-system-toggle]").click();
    await expect(nav(page, "Configurazione")).toBeVisible();
    await expect(page.locator(".nav-mobile-system-extra").filter({ hasText: "Esplora" })).toBeVisible();
    await expect(page.locator(".nav-mobile-system-extra").filter({ hasText: "Archivio" })).toBeVisible();
  });
});
