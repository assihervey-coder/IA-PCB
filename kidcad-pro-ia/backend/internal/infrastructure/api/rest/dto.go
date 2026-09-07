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

// --------------------------------------------------------------
// DTO Magic Pack (additif, hors contrat figé contracts.md §2)
// --------------------------------------------------------------

// MagicRequest is the payload of POST .../magic.
type MagicRequest struct {
        Utterance string `json:"utterance"`
        Apply     bool   `json:"apply"` // false = simple interprétation
}

// MagicActionDTO mirrors magic.Action (snake_case for the frontend).
type MagicActionDTO struct {
        Kind       string         `json:"kind"`
        Params     map[string]any `json:"params"`
        Summary    string         `json:"summary"`
        Executable bool           `json:"executable"`
}

// MagicInterpretationDTO mirrors magic.Interpretation.
type MagicInterpretationDTO struct {
        Utterance  string           `json:"utterance"`
        Language   string           `json:"language"`
        Actions    []MagicActionDTO `json:"actions"`
        Confidence float64          `json:"confidence"`
        Reply      string           `json:"reply"`
}

// MagicResult is the response of POST .../magic.
type MagicResult struct {
        Interpretation MagicInterpretationDTO `json:"interpretation"`
        Applied        []string               `json:"applied"`
        Skipped        []string               `json:"skipped"`
        BoardChanged   bool                   `json:"board_changed"`
        RulesChanged   bool                   `json:"rules_changed"`
        Mode           string                 `json:"mode"` // "interpret" | "apply"
}

// ThermalSourceDTO is one heat source of the thermal payload.
type ThermalSourceDTO struct {
        Ref      string  `json:"ref"`
        PowerW   float64 `json:"power_w"`
        X        float64 `json:"x"`
        Y        float64 `json:"y"`
        WidthMM  float64 `json:"width_mm"`
        HeightMM float64 `json:"height_mm"`
}

// ThermalRequest allows a custom ambient temperature and explicit sources
// (simulation "what-if"); when Sources is empty the project components and
// their guessed dissipation are used.
type ThermalRequest struct {
        AmbientC float64            `json:"ambient_c"`
        Sources  []ThermalSourceDTO `json:"sources"`
}

// ThermalResultDTO mirrors verification.ThermalResult (grid rows kept
// compact: rounded to 0.1 °C).
type ThermalResultDTO struct {
        GridW       int             `json:"grid_w"`
        GridH       int             `json:"grid_h"`
        CellMM      float64         `json:"cell_mm"`
        AmbientC    float64         `json:"ambient_c"`
        MaxTempC    float64         `json:"max_temp_c"`
        MeanTempC   float64         `json:"mean_temp_c"`
        MinTempC    float64         `json:"min_temp_c"`
        MaxGradient float64         `json:"max_gradient_c_per_mm"`
        Hotspots    []ThermalHotDTO `json:"hotspots"`
        Grid        []float64       `json:"grid"`
        Warnings    []string        `json:"warnings"`
}

// ThermalHotDTO is one hotspot of the thermal payload.
type ThermalHotDTO struct {
        Rank          int     `json:"rank"`
        X             float64 `json:"x"`
        Y             float64 `json:"y"`
        TempC         float64 `json:"temp_c"`
        AboveAmbientC float64 `json:"above_ambient_c"`
        LikelyRef     string  `json:"likely_ref"`
}

// SIResultDTO mirrors verification.SIResult.
type SIResultDTO struct {
        DriverPS  float64    `json:"driver_rise_time_ps"`
        BitPeriod float64    `json:"bit_period_ps"`
        Analyzed  int        `json:"analyzed"`
        OkCount   int        `json:"ok_count"`
        WarnCount int        `json:"warning_count"`
        CritCount int        `json:"critical_count"`
        Nets      []SINetDTO `json:"nets"`
        MaxEyeNet string     `json:"worst_eye_net"`
        ScorePct  float64    `json:"si_score_pct"`
        Summary   string     `json:"summary"`
}

// SINetDTO mirrors verification.SINetReport.
type SINetDTO struct {
        Net          string   `json:"net"`
        LengthMM     float64  `json:"length_mm"`
        ViaCount     int      `json:"via_count"`
        WidthMM      float64  `json:"width_mm"`
        Z0Ohms       float64  `json:"z0_ohms"`
        DelayPS      float64  `json:"delay_ps"`
        ReflectionPS float64  `json:"reflection_budget_ps"`
        EyeHeightPct float64  `json:"eye_height_pct"`
        EyeWidthPS   float64  `json:"eye_width_ps"`
        JitterPS     float64  `json:"jitter_ps"`
        Criticality  string   `json:"criticality"`
        Advices      []string `json:"advices"`
}

// ArenaReportDTO mirrors arena.MatchReport.
type ArenaReportDTO struct {
        ProjectID string              `json:"project_id"`
        Nets      []string            `json:"nets"`
        Greedy    ArenaCardDTO        `json:"greedy"`
        Astar     ArenaCardDTO        `json:"astar"`
        Winner    string              `json:"winner"`
        Margin    float64             `json:"margin"`
        Elo       [2]ArenaStandingDTO `json:"elo"`
        Log       []string            `json:"log"`
        At        string              `json:"at"`
}

// ArenaCardDTO mirrors arena.FighterResult.
type ArenaCardDTO struct {
        Strategy      string   `json:"strategy"`
        Completed     int      `json:"completed"`
        Failed        int      `json:"failed"`
        TotalLengthMM float64  `json:"total_length_mm"`
        ViaCount      int      `json:"via_count"`
        Collisions    int      `json:"collisions"`
        DurationMS    int64    `json:"duration_ms"`
        Score         float64  `json:"score"`
        NetLog        []string `json:"net_log"`
}

// ArenaStandingDTO mirrors arena.Standing.
type ArenaStandingDTO struct {
        Strategy string  `json:"strategy"`
        Rating   float64 `json:"rating"`
        Matches  int     `json:"matches"`
        Wins     int     `json:"wins"`
        Losses   int     `json:"losses"`
        Draws    int     `json:"draws"`
}

// ArenaLeaderboardDTO is the response of GET /api/v1/arena/leaderboard.
type ArenaLeaderboardDTO struct {
        Standings []ArenaStandingDTO `json:"standings"`
}

// --------------------------------------------------------------
// DTO Pack WOW (additif, hors contrat figé contracts.md §2)
// --------------------------------------------------------------
//  - DRC Auto-Healer   : POST .../drc/autofix
//  - Design Doctor     : GET  .../doctor
//  - Oracle DFM        : POST .../dfm
//  - Time Machine      : POST/GET .../snapshots[...]
//  - Stats live        : GET  .../stats

// AutoFixRequest is the payload of POST .../drc/autofix.
type AutoFixRequest struct {
        DryRun bool `json:"dry_run"` // true = propositions sans application
}

// AutoFixDTO mirrors verification.DRCFix.
type AutoFixDTO struct {
        Code       string  `json:"code"`
        Action     string  `json:"action"`
        Message    string  `json:"message"`
        Confidence float64 `json:"confidence"`
        Applied    bool    `json:"applied"`
        Target     string  `json:"target"`
}

// AutoFixResultDTO mirrors verification.AutoFixResult.
type AutoFixResultDTO struct {
        DryRun           bool        `json:"dry_run"`
        PassedBefore     bool        `json:"passed_before"`
        PassedAfter      bool        `json:"passed_after"`
        ViolationsBefore int         `json:"violations_before"`
        ViolationsAfter  int         `json:"violations_after"`
        Fixed            int         `json:"fixed"`
        Remaining        []string    `json:"remaining"`
        Fixes            []AutoFixDTO `json:"fixes"`
        BoardChanged     bool        `json:"board_changed"`
        DurationMS       int64       `json:"duration_ms"`
}

// DoctorAxisDTO mirrors doctor.AxisScore.
type DoctorAxisDTO struct {
        Axe   string  `json:"axe"`
        Score float64 `json:"score"`
        Max   float64 `json:"max"`
}

// DoctorPrescriptionDTO mirrors doctor.Prescription.
type DoctorPrescriptionDTO struct {
        Priority int     `json:"priority"`
        Axe      string  `json:"axe"`
        Title    string  `json:"title"`
        Detail   string  `json:"detail"`
        GainPts  float64 `json:"gain_pts"`
}

// DoctorMetricsDTO mirrors doctor.Metrics.
type DoctorMetricsDTO struct {
        Components     int     `json:"components"`
        Nets           int     `json:"nets"`
        UnroutedNets   int     `json:"unrouted_nets"`
        Tracks         int     `json:"tracks"`
        TotalLengthMM  float64 `json:"total_length_mm"`
        Vias           int     `json:"vias"`
        UtilizationPct float64 `json:"utilization_pct"`
        MaxTempC       float64 `json:"max_temp_c"`
        SIScorePct     float64 `json:"si_score_pct"`
        DRCViolations  int     `json:"drc_violations"`
        ERCViolations  int     `json:"erc_violations"`
        MinTrackMM     float64 `json:"min_track_mm"`
        MinDrillMM     float64 `json:"min_drill_mm"`
}

// DoctorReportDTO mirrors doctor.Report.
type DoctorReportDTO struct {
        Score         float64                 `json:"score"`
        Grade         string                  `json:"grade"`
        Verdict       string                  `json:"verdict"`
        Axes          []DoctorAxisDTO         `json:"axes"`
        Prescriptions []DoctorPrescriptionDTO `json:"prescriptions"`
        Metrics       DoctorMetricsDTO        `json:"metrics"`
        DurationMS    int64                   `json:"duration_ms"`
}

// DFMUnitPriceDTO mirrors dfm.UnitPrice.
type DFMUnitPriceDTO struct {
        Qty      int     `json:"qty"`
        Label    string  `json:"label"`
        UnitEUR  float64 `json:"unit_eur"`
        TotalEUR float64 `json:"total_eur"`
}

// DFMRiskFlagDTO mirrors dfm.RiskFlag.
type DFMRiskFlagDTO struct {
        Code    string  `json:"code"`
        Message string  `json:"message"`
        Impact  float64 `json:"impact_pct"`
}

// DFMSurchargeDTO mirrors dfm.Surcharge.
type DFMSurchargeDTO struct {
        Label string  `json:"label"`
        Pct   float64 `json:"pct"`
}

// DFMEstimateDTO mirrors dfm.Estimate.
type DFMEstimateDTO struct {
        Currency   string            `json:"currency"`
        AreaDM2    float64           `json:"area_dm2"`
        Layers     int               `json:"layers"`
        ViaCount   int               `json:"via_count"`
        ViaDensity float64           `json:"via_density_per_cm2"`
        MinTrackMM float64           `json:"min_track_mm"`
        MinDrillMM float64           `json:"min_drill_mm"`
        BOMLines   int               `json:"bom_lines"`
        Components int               `json:"components"`
        UnitPrices []DFMUnitPriceDTO `json:"unit_prices"`
        YieldPct   float64           `json:"first_pass_yield_pct"`
        DefectRisk string            `json:"defect_risk"`
        RiskFlags  []DFMRiskFlagDTO  `json:"risk_flags"`
        Surcharges []DFMSurchargeDTO `json:"surcharges"`
        Advice     []string          `json:"advice"`
        DurationMS int64             `json:"duration_ms"`
}

// SnapshotMetaDTO mirrors timemachine.SnapshotMeta.
type SnapshotMetaDTO struct {
        ID        string           `json:"id"`
        Label     string           `json:"label"`
        At        string           `json:"at"`
        Board     SnapshotBoardDTO `json:"board"`
        RuleCount int              `json:"rule_count"`
}

// SnapshotBoardDTO mirrors timemachine.BoardMeta.
type SnapshotBoardDTO struct {
        WidthMM    float64 `json:"width_mm"`
        HeightMM   float64 `json:"height_mm"`
        Components int     `json:"components"`
        Tracks     int     `json:"tracks"`
        Vias       int     `json:"vias"`
        LengthMM   float64 `json:"length_mm"`
        MinTrackMM float64 `json:"min_track_mm"`
}

// SnapshotListDTO is the response of GET .../snapshots.
type SnapshotListDTO struct {
        Snapshots []SnapshotMetaDTO `json:"snapshots"`
}

// SnapshotCaptureRequest is the payload of POST .../snapshots.
type SnapshotCaptureRequest struct {
        Label string `json:"label"`
}

// DiffEntryDTO mirrors timemachine.DiffEntry.
type DiffEntryDTO struct {
        Kind   string `json:"kind"`
        Change string `json:"change"`
        Target string `json:"target"`
        Detail string `json:"detail"`
}

// DiffDTO mirrors timemachine.Diff.
type DiffDTO struct {
        SnapshotID  string        `json:"snapshot_id"`
        SnapshotLbl string        `json:"snapshot_label"`
        Entries     []DiffEntryDTO `json:"entries"`
        Summary     string        `json:"summary"`
        Changed     bool          `json:"changed"`
}

// StatsBoardDTO mirrors stats.BoardStats.
type StatsBoardDTO struct {
        WidthMM    float64 `json:"width_mm"`
        HeightMM   float64 `json:"height_mm"`
        AreaCM2    float64 `json:"area_cm2"`
        LayerCount int     `json:"layer_count"`
}

// StatsLayerUsageDTO mirrors stats.LayerUsage.
type StatsLayerUsageDTO struct {
        Layer    int     `json:"layer"`
        Name     string  `json:"name"`
        Tracks   int     `json:"tracks"`
        LengthMM float64 `json:"length_mm"`
        ViasUsed int     `json:"vias_touching"`
}

// StatsNetDTO mirrors stats.NetStat.
type StatsNetDTO struct {
        Name     string  `json:"name"`
        Class    string  `json:"class"`
        Tracks   int     `json:"tracks"`
        LengthMM float64 `json:"length_mm"`
        Vias     int     `json:"vias"`
        Routed   bool    `json:"routed"`
        PadCount int     `json:"pad_count"`
}

// StatsNetClassDTO mirrors stats.NetClassStat.
type StatsNetClassDTO struct {
        Class    string  `json:"class"`
        Nets     int     `json:"nets"`
        LengthMM float64 `json:"length_mm"`
}

// StatsReportDTO mirrors stats.Report.
type StatsReportDTO struct {
        Board          StatsBoardDTO      `json:"board"`
        Components     int                `json:"components"`
        Pads           int                `json:"pads"`
        Nets           int                `json:"nets"`
        TrackedNets    int                `json:"tracked_nets"`
        Tracks         int                `json:"tracks"`
        Vias           int                `json:"vias"`
        TotalLengthMM  float64            `json:"total_length_mm"`
        UtilizationPct float64            `json:"utilization_pct"`
        LayerUsage     []StatsLayerUsageDTO `json:"layer_usage"`
        TopNets        []StatsNetDTO      `json:"top_nets"`
        NetClasses     []StatsNetClassDTO `json:"net_classes"`
        DurationMS     int64              `json:"duration_ms"`
}
