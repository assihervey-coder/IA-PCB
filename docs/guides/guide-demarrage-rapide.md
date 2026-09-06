# Guide de démarrage rapide — KidCAD-Pro-IA

> De l'installation aux Gerbers en ~15 minutes, avec le projet démo
> **« Kit LED Chaser 10 voies »** (NE555 + CD4017 + 10 LEDs).

## 1. Installer

Prérequis : [Bun](https://bun.sh) ≥ 1.1 (ou Node.js ≥ 20), Docker optionnel.

```bash
# Cloner puis installer les dépendances
git clone <url-du-depot> kidcad-pro-ia
cd kidcad-pro-ia
bun install
```

Ou tout automatiser :

```bash
./scripts/setup-dev.sh
```

Ce script installe les dépendances de la racine **et** du service IA
(`mini-services/ai-engine`), puis applique le schéma Prisma.

## 2. Préparer la base de données

```bash
bun run db:push
```

Crée/actualise `db/custom.db` (SQLite) à partir de `prisma/schema.prisma`.

## 3. Lancer l'application

Terminal 1 — l'app Next.js (port 3000) :

```bash
bun run dev
```

Terminal 2 — le service IA socket.io (port 3010) :

```bash
cd mini-services/ai-engine
bun install
bun run dev
```

Vérification : <http://localhost:3000/api/health> doit répondre
`{"status":"ok","aiEngine":"up"}` (ou `degraded` si le service IA n'est pas lancé —
l'éditeur reste utilisable, seuls les jobs IA sont indisponibles).

> Alternative : `make dev-all` affiche les instructions, ou
> `docker compose -f docker/docker-compose.yml up --build` pour tout lancer en conteneurs.

## 4. Créer le projet démo « Kit LED Chaser 10 voies »

1. Ouvrir <http://localhost:3000> ;
2. Dans la barre d'activités à gauche, cliquer **Projets** ;
3. Cliquer **Nouveau projet** ;
4. Renseigner :
   - **Nom** : `Kit LED Chaser 10 voies`
   - **Description** : `Chenillard NE555 + CD4017, 10 LEDs`
   - **Modèle** : `led-chaser`
5. Valider.

Le schéma pré-rempli contient : U1 (NE555, DIP-8), U2 (CD4017, DIP-16),
R1 4.7k, R2 1k, R3…R12 330 Ω, C1 10 µF, C2 10 nF, J1 (Header-2), D1…D10 (LED),
avec les nets VCC, GND, CLK, RESET, OUT0…OUT9, K0…K9, etc.

> La même netlist est disponible en KiCad dans
> `tests/fixtures/led-chaser.netlist.kicad.net` et importable via
> **Projets → Importer une netlist**.

## 5. Basculer en vue PCB

1. Cliquer **PCB** dans la barre d'activités ;
2. Au premier passage, définir le contour de carte (ex. 65 × 40 mm) ou utiliser
   **Outils → Contour automatique** ;
3. Les composants arrivent empilés au centre avec le **ratsnest** (fines lignes
   blanches = connexions à router) affiché par défaut.

## 6. Lancer le placement IA

1. Cliquer **Outils IA → Placement IA** (ou le bouton 🤖 de la barre PCB) ;
2. Laisser les options par défaut (graine 42, 20 000 itérations de recuit simulé) ;
3. Observer le **log IA en direct** (panneau inférieur) :

```text
[ai:progress] placement — 0 %   Initialisation…
[ai:progress] placement — 38 %  Recuit simulé T=412 K, HPWL=1842 mm
[ai:progress] placement — 100 % Placement terminé : HPWL=731 mm, chevauchements=0
[ai:result]   stats: { timeMs: 3120, iterations: 20000, hpwlMm: 731 }
```

4. Les composants se déplacent en temps réel dans le canvas pendant le recuit.

## 7. Lancer le routage IA

1. Cliquer **Outils IA → Routage IA** ;
2. Options par défaut : grille 0.635 mm, coût de via 30, rip-up & reroute 5 passes ;
3. Suivre la progression :

```text
[ai:progress] routing — 12 %  Net VCC routé sur F.Cu (18 segments)
[ai:progress] routing — 57 %  Net CLK : rip-up de 2 segments, reroute via B.Cu
[ai:progress] routing — 100 % 26/26 nets routés, 14 vias
[ai:result]   stats: { routedNets: 26, totalNets: 26, vias: 14, lengthMm: 612 }
```

4. Les pistes apparaissent : **rouge = F.Cu**, **bleu = B.Cu**, les vias sont les
   petits disques bicolores.

*(Optionnel)* **Outils IA → Optimiser** réduit le nombre de vias.

## 8. Vérifier avec le DRC

1. Cliquer **Vérification** dans la barre d'activités ;
2. Cliquer **Lancer DRC** (et **Lancer ERC** pour le schéma) ;
3. Lire le rapport :

| Colonne | Signification |
|---|---|
| Type | `MIN_TRACK_WIDTH`, `MIN_CLEARANCE`, `VIA_DRILL`, `EDGE_CLEARANCE`… |
| Gravité | `error` (bloquant) / `warning` (à considérer) |
| Localisation | Coordonnées mm ; un clic centre le canvas sur la violation |

Règles par défaut : piste ≥ 0.25 mm, isolation ≥ 0.2 mm, perçage via ≥ 0.35 mm,
diamètre via ≥ 0.7 mm, bord ≥ 0.3 mm.

4. Corriger les violations (déplacer un composant, ré-router le net en cause avec
   le clic droit → **Router ce net**), relancer le DRC jusqu'à **✅ Aucune violation**.

## 9. Exporter les Gerbers

1. Cliquer **Export** dans la barre d'activités ;
2. Choisir **Gerber (.zip)** → le fichier `led-chaser-gerbers.zip` contient :

| Fichier | Couche |
|---|---|
| `*.F_Cu.gbr` (GTL) | Cuivre face supérieure |
| `*.B_Cu.gbr` (GBL) | Cuivre face inférieure |
| `*.F_Mask.gbr` (GTS) | Masque de soudure top |
| `*.B_Mask.gbr` (GBS) | Masque de soudure bottom |
| `*.F_Silk.gbr` (GTO) | Sérigraphie top |
| `*.B_Silk.gbr` (GBO) | Sérigraphie bottom |
| `*.Edge_Cuts.gbr` (GKO) | Contour de carte |
| `*.drl` | Perçages (Excellon) |

3. Envoyer le ZIP tel quel à un fabricant (JLCPCB, PCBWay, Aisler…).

Autres exports disponibles : **BOM CSV**, **STEP** (mécanique), **STL**, **JSON**
(sauvegarde native), **Netlist KiCad**.

## 🎉 Et après ?

- Guide complet de l'interface : [`guide-utilisateur.md`](./guide-utilisateur.md)
- Architecture et ADR : [`../architecture/architecture.md`](../architecture/architecture.md)
- Référence API : [`../api/openapi.yaml`](../api/openapi.yaml)
