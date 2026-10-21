import { test, expect, type Page, type APIRequestContext } from "@playwright/test";
import * as path from "path";

// Screenshots of a live test instance (scripts/screenshots.sh). Every image is
// written to docs/screenshots/<lang>/<name>.png with the same name in each
// language, so README and tour pages only change the folder.

const outRoot = process.env.GEXTTO_SHOTS_DIR || path.resolve(__dirname, "../../../docs/screenshots");
const gxURL = `http://127.0.0.1:${process.env.GEXTTO_SHOTS_GX_PORT || 18913}`;
const fullLanguages = (process.env.GEXTTO_SHOTS_LANGS || "en,it").split(",").filter(Boolean);
const dashboardLanguages = (process.env.GEXTTO_SHOTS_DASHBOARD_LANGS || "de,fr,es,pl").split(",").filter(Boolean);

function file(lang: string, name: string) {
  return path.join(outRoot, lang, `${name}.png`);
}

async function setLanguage(request: APIRequestContext, baseURL: string, lang: string) {
  const response = await request.post("/api/i18n/language", {
    data: { lang },
    headers: { Origin: baseURL },
  });
  expect(response.ok()).toBeTruthy();
}

// HTMX keeps polling, so "network idle" never comes: give the page a moment
// to swap in its live fragments instead.
async function open(page: Page, url: string) {
  await page.goto(url);
  await page.waitForLoadState("domcontentloaded");
  await page.waitForTimeout(2500);
  await sanitize(page);
}

// The shots are published: they must not carry the peers' addresses, the home
// folder of the machine that took them or its network shares. Peer IPs keep
// their first octet, the home becomes /home/gextto, and network mounts
// (nfs, cifs, smb) leave the disk lists. Run again after a live refresh.
const home = process.env.HOME || "";
async function sanitize(page: Page) {
  await page.evaluate((home) => {
    const ip = /(?<![\w/.-])(\d{1,3})\.\d{1,3}\.\d{1,3}\.\d{1,3}(:\d+)?\b/g;
    const ip6 = /\[?[0-9a-f]{1,4}(:[0-9a-f]{0,4}){3,7}\]?(:\d+)?/gi;
    const clean = (text: string) => {
      let out = text.replace(ip, (_m, first, port) => `${first}.x.x.x${port || ""}`).replace(ip6, "[2001:db8::x]");
      if (home) out = out.split(home).join("/home/gextto");
      return out;
    };
    const walker = document.createTreeWalker(document.body, NodeFilter.SHOW_TEXT);
    for (let node = walker.nextNode(); node; node = walker.nextNode()) {
      const text = node.nodeValue || "";
      const next = clean(text);
      if (next !== text) node.nodeValue = next;
    }
    for (const input of document.querySelectorAll<HTMLInputElement | HTMLTextAreaElement>("input, textarea")) {
      if (input.value) input.value = clean(input.value);
      if (input.placeholder) input.placeholder = clean(input.placeholder);
    }
    for (const el of document.querySelectorAll<HTMLElement>("li, tr, .kv, .row")) {
      if (el.children.length < 12 && /\b(nfs4?|cifs|smb3?|fuse\.sshfs)(?![a-z])/.test(el.textContent || "")) el.remove();
    }
  }, home);
}

async function view(page: Page, lang: string, name: string, url: string) {
  await open(page, url);
  await page.screenshot({ path: file(lang, name) });
}

// The floating Donate button would sit on top of a cropped detail.
async function hideFloating(page: Page) {
  await page.addStyleTag({ content: ".v2-donate { display: none !important; }" });
}

for (const lang of fullLanguages) {
  test.describe(`tour ${lang}`, () => {
    test.beforeAll(async ({ request, baseURL }) => {
      await setLanguage(request, baseURL!, lang);
    });

    test(`desktop pages ${lang}`, async ({ page }) => {
      await view(page, lang, "dashboard", "/?view=dashboard");
      await view(page, lang, "series", "/?view=series");
      await view(page, lang, "movies", "/?view=movies");
      await view(page, lang, "integrations", "/?view=integrations");
      await view(page, lang, "maintenance", "/?view=maintenance");
      await view(page, lang, "health", "/?view=health");
      await view(page, lang, "logs", "/?view=logs");
      await view(page, lang, "settings", "/?view=settings");
      await view(page, lang, "setup", "/?view=setup");
    });

    test(`downloads ${lang}`, async ({ page }) => {
      // Taller than the other pages: the search box sits above the list.
      await page.setViewportSize({ width: 1440, height: 1220 });
      await view(page, lang, "downloads", "/?view=downloads");
    });

    test(`light theme ${lang}`, async ({ page }) => {
      await open(page, "/?view=dashboard");
      await page.evaluate(() => document.documentElement.setAttribute("data-theme", "light"));
      await page.waitForTimeout(500);
      await page.screenshot({ path: file(lang, "dashboard-light") });
    });

    test(`torrent details ${lang}`, async ({ page }) => {
      await open(page, "/?view=downloads");
      await page.locator("button.torrent-open", { hasText: /debian/i }).first().click();
      const drawer = page.locator("#v2-detail-drawer");
      await expect(drawer).not.toBeEmpty();
      await hideFloating(page);
      await drawer.locator('[hx-get*="tab=pieces"]').first().click();
      await page.waitForTimeout(2000);
      await drawer.locator('[hx-get*="tab=general"]').first().scrollIntoViewIfNeeded();
      await drawer.screenshot({ path: file(lang, "detail") });
      await drawer.locator('[hx-get*="tab=peers"]').first().click();
      await page.waitForTimeout(2000);
      await sanitize(page);
      await drawer.screenshot({ path: file(lang, "detail-peers") });
    });

    test(`bandwidth menu ${lang}`, async ({ page }) => {
      await page.setViewportSize({ width: 1440, height: 1150 });
      await open(page, "/?view=downloads");
      await hideFloating(page);
      const menu = page.locator("details.speed-menu");
      await menu.evaluate((el) => {
        el.scrollIntoView({ block: "start" });
        window.scrollBy(0, -40);
      });
      await menu.locator("summary").first().click();
      await menu.locator("details.speed-sub > summary").first().click();
      await page.waitForTimeout(500);
      const summary = (await menu.locator("summary").first().boundingBox())!;
      const panel = (await page.locator("#v2-speed-panel").boundingBox())!;
      const x = Math.max(0, panel.x - 320);
      const y = Math.max(0, summary.y - 16);
      const right = Math.max(summary.x + summary.width, panel.x + panel.width) + 20;
      const bottom = panel.y + panel.height + 16;
      await page.screenshot({ path: file(lang, "bandwidth"), clip: { x, y, width: right - x, height: bottom - y } });
    });

    test(`speed rule ${lang}`, async ({ page }) => {
      await open(page, "/?view=settings&tab=network");
      await hideFloating(page);
      const panel = page.locator("#v2-setting-torrent_speed_mode").locator("xpath=ancestor::div[contains(@class,'settings-panel')][1]");
      // Scroll the panel just below the sticky header, which would cover it.
      await panel.evaluate((el) => {
        el.scrollIntoView({ block: "start" });
        let covered = 0;
        for (const node of document.querySelectorAll<HTMLElement>("body *")) {
          const style = getComputedStyle(node);
          if (style.position !== "sticky" && style.position !== "fixed") continue;
          const rect = node.getBoundingClientRect();
          if (rect.top <= 1 && rect.width > 400 && rect.bottom < window.innerHeight / 2) covered = Math.max(covered, rect.bottom);
        }
        window.scrollBy(0, -(covered + 8));
      });
      await page.waitForTimeout(500);
      const box = (await panel.boundingBox())!;
      await page.screenshot({ path: file(lang, "speed-rule"), clip: { x: box.x, y: Math.max(0, box.y), width: box.width, height: Math.min(box.height, 640) } });
    });

    // The managed daemon's page speaks the language Gextto had when it started
    // gx-torrent (the script sets English before that): its live fragments do
    // not carry ?lang, so another language would come out mixed.
    test(`gx-torrent page ${lang}`, async ({ page }) => {
      test.skip(lang !== "en", "gx-torrent runs in English for the shots");
      await page.goto(`${gxURL}/`);
      await page.waitForTimeout(2500);
      await page.screenshot({ path: file(lang, "gx-torrent") });
    });
  });

  test.describe(`phone ${lang}`, () => {
    test.use({ viewport: { width: 400, height: 860 }, isMobile: true, hasTouch: true, deviceScaleFactor: 1 });

    test.beforeAll(async ({ request, baseURL }) => {
      await setLanguage(request, baseURL!, lang);
    });

    test(`phone pages ${lang}`, async ({ page }) => {
      await view(page, lang, "phone", "/?view=downloads");
      await view(page, lang, "phone-dashboard", "/?view=dashboard");
    });
  });
}

test.describe("dashboard in the other languages", () => {
  for (const lang of dashboardLanguages) {
    test(`dashboard ${lang}`, async ({ page, request, baseURL }) => {
      await setLanguage(request, baseURL!, lang);
      await view(page, lang, "dashboard", "/?view=dashboard");
    });
  }

  test.afterAll(async ({ request, baseURL }) => {
    await setLanguage(request, baseURL!, fullLanguages[0] || "en");
  });
});
