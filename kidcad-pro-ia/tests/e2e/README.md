# Tests E2E — KidCAD-Pro-IA (Playwright)

Parcours de bout en bout du frontend, en s'appuyant sur les `data-testid`
normalisés (`contracts.md` §8) : grille de projets, création de projet,
éditeur PCB et lancement d'un routage IA avec suivi de progression.

## Prérequis

- Node.js >= 20 et npm.
- Le frontend (`frontend/`) doit avoir ses dépendances installées :
  `cd frontend && npm install`.
- Backend + moteur IA optionnels : sans API joignable, l'application bascule
  en **mode démo** (projet d'exemple, bannière `demo-banner`) et les tests
  restent verts — ils sont écrits pour tolérer les deux modes.

## Installation

```bash
cd tests/e2e
npm install
npx playwright install chromium
```

## Exécution

```bash
npm test
```

La configuration (`playwright.config.ts`) démarre automatiquement le frontend
en mode dev (`npm --prefix ../../frontend run dev`, jusqu'à 120 s) et réutilise
un serveur déjà lancé sur `http://localhost:3000` (`reuseExistingServer: true`).

Pour lancer les tests avec la stack complète (recommandé pour le parcours de
routage réel) :

```bash
docker compose -f docker/docker-compose.yml up -d --build
cd tests/e2e && npm test
```

## Ce que couvrent les specs

| Test | Parcours |
|---|---|
| `affiche la grille de projets` | `/pages/project-manager` → grille `projects-grid` visible (mode démo accepté). |
| `crée un projet (mode démo ou API)` | bouton `create-project-btn` → formulaire (`project-form-name`, `project-form-submit`) → carte `project-card` contenant « Carte Test » sous 10 s. |
| `ouvre l'éditeur PCB et lance le routage` | ouverture du premier `project-card` → `pcb-canvas` visible → page `/pages/pcb-layout/router` → `route-start-btn` → `progress-bar` visible. |

## Astuces

- `npx playwright test --headed` pour voir le navigateur.
- `npx playwright test --trace on` puis `npx playwright show-report` pour
  inspecter un échec (trace conservée en cas d'échec).
- En CI, installez les navigateurs en cache : `npx playwright install --with-deps chromium`.
