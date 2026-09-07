// Package layout models the physical board: footprints, placed components,
// tracks, vias and the Board aggregate with its routing bookkeeping.
package layout

import (
	"math"

	"github.com/assihervey-coder/IA-PCB/backend/internal/pkg/geometry"
)

// PadShape enumerates the supported pad geometries.
type PadShape string

const (
	ShapeRect      PadShape = "rect"
	ShapeRoundRect PadShape = "roundrect"
	ShapeCircle    PadShape = "circle"
	ShapeOval      PadShape = "oval"
)

// Pad is a copper landing area of a footprint. Coordinates are relative to
// the footprint origin; Layer is the copper layer index and -1 means
// through-hole (all copper layers).
type Pad struct {
	Name     string
	Shape    PadShape
	X, Y     float64
	Width    float64
	Height   float64
	Rotation float64 // degrés, relatif à l'empreinte
	Layer    int     // -1 = traversant
	Net      string
}

// OnLayer reports whether the pad is present on copper layer idx.
func (p Pad) OnLayer(idx int) bool { return p.Layer < 0 || p.Layer == idx }

// Footprint is the physical definition of a component package.
type Footprint struct {
	Name         string // ex. "SOIC-8_3.9x4.9mm_P1.27mm"
	Value        string
	Pads         []Pad
	BodyWidthMM  float64
	BodyHeightMM float64
	HeightMM     float64 // hauteur 3D (exports STEP/STL)
}

// BBox returns the axis-aligned bounding box of the footprint placed at
// (cx, cy) with rotation rotDeg. The body outline and every pad contribute.
func (f *Footprint) BBox(cx, cy, rotDeg float64) geometry.AABB {
	hw, hh := f.BodyWidthMM/2, f.BodyHeightMM/2
	if hw <= geometry.Epsilon || hh <= geometry.Epsilon {
		// Dériver l'encombrement des pads si le corps n'est pas défini.
		minX, minY, maxX, maxY := 0.0, 0.0, 0.0, 0.0
		for _, p := range f.Pads {
			minX = math.Min(minX, p.X-p.Width/2)
			maxX = math.Max(maxX, p.X+p.Width/2)
			minY = math.Min(minY, p.Y-p.Height/2)
			maxY = math.Max(maxY, p.Y+p.Height/2)
		}
		hw, hh = (maxX-minX)/2, (maxY-minY)/2
	}

	pts := []geometry.Point{
		{X: -hw, Y: -hh}, {X: hw, Y: -hh}, {X: hw, Y: hh}, {X: -hw, Y: hh},
	}
	for _, p := range f.Pads {
		pts = append(pts,
			geometry.Point{X: p.X - p.Width/2, Y: p.Y - p.Height/2},
			geometry.Point{X: p.X + p.Width/2, Y: p.Y + p.Height/2},
		)
	}

	center := geometry.Point{X: cx, Y: cy}
	out := make([]geometry.Point, 0, len(pts))
	for _, pt := range pts {
		rot := geometry.RotatePoint(pt, geometry.Point{}, rotDeg)
		out = append(out, center.Add(rot))
	}
	return geometry.AABBFromPoints(out)
}

// PlacedComponent binds a footprint instance to its physical position on
// the board.
type PlacedComponent struct {
	Ref       string
	Footprint Footprint
	X, Y      float64
	Rotation  float64
	Fixed     bool
}

// BBox returns the bounding box of the placed component.
func (c *PlacedComponent) BBox() geometry.AABB {
	return c.Footprint.BBox(c.X, c.Y, c.Rotation)
}

// PadByName returns the pad with the given name.
func (c *PlacedComponent) PadByName(name string) (*Pad, bool) {
	for i := range c.Footprint.Pads {
		if c.Footprint.Pads[i].Name == name {
			return &c.Footprint.Pads[i], true
		}
	}
	return nil, false
}

// PadAbsolutePosition returns the board position of a pad, taking the
// component rotation into account.
func (c *PlacedComponent) PadAbsolutePosition(p Pad) geometry.Point {
	rot := geometry.RotatePoint(geometry.Point{X: p.X, Y: p.Y}, geometry.Point{}, c.Rotation)
	return geometry.Point{X: c.X + rot.X, Y: c.Y + rot.Y}
}

// PadsOnNet returns every pad of the component connected to the given net.
func (c *PlacedComponent) PadsOnNet(net string) []Pad {
	var pads []Pad
	for _, p := range c.Footprint.Pads {
		if p.Net == net {
			pads = append(pads, p)
		}
	}
	return pads
}
