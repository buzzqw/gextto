import { test, expect } from "@playwright/test";

const jobsPanel = (state: string, includeCancel: boolean) => `
  <div class="panel" id="v2-jobs-panel">
    <div class="panel-head"><h3>Operazioni in background</h3><small>1 in corso · 1 recenti</small></div>
    <div class="panel-body"><div class="table-wrap"><table class="data-table jobs-table"><tbody><tr>
      <td class="job-kind">scan-archives</td><td class="job-state"><span class="badge">${state}</span></td>
      <td class="job-progress-cell"><div class="progress" role="progressbar" aria-valuemin="0" aria-valuemax="100" aria-valuenow="25"><span style="width:25%"></span></div><small>25%</small></td>
      <td class="job-message truncate">Scansione archivi</td>
      <td class="row-actions job-actions">${includeCancel ? '<form hx-post="/jobs/cancel" hx-target="#v2-jobs-panel" hx-swap="outerHTML"><input type="hidden" name="id" value="test-job" /><button type="submit">Annulla</button></form>' : ""}</td>
    </tr></tbody></table></div></div>
  </div>`;

async function insertJobsPanel(page: import("@playwright/test").Page, html: string) {
  await page.evaluate((markup) => {
    const host = document.querySelector("#v2-page");
    if (!host) throw new Error("pagina v2 non trovata");
    host.querySelector("#v2-jobs-panel")?.remove();
    host.insertAdjacentHTML("beforeend", markup);
    const jobs = host.querySelector("#v2-jobs-panel");
    if (jobs) (window as unknown as { htmx: { process(node: Element): void } }).htmx.process(jobs);
  }, html);
}

test("la sezione operazioni mostra stato e permette di annullare un job", async ({ page }) => {
  let canceled = false;
  await page.route("**/partial/jobs", async (route) => {
    await route.fulfill({ status: 200, contentType: "text/html; charset=utf-8", body: jobsPanel("in corso", true) });
  });
  await page.route("**/jobs/cancel", async (route) => {
    canceled = true;
    await route.fulfill({ status: 200, contentType: "text/html; charset=utf-8", body: jobsPanel("annullato", false) });
  });

  await page.goto("/?view=maintenance");
  const panel = await page.evaluate(async () => {
    const response = await fetch("/partial/jobs");
    return response.text();
  });
  await insertJobsPanel(page, panel);

  await expect(page.locator("#v2-jobs-panel")).toContainText("in corso");
  await page.locator("#v2-jobs-panel button", { hasText: "Annulla" }).click();
  await expect(page.locator("#v2-jobs-panel")).toContainText("annullato");
  expect(canceled).toBe(true);
  expect(panel).toContain("Operazioni in background");
});

test.describe("operazioni in background su un telefono", () => {
  test.use({ viewport: { width: 390, height: 844 }, isMobile: true, hasTouch: true });

  test("ogni operazione sta nella larghezza dello schermo", async ({ page }) => {
    await page.goto("/?view=maintenance");
    await insertJobsPanel(page, jobsPanel("in corso", true));

    const panel = page.locator("#v2-jobs-panel");
    await expect(panel).toContainText("Scansione archivi");
    await expect(panel.locator(".job-message")).toBeVisible();
    await expect(panel.locator(".job-actions button")).toBeVisible();

    // Nothing scrolls sideways: the message and the Cancel button end inside
    // the panel instead of past its right edge.
    const overflow = await panel
      .locator(".table-wrap")
      .evaluate((wrap) => wrap.scrollWidth - wrap.clientWidth);
    expect(overflow).toBeLessThanOrEqual(1);
    const panelBox = await panel.boundingBox();
    const viewport = page.viewportSize();
    if (!panelBox || !viewport) throw new Error("geometria non disponibile");
    expect(panelBox.x + panelBox.width).toBeLessThanOrEqual(viewport.width);
    for (const selector of [".job-message", ".job-actions button"]) {
      const box = await panel.locator(selector).boundingBox();
      if (!box) throw new Error(`${selector} senza geometria`);
      expect(box.x + box.width).toBeLessThanOrEqual(panelBox.x + panelBox.width + 1);
    }
  });
});
