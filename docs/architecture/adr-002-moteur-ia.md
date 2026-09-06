# ADR-002 — Moteur d'inférence déterministe TypeScript pour le produit, RL PyTorch en référence R&D

- **Statut :** Accepté
- **Date :** 2025
- **Décideurs :** équipe KidCAD-Pro-IA

## Contexte

Le placement et le routage automatiques sont les fonctions « IA » centrales de
KidCAD-Pro-IA. Deux voies étaient possibles :

1. **Modèles RL entraînés** (PyTorch : PPO + GNN sur un environnement grille type
   Gymnasium) servis en production, comme initialement spécifié (`ai-engine/` gRPC
   servant des modèles `.pt`) ;
2. **Algorithmes déterministes classiques** implémentés en TypeScript et exécutés
   directement dans le produit.

Contraintes réelles :

- la production n'a **pas de GPU** et doit répondre en **temps réel** (routage interactif
  d'une carte de 100 à 500 nets en secondes) ;
- l'utilisateur doit obtenir **le même résultat pour le même design** (reproductibilité
  des Gerbers, comparaison de versions, confiance dans le DRC) ;
- l'empreinte mémoire du service doit rester faible (conteneur 3010 léger) ;
- l'entraînement RL reste utile pour la R&D et l'amélioration des heuristiques.

## Décision

1. **Le produit déployé embarque un moteur d'inférence déterministe TypeScript**
   (`src/lib/kidcad/ai-engine/`, pur, sans dépendance Next.js) composé de :
   - un **routeur A\* maze-routing multi-couches 2 couches** (F.Cu / B.Cu) avec coût de
     via, grille de routage 0.635 mm ;
   - un **rip-up & reroute** itératif pour débloquer les nets conflictuels ;
   - un **placeur par recuit simulé** (simulated annealing) minimisant le HPWL plus les
     pénalités de chevauchement ;
   - un **optimiseur de réduction de vias** post-routage.
2. **L'entraînement RL réel (PyTorch, PPO + GNN) est conservé hors-ligne** dans
   `ai-engine/` (racine du dépôt) : environnement type Gymnasium (`pcb_env.py`), agent
   PPO (`ppo_agent.py`), GNN (`graph_net.py`), benchmark (`evaluation/`). Ce pipeline est
   une **référence R&D, non requise** pour faire tourner l'application. Les modèles `.pt`
   produits ne sont jamais chargés en production.

## Justification

| Critère | Moteur TS déterministe | RL PyTorch en prod |
|---|---|---|
| **Latence** | Routage A\* de quelques millisecondes à quelques secondes par net ; placement par recuit borné par budget d'itérations | Inférence GNN + décodage par pas de temps : nettement plus lent sans GPU, dépendant du batching |
| **Déterminisme** | Garanti (même graine → même routage → mêmes Gerbers) | Non garanti (stochastique, versions de runtime, précision flottante) |
| **GPU en prod** | Aucun requis | Recommandé, sinon dégradation majeure |
| **Reproductibilité** | Parfaite, journalisable pas à pas | Difficile (contrôle des graines, drift de versions CUDA) |
| **Certifiabilité DRC** | Le routeur respecte les règles par construction (grille, clearance) | Nécessite une validation systématique après coup |
| **Complexité d'exploitation** | Zéro : fonction pure dans le bundle | Service GPU, gestion de modèles, monitoring |

Le RL garde un rôle de **recherche** : les politiques entraînées peuvent un jour
**remplacer ou guider** le moteur TS (par exemple proposer l'ordre de routage des nets ou
les couches par net) ; `ai-engine/src/service.py` documente comment un modèle entraîné
serait exposé via socket.io pour être substitué au moteur TS sans changer le protocole
(`ai:route` / `ai:result`).

## Conséquences

### Positives

- Temps réel garanti, déploiement trivial (pas de runtime Python/GPU en prod).
- Résultats reproductibles et explicables (chaque trace est justifiée par le coût A\*).
- Le pipeline RL reste disponible pour la R&D et les benchmarks comparatifs
  (`ai-engine/evaluation/bench.py`).

### Négatives

- Qualité de routage plafonnée par les heuristiques classiques sur des cartes très denses
  (le RL pourrait à terme mieux arbitrer l'ordre des nets) — compensé par rip-up & reroute.
- Deux moteurs à maintenir conceptuellement (TS inférence / Python entraînement), même si
  leurs périmètres sont clairement séparés.
