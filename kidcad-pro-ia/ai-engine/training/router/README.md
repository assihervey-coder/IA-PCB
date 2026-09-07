# Entrainement du routeur IA (PPO)

## Objectif

Apprendre une politique de routage sur grille (12 actions = 4 deplacements x
{rester, via-haut, via-bas}) avec l'environnement `src/environment/pcb_env.py`
et l'agent PPO de `src/agents/ppo_agent.py`. Le modele resulte en un
checkpoint `.pt` consommable par `src/service.py` : s'il est present ET que
torch est installe, le RPC `RouteBoard` effectue des rollouts greedy RL ;
sinon le moteur utilise le repli A* deterministe (mode de production par
defaut).

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
