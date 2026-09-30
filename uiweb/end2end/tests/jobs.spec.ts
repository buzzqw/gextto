import { test, expect } from "@playwright/test";

// The maintenance actions that moved to the background job manager answer 202
// with a job id. The client must follow the job to completion and report the
// real outcome instead of reloading right after "started".

test("un'azione con job_id riporta il completamento", async ({ page }) => {
  let polls = 0;
  await page.route("**/api/scan-all-archives", async (route) => {
    await route.fulfill({
      status: 202,
      contentType: "application/json",
      body: JSON.stringify({ ok: true, job_id: "test-job-1", message: "Scansione archivi avviata" }),
    });
  });
  await page.route("**/api/jobs/test-job-1", async (route) => {
    polls += 1;
    const state = polls < 2 ? "running" : "succeeded";
    await route.fulfill({
      contentType: "application/json",
      body: JSON.stringify({ job: { id: "test-job-1", kind: "scan-archives", state } }),
    });
  });

  await page.on("dialog", (dialog) => void dialog.accept());
  await page.goto("/?view=maintenance");
  await page.getByRole("button", { name: "Scansiona archivi" }).click();

  await expect(page.locator(".toast", { hasText: "Scansione archivi avviata" })).toBeVisible();
  await expect(page.locator(".toast", { hasText: "completata" })).toBeVisible({ timeout: 10000 });
  expect(polls).toBeGreaterThanOrEqual(2);
});

test("un job fallito riporta l'errore", async ({ page }) => {
  await page.route("**/api/scan-all-archives", async (route) => {
    await route.fulfill({
      status: 202,
      contentType: "application/json",
      body: JSON.stringify({ ok: true, job_id: "test-job-err", message: "Scansione archivi avviata" }),
    });
  });
  await page.route("**/api/jobs/test-job-err", async (route) => {
    await route.fulfill({
      contentType: "application/json",
      body: JSON.stringify({ job: { id: "test-job-err", kind: "scan-archives", state: "failed", error: "cartella non leggibile" } }),
    });
  });

  await page.on("dialog", (dialog) => void dialog.accept());
  await page.goto("/?view=maintenance");
  await page.getByRole("button", { name: "Scansiona archivi" }).click();

  await expect(page.locator(".toast", { hasText: "non riuscita" })).toBeVisible({ timeout: 10000 });
  await expect(page.locator(".toast", { hasText: "cartella non leggibile" })).toBeVisible();
});

test("un job in corso offre il pulsante Annulla", async ({ page }) => {
  let canceled = false;
  await page.route("**/api/scan-all-archives", async (route) => {
    await route.fulfill({
      status: 202,
      contentType: "application/json",
      body: JSON.stringify({ ok: true, job_id: "test-job-cancel", message: "Scansione archivi avviata" }),
    });
  });
  await page.route("**/api/jobs/test-job-cancel/cancel", async (route) => {
    canceled = true;
    await route.fulfill({ status: 202, contentType: "application/json", body: JSON.stringify({ ok: true }) });
  });
  await page.route("**/api/jobs/test-job-cancel", async (route) => {
    await route.fulfill({
      contentType: "application/json",
      body: JSON.stringify({ job: { id: "test-job-cancel", kind: "scan-archives", state: canceled ? "canceled" : "running" } }),
    });
  });

  await page.on("dialog", (dialog) => void dialog.accept());
  await page.goto("/?view=maintenance");
  await page.getByRole("button", { name: "Scansiona archivi" }).click();

  const cancelButton = page.locator(".toast button", { hasText: "Annulla" });
  await expect(cancelButton).toBeVisible();
  await cancelButton.click();

  await expect(page.locator(".toast", { hasText: "annullata" })).toBeVisible({ timeout: 10000 });
  expect(canceled).toBe(true);
});
