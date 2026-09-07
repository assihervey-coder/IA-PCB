import { expect, test } from "@playwright/test";

/**
 * Parcours projet de bout en bout, tolérant au mode démo (API backend
 * injoignable) : les `data-testid` suivent contracts.md §8.
 */

test.describe("Parcours projet YahriaCad", () => {
  test("affiche la grille de projets", async ({ page }) => {
    await page.goto("/pages/project-manager");

    // En mode démo la bannière est affichée ; la grille doit l'être dans
    // tous les cas (API réelle ou démo).
    await expect(page.getByTestId("projects-grid")).toBeVisible();
  });

  test("crée un projet (mode démo ou API)", async ({ page }) => {
    await page.goto("/pages/project-manager");
    await expect(page.getByTestId("projects-grid")).toBeVisible();

    await page.getByTestId("create-project-btn").click();
    await page.getByTestId("project-form-name").fill("Carte Test");
    await page.getByTestId("project-form-submit").click();

    // Une carte projet portant le nom saisi doit apparaître sous 10 s.
    await expect(
      page
        .getByTestId("project-card")
        .filter({ hasText: "Carte Test" })
        .first()
    ).toBeVisible({ timeout: 10_000 });
  });

  test("ouvre l'éditeur PCB et lance le routage", async ({ page }) => {
    await page.goto("/pages/project-manager");
    await expect(page.getByTestId("projects-grid")).toBeVisible();

    await page.getByTestId("project-card").first().click();

    // L'éditeur PCB doit afficher le canvas (page par défaut de l'espace
    // pcb-layout). En mode démo, le projet d'exemple est chargé.
    await expect(page.getByTestId("pcb-canvas")).toBeVisible();

    // Page de routage IA du projet courant (identifiant lu dans l'URL si la
    // sélection l'a écrit, sinon paramètre tolérant du mode démo).
    const projectId =
      new URL(page.url()).searchParams.get("project") ?? "first";
    await page.goto(
      `/pages/pcb-layout/router?project=${encodeURIComponent(projectId)}`
    );
    await expect(page.getByTestId("pcb-canvas")).toBeVisible();

    await page.getByTestId("route-start-btn").click();

    // La progression doit apparaître (barre de progression temps réel).
    await expect(page.getByTestId("progress-bar")).toBeVisible();
  });
});
