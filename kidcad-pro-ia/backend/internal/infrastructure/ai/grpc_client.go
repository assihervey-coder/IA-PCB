// Package ai implements the gRPC client toward the Python AI engine
// (ai-engine, service kidcad.pcb.v1.AIRouterService). It satisfies the
// layoutapp.AIService port and maps transport failures onto the sentinel
// layoutapp.ErrAIUnreachable so the REST layer can answer 503 ai_unreachable.
package ai

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	layoutapp "github.com/kidcad/kidcad-pro-ia/backend/internal/application/layout"
	domainconstraints "github.com/kidcad/kidcad-pro-ia/backend/internal/domain/constraints"
	domainlayout "github.com/kidcad/kidcad-pro-ia/backend/internal/domain/layout"
	domainschematic "github.com/kidcad/kidcad-pro-ia/backend/internal/domain/schematic"
	pcbv1 "github.com/kidcad/kidcad-pro-ia/shared/gen/go/pcbv1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

// unaryBudget bounds the synchronous placement call: simulated annealing on
// a realistic board takes seconds, so the short probe timeout is not reused.
const unaryBudget = 60 * time.Second

// Defaults applied when the constraint set carries no matching rule.
const (
	defaultTrackWidthMM = 0.25
	defaultClearanceMM  = 0.2
)

// Client is the gRPC adapter implementing layoutapp.AIService.
type Client struct {
	conn    *grpc.ClientConn
	stub    pcbv1.AIRouterServiceClient
	timeout time.Duration
	log     *slog.Logger
}

// Compile-time check: the adapter satisfies the application port.
var _ layoutapp.AIService = (*Client)(nil)

// NewClient opens a (lazy) connection to the AI engine. The connection is
// not established here: the first RPC triggers it, so an unreachable engine
// never prevents the backend from starting.
func NewClient(addr string, timeout time.Duration, log *slog.Logger) (*Client, error) {
	if addr == "" {
		return nil, errors.New("ai : adresse du moteur IA vide")
	}
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	if log == nil {
		log = slog.Default()
	}
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("ai : connexion gRPC impossible : %w", err)
	}
	return &Client{
		conn:    conn,
		stub:    pcbv1.NewAIRouterServiceClient(conn),
		timeout: timeout,
		log:     log,
	}, nil
}

// Close releases the underlying connection.
func (c *Client) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

// Health probes the engine within the configured timeout.
func (c *Client) Health(ctx context.Context) error {
	_, err := c.EngineInfo(ctx)
	return err
}

// EngineInfo reports the engine runtime configuration (GetHealth RPC):
// device d'inférence, modèle RL chargé ou non, version du service.
func (c *Client) EngineInfo(ctx context.Context) (layoutapp.EngineInfo, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	resp, err := c.stub.GetHealth(ctx, &pcbv1.HealthRequest{})
	if err != nil {
		return layoutapp.EngineInfo{}, c.mapError(err, "health")
	}
	info := layoutapp.EngineInfo{
		Status:      resp.GetStatus(),
		Version:     resp.GetVersion(),
		Device:      resp.GetDevice(),
		ModelLoaded: resp.GetModelLoaded(),
	}
	if s := resp.GetStatus(); s != "" && s != "ok" && s != "degraded" {
		return info, fmt.Errorf("ai : état du moteur inattendu %q", s)
	}
	return info, nil
}

// PlanPlacement asks the engine for component positions and maps them back
// onto the board components by reference designator.
func (c *Client) PlanPlacement(ctx context.Context, b *domainlayout.Board,
	comps []domainschematic.Component, strategy string) ([]domainlayout.PlacedComponent, error) {
	if b == nil {
		return nil, errors.New("ai : carte absente pour le placement")
	}

	specs := make([]*pcbv1.ComponentSpec, 0, len(comps))
	for _, comp := range comps {
		spec := &pcbv1.ComponentSpec{
			Ref:         comp.Ref,
			Footprint:   comp.Footprint,
			Position:    &pcbv1.Point{X: comp.X, Y: comp.Y},
			RotationDeg: comp.Rotation,
			Fixed:       comp.Fixed,
		}
		if placed, ok := b.ComponentByRef(comp.Ref); ok {
			bb := placed.BBox()
			spec.BboxMm = &pcbv1.BBox{MinX: bb.MinX, MinY: bb.MinY, MaxX: bb.MaxX, MaxY: bb.MaxY}
			spec.HeightMm = placed.Footprint.HeightMM
			if spec.Footprint == "" {
				spec.Footprint = placed.Footprint.Name
			}
		}
		specs = append(specs, spec)
	}

	req := &pcbv1.PlacementRequest{
		Board:      boardSpec(b),
		Components: specs,
		Strategy:   strategy,
	}

	ctx, cancel := context.WithTimeout(ctx, unaryBudget)
	defer cancel()
	resp, err := c.stub.PlanPlacement(ctx, req)
	if err != nil {
		return nil, c.mapError(err, "placement")
	}

	placed := make([]domainlayout.PlacedComponent, 0, len(resp.GetPlaced()))
	for _, p := range resp.GetPlaced() {
		pc := domainlayout.PlacedComponent{
			Ref:      p.GetRef(),
			X:        p.GetPosition().GetX(),
			Y:        p.GetPosition().GetY(),
			Rotation: p.GetRotationDeg(),
			Fixed:    p.GetFixed(),
		}
		// L'empreinte physique (pads, nets) appartient au backend : elle est
		// reprise du board d'origine, la réponse ne transportant que la
		// position.
		if prev, ok := b.ComponentByRef(p.GetRef()); ok {
			pc.Footprint = prev.Footprint
		} else {
			bb := p.GetBboxMm()
			pc.Footprint = domainlayout.Footprint{
				Name:         p.GetFootprint(),
				BodyWidthMM:  bb.GetMaxX() - bb.GetMinX(),
				BodyHeightMM: bb.GetMaxY() - bb.GetMinY(),
				HeightMM:     p.GetHeightMm(),
			}
			if pc.Footprint.HeightMM <= 0 {
				pc.Footprint.HeightMM = domainlayout.DefaultBoardHeightMM
			}
		}
		placed = append(placed, pc)
	}
	return placed, nil
}

// RouteBoard streams the routing of the requested nets, forwarding progress
// events and accumulating the per-net results.
func (c *Client) RouteBoard(ctx context.Context, b *domainlayout.Board,
	nets []domainschematic.Net, cs *domainconstraints.ConstraintSet, strategy string,
	onProgress func(layoutapp.RouteProgress)) (*layoutapp.RouteOutcome, error) {
	if b == nil {
		return nil, errors.New("ai : carte absente pour le routage")
	}
	start := time.Now()

	req := &pcbv1.RouteRequest{
		Board:     boardSpec(b),
		Nets:      c.netSpecs(b, nets, cs),
		Placed:    componentSpecs(b),
		Strategy:  strategy,
		NetFilter: netNames(nets),
	}

	stream, err := c.stub.RouteBoard(ctx, req)
	if err != nil {
		return nil, c.mapError(err, "routage")
	}
	outcome, err := c.consumeStream(ctx, stream, layoutapp.KindRoute, onProgress)
	if err != nil {
		return nil, err
	}
	outcome.Strategy = strategy
	outcome.DurationMS = time.Since(start).Milliseconds()
	return outcome, nil
}

// OptimizeRoutes streams the post-routing optimisation of a previous
// outcome (rip-up & reroute, via reduction).
func (c *Client) OptimizeRoutes(ctx context.Context, b *domainlayout.Board,
	outcome *layoutapp.RouteOutcome, cs *domainconstraints.ConstraintSet,
	onProgress func(layoutapp.RouteProgress)) (*layoutapp.RouteOutcome, error) {
	if b == nil {
		return nil, errors.New("ai : carte absente pour l'optimisation")
	}
	if outcome == nil {
		return nil, errors.New("ai : résultat de routage absent pour l'optimisation")
	}
	start := time.Now()

	nets := netsFromOutcome(b, outcome)
	req := &pcbv1.OptimizeRequest{
		Board:      boardSpec(b),
		Nets:       c.netSpecs(b, nets, cs),
		Routes:     routesToProto(outcome.Nets),
		Objectives: []string{"length", "vias", "drc"},
	}

	stream, err := c.stub.OptimizeRoutes(ctx, req)
	if err != nil {
		return nil, c.mapError(err, "optimisation")
	}
	optimized, err := c.consumeStream(ctx, stream, layoutapp.KindOptimize, onProgress)
	if err != nil {
		return nil, err
	}
	optimized.Strategy = outcome.Strategy
	optimized.DurationMS = time.Since(start).Milliseconds()
	return optimized, nil
}

// consumeStream drains a ProgressEvent stream: events are forwarded to
// onProgress, partial results accumulated, error events abort the run.
func (c *Client) consumeStream(ctx context.Context,
	stream interface {
		Recv() (*pcbv1.ProgressEvent, error)
	},
	stage string, onProgress func(layoutapp.RouteProgress)) (*layoutapp.RouteOutcome, error) {

	outcome := &layoutapp.RouteOutcome{Nets: []layoutapp.NetRoute{}}
	for {
		ev, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, ctxErr
			}
			return nil, c.mapError(err, "flux de progression")
		}
		if errMsg := ev.GetError(); errMsg != "" {
			return nil, fmt.Errorf("moteur IA : %s", errMsg)
		}
		if onProgress != nil {
			onProgress(layoutapp.RouteProgress{
				JobID:      ev.GetJobId(),
				Stage:      stage,
				CurrentNet: ev.GetCurrentNet(),
				Message:    ev.GetMessage(),
				Percent:    float64(ev.GetPercent()),
				Done:       ev.GetDone(),
				Err:        ev.GetError(),
			})
		}
		if partial := ev.GetPartial(); partial != nil && partial.GetNet() != "" {
			outcome.Nets = append(outcome.Nets, netRouteFromProto(partial))
		}
	}
	return outcome, nil
}

// mapError wraps gRPC failures: transport-level problems become
// layoutapp.ErrAIUnreachable (errors.Is-compatible), the rest are generic.
func (c *Client) mapError(err error, action string) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("moteur IA (%s) : %w", action, err)
	}
	switch status.Code(err) {
	case codes.Unavailable, codes.DeadlineExceeded, codes.Canceled:
		return fmt.Errorf("%w : %v", layoutapp.ErrAIUnreachable, err)
	default:
		return fmt.Errorf("moteur IA (%s) : %w", action, err)
	}
}

// --------------------------------------------------------------
// Mapping domaine -> protobuf
// --------------------------------------------------------------

// boardSpec maps the board outline onto the protobuf message.
func boardSpec(b *domainlayout.Board) *pcbv1.BoardSpec {
	if b == nil {
		return nil
	}
	return &pcbv1.BoardSpec{
		WidthMm:    b.WidthMM,
		HeightMm:   b.HeightMM,
		LayerCount: int32(b.LayerCount),
		LayerNames: b.LayerNames,
	}
}

// componentSpecs maps the placed components onto the protobuf message.
func componentSpecs(b *domainlayout.Board) []*pcbv1.ComponentSpec {
	if b == nil {
		return nil
	}
	specs := make([]*pcbv1.ComponentSpec, 0, len(b.Components))
	for i := range b.Components {
		pc := &b.Components[i]
		bb := pc.BBox()
		specs = append(specs, &pcbv1.ComponentSpec{
			Ref:         pc.Ref,
			Footprint:   pc.Footprint.Name,
			Position:    &pcbv1.Point{X: pc.X, Y: pc.Y},
			RotationDeg: pc.Rotation,
			Fixed:       pc.Fixed,
			BboxMm:      &pcbv1.BBox{MinX: bb.MinX, MinY: bb.MinY, MaxX: bb.MaxX, MaxY: bb.MaxY},
			HeightMm:    pc.Footprint.HeightMM,
		})
	}
	return specs
}

// netSpecs maps the logical nets with their pads (absolute positions) and
// per-class rule values onto the protobuf message.
func (c *Client) netSpecs(b *domainlayout.Board, nets []domainschematic.Net,
	cs *domainconstraints.ConstraintSet) []*pcbv1.NetSpec {

	specs := make([]*pcbv1.NetSpec, 0, len(nets))
	for _, n := range nets {
		class := string(n.Class)
		if class == "" {
			class = string(domainschematic.ClassDefault)
		}
		spec := &pcbv1.NetSpec{
			Name:     n.Name,
			NetClass: class,
			Pads:     padRefsForNet(b, n.Name),
		}
		if v, ok := constraintValue(cs, domainconstraints.RuleMinTrackWidth, class); ok {
			spec.MinTrackWidthMm = v
		} else {
			spec.MinTrackWidthMm = defaultTrackWidthMM
		}
		if v, ok := constraintValue(cs, domainconstraints.RuleMinClearance, class); ok {
			spec.ClearanceMm = v
		} else {
			spec.ClearanceMm = defaultClearanceMM
		}
		specs = append(specs, spec)
	}
	return specs
}

// constraintValue reads a rule value, ignoring scoping layers (net class is
// the meaningful dimension here).
func constraintValue(cs *domainconstraints.ConstraintSet, t domainconstraints.RuleType, netClass string) (float64, bool) {
	if cs == nil {
		return 0, false
	}
	return cs.Value(t, netClass, domainconstraints.AnyLayer)
}

// padRefsForNet collects the absolute pad positions of every component
// connected to the net. Through-hole pads (Layer -1) are exported on both
// outer layers so the router can reach them from either side.
func padRefsForNet(b *domainlayout.Board, net string) []*pcbv1.PadRef {
	if b == nil {
		return nil
	}
	var refs []*pcbv1.PadRef
	for i := range b.Components {
		pc := &b.Components[i]
		for _, pad := range pc.Footprint.Pads {
			if pad.Net != net {
				continue
			}
			pos := pc.PadAbsolutePosition(pad)
			layers := []int32{int32(pad.Layer)}
			if pad.Layer < 0 {
				layers = []int32{0, int32(b.LayerCount - 1)}
			}
			for _, layer := range layers {
				refs = append(refs, &pcbv1.PadRef{
					ComponentRef: pc.Ref,
					PadName:      pad.Name,
					Position:     &pcbv1.Point{X: pos.X, Y: pos.Y},
					Layer:        layer,
					WidthMm:      pad.Width,
					HeightMm:     pad.Height,
					RotationDeg:  pad.Rotation,
				})
			}
		}
	}
	return refs
}

// netNames returns the net names of a list (used as the gRPC net_filter).
func netNames(nets []domainschematic.Net) []string {
	out := make([]string, 0, len(nets))
	for _, n := range nets {
		out = append(out, n.Name)
	}
	return out
}

// netsFromOutcome rebuilds the minimal net list touched by an outcome
// (name + connections derived from the board pads) for the optimiser.
func netsFromOutcome(b *domainlayout.Board, outcome *layoutapp.RouteOutcome) []domainschematic.Net {
	if outcome == nil {
		return nil
	}
	out := make([]domainschematic.Net, 0, len(outcome.Nets))
	for _, nr := range outcome.Nets {
		if nr.Net == "" {
			continue
		}
		out = append(out, domainschematic.Net{Name: nr.Net, Class: domainschematic.ClassDefault})
	}
	return out
}

// routesToProto maps the outcome routes back onto protobuf results.
func routesToProto(nets []layoutapp.NetRoute) []*pcbv1.RouteNetResult {
	out := make([]*pcbv1.RouteNetResult, 0, len(nets))
	for _, nr := range nets {
		out = append(out, &pcbv1.RouteNetResult{
			Net:       nr.Net,
			Segments:  tracksToProto(nr.Tracks),
			Vias:      viasToProto(nr.Vias),
			LengthMm:  nr.LengthMM,
			Completed: nr.Completed,
		})
	}
	return out
}

// tracksToProto flattens the track polylines into segment pairs.
func tracksToProto(tracks []domainlayout.Track) []*pcbv1.TrackSegment {
	var segs []*pcbv1.TrackSegment
	for _, t := range tracks {
		for i := 1; i < len(t.Points); i++ {
			segs = append(segs, &pcbv1.TrackSegment{
				A: &pcbv1.TrackPoint{
					Position: &pcbv1.Point{X: t.Points[i-1].X, Y: t.Points[i-1].Y},
					Layer:    int32(t.Layer),
				},
				B: &pcbv1.TrackPoint{
					Position: &pcbv1.Point{X: t.Points[i].X, Y: t.Points[i].Y},
					Layer:    int32(t.Layer),
				},
				WidthMm: t.Width,
			})
		}
	}
	return segs
}

// viasToProto maps the vias onto the protobuf message.
func viasToProto(vias []domainlayout.Via) []*pcbv1.Via {
	out := make([]*pcbv1.Via, 0, len(vias))
	for _, v := range vias {
		out = append(out, &pcbv1.Via{
			Position:   &pcbv1.Point{X: v.X, Y: v.Y},
			FromLayer:  int32(v.FromLayer),
			ToLayer:    int32(v.ToLayer),
			DiameterMm: v.Diameter,
			DrillMm:    v.Drill,
		})
	}
	return out
}

// --------------------------------------------------------------
// Mapping protobuf -> domaine
// --------------------------------------------------------------

// netRouteFromProto converts one partial routing result into domain tracks
// and vias. Contiguous same-layer segments are merged back into single
// polylines; layer changes are kept as separate tracks (linked by vias).
func netRouteFromProto(p *pcbv1.RouteNetResult) layoutapp.NetRoute {
	nr := layoutapp.NetRoute{
		Net:       p.GetNet(),
		Tracks:    []domainlayout.Track{},
		Vias:      []domainlayout.Via{},
		LengthMM:  p.GetLengthMm(),
		Completed: p.GetCompleted(),
	}

	var current *domainlayout.Track
	for _, seg := range p.GetSegments() {
		layer := int(seg.GetA().GetLayer())
		width := seg.GetWidthMm()
		ax, ay := seg.GetA().GetPosition().GetX(), seg.GetA().GetPosition().GetY()
		bx, by := seg.GetB().GetPosition().GetX(), seg.GetB().GetPosition().GetY()

		if current != nil && current.Layer == layer && sameWidth(current.Width, width) &&
			continues(current, ax, ay) {
			if current.Width == 0 {
				current.Width = width
			}
			current.Points = append(current.Points, domainlayout.TrackPoint{X: bx, Y: by})
			continue
		}
		nr.Tracks = append(nr.Tracks, domainlayout.Track{
			Net:    nr.Net,
			Layer:  layer,
			Width:  width,
			Points: []domainlayout.TrackPoint{{X: ax, Y: ay}, {X: bx, Y: by}},
		})
		current = &nr.Tracks[len(nr.Tracks)-1]
	}

	for _, v := range p.GetVias() {
		nr.Vias = append(nr.Vias, domainlayout.Via{
			X:         v.GetPosition().GetX(),
			Y:         v.GetPosition().GetY(),
			FromLayer: int(v.GetFromLayer()),
			ToLayer:   int(v.GetToLayer()),
			Diameter:  v.GetDiameterMm(),
			Drill:     v.GetDrillMm(),
			Net:       nr.Net,
		})
	}

	if nr.LengthMM == 0 {
		for i := range nr.Tracks {
			nr.LengthMM += nr.Tracks[i].Length()
		}
	}
	return nr
}

// sameWidth compares two track widths with a small tolerance; a zero width
// means "undefined" and always merges (the caller adopts the defined one).
func sameWidth(a, b float64) bool {
	if a == 0 || b == 0 {
		return true
	}
	const eps = 1e-6
	return a >= b-eps && a <= b+eps
}

// continues reports whether a segment starting at (x, y) extends the last
// point of the current polyline.
func continues(t *domainlayout.Track, x, y float64) bool {
	if t == nil || len(t.Points) == 0 {
		return false
	}
	last := t.Points[len(t.Points)-1]
	const eps = 1e-6
	dx := last.X - x
	dy := last.Y - y
	return dx*dx+dy*dy <= eps*eps
}
