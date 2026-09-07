// Package rest exposes the HTTP API of the backend (base /api/v1 plus the
// /healthz probe and the WebSocket endpoint). The DTOs mirror 1:1 the
// shared/types/pcb.d.ts definitions with snake_case JSON tags
// (contracts.md §2).
package rest

import (
	"time"

	layoutapp "github.com/kidcad/kidcad-pro-ia/backend/internal/application/layout"
	domainlayout "github.com/kidcad/kidcad-pro-ia/backend/internal/domain/layout"
	domainproject "github.com/kidcad/kidcad-pro-ia/backend/internal/domain/project"
	domainschematic "github.com/kidcad/kidcad-pro-ia/backend/internal/domain/schematic"
	"github.com/kidcad/kidcad-pro-ia/backend/pkg/pcb-format"
)

// --------------------------------------------------------------
// DTO REST (snake_case, reflète shared/types/pcb.d.ts)
// --------------------------------------------------------------

// Project is the metadata view of a project.
type Project struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	LayerCount  int    `json:"layer_count"`
	Status      string `json:"status"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

// ProjectCreate is the payload of POST /api/v1/projects.
type ProjectCreate struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	LayerCount  int    `json:"layer_count"` // défaut 2
}

// ProjectUpdate is the payload of PUT /api/v1/projects/{id}.
type ProjectUpdate struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
}

// ProjectList is the response of GET /api/v1/projects.
type ProjectList struct {
	Projects []Project `json:"projects"`
	Total    int       `json:"total"`
}

// BoardInfo is the outline section of LayoutData.
type BoardInfo struct {
	WidthMM    float64  `json:"width_mm"`
	HeightMM   float64  `json:"height_mm"`
	LayerCount int      `json:"layer_count"`
	LayerNames []string `json:"layer_names"`
}

// LayoutComponent is one placed component of LayoutData.
type LayoutComponent struct {
	Ref       string  `json:"ref"`
	Value     string  `json:"value,omitempty"`
	Footprint string  `json:"footprint"`
	X         float64 `json:"x"`
	Y         float64 `json:"y"`
	Rotation  float64 `json:"rotation"`
	Fixed     bool    `json:"fixed"`
}

// LayoutNet is one logical net with its pad count.
type LayoutNet struct {
	Name     string `json:"name"`
	NetClass string `json:"net_class"`
	PadCount int    `json:"pad_count"`
}

// LayoutPoint is one 2D coordinate.
type LayoutPoint struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// LayoutTrack is one copper polyline.
type LayoutTrack struct {
	Net    string        `json:"net"`
	Layer  int           `json:"layer"`
	Width  float64       `json:"width"`
	Points []LayoutPoint `json:"points"`
}

// LayoutVia is one vertical connection.
type LayoutVia struct {
	Net       string  `json:"net"`
	X         float64 `json:"x"`
	Y         float64 `json:"y"`
	FromLayer int     `json:"from_layer"`
	ToLayer   int     `json:"to_layer"`
	Diameter  float64 `json:"diameter"`
	Drill     float64 `json:"drill"`
}

// LayoutData is the complete physical view of a project
// (GET/PUT /api/v1/projects/{id}/layout).
type LayoutData struct {
	Board      BoardInfo         `json:"board"`
	Components []LayoutComponent `json:"components"`
	Nets       []LayoutNet       `json:"nets"`
	Tracks     []LayoutTrack     `json:"tracks"`
	Vias       []LayoutVia       `json:"vias"`
}

// RouteJobStart is the payload of POST .../route.
type RouteJobStart struct {
	Strategy string   `json:"strategy"`
	Nets     []string `json:"nets"`
}

// PlaceJobStart is the payload of POST .../place.
type PlaceJobStart struct {
	Strategy string `json:"strategy"`
}

// JobStarted is the 202 response of the asynchronous IA endpoints.
type JobStarted struct {
	JobID     string `json:"job_id"`
	ProjectID string `json:"project_id"`
}

// JobStatus is the observable state of an asynchronous job; the
// application-layer JobInfo already carries the contract JSON tags.
type JobStatus = layoutapp.JobInfo

// DRCViolation is one design-rule violation.
type DRCViolation struct {
	Code     string  `json:"code"`
	Severity string  `json:"severity"`
	Message  string  `json:"message"`
	X        float64 `json:"x"`
	Y        float64 `json:"y"`
	Layer    int     `json:"layer"`
	Net      string  `json:"net"`
}

// DRCResult is the response of POST .../drc.
type DRCResult struct {
	Passed       bool           `json:"passed"`
	CheckedRules int            `json:"checked_rules"`
	Violations   []DRCViolation `json:"violations"`
	DurationMS   int64          `json:"duration_ms"`
}

// ERCViolation is one electrical-rule violation.
type ERCViolation struct {
	Code         string `json:"code"`
	Severity     string `json:"severity"`
	Message      string `json:"message"`
	ComponentRef string `json:"component_ref"`
}

// ERCResult is the response of POST .../erc.
type ERCResult struct {
	Passed     bool           `json:"passed"`
	Violations []ERCViolation `json:"violations"`
}

// ImportResult is the response of POST .../import.
type ImportResult struct {
	Format     string   `json:"format"`
	Components int      `json:"components"`
	Nets       int      `json:"nets"`
	Tracks     int      `json:"tracks"`
	Vias       int      `json:"vias"`
	Warnings   []string `json:"warnings"`
}

// ExportFile describes one produced file.
type ExportFile struct {
	Name      string `json:"name"`
	SizeBytes int64  `json:"size_bytes"`
}

// errorBody is the inner object of ErrorResponse.
type errorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// ErrorResponse is the uniform error payload (contracts.md §2).
type ErrorResponse struct {
	Error errorBody `json:"error"`
}

// --------------------------------------------------------------
// Mappers domaine -> DTO
// --------------------------------------------------------------

// rfc3339 formats a domain timestamp for the REST payloads.
func rfc3339(t time.Time) string { return t.UTC().Format(time.RFC3339) }

// projectToDTO maps the aggregate metadata onto the REST Project.
func projectToDTO(p *domainproject.Project) Project {
	return Project{
		ID:          p.ID(),
		Name:        p.Name(),
		Description: p.Description(),
		LayerCount:  p.LayerCount(),
		Status:      string(p.Status()),
		CreatedAt:   rfc3339(p.CreatedAt()),
		UpdatedAt:   rfc3339(p.UpdatedAt()),
	}
}

// boardToLayoutData maps the board (and the schematic for the nets) onto
// the REST LayoutData payload.
func boardToLayoutData(b *domainlayout.Board, sch *domainschematic.Schematic) LayoutData {
	out := LayoutData{
		Components: []LayoutComponent{},
		Nets:       []LayoutNet{},
		Tracks:     []LayoutTrack{},
		Vias:       []LayoutVia{},
	}
	if b == nil {
		return out
	}

	out.Board = BoardInfo{
		WidthMM:    b.WidthMM,
		HeightMM:   b.HeightMM,
		LayerCount: b.LayerCount,
		LayerNames: append([]string(nil), b.LayerNames...),
	}

	for i := range b.Components {
		c := &b.Components[i]
		out.Components = append(out.Components, LayoutComponent{
			Ref:       c.Ref,
			Value:     c.Footprint.Value,
			Footprint: c.Footprint.Name,
			X:         c.X,
			Y:         c.Y,
			Rotation:  c.Rotation,
			Fixed:     c.Fixed,
		})
	}

	if sch != nil && len(sch.Nets) > 0 {
		for _, n := range sch.Nets {
			out.Nets = append(out.Nets, LayoutNet{
				Name:     n.Name,
				NetClass: string(n.Class),
				PadCount: len(n.Connections),
			})
		}
	} else {
		out.Nets = netsFromBoard(b)
	}

	for _, t := range b.Tracks {
		track := LayoutTrack{Net: t.Net, Layer: t.Layer, Width: t.Width, Points: []LayoutPoint{}}
		for _, p := range t.Points {
			track.Points = append(track.Points, LayoutPoint{X: p.X, Y: p.Y})
		}
		out.Tracks = append(out.Tracks, track)
	}
	for _, v := range b.Vias {
		out.Vias = append(out.Vias, LayoutVia{
			Net: v.Net, X: v.X, Y: v.Y,
			FromLayer: v.FromLayer, ToLayer: v.ToLayer,
			Diameter: v.Diameter, Drill: v.Drill,
		})
	}
	return out
}

// netsFromBoard derives the net list with pad counts from the placed pads
// (fallback when no schematic is imported).
func netsFromBoard(b *domainlayout.Board) []LayoutNet {
	index := map[string]*LayoutNet{}
	var order []string
	for i := range b.Components {
		c := &b.Components[i]
		for _, pad := range c.Footprint.Pads {
			if pad.Net == "" {
				continue
			}
			n, ok := index[pad.Net]
			if !ok {
				n = &LayoutNet{Name: pad.Net, NetClass: "default"}
				index[pad.Net] = n
				order = append(order, pad.Net)
			}
			n.PadCount++
		}
	}
	out := make([]LayoutNet, 0, len(order))
	for _, name := range order {
		out = append(out, *index[name])
	}
	return out
}

// --------------------------------------------------------------
// Mapper DTO -> domaine (PUT layout)
// --------------------------------------------------------------

// layoutDataToLayoutDTO converts the REST payload into the interchange
// LayoutDTO (the pcbformat.ToBoard factory then rebuilds the aggregate).
func layoutDataToLayoutDTO(ld *LayoutData) *pcbformat.LayoutDTO {
	dto := &pcbformat.LayoutDTO{
		Board: pcbformat.BoardDTO{
			WidthMM:    ld.Board.WidthMM,
			HeightMM:   ld.Board.HeightMM,
			LayerCount: ld.Board.LayerCount,
			LayerNames: ld.Board.LayerNames,
		},
		Footprints: map[string]pcbformat.FootprintDTO{},
		Components: make([]pcbformat.ComponentDTO, 0, len(ld.Components)),
		Tracks:     make([]pcbformat.TrackDTO, 0, len(ld.Tracks)),
		Vias:       make([]pcbformat.ViaDTO, 0, len(ld.Vias)),
	}
	for _, c := range ld.Components {
		dto.Components = append(dto.Components, pcbformat.ComponentDTO{
			Ref: c.Ref, Value: c.Value, Footprint: c.Footprint,
			X: c.X, Y: c.Y, Rotation: c.Rotation, Fixed: c.Fixed,
		})
	}
	for _, t := range ld.Tracks {
		td := pcbformat.TrackDTO{Net: t.Net, Layer: t.Layer, Width: t.Width,
			Points: make([]pcbformat.PointDTO, 0, len(t.Points))}
		for _, p := range t.Points {
			td.Points = append(td.Points, pcbformat.PointDTO{X: p.X, Y: p.Y})
		}
		dto.Tracks = append(dto.Tracks, td)
	}
	for _, v := range ld.Vias {
		dto.Vias = append(dto.Vias, pcbformat.ViaDTO{
			Net: v.Net, X: v.X, Y: v.Y,
			FromLayer: v.FromLayer, ToLayer: v.ToLayer,
			Diameter: v.Diameter, Drill: v.Drill,
		})
	}
	return dto
}

// footprintToDTO maps a domain footprint onto its interchange DTO.
func footprintToDTO(fp *domainlayout.Footprint) pcbformat.FootprintDTO {
	fd := pcbformat.FootprintDTO{
		Name:         fp.Name,
		Value:        fp.Value,
		BodyWidthMM:  fp.BodyWidthMM,
		BodyHeightMM: fp.BodyHeightMM,
		HeightMM:     fp.HeightMM,
		Pads:         make([]pcbformat.PadDTO, 0, len(fp.Pads)),
	}
	for _, p := range fp.Pads {
		fd.Pads = append(fd.Pads, pcbformat.PadDTO{
			Name: p.Name, Shape: string(p.Shape),
			X: p.X, Y: p.Y, Width: p.Width, Height: p.Height,
			Rotation: p.Rotation, Layer: p.Layer, Net: p.Net,
		})
	}
	return fd
}
