# ADR-002 — Moteur IA en microservice Python séparé, contrat gRPC `pcb.proto`

| | |
|---|---|
| **Statut** | Accepté |
| **Date** | 2025 |
| **Décideurs** | Équipe YahriaCad |
| **Concerné** | `ai-engine/`, `backend/internal/infrastructure/ai/`, `shared/types/pcb.proto` |

## Contexte

Les algorithmes de placement/routage intelligents s'appuient sur
l'écosystème scientifique Python (PyTorch, numpy) pour l'apprentissage par
renforcement, alors que le backend applicatif est en Go pour la robustesse
et la concurrence. Il faut faire coopérer deux runtimes avec des exigences
fortes :

- **Temps réel** : un routage de plusieurs dizaines de nets doit publier sa
  progression net par net (le frontend affiche une barre et un journal).
- **Dégradation propre** : le service IA peut être absent, lent ou sans
  torch — le backend doit rester utilisable.
- **Évolution des modèles** : remplacer un modèle RL ne doit pas toucher au
  backend ni au frontend.

## Décision

Extraire le moteur IA en **microservice Python autonome**
(`ai-engine/`, port gRPC 50051) derrière un **contrat protobuf unique**
`shared/types/pcb.proto` (service `AIRouterService`) :

- **RPC** : `GetHealth` (état, device cpu/cuda, modèle chargé),
  `PlanPlacement` (synchrone), `RouteBoard` et `OptimizeRoutes`
  (**streaming** de `ProgressEvent` : un événement par net terminé, final
  `done=true`, champ `partial` portant le résultat du net).
- **Contrats partagés** : stubs générés une seule fois dans `shared/gen/go`
  et `shared/gen/python` (jamais dupliqués) ; côté Go, un seul adaptateur
  `infrastructure/ai/grpc_client.go` implémente le port `layoutapp.AIService`
  — le reste de l'application ne voit que cette interface.
- **Raisons du choix de gRPC** :
  1. *contrat typé* : messages versionnés (`yahriacad.pcb.v1`), champs en mm,
     énumérations de stratégies — pas de schéma JSON à maintenir à la main
     des deux côtés ;
  2. *streaming serveur natif* : le flux de `ProgressEvent` correspond
     exactement au besoin (HTTP/2 multiplexé, backpressure) ;
  3. *polyglotte* : Go et Python mûrement supportés (protoc + grpcio),
     génération déterministe via `make proto`.
- **Repli** : au démarrage, `Health` est sondée (warning + `ai_unreachable`
  dans `/healthz` en cas d'échec) ; les routes IA renvoient HTTP 502 code
  `ai_unreachable` si le moteur est down. Le routage local A\* du moteur
  Python garantit qu'un routage reste possible même sans torch ni GPU.
- **Déploiement** : `docker/docker-compose.yml` isole `ai-engine` sur le
  réseau interne (non exposé publiquement), seul le backend le consomme.

## Conséquences

**Positives**

- Choix de stack optimal par problème : Go pour le service d'API, Python
  pour la science des données, sans compromis ni FFI.
- Indépendance des releases et du cycle d'entraînement (checkpoints `.pt`
  montés, jamais commités ; service redémarré sans toucher au backend).
- Progression temps réel fiable (stream gRPC → hub WebSocket).
- Tests faciles : le port `AIService` se mocke côté Go (cf. test
  `TestRouteJobWithMockAI`), le servicer se teste côté Python avec les
  stubs partagés.

**Négatives / coûts**

- Un service de plus à opérer (santé, logs, redémarrage) ; latence réseau
  gRPC (~ms) négligeable face au calcul.
- Génération de stubs à synchroniser (protoc + plugins) — outillée par le
  Makefile.
- Deux images Docker, `docker-compose` légèrement plus riche.

## Alternatives écartées

- **Bibliothèque Python embarquée dans le backend** (bindings
  go-python/cgo) : complexité de build, GIL, CGO incompatible avec l'image
  distroless statique choisie.
- **API REST + polling JSON** : polling et schémas manuels, pas de stream
  natif ; plus verbeux pour la progression.
- **Files d'attente (NATS/RabbitMQ)** : overkill pour un couplage
  requête/réponse avec flux, ajoute un intergiciel à opérer.
