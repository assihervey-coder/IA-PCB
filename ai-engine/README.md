# R&D — Entraînement RL pour le routage/placement PCB (PyTorch)

> ⚠️ **Ce dossier n'est PAS requis pour utiliser YahriaCad.**
> L'application en production utilise un **moteur d'inférence TypeScript
> déterministe** embarqué (A\* maze-routing 2 couches + recuit simulé +
> rip-up & reroute, voir `src/lib/yahriacad/ai-engine/`).
> Ce dossier est le **pipeline de recherche & développement** (référence) pour
> l'entraînement de politiques de reinforcement learning qui, un jour, pourraient
> remplacer ou guider les heuristiques TS. Voir
> [`docs/architecture/adr-002-moteur-ia.md`](../docs/architecture/adr-002-moteur-ia.md).

## Contenu

```text
ai-engine/
├── requirements.txt           # torch>=2.2, numpy, tqdm, matplotlib, tensorboard
├── train.py                   # (à venir) boucle d'entraînement PPO multi-config
├── src/
│   ├── environment/
│   │   ├── pcb_env.py         # Environnement type Gymnasium (grille 2 couches)
│   │   ├── reward.py          # Fonctions de récompense paramétrables
│   │   └── action_space.py    # Espace d'actions discret (6 actions)
│   ├── agents/
│   │   ├── base_agent.py      # Classe abstraite (select_action, learn, save, load)
│   │   └── ppo_agent.py       # PPO : actor-critic MLP, GAE, clipping
│   ├── models/
│   │   └── graph_net.py       # GNN léger (message passing sur graphe de nets)
│   └── service.py             # Client socket.io-python : brancher un modèle entraîné
├── training/
│   ├── router/config.yaml     # Hyperparamètres du routeur
│   ├── placer/config.yaml     # Hyperparamètres du placeur
│   └── optimizer/config.yaml  # Hyperparamètres de l'optimiseur de vias
├── evaluation/
│   ├── metrics.py             # Métriques (longueur, vias, complétion, DRC)
│   └── bench.py               # Benchmark CLI sur N grilles aléatoires
└── data/                      # raw / processed / augmentations (gitignorés)
```

## Installation

```bash
cd ai-engine
python3 -m venv venv
source venv/bin/activate
pip install -r requirements.txt
```

(ou simplement `./scripts/generate-models.sh` à la racine du dépôt)

## Entraîner

```bash
source venv/bin/activate
python train.py --config training/router/config.yaml
```

La boucle d'entraînement (PPO) optimise une politique sur l'environnement
`src/environment/pcb_env.py` (routage d'un net sur grille 2 couches). Les runs
TensorBoard sont écrits dans `training/<run>/logs/` :

```bash
tensorboard --logdir training/router
```

## Où sortent les modèles `.pt`

```text
training/router/model_v1.pt      # politique de routage (PpoAgent.save)
training/router/model_v2.pt      # itérations suivantes / checkpoints
training/placer/model_v1.pt      # politique de placement (à venir)
training/optimizer/model_v1.pt   # politique d'optimisation de vias (à venir)
```

Conventions : un checkpoint = état `state_dict` de l'acteur-critic + métadonnées
(hyperparamètres, version de l'environnement) sauvés via `torch.save`.

## Évaluer

```bash
python evaluation/bench.py --grids 20 --agent random
python evaluation/bench.py --grids 50 --agent ppo --ckpt training/router/model_v1.pt
```

`bench.py` affiche un tableau : longueur totale routée, nombre de vias, taux de
complétion des nets, nombre de violations DRC.

## Brancher un modèle entraîné sur l'application (documenté, optionnel)

`src/service.py` montre, avec **python-socketio**, comment un service Python
(exposant une politique PPO) pourrait remplacer le moteur TS : le protocole
événementiel (`ai:route` → `ai:progress`/`ai:result`) reste identique côté client,
seul le port/URL du service change. **Aucun modèle n'est chargé par défaut.**

## Statut

| Composant | Statut |
|---|---|
| Environnement PCB (gym-like) | ✅ fourni (`src/environment/`) |
| PPO + GNN de référence | ✅ fourni (`src/agents/`, `src/models/`) |
| Boucle `train.py` complète | 🔜 prévue (configs déjà en place) |
| Checkpoints `.pt` publiés | ❌ aucun (non requis) |
