# Entrainement du routeur IA (PPO + imitation learning)

## Objectif

Apprendre une politique de routage sur grille (12 actions = 4 deplacements x
{rester, via-haut, via-bas}) avec l'environnement `src/environment/pcb_env.py`
et l'agent PPO de `src/agents/ppo_agent.py`. Le modele resulte en un
checkpoint `.pt` consommable par `src/service.py` : s'il est present ET que
torch est installe, le RPC `RouteBoard` effectue des rollouts greedy RL ;
sinon le moteur utilise le repli A* deterministe (mode de production par
defaut).

Observations (depuis sept. 2026) : `layer_count + 4` canaux — obstacles par
couche, pad source, pads cibles restants, indicateur de couche, et blob
gradue de position de l'agent (sans lui la politique n'est pas reactive :
l'observation etait constante d'un pas a l'autre). Le reseau ajoute un
masquage des actions invalides calcule dans `forward` (fonction deterministe
de l'observation) : un rollout greedy ne peut plus percuter un mur ni poser
un via sur une cellule bloquee. Les checkpoints 5 canaux anterieurs sont
incompatibles et doivent etre re-entraines. `_rl_route` plafonne en plus le
budget de pas a ~4x la distance manhattan optimale : un rollout qui n'a pas
converge dans ce budget echoue vite et laisse le repli A* prendre la main.

## Comment entrainer

```bash
# 1) environnement Python avec torch (scripts/generate-models.sh fait tout)
python3 -m venv .venv && . .venv/bin/activate
pip install torch numpy pyyaml grpcio

# 2) entrainement (exemple minimal avec PPOTrainer)
python3 - <<'EOF'
from src.environment.pcb_env import PCBRouteEnv, EnvConfig
from src.agents.ppo_agent import PPOAgent, PPOConfig, PPOTrainer
from evaluation.bench import make_synthetic_board

def env_factory():
    board, nets = make_synthetic_board(1234)
    return PCBRouteEnv(board, nets, EnvConfig(clearance_cells=1, seed=0))

agent = PPOAgent(PPOConfig(), in_channels=5, n_actions=12)
history = PPOTrainer(env_factory, agent, total_steps=200_000).train()
agent.save("training/router/model_v1.pt")
print(history["steps"][-1], history["episode_reward"][-1])
EOF
```

## Script prêt à l'emploi

Un script CLI évite d'écrire le snippet à la main :

```bash
pip install torch --index-url https://download.pytorch.org/whl/cpu
python3 ai-engine/training/router/train.py --steps 200000 --seed 0
# options : --out training/router/model_v1.pt --save-every 50000 (checkpoints intermédiaires)
# équivalent Makefile : make train-router STEPS=200000
```

## Ou arrivent les checkpoints

- `training/router/model_v1.pt` : checkpoint PPO (charge par `src/service.py`
  via la cle `router.model_path` de `config.yaml`).
- Les fichiers `.pt` sont generes par `scripts/generate-models.sh` et ne sont
  JAMAIS commits a la main (voir `.gitignore`).

## Tokenizer

`tokenizer/tokenizer.py` fournit `NetTokenizer` (vocabulaire JSON
`tokenizer/vocab.json`, tokens speciaux `<pad>/<unk>/<bos>/<eos>`) pour les
experiences sequentielles sur les noms de nets. Demo :

```bash
python3 training/router/tokenizer/tokenizer.py
```

## Imitation learning (behavioral cloning de A*) — `imitation.py`

Le PPO explore depuis zero et reste domine par A* ; l'imitation learning
distille directement les trajectoires de l'expert deterministe dans le MEME
reseau (checkpoint au meme format, chargeable par le service). Pipeline
complet en 4 commandes :

```bash
# 1) dumper les entrees exactes d'un routage reel (hook dans RouteBoard) :
YAHRIACAD_DUMP_ROUTE_INPUT=/tmp/dump make run-ai
#    ... puis declencher un routage (import + POST /route) : /tmp/dump/route_input.json

# 2) demonstrations : trajectoire experte par net + cellules-sondes
#    (champ de reprise A* : premier pas correct depuis N cellules libres
#    aleatoires, avec sur-echantillonnage x3 des cas d'evitement d'obstacle)
#    + curriculum synthetique (res 0.5 mm conseille : grilles 4x plus rapides)
python3 training/router/imitation.py generate \
    --input /tmp/dump/route_input.json --probes-real 25 \
    --synthetic 32 --probes 40 --out training/router/demos.npz

# 3) entrainement (cross-entropy sur les actions expertes, split par demo,
#    budget temps optionnel, reprise --init-from)
python3 training/router/imitation.py train --demos training/router/demos.npz \
    --out training/router/model_bc.pt --epochs 6 --batch 128 --time-budget 330

# 4) evaluation hors-ligne A* vs BC sur la carte reelle (repli A* net par
#    net inclus, cap de pas pour les rollouts en echec)
python3 training/router/imitation.py eval --model training/router/model_bc.pt \
    --input /tmp/dump/route_input.json --rollout-cap 400

# 5) mise en production : POST /api/v1/ai/model/reload {"checkpoint_path": "..."}
```

Une passe **DAgger** (`collect_dagger`) complete leBC : la politique BC roule
chaque net comme en production et A* etiquete les etats REELLEMENT visites
(y compris ses erreurs) — c'est la cure des erreurs composees.

Resultats mesures (carte demo 6 nets, arene A* vs RL) :

| politique | score arene | temps |
|---|---|---|
| A* (expert) | 592.70 | 0 ms (local) |
| PPO 6k pas | 49.72 | 45.9 s |
| PPO ~20k pas | 130.67 | — |
| **BC (imitation)** | **461.75** | **12.8 s** |

Limite documentee : le transfert vers une GRANDE carte reelle (complex_hierarchy,
grille 294x352) reste faible avec un curriculum synthetique (2/52 nets BC seul ;
le repli A* net par net couvre le reste) — la voie d'echelle identifiee est un
curriculum a resolution appariee + davantage de sondes reelles + rondes DAgger
supplementaires. Le mode RL reste donc utilisable en ligne : echec rapide par
net (budget de pas) et repli A* systematique.
