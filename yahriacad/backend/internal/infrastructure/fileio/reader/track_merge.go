package reader

import (
	"sort"

	domainlayout "github.com/assihervey-coder/IA-PCB/backend/internal/domain/layout"
)

// mergeTracks fuse les pistes élémentaires contiguës partageant la même clé
// (net, couche, largeur) en pistes multi-points, puis supprime les points
// intermédiaires alignés. La transformation est purement cosmétique : la
// géométrie du cuivre est conservée au point près.
//
// Sécurités topologiques :
//   - une jonction (extrémité partagée par plus de deux segments) n'est
//     jamais traversée : chaque branche reste une piste distincte ;
//   - un chaînage ne se referme jamais sur son propre départ (deux segments
//     géométriquement identiques restent des pistes distinctes) ;
//   - une véritable boucle fermée est émise comme une piste dont le premier
//     point est répété à la fin.
//
// mergePt est un point 2D quantifié pour l'indexation des extrémités.
type mergePt struct{ x, y float64 }

func mergeTracks(tracks []domainlayout.Track) []domainlayout.Track {
	type key struct {
		net   string
		layer int
		width float64
	}

	type seg struct {
		idx  int     // position d'origine dans tracks
		a, b mergePt // extrémités
	}

	// Les pistes déjà multi-points (routage interne) et les segments
	// dégénérés passent tels quels ; seuls les segments élémentaires
	// (2 points) sont candidats au chaînage.
	type out struct {
		idx int
		t   domainlayout.Track
	}
	var results []out
	groups := map[key][]seg{}
	for i, t := range tracks {
		if len(t.Points) != 2 {
			results = append(results, out{i, t})
			continue
		}
		a := mergePt{t.Points[0].X, t.Points[0].Y}
		b := mergePt{t.Points[1].X, t.Points[1].Y}
		if a == b {
			results = append(results, out{i, t})
			continue
		}
		k := key{t.Net, t.Layer, t.Width}
		groups[k] = append(groups[k], seg{idx: i, a: a, b: b})
	}

	for k, segs := range groups {
		at := map[mergePt][]int{} // extrémité -> indices (dans segs) attachés
		for i, s := range segs {
			at[s.a] = append(at[s.a], i)
			at[s.b] = append(at[s.b], i)
		}
		visited := make([]bool, len(segs))
		sameGeom := func(i, j int) bool {
			return (segs[i].a == segs[j].a && segs[i].b == segs[j].b) ||
				(segs[i].a == segs[j].b && segs[i].b == segs[j].a)
		}

		for i := range segs {
			if visited[i] {
				continue
			}
			visited[i] = true
			pts := []mergePt{segs[i].a, segs[i].b}

			// Marche aval : depuis segs[i].b, tant que l'extrémité porte
			// exactement deux segments (le courant + un suivant) non visité
			// et non identique au départ du chaîne.
			attach, last := segs[i].b, i
			for {
				att := at[attach]
				if len(att) != 2 {
					break // extrémité libre ou jonction : on s'arrête
				}
				nxt := att[0]
				if nxt == last {
					nxt = att[1]
				}
				if nxt == last || visited[nxt] || sameGeom(i, nxt) {
					break
				}
				var other mergePt
				switch {
				case segs[nxt].a == attach:
					other = segs[nxt].b
				case segs[nxt].b == attach:
					other = segs[nxt].a
				default:
					break
				}
				visited[nxt] = true
				pts = append(pts, other)
				attach, last = other, nxt
			}

			// Marche amont : même règle depuis segs[i].a.
			var head []mergePt
			attach, last = segs[i].a, i
			for {
				att := at[attach]
				if len(att) != 2 {
					break
				}
				nxt := att[0]
				if nxt == last {
					nxt = att[1]
				}
				if nxt == last || visited[nxt] || sameGeom(i, nxt) {
					break
				}
				var other mergePt
				switch {
				case segs[nxt].a == attach:
					other = segs[nxt].b
				case segs[nxt].b == attach:
					other = segs[nxt].a
				default:
					break
				}
				visited[nxt] = true
				head = append(head, other)
				attach, last = other, nxt
			}
			for l, r := 0, len(head)-1; l < r; l, r = l+1, r-1 {
				head[l], head[r] = head[r], head[l]
			}
			pts = append(head, pts...)

			merged := domainlayout.Track{
				Net:   k.net,
				Layer: k.layer,
				Width: k.width,
			}
			for _, p := range simplifyCollinear(pts) {
				merged.Points = append(merged.Points,
					domainlayout.TrackPoint{X: p.x, Y: p.y})
			}
			results = append(results, out{segs[i].idx, merged})
		}
	}

	sort.Slice(results, func(p, q int) bool { return results[p].idx < results[q].idx })
	mergedTracks := make([]domainlayout.Track, 0, len(tracks))
	for _, r := range results {
		mergedTracks = append(mergedTracks, r.t)
	}
	return mergedTracks
}

// simplifyCollinear supprime les points intermédiaires alignés (même sens)
// d'une polyligne ; un demi-tour (recouvrement) est conservé par prudence.
func simplifyCollinear(pts []mergePt) []mergePt {
	const eps = 1e-7
	if len(pts) < 3 {
		return pts
	}
	out := pts[:1]
	for i := 1; i < len(pts)-1; i++ {
		prev, mid, next := out[len(out)-1], pts[i], pts[i+1]
		cross := (mid.x-prev.x)*(next.y-prev.y) - (mid.y-prev.y)*(next.x-prev.x)
		dot := (mid.x-prev.x)*(next.x-mid.x) + (mid.y-prev.y)*(next.y-mid.y)
		if (cross > -eps && cross < eps) && dot > 0 {
			continue // mid strictement aligné entre prev et next
		}
		out = append(out, mid)
	}
	return append(out, pts[len(pts)-1])
}
