// Package geometry provides pure 2D geometric primitives shared by the
// domain layers (layout, DRC checks, exporters). All coordinates are in
// millimeters with the origin at the top-left corner of the board.
package geometry

import "math"

// Epsilon is the tolerance used for floating point comparisons.
const Epsilon = 1e-9

// Point is a 2D point (or vector) in millimeters.
type Point struct {
	X, Y float64
}

// Add returns p + o.
func (p Point) Add(o Point) Point { return Point{X: p.X + o.X, Y: p.Y + o.Y} }

// Sub returns p - o.
func (p Point) Sub(o Point) Point { return Point{X: p.X - o.X, Y: p.Y - o.Y} }

// Scale returns p multiplied by scalar s.
func (p Point) Scale(s float64) Point { return Point{X: p.X * s, Y: p.Y * s} }

// DistanceTo returns the Euclidean distance between p and o.
func (p Point) DistanceTo(o Point) float64 {
	dx, dy := p.X-o.X, p.Y-o.Y
	return math.Hypot(dx, dy)
}

// Segment is a straight line segment between two points.
type Segment struct {
	A, B Point
}

// Length returns the Euclidean length of the segment.
func (s Segment) Length() float64 { return s.A.DistanceTo(s.B) }

// PointSegmentDistance returns the shortest distance between point p and
// the segment delimited by a and b.
func PointSegmentDistance(p, a, b Point) float64 {
	abx, aby := b.X-a.X, b.Y-a.Y
	apx, apy := p.X-a.X, p.Y-a.Y
	abLen2 := abx*abx + aby*aby
	if abLen2 < Epsilon {
		return p.DistanceTo(a)
	}
	t := Clamp((apx*abx+apy*aby)/abLen2, 0, 1)
	closest := Point{X: a.X + t*abx, Y: a.Y + t*aby}
	return p.DistanceTo(closest)
}

// orientation returns >0 for counter-clockwise, <0 for clockwise and ~0 for
// collinear, given the ordered triplet (a, b, c).
func orientation(a, b, c Point) float64 {
	return (b.Y-a.Y)*(c.X-b.X) - (b.X-a.X)*(c.Y-b.Y)
}

// onSegment reports whether collinear point p lies on segment ab.
func onSegment(a, b, p Point) bool {
	return p.X >= math.Min(a.X, b.X)-Epsilon && p.X <= math.Max(a.X, b.X)+Epsilon &&
		p.Y >= math.Min(a.Y, b.Y)-Epsilon && p.Y <= math.Max(a.Y, b.Y)+Epsilon
}

// SegmentsIntersect reports whether segments p1p2 and p3p4 intersect,
// including touching endpoints and collinear overlaps.
func SegmentsIntersect(p1, p2, p3, p4 Point) bool {
	d1 := orientation(p3, p4, p1)
	d2 := orientation(p3, p4, p2)
	d3 := orientation(p1, p2, p3)
	d4 := orientation(p1, p2, p4)

	if ((d1 > Epsilon && d2 < -Epsilon) || (d1 < -Epsilon && d2 > Epsilon)) &&
		((d3 > Epsilon && d4 < -Epsilon) || (d3 < -Epsilon && d4 > Epsilon)) {
		return true
	}
	if math.Abs(d1) < Epsilon && onSegment(p3, p4, p1) {
		return true
	}
	if math.Abs(d2) < Epsilon && onSegment(p3, p4, p2) {
		return true
	}
	if math.Abs(d3) < Epsilon && onSegment(p1, p2, p3) {
		return true
	}
	if math.Abs(d4) < Epsilon && onSegment(p1, p2, p4) {
		return true
	}
	return false
}

// AABB is an axis-aligned bounding box.
type AABB struct {
	MinX, MinY, MaxX, MaxY float64
}

// Width returns the X extent of the box.
func (b AABB) Width() float64 { return b.MaxX - b.MinX }

// Height returns the Y extent of the box.
func (b AABB) Height() float64 { return b.MaxY - b.MinY }

// Center returns the box center point.
func (b AABB) Center() Point {
	return Point{X: (b.MinX + b.MaxX) / 2, Y: (b.MinY + b.MaxY) / 2}
}

// Contains reports whether point p lies inside (or on the edge of) the box.
func (b AABB) Contains(p Point) bool {
	return p.X >= b.MinX-Epsilon && p.X <= b.MaxX+Epsilon &&
		p.Y >= b.MinY-Epsilon && p.Y <= b.MaxY+Epsilon
}

// Overlaps reports whether the two boxes intersect.
func (b AABB) Overlaps(o AABB) bool {
	return b.MinX <= o.MaxX+Epsilon && o.MinX <= b.MaxX+Epsilon &&
		b.MinY <= o.MaxY+Epsilon && o.MinY <= b.MaxY+Epsilon
}

// Expand returns the box grown by margin m on every side.
func (b AABB) Expand(m float64) AABB {
	return AABB{MinX: b.MinX - m, MinY: b.MinY - m, MaxX: b.MaxX + m, MaxY: b.MaxY + m}
}

// Union returns the smallest box containing both b and o.
func (b AABB) Union(o AABB) AABB {
	return AABB{
		MinX: math.Min(b.MinX, o.MinX),
		MinY: math.Min(b.MinY, o.MinY),
		MaxX: math.Max(b.MaxX, o.MaxX),
		MaxY: math.Max(b.MaxY, o.MaxY),
	}
}

// AABBFromPoints returns the smallest box containing all points.
// An empty input yields the zero box.
func AABBFromPoints(pts []Point) AABB {
	var box AABB
	for i, p := range pts {
		if i == 0 {
			box = AABB{MinX: p.X, MinY: p.Y, MaxX: p.X, MaxY: p.Y}
			continue
		}
		box.MinX = math.Min(box.MinX, p.X)
		box.MinY = math.Min(box.MinY, p.Y)
		box.MaxX = math.Max(box.MaxX, p.X)
		box.MaxY = math.Max(box.MaxY, p.Y)
	}
	return box
}

// Polygon is a closed polygon (vertices in order, last joined to first).
type Polygon []Point

// Area returns the absolute shoelace area of the polygon.
func (p Polygon) Area() float64 {
	var a float64
	n := len(p)
	for i := 0; i < n; i++ {
		j := (i + 1) % n
		a += p[i].X*p[j].Y - p[j].X*p[i].Y
	}
	return math.Abs(a) / 2
}

// Perimeter returns the total boundary length of the polygon.
func (p Polygon) Perimeter() float64 {
	var total float64
	n := len(p)
	for i := 0; i < n; i++ {
		total += p[i].DistanceTo(p[(i+1)%n])
	}
	return total
}

// Centroid returns the area centroid of the polygon.
func (p Polygon) Centroid() Point {
	var cx, cy, a float64
	n := len(p)
	for i := 0; i < n; i++ {
		j := (i + 1) % n
		cross := p[i].X*p[j].Y - p[j].X*p[i].Y
		a += cross
		cx += (p[i].X + p[j].X) * cross
		cy += (p[i].Y + p[j].Y) * cross
	}
	if math.Abs(a) < Epsilon {
		return AABBFromPoints(p).Center()
	}
	a /= 2
	return Point{X: cx / (6 * a), Y: cy / (6 * a)}
}

// RotatePoint returns point p rotated around center by deg degrees
// (counter-clockwise in the standard math sense).
func RotatePoint(p, center Point, deg float64) Point {
	rad := deg * math.Pi / 180
	sin, cos := math.Sin(rad), math.Cos(rad)
	dx, dy := p.X-center.X, p.Y-center.Y
	return Point{
		X: center.X + dx*cos - dy*sin,
		Y: center.Y + dx*sin + dy*cos,
	}
}

// NormalizeDeg normalizes an angle to [0, 360).
func NormalizeDeg(deg float64) float64 {
	d := math.Mod(deg, 360)
	if d < 0 {
		d += 360
	}
	return d
}

// Clamp constrains v within [lo, hi].
func Clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
