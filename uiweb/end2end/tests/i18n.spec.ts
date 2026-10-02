import { test, expect } from "@playwright/test";

// Regression guard for the "language problem" fixed in the technical review:
// the English catalog used to miss ~1.345 strings already covered by de/fr/es/pl,
// so the English UI silently fell back to Italian. A table header is a stable
// probe because it is rendered client-side and then translated by the catalog.
const languages = [
  { code: "en", title: "Title", reason: "Reason" },
  { code: "de", title: "Titel", reason: "Grund" },
  { code: "fr", title: "Titre", reason: "Raison" },
  { code: "es", title: "Título", reason: "Motivo" },
  { code: "pl", title: "Tytuł", reason: "Powód" },
];

test("le lingue complete traducono le intestazioni delle tabelle", async ({ page }) => {
  await page.goto("/?view=blocklist");
  const head = page.locator("main table.data-table thead").first();
  await expect(head.locator("th").first()).toHaveText("Titolo");

  for (const language of languages) {
    await page.locator(".lang-select").selectOption(language.code);
    await page.waitForLoadState("domcontentloaded");
    await expect(head.locator("th").first()).toHaveText(language.title, { timeout: 15000 });
    await expect(head.locator("th").nth(1)).toHaveText(language.reason, { timeout: 15000 });
  }

  // Leave the shared test server on its default language for the other specs.
  await page.locator(".lang-select").selectOption("it");
  await page.waitForLoadState("domcontentloaded");
  await expect(head.locator("th").first()).toHaveText("Titolo", { timeout: 15000 });
});
