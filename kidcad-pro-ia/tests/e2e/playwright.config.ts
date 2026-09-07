import { defineConfig, devices } from "@playwright/test";

/**
 * Configuration Playwright — KidCAD-Pro-IA.
 * Le frontend est démarré en mode dev (npm run dev du frontend) ; si un
 * serveur tourne déjà sur http://localhost:3000, il est réutilisé.
 */
export default defineConfig({
  testDir: "./specs",
  timeout: 60_000,
  retries: 0,
  reporter: [["list"]],
  use: {
    baseURL: "http://localhost:3000",
    trace: "retain-on-failure",
  },
  projects: [
    {
      name: "chromium",
      use: { ...devices["Desktop Chrome"] },
    },
  ],
  webServer: {
    command: "npm --prefix ../../frontend run dev",
    url: "http://localhost:3000",
    reuseExistingServer: true,
    timeout: 120_000,
  },
});
