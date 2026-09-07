# Placement IA (recuit simule)

## Objectif

Le RPC `PlanPlacement` de `src/service.py` minimise un cout composite :
proxy HPWL (graphe complet, decomposition en etoile autour du centroide
initial - la v1 du contrat protobuf ne transporte pas la netlist) plus une
penalite de recouvrement d'empreintes. Seuls les composants non fixes
bougent ; la graine est determinee (`placer.seed`) pour des resultats
reproductibles.

## Parametres (config.yaml)

- `placer.iterations` (1500) : borne d'iterations du recuit.
- `placer.time_budget_ms` (2000) : budget temps reel par requete.
- `placer.seed` (null) : graine du RNG ; `null` = graine derivee du nombre
  de composants de la requete (deterministe : meme entree -> meme placement).
- `placer.overlap_weight` (5.0) : poids d'un mm^2 de recouvrement.

## Entrainement RL (roadmap)

Le placer n'utilise pas encore de modele entraine : lorsqu'un checkpoint
`training/placer/model_v1.pt` sera produit (politique GNN via
`src/models/graph_net.py` + build_graph), `PlanPlacement` utilisera ses poids
pour ponderer le cout et reportera `strategy="rl"`. En attendant, la
strategie reportee est toujours `heuristic`.

Les checkpoints sont generes par `scripts/generate-models.sh` (torch requis),
jamais commits a la main.
