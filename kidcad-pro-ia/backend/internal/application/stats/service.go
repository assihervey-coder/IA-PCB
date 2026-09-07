// Package stats computes the live design dashboard of a project: counts,
// copper length, board utilization, per-layer and per-net-class breakdowns,
// top nets by length. Pure read-only aggregation over the aggregate — no
// heavy geometry, safe to poll on every layout refresh.
package stats

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"time"

	apperrors "github.com/kidcad/kidcad-pro-ia/backend/internal/application"
	domainproject "github.com/kidcad/kidcad-pro-ia/backend/internal/domain/project"
	domainschematic "github.com/kidcad/kidcad-pro-ia/backend/internal/domain/schematic"
)

// Report is the response of GET .../stats.
type Report struct {
	Board          BoardStats     `json:"board"`
	Components     int            `json:"components"`
	Pads           int            `json:"pads"`
	Nets           int            `json:"nets"`
	TrackedNets    int            `json:"tracked_nets"`
	Tracks         int            `json:"tracks"`
	Vias           int            `json:"vias"`
	TotalLengthMM  float64        `json:"total_length_mm"`
	UtilizationPct float64        `json:"utilization_pct"`
	LayerUsage     []LayerUsage   `json:"layer_usage"`
	TopNets        []NetStat      `json:"top_nets"` // 5 plus longs nets
	NetClasses     []NetClassStat `json:"net_classes"`
	DurationMS     int64          `json:"duration_ms"`
}

// BoardStats is the outline part of the report.
type BoardStats struct {
	WidthMM    float64 `json:"width_mm"`
	HeightMM   float64 `json:"height_mm"`
	AreaCM2    float64 `json:"area_cm2"`
	LayerCount int     `json:"layer_count"`
}

// LayerUsage aggregates the copper of one layer.
type LayerUsage struct {
	Layer    int     `json:"layer"`
	Name     string  `json:"name"`
	Tracks   int     `json:"tracks"`
	LengthMM float64 `json:"length_mm"`
	ViasUsed int     `json:"vias_touching"`
}

// NetStat is one net's copper footprint.
type NetStat struct {
	Name     string  `json:"name"`
	Class    string  `json:"class"`
	Tracks   int     `json:"tracks"`
	LengthMM float64 `json:"length_mm"`
	Vias     int     `json:"vias"`
	Routed   bool    `json:"routed"`
	PadCount int     `json:"pad_count"`
}

// NetClassStat aggregates nets per class.
type NetClassStat struct {
	Class    string  `json:"class"`
	Nets     int     `json:"nets"`
	LengthMM float64 `json:"length_mm"`
}

// Service is the stats use case.
type Service struct {
	projects domainproject.Repository
	log      *slog.Logger
}

// NewService builds the stats use case.
func NewService(projects domainproject.Repository, log *slog.Logger) *Service {
	if log == nil {
		log = slog.Default()
	}
	return &Service{projects: projects, log: log}
}

// Report computes the dashboard of the current project state.
func (s *Service) Report(ctx context.Context, projectID string) (*Report, error) {
	start := time.Now()

	p, err := s.projects.FindByID(ctx, projectID)
	if err != nil {
		if errors.Is(err, apperrors.ErrNotFound) {
			return nil, fmt.Errorf("stats : projet %q introuvable : %w", projectID, apperrors.ErrNotFound)
		}
		return nil, fmt.Errorf("stats : chargement du projet : %w", err)
	}
	b := p.Board()
	if b == nil {
		return nil, fmt.Errorf("stats : le projet %q n'a pas de carte (importez ou placez d'abord)", projectID)
	}

	rep := &Report{
		Board: BoardStats{
			WidthMM:    b.WidthMM,
			HeightMM:   b.HeightMM,
			AreaCM2:    round2(b.WidthMM * b.HeightMM / 100),
			LayerCount: b.LayerCount,
		},
		Components: len(b.Components),
		Tracks:     len(b.Tracks),
		Vias:       len(b.Vias),
	}

	// Pads + référentiel de classes.
	for i := range b.Components {
		rep.Pads += len(b.Components[i].Footprint.Pads)
	}

	// Nets : depuis la schéma quand il existe, sinon depuis les pads.
	netClass := map[string]string{}
	netPads := map[string]int{}
	if sch := p.Schematic(); sch != nil && len(sch.Nets) > 0 {
		rep.Nets = len(sch.Nets)
		for _, n := range sch.Nets {
			netClass[n.Name] = string(n.Class)
			netPads[n.Name] = len(n.Connections)
		}
	} else {
		seen := map[string]struct{}{}
		for i := range b.Components {
			for _, pad := range b.Components[i].Footprint.Pads {
				if pad.Net == "" {
					continue
				}
				if _, ok := seen[pad.Net]; !ok {
					seen[pad.Net] = struct{}{}
					netClass[pad.Net] = string(domainschematic.ClassDefault)
				}
				netPads[pad.Net]++
			}
		}
		rep.Nets = len(seen)
	}

	// Cuivre par net et par couche.
	netStats := map[string]*NetStat{}
	getNet := func(name string) *NetStat {
		if st, ok := netStats[name]; ok {
			return st
		}
		cls := netClass[name]
		if cls == "" {
			cls = string(domainschematic.ClassDefault)
		}
		st := &NetStat{Name: name, Class: cls, PadCount: netPads[name]}
		netStats[name] = st
		return st
	}

	layerIndex := map[int]*LayerUsage{}
	layerName := func(idx int) string {
		if idx >= 0 && idx < len(b.LayerNames) {
			return b.LayerNames[idx]
		}
		return fmt.Sprintf("Layer %d", idx)
	}

	for i := range b.Tracks {
		t := &b.Tracks[i]
		l := t.Length()
		rep.TotalLengthMM += l

		st := getNet(t.Net)
		st.Tracks++
		st.LengthMM += l
		st.Routed = true

		lu, ok := layerIndex[t.Layer]
		if !ok {
			lu = &LayerUsage{Layer: t.Layer, Name: layerName(t.Layer)}
			layerIndex[t.Layer] = lu
		}
		lu.Tracks++
		lu.LengthMM += l
	}
	rep.TotalLengthMM = round1(rep.TotalLengthMM)
	rep.TrackedNets = len(netStats)

	for i := range b.Vias {
		v := &b.Vias[i]
		if st, ok := netStats[v.Net]; ok {
			st.Vias++
		}
		for layer := range layerIndex {
			if v.OnLayer(layer) {
				layerIndex[layer].ViasUsed++
			}
		}
	}

	// Utilisation : corps de composants / surface de l'outline.
	if area := b.WidthMM * b.HeightMM; area > 0 {
		var used float64
		for i := range b.Components {
			fp := b.Components[i].Footprint
			used += fp.BodyWidthMM * fp.BodyHeightMM
		}
		rep.UtilizationPct = round1(math.Min(100, used/area*100))
	}

	// Top 5 des nets les plus longs.
	rep.TopNets = topNets(netStats, 5)

	// Agrégat par classe.
	classes := map[string]*NetClassStat{}
	for _, st := range netStats {
		cs, ok := classes[st.Class]
		if !ok {
			cs = &NetClassStat{Class: st.Class}
			classes[st.Class] = cs
		}
		cs.Nets++
		cs.LengthMM += st.LengthMM
	}
	for _, cs := range classes {
		cs.LengthMM = round1(cs.LengthMM)
		rep.NetClasses = append(rep.NetClasses, *cs)
	}
	sort.Slice(rep.NetClasses, func(i, j int) bool {
		return rep.NetClasses[i].LengthMM > rep.NetClasses[j].LengthMM
	})

	// Couches triées par index.
	for idx := 0; idx < b.LayerCount; idx++ {
		if lu, ok := layerIndex[idx]; ok {
			lu.LengthMM = round1(lu.LengthMM)
			rep.LayerUsage = append(rep.LayerUsage, *lu)
		}
	}

	rep.DurationMS = time.Since(start).Milliseconds()
	return rep, nil
}

// topNets returns the n longest routed nets (plus unrouted ones kept out).
func topNets(netStats map[string]*NetStat, n int) []NetStat {
	all := make([]NetStat, 0, len(netStats))
	for _, st := range netStats {
		all = append(all, *st)
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].LengthMM != all[j].LengthMM {
			return all[i].LengthMM > all[j].LengthMM
		}
		return all[i].Name < all[j].Name
	})
	for i := range all {
		all[i].LengthMM = round1(all[i].LengthMM)
	}
	if len(all) > n {
		all = all[:n]
	}
	return all
}

func round1(v float64) float64 { return math.Round(v*10) / 10 }
func round2(v float64) float64 { return math.Round(v*100) / 100 }
