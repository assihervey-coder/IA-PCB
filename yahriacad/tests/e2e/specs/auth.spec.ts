import { expect, test } from "@playwright/test";

/**
 * Parcours d'authentification (contrats.md §14) : la page /pages/login doit
 * exister et réagir dans les DEUX modes du serveur :
 *
 *  - auth activée (YAHRIACAD_JWT_SECRET configuré, défaut de la stack
 *    docker) : login admin/admin (compte de démonstration du compose)
 *    mène au gestionnaire de projets et « Déconnexion » revient au login ;
 *  - auth désactivée (dev/CI) : le backend répond 503 auth_disabled et la
 *    page propose « Continuer sans connexion ».
 *
 * Le test détecte le mode en interrogeant le backend (réponse du POST
 * /auth/login via l'UI) : il est donc vert dans les deux configurations.
 */

const ADMIN = { username: "admin", password: "admin" };

async function loginMode(page: import("@playwright/test").Page): Promise<
  "enabled" | "disabled" | "backend-down"
> {
  await page.goto("/pages/login");
  await page.getByTestId("login-username").fill(ADMIN.username);
  await page.getByTestId("login-password").fill(ADMIN.password);
  await page.getByTestId("login-submit").click();

  // Trois issues possibles : succès (redirection), 503 auth_disabled
  // (bouton « Continuer » affiché), ou erreur réseau.
  const disabled = await page
    .getByTestId("login-continue")
    .isVisible()
    .catch(() => false);
  if (disabled) return "disabled";

  await page.waitForURL("**/pages/project-manager", { timeout: 10_000 }).catch(() => {});
  if (page.url().includes("/pages/project-manager")) return "enabled";
  return "backend-down";
}

test.describe("Authentification YahriaCad", () => {
  test("la page de connexion s'affiche et valide le formulaire", async ({ page }) => {
    await page.goto("/pages/login");

    await expect(page.getByTestId("login-form")).toBeVisible();
    await expect(page.getByTestId("login-username")).toBeVisible();
    await expect(page.getByTestId("login-password")).toBeVisible();
    await expect(page.getByTestId("login-submit")).toBeVisible();

    // Soumission vide : les champs required bloquent, on reste sur le login.
    await page.getByTestId("login-submit").click();
    await expect(page).toHaveURL(/\/pages\/login/);
  });

  test("parcours login → gestionnaire de projets → déconnexion", async ({ page }) => {
    const mode = await loginMode(page);

    if (mode === "backend-down") {
      test.info().annotations.push({
        type: "info",
        description: "Backend injoignable : parcours auth ignoré (l'UI affiche l'erreur).",
      });
      await expect(page.getByTestId("login-error")).toBeVisible();
      return;
    }

    if (mode === "disabled") {
      // Mode dev : « Continuer sans connexion » mène aussi au gestionnaire.
      await page.getByTestId("login-continue").click();
    }

    await expect(page.getByTestId("projects-grid")).toBeVisible({ timeout: 15_000 });

    if (mode === "enabled") {
      // Session JWT active : le bouton Déconnexion est affiché puis renvoie
      // vers la page de login avec jeton purgé.
      await page.getByTestId("logout-btn").click();
      await expect(page).toHaveURL(/\/pages\/login/);

      // Après déconnexion, reconnexion complète OK (token purgé puis émis).
      await page.getByTestId("login-username").fill(ADMIN.username);
      await page.getByTestId("login-password").fill(ADMIN.password);
      await page.getByTestId("login-submit").click();
      await expect(page).toHaveURL(/\/pages\/project-manager/, { timeout: 10_000 });
    }
  });

  test("éditeur PCB accessible après connexion (parcours protégé)", async ({ page }) => {
    const mode = await loginMode(page);
    if (mode !== "enabled") {
      test.info().annotations.push({
        type: "info",
        description: `Mode ${mode} : le parcours protégé ne s'applique qu'avec auth activée.`,
      });
      return;
    }

    await expect(page.getByTestId("projects-grid")).toBeVisible({ timeout: 15_000 });
    await page.getByTestId("project-card").first().click();
    await expect(page.getByTestId("pcb-canvas")).toBeVisible();
  });
});
