// Package verificationapp contains the design-rule (DRC) and electrical-rule
// (ERC) checkers. The DRC combines a spatial hash (≈2 mm cells) with the
// pure geometry primitives of internal/pkg/geometry. The package name
// differs from the folder name to avoid collisions with the domain package.
package verificationapp

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	apperrors "github.com/kidcad/kidcad-pro-ia/backend/internal/application"
	domainconstraints "github.com/kidcad/kidcad-pro-ia/backend/internal/domain/constraints"
	domainlayout "github.com/kidcad/kidcad-pro-ia/backend/internal/domain/layout"
	domainproject "github.com/kidcad/kidcad-pro-ia/backend/internal/domain/project"
	domainschematic "github.com/kidcad/kidcad-pro-ia/backend/internal/domain/schematic"
	"github.com/kidcad/kidcad-pro-ia/backend/internal/pkg/geometry"
)

// DRC violation codes.
const (
	CodeDRCClearance     = "DRC_CLEARANCE"
	CodeDRCTrackWidth    = "DRC_TRACK_WIDTH"
	CodeDRCViaDiameter   = "DRC_VIA_DIAMETER"
	CodeDRCDrill         = "DRC_DRILL"
	CodeDRCAnnular       = "DRC_ANNULAR"
	CodeDRCEdgeClearance = "DRC_EDGE_CLEARANCE"
)

// maxViolations caps the report so a pathological board cannot exhaust
// memory.
const maxViolations = 1000

// drcCellMM is the spatial hash cell size.
const drcCellMM = 2.0

// DRCViolation is one design-rule violation with its location.
type DRCViolation struct {
	Code     string  `json:"code"`
	Severity string  `json:"severity"`
	Message  string  `json:"message"`
	X        float64 `json:"x"`
	Y        float64 `json:"y"`
	Layer    int     `json:"layer"`
	Net      string  `json:"net"`
}

// DRCResult is the outcome of a design-rule check.
type DRCResult struct {
	Passed       bool           `json:"passed"`
	CheckedRules int            `json:"checked_rules"`
	Violations   []DRCViolation `json:"violations"`
	DurationMS   int64          `json:"duration_ms"`
}

// DRCChecker runs the design-rule check of a project board.
type DRCChecker struct {
	projects domainproject.Repository
}

// NewDRCChecker builds the DRC use case.
func NewDRCChecker(projects domainproject.Repository) *DRCChecker {
	return &DRCChecker{projects: projects}
}

// Run executes every enabled rule against the board and returns the report.
func (c *DRCChecker) Run(ctx context.Context, projectID string) (*DRCResult, error) {
	start := time.Now()

	p, err := c.projects.FindByID(ctx, projectID)
	if err != nil {
		if errors.Is(err, apperrors.ErrNotFound) {
			return nil, fmt.Errorf("drc : projet %q introuvable : %w", projectID, apperrors.ErrNotFound)
		}
		return nil, fmt.Errorf("drc : chargement du projet : %w", err)
	}
	board := p.Board()
	if board == nil {
		return nil, fmt.Errorf("drc : le projet %q n'a pas de carte (importez d'abord un design)", projectID)
	}
	cs := p.Constraints()
	if cs == nil {
		cs = domainconstraints.NewDefault()
	}

	run := &drcRun{
		board:      board,
		cs:         cs,
		netClasses: netClassMap(p.Schematic()),
		maxClearMM: defaultMaxClearance(cs),
		violations: []DRCViolation{},
	}

	run.checkTrackWidths()
	run.checkVias()
	run.checkEdgeClearance()
	run.checkClearances()

	res := &DRCResult{
		Passed:       len(run.violations) == 0,
		CheckedRules: run.checkedRules(),
		Violations:   run.violations,
		DurationMS:   time.Since(start).Milliseconds(),
	}
	return res, nil
}

// --------------------------------------------------------------
// Objets géométriques indexés
// --------------------------------------------------------------

type drcKind int

const (
	kindSegment drcKind = iota
	kindPad
	kindVia
)

// drcObj is one copper object inserted into the spatial hash.
type drcObj struct {
	kind  drcKind
	seg   geometry.Segment
	layer int // segment : couche cuivre ; pad : couche (-1 traversant)
	viaLo int // via : borne basse
	viaHi int // via : borne haute
	net   string
	cls   string
	width float64 // segment : largeur
	r     float64 // pad/via : rayon effectif
	padW  float64
	padH  float64
}

// bbox returns the axis-aligned bounding box of the object.
func (o *drcObj) bbox() geometry.AABB {
	switch o.kind {
	case kindSegment:
		return geometry.AABBFromPoints([]geometry.Point{o.seg.A, o.seg.B}).Expand(o.width / 2)
	default:
		return geometry.AABB{
			MinX: o.seg.A.X - o.r, MinY: o.seg.A.Y - o.r,
			MaxX: o.seg.A.X + o.r, MaxY: o.seg.A.Y + o.r,
		}
	}
}

// spansLayer reports whether the object has copper on copper layer idx.
func (o *drcObj) spansLayer(idx int) bool {
	switch o.kind {
	case kindSegment:
		return o.layer == idx
	case kindPad:
		return o.layer < 0 || o.layer == idx
	default:
		lo, hi := o.viaLo, o.viaHi
		if lo > hi {
			lo, hi = hi, lo
		}
		return idx >= lo && idx <= hi
	}
}

// drcHash is a uniform spatial hash over ≈2 mm cells.
type drcHash struct {
	cell    float64
	margin  float64
	buckets map[[2]int][]int
	objs    []drcObj
}

func newDRCHash(margin float64) *drcHash {
	return &drcHash{
		cell:    drcCellMM,
		margin:  margin,
		buckets: make(map[[2]int][]int),
	}
}

func cellKey(x, y float64, cell float64) [2]int {
	return [2]int{int(math.Floor(x / cell)), int(math.Floor(y / cell))}
}

// insert adds an object, indexing every cell covered by its bounding box.
func (h *drcHash) insert(o drcObj) {
	idx := len(h.objs)
	h.objs = append(h.objs, o)
	bb := o.bbox()
	for cx := int(math.Floor((bb.MinX - h.margin) / h.cell)); cx <= int(math.Floor((bb.MaxX+h.margin)/h.cell)); cx++ {
		for cy := int(math.Floor((bb.MinY - h.margin) / h.cell)); cy <= int(math.Floor((bb.MaxY+h.margin)/h.cell)); cy++ {
			key := [2]int{cx, cy}
			h.buckets[key] = append(h.buckets[key], idx)
		}
	}
}

// forCandidates calls fn once for every distinct pair (i, j) with i < j
// whose objects are close enough to possibly violate a clearance rule.
func (h *drcHash) forCandidates(fn func(i, j int)) {
	seen := make(map[[2]int]struct{})
	for i := range h.objs {
		bb := h.objs[i].bbox()
		for cx := int(math.Floor((bb.MinX - h.margin) / h.cell)); cx <= int(math.Floor((bb.MaxX+h.margin)/h.cell)); cx++ {
			for cy := int(math.Floor((bb.MinY - h.margin) / h.cell)); cy <= int(math.Floor((bb.MaxY+h.margin)/h.cell)); cy++ {
				for _, j := range h.buckets[[2]int{cx, cy}] {
					if j <= i {
						continue
					}
					key := [2]int{i, j}
					if _, dup := seen[key]; dup {
						continue
					}
					seen[key] = struct{}{}
					fn(i, j)
				}
			}
		}
	}
}

// --------------------------------------------------------------
// Contexte d'exécution du DRC
// --------------------------------------------------------------

type drcRun struct {
	board      *domainlayout.Board
	cs         *domainconstraints.ConstraintSet
	netClasses map[string]domainschematic.NetClass
	maxClearMM float64
	violations []DRCViolation
}

// class returns the net class of a net name (default when unknown).
func (r *drcRun) class(net string) string {
	if c, ok := r.netClasses[net]; ok && c != "" {
		return string(c)
	}
	return string(domainschematic.ClassDefault)
}

// value resolves a rule value for a net class and layer.
func (r *drcRun) value(t domainconstraints.RuleType, cls string, layer int) (float64, bool) {
	return r.cs.Value(t, cls, layer)
}

// severity returns the severity of the most specific matching rule.
func (r *drcRun) severity(t domainconstraints.RuleType, cls string, layer int) string {
	rules := r.cs.RulesFor(t, cls, layer)
	if len(rules) > 0 {
		return string(rules[0].Severity)
	}
	return string(domainconstraints.SeverityError)
}

// checkedRules counts the distinct rule types enabled in the constraint set.
func (r *drcRun) checkedRules() int {
	types := []domainconstraints.RuleType{
		domainconstraints.RuleMinClearance,
		domainconstraints.RuleMinTrackWidth,
		domainconstraints.RuleMinViaDiameter,
		domainconstraints.RuleMinDrill,
		domainconstraints.RuleMinAnnularRing,
		domainconstraints.RuleEdgeClearance,
	}
	n := 0
	for _, t := range types {
		if _, ok := r.value(t, domainconstraints.AnyNetClass, domainconstraints.AnyLayer); ok {
			n++
		}
	}
	return n
}

// add records a violation, stopping early at the cap.
func (r *drcRun) add(v DRCViolation) bool {
	if len(r.violations) >= maxViolations {
		return false
	}
	r.violations = append(r.violations, v)
	return true
}

// checkTrackWidths applies the min_track_width rule to every track.
func (r *drcRun) checkTrackWidths() {
	minW, ok := r.value(domainconstraints.RuleMinTrackWidth, domainconstraints.AnyNetClass, domainconstraints.AnyLayer)
	if !ok {
		return
	}
	for i := range r.board.Tracks {
		t := &r.board.Tracks[i]
		if len(t.Points) == 0 {
			continue
		}
		cls := r.class(t.Net)
		if v, ok := r.value(domainconstraints.RuleMinTrackWidth, cls, t.Layer); ok {
			minW = v
		}
		if t.Width+geometry.Epsilon < minW {
			if !r.add(DRCViolation{
				Code:     CodeDRCTrackWidth,
				Severity: r.severity(domainconstraints.RuleMinTrackWidth, cls, t.Layer),
				Message: fmt.Sprintf("piste du net %q trop fine : %.3f mm < %.3f mm",
					t.Net, t.Width, minW),
				X: t.Points[0].X, Y: t.Points[0].Y,
				Layer: t.Layer, Net: t.Net,
			}) {
				return
			}
		}
	}
}

// checkVias applies the via diameter / drill / annular ring rules.
func (r *drcRun) checkVias() {
	for i := range r.board.Vias {
		v := &r.board.Vias[i]
		cls := r.class(v.Net)

		if minD, ok := r.value(domainconstraints.RuleMinViaDiameter, cls, v.FromLayer); ok &&
			v.Diameter+geometry.Epsilon < minD {
			if !r.add(DRCViolation{
				Code:     CodeDRCViaDiameter,
				Severity: r.severity(domainconstraints.RuleMinViaDiameter, cls, v.FromLayer),
				Message: fmt.Sprintf("via du net %q trop petit : %.3f mm < %.3f mm",
					v.Net, v.Diameter, minD),
				X: v.X, Y: v.Y, Layer: v.FromLayer, Net: v.Net,
			}) {
				return
			}
		}

		if minDrill, ok := r.value(domainconstraints.RuleMinDrill, cls, v.FromLayer); ok &&
			v.Drill+geometry.Epsilon < minDrill {
			if !r.add(DRCViolation{
				Code:     CodeDRCDrill,
				Severity: r.severity(domainconstraints.RuleMinDrill, cls, v.FromLayer),
				Message: fmt.Sprintf("perçage du via du net %q trop petit : %.3f mm < %.3f mm",
					v.Net, v.Drill, minDrill),
				X: v.X, Y: v.Y, Layer: v.FromLayer, Net: v.Net,
			}) {
				return
			}
		}

		if minRing, ok := r.value(domainconstraints.RuleMinAnnularRing, cls, v.FromLayer); ok &&
			v.AnnularRing()+geometry.Epsilon < minRing {
			if !r.add(DRCViolation{
				Code:     CodeDRCAnnular,
				Severity: r.severity(domainconstraints.RuleMinAnnularRing, cls, v.FromLayer),
				Message: fmt.Sprintf("anneau annulaire du via du net %q trop fin : %.3f mm < %.3f mm",
					v.Net, v.AnnularRing(), minRing),
				X: v.X, Y: v.Y, Layer: v.FromLayer, Net: v.Net,
			}) {
				return
			}
		}
	}
}

// checkEdgeClearance applies the edge_clearance rule to tracks and vias.
func (r *drcRun) checkEdgeClearance() {
	w, h := r.board.WidthMM, r.board.HeightMM
	edges := rectEdges(w, h)

	baseEdge, ok := r.value(domainconstraints.RuleEdgeClearance, domainconstraints.AnyNetClass, domainconstraints.AnyLayer)
	if !ok {
		return
	}

	for i := range r.board.Tracks {
		t := &r.board.Tracks[i]
		cls := r.class(t.Net)
		edge := baseEdge
		if v, ok := r.value(domainconstraints.RuleEdgeClearance, cls, t.Layer); ok {
			edge = v
		}
		required := edge + t.Width/2
		for _, seg := range t.Segments() {
			d, pt := segmentRectDistance(seg, edges)
			if d+geometry.Epsilon < required {
				if !r.add(DRCViolation{
					Code:     CodeDRCEdgeClearance,
					Severity: r.severity(domainconstraints.RuleEdgeClearance, cls, t.Layer),
					Message: fmt.Sprintf("piste du net %q trop proche du bord : %.3f mm < %.3f mm",
						t.Net, d, required),
					X: pt.X, Y: pt.Y, Layer: t.Layer, Net: t.Net,
				}) {
					return
				}
			}
		}
	}

	for i := range r.board.Vias {
		v := &r.board.Vias[i]
		cls := r.class(v.Net)
		edge := baseEdge
		if val, ok := r.value(domainconstraints.RuleEdgeClearance, cls, v.FromLayer); ok {
			edge = val
		}
		required := edge + v.Diameter/2
		d, pt := pointRectBoundaryDistance(geometry.Point{X: v.X, Y: v.Y}, edges)
		if d+geometry.Epsilon < required {
			if !r.add(DRCViolation{
				Code:     CodeDRCEdgeClearance,
				Severity: r.severity(domainconstraints.RuleEdgeClearance, cls, v.FromLayer),
				Message: fmt.Sprintf("via du net %q trop proche du bord : %.3f mm < %.3f mm",
					v.Net, d, required),
				X: pt.X, Y: pt.Y, Layer: v.FromLayer, Net: v.Net,
			}) {
				return
			}
		}
	}
}

// checkClearances runs the pairwise isolation checks through the spatial
// hash: piste↔piste, piste↔pad, piste↔via, via↔via.
func (r *drcRun) checkClearances() {
	hash := newDRCHash(r.hashMargin())

	for i := range r.board.Tracks {
		t := &r.board.Tracks[i]
		cls := r.class(t.Net)
		for _, seg := range t.Segments() {
			hash.insert(drcObj{
				kind: kindSegment, seg: seg, layer: t.Layer,
				net: t.Net, cls: cls, width: t.Width,
			})
		}
	}
	last := r.board.LayerCount - 1
	for i := range r.board.Components {
		bc := &r.board.Components[i]
		for _, pad := range bc.Footprint.Pads {
			if pad.Net == "" {
				continue
			}
			pos := bc.PadAbsolutePosition(pad)
			radius := math.Min(pad.Width, pad.Height) / 2
			if radius <= 0 {
				radius = math.Max(pad.Width, pad.Height) / 2
			}
			layer := pad.Layer
			if layer >= r.board.LayerCount {
				layer = last
			}
			hash.insert(drcObj{
				kind:  kindPad,
				seg:   geometry.Segment{A: pos, B: pos},
				layer: layer, net: pad.Net,
				cls: r.class(pad.Net), r: radius,
				padW: pad.Width, padH: pad.Height,
			})
		}
	}
	for i := range r.board.Vias {
		v := &r.board.Vias[i]
		hash.insert(drcObj{
			kind:  kindVia,
			seg:   geometry.Segment{A: geometry.Point{X: v.X, Y: v.Y}, B: geometry.Point{X: v.X, Y: v.Y}},
			viaLo: v.FromLayer, viaHi: v.ToLayer,
			net: v.Net, cls: r.class(v.Net), r: v.Diameter / 2,
		})
	}

	hash.forCandidates(func(i, j int) {
		a, b := &hash.objs[i], &hash.objs[j]
		if a.net == b.net {
			return
		}
		switch {
		case a.kind == kindSegment && b.kind == kindSegment:
			if a.layer != b.layer {
				return
			}
			r.checkSegSeg(a, b)
		case a.kind == kindSegment && b.kind == kindPad:
			if !b.spansLayer(a.layer) {
				return
			}
			r.checkSegPad(a, b)
		case a.kind == kindPad && b.kind == kindSegment:
			if !a.spansLayer(b.layer) {
				return
			}
			r.checkSegPad(b, a)
		case a.kind == kindSegment && b.kind == kindVia:
			if !b.spansLayer(a.layer) {
				return
			}
			r.checkSegVia(a, b)
		case a.kind == kindVia && b.kind == kindSegment:
			if !a.spansLayer(b.layer) {
				return
			}
			r.checkSegVia(b, a)
		case a.kind == kindVia && b.kind == kindVia:
			if !viaLayersOverlap(a, b) {
				return
			}
			r.checkViaVia(a, b)
		default:
			// pad↔pad : hors périmètre contractuel (§4), volontairement ignoré.
		}
	})
}

// hashMargin computes the lookup margin guaranteeing that no violating pair
// is missed: max clearance + largest half-width + largest radius.
func (r *drcRun) hashMargin() float64 {
	margin := r.maxClearMM
	for i := range r.board.Tracks {
		margin = math.Max(margin, r.board.Tracks[i].Width/2)
	}
	for i := range r.board.Vias {
		margin = math.Max(margin, r.board.Vias[i].Diameter/2)
	}
	for i := range r.board.Components {
		for _, pad := range r.board.Components[i].Footprint.Pads {
			margin = math.Max(margin, math.Max(pad.Width, pad.Height)/2)
		}
	}
	return margin + 0.5
}

func (r *drcRun) clearanceFor(a, b *drcObj, layer int) (float64, string) {
	ca, oka := r.value(domainconstraints.RuleMinClearance, a.cls, layer)
	cb, okb := r.value(domainconstraints.RuleMinClearance, b.cls, layer)
	best := r.maxClearMM
	sev := string(domainconstraints.SeverityError)
	if oka {
		best = ca
		sev = r.severity(domainconstraints.RuleMinClearance, a.cls, layer)
	}
	if okb && cb > best {
		best = cb
		sev = r.severity(domainconstraints.RuleMinClearance, b.cls, layer)
	}
	return best, sev
}

func (r *drcRun) checkSegSeg(a, b *drcObj) {
	clearance, sev := r.clearanceFor(a, b, a.layer)
	required := clearance + a.width/2 + b.width/2
	d, pt := segmentSegmentDistance(a.seg, b.seg)
	if d+geometry.Epsilon < required {
		r.add(DRCViolation{
			Code: CodeDRCClearance, Severity: sev,
			Message: fmt.Sprintf("isolation insuffisante entre %q et %q : %.3f mm < %.3f mm",
				a.net, b.net, d, required),
			X: pt.X, Y: pt.Y, Layer: a.layer, Net: a.net,
		})
	}
}

func (r *drcRun) checkSegPad(seg, pad *drcObj) {
	clearance, sev := r.clearanceFor(seg, pad, seg.layer)
	required := clearance + seg.width/2 + pad.r
	d, pt := closestPointOnSegment(pad.seg.A, seg.seg)
	if d+geometry.Epsilon < required {
		r.add(DRCViolation{
			Code: CodeDRCClearance, Severity: sev,
			Message: fmt.Sprintf("isolation insuffisante entre la piste %q et la pastille %q : %.3f mm < %.3f mm",
				seg.net, pad.net, d, required),
			X: pt.X, Y: pt.Y, Layer: seg.layer, Net: seg.net,
		})
	}
}

func (r *drcRun) checkSegVia(seg, via *drcObj) {
	clearance, sev := r.clearanceFor(seg, via, seg.layer)
	required := clearance + seg.width/2 + via.r
	d, pt := closestPointOnSegment(via.seg.A, seg.seg)
	if d+geometry.Epsilon < required {
		r.add(DRCViolation{
			Code: CodeDRCClearance, Severity: sev,
			Message: fmt.Sprintf("isolation insuffisante entre la piste %q et le via %q : %.3f mm < %.3f mm",
				seg.net, via.net, d, required),
			X: pt.X, Y: pt.Y, Layer: seg.layer, Net: seg.net,
		})
	}
}

func (r *drcRun) checkViaVia(a, b *drcObj) {
	clearance, sev := r.clearanceFor(a, b, a.viaLo)
	required := clearance + a.r + b.r
	d := a.seg.A.DistanceTo(b.seg.A)
	if d+geometry.Epsilon < required {
		r.add(DRCViolation{
			Code: CodeDRCClearance, Severity: sev,
			Message: fmt.Sprintf("isolation insuffisante entre les vias %q et %q : %.3f mm < %.3f mm",
				a.net, b.net, d, required),
			X: (a.seg.A.X + b.seg.A.X) / 2, Y: (a.seg.A.Y + b.seg.A.Y) / 2,
			Layer: a.viaLo, Net: a.net,
		})
	}
}

// --------------------------------------------------------------
// Primitives géométriques locales
// --------------------------------------------------------------

// closestPointOnSegment returns the distance from p to segment s and the
// closest point on s.
func closestPointOnSegment(p geometry.Point, s geometry.Segment) (float64, geometry.Point) {
	abx, aby := s.B.X-s.A.X, s.B.Y-s.A.Y
	apx, apy := p.X-s.A.X, p.Y-s.A.Y
	len2 := abx*abx + aby*aby
	if len2 < geometry.Epsilon {
		return p.DistanceTo(s.A), s.A
	}
	t := geometry.Clamp((apx*abx+apy*aby)/len2, 0, 1)
	cp := geometry.Point{X: s.A.X + t*abx, Y: s.A.Y + t*aby}
	return p.DistanceTo(cp), cp
}

// segmentSegmentDistance returns the shortest distance between two segments
// and the closest point on the first one. Crossing segments yield 0.
func segmentSegmentDistance(a, b geometry.Segment) (float64, geometry.Point) {
	if geometry.SegmentsIntersect(a.A, a.B, b.A, b.B) {
		return 0, a.A
	}
	best := math.Inf(1)
	var bestPt geometry.Point
	for _, p := range [4]geometry.Point{a.A, a.B, b.A, b.B} {
		var d float64
		var pt geometry.Point
		if p == a.A || p == a.B {
			d, pt = closestPointOnSegment(p, b)
		} else {
			d, pt = closestPointOnSegment(p, a)
		}
		if d < best {
			best, bestPt = d, pt
		}
	}
	return best, bestPt
}

// rectEdges returns the four boundary segments of the board rectangle.
func rectEdges(w, h float64) [4]geometry.Segment {
	c := []geometry.Point{{X: 0, Y: 0}, {X: w, Y: 0}, {X: w, Y: h}, {X: 0, Y: h}}
	return [4]geometry.Segment{
		{A: c[0], B: c[1]}, {A: c[1], B: c[2]}, {A: c[2], B: c[3]}, {A: c[3], B: c[0]},
	}
}

// segmentRectDistance returns the shortest distance between a segment and
// the board boundary, plus the closest point on the segment.
func segmentRectDistance(s geometry.Segment, edges [4]geometry.Segment) (float64, geometry.Point) {
	best := math.Inf(1)
	var bestPt geometry.Point
	for _, e := range edges {
		d, pt := segmentSegmentDistance(s, e)
		if d < best {
			best, bestPt = d, pt
		}
	}
	return best, bestPt
}

// pointRectBoundaryDistance returns the shortest distance between a point
// and the board boundary, plus the closest boundary point.
func pointRectBoundaryDistance(p geometry.Point, edges [4]geometry.Segment) (float64, geometry.Point) {
	best := math.Inf(1)
	var bestPt geometry.Point
	for _, e := range edges {
		d, pt := closestPointOnSegment(p, e)
		if d < best {
			best, bestPt = d, pt
		}
	}
	return best, bestPt
}

func viaLayersOverlap(a, b *drcObj) bool {
	alo, ahi := a.viaLo, a.viaHi
	blo, bhi := b.viaLo, b.viaHi
	if alo > ahi {
		alo, ahi = ahi, alo
	}
	if blo > bhi {
		blo, bhi = bhi, blo
	}
	return alo <= bhi && blo <= ahi
}

// netClassMap builds the net name → class lookup used by the rule resolver.
func netClassMap(s *domainschematic.Schematic) map[string]domainschematic.NetClass {
	out := map[string]domainschematic.NetClass{}
	if s == nil {
		return out
	}
	for i := range s.Nets {
		out[s.Nets[i].Name] = s.Nets[i].Class
	}
	return out
}

// defaultMaxClearance returns a safe fallback clearance used when no rule
// matches.
func defaultMaxClearance(cs *domainconstraints.ConstraintSet) float64 {
	if v, ok := cs.Value(domainconstraints.RuleMinClearance, domainconstraints.AnyNetClass, domainconstraints.AnyLayer); ok {
		return v
	}
	return 0.2
}
