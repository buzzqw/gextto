import { test, expect } from "@playwright/test";

const navViews: Record<string, string> = {
  "Blocklist": "blocklist",
  "Integrazioni": "integrations",
  "Archivio": "archive",
  "Configurazione": "settings",
  "Manutenzione": "maintenance",
  "Salute": "health",
  "Log": "logs",
  "Licenza": "license",
};
const nav = (page: import("@playwright/test").Page, label: string) =>
  page.locator(`aside .nav-item:not(.nav-search):not(.nav-mobile-system-extra)[href="/?view=${navViews[label]}"]`);

test("dashboard and configuration areas are usable", async ({ page }) => {
  await page.goto("/");

  await expect(page).toHaveTitle(/Gextto/);
  await expect(page.locator("h1").filter({ hasText: "Dashboard" })).toBeVisible();
  await expect(page.getByText("Ricerca automatica")).toBeVisible();
  await page.getByRole("button", { name: "Carica risultati" }).click();
  await expect(page.locator("#v2-dashboard-feed")).not.toContainText("Premi \"Carica risultati\"", { timeout: 15000 });

  await nav(page, "Blocklist").click();
  await expect(page.locator("h3").filter({ hasText: "Blocklist" })).toBeVisible();

  await nav(page, "Integrazioni").click();
  await expect(page.getByRole("heading", { name: "Simkl", exact: true })).toBeVisible();
  await expect(page.getByRole("link", { name: "Magnet handler" })).toBeVisible();

  await nav(page, "Archivio").click();
  await expect(page.locator("h1").filter({ hasText: "Archivio" })).toBeVisible();
  await expect(page.getByRole("table", { name: "Archivio" })).toBeVisible();

  await nav(page, "Configurazione").click();
  await page.getByRole("link", { name: "Traduzioni" }).click();
  await expect(page.getByRole("link", { name: "Esporta YAML" })).toBeVisible();

  await nav(page, "Manutenzione").click();
  await expect(page.getByRole("button", { name: "Backup ora" })).toBeVisible();

  // "Grafici"/"Attività" non sono più pagine a sé: i dati live sono in Salute/Log.
  await nav(page, "Salute").click();
  await expect(page.locator("h3").filter({ hasText: "Stato provider" })).toBeVisible();
  await nav(page, "Log").click();
  await expect(page.locator("h3").filter({ hasText: "Log daemon" })).toBeVisible();

  await nav(page, "Licenza").click();
  await expect(page.locator("h3").filter({ hasText: "Licenza" })).toBeVisible();
});
