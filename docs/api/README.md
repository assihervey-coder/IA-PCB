# API KidCAD-Pro-IA

Cette documentation décrit l'API REST de KidCAD-Pro-IA.

## Spécification

La référence complète est la spécification **OpenAPI 3.0** :
[`openapi.yaml`](./openapi.yaml).

Pour la lire et la tester :

- **Swagger Editor** (en ligne) : coller le contenu de `openapi.yaml` dans
  <https://editor.swagger.io> ;
- **Redoc** : `npx @redocly/cli build-docs docs/api/openapi.yaml` ;
- **Swagger UI** : `npx swagger-ui-watcher docs/api/openapi.yaml` ;
- tout client OpenAPI (Postman → *Import* → fichier OpenAPI).

## Base URL

| Environnement | Base URL |
|---|---|
| Développement | `http://localhost:3000/api` |
| Production | `https://kidcad.example.com/api` (voir `configs/production/appsettings.prod.json`) |

Tous les chemins listés dans la spec sont donc relatifs à `/api`
(ex. `GET {baseUrl}/projects/{id}`).

## Résumé des endpoints

| Méthode | Chemin | Rôle |
|---|---|---|
| GET | `/projects` | Liste des projets |
| POST | `/projects` | Création (`template: empty \| led-chaser`) |
| GET | `/projects/{id}` | Design complet (schematicJson, layoutJson, rulesJson) |
| PUT | `/projects/{id}` | Sauvegarde du design |
| DELETE | `/projects/{id}` | Suppression |
| POST | `/projects/{id}/drc` | Rapport DRC |
| POST | `/projects/{id}/erc` | Rapport ERC |
| POST | `/projects/{id}/import` | Import netlist (`kicad` \| `json`) |
| GET | `/projects/{id}/export/{format}` | `gerber` (ZIP) \| `bom` \| `step` \| `stl` \| `json` \| `netlist-kicad` |
| GET | `/footprints` | Bibliothèque d'empreintes |
| GET | `/health` | État des services |

## Temps réel (hors REST)

Les jobs IA ne sont pas exposés en REST : ils passent par **socket.io** sur le port 3010
(via la gateway : `io("/?XTransformPort=3010", { path: "/" })`), événements
`ai:place`, `ai:route`, `ai:optimize` → `ai:progress`, `ai:result`, `ai:error`.
Détails : `docs/architecture/adr-003-formats-echanges.md`.

## Authentification

**Aucune authentification** n'est requise en l'état (application mono-utilisateur locale).
Une auth future est prévue :

- Jetons **Bearer** (`Authorization: Bearer <token>`) via un middleware Next.js ;
- Clés d'API par projet pour l'automatisation ;
- Schéma `securitySchemes` à ajouter dans `openapi.yaml` (emplacement réservé :
  section `components`).

En attendant, une instance exposée publiquement doit être protégée au niveau du reverse
proxy (`configs/production/nginx.conf`) — cf. licence AGPL-3.0 et obligation de mise à
disposition des sources.
