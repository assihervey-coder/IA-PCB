package layout

import (
	"errors"
	"fmt"
	"strings"

	"github.com/kidcad/kidcad-pro-ia/backend/internal/pkg/geometry"
	"github.com/kidcad/kidcad-pro-ia/backend/internal/pkg/utils"
)

// CopperPour is a filled copper region tied to a net (typically GND):
// a ground plane on an inner layer, a bottom pour, or a stitched island.
// Real EDA tools keep pours as boolean polygons; KidCAD stores the outline
// plus the computed fill statistics so the pour survives persistence and
// can be re-filled after each routing pass.
type CopperPour struct {
	ID   string `json:"id"`
	Net  string `json:"net"`
	Name string `json:"name,omitempty"`

	Layer int `json:"layer"`

	// Outline is the pour polygon in millimetres (closed implicitly:
	// the last vertex joins the first).
	Outline []geometry.Point `json:"outline"`

	// ClearanceMM is the copper-to-copper keepout applied around every
	// foreign track, pad and via crossing the pour.
	ClearanceMM float64 `json:"clearance_mm"`

	// HatchMM > 0 renders the pour as a hatch grid (thermal-friendly),
	// 0 keeps it solid.
	HatchMM float64 `json:"hatch_mm,omitempty"`

	// IsGround marks plane pours participating in the return-current
	// path (used by the doctor and the SI checker).
	IsGround bool `json:"is_ground"`

	// FillPct is the computed copper coverage of the outline (0..100)
	// after subtracting the clearance zones of crossing geometry.
	FillPct float64 `json:"fill_pct"`

	// AreaMM2 is the outline area in square millimetres.
	AreaMM2 float64 `json:"area_mm2"`

	// Stitched counts the same-net vias landing inside the outline.
	Stitched int `json:"stitched"`
}

// Pour defaults.
const (
	DefaultPourClearanceMM = 0.3
	MinPourClearanceMM     = 0.05
	MinPourVertices        = 3
)

// Validate checks the intrinsic consistency of a pour.
func (p *CopperPour) Validate() error {
	if strings.TrimSpace(p.Net) == "" {
		return errors.New("layout : un plan de cuivre doit être rattaché à un net")
	}
	if len(p.Outline) < MinPourVertices {
		return fmt.Errorf("layout : le contour du plan de cuivre %q doit avoir au moins %d sommets", p.Net, MinPourVertices)
	}
	if p.ClearanceMM < MinPourClearanceMM {
		return fmt.Errorf("layout : isolement du plan de cuivre %.3f mm < %.2f mm", p.ClearanceMM, MinPourClearanceMM)
	}
	if p.HatchMM < 0 {
		return errors.New("layout : la période de hachure ne peut pas être négative")
	}
	if area := geometry.Polygon(p.Outline).Area(); area <= geometry.Epsilon {
		return errors.New("layout : le contour du plan de cuivre est dégénéré (aire nulle)")
	}
	return nil
}

// Area returns the shoelace area of the pour outline (mm²).
func (p *CopperPour) Area() float64 { return geometry.Polygon(p.Outline).Area() }

// Contains reports whether the point lies inside the pour outline.
func (p *CopperPour) Contains(x, y float64) bool {
	return geometry.Polygon(p.Outline).ContainsPoint(geometry.Point{X: x, Y: y})
}

// DistanceToBoundary returns the distance from the point to the outline;
// 0 when the point is inside.
func (p *CopperPour) DistanceToBoundary(x, y float64) float64 {
	return geometry.Polygon(p.Outline).DistanceToPoint(geometry.Point{X: x, Y: y})
}

// RectOutline builds the 4-vertex outline of an axis-aligned rectangle
// (used by the plane generator: board outline inset by the edge margin).
func RectOutline(minX, minY, maxX, maxY float64) []geometry.Point {
	return []geometry.Point{
		{X: minX, Y: minY},
		{X: maxX, Y: minY},
		{X: maxX, Y: maxY},
		{X: minX, Y: maxY},
	}
}

// AddPour appends a copper pour after validation, assigning an identifier
// when missing. Layer bounds are checked against the board.
func (b *Board) AddPour(p CopperPour) (*CopperPour, error) {
	p.Net = strings.TrimSpace(p.Net)
	p.ID = strings.TrimSpace(p.ID)
	if p.ID == "" {
		p.ID = NewPourID(p.Net, p.Layer)
	}
	if p.Layer < 0 || (b.LayerCount > 0 && p.Layer >= b.LayerCount) {
		return nil, fmt.Errorf("layout : couche %d hors stack pour un plan de cuivre", p.Layer)
	}
	if p.ClearanceMM <= 0 {
		p.ClearanceMM = DefaultPourClearanceMM
	}
	if err := p.Validate(); err != nil {
		return nil, err
	}
	if p.FillPct < 0 {
		p.FillPct = 0
	} else if p.FillPct > 100 {
		p.FillPct = 100
	}
	p.AreaMM2 = p.Area()
	b.Pours = append(b.Pours, p)
	return &b.Pours[len(b.Pours)-1], nil
}

// PourByID looks a pour up by identifier.
func (b *Board) PourByID(id string) (*CopperPour, bool) {
	for i := range b.Pours {
		if b.Pours[i].ID == id {
			return &b.Pours[i], true
		}
	}
	return nil, false
}

// PoursForLayer returns every pour living on the given layer.
func (b *Board) PoursForLayer(layer int) []CopperPour {
	var out []CopperPour
	for i := range b.Pours {
		if b.Pours[i].Layer == layer {
			out = append(out, b.Pours[i])
		}
	}
	return out
}

// RemovePour deletes a pour by identifier. Returns true when found.
func (b *Board) RemovePour(id string) bool {
	for i := range b.Pours {
		if b.Pours[i].ID == id {
			b.Pours = append(b.Pours[:i], b.Pours[i+1:]...)
			return true
		}
	}
	return false
}

// RemovePoursForNet deletes every pour tied to the net (used before
// re-generating planes). Returns the number of removed pours.
func (b *Board) RemovePoursForNet(net string) int {
	kept := b.Pours[:0]
	removed := 0
	for _, p := range b.Pours {
		if p.Net == net {
			removed++
			continue
		}
		kept = append(kept, p)
	}
	b.Pours = kept
	return removed
}

// NewPourID derives a unique, readable identifier from net and layer.
func NewPourID(net string, layer int) string {
	return fmt.Sprintf("pour-%s-L%d-%s", sanitizeIDPart(net), layer, utils.NewID())
}

// sanitizeIDPart keeps the identifier readable and filesystem-safe.
func sanitizeIDPart(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "net"
	}
	return out
}
