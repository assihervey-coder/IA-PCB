# ADR-003 — Routage par apprentissage par renforcement (PPO/A3C) avec repli A*

| | |
|---|---|
| **Statut** | Accepté |
| **Date** | 2025 |
| **Décideurs** | Équipe YahriaCad |
| **Concerné** | `ai-engine/src/` (environment, agents, models), `scripts/generate-models.sh` |

## Contexte

Le routage automatique de nets sur une carte multi-couches est un problème
combinatoire (grille ~0,25 mm, ordres de grandeur : 260×160 cellules pour une
carte 65×40 mm) où les heuristiques classiques (A*) sont rapides mais
locales : ordre des nets figé, vias sous-optimaux, congestion mal anticipée.
L'apprentissage par renforcement a démontré son intérêt pour apprendre
l'ordre, la direction et les changements de couche. Contrainte forte : le
produit doit rester **fonctionnel sans GPU, sans torch et sans checkpoint**.

## Décision

Définir le routage comme un **processus de décision markovien** sur une
grille multi-couches, avec un environnement gym-like
(`ai-engine/src/environment/pcb_env.py`) :

- **Observation** — tenseur multi-canaux (5 canaux) : obstacles/pads par
  couche, net courant, frontière d'objectif, position du curseur.
  Dimensions : `(canaux, hauteur_grille, largeur_grille)`.
- **Actions** — 12 : 4 directions orthogonales × {rester sur la couche,
  monter d'une couche via, descendre d'une couche via}
  (`src/environment/action_space.py`).
- **Récompense** (`reward.py`) — modelée sur les objectifs réels :
  - progression vers le pad cible (delta de distance Manhattan, positif),
  - pénalité par **via** posé,
  - forte pénalité par **collision** (cuivre étranger, clearance DRC),
  - bonus de **succès** à la connexion complète du net.
- **Agents** — PPO (`ppo_agent.py`, actor-critic CNN, GAE, clipping) comme
  référence principale, A3C (`a3c_agent.py`, workers threads + réseau
  global, n-step) comme variante d'entraînement parallèle ; interface
  commune `BaseAgent`.
- **Réseaux auxiliaires** — `GraphNet` (GNN message passing pur torch, sans
  torch_geometric) propose le **placement** (graphe de nets, minimisation
  HPWL) ; `BoardViT` (patch embedding conv + TransformerEncoder) évalue la
  qualité globale d'un routage pour guider l'**optimisation** (rip-up &
  reroute des pires nets).
- **Stratégie d'exécution** — le serveur gRPC expose `strategy: "rl" |
  "astar"` :
  - `astar` (défaut) : A\* multi-couches déterministe, toujours disponible ;
  - `rl` : politique PPO chargée depuis `training/router/model_v1.pt`,
    **uniquement si** torch est importable et le checkpoint présent ; sinon
    bascule transparente sur A\* (le champ `strategy` renvoyé dans les
    résultats reflète la stratégie réellement utilisée).
- **Réutilisation à l'inférence** — l'inférence RL est conditionnée à
  `torch + checkpoint` : `scripts/generate-models.sh` produit des checkpoints
  initiaux (poids aléatoires) pour la chaîne complète (routeur ActorCritic,
  placer GraphNet, optimizer BoardViT) ; l'entraînement série reste hors
  ligne (`ai-engine/`), jamais au chemin d'une requête utilisateur sans
  modèle.

## Conséquences

**Positives**

- Qualité potentielle supérieure à A\* (ordre des nets appris, vias et
  congestion optimisés) tout en gardant une **garantie de service** : A\*
  route tout net routable, de façon déterministe et sans dépendance.
- Environnement gym-like réutilisable pour la recherche (bench `--agent
  random|greedy|ppo`, métriques : longueur, vias, complétion, pénalité DRC).
- Coût mémoire maîtrisé : canaux 2D compacts, modèles volontairement petits
  (checkpoints initiaux de l'ordre du Mo).
- Degré de dégradation lisible côté produit (champ `model_loaded` dans
  `GetHealth`, stratégie réelle dans les résultats).

**Négatives / coûts**

- Entraînement coûteux (heures CPU/GPU) et détachable du produit : les
  checkpoints ne sont **jamais commités** (générés par script).
- Le RL peut être battu par A\* sur des cartes simples ; la stratégie par
  défaut reste `astar` tant que le modèle n'est pas validé.
- Deux implémentations de routage à maintenir (RL et A\*) avec un
  environnement partagé ; les imports torch sont **paresseux** pour garder
  un service léger sans torch.

## Alternatives écartées

- **RL pur sans repli** : inacceptable — pas de garantie qu'un routage
  existe pour toute requête, dépendance torch obligatoire au runtime.
- **Programmation par contraintes / solveurs SAT** : résultats optimaux
  mais temps d'exécution imprévisibles pour des cartes denses ; à reconsidérer
  pour la vérification, pas pour le routage interactif.
- **Imitation learning sur des routages KiCad** : intéressant mais exige un
  corpus de cartes routées annotées ; retenu comme piste future
  (pré-entraînement) plutôt que décision fondatrice.
