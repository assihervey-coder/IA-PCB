package layout

import (
	"errors"
	"fmt"
	"strings"

	"github.com/assihervey-coder/IA-PCB/backend/internal/pkg/geometry"
)

// ErrDuplicateComponentRef is raised when two placed components share the
// same reference designator.
var ErrDuplicateComponentRef = errors.New("layout : référence de composant dupliquée")

// DefaultBoardHeightMM is the 3D height assumed for the bare board.
const DefaultBoardHeightMM = 1.6

// Board is the physical aggregate of a PCB design: placed components, copper
// tracks and vias, copper pours (ground planes) inside a rectangular outline.
type Board struct {
	WidthMM    float64
	HeightMM   float64
	LayerCount int
	LayerNames []string

	Components []PlacedComponent
	Tracks     []Track
	Vias       []Via
	Pours      []CopperPour
}

// NewBoard creates a board with canonical layer names (F.Cu, In1.Cu, B.Cu).
func NewBoard(widthMM, heightMM float64, layerCount int) (*Board, error) {
	if widthMM <= 0 || heightMM <= 0 {
		return nil, errors.New("layout : dimensions de carte invalides")
	}
	if layerCount < 1 || layerCount > 34 {
		return nil, errors.New("layout : nombre de couches invalide")
	}
	names := make([]string, layerCount)
	for i := range names {
		names[i] = layerName(i, layerCount)
	}
	return &Board{
		WidthMM:    widthMM,
		HeightMM:   heightMM,
		LayerCount: layerCount,
		LayerNames: names,
		Components: []PlacedComponent{},
		Tracks:     []Track{},
		Vias:       []Via{},
		Pours:      []CopperPour{},
	}, nil
}

func layerName(i, n int) string {
	switch {
	case i == 0:
		return "F.Cu"
	case i == n-1 && n > 1:
		return "B.Cu"
	default:
		return fmt.Sprintf("In%d.Cu", i)
	}
}

// AddComponent inserts a placed component, rejecting duplicates.
func (b *Board) AddComponent(c PlacedComponent) error {
	c.Ref = strings.TrimSpace(c.Ref)
	if c.Ref == "" {
		return errors.New("layout : référence de composant vide")
	}
	if _, ok := b.ComponentByRef(c.Ref); ok {
		return fmt.Errorf("%w : %s", ErrDuplicateComponentRef, c.Ref)
	}
	if c.Footprint.HeightMM <= 0 {
		c.Footprint.HeightMM = DefaultBoardHeightMM
	}
	b.Components = append(b.Components, c)
	return nil
}

// ComponentByRef looks a placed component up by reference designator.
func (b *Board) ComponentByRef(ref string) (*PlacedComponent, bool) {
	for i := range b.Components {
		if b.Components[i].Ref == ref {
			return &b.Components[i], true
		}
	}
	return nil, false
}

// RemoveComponent deletes a placed component. Returns true when found.
func (b *Board) RemoveComponent(ref string) bool {
	for i := range b.Components {
		if b.Components[i].Ref == ref {
			b.Components = append(b.Components[:i], b.Components[i+1:]...)
			return true
		}
	}
	return false
}

// AddTrack appends a copper polyline.
func (b *Board) AddTrack(t Track) {
	b.Tracks = append(b.Tracks, t)
}

// AddVia appends a via after validation.
func (b *Board) AddVia(v Via) error {
	if err := v.Validate(); err != nil {
		return err
	}
	b.Vias = append(b.Vias, v)
	return nil
}

// TracksForNet returns every track belonging to the given net.
func (b *Board) TracksForNet(net string) []Track {
	var out []Track
	for _, t := range b.Tracks {
		if t.Net == net {
			out = append(out, t)
		}
	}
	return out
}

// ViasForNet returns every via belonging to the given net.
func (b *Board) ViasForNet(net string) []Via {
	var out []Via
	for _, v := range b.Vias {
		if v.Net == net {
			out = append(out, v)
		}
	}
	return out
}

// RemoveRoutesForNet deletes every track and via belonging to the net.
func (b *Board) RemoveRoutesForNet(net string) {
	tracks := b.Tracks[:0]
	for _, t := range b.Tracks {
		if t.Net != net {
			tracks = append(tracks, t)
		}
	}
	b.Tracks = tracks

	vias := b.Vias[:0]
	for _, v := range b.Vias {
		if v.Net != net {
			vias = append(vias, v)
		}
	}
	b.Vias = vias
}

// ReplaceRoutesForNet atomically replaces the routing of a single net.
func (b *Board) ReplaceRoutesForNet(net string, tracks []Track, vias []Via) {
	b.RemoveRoutesForNet(net)
	b.Tracks = append(b.Tracks, tracks...)
	b.Vias = append(b.Vias, vias...)
}

// TotalTrackLength sums the length of every track (mm).
func (b *Board) TotalTrackLength() float64 {
	var total float64
	for i := range b.Tracks {
		total += b.Tracks[i].Length()
	}
	return total
}

// ViaCount returns the number of vias on the board.
func (b *Board) ViaCount() int { return len(b.Vias) }

// BBox returns the bounding box of the board content (components, tracks,
// vias). When the board is empty, the board outline itself is returned.
func (b *Board) BBox() geometry.AABB {
	outline := geometry.AABB{MinX: 0, MinY: 0, MaxX: b.WidthMM, MaxY: b.HeightMM}
	var box geometry.AABB
	found := false

	for i := range b.Components {
		bb := b.Components[i].BBox()
		if !found {
			box, found = bb, true
			continue
		}
		box = box.Union(bb)
	}
	for i := range b.Tracks {
		bb := b.Tracks[i].BBox()
		if !found {
			box, found = bb, true
			continue
		}
		box = box.Union(bb)
	}
	for _, v := range b.Vias {
		r := v.Diameter / 2
		vb := geometry.AABB{MinX: v.X - r, MinY: v.Y - r, MaxX: v.X + r, MaxY: v.Y + r}
		if !found {
			box, found = vb, true
			continue
		}
		box = box.Union(vb)
	}
	if !found {
		return outline
	}
	return box
}
