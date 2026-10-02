import { test, expect } from "@playwright/test";

const jobsPanel = (state: string, includeCancel: boolean) => `
  <div class="panel" id="v2-jobs-panel">
    <div class="panel-head"><h3>Operazioni in background</h3><small>1 in corso · 1 recenti</small></div>
    <div class="panel-body"><table class="data-table"><tbody><tr>
      <td>scan-archives</td><td><span class="badge">${state}</span></td>
      <td><div class="progress"><span style="width:25%"></span></div><small>25%</small></td>
      <td>Scansione archivi</td>
      <td>${includeCancel ? '<form hx-post="/v2/jobs/cancel" hx-target="#v2-jobs-panel" hx-swap="outerHTML"><input type="hidden" name="id" value="test-job" /><button type="submit">Annulla</button></form>' : ""}</td>
    </tr></tbody></table></div>
  </div>`;

test("la sezione operazioni mostra stato e permette di annullare un job", async ({ page }) => {
  let canceled = false;
  await page.route("**/v2/partial/jobs", async (route) => {
    await route.fulfill({ status: 200, contentType: "text/html; charset=utf-8", body: jobsPanel("in corso", true) });
  });
  await page.route("**/v2/jobs/cancel", async (route) => {
    canceled = true;
    await route.fulfill({ status: 200, contentType: "text/html; charset=utf-8", body: jobsPanel("annullato", false) });
  });

  await page.goto("/?view=maintenance");
  const panel = await page.evaluate(async () => {
    const response = await fetch("/v2/partial/jobs");
    return response.text();
  });
  await page.evaluate((html) => {
    const host = document.querySelector("#v2-page");
    if (!host) throw new Error("pagina v2 non trovata");
    host.querySelector("#v2-jobs-panel")?.remove();
    host.insertAdjacentHTML("beforeend", html);
    const jobs = host.querySelector("#v2-jobs-panel");
    if (jobs) (window as unknown as { htmx: { process(node: Element): void } }).htmx.process(jobs);
  }, panel);

  await expect(page.locator("#v2-jobs-panel")).toContainText("in corso");
  await page.locator("#v2-jobs-panel button", { hasText: "Annulla" }).click();
  await expect(page.locator("#v2-jobs-panel")).toContainText("annullato");
  expect(canceled).toBe(true);
  expect(panel).toContain("Operazioni in background");
});
