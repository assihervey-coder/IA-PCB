# Guide des fonctions avancées — v0.2

Ce guide couvre les extensions additives de la plateforme : routage
multi-couches, plans de masse, classes de nets, collaboration CRDT,
intégration des modèles RL et la démonstration « carte cauchemar ».
Toutes les routes partent de la base `http://localhost:8080/api/v1`.

---

## 1. Démo : la carte cauchemar 🧟

Le moyen le plus rapide de voir toute la chaîne de réparation agir.

```bash
# Création du projet piégé (aucun paramètre requis)
curl -s -X POST localhost:8080/api/v1/demo/nightmare | jq
```

Le projet créé contient cinq fautes volontaires, toutes détectées par la
plateforme :

| Faute | Effet | Réparation |
|---|---|---|
| Piste VCC de 0.08 mm | violation `DRC_TRACK_WIDTH` | `widen_track` de l'Auto-Healer |
| Piste SCL à 0.2 mm du bord | violation `DRC_EDGE_CLEARANCE` | `nudge_edge` |
| Via perçage 0.12 mm | violation `DRC_DRILL`/`DRC_ANNULAR` | `enlarge_via` |
| Nets N1/N2 non routés | axe SI et DFM dégradés | `POST .../route` (RL/A*) |
| Aucune règle personnalisée | valeurs par défaut s'appliquent | `PUT .../netclasses/power/rules` |

Scénario complet :

```bash
ID=$(curl -s -X POST localhost:8080/api/v1/demo/nightmare | jq -r .project_id)
curl -s localhost:8080/api/v1/projects/$ID/doctor | jq '.score,.grade'      # ~55 / D
curl -s -X POST localhost:8080/api/v1/projects/$ID/drc/autofix | jq '.violations_before,.violations_after'
curl -s -X POST localhost:8080/api/v1/projects/$ID/route -H 'Content-Type: application/json' -d '{"strategy":"astar"}'
curl -s localhost:8080/api/v1/projects/$ID/doctor | jq '.score,.grade'      # le score a grimpé
```

---

## 2. Routage multi-couches et plans de masse 🗺️🟩

Le moteur IA reçoit la stack complète (`layer_count`, `layer_names`) et route
sur N couches : les vias de changement de couche sont insérés par le routeur
(A* ou RL) au besoin. Créez simplement un projet avec `layer_count` > 2 :

```bash
curl -s -X POST localhost:8080/api/v1/projects -d '{"name":"4 couches","layer_count":4}'
```

### Plans de masse (copper pours)

```bash
# Plan GND sur B.Cu + couture de vias vers les couches internes :
curl -s -X POST localhost:8080/api/v1/projects/<id>/pours \
  -H 'Content-Type: application/json' \
  -d '{"net":"GND","layers":[3],"clearance_mm":0.3,"stitch":true,"stitch_grid_mm":2.5}' | jq
```

Le générateur est déterministe : contour rectangulaire en retrait de la
marge de bord (règle `edge_clearance` du projet par défaut), remplissage
calculé par échantillonnage (pas 0.25–0.5 mm) en respectant l'isolement
autour de chaque piste/pad/via **étranger**, couture en quinconce avec cap à
400 vias. Les statistiques (`fill_pct`, `area_mm2`, `stitched`) sont
stockées dans le projet et voyagent dans le format d'échange, la persistance
SQL et l'API REST (`GET/PUT .../layout`, champ `pours`).

Conseils : re-remplissez après chaque routage (le même `POST .../pours`
remplace les plans du net), et préférez les couches internes pour les
masses sur les cartes ≥ 4 couches.

---

## 3. Classes de nets et autoroutage interactif 🏷️

```bash
# Vue d'ensemble : nets, classes, valeurs effectives
curl -s localhost:8080/api/v1/projects/<id>/netclasses | jq

# Classer un net (le schéma doit être importé)
curl -s -X PUT localhost:8080/api/v1/projects/<id>/netclasses/USB_D%2B \
  -H 'Content-Type: application/json' -d '{"net_class":"high-speed"}'

# Règles de la classe power : largeur 0.5 mm, isolation 0.35 mm
curl -s -X PUT localhost:8080/api/v1/projects/<id>/netclasses/power/rules \
  -H 'Content-Type: application/json' \
  -d '{"min_track_width_mm":0.5,"min_clearance_mm":0.35}'
```

Les valeurs effectives (règle de classe > règle globale > défaut moteur)
sont exactement celles transmises au moteur IA. L'autoroutage interactif
utilise la même route que d'habitude avec une sélection :

```bash
# Ne router que les nets d'alimentation :
curl -s -X POST localhost:8080/api/v1/projects/<id>/route \
  -H 'Content-Type: application/json' \
  -d '{"strategy":"astar","nets":["VCC","GND"]}'
```

La réponse est un job asynchrone : suivez `GET .../jobs/{jobID}` ou la
WebSocket `/ws/v1/progress?project_id=<id>` (un événement par net routé).

---

## 4. Collaboration multi-utilisateurs (CRDT) 👥

Plusieurs éditeurs peuvent travailler sur le même projet : les opérations
sont fusionnables (registres *last-writer-wins* par horloge de Lamport pour
les positions/rotations/règles, ensembles additifs dédupliqués pour le
cuivre) et diffusées en temps réel sur la WebSocket (`type:"collab"`).

```bash
# Alice déplace U1 et resserre l'isolation de la classe signal :
curl -s -X POST localhost:8080/api/v1/projects/<id>/collab/ops \
  -H 'Content-Type: application/json' -d '{
    "actor": "alice",
    "ops": [
      {"client_id": "op-1", "kind": "component.move", "target": "U1",
       "payload": {"x": 12, "y": 18}},
      {"client_id": "op-2", "kind": "constraint.width",
       "payload": {"mm": 0.4, "net_class": "signal"}}
    ]}' | jq

# Bob rattrape son retard après une reconnexion :
curl -s "localhost:8080/api/v1/projects/<id>/collab/state?since=42" | jq

# Undo / redo par acteur :
curl -s -X POST localhost:8080/api/v1/projects/<id>/collab/undo \
  -H 'Content-Type: application/json' -d '{"actor":"alice"}' | jq
curl -s -X POST localhost:8080/api/v1/projects/<id>/collab/redo \
  -H 'Content-Type: application/json' -d '{"actor":"alice"}' | jq
```

Le champ `client_id` rend chaque opération idempotente (retransmission =
rejet sans effet). L'historique undo/redo est **persistant** : le journal
JSONL sous `$KIDCAD_DATA_DIR/collab/<project>.jsonl` est rejoué au
redémarrage et reconstruit piles, horloges et registres — un undo fonctionne
donc après un restart du serveur.

---

## 5. Modèles RL entraînés (optionnel) 🤖

Le moteur IA fonctionne sans torch (repli A* déterministe). Quand un
checkpoint entraîné est présent, le routeur RL prend le relais :

```bash
make train-router STEPS=200000      # produit ai-engine/training/router/model_v1.pt
make run-ai                          # redémarrage : le checkpoint est chargé
curl -s localhost:8080/healthz | jq  # "ai_model_loaded": true, "ai_device": "cpu"
curl -s -X POST localhost:8080/api/v1/projects/<id>/route \
  -H 'Content-Type: application/json' -d '{"strategy":"rl"}'
```

Sans modèle chargé, `strategy: "rl"` retombe automatiquement sur A* : aucune
configuration à changer. Le Design Doctor affiche l'état du moteur et, si
des nets manquent, une **répétition sandbox** :

```bash
curl -s localhost:8080/api/v1/projects/<id>/doctor | jq '.ai, .rehearsal'
# "ai":        {"reachable": true, "device": "cpu", "model_loaded": true, "strategy": "rl"}
# "rehearsal": {"attempted": 2, "completed": 2, "predicted_length_mm": 41.2, ...}
```

La répétition ne modifie jamais la carte : elle chiffre ce qu'un clic sur
`POST .../route` produirait.
