import { test, expect } from "@playwright/test";

// The shell itself: the menu of a computer (symbols, areas, documentation at
// the foot), the queue first on Downloads, and the «Aggiorna» button that
// replaces the page content instead of reloading the whole document.

test.describe("menu del computer", () => {
  test.use({ viewport: { width: 1280, height: 800 } });

  test("ogni voce ha il suo simbolo e ogni area il suo nome", async ({ page }) => {
    await page.goto("/");
    const firstIcon = page.locator('#app-sidebar .nav-item[data-nav="dashboard"] .nav-icon');
    await expect(firstIcon).toBeVisible();
    await expect(firstIcon.locator("svg")).toBeVisible();
    // The five areas are written above their entries.
    for (const label of ["Panoramica", "Download", "Libreria", "Scoperta", "Sistema"]) {
      const groupLabel = page.locator("#app-sidebar .nav-group-label", { hasText: label });
      await expect(groupLabel).toBeVisible();
    }
  });

  test("la documentazione chiude il menu in basso, non lo allunga", async ({ page }) => {
    await page.goto("/?view=series");
    const docs = page.locator("aside .nav-docs");
    await expect(docs).toBeVisible();
    for (const id of ["manual", "changelog"]) {
      await expect(docs.locator(`.nav-doc[data-nav="${id}"]`)).toBeVisible();
    }
    // It starts below the last destination, not among them.
    const groups = page.locator("aside nav:not(.nav-docs)");
    const groupsBox = await groups.boundingBox();
    const docsBox = await docs.boundingBox();
    if (!groupsBox || !docsBox) throw new Error("geometria non disponibile");
    expect(docsBox.y).toBeGreaterThan(groupsBox.y + groupsBox.height - 1);
    // The licence is a documentation page too when it is offered.
    const licence = docs.locator('.nav-doc[data-nav="license"]');
    if (await licence.count()) await expect(licence).toBeVisible();
  });

  test("Scarico mostra la coda prima della ricerca", async ({ page }) => {
    await page.goto("/?view=downloads");
    const queue = await page.locator(".session-panel").boundingBox();
    const search = await page.locator(".downloads-search-panel").boundingBox();
    if (!queue || !search) throw new Error("geometria non disponibile");
    expect(queue.y).toBeLessThan(search.y);
    // The search is one click away, not spread over the first screen.
    await expect(page.locator(".downloads-search-panel")).not.toHaveAttribute("open", "");
  });

  test("«Aggiorna» sostituisce il contenuto senza ricaricare la pagina", async ({ page }) => {
    await page.goto("/?view=series");
    await expect(page.locator("#v2-page")).toBeVisible();
    let requests = 0;
    page.on("request", (request) => {
      if (request.resourceType() === "document") requests += 1;
    });
    // A marker on the window survives a content swap and dies with a reload.
    await page.evaluate(() => { (window as unknown as { __noReload?: boolean }).__noReload = true; });
    await page.locator(".btn-refresh").click();
    await expect(page.locator(".btn-refresh")).toBeEnabled();
    const survived = await page.evaluate(() => (window as unknown as { __noReload?: boolean }).__noReload === true);
    expect(survived).toBe(true);
    expect(requests).toBe(0);
    await expect(page.locator("#v2-table-panel-series-t0")).toBeVisible();
  });
});

test.describe("menu del telefono", () => {
  test.use({ viewport: { width: 390, height: 844 }, isMobile: true, hasTouch: true });

  test("le etichette delle aree restano fuori dalla barra in basso", async ({ page }) => {
    await page.goto("/");
    await expect(page.locator("#app-sidebar .nav-item").first()).toBeVisible();
    await expect(page.locator("#app-sidebar .nav-group-label").first()).toBeHidden();
    // The documentation has no strip of its own here: it is inside "Altro".
    await expect(page.locator("aside .nav-docs")).toBeHidden();
    await page.locator("[data-mobile-system-toggle]").click();
    await expect(page.locator("#app-system-menu a", { hasText: "Manuale" })).toBeVisible();
  });

  test("il menu di una riga si apre come foglio in basso", async ({ page }) => {
    await page.goto("/?view=downloads");
    // A row menu inside a table that scrolls sideways: created here because a
    // torrent in session is not what this check is about.
    await page.evaluate(() => {
      const host = document.querySelector(".content");
      if (!host) throw new Error("contenuto assente");
      host.insertAdjacentHTML(
        "beforeend",
        `<div id="sheet-test"><details class="v2-menu" data-v2-menu>
           <summary class="btn sm icon-action">⋯</summary>
           <div class="v2-menu-panel v2-menu-end"><button class="v2-menu-item">Voce</button></div>
         </details></div>`,
      );
    });
    await page.locator("#sheet-test summary").click();
    const panel = page.locator("#sheet-test .v2-menu-panel");
    await expect(panel).toBeVisible();
    const box = await panel.boundingBox();
    if (!box) throw new Error("geometria non disponibile");
    // A sheet: eight pixels from each side, above the bottom navigation.
    expect(box.x).toBeLessThan(12);
    expect(box.y + box.height).toBeLessThan(844 - 40);
    const width = await page.evaluate(() => window.innerWidth);
    expect(box.width).toBeGreaterThan(width - 24);
  });
});
