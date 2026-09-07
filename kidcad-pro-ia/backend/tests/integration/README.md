# Test d'intégration backend (emplacement exécutable)

Ce dossier héberge une **copie exécutable** du test d'intégration
`tests/integration/backend_pipeline_test.go` (agent 3-d).

## Pourquoi cette copie ?

Le fichier original vit dans `tests/integration/`, à la racine du monorepo.
Or il importe des packages `backend/internal/...` : la règle de visibilité
`internal` de Go n'autorise que le code placé sous `backend/` à les
importer. Depuis `tests/integration/`, la compilation échoue donc avec
« use of internal package … not allowed » — indépendamment du contenu du
test (le fichier d'origine est conservé tel quel, hors périmètre backend).

Le test est ici placé sous `backend/tests/integration/`, ce qui satisfait
la règle `internal` et rend le scénario réellement exécutable :

```
go test ./backend/tests/integration/
```

Le lien symbolique `backend/tests/fixtures -> ../../tests/fixtures` fait
pointer la résolution de chemins du test (`../../tests/fixtures`) vers les
fixtures d'origine, source de vérité unique (aucune fixture dupliquée).

## Scénario couvert (contracts.md §9)

1. création projet (dépôt mémoire) ;
2. import de `tests/fixtures/sample.kicad_pcb` via `reader.NewRegistry` ;
3. ERC sans gravité `error` ;
4. DRC détectant deux pistes trop proches (DRC_CLEARANCE) ;
5. export Gerber en ZIP contenant `*-F_Cu.gbr` ;
6. job de routage asynchrone piloté par un `AIService` simulé : état
   `done`, pistes persistées, événements publiés.
