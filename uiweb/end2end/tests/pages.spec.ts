import { test, expect } from "@playwright/test";

const navViews: Record<string, string> = {
  "Salute": "health",
  "Configurazione": "settings",
  "Scarico": "downloads",
  "Film": "movies",
  "Archivio": "archive",
  "Manutenzione": "maintenance",
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
  // The form is hidden behind the top-bar toggle so the closed block costs no
  // vertical space; the toggle opens it.
  const toggle = page.locator("[data-add-torrent-toggle]");
  await expect(toggle).toBeVisible();
  await expect(page.locator("details.add-torrent-panel")).toBeHidden();
  await toggle.click();
  await expect(page.locator("details.add-torrent-panel")).toBeVisible();
  await expect(page.locator("h3").filter({ hasText: "Aggiungi torrent" })).toBeVisible();
  await expect(page.locator("label.check").filter({ hasText: "Scarica subito" })).toBeVisible();
});

test("il pulsante Aggiungi torrent è in ogni pagina e apre il modulo su Scarico", async ({ page }) => {
  await page.goto("/");
  // The button is part of the chrome, not only of Scarico.
  const toggle = page.locator("[data-add-torrent-toggle]");
  await expect(toggle).toBeVisible();
  await toggle.click();
  await expect(page).toHaveURL(/view=downloads/);
  await expect(page.locator("details.add-torrent-panel")).toBeVisible();
  await expect(page.locator('input[name="magnet"]')).toBeFocused();
});

test("Aggiorna e lo stato restano nella riga di Aggiungi torrent", async ({ page }) => {
  await page.setViewportSize({ width: 1920, height: 900 });
  await page.goto("/");
  const chrome = await page.evaluate(() => {
    const box = (selector: string) => {
      const element = document.querySelector(selector);
      if (!element) return null;
      const rect = element.getBoundingClientRect();
      return {
        inActions: element.closest(".top-actions") !== null,
        x: Math.round(rect.x),
        middle: Math.round(rect.y + rect.height / 2),
      };
    };
    return {
      add: box("[data-add-torrent-toggle]"),
      refresh: box(".btn-refresh"),
      status: box("#v2-live-status"),
      // #v2-live-status uses display:contents on the desktop, so its own box
      // is empty: the pill is the element that has a position.
      pill: box(".system-pill"),
    };
  });
  // Refresh and the service state are part of the same row as the add button,
  // to its right, not in the controls row below.
  expect(chrome.add).not.toBeNull();
  expect(chrome.refresh?.inActions).toBe(true);
  expect(chrome.status?.inActions).toBe(true);
  expect(chrome.pill).not.toBeNull();
  expect(chrome.add!.x).toBeLessThan(chrome.refresh!.x);
  expect(chrome.refresh!.x).toBeLessThan(chrome.pill!.x);
  expect(Math.abs(chrome.add!.middle - chrome.refresh!.middle)).toBeLessThanOrEqual(3);
  expect(Math.abs(chrome.refresh!.middle - chrome.pill!.middle)).toBeLessThanOrEqual(3);
});

test("il riepilogo della Dashboard vive nella barra in alto, accanto al menu Aspetto", async ({ page }) => {
  await page.setViewportSize({ width: 1920, height: 900 });
  await page.goto("/");
  const kpis = page.locator(".top-kpis");
  await expect(kpis).toBeVisible();
  // The badges are part of the header controls, beside the Aspetto menu, and no
  // longer occupy a row of their own in the page body.
  await expect(page.locator(".dashboard-summary-grid")).toHaveCount(0);
  await expect(page.locator(".top-kpi")).toHaveCount(2);
  const positions = await page.evaluate(() => {
    const kpis = document.querySelector(".top-kpis");
    const appearance = document.querySelector(".top-controls .appearance-toggle");
    const controls = document.querySelector(".top-controls");
    const clock = document.querySelector(".current-time");
    if (!kpis || !appearance || !controls || !clock) return null;
    const kpiBox = kpis.getBoundingClientRect();
    const clockBox = clock.getBoundingClientRect();
    return {
      insideControls: kpis.closest(".top-controls") === controls,
      kpiX: Math.round(kpiBox.x),
      appearanceX: Math.round(appearance.getBoundingClientRect().x),
      kpiRight: Math.round(kpiBox.right),
      clockRight: Math.round(clockBox.right),
    };
  });
  expect(positions).not.toBeNull();
  expect(positions!.insideControls).toBe(true);
  // The two badges sit at the left of the row, the Aspetto menu after them.
  expect(positions!.kpiX).toBeLessThan(positions!.appearanceX);
  expect(positions!.kpiRight).toBeLessThan(positions!.appearanceX);
  // Only the Dashboard carries them.
  await page.goto("/?view=downloads");
  await expect(page.locator(".top-kpis")).toHaveCount(0);
});

test("la selezione torrent aggiorna il riepilogo delle azioni", async ({ page }) => {
  await page.route("**/downloads/table", async (route) => {
    await route.fulfill({
      contentType: "text/html; charset=utf-8",
      body: `<div id="v2-torrents-wrap"><form id="v2-torrent-form">
        <span data-v2-selected-count>0 selezionati</span>
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

test("shift+clic seleziona l'intervallo di righe", async ({ page }) => {
  await page.route("**/downloads/table", async (route) => {
    const rows = [1, 2, 3, 4].map((n) => `<tr><td><input type="checkbox" data-v2-select value="range-${n}" aria-label="Seleziona ${n}" /></td></tr>`).join("");
    await route.fulfill({
      contentType: "text/html; charset=utf-8",
      body: `<div id="v2-torrents-wrap"><form id="v2-torrent-form">
        <span data-v2-selected-count>0 selezionati</span>
        <table><tbody>${rows}</tbody></table>
      </form></div>
      <div class="view" data-torrents-slot>
      </div>`,
    });
  });
  await page.goto("/?view=downloads");
  const boxes = page.locator("[data-v2-select]");
  if (!(await boxes.first().isVisible())) {
    await page.locator('#v2-torrent-form button[name="refresh"]').click();
  }
  await expect(boxes).toHaveCount(4);
  await boxes.nth(0).click();
  await boxes.nth(3).click({ modifiers: ["Shift"] });
  for (let i = 0; i < 4; i++) await expect(boxes.nth(i)).toBeChecked();
  await expect(page.locator("[data-v2-selected-count]")).toContainText("4 selezionati");
});

test("/ porta al filtro e un magnet incollato compila Aggiungi torrent", async ({ page }) => {
  await page.goto("/?view=downloads");
  await page.locator("body").click({ position: { x: 5, y: 5 } });
  await page.keyboard.press("/");
  await expect(page.locator("#v2-filter")).toBeFocused();
  await page.locator("h1").first().click();
  const magnet = "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567";
  await page.evaluate((text) => {
    const data = new DataTransfer();
    data.setData("text/plain", text);
    document.body.dispatchEvent(new ClipboardEvent("paste", { clipboardData: data, bubbles: true, cancelable: true }));
  }, magnet);
  await expect(page.locator("#v2-add-torrent")).toHaveAttribute("open", "");
  await expect(page.locator("#v2-add-torrent input[name='magnet']")).toHaveValue(magnet);
});

test("selettore lingua presente e selezionabile", async ({ page }) => {
  await page.goto("/");
  // Language, text size, font and theme live in the Aspetto menu of the top bar.
  await expect(page.locator(".lang-select")).toBeHidden();
  await page.locator(".appearance-toggle").click();
  await expect(page.locator(".lang-select")).toBeVisible();
  await page.locator(".lang-select").selectOption("en");
  await page.waitForLoadState("domcontentloaded");
  await expect(page.locator("aside .nav-item").first()).toBeVisible();
  await page.locator(".appearance-toggle").click();
  await page.locator(".lang-select").selectOption("it");
  await expect(page.locator("aside .nav-item").filter({ hasText: "Scarico" }).first()).toBeVisible({ timeout: 15000 });
});

test("manuale e changelog puntano alla documentazione online", async ({ page }) => {
  await page.goto("/");
  const manual = page.locator('aside .nav-item[data-nav="manual"]');
  await expect(manual).toHaveAttribute("href", "https://github.com/buzzqw/gextto/blob/main/docs/MANUAL.it.md");
  await expect(manual).toHaveAttribute("target", "_blank");
  const changelog = page.locator('aside .nav-item[data-nav="changelog"]');
  await expect(changelog).toHaveAttribute("href", "https://github.com/buzzqw/gextto/blob/main/changelog.md");
  await expect(changelog).toHaveAttribute("target", "_blank");
});

test("manutenzione espone la revisione rinomina cartella", async ({ page }) => {
  await page.goto("/");
  await nav(page, "Manutenzione").click();
  await expect(page.locator("h3").filter({ hasText: "Rinomina contenuto cartella" })).toBeVisible();
  await expect(page.getByRole("textbox", { name: "Cartella", exact: true })).toBeVisible();
  await expect(page.getByRole("button", { name: "Scansiona e proponi" })).toBeVisible();
});

test("manutenzione SQL dell'archivio ha un tab e usa la lingua scelta", async ({ page }) => {
  await page.goto("/?view=maintenance");
  await page.getByRole("tab", { name: "Manutenzione SQL dell'archivio" }).click();
  await expect(page.locator("#v2-archive-sql-panel")).toBeVisible();
  const language = page.locator("#v2-archive-sql-language");
  await expect(language).toBeVisible();
  await language.selectOption("eng");
  const languageOp = page.locator(".v2-archive-sql-op", { hasText: "Release nella lingua selezionata più vecchie di 6 mesi" });
  await languageOp.getByRole("button", { name: "Anteprima", exact: true }).click();
  await expect(page.locator("#v2-archive-sql-output")).toContainText("righe corrispondenti");
  await expect(page.locator("#v2-archive-sql-output")).toContainText("English");
});

test("le diagnostiche SQL mostrano il risultato in vista", async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 480 });
  await page.goto("/?view=maintenance&tab=archive-sql");
  const operation = page.locator(".v2-archive-sql-op", { hasText: "Stato dell'archivio" });
  await operation.scrollIntoViewIfNeeded();
  await operation.getByRole("button", { name: "Esegui", exact: true }).click();
  const output = page.locator("#v2-archive-sql-output");
  await expect(output).toContainText("righe corrispondenti");
  await expect(output).toBeInViewport();
});

test("manutenzione SQL mostra l'indicatore mentre la query lavora", async ({ page }) => {
  await page.goto("/?view=maintenance&tab=archive-sql");
  await page.route("**/maintenance/archive-sql/preview", async (route) => {
    await new Promise((resolve) => setTimeout(resolve, 1200));
    await route.fulfill({ contentType: "text/html; charset=utf-8", body: "<div>ok</div>" });
  });
  const operation = page.locator(".v2-archive-sql-op", { hasText: "Release più vecchie di 6 mesi" }).first();
  await operation.getByRole("button", { name: "Anteprima", exact: true }).click();
  const working = page.locator("#v2-archive-sql-working");
  await expect(working).toHaveClass(/htmx-request/);
  await expect(working).toContainText("Query in corso");
  await expect(working).not.toHaveClass(/htmx-request/);
});

test("manutenzione chiude con build completa e commit", async ({ page }) => {
  await page.goto("/?view=maintenance");
  const info = page.locator(".v2-build-info");
  await expect(info).toBeVisible();
  await expect(info).toContainText("Versione");
  await expect(info).toContainText("build");
  await expect(info).toContainText("commit");
  // The short hash is shown, the full commit is in the tooltip.
  const commit = info.locator("code");
  await expect(commit).toHaveText(/^[0-9a-f]{7,8}$|^—$/);
  await expect(commit).toHaveAttribute("title", /^([0-9a-f]{40})?$/);
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
  await expect(page.locator("#input-torrent_ramdisk_dir")).toBeVisible();
});

test("il menu Aspetto resta dentro la finestra della Dashboard", async ({ page }) => {
  // The Dashboard pushes the button to the far right of its row: the panel used
  // to be cut by the window edge there.
  await page.setViewportSize({ width: 1280, height: 800 });
  await page.goto("/");
  await page.locator(".appearance-toggle").click();
  const panel = page.locator(".appearance-panel");
  await expect(panel).toBeVisible();
  const box = await panel.boundingBox();
  const width = await page.evaluate(() => window.innerWidth);
  expect(box).not.toBeNull();
  expect(box!.x).toBeGreaterThanOrEqual(0);
  expect(box!.x + box!.width).toBeLessThanOrEqual(width);
});

test("il menu «⋯» di una riga non apre il dettaglio del torrent", async ({ page }) => {
  // The row opens the detail drawer when the click lands on a dead area. The
  // menu is a <details> whose head is a summary: it must stay its own thing.
  await page.route("**/partial/downloads/rows", async (route) => {
    await route.fulfill({
      contentType: "text/html; charset=utf-8",
      body: `<div id="v2-download-rows" data-v2-poll hx-post="/partial/downloads/rows" hx-target="this" hx-include="#v2-torrent-form" hx-trigger="every 5s [window.gexttoCanPoll()], v2wake" hx-swap="outerHTML">
        <table class="data-table torrent-table"><tbody><tr id="v2-torrent-abc" data-hash="abc"><td class="torrent-card-cell"><div class="torrent-card">
          <button class="torrent-open" type="button" hx-get="/downloads/detail?hash=abc&tab=general" hx-target="#v2-detail-drawer" hx-swap="innerHTML">Nome</button>
          <div class="row-actions"><details class="v2-menu" data-v2-menu><summary class="btn sm icon-action" aria-label="Altre azioni">⋯</summary>
            <div class="v2-menu-panel v2-menu-end"><button class="v2-menu-item" type="submit" name="row_action" value="recheck:abc">Verifica dati</button></div>
          </details></div>
        </div></td></tr></tbody></table></div>`,
    });
  });
  let details = 0;
  page.on("request", (request) => { if (request.url().includes("/downloads/detail")) details += 1; });
  await page.goto("/?view=downloads");
  const menu = page.locator("#v2-download-rows details.v2-menu");
  await expect(menu).toBeVisible({ timeout: 15000 });
  await page.locator("#v2-download-rows details.v2-menu > summary").click();
  await expect(menu).toHaveAttribute("open", "");
  await page.waitForTimeout(500);
  expect(details).toBe(0);
  await expect(page.locator("#v2-detail-drawer")).toBeHidden();
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

  test("Scarico accetta più link eD2k e le impostazioni eD2k sono leggibili", async ({ page }) => {
    await page.goto("/?view=downloads");
    const links = page.locator('textarea[name="ed2k_link"]');
    await expect(links).toBeVisible();
    await expect(links).toHaveAttribute("rows", "2");
    await expect(page.locator('button:has-text("Apri link eD2k")')).toBeVisible();
    const overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth);
    expect(overflow).toBeLessThanOrEqual(2);

    await page.goto("/?view=settings");
    await page.locator("input.settings-search").fill("aMule");
    await expect(page.locator("#v2-settings-search").getByText("aMule — porta connessioni esterne (EC)")).toBeVisible();
    const settingsOverflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth);
    expect(settingsOverflow).toBeLessThanOrEqual(2);
  });

  test("la pagina eD2k spiega come collegare aMule quando è spento", async ({ page }) => {
    await page.goto("/?view=ed2k");
    await expect(page.locator("#v2-ed2k-state")).toContainText("L'integrazione con aMule è spenta");
    const settingsLink = page.locator('#v2-ed2k-state a[href*="highlight=ed2k_enabled"]');
    await expect(settingsLink).toBeVisible();
    await settingsLink.focus();
    await expect(settingsLink).toBeFocused();
    await expect(page.locator('a[data-nav="ed2k"]').first()).toBeAttached();
    const overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth);
    expect(overflow).toBeLessThanOrEqual(2);
  });

  test("Scarico usa schede compatte e conserva l'ordinamento touch", async ({ page }) => {
    await page.goto("/?view=downloads");
    await expect(page.locator("#v2-sort")).toBeVisible();
    await expect(page.locator(".torrent-sort")).toContainText("Ordina per");
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
    expect(cardStyles).toEqual({ table: "block", row: "block" });
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
    // The toggle stays in the bottom bar. The appearance controls left this
    // panel for the Aspetto menu of the top bar; the donation link is still
    // here, because the floating one is hidden on a phone.
    await expect(page.locator("[data-mobile-system-toggle]")).toBeVisible();
    await expect(page.locator(".nav-prefs .nav-prefs-donate")).toBeVisible();
    await expect(page.locator(".top-controls .appearance-toggle")).toBeVisible();
    await expect(page.locator(".nav-mobile-system-extra").filter({ hasText: "Esplora" })).toBeVisible();
    await expect(page.locator(".nav-mobile-system-extra").filter({ hasText: "Archivio" })).toBeVisible();
    // Le voci facoltative (Blocklist, Licenza) vivono solo nel pannello: se non
    // comparissero qui resterebbero irraggiungibili dal telefono.
    await expect(page.locator('aside .nav-item[data-nav="blocklist"]')).toBeVisible();
    await expect(page.locator('aside .nav-item[data-nav="license"]')).toBeVisible();
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

test("il dettaglio torrent è un frame sopra lo storico, non un modale", async ({ page }) => {
  await page.goto("/?view=downloads");
  const drawer = page.locator("#v2-detail-drawer");
  await expect(drawer).toHaveCount(1);
  await expect(drawer).toBeHidden();
  // The frame is placed above the download history, not overlaid on the viewport.
  const position = await drawer.evaluate((el) => getComputedStyle(el).position);
  expect(position).not.toBe("fixed");
  // The old centered torrent modal must be gone.
  await expect(page.locator(".modal.torrent-modal")).toHaveCount(0);
});

test("lo storico download lascia una colonna nome leggibile a ogni larghezza", async ({ page }) => {
  // Between 701px and ~1000px the history table switches to a fixed layout: if
  // the secondary columns are not folded early enough the name column keeps
  // only a few pixels and wraps one character per line.
  for (const width of [720, 800, 900, 1024]) {
    await page.setViewportSize({ width, height: 900 });
    await page.goto("/?view=downloads");
    const name = page.locator("table.download-history-table thead th").first();
    await expect(name).toBeVisible();
    const box = await name.boundingBox();
    expect(box, `history name column at ${width}px`).not.toBeNull();
    expect(box!.width, `history name column at ${width}px`).toBeGreaterThan(150);
  }
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

test("il filtro del log funziona anche con lo scorrimento fermo", async ({ page }) => {
  await page.goto("/?view=logs");
  const view = page.locator("#v2-logs-view");
  const count = page.locator("#v2-logs-count");
  const follow = page.locator("[data-v2-logs-follow]");
  await expect(view).toBeVisible();
  await expect(count).not.toHaveText("0");

  // Pause the automatic follow: its label flips whatever the interface language.
  const before = await follow.innerText();
  await follow.click();
  await expect(follow).not.toHaveText(before);

  // The filter must still be applied while paused: a term that matches nothing
  // empties the view and the line count. Before the fix every response was
  // treated as a poll and dropped, so the filter looked broken until a reload.
  await page.fill("#v2-log-filter", "nessuna-riga-corrisponde-zzz");
  await expect(count).toHaveText("0");
  await expect(view).not.toContainText("INFO");

  // Clearing the filter brings the lines and the count back.
  await page.fill("#v2-log-filter", "");
  await expect(count).not.toHaveText("0");
});


test("la pagina Importa file analizza un percorso", async ({ page }) => {
  await page.goto("/?view=maintenance&tab=import");
  await page.getByRole("button", { name: "Sfoglia" }).click();
  await expect(page.getByRole("dialog", { name: "Scegli un video o una cartella" })).toBeVisible();
  await page.getByRole("button", { name: "Chiudi" }).click();
  await expect(page.getByRole("heading", { name: "Importa un file esistente" })).toBeVisible();
  const input = page.locator('input[name="path"]');
  await expect(input).toBeVisible();
  await input.fill("/definitely/not/a/video.mkv");
  await page.getByRole("button", { name: "Analizza" }).click();
  await expect(page.locator("#v2-import-result [role=alert]")).toBeVisible();
});

test("la tabella delle serie modifica le righe selezionate", async ({ page }) => {
  await page.goto("/?view=series&layout=list");
  const toggle = page.getByRole("button", { name: "Modifica selezionati" });
  const refresh = page.getByRole("button", { name: "Aggiorna elenco" });
  // The two buttons sit on the same toolbar row, "Modifica selezionati" first.
  const [t, r] = [await toggle.boundingBox(), await refresh.boundingBox()];
  expect(t && r && Math.abs(t.y - r.y) < 4 && t.x < r.x).toBeTruthy();
  await toggle.click();
  await expect(toggle).toHaveAttribute("aria-expanded", "true");
  await expect(page.getByRole("button", { name: "Applica ai titoli selezionati" })).toBeVisible();
  await expect(page.locator('input[name="set_quality"]')).toBeVisible();
});

test("Serie TV ha la scheda Mancanti al posto della pagina separata", async ({ page }) => {
  await page.goto("/?view=gaps");
  await expect(page).toHaveURL(/view=series&tab=gaps/);
  await expect(page.getByRole("tab", { name: /Mancanti/ })).toHaveAttribute("aria-selected", "true");
  await expect(page.getByRole("heading", { name: "Puntate mancanti" })).toBeVisible();
  await expect(page.getByRole("searchbox", { name: "Filtra per serie" })).toBeVisible();
});

test("il filtro per stato della vista Download mostra Tutti i torrent premuto", async ({ page }) => {
  await page.goto("/?view=downloads");
  const group = page.locator(".state-chips");
  await expect(group).toBeVisible();
  const all = group.locator('button[value="all"]');
  await expect(all).toHaveAttribute("aria-pressed", "true");
  await expect(all.locator(".n")).toHaveText(/^\d+$/);
});
