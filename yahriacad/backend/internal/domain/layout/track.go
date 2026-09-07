package layout

import "github.com/assihervey-coder/IA-PCB/backend/internal/pkg/geometry"

// TrackPoint is one vertex of a track polyline.
type TrackPoint struct {
	X, Y float64
}

// Track is a copper polyline restricted to a single layer. Layer changes are
// materialised by Via objects, never inside a Track.
type Track struct {
	Net    string
	Layer  int
	Width  float64
	Points []TrackPoint
}

// Length returns the cumulative XY length of the polyline (mm).
func (t *Track) Length() float64 {
	var total float64
	for i := 1; i < len(t.Points); i++ {
		a := geometry.Point{X: t.Points[i-1].X, Y: t.Points[i-1].Y}
		b := geometry.Point{X: t.Points[i].X, Y: t.Points[i].Y}
		total += a.DistanceTo(b)
	}
	return total
}

// Segments returns the polyline as a slice of geometry segments.
func (t *Track) Segments() []geometry.Segment {
	if len(t.Points) < 2 {
		return nil
	}
	segs := make([]geometry.Segment, 0, len(t.Points)-1)
	for i := 1; i < len(t.Points); i++ {
		segs = append(segs, geometry.Segment{
			A: geometry.Point{X: t.Points[i-1].X, Y: t.Points[i-1].Y},
			B: geometry.Point{X: t.Points[i].X, Y: t.Points[i].Y},
		})
	}
	return segs
}

// BBox returns the bounding box of the polyline, grown by half the track
// width so clearance checks see the copper edge, not the centerline.
func (t *Track) BBox() geometry.AABB {
	pts := make([]geometry.Point, 0, len(t.Points))
	for _, p := range t.Points {
		pts = append(pts, geometry.Point{X: p.X, Y: p.Y})
	}
	return geometry.AABBFromPoints(pts).Expand(t.Width / 2)
}
