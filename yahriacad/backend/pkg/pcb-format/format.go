// Package pcbformat defines the YahriaCad interchange format (.yahriacad.json),
// the neutral serialisation used for imports, exports and SQL persistence.
// It is the only exported (public) package of the backend and deliberately
// depends on the domain layer to offer domain <-> DTO conversion helpers.
package pcbformat

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/assihervey-coder/IA-PCB/backend/internal/domain/layout"
	"github.com/assihervey-coder/IA-PCB/backend/internal/domain/schematic"
	"github.com/assihervey-coder/IA-PCB/backend/internal/pkg/geometry"
)

const (
	FormatName = "yahriacad-interchange"
	Version    = "1.0"
	Extension  = ".yahriacad.json"
)

// ErrUnsupportedVersion is returned when the file version differs from
// Version and cannot be migrated.
var ErrUnsupportedVersion = errors.New("pcbformat : version de format non supportée")

// Meta carries provenance information.
type Meta struct {
	Generator string `json:"generator,omitempty"`
	CreatedAt string `json:"created_at,omitempty"`
	Source    string `json:"source,omitempty"`
}

// NewMeta builds a Meta with the current UTC timestamp.
func NewMeta(generator, source string) Meta {
	return Meta{
		Generator: generator,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
		Source:    source,
	}
}

// PointDTO is a 2D coordinate in millimeters.
type PointDTO struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// PinDTO is a symbol pin of the schematic view.
type PinDTO struct {
	Number string  `json:"number"`
	Name   string  `json:"name,omitempty"`
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
}

// ComponentDTO is used both by the schematic view (symbol instance) and the
// layout view (placed component referencing a footprint by name).
type ComponentDTO struct {
	Ref       string   `json:"ref"`
	Value     string   `json:"value,omitempty"`
	Footprint string   `json:"footprint"`
	X         float64  `json:"x"`
	Y         float64  `json:"y"`
	Rotation  float64  `json:"rotation,omitempty"`
	Fixed     bool     `json:"fixed,omitempty"`
	Pins      []PinDTO `json:"pins,omitempty"`
}

// ConnectionDTO is a (component, pin) membership of a net.
type ConnectionDTO struct {
	ComponentRef string `json:"component_ref"`
	PinNumber    string `json:"pin_number"`
}

// NetDTO is a logical net with its class and connections.
type NetDTO struct {
	Name        string          `json:"name"`
	NetClass    string          `json:"net_class,omitempty"`
	Connections []ConnectionDTO `json:"connections,omitempty"`
}

// SchematicDTO is the electrical view of the interchange file.
type SchematicDTO struct {
	Name       string         `json:"name,omitempty"`
	Components []ComponentDTO `json:"components"`
	Nets       []NetDTO       `json:"nets,omitempty"`
}

// PadDTO is a copper landing area (coordinates relative to the footprint).
type PadDTO struct {
	Name     string  `json:"name"`
	Shape    string  `json:"shape,omitempty"`
	X        float64 `json:"x"`
	Y        float64 `json:"y"`
	Width    float64 `json:"width"`
	Height   float64 `json:"height"`
	Rotation float64 `json:"rotation,omitempty"`
	Layer    int     `json:"layer"`
	Net      string  `json:"net,omitempty"`
}

// FootprintDTO is a physical package definition.
type FootprintDTO struct {
	Name         string   `json:"name"`
	Value        string   `json:"value,omitempty"`
	Pads         []PadDTO `json:"pads,omitempty"`
	BodyWidthMM  float64  `json:"body_width_mm,omitempty"`
	BodyHeightMM float64  `json:"body_height_mm,omitempty"`
	HeightMM     float64  `json:"height_mm,omitempty"`
}

// TrackDTO is a copper polyline on a single layer.
type TrackDTO struct {
	Net    string     `json:"net"`
	Layer  int        `json:"layer"`
	Width  float64    `json:"width"`
	Points []PointDTO `json:"points"`
}

// ViaDTO is a vertical connection between two layers.
type ViaDTO struct {
	Net       string  `json:"net,omitempty"`
	X         float64 `json:"x"`
	Y         float64 `json:"y"`
	FromLayer int     `json:"from_layer"`
	ToLayer   int     `json:"to_layer"`
	Diameter  float64 `json:"diameter"`
	Drill     float64 `json:"drill"`
}

// PourDTO is a filled copper region (ground plane / pour).
type PourDTO struct {
	ID          string     `json:"id"`
	Net         string     `json:"net"`
	Name        string     `json:"name,omitempty"`
	Layer       int        `json:"layer"`
	Outline     []PointDTO `json:"outline"`
	ClearanceMM float64    `json:"clearance_mm"`
	HatchMM     float64    `json:"hatch_mm,omitempty"`
	IsGround    bool       `json:"is_ground,omitempty"`
	FillPct     float64    `json:"fill_pct,omitempty"`
	AreaMM2     float64    `json:"area_mm2,omitempty"`
	Stitched    int        `json:"stitched,omitempty"`
}

// BoardDTO is the physical outline of the board.
type BoardDTO struct {
	WidthMM    float64  `json:"width_mm"`
	HeightMM   float64  `json:"height_mm"`
	LayerCount int      `json:"layer_count"`
	LayerNames []string `json:"layer_names,omitempty"`
}

// LayoutDTO is the physical view of the interchange file.
type LayoutDTO struct {
	Board      BoardDTO                `json:"board"`
	Footprints map[string]FootprintDTO `json:"footprints,omitempty"`
	Components []ComponentDTO          `json:"components"`
	Tracks     []TrackDTO              `json:"tracks,omitempty"`
	Vias       []ViaDTO                `json:"vias,omitempty"`
	Pours      []PourDTO               `json:"pours,omitempty"`
	Nets       []NetDTO                `json:"nets,omitempty"`
}

// File is the root interchange document.
type File struct {
	Format    string        `json:"format"`
	Version   string        `json:"version"`
	Meta      Meta          `json:"meta,omitempty"`
	Schematic *SchematicDTO `json:"schematic,omitempty"`
	Layout    *LayoutDTO    `json:"layout,omitempty"`
}

// Encode serialises the file as pretty JSON, filling default format/version.
func Encode(f *File) ([]byte, error) {
	if f.Format == "" {
		f.Format = FormatName
	}
	if f.Version == "" {
		f.Version = Version
	}
	return json.MarshalIndent(f, "", "  ")
}

// Decode parses and validates an interchange document.
func Decode(data []byte) (*File, error) {
	var f File
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("pcbformat : JSON invalide : %w", err)
	}
	if f.Format != "" && f.Format != FormatName {
		return nil, fmt.Errorf("pcbformat : format inconnu %q", f.Format)
	}
	if f.Version != "" && f.Version != Version {
		return nil, ErrUnsupportedVersion
	}
	return &f, nil
}

// --------------------------------------------------------------
// Conversions domaine <-> DTO
// --------------------------------------------------------------

// FromSchematic maps a domain schematic onto its DTO.
func FromSchematic(s *schematic.Schematic) *SchematicDTO {
	if s == nil {
		return nil
	}
	dto := &SchematicDTO{
		Name:       s.Name,
		Components: make([]ComponentDTO, 0, len(s.Components)),
		Nets:       make([]NetDTO, 0, len(s.Nets)),
	}
	for _, c := range s.Components {
		cd := ComponentDTO{
			Ref:       c.Ref,
			Value:     c.Value,
			Footprint: c.Footprint,
			X:         c.X,
			Y:         c.Y,
			Rotation:  c.Rotation,
			Fixed:     c.Fixed,
			Pins:      make([]PinDTO, 0, len(c.Pins)),
		}
		for _, p := range c.Pins {
			cd.Pins = append(cd.Pins, PinDTO{Number: p.Number, Name: p.Name, X: p.X, Y: p.Y})
		}
		dto.Components = append(dto.Components, cd)
	}
	for _, n := range s.Nets {
		nd := NetDTO{Name: n.Name, NetClass: string(n.Class)}
		for _, c := range n.Connections {
			nd.Connections = append(nd.Connections,
				ConnectionDTO{ComponentRef: c.ComponentRef, PinNumber: c.PinNumber})
		}
		dto.Nets = append(dto.Nets, nd)
	}
	return dto
}

// ToSchematic rebuilds a domain schematic from its DTO.
func ToSchematic(dto *SchematicDTO) (*schematic.Schematic, error) {
	if dto == nil {
		return schematic.New("import"), nil
	}
	s := schematic.New(dto.Name)
	for _, c := range dto.Components {
		comp := schematic.Component{
			Ref:       c.Ref,
			Value:     c.Value,
			Footprint: c.Footprint,
			X:         c.X,
			Y:         c.Y,
			Rotation:  c.Rotation,
			Fixed:     c.Fixed,
			Pins:      make([]schematic.Pin, 0, len(c.Pins)),
		}
		for _, p := range c.Pins {
			comp.Pins = append(comp.Pins, schematic.Pin{Number: p.Number, Name: p.Name, X: p.X, Y: p.Y})
		}
		if err := s.AddComponent(comp); err != nil {
			return nil, err
		}
	}
	for _, n := range dto.Nets {
		net := schematic.Net{Name: n.Name, Class: schematic.NetClass(n.NetClass)}
		for _, c := range n.Connections {
			net.Connections = append(net.Connections,
				schematic.PinRef{ComponentRef: c.ComponentRef, PinNumber: c.PinNumber})
		}
		if err := s.AddNet(net); err != nil {
			return nil, err
		}
	}
	return s, nil
}

// FromBoard maps a domain board onto its DTO (footprints are inlined in the
// Footprints map keyed by footprint name).
func FromBoard(b *layout.Board) *LayoutDTO {
	if b == nil {
		return nil
	}
	dto := &LayoutDTO{
		Board: BoardDTO{
			WidthMM:    b.WidthMM,
			HeightMM:   b.HeightMM,
			LayerCount: b.LayerCount,
			LayerNames: b.LayerNames,
		},
		Footprints: map[string]FootprintDTO{},
		Components: make([]ComponentDTO, 0, len(b.Components)),
		Tracks:     make([]TrackDTO, 0, len(b.Tracks)),
		Vias:       make([]ViaDTO, 0, len(b.Vias)),
	}

	for i := range b.Components {
		c := &b.Components[i]
		dto.Components = append(dto.Components, ComponentDTO{
			Ref:       c.Ref,
			Value:     c.Footprint.Value,
			Footprint: c.Footprint.Name,
			X:         c.X,
			Y:         c.Y,
			Rotation:  c.Rotation,
			Fixed:     c.Fixed,
		})
		if _, ok := dto.Footprints[c.Footprint.Name]; ok {
			continue
		}
		fd := FootprintDTO{
			Name:         c.Footprint.Name,
			Value:        c.Footprint.Value,
			BodyWidthMM:  c.Footprint.BodyWidthMM,
			BodyHeightMM: c.Footprint.BodyHeightMM,
			HeightMM:     c.Footprint.HeightMM,
			Pads:         make([]PadDTO, 0, len(c.Footprint.Pads)),
		}
		for _, p := range c.Footprint.Pads {
			fd.Pads = append(fd.Pads, PadDTO{
				Name: p.Name, Shape: string(p.Shape),
				X: p.X, Y: p.Y, Width: p.Width, Height: p.Height,
				Rotation: p.Rotation, Layer: p.Layer, Net: p.Net,
			})
		}
		dto.Footprints[c.Footprint.Name] = fd
	}

	for _, t := range b.Tracks {
		td := TrackDTO{Net: t.Net, Layer: t.Layer, Width: t.Width,
			Points: make([]PointDTO, 0, len(t.Points))}
		for _, p := range t.Points {
			td.Points = append(td.Points, PointDTO{X: p.X, Y: p.Y})
		}
		dto.Tracks = append(dto.Tracks, td)
	}
	for _, v := range b.Vias {
		dto.Vias = append(dto.Vias, ViaDTO{
			Net: v.Net, X: v.X, Y: v.Y,
			FromLayer: v.FromLayer, ToLayer: v.ToLayer,
			Diameter: v.Diameter, Drill: v.Drill,
		})
	}
	dto.Pours = make([]PourDTO, 0, len(b.Pours))
	for i := range b.Pours {
		p := &b.Pours[i]
		pd := PourDTO{
			ID: p.ID, Net: p.Net, Name: p.Name, Layer: p.Layer,
			ClearanceMM: p.ClearanceMM, HatchMM: p.HatchMM,
			IsGround: p.IsGround, FillPct: p.FillPct,
			AreaMM2: p.AreaMM2, Stitched: p.Stitched,
			Outline: make([]PointDTO, 0, len(p.Outline)),
		}
		for _, pt := range p.Outline {
			pd.Outline = append(pd.Outline, PointDTO{X: pt.X, Y: pt.Y})
		}
		dto.Pours = append(dto.Pours, pd)
	}
	return dto
}

// ToBoard rebuilds a domain board from its DTO. Components referencing an
// unknown footprint get a minimal placeholder footprint (documented,
// non-fatal behaviour).
func ToBoard(dto *LayoutDTO) (*layout.Board, error) {
	if dto == nil {
		return nil, errors.New("pcbformat : layout absent")
	}
	b, err := layout.NewBoard(dto.Board.WidthMM, dto.Board.HeightMM, dto.Board.LayerCount)
	if err != nil {
		return nil, err
	}
	if len(dto.Board.LayerNames) == b.LayerCount {
		b.LayerNames = append([]string(nil), dto.Board.LayerNames...)
	}

	for _, c := range dto.Components {
		fd, ok := dto.Footprints[c.Footprint]
		if !ok {
			fd = FootprintDTO{
				Name: c.Footprint, BodyWidthMM: 2, BodyHeightMM: 2, HeightMM: layout.DefaultBoardHeightMM,
			}
		}
		fp := layout.Footprint{
			Name:         fd.Name,
			Value:        fd.Value,
			BodyWidthMM:  fd.BodyWidthMM,
			BodyHeightMM: fd.BodyHeightMM,
			HeightMM:     fd.HeightMM,
			Pads:         make([]layout.Pad, 0, len(fd.Pads)),
		}
		for _, p := range fd.Pads {
			fp.Pads = append(fp.Pads, layout.Pad{
				Name: p.Name, Shape: layout.PadShape(p.Shape),
				X: p.X, Y: p.Y, Width: p.Width, Height: p.Height,
				Rotation: p.Rotation, Layer: p.Layer, Net: p.Net,
			})
		}
		if err := b.AddComponent(layout.PlacedComponent{
			Ref: c.Ref, Footprint: fp, X: c.X, Y: c.Y, Rotation: c.Rotation, Fixed: c.Fixed,
		}); err != nil {
			return nil, err
		}
	}

	for _, t := range dto.Tracks {
		tr := layout.Track{Net: t.Net, Layer: t.Layer, Width: t.Width,
			Points: make([]layout.TrackPoint, 0, len(t.Points))}
		for _, p := range t.Points {
			tr.Points = append(tr.Points, layout.TrackPoint{X: p.X, Y: p.Y})
		}
		b.AddTrack(tr)
	}
	for _, v := range dto.Vias {
		if err := b.AddVia(layout.Via{
			Net: v.Net, X: v.X, Y: v.Y,
			FromLayer: v.FromLayer, ToLayer: v.ToLayer,
			Diameter: v.Diameter, Drill: v.Drill,
		}); err != nil {
			return nil, err
		}
	}
	for _, p := range dto.Pours {
		pour := layout.CopperPour{
			ID: p.ID, Net: p.Net, Name: p.Name, Layer: p.Layer,
			ClearanceMM: p.ClearanceMM, HatchMM: p.HatchMM,
			IsGround: p.IsGround, FillPct: p.FillPct,
			AreaMM2: p.AreaMM2, Stitched: p.Stitched,
			Outline: make([]geometry.Point, 0, len(p.Outline)),
		}
		for _, pt := range p.Outline {
			pour.Outline = append(pour.Outline, geometry.Point{X: pt.X, Y: pt.Y})
		}
		if _, err := b.AddPour(pour); err != nil {
			return nil, err
		}
	}
	return b, nil
}
