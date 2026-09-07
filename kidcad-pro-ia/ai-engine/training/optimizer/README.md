# Optimisation des pistes (rip-up & reroute)

## Objectif

Le RPC `OptimizeRoutes` de `src/service.py` rippe et re-route successivement
les nets (du plus long au plus court) avec les routes des autres nets comme
obstacles. Le resultat est conserve si le score composite diminue :

```
score = longueur_mm
      + via_cost_mm * nb_vias        (objectif "vias")
      + drc_cost    * violations_DRC (objectif "drc")
```

## Parametres (config.yaml)

- `optimizer.via_cost_mm` (2.0) : equivalent mm d'un via dans le score.
- `optimizer.drc_cost` (10.0) : penalite par violation DRC estimee.
- `optimizer.clearance_mm` (0.2) : clearance utilisee pour l'estimation DRC.
- `optimizer.objectives` (proto) : sous-ensemble de `["length", "vias", "drc"]`.

## Entrainement RL (roadmap)

Un futur modele (ViT `src/models/transformer.py` entraime sur des grilles
routees) pourra predire les zones de congestion et guider l'ordre de rip-up ;
checkpoint attendu : `training/optimizer/model_v1.pt` (genere par
`scripts/generate-models.sh`, jamais committe a la main).
