# YahriaCad - Moteur IA (Python / gRPC)

Microservice IA pour le placement et le routage PCB, expose en gRPC selon le
contrat partage `shared/types/pcb.proto` (service `yahriacad.pcb.v1.AIRouterService`,
consomme par le backend Go via `backend/internal/infrastructure/ai/grpc_client.go`).

## Architecture

```
cmd/ai-server/main.py      point d'entree gRPC (port YAHRIACAD_AI_PORT, defaut 50051)
src/service.py             AIRouterServicer : GetHealth / PlanPlacement /
                           RouteBoard (streaming) / OptimizeRoutes (streaming)
src/environment/           environnement de routage sur grille + repli A*
  pcb_env.py                 PCBRouteEnv (gym-like), astar_route, route_all
  action_space.py            ActionSpace : 12 actions = 4 deplacements x {rester, via+, via-}
  reward.py                  RewardConfig / RewardShaper (fonctions pures)
src/agents/                RL (torch OPTIONNEL, import paresseux)
  base_agent.py              BaseAgent + RolloutBuffer (GAE)
  ppo_agent.py               PPOAgent / PPOTrainer (PPO clippe + GAE)
  a3c_agent.py               A3CAgent / A3CWorker / A3CTrainer (threads)
src/models/                reseaux (torch pur, sans torch_geometric)
  graph_net.py               GraphConv / GraphNet + build_graph (numpy)
  transformer.py             BoardViT (patch embedding + TransformerEncoder)
evaluation/                metrics, bench CLI, smoke tests (env + gRPC)
training/                  configs + READMEs (router / placer / optimizer)
data/                      raw / processed / augmentations
```

Unites : toutes les coordonnees sont en **millimetres**, origine en coin
haut-gauche, **couche 0 = F.Cu**. Determinisme : graines via `random.Random`,
aucun torch requis pour la production.

## Fonctionnement du repli A* (mode production, sans torch)

Le service fonctionne de bout en bout sans PyTorch :

1. la demande `RouteBoard` est convertie en grille (`grid_resolution_mm`,
   defaut 0.25 mm) ; les pads des AUTRES nets (halo de clearance), le bord de
   carte et les pistes deja routees bloquent des cellules ;
2. chaque net est route par A* 4-connexe (tas, heuristique de Manhattan,
   cout 1 par deplacement, `via_penalty` par changement de couche), les pads
   multi-terminaux etant chaine du plus proche au plus proche ;
3. les segments colineaires sont fusionnes, les vias generees, et le resultat
   est enregistre comme obstacle pour les nets suivants (routage progressif,
   nets courts d'abord) ;
4. `OptimizeRoutes` rippe les nets les plus longs et garde le reroutage si le
   score (longueur + vias + DRC estime) diminue.

Le meme environnement sert a l'entrainement RL : observation
`(layer_count + 3, H, W)` (obstacles par couche, masque source, masque cible,
indicateur de couche), 12 actions, recompense = progression + bonus de
reussite - penalites (pas, via, collision). `GetHealth` repond `ok` avec
`model_loaded=false` tant qu'aucun checkpoint n'est charge - c'est l'etat
nominal de production (le routage s'effectue alors en A* deterministe).

## Lancer le serveur

```bash
python3 cmd/ai-server/main.py --port 50051
# ou : YAHRIACAD_AI_PORT=50051 python3 cmd/ai-server/main.py
# options : --config training/router/config.yaml --log-level info
```

## Tests / evaluation

```bash
python3 evaluation/smoke_test.py                 # environnement + A*
python3 evaluation/smoke_grpc.py                 # serveur gRPC reel (port 50077)
python3 evaluation/bench.py --grids 3 --agent greedy
python3 evaluation/bench.py --grids 5 --agent random --json report.json
```

## Entrainement (torch optionnel)

torch n'est PAS requis pour le service ; il ne l'est que pour produire les
checkpoints `.pt` (PPO/A3C, GNN, ViT). Voir `training/router/README.md` et
`scripts/generate-models.sh` (les `.pt` sont generes, jamais commits).
Configuration des hyperparametres : `training/router/config.yaml`.

## Contrat

Tous les messages (`BoardSpec`, `NetSpec`, `ProgressEvent`, ...) sont definis
dans `shared/types/pcb.proto` (source de verite, cf.
`docs/architecture/contracts.md` sections 6 et 10). Les stubs Python sont
importes depuis `shared/gen/python` - jamais dupliques.
