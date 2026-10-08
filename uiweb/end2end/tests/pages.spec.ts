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
  await page.route("**/settings/save", async (route) => {
    const key = new URL(route.request().url()).searchParams.get("key") || "refresh_interval";
    await route.fulfill({
      contentType: "text/html; charset=utf-8",
      body: `<form class="setting-row" data-setting-key="${key}"><small class="setting-status"><span class="htx-status ok">Salvato</span></small></form>`,
    });
  });
  await page.goto("/");
  await nav(page, "Configurazione").click();
  const form = page.locator("#v2-setting-refresh_interval");
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

test("la navigazione desktop conserva la barra laterale verticale", async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 800 });
  await page.goto("/");
  const navStyle = await page.locator(".sidebar nav").evaluate((nav) => ({
    display: getComputedStyle(nav).display,
    direction: getComputedStyle(nav).flexDirection,
  }));
  expect(navStyle).toEqual({ display: "flex", direction: "column" });
});

test("scarico espone aggiunta torrent e registrazione magnet", async ({ page }) => {
  await page.goto("/");
  await nav(page, "Scarico").click();
  await expect(page.locator("h3").filter({ hasText: "Aggiungi torrent" })).toBeVisible();
  await expect(page.locator("label.check").filter({ hasText: "Scarica subito" })).toBeVisible();
});

test("la selezione torrent aggiorna il riepilogo delle azioni", async ({ page }) => {
  await page.route("**/downloads/table", async (route) => {
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
      await page.locator('#v2-torrent-form button[name="refresh"]').click();
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

  test("la striscia di stato mostra servizio e velocità e porta alle pagine", async ({ page }) => {
    await page.goto("/");
    const strip = page.locator(".mobile-status-strip");
    await expect(strip).toBeVisible();
    await expect(strip.locator('a[href="/?view=health"]')).toBeVisible();
    await expect(strip.locator('a[href="/?view=downloads"]').first()).toContainText("↓");
    // A dry-run daemon says so in the strip.
    await expect(strip).toContainText("DRY-RUN");
    // CPU and RAM live in Salute, not on every page.
    await expect(strip).not.toContainText("CPU");
  });

  test("un link condiviso apre Scarico con il modulo già compilato", async ({ page }) => {
    const magnet = "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567&dn=Test";
    await page.goto("/share?text=" + encodeURIComponent("guarda " + magnet));
    await expect(page).toHaveURL(/view=downloads/);
    await expect(page.locator('input[name="magnet"]')).toHaveValue(magnet);
    await expect(page.locator("details.add-torrent-panel")).toHaveAttribute("open", "");
  });

  test("Scarico usa schede compatte e conserva l'ordinamento touch", async ({ page }) => {
    await page.goto("/?view=downloads");
    await expect(page.locator("#v2-sort")).toBeVisible();
    await expect(page.locator(".mobile-torrent-sort")).toContainText("Ordina per");
    const cardStyles = await page.evaluate(() => {
      const table = document.createElement("table");
      table.className = "data-table torrent-table";
      table.innerHTML = `<thead><tr><th>Nome</th></tr></thead><tbody><tr><td class="torrent-name">Torrent</td><td class="torrent-state">In scarico</td><td class="torrent-progress">50%</td><td class="row-actions">Azioni</td></tr></tbody>`;
      document.querySelector(".content")?.append(table);
      const row = table.querySelector("tbody tr")!;
      const result = { table: getComputedStyle(table).display, row: getComputedStyle(row).display };
      table.remove();
      return result;
    });
    expect(cardStyles).toEqual({ table: "block", row: "grid" });
    const overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth);
    expect(overflow).toBeLessThanOrEqual(2);
  });

  test("le sezioni della configurazione si scelgono da una tendina", async ({ page }) => {
    await page.goto("/?view=settings");
    const picker = page.locator(".settings-nav-select select");
    await expect(picker).toBeVisible();
    await expect(page.locator(".settings-nav-list")).toBeHidden();
    await picker.selectOption("notify");
    await expect(page.locator("#v2-settings-heading")).toHaveText("Notifiche");
    await expect(page).toHaveURL(/tab=notify/);
    const overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth);
    expect(overflow).toBeLessThanOrEqual(2);
  });

  test("mobile tiene Log nella barra e raccoglie il resto in Altro", async ({ page }) => {
    await page.goto("/");
    await expect(nav(page, "Log")).toBeVisible();
    await expect(nav(page, "Salute")).toBeHidden();
    await expect(page.locator('aside .nav-mobile-hidden[href="/?view=archive"]')).toBeHidden();

    await page.locator("[data-mobile-system-toggle]").click();
    await expect(nav(page, "Salute")).toBeVisible();
    await expect(nav(page, "Configurazione")).toBeVisible();
    // The toggle stays in the bottom bar, and the appearance controls are here.
    await expect(page.locator("[data-mobile-system-toggle]")).toBeVisible();
    await expect(page.locator(".nav-prefs [data-theme-toggle]")).toBeVisible();
    await expect(page.locator(".nav-mobile-system-extra").filter({ hasText: "Esplora" })).toBeVisible();
    await expect(page.locator(".nav-mobile-system-extra").filter({ hasText: "Archivio" })).toBeVisible();
  });

  test("le pagine di sistema non aprono il menu da sole", async ({ page }) => {
    await page.goto("/?view=settings");
    await expect(page.locator("#app-system-menu")).not.toHaveClass(/\bopen\b/);
  });

  test("la navigazione inferiore usa una riga leggibile senza coprire il contenuto", async ({ page }) => {
    await page.goto("/");
    const metrics = await page.evaluate(() => {
      const nav = document.querySelector(".sidebar nav")!;
      const sidebar = document.querySelector(".sidebar")!;
      const content = document.querySelector(".content")!;
      const visibleLinks = Array.from(nav.querySelectorAll<HTMLElement>(".nav-item, .nav-more"))
        .filter((item) => item.getBoundingClientRect().height > 0);
      return {
        rows: getComputedStyle(nav).gridTemplateRows.split(" ").length,
        sidebarHeight: sidebar.getBoundingClientRect().height,
        largestLabel: Math.max(...visibleLinks.map((item) => parseFloat(getComputedStyle(item).fontSize))),
        contentPaddingBottom: parseFloat(getComputedStyle(content).paddingBottom),
      };
    });
    expect(metrics.rows).toBe(1);
    expect(metrics.sidebarHeight).toBeLessThan(90);
    expect(metrics.largestLabel).toBeGreaterThanOrEqual(12);
    expect(metrics.contentPaddingBottom).toBeGreaterThanOrEqual(metrics.sidebarHeight);
  });
});

test("la ricerca archivio mostra l'indicatore web solo se richiesto", async ({ page }) => {
  // Slow down the archive table request so the spinner would stay visible while
  // it is in flight, which is exactly the window the user sees.
  await page.route("**/table?*", async (route) => {
    if (!route.request().url().includes("view=archive")) {
      await route.continue();
      return;
    }
    await new Promise((resolve) => setTimeout(resolve, 1200));
    await route.continue();
  });

  await page.goto("/?view=archive");
  const indicator = page.locator(".archive-indicator");
  const indicatorShown = () =>
    indicator.evaluate((element) => getComputedStyle(element).display !== "none");

  await page.fill('input[name="q"]', "matrix");
  await page.getByRole("button", { name: "Cerca" }).click();
  await page.waitForTimeout(300);
  expect(await indicatorShown(), "local search must not advertise a web query").toBe(false);

  await page.check('input[name="web"]');
  await page.getByRole("button", { name: "Cerca" }).click();
  await page.waitForTimeout(300);
  expect(await indicatorShown(), "web search must show its indicator").toBe(true);
});

test("integrazioni raggruppa le voci con intestazioni sticky", async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 800 });
  await page.goto("/?view=integrations");
  const labels = page.locator(".section-group-label");
  await expect(labels).toHaveText(["Servizi", "Media server", "Sorgenti", "Collegamenti"]);
  await expect(labels.first()).toHaveCSS("position", "sticky");
  await expect(labels.first()).toBeVisible();
});

test("Salute affianca Motore torrent e Stato provider", async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 900 });
  await page.goto("/?view=health");
  const engine = page.locator("#v2-health-engine");
  const provider = page.locator("main .panel").filter({ has: page.locator("h3", { hasText: "Stato provider" }) });
  await expect(engine).toBeVisible();
  await expect(provider).toBeVisible();
  const [e, p] = await Promise.all([engine.boundingBox(), provider.boundingBox()]);
  expect(e).not.toBeNull();
  expect(p).not.toBeNull();
  // Same row, provider to the right of the engine panel.
  expect(Math.abs(e!.y - p!.y)).toBeLessThan(60);
  expect(p!.x).toBeGreaterThan(e!.x + e!.width - 5);
});
