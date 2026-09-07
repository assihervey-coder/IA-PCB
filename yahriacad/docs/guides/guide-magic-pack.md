# Guide — Magic Pack : parler à votre carte

Le Magic Pack ajoute une couche d'intelligence conversationnelle et de
simulation avancée à YahriaCad. Tout est accessible depuis la palette
**⌘K** (ou **Ctrl+K**) du frontend, ou directement en REST.

## 1. Magic Copilot (⌘K)

Ouvrez la palette (bouton ✨ en bas à droite ou ⌘K), écrivez une phrase,
*Interpréter* pour l'aperçu, *Exécuter* (Entrée) pour appliquer.

### Ce que le copilot comprend

| Phrase | Actions produites |
|---|---|
| « place R1 et R2 près de U3 » | déplace R1 et R2 à côté de U3 |
| « pose deux condensateurs près de U1 » | crée C(n), C(n+1) près de U1 |
| « déplace R7 en (12.5, 3) » | repositionne R7 aux coordonnées |
| « largeur de piste 0.3 mm » | règle la règle + re-large les pistes |
| « mets la largeur à 12 mil sur la classe power » | idem, scope classe |
| « ajoute une classe de nets high-speed, largeur 0.15 mm » | crée la règle |
| « route tout en rapide » | lance le routage IA (délégué) |
| « drc puis exporte gerber et bom » | enchaîne 3 actions |
| « supprime C17 » | retire le composant |

Le score de **confiance** (0–100 %) indique la solidité de l'interprétation ;
au-dessous de 40 %, une reformulation est suggérée. Unités : `mm` et `mil`
(1 mil = 0,0254 mm), séparateurs décimaux `,` ou `.`.

### Exécution directe vs délégation

Les actions marquées **délégué** (routage, optimisation, DRC/ERC, exports)
ne sont pas exécutées par le copilot : le frontend appelle l'endpoint du
use case correspondant. Le copilot orchestre, il ne duplique pas.

## 2. Thermal Ghost — carte thermique instantanée

`POST /api/v1/projects/{id}/thermal` simule l'état stationnaire de la carte
(équation de la chaleur, bords à l'ambiance) :

```jsonc
// réponse (extrait)
{
  "max_temp_c": 58.3,
  "max_gradient_c_per_mm": 4.1,
  "hotspots": [
    { "rank": 1, "x": 24.2, "y": 19.8, "temp_c": 58.3, "likely_ref": "U3" }
  ],
  "warnings": ["Point chaud à 58 °C : ajouter un plan cuivre près de U3."]
}
```

- La dissipation par composant est **estimée** (référence + valeur) ; passez
  un corps `{"sources":[{"ref":"U1","power_w":1.2,...}]}` pour un what-if.
- Les couches internes dispersent la chaleur (étalement cuivre).
- Astuce : lancez la simulation avant et après un déplacement de l'alimentation
  pour comparer les écarts (plus fiables que les valeurs absolues).

## 3. Eye Oracle — intégrité du signal en un POST

`POST /api/v1/projects/{id}/si` analyse chaque net routé :

- **Z0** : impédance microstrip (forme fermée IPC-2141, FR4 par défaut) ;
- **délai** : longueur / vitesse (εr effectif ≈ 2,5) ;
- **réflexions** : budget ps par via ;
- **œil** : hauteur % (atténuation + ISI) et largeur ps à 1 Gbps ;
- **verdict** `ok` / `warning` / `critical` + conseils actionnables
  (« réduire les vias », « ajuster la largeur pour viser 50 Ω »…).

Le `si_score_pct` global est la moyenne des hauteurs d'œil : un indicateur
de progression entre deux itérations de routage.

## 4. AI Arena — deux routeurs, un classement

`POST /api/v1/projects/{id}/arena` fait s'affronter, sur les mêmes nets :

- **greedy** : L-Manhattan naïf — brutal, rapide, et il *compte* ses
  collisions avec les empreintes ;
- **astar** : A* sur grille 1 mm, keep-outs = empreintes gonflées — jamais
  de collision, mais des détours.

Le score combine nets complétés, longueur totale, collisions et durée.
Le classement **ELO** (démarrage 1200, K=24) évolue à chaque combat :

```bash
curl -X POST http://localhost:8080/api/v1/projects/P1/arena
curl http://localhost:8080/api/v1/arena/leaderboard
```

Le récit du combat est poussé en direct sur `ws/v1/progress` (stage `arena`).
Prérequis : un schéma importé, des composants placés (le placement IA suffit).

## 5. EvoPlace — placement par évolution

Côté ai-engine, `src/evolution/evo_place.py` fait évoluer une population de
placements (sélection par tournoi, croisement BLX-α, mutation gaussienne,
élitisme) minimisant le HPWL avec pénalité de recouvrement :

```bash
cd ai-engine
python -m src.evolution.evo_place --demo   # évolution animée dans le terminal
python -m pytest tests/test_evo_place.py  # 5 tests, déterministes
```

Le callback `on_generation` fournit (génération, meilleure fitness, moyenne,
meilleur placement) — prêt pour un streaming WebSocket des générations,
comme le routage RL.

## Raccourcis

| Raccourci | Effet |
|---|---|
| ⌘K / Ctrl+K | ouvre/ferme la palette magique |
| Entrée (palette) | exécute les actions interprétées |
| Échap | ferme la palette |
| 🌡️ / 📡 / ⚔️ | thermique, eye oracle, arène — depuis la palette |
