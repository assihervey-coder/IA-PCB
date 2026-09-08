package writer

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	domainlayout "github.com/assihervey-coder/IA-PCB/backend/internal/domain/layout"
)

// StepWriter produces a simplified STEP AP214 (ISO-10303-21) model of the
// board: a rectangular slab plus one axis-aligned box per component (body
// footprint × height). The BREP topology (vertices, edges, loops, faces,
// shell, solid) is complete and correctly connected so CAD tools can import
// the file.
type StepWriter struct{}

// NewStepWriter builds the STEP generator.
func NewStepWriter() *StepWriter { return &StepWriter{} }

// boardFileName is the product name of the board solid.
const boardFileName = "board"

// Write generates the STEP file of the board and returns its path.
func (w *StepWriter) Write(b *domainlayout.Board, projectName, outDir string) (string, error) {
	if b == nil {
		return "", fmt.Errorf("step : carte absente")
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return "", fmt.Errorf("step : répertoire de sortie : %w", err)
	}

	slug := slugifyName(projectName)
	path := filepath.Join(outDir, slug+".step")

	var sb strings.Builder
	s := &stepBuilder{target: &sb}

	writeStepHeader(&sb, slug)
	writeDataContext(s)

	// Plaque : dalle de 1,6 mm posée à l'origine.
	boardThickness := domainlayout.DefaultBoardHeightMM
	boardBrep := s.box(0, 0, 0, b.WidthMM, b.HeightMM, boardThickness, boardFileName)
	writeRepresentation(s, boardFileName, boardBrep)

	// Composants : boîtes centrées sur (X, Y), posées sur la plaque.
	for i := range b.Components {
		c := &b.Components[i]
		bw := c.Footprint.BodyWidthMM
		bh := c.Footprint.BodyHeightMM
		if bw <= 0 {
			bw = 2
		}
		if bh <= 0 {
			bh = 2
		}
		h := c.Footprint.HeightMM
		if h <= 0 {
			h = domainlayout.DefaultBoardHeightMM
		}
		brep := s.box(c.X-bw/2, c.Y-bh/2, boardThickness, bw, bh, h, c.Ref)
		writeRepresentation(s, c.Ref, brep)
	}

	sb.WriteString("ENDSEC;\n")
	sb.WriteString("END-ISO-10303-21;\n")

	if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
		return "", fmt.Errorf("step : écriture de %s : %w", path, err)
	}
	return path, nil
}

// writeStepHeader emits the ISO-10303-21 HEADER section.
func writeStepHeader(sb *strings.Builder, slug string) {
	sb.WriteString("ISO-10303-21;\n")
	sb.WriteString("HEADER;\n")
	fmt.Fprintf(sb,
		"FILE_DESCRIPTION(('Modele 3D simplifie YahriaCad : plaque et composants en boites (BREP complet)'),'2;1');\n")
	fmt.Fprintf(sb, "FILE_NAME('%s.step','%s',('YahriaCad'),('YahriaCad'),'yahriacad-step-writer','','');\n",
		sanitizeStepString(slug), time.Now().UTC().Format("2006-01-02T15:04:05"))
	sb.WriteString("FILE_SCHEMA(('AUTOMOTIVE_DESIGN { 1 0 10303 214 1 1 1 1 }'));\n")
	sb.WriteString("ENDSEC;\n")
	sb.WriteString("DATA;\n")
}

// stepBuilder accumulates the DATA section with a monotonically increasing
// entity identifier counter.
type stepBuilder struct {
	target *strings.Builder
	next   int

	// Identifiants du contexte partagés par toutes les représentations.
	geomContext        int
	productDefShape    int
	rootRepresentation int
}

// id reserves the next entity number.
func (s *stepBuilder) id() int { s.next++; return s.next }

// put appends one entity line and returns its id.
func (s *stepBuilder) put(format string, args ...any) int {
	n := s.id()
	fmt.Fprintf(s.target, "#%d="+format+";\n", append([]any{n}, args...)...)
	return n
}

// writeDataContext emits the units, the product structure and the root
// shape representation shared by every solid.
func writeDataContext(s *stepBuilder) {
	lengthUnit := s.put("(LENGTH_UNIT()NAMED_UNIT(*)SI_UNIT(.MILLI.,.METRE.))")
	angleUnit := s.put("(NAMED_UNIT(*)PLANE_ANGLE_UNIT()SI_UNIT($,.RADIAN.))")
	solidAngleUnit := s.put("(NAMED_UNIT(*)SI_UNIT($,.STERADIAN.)SOLID_ANGLE_UNIT())")
	uncertainty := s.put("UNCERTAINTY_MEASURE_WITH_UNIT(LENGTH_MEASURE(1.E-06),#%d,'distance_accuracy_value','tolerance de distance')", lengthUnit)
	s.geomContext = s.put("(GEOMETRIC_REPRESENTATION_CONTEXT(3)GLOBAL_UNCERTAINTY_ASSIGNED_CONTEXT((#%d))GLOBAL_UNIT_ASSIGNED_CONTEXT((#%d,#%d,#%d))REPRESENTATION_CONTEXT('','3D'))",
		uncertainty, lengthUnit, angleUnit, solidAngleUnit)

	appContext := s.put("APPLICATION_CONTEXT('core data for automotive mechanical design processes')")
	s.put("APPLICATION_PROTOCOL_DEFINITION('international standard','automotive_design',2010,#%d)", appContext)
	productContext := s.put("PRODUCT_CONTEXT('',#%d,'')", appContext)
	product := s.put("PRODUCT('yahriacad-board','yahriacad-board','',(#%d))", productContext)
	formation := s.put("PRODUCT_DEFINITION_FORMATION('','',#%d)", product)
	pdContext := s.put("PRODUCT_DEFINITION_CONTEXT('part definition',#%d,'design')", appContext)
	pd := s.put("PRODUCT_DEFINITION('design','',#%d,#%d)", formation, pdContext)
	s.productDefShape = s.put("PRODUCT_DEFINITION_SHAPE('','',#%d)", pd)

	origin := s.put("CARTESIAN_POINT('',(0.E0,0.E0,0.E0))")
	zDir := s.put("DIRECTION('',(0.E0,0.E0,1.E0))")
	xDir := s.put("DIRECTION('',(1.E0,0.E0,0.E0))")
	axis := s.put("AXIS2_PLACEMENT_3D('',#%d,#%d,#%d)", origin, zDir, xDir)
	s.rootRepresentation = s.put("SHAPE_REPRESENTATION('yahriacad model',(#%d),#%d)", axis, s.geomContext)
	s.put("SHAPE_DEFINITION_REPRESENTATION(#%d,#%d)", s.productDefShape, s.rootRepresentation)
}

// writeRepresentation wires one solid into the product structure through an
// ADVANCED_BREP_SHAPE_REPRESENTATION linked to the root representation.
func writeRepresentation(s *stepBuilder, name string, brep int) {
	origin := s.put("CARTESIAN_POINT('',(0.E0,0.E0,0.E0))")
	zDir := s.put("DIRECTION('',(0.E0,0.E0,1.E0))")
	xDir := s.put("DIRECTION('',(1.E0,0.E0,0.E0))")
	axis := s.put("AXIS2_PLACEMENT_3D('',#%d,#%d,#%d)", origin, zDir, xDir)
	abr := s.put("ADVANCED_BREP_SHAPE_REPRESENTATION('%s',(#%d,#%d),#%d)",
		sanitizeStepString(name), axis, brep, s.geomContext)
	s.put("SHAPE_REPRESENTATION_RELATIONSHIP('','',#%d,#%d)", s.rootRepresentation, abr)
}

// edgeWalk is one oriented traversal of an edge inside a face loop.
type edgeWalk struct {
	edge int
	fwd  bool
}

// faceDef describes one planar face: the ordered walk of its four edges
// (counter-clockwise seen from outside) plus its plane placement.
type faceDef struct {
	walk     [4]edgeWalk
	normal   [3]float64
	refDir   [3]float64
	anchorPt [3]float64
}

// box emits a full CLOSED_SHELL BREP of an axis-aligned box and returns the
// MANIFOLD_SOLID_BREP entity id.
func (s *stepBuilder) box(x, y, z, dx, dy, dz float64, name string) int {
	x1, y1, z1 := x+dx, y+dy, z+dz
	corners := [8][3]float64{
		{x, y, z}, {x1, y, z}, {x1, y1, z}, {x, y1, z},
		{x, y, z1}, {x1, y, z1}, {x1, y1, z1}, {x, y1, z1},
	}

	points := make([]int, 8)
	vertices := make([]int, 8)
	for i, c := range corners {
		points[i] = s.put("CARTESIAN_POINT('',(%E,%E,%E))", c[0], c[1], c[2])
		vertices[i] = s.put("VERTEX_POINT('',#%d)", points[i])
	}

	// Arêtes : anneau inférieur (0-3), anneau supérieur (4-7), montants.
	edgeDefs := [12][2]int{
		{0, 1}, {1, 2}, {2, 3}, {3, 0},
		{4, 5}, {5, 6}, {6, 7}, {7, 4},
		{0, 4}, {1, 5}, {2, 6}, {3, 7},
	}
	edges := make([]int, 12)
	for i, e := range edgeDefs {
		a, b := corners[e[0]], corners[e[1]]
		dir := [3]float64{b[0] - a[0], b[1] - a[1], b[2] - a[2]}
		l := math.Sqrt(dir[0]*dir[0] + dir[1]*dir[1] + dir[2]*dir[2])
		if l < 1e-12 {
			dir, l = [3]float64{1, 0, 0}, 1
		}
		dir[0], dir[1], dir[2] = dir[0]/l, dir[1]/l, dir[2]/l
		dirID := s.put("DIRECTION('',(%E,%E,%E))", dir[0], dir[1], dir[2])
		vecID := s.put("VECTOR('',#%d,1.0)", dirID)
		lineID := s.put("LINE('',#%d,#%d)", points[e[0]], vecID)
		edges[i] = s.put("EDGE_CURVE('',#%d,#%d,#%d,.T.)", vertices[e[0]], vertices[e[1]], lineID)
	}

	// Faces : parcours des arêtes avec orientation (normale sortante).
	faces := []faceDef{
		{walk: [4]edgeWalk{{3, false}, {2, false}, {1, false}, {0, false}},
			normal: [3]float64{0, 0, -1}, refDir: [3]float64{1, 0, 0}, anchorPt: corners[0]},
		{walk: [4]edgeWalk{{4, true}, {5, true}, {6, true}, {7, true}},
			normal: [3]float64{0, 0, 1}, refDir: [3]float64{1, 0, 0}, anchorPt: corners[4]},
		{walk: [4]edgeWalk{{0, true}, {9, true}, {4, false}, {8, false}},
			normal: [3]float64{0, -1, 0}, refDir: [3]float64{1, 0, 0}, anchorPt: corners[0]},
		{walk: [4]edgeWalk{{1, true}, {10, true}, {5, false}, {9, false}},
			normal: [3]float64{1, 0, 0}, refDir: [3]float64{0, 1, 0}, anchorPt: corners[1]},
		{walk: [4]edgeWalk{{2, true}, {11, true}, {6, false}, {10, false}},
			normal: [3]float64{0, 1, 0}, refDir: [3]float64{1, 0, 0}, anchorPt: corners[2]},
		{walk: [4]edgeWalk{{3, true}, {8, true}, {7, false}, {11, false}},
			normal: [3]float64{-1, 0, 0}, refDir: [3]float64{0, 1, 0}, anchorPt: corners[3]},
	}

	faceIDs := make([]int, 0, len(faces))
	for _, f := range faces {
		oriented := make([]int, 0, 4)
		for _, w := range f.walk {
			flag := ".F."
			if w.fwd {
				flag = ".T."
			}
			oriented = append(oriented, s.put("ORIENTED_EDGE('',*,*,#%d,%s)", edges[w.edge], flag))
		}
		loop := s.put("EDGE_LOOP('',(#%d,#%d,#%d,#%d))", oriented[0], oriented[1], oriented[2], oriented[3])
		bound := s.put("FACE_OUTER_BOUND('',(#%d),.T.)", loop)
		anchor := s.put("CARTESIAN_POINT('',(%E,%E,%E))", f.anchorPt[0], f.anchorPt[1], f.anchorPt[2])
		nDir := s.put("DIRECTION('',(%E,%E,%E))", f.normal[0], f.normal[1], f.normal[2])
		rDir := s.put("DIRECTION('',(%E,%E,%E))", f.refDir[0], f.refDir[1], f.refDir[2])
		axis := s.put("AXIS2_PLACEMENT_3D('',#%d,#%d,#%d)", anchor, nDir, rDir)
		plane := s.put("PLANE('',#%d)", axis)
		faceIDs = append(faceIDs, s.put("ADVANCED_FACE('',(#%d),#%d,.T.)", bound, plane))
	}

	shellArgs := make([]string, 0, len(faceIDs))
	for _, f := range faceIDs {
		shellArgs = append(shellArgs, fmt.Sprintf("#%d", f))
	}
	shell := s.put("CLOSED_SHELL('',(%s))", strings.Join(shellArgs, ","))
	return s.put("MANIFOLD_SOLID_BREP('%s',#%d)", sanitizeStepString(name), shell)
}

// sanitizeStepString escapes single quotes for the STEP string encoding.
func sanitizeStepString(s string) string {
	return strings.NewReplacer("'", "''", "\n", " ").Replace(s)
}
