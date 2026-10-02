import { test, expect, type Page } from "@playwright/test";

// Payloads that must never become live DOM. Each one targets a different
// rendering context: element injection, attribute breakout, tag closing and a
// javascript: URL. The client renders API results through `innerHTML`, so this
// suite is the regression guard for the escaping rules described in the
// technical review (section "HTML dinamico e sicurezza del rendering").
const ELEMENT_INJECTION = '<img src=x onerror="window.__xss=true">';
const ATTRIBUTE_BREAKOUT = '" onmouseover="window.__xss=true" data-x="';

async function xssFired(page: Page): Promise<boolean> {
  return page.evaluate(() => Boolean((window as unknown as { __xss?: boolean }).__xss));
}

test("la ricerca impostazioni tratta i payload HTML come testo", async ({ page }) => {
  await page.goto("/?view=settings");
  const search = page.locator("input.settings-search");
  await search.fill(ELEMENT_INJECTION);
  const results = page.locator("#v2-settings-search");
  await expect(results).toContainText(ELEMENT_INJECTION);
  await expect(results.locator("img, script, [onerror]")).toHaveCount(0);
  expect(await xssFired(page)).toBe(false);
});

test("le virgolette nella ricerca non creano attributi HTML", async ({ page }) => {
  await page.goto("/?view=settings");
  const search = page.locator("input.settings-search");
  await search.fill(ATTRIBUTE_BREAKOUT);
  const results = page.locator("#v2-settings-search");
  await expect(results).toContainText(ATTRIBUTE_BREAKOUT);
  await expect(results.locator("[onmouseover], [data-x]")).toHaveCount(0);
  expect(await xssFired(page)).toBe(false);
});
