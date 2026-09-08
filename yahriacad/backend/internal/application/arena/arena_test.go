package arena

import (
	"strings"
	"testing"

	domainlayout "github.com/assihervey-coder/IA-PCB/backend/internal/domain/layout"
	domainschematic "github.com/assihervey-coder/IA-PCB/backend/internal/domain/schematic"
)

func testBoardAndSchematic(t *testing.T) (*domainlayout.Board, *domainschematic.Schematic) {
	t.Helper()
	board, err := domainlayout.NewBoard(40, 30, 2)
	if err != nil {
		t.Fatalf("NewBoard : %v", err)
	}
	fp := domainlayout.Footprint{Name: "0603", BodyWidthMM: 2, BodyHeightMM: 1.2, HeightMM: 1}
	_ = fp
	for _, c := range []domainlayout.PlacedComponent{
		{Ref: "U1", Footprint: fp, X: 8, Y: 8},
		{Ref: "R1", Footprint: fp, X: 30, Y: 8},
		{Ref: "C1", Footprint: fp, X: 8, Y: 22},
		{Ref: "BLOCK", Footprint: domainlayout.Footprint{Name: "wall", BodyWidthMM: 10, BodyHeightMM: 8, HeightMM: 1}, X: 19, Y: 14},
	} {
		if err := board.AddComponent(c); err != nil {
			t.Fatalf("AddComponent %s : %v", c.Ref, err)
		}
	}

	sch := domainschematic.New("test")
	mk := func(ref string, pins ...string) {
		c := domainschematic.Component{Ref: ref}
		for i, p := range pins {
			c.Pins = append(c.Pins, domainschematic.Pin{Number: p, Name: p, X: float64(i), Y: 0})
		}
		if err := sch.AddComponent(c); err != nil {
			t.Fatalf("AddComponent %s : %v", ref, err)
		}
	}
	mk("U1", "1", "2")
	mk("R1", "1")
	mk("C1", "1")
	if err := sch.AddNet(domainschematic.Net{Name: "SIG1", Class: domainschematic.ClassSignal,
		Connections: []domainschematic.PinRef{
			{ComponentRef: "U1", PinNumber: "1"},
			{ComponentRef: "R1", PinNumber: "1"},
		}}); err != nil {
		t.Fatalf("AddNet : %v", err)
	}
	if err := sch.AddNet(domainschematic.Net{Name: "SIG2", Class: domainschematic.ClassSignal,
		Connections: []domainschematic.PinRef{
			{ComponentRef: "U1", PinNumber: "2"},
			{ComponentRef: "C1", PinNumber: "1"},
		}}); err != nil {
		t.Fatalf("AddNet : %v", err)
	}
	return board, sch
}

func TestBuildTasks(t *testing.T) {
	board, sch := testBoardAndSchematic(t)
	tasks := buildTasks(sch, board)
	if len(tasks) != 2 {
		t.Fatalf("2 nets attendus, obtenu %d", len(tasks))
	}
	// U1 pin1 -> R1 pin1 : absolute (8,8) -> (30,8).
	if tasks[0].Endpoints[0].X != 8 || tasks[0].Endpoints[0].Y != 8 {
		t.Fatalf("endpoint U1 attendu (8,8) : %+v", tasks[0].Endpoints[0])
	}
	if tasks[0].Endpoints[1].X != 30 {
		t.Fatalf("endpoint R1 attendu x=30 : %+v", tasks[0].Endpoints[1])
	}
}

func TestGreedyCollidesWhereAstarAvoids(t *testing.T) {
	board, sch := testBoardAndSchematic(t)
	tasks := buildTasks(sch, board)
	if len(tasks) == 0 {
		t.Fatal("aucune tâche construite")
	}

	greedy := runFighter("greedy", tasks, board, nil)
	astar := runFighter("astar", tasks, board, nil)

	if greedy.Completed != len(tasks) {
		t.Fatalf("greedy doit compléter tous les nets (brutalement) : %+v", greedy)
	}
	if astar.Completed != len(tasks) {
		t.Fatalf("astar doit compléter tous les nets : %+v", astar)
	}
	if astar.Collisions != 0 {
		t.Fatalf("astar ne doit jamais percuter : %d collisions", astar.Collisions)
	}
	if astar.TotalLengthMM <= greedy.TotalLengthMM {
		// Non strict (dépend de la géométrie) mais loggé pour diagnostic.
		t.Logf("info : astar %.1f mm vs greedy %.1f mm", astar.TotalLengthMM, greedy.TotalLengthMM)
	}
	if !strings.Contains(greedy.NetLog[0], "L-manhattan") {
		t.Fatalf("log greedy inattendu : %q", greedy.NetLog[0])
	}
}

func TestEloUpdateAndLeaderboard(t *testing.T) {
	board, sch := testBoardAndSchematic(t)
	buildTasks(sch, board) // s'assure que la construction ne panique pas
	svc := NewArenaService(nil, nil)

	rep := &MatchReport{Winner: "astar"}
	svc.updateElo(rep)

	lb := svc.Leaderboard()
	if len(lb) != 2 {
		t.Fatalf("2 combattants attendus, obtenu %d", len(lb))
	}
	// astar (index 0, rating le plus haut après victoire) > greedy.
	if lb[0].Strategy != "astar" || lb[0].Rating <= lb[1].Rating {
		t.Fatalf("classement ELO incohérent : %+v", lb)
	}
	if lb[0].Wins != 1 || lb[1].Losses != 1 {
		t.Fatalf("bilan W/L incohérent : %+v", lb)
	}
}

func TestAstarPathFindsLShapedRoute(t *testing.T) {
	w, h := 10, 10
	blocked := make([]bool, w*h)
	// wall in the middle column, rows 2..7
	for y := 2; y <= 7; y++ {
		blocked[y*w+5] = true
	}
	path, ok := astarPath(w, h, blocked, 3, 4, 7, 4)
	if !ok {
		t.Fatal("chemin attendu autour du mur")
	}
	for _, p := range path {
		if p[0] == 5 && p[1] >= 2 && p[1] <= 7 {
			t.Fatalf("le chemin traverse le mur en %v", p)
		}
	}
	if path[0][0] != 3 || path[len(path)-1][0] != 7 {
		t.Fatalf("extrémités incohérentes : %v … %v", path[0], path[len(path)-1])
	}
}
