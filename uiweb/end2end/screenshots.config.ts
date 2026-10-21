import { devices, defineConfig } from "@playwright/test";

/**
 * Screenshots for the README and the guided tour (docs/TOUR.*.md). Not a test
 * suite: scripts/screenshots.sh starts a real test instance with a few legal
 * torrents downloading, then runs this config against it. No webServer here,
 * the script owns the instance.
 */
const port = Number(process.env.GEXTTO_SHOTS_PORT || 15023);

export default defineConfig({
  testDir: "./screenshots",
  timeout: 10 * 60 * 1000,
  expect: { timeout: 15000 },
  fullyParallel: false,
  workers: 1,
  retries: 0,
  reporter: [["line"]],
  outputDir: process.env.GEXTTO_SHOTS_RESULTS || "test-results-screenshots",
  use: {
    baseURL: `http://127.0.0.1:${port}`,
    ...devices["Desktop Chrome"],
    channel: "chrome",
    viewport: { width: 1440, height: 900 },
    colorScheme: "dark",
    actionTimeout: 20000,
    navigationTimeout: 30000,
  },
});
