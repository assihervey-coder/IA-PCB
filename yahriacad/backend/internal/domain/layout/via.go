package layout

import "errors"

var (
	ErrInvalidViaDiameter = errors.New("layout : diamètre de via invalide")
	ErrInvalidViaDrill    = errors.New("layout : diamètre de perçage invalide")
	ErrInvalidViaLayers   = errors.New("layout : couches de via invalides")
)

// Via is a vertical copper barrel connecting two copper layers.
type Via struct {
	X, Y      float64
	FromLayer int
	ToLayer   int
	Diameter  float64
	Drill     float64
	Net       string
}

// Validate checks the intrinsic consistency of the via.
func (v *Via) Validate() error {
	if v.Diameter <= 0 {
		return ErrInvalidViaDiameter
	}
	if v.Drill <= 0 || v.Drill >= v.Diameter {
		return ErrInvalidViaDrill
	}
	if v.FromLayer < 0 || v.ToLayer < 0 || v.FromLayer == v.ToLayer {
		return ErrInvalidViaLayers
	}
	return nil
}

// AnnularRing returns the annular copper ring width (mm): (diameter-drill)/2.
func (v *Via) AnnularRing() float64 { return (v.Diameter - v.Drill) / 2 }

// OnLayer reports whether the via reaches the given copper layer (inclusive).
func (v *Via) OnLayer(idx int) bool {
	lo, hi := v.FromLayer, v.ToLayer
	if lo > hi {
		lo, hi = hi, lo
	}
	return idx >= lo && idx <= hi
}
