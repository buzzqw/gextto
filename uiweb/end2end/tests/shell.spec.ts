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

  test("«Aggiorna» sostituisce il contenuto senza ricaricare la pagina", async ({ page }) => {    await page.goto("/?view=series");
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

test.describe("locandine", () => {
  test("la card è una locandina 2:3 con i dati tecnici sopra la copertina", async ({ page }) => {
    await page.goto("/?view=series");
    // A card as the server renders it (the test library is empty): the measure
    // is what the component promises, not what a screenshot happens to show.
    await page.evaluate(() => {
      const host = document.querySelector(".content");
      if (!host) throw new Error("contenuto assente");
      host.insertAdjacentHTML("beforeend", `<div class="library-cards"><article class="library-card">
        <a class="library-card-poster" href="/"><span class="library-card-badges">
          <span class="library-card-badge" title="Qualità">1080p</span><span class="library-card-badge" title="Lingua">ita</span></span></a>
        <div class="library-card-body"><strong class="library-card-title"><a href="/">Series A</a></strong>
          <div class="card-meta"><span class="pbar"><progress value="5" max="10"></progress><span class="pbar-value">50%</span></span></div></div>
      </article></div>`);
    });
    const poster = page.locator(".library-card-poster");
    const box = await poster.boundingBox();
    if (!box) throw new Error("geometria non disponibile");
    expect(box.height / box.width).toBeCloseTo(1.5, 1);
    // The badges are over the artwork, not under it.
    const badge = page.locator(".library-card-badge").first();
    const badgeBox = await badge.boundingBox();
    if (!badgeBox) throw new Error("badge non posizionato");
    expect(badgeBox.y).toBeGreaterThan(box.y);
    expect(badgeBox.y + badgeBox.height).toBeLessThan(box.y + box.height);
    // The completion bar has a line of its own under the title.
    const bar = page.locator(".card-meta .pbar");
    await expect(bar).toBeVisible();
    expect((await bar.boundingBox())!.width).toBeGreaterThan(box.width * 0.8);
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

test.describe("aggiunta di un torrent", () => {
  test.use({ viewport: { width: 1280, height: 800 } });

  test("il modulo è un frame che scende dall'alto, prende il focus e si chiude con Esc o con la ✕", async ({ page }) => {
    await page.goto("/?view=downloads");
    const panel = page.locator("#v2-add-torrent");
    await expect(panel).toBeHidden();
    const toggle = page.locator("[data-add-torrent-toggle]");
    await toggle.click();
    await expect(panel).toBeVisible();
    await expect(toggle).toHaveAttribute("aria-expanded", "true");

    // A frame hanging from the top, as wide as the content column: the page
    // stays visible under it instead of sitting behind a centred popup.
    const style = await panel.evaluate((element) => {
      const computed = getComputedStyle(element);
      return { position: computed.position, zIndex: Number(computed.zIndex) };
    });
    expect(style.position).toBe("fixed");
    expect(style.zIndex).toBeGreaterThan(40); // above the sticky top bar
    const box = await panel.boundingBox();
    const sidebar = await page.locator("#app-sidebar").boundingBox();
    if (!box || !sidebar) throw new Error("geometria non disponibile");
    expect(box.y).toBeLessThanOrEqual(1);
    expect(box.x).toBeGreaterThanOrEqual(sidebar.width - 1);
    expect(box.width).toBeGreaterThan(1280 - sidebar.width - 40);

    // The field to paste into takes the focus at once.
    await expect.poll(() => page.evaluate(() => document.activeElement?.getAttribute("name"))).toBe("magnet");

    // The keyboard stays inside the frame while it is open.
    const last = panel.locator("button[type=submit]").last();
    await last.focus();
    await page.keyboard.press("Tab");
    const inside = await page.evaluate(() => !!document.activeElement?.closest("#v2-add-torrent"));
    expect(inside).toBe(true);

    // «Annulla» is the way out under the primary button, right of the last
    // option: it leaves without adding anything.
    const cancel = panel.locator(".add-torrent-cancel");
    await expect(cancel).toBeVisible();
    await expect(cancel).toHaveText("Annulla");
    const seed = panel.getByText("Salta verifica").first();
    const seedBox = await seed.boundingBox();
    const cancelBox = await cancel.boundingBox();
    if (!seedBox || !cancelBox) throw new Error("geometria non disponibile");
    expect(cancelBox.x).toBeGreaterThan(seedBox.x);

    // ✕ closes it and gives the focus back to the button that opened it.
    await panel.locator(".add-torrent-close").click();
    await expect(panel).toBeHidden();
    await expect(toggle).toHaveAttribute("aria-expanded", "false");
    await expect(toggle).toBeFocused();

    // So does «Annulla», and so does Escape.
    await toggle.click();
    await expect(panel).toBeVisible();
    await panel.locator(".add-torrent-cancel").click();
    await expect(panel).toBeHidden();
    await expect(toggle).toBeFocused();
    await toggle.click();
    await expect(panel).toBeVisible();
    await page.keyboard.press("Escape");
    await expect(panel).toBeHidden();
    await expect(toggle).toBeFocused();
  });
});

test.describe("telefono stretto", () => {
  test.use({ viewport: { width: 320, height: 700 }, isMobile: true, hasTouch: true });

  test("nella barra restano i simboli, con il nome ancora annunciato", async ({ page }) => {
    await page.goto("/");
    // A label at 8.5 px was not read, it was guessed: it steps aside and the
    // name stays for assistive technology only.
    const label = page.locator('#app-sidebar .nav-item[data-nav="dashboard"] > span:not(.nav-icon)');
    const box = await label.boundingBox();
    expect(box === null || box.width <= 2).toBe(true);
    await expect(page.getByRole("link", { name: "Dashboard" }).first()).toBeVisible();
    await expect(page.getByRole("link", { name: "Scarico" }).first()).toBeVisible();
    const overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth);
    expect(overflow).toBeLessThanOrEqual(2);
  });
});
