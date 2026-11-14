import { test, expect } from "@playwright/test";

// Regression guard for the "language problem" fixed in the technical review:
// the English catalog used to miss ~1.345 strings already covered by de/fr/es/pl,
// so the English UI silently fell back to Italian. A table header is a stable
// probe because it is rendered client-side and then translated by the catalog.
// The language selector lives in the "Aspetto" menu of the top bar: open it,
// choose, and let the form reload the page.
async function setLanguage(page: import("@playwright/test").Page, code: string) {
  await page.locator(".appearance-toggle").click();
  await page.locator(".lang-select").selectOption(code);
  await page.waitForLoadState("domcontentloaded");
}

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
    await setLanguage(page, language.code);
    await expect(head.locator("th").first()).toHaveText(language.title, { timeout: 15000 });
    await expect(head.locator("th").nth(1)).toHaveText(language.reason, { timeout: 15000 });
  }

  // Leave the shared test server on its default language for the other specs.
  await setLanguage(page, "it");
  await expect(head.locator("th").first()).toHaveText("Titolo", { timeout: 15000 });
});

test("il tab SQL dell'archivio è tradotto in tutte le lingue", async ({ page }) => {
  // The language selector lives in the "Aspetto" menu of the top bar: open it,
// choose, and let the form reload the page.
async function setLanguage(page: import("@playwright/test").Page, code: string) {
  await page.locator(".appearance-toggle").click();
  await page.locator(".lang-select").selectOption(code);
  await page.waitForLoadState("domcontentloaded");
}

const languages = [
    { code: "en", tab: "Archive SQL maintenance", language: "Language for the operations" },
    { code: "de", tab: "SQL-Wartung des Archivs", language: "Sprache für die Vorgänge" },
    { code: "fr", tab: "Maintenance SQL de l'archive", language: "Langue des opérations" },
    { code: "es", tab: "Mantenimiento SQL del archivo", language: "Idioma de las operaciones" },
    { code: "pl", tab: "Konserwacja SQL archiwum", language: "Język operacji" },
  ];
  await page.goto("/?view=maintenance&tab=archive-sql");
  await expect(page.getByRole("tab", { name: "Manutenzione SQL dell'archivio" })).toBeVisible();
  for (const language of languages) {
    await setLanguage(page, language.code);
    await expect(page.getByRole("tab", { name: language.tab })).toBeVisible({ timeout: 15000 });
    await expect(page.locator('label[for="v2-archive-sql-language"]')).toHaveText(language.language, { timeout: 15000 });
  }
  // Leave the shared test server on its default language for the other specs.
  await setLanguage(page, "it");
  await expect(page.getByRole("tab", { name: "Manutenzione SQL dell'archivio" })).toBeVisible({ timeout: 15000 });
});
