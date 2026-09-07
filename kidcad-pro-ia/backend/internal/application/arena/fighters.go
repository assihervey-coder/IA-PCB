// fighters.go holds the two arena combatants. Both implement the same
// brief: connect every endpoint pair of a net, return tracks, vias and a
// collision count. They are pure functions of (tasks, board) so they can
// be benchmarked and unit-tested without any I/O.
package arena

import (
	"container/heap"
	"fmt"
	"math"
	"sort"
	"time"

	domainlayout "github.com/kidcad/kidcad-pro-ia/backend/internal/domain/layout"
)

// routeResult is a fighter's answer for one net.
type routeResult struct {
	lengthMM   float64
	vias       int
	collisions int
	completed  bool
	log        string
}

// obstacle describes an inflated component body the fighters must (or
// naively fail to) avoid.
type obstacle struct {
	minX, minY, maxX, maxY float64
	ref                    string
}

// collectObstacles inflates each component bbox by half a clearance
// margin (0.4 mm) — a coarse but honest stand-in for keep-outs.
func collectObstacles(board *domainlayout.Board) []obstacle {
	out := make([]obstacle, 0, len(board.Components))
	for i := range board.Components {
		bb := board.Components[i].BBox().Expand(0.4)
		out = append(out, obstacle{
			minX: bb.MinX, minY: bb.MinY, maxX: bb.MaxX, maxY: bb.MaxY,
			ref: board.Components[i].Ref,
		})
	}
	return out
}

// runFighter executes one strategy over all tasks and aggregates the card.
func runFighter(strategy string, tasks []NetTask, board *domainlayout.Board,
	onNet func(net string, pct float64, msg string)) FighterResult {

	card := FighterResult{Strategy: strategy}
	var obstacles []obstacle
	if strategy == "astar" {
		obstacles = collectObstacles(board)
	}

	start := time.Now()
	for i := range tasks {
		t := &tasks[i]
		var res routeResult
		if strategy == "astar" {
			res = routeAstar(t, board, obstacles)
		} else {
			res = routeGreedy(t, board)
		}
		card.TotalLengthMM += res.lengthMM
		card.ViaCount += res.vias
		card.Collisions += res.collisions
		if res.completed {
			card.Completed++
		} else {
			card.Failed++
		}
		line := fmt.Sprintf("  %s [%s] : %s", t.Name, netTaskLabel(*t), res.log)
		card.NetLog = append(card.NetLog, line)
		if onNet != nil {
			onNet(t.Name, float64(i+1)/float64(len(tasks))*100, line)
		}
	}
	card.DurationMS = time.Since(start).Milliseconds()

	// Score: completing nets dominates, then shorter routes, then fewer
	// collisions; duration is a light tie-breaker.
	card.Score = float64(card.Completed)*100 -
		card.TotalLengthMM*0.05 -
		float64(card.Collisions)*40 -
		float64(card.DurationMS)*0.01
	card.Score = math.Round(card.Score*100) / 100
	return card
}

// routeGreedy draws naive L-shaped paths (horizontal first) between
// consecutive endpoints and counts how many segments cross a component
// body — it does not avoid anything. Brutal. Fast. Honest.
func routeGreedy(t *NetTask, board *domainlayout.Board) routeResult {
	res := routeResult{}
	obstacles := collectObstacles(board)
	poly := make([]domainlayout.TrackPoint, 0, len(t.Endpoints)*2)
	for i, e := range t.Endpoints {
		if i == 0 {
			poly = append(poly, domainlayout.TrackPoint{X: e.X, Y: e.Y})
			continue
		}
		prev := poly[len(poly)-1]
		// L-path: horizontal then vertical.
		if math.Abs(e.X-prev.X) > 1e-9 {
			poly = append(poly, domainlayout.TrackPoint{X: e.X, Y: prev.Y})
		}
		poly = append(poly, domainlayout.TrackPoint{X: e.X, Y: e.Y})
	}
	if len(poly) < 2 {
		res.log = "points insuffisants"
		return res
	}
	res.completed = true
	for i := 1; i < len(poly); i++ {
		a, b := poly[i-1], poly[i]
		res.lengthMM += math.Abs(b.X-a.X) + math.Abs(b.Y-a.Y)
		for _, o := range obstacles {
			if segmentHitsBox(a, b, o) {
				res.collisions++
			}
		}
	}
	if res.collisions > 0 {
		res.log = fmt.Sprintf("L-manhattan %.1f mm, %d collision(s) ☠️", res.lengthMM, res.collisions)
	} else {
		res.log = fmt.Sprintf("L-manhattan %.1f mm, chemin libre", res.lengthMM)
	}
	return res
}

// routeAstar routes each consecutive endpoint pair on a 1 mm grid with
// component obstacles inflated, four-neighbour movement and a Manhattan
// heuristic. Falls back to "échec" when the grid says no path exists.
func routeAstar(t *NetTask, board *domainlayout.Board, obstacles []obstacle) routeResult {
	res := routeResult{}
	const cell = 1.0
	w := int(math.Ceil(board.WidthMM / cell))
	h := int(math.Ceil(board.HeightMM / cell))
	if w <= 0 || h <= 0 {
		res.log = "carte invalide"
		return res
	}
	blocked := make([]bool, w*h)
	for _, o := range obstacles {
		x0 := clampi(int(o.minX/cell)-1, 0, w-1)
		x1 := clampi(int(o.maxX/cell)+1, 0, w-1)
		y0 := clampi(int(o.minY/cell)-1, 0, h-1)
		y1 := clampi(int(o.maxY/cell)+1, 0, h-1)
		for y := y0; y <= y1; y++ {
			for x := x0; x <= x1; x++ {
				blocked[y*w+x] = true
			}
		}
	}

	for i := 1; i < len(t.Endpoints); i++ {
		from, to := t.Endpoints[i-1], t.Endpoints[i]
		// Pads live INSIDE their component body: unblock the cells of the
		// two endpoint components so the fighter can start/land there
		// while still respecting every other keep-out.
		local := make([]bool, len(blocked))
		copy(local, blocked)
		for _, o := range obstacles {
			if o.ref == from.Ref || o.ref == to.Ref {
				x0 := clampi(int(o.minX/cell)-1, 0, w-1)
				x1 := clampi(int(o.maxX/cell)+1, 0, w-1)
				y0 := clampi(int(o.minY/cell)-1, 0, h-1)
				y1 := clampi(int(o.maxY/cell)+1, 0, h-1)
				for y := y0; y <= y1; y++ {
					for x := x0; x <= x1; x++ {
						local[y*w+x] = false
					}
				}
			}
		}
		path, ok := astarPath(w, h, local,
			clampi(int(from.X/cell), 0, w-1), clampi(int(from.Y/cell), 0, h-1),
			clampi(int(to.X/cell), 0, w-1), clampi(int(to.Y/cell), 0, h-1))
		if !ok {
			res.log = fmt.Sprintf("%s→%s : aucune route trouvée (obstacles)", from.Ref, to.Ref)
			return res
		}
		res.lengthMM += float64(len(path)-1) * cell
	}
	res.completed = true
	if res.lengthMM > 0 && len(obstacles) == 0 {
		res.log = fmt.Sprintf("A* %.1f mm, champ libre", res.lengthMM)
	} else {
		res.log = fmt.Sprintf("A* %.1f mm, contournements propres", res.lengthMM)
	}
	return res
}

// astarPath is a textbook A* over a 4-neighbour grid.
func astarPath(w, h int, blocked []bool, sx, sy, tx, ty int) ([][2]int, bool) {
	if sx == tx && sy == ty {
		return [][2]int{{sx, sy}, {tx, ty}}, true
	}
	start := sy*w + sx
	target := ty*w + tx
	if blocked[target] || blocked[start] {
		// allow start/target inside bodies (pads live there)
		if blocked[target] {
			blocked[target] = false
		}
		if blocked[start] {
			blocked[start] = false
		}
	}

	g := make([]float64, w*h)
	prev := make([]int, w*h)
	closed := make([]bool, w*h)
	for i := range g {
		g[i] = math.Inf(1)
		prev[i] = -1
	}
	g[start] = 0
	pq := &priorityQueue{}
	heap.Push(pq, &pqItem{idx: start, f: manhattan(sx, sy, tx, ty)})

	dirs := [4][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}}
	for pq.Len() > 0 {
		cur := heap.Pop(pq).(*pqItem)
		if cur.idx == target {
			return rebuild(prev, target, w), true
		}
		if closed[cur.idx] {
			continue
		}
		closed[cur.idx] = true
		cx, cy := cur.idx%w, cur.idx/w
		for _, d := range dirs {
			nx, ny := cx+d[0], cy+d[1]
			if nx < 0 || ny < 0 || nx >= w || ny >= h {
				continue
			}
			ni := ny*w + nx
			if blocked[ni] || closed[ni] {
				continue
			}
			ng := g[cur.idx] + 1
			if ng < g[ni] {
				g[ni] = ng
				prev[ni] = cur.idx
				heap.Push(pq, &pqItem{idx: ni, f: ng + manhattan(nx, ny, tx, ty)})
			}
		}
	}
	return nil, false
}

func manhattan(x1, y1, x2, y2 int) float64 {
	return math.Abs(float64(x1-x2)) + math.Abs(float64(y1-y2))
}

func rebuild(prev []int, target, w int) [][2]int {
	var rev [][2]int
	for at := target; at != -1; at = prev[at] {
		rev = append(rev, [2]int{at % w, at / w})
	}
	// reverse
	for i, j := 0, len(rev)-1; i < j; i, j = i+1, j-1 {
		rev[i], rev[j] = rev[j], rev[i]
	}
	return rev
}

func clampi(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// segmentHitsBox reports whether the axis-aligned segment a→b crosses the
// inflated body box (with a 0.2 mm tolerance for touches).
func segmentHitsBox(a, b domainlayout.TrackPoint, o obstacle) bool {
	const tol = 0.2
	minX, maxX := math.Min(a.X, b.X), math.Max(a.X, b.X)
	minY, maxY := math.Min(a.Y, b.Y), math.Max(a.Y, b.Y)
	return maxX > o.minX+tol && minX < o.maxX-tol &&
		maxY > o.minY+tol && minY < o.maxY-tol
}

// sortObstaclesByRef keeps obstacle order stable for deterministic tests.
func sortObstaclesByRef(o []obstacle) {
	sort.Slice(o, func(i, j int) bool { return o[i].ref < o[j].ref })
}

// ---------------------------------------------------------- priority queue

type pqItem struct {
	idx int
	f   float64
}
type priorityQueue struct{ items []*pqItem }

func (q *priorityQueue) Len() int { return len(q.items) }
func (q *priorityQueue) Less(i, j int) bool {
	return q.items[i].f < q.items[j].f ||
		(q.items[i].f == q.items[j].f && q.items[i].idx < q.items[j].idx)
}
func (q *priorityQueue) Swap(i, j int) { q.items[i], q.items[j] = q.items[j], q.items[i] }
func (q *priorityQueue) Push(x any)    { q.items = append(q.items, x.(*pqItem)) }
func (q *priorityQueue) Pop() any {
	old := q.items
	n := len(old)
	it := old[n-1]
	q.items = old[:n-1]
	return it
}
