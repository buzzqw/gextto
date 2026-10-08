import { test, expect } from "@playwright/test";

// Configurazione: section navigation, unsaved-changes handling, switches with
// dependent rows and restoring defaults. Saves are intercepted so the shared
// dry-run data directory is never changed by these tests.

test.beforeEach(async ({ page }) => {
  await page.route("**/settings/save", async (route) => {
    const form = new URLSearchParams(route.request().postData() || "");
    const key = form.get("key") || "";
    await route.fulfill({
      contentType: "text/html; charset=utf-8",
      body: `<form class="setting-row" data-setting-key="${key}" id="v2-setting-${key}"><label class="setting-label" id="label-${key}">${key}</label><small class="setting-status" id="status-${key}"><span class="htx-status ok">salvata</span></small></form>`,
    });
  });
});

test("la navigazione aggiorna titolo, sezione attiva e indirizzo", async ({ page }) => {
  await page.goto("/?view=settings");
  const nav = page.getByRole("navigation", { name: "Sezioni della configurazione" });
  await expect(nav.getByText("Cosa cercare")).toBeVisible();
  await expect(nav.getByText("Sistema")).toBeVisible();
  await nav.getByRole("link", { name: /Seed e completamento/ }).click();
  await expect(page.locator("#v2-settings-heading")).toHaveText("Seed e completamento");
  await expect(page.locator("#v2-settings-heading")).toBeFocused();
  await expect(nav.locator('[aria-current="page"]')).toContainText("Seed e completamento");
  await expect(page).toHaveURL(/tab=seeding/);
  // An old link still opens the section that took over its content.
  await page.goto("/?view=settings&tab=acquisition");
  await expect(page.locator("#v2-settings-heading")).toHaveText("Manutenzione automatica");
});

test("la configurazione nasconde il menu principale e lo riapre dal pulsante", async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 800 });
  await page.goto("/?view=settings");
  // The main navigation is out of the way, the «Cosa cercare» sidebar stays.
  await expect(page.locator("#app-sidebar")).toBeHidden();
  await expect(page.getByRole("navigation", { name: "Sezioni della configurazione" })).toBeVisible();
  const toggle = page.locator("[data-app-menu-toggle]");
  await expect(toggle).toBeVisible();
  await expect(toggle).toHaveAttribute("aria-expanded", "false");
  await toggle.click();
  await expect(page.locator("#app-sidebar")).toBeVisible();
  await expect(toggle).toHaveAttribute("aria-expanded", "true");
  // Escape closes the drawer and returns to the focused button.
  await page.keyboard.press("Escape");
  await expect(page.locator("#app-sidebar")).toBeHidden();
});

test("il menu principale resta visibile fuori dalla configurazione", async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 800 });
  await page.goto("/");
  await expect(page.locator("#app-sidebar")).toBeVisible();
  await expect(page.locator("[data-app-menu-toggle]")).toHaveCount(0);
});

test("le modifiche non salvate si vedono, si annullano e si salvano insieme", async ({ page }) => {
  await page.goto("/?view=settings&tab=daemon");
  const bar = page.locator("[data-v2-savebar]");
  await expect(bar).toBeHidden();
  await page.locator("#input-refresh_interval").fill("7200");
  await page.locator("#input-max_release_age_days").fill("30");
  await expect(bar).toBeVisible();
  await expect(bar.locator("[data-v2-dirty-count]")).toHaveText("2");
  await expect(page.locator("#v2-setting-refresh_interval [data-v2-dirty-badge]")).toBeVisible();

  await bar.getByRole("button", { name: "Annulla modifiche" }).click();
  await expect(bar).toBeHidden();
  await expect(page.locator("#input-refresh_interval")).not.toHaveValue("7200");

  await page.locator("#input-refresh_interval").fill("7200");
  await page.locator("#input-max_release_age_days").fill("30");
  await bar.getByRole("button", { name: "Salva tutto" }).click();
  await expect(page.locator("#v2-setting-refresh_interval .htx-status")).toHaveText("salvata");
  await expect(page.locator("#v2-setting-max_release_age_days .htx-status")).toHaveText("salvata");
  await expect(bar).toBeHidden();
  await expect(page.locator("#v2-settings-live")).toContainText("Impostazioni salvate: 2");
});

test("cambiare sezione con modifiche in sospeso chiede conferma", async ({ page }) => {
  await page.goto("/?view=settings&tab=daemon");
  await page.locator("#input-refresh_interval").fill("7200");
  page.once("dialog", (dialog) => dialog.dismiss());
  await page.getByRole("link", { name: /Notifiche/ }).first().click();
  await expect(page.locator("#v2-settings-heading")).toHaveText("Generale");
  await expect(page.locator("#input-refresh_interval")).toHaveValue("7200");

  page.once("dialog", (dialog) => dialog.accept());
  await page.getByRole("link", { name: /Notifiche/ }).first().click();
  await expect(page.locator("#v2-settings-heading")).toHaveText("Notifiche");
});

test("un interruttore mostra i campi che dipendono da lui", async ({ page }) => {
  await page.goto("/?view=settings&tab=libtorrent");
  const start = page.locator("#v2-setting-libtorrent_sched_start");
  const toggle = page.getByRole("switch", { name: "Programmazione velocità attiva" });
  await expect(toggle).not.toBeChecked();
  await expect(start).toBeHidden();
  await toggle.check();
  await expect(start).toBeVisible();
  await expect(page.locator("#input-libtorrent_sched_start")).toHaveAttribute("type", "time");
  await expect(page.locator("#v2-setting-libtorrent_sched_days").getByRole("checkbox")).toHaveCount(7);
  await toggle.uncheck();
  await expect(start).toBeHidden();
});

test("predefinito ripristina il valore e va salvato", async ({ page }) => {
  await page.goto("/?view=settings&tab=daemon");
  const row = page.locator("#v2-setting-refresh_interval");
  // The server renders «Predefinito» only when the saved value differs from
  // the default; on a pristine data directory add the same control.
  if (!(await row.locator("[data-v2-reset]").count())) {
    await row.evaluate((form) => {
      const button = document.createElement("button");
      button.type = "button";
      button.setAttribute("data-v2-reset", "21600");
      button.textContent = "Predefinito";
      form.querySelector(".setting-actions")!.prepend(button);
    });
  }
  await page.locator("#input-refresh_interval").evaluate((input: HTMLInputElement) => { input.value = "60"; input.defaultValue = "60"; });
  await row.locator("[data-v2-reset]").first().click();
  await expect(page.locator("#input-refresh_interval")).toHaveValue("21600");
  await expect(page.locator("[data-v2-savebar]")).toBeVisible();
});

test("le chiavi tecniche si mostrano a richiesta", async ({ page }) => {
  await page.goto("/?view=settings&tab=daemon");
  const key = page.locator("#v2-setting-refresh_interval .setting-key");
  await expect(key).toBeHidden();
  await page.getByLabel("Mostra chiavi tecniche").check();
  await expect(key).toBeVisible();
  await page.reload();
  await expect(page.locator("#v2-setting-refresh_interval .setting-key")).toBeVisible();
  await page.getByLabel("Mostra chiavi tecniche").uncheck();
});

test("ogni sezione è senza violazioni di accessibilità", async ({ page }) => {
  test.setTimeout(180000);
  const AxeBuilder = (await import("@axe-core/playwright")).default;
  for (const tab of ["daemon", "sources", "scores", "backend", "libtorrent", "performance", "seeding", "rename", "paths", "maintenance", "notify", "access", "system"]) {
    await page.goto(`/?view=settings&tab=${tab}`);
    const results = await new AxeBuilder({ page }).withTags(["wcag2a", "wcag2aa"]).analyze();
    expect(results.violations.map((violation) => `${tab}: ${violation.id} ${violation.nodes.map((node) => node.html).join(" | ")}`)).toEqual([]);
  }
});

test("il tema chiaro mantiene leggibili stato salvato e sezione attiva", async ({ page }) => {
  await page.goto("/?view=settings&tab=daemon");
  await page.evaluate(() => document.documentElement.setAttribute("data-theme", "light"));
  const AxeBuilder = (await import("@axe-core/playwright")).default;
  const results = await new AxeBuilder({ page }).withRules(["color-contrast"]).analyze();
  expect(results.violations.map((violation) => violation.nodes.map((node) => node.html).join(" | "))).toEqual([]);
});

test("l'indice dei gruppi salta al gruppo scelto", async ({ page }) => {
  await page.goto("/?view=settings&tab=libtorrent");
  const index = page.getByRole("navigation", { name: "Gruppi di questa sezione" });
  await expect(index).toBeVisible();
  const chip = index.getByRole("link", { name: "Ricerca peer e tracker" });
  await expect(chip).toBeVisible();
  const target = await chip.getAttribute("href");
  await chip.click();
  await expect(page.locator(target!)).toBeInViewport();
});

test("il filtro «Solo modificate» mostra solo le impostazioni cambiate", async ({ page }) => {
  await page.goto("/?view=settings&tab=daemon");
  const rows = page.locator(".setting-row:visible");
  const before = await rows.count();
  expect(before).toBeGreaterThan(1);
  await page.locator("#input-refresh_interval").fill("7200");
  await page.getByLabel("Solo modificate").check();
  await expect(page.locator("#v2-setting-refresh_interval")).toBeVisible();
  // Only the changed row (and the group holding it) stays.
  await expect(rows).toHaveCount(1);
  await expect(page.locator(".settings-group-index")).toBeHidden();
  await page.getByLabel("Solo modificate").uncheck();
  await expect(rows).toHaveCount(before);
  await expect(page.locator(".settings-group-index")).toBeVisible();
});
