import { request } from "@playwright/test";

/**
 * A fresh data directory opens the first-run wizard instead of the shell, so
 * every check that expects the dashboard would fail. Mark the setup as
 * completed once, through the wizard's "skip" action, without changing anything
 * else: the tests rely on the daemon staying in dry-run.
 */
export default async function globalSetup() {
  const baseURL = process.env.PLAYWRIGHT_BASE_URL || "http://127.0.0.1:15003";
  const context = await request.newContext({
    baseURL,
    extraHTTPHeaders: { Origin: baseURL },
  });
  try {
    const response = await context.post("/setup/finish", { form: { action: "skip" } });
    if (!response.ok()) {
      throw new Error(`setup finish failed: HTTP ${response.status()}`);
    }
  } finally {
    await context.dispose();
  }
}
