# Tests d'intégration

Le test d'intégration Go du backend vit dans
[`backend/tests/integration/backend_pipeline_test.go`](../../backend/tests/integration/backend_pipeline_test.go).

**Pourquoi ?** Il importe des packages `backend/internal/...` et la règle de
visibilité `internal` du langage Go n'autorise l'import de ces packages que
depuis le code placé sous `backend/`. Depuis ce dossier racine, la compilation
échouerait avec « use of internal package … not allowed ».

```bash
# Exécution (cible make test-backend)
go test ./backend/tests/integration/
```

## Scénario couvert (docs/architecture/contracts.md §9)

1. création de projet (dépôt mémoire) ;
2. import de `tests/fixtures/sample.kicad_pcb` via `reader.NewRegistry` ;
3. ERC sans violation de gravité `error` ;
4. DRC détectant deux pistes trop proches (`DRC_CLEARANCE`) ;
5. export Gerber en ZIP contenant `*-F_Cu.gbr` ;
6. job de routage asynchrone piloté par un `AIService` simulé : état `done`,
   pistes persistées dans l'agrégat, événements de progression publiés.

Les fixtures partagées vivent dans `tests/fixtures/` (source de vérité unique,
consommée aussi bien par le test Go que par la documentation).
