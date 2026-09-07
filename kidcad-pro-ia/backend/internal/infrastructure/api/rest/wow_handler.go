// Handlers REST du Pack WOW (additif, hors contrat figé contracts.md §2) :
// DRC Auto-Healer, Design Doctor, Oracle DFM, Time Machine et Stats live.
package rest

import (
        "net/http"
        "time"

        timemachineapp "github.com/kidcad/kidcad-pro-ia/backend/internal/application/timemachine"
)

// --------------------------------------------------------------
// DRC Auto-Healer
// --------------------------------------------------------------

// handleAutoFix answers POST /api/v1/projects/{id}/drc/autofix.
func (d *Deps) handleAutoFix(w http.ResponseWriter, r *http.Request) {
        if d.AutoFix == nil {
                writeError(w, r, http.StatusNotImplemented, "unavailable", "auto-healer indisponible")
                return
        }
        projectID := r.PathValue("id")

        var req AutoFixRequest
        _ = decodeJSON(w, r, &req) // corps optionnel : {} = application réelle

        res, err := d.AutoFix.Run(r.Context(), projectID, req.DryRun)
        if mapServiceError(w, r, err) {
                return
        }

        dto := AutoFixResultDTO{
                DryRun:           res.DryRun,
                PassedBefore:     res.PassedBefore,
                PassedAfter:      res.PassedAfter,
                ViolationsBefore: res.ViolationsBefore,
                ViolationsAfter:  res.ViolationsAfter,
                Fixed:            res.Fixed,
                Remaining:        res.Remaining,
                BoardChanged:     res.BoardChanged,
                DurationMS:       res.DurationMS,
        }
        dto.Fixes = make([]AutoFixDTO, 0, len(res.Fixes))
        for _, f := range res.Fixes {
                dto.Fixes = append(dto.Fixes, AutoFixDTO{
                        Code: f.Code, Action: f.Action, Message: f.Message,
                        Confidence: f.Confidence, Applied: f.Applied, Target: f.Target,
                })
        }
        writeJSON(w, http.StatusOK, dto)
}

// --------------------------------------------------------------
// Design Doctor
// --------------------------------------------------------------

// handleDoctor answers GET /api/v1/projects/{id}/doctor.
func (d *Deps) handleDoctor(w http.ResponseWriter, r *http.Request) {
        if d.Doctor == nil {
                writeError(w, r, http.StatusNotImplemented, "unavailable", "design doctor indisponible")
                return
        }
        projectID := r.PathValue("id")

        rep, err := d.Doctor.Examine(r.Context(), projectID)
        if mapServiceError(w, r, err) {
                return
        }

        dto := DoctorReportDTO{
                Score:      rep.Score,
                Grade:      rep.Grade,
                Verdict:    rep.Verdict,
                DurationMS: rep.DurationMS,
        }
        dto.Axes = make([]DoctorAxisDTO, 0, len(rep.Axes))
        for _, a := range rep.Axes {
                dto.Axes = append(dto.Axes, DoctorAxisDTO{Axe: a.Axe, Score: a.Score, Max: a.Max})
        }
        dto.Prescriptions = make([]DoctorPrescriptionDTO, 0, len(rep.Prescriptions))
        for _, p := range rep.Prescriptions {
                dto.Prescriptions = append(dto.Prescriptions, DoctorPrescriptionDTO{
                        Priority: p.Priority, Axe: p.Axe, Title: p.Title,
                        Detail: p.Detail, GainPts: p.GainPts,
                })
        }
        dto.Metrics = DoctorMetricsDTO{
                Components:     rep.Metrics.Components,
                Nets:           rep.Metrics.Nets,
                UnroutedNets:   rep.Metrics.UnroutedNets,
                Tracks:         rep.Metrics.Tracks,
                TotalLengthMM:  rep.Metrics.TotalLengthMM,
                Vias:           rep.Metrics.Vias,
                UtilizationPct: rep.Metrics.UtilizationPct,
                MaxTempC:       rep.Metrics.MaxTempC,
                SIScorePct:     rep.Metrics.SIScorePct,
                DRCViolations:  rep.Metrics.DRCViolations,
                ERCViolations:  rep.Metrics.ERCViolations,
                MinTrackMM:     rep.Metrics.MinTrackMM,
                MinDrillMM:     rep.Metrics.MinDrillMM,
        }
        writeJSON(w, http.StatusOK, dto)
}

// --------------------------------------------------------------
// Oracle DFM
// --------------------------------------------------------------

// handleDFM answers POST /api/v1/projects/{id}/dfm.
func (d *Deps) handleDFM(w http.ResponseWriter, r *http.Request) {
        if d.DFM == nil {
                writeError(w, r, http.StatusNotImplemented, "unavailable", "oracle DFM indisponible")
                return
        }
        projectID := r.PathValue("id")

        est, err := d.DFM.Estimate(r.Context(), projectID)
        if mapServiceError(w, r, err) {
                return
        }

        dto := DFMEstimateDTO{
                Currency:   est.Currency,
                AreaDM2:    est.AreaDM2,
                Layers:     est.Layers,
                ViaCount:   est.ViaCount,
                ViaDensity: est.ViaDensity,
                MinTrackMM: est.MinTrackMM,
                MinDrillMM: est.MinDrillMM,
                BOMLines:   est.BOMLines,
                Components: est.Components,
                YieldPct:   est.YieldPct,
                DefectRisk: est.DefectRisk,
                Advice:     est.Advice,
                DurationMS: est.DurationMS,
        }
        dto.UnitPrices = make([]DFMUnitPriceDTO, 0, len(est.UnitPrices))
        for _, u := range est.UnitPrices {
                dto.UnitPrices = append(dto.UnitPrices, DFMUnitPriceDTO{
                        Qty: u.Qty, Label: u.Label, UnitEUR: u.UnitEUR, TotalEUR: u.TotalEUR})
        }
        dto.RiskFlags = make([]DFMRiskFlagDTO, 0, len(est.RiskFlags))
        for _, rf := range est.RiskFlags {
                dto.RiskFlags = append(dto.RiskFlags, DFMRiskFlagDTO{
                        Code: rf.Code, Message: rf.Message, Impact: rf.Impact})
        }
        dto.Surcharges = make([]DFMSurchargeDTO, 0, len(est.Surcharges))
        for _, sc := range est.Surcharges {
                dto.Surcharges = append(dto.Surcharges, DFMSurchargeDTO{Label: sc.Label, Pct: sc.Pct})
        }
        writeJSON(w, http.StatusOK, dto)
}

// --------------------------------------------------------------
// Time Machine
// --------------------------------------------------------------

// handleSnapshotCapture answers POST /api/v1/projects/{id}/snapshots.
func (d *Deps) handleSnapshotCapture(w http.ResponseWriter, r *http.Request) {
        if d.TimeMachine == nil {
                writeError(w, r, http.StatusNotImplemented, "unavailable", "time machine indisponible")
                return
        }
        projectID := r.PathValue("id")

        var req SnapshotCaptureRequest
        _ = decodeJSON(w, r, &req) // corps optionnel : label par défaut

        meta, err := d.TimeMachine.Capture(r.Context(), projectID, req.Label)
        if mapServiceError(w, r, err) {
                return
        }
        writeJSON(w, http.StatusCreated, snapshotMetaToDTO(*meta))
}

// handleSnapshotList answers GET /api/v1/projects/{id}/snapshots.
func (d *Deps) handleSnapshotList(w http.ResponseWriter, r *http.Request) {
        if d.TimeMachine == nil {
                writeError(w, r, http.StatusNotImplemented, "unavailable", "time machine indisponible")
                return
        }
        projectID := r.PathValue("id")

        metas := d.TimeMachine.List(projectID)
        out := SnapshotListDTO{Snapshots: make([]SnapshotMetaDTO, 0, len(metas))}
        for i := range metas {
                out.Snapshots = append(out.Snapshots, snapshotMetaToDTO(metas[i]))
        }
        writeJSON(w, http.StatusOK, out)
}

// handleSnapshotDiff answers GET /api/v1/projects/{id}/snapshots/{sid}/diff.
func (d *Deps) handleSnapshotDiff(w http.ResponseWriter, r *http.Request) {
        if d.TimeMachine == nil {
                writeError(w, r, http.StatusNotImplemented, "unavailable", "time machine indisponible")
                return
        }
        projectID := r.PathValue("id")
        snapshotID := r.PathValue("sid")

        diff, err := d.TimeMachine.Diff(r.Context(), projectID, snapshotID)
        if mapServiceError(w, r, err) {
                return
        }

        dto := DiffDTO{
                SnapshotID:  diff.SnapshotID,
                SnapshotLbl: diff.SnapshotLbl,
                Summary:     diff.Summary,
                Changed:     diff.Changed,
                Entries:     make([]DiffEntryDTO, 0, len(diff.Entries)),
        }
        for _, e := range diff.Entries {
                dto.Entries = append(dto.Entries, DiffEntryDTO{
                        Kind: e.Kind, Change: e.Change, Target: e.Target, Detail: e.Detail})
        }
        writeJSON(w, http.StatusOK, dto)
}

// handleSnapshotRestore answers POST /api/v1/projects/{id}/snapshots/{sid}/restore.
func (d *Deps) handleSnapshotRestore(w http.ResponseWriter, r *http.Request) {
        if d.TimeMachine == nil {
                writeError(w, r, http.StatusNotImplemented, "unavailable", "time machine indisponible")
                return
        }
        projectID := r.PathValue("id")
        snapshotID := r.PathValue("sid")

        meta, err := d.TimeMachine.Restore(r.Context(), projectID, snapshotID)
        if mapServiceError(w, r, err) {
                return
        }
        writeJSON(w, http.StatusOK, snapshotMetaToDTO(*meta))
}

func snapshotMetaToDTO(m timemachineapp.SnapshotMeta) SnapshotMetaDTO {
        return SnapshotMetaDTO{
                ID:    m.ID,
                Label: m.Label,
                At:    m.At.Format(time.RFC3339),
                Board: SnapshotBoardDTO{
                        WidthMM:    m.Board.WidthMM,
                        HeightMM:   m.Board.HeightMM,
                        Components: m.Board.Components,
                        Tracks:     m.Board.Tracks,
                        Vias:       m.Board.Vias,
                        LengthMM:   m.Board.LengthMM,
                        MinTrackMM: m.Board.MinTrackMM,
                },
                RuleCount: m.RuleCount,
        }
}

// --------------------------------------------------------------
// Stats live
// --------------------------------------------------------------

// handleStats answers GET /api/v1/projects/{id}/stats.
func (d *Deps) handleStats(w http.ResponseWriter, r *http.Request) {
        if d.Stats == nil {
                writeError(w, r, http.StatusNotImplemented, "unavailable", "statistiques indisponibles")
                return
        }
        projectID := r.PathValue("id")

        rep, err := d.Stats.Report(r.Context(), projectID)
        if mapServiceError(w, r, err) {
                return
        }

        dto := StatsReportDTO{
                Board: StatsBoardDTO{
                        WidthMM:    rep.Board.WidthMM,
                        HeightMM:   rep.Board.HeightMM,
                        AreaCM2:    rep.Board.AreaCM2,
                        LayerCount: rep.Board.LayerCount,
                },
                Components:     rep.Components,
                Pads:           rep.Pads,
                Nets:           rep.Nets,
                TrackedNets:    rep.TrackedNets,
                Tracks:         rep.Tracks,
                Vias:           rep.Vias,
                TotalLengthMM:  rep.TotalLengthMM,
                UtilizationPct: rep.UtilizationPct,
                DurationMS:     rep.DurationMS,
        }
        dto.LayerUsage = make([]StatsLayerUsageDTO, 0, len(rep.LayerUsage))
        for _, l := range rep.LayerUsage {
                dto.LayerUsage = append(dto.LayerUsage, StatsLayerUsageDTO{
                        Layer: l.Layer, Name: l.Name, Tracks: l.Tracks, LengthMM: l.LengthMM, ViasUsed: l.ViasUsed})
        }
        dto.TopNets = make([]StatsNetDTO, 0, len(rep.TopNets))
        for _, n := range rep.TopNets {
                dto.TopNets = append(dto.TopNets, StatsNetDTO{
                        Name: n.Name, Class: n.Class, Tracks: n.Tracks,
                        LengthMM: n.LengthMM, Vias: n.Vias, Routed: n.Routed, PadCount: n.PadCount})
        }
        dto.NetClasses = make([]StatsNetClassDTO, 0, len(rep.NetClasses))
        for _, c := range rep.NetClasses {
                dto.NetClasses = append(dto.NetClasses, StatsNetClassDTO{
                        Class: c.Class, Nets: c.Nets, LengthMM: c.LengthMM})
        }
        writeJSON(w, http.StatusOK, dto)
}
