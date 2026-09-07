// magic_handler.go exposes the Magic Pack endpoints: the natural-language
// copilot, the thermal ghost, the eye oracle and the routing arena. The
// handlers follow the same conventions as the contract handlers (uniform
// errors, JSON DTOs) but are additive and optional: a nil service answers
// 503 so older deployments keep working.
package rest

import (
	"net/http"
	"time"

	arenaapp "github.com/assihervey-coder/IA-PCB/backend/internal/application/arena"
	layoutapp "github.com/assihervey-coder/IA-PCB/backend/internal/application/layout"
	magicapp "github.com/assihervey-coder/IA-PCB/backend/internal/application/magic"
	verificationapp "github.com/assihervey-coder/IA-PCB/backend/internal/application/verification"
)

// errNoService is reused from layout_handler.go.

// --------------------------------------------------------------
// Magic Copilot
// --------------------------------------------------------------

// handleMagic answers POST /api/v1/projects/{id}/magic.
func (d *Deps) handleMagic(w http.ResponseWriter, r *http.Request) {
	if d.Magic == nil {
		writeError(w, r, http.StatusServiceUnavailable, "internal", errNoService)
		return
	}
	var req MagicRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Utterance == "" {
		writeError(w, r, http.StatusBadRequest, "invalid", "utterance vide : dites quelque chose au copilot")
		return
	}

	if req.Apply {
		rep, err := d.Magic.Apply(r.Context(), r.PathValue("id"), req.Utterance)
		if err != nil {
			mapServiceError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, magicReportToDTO(rep, "apply"))
		return
	}
	rep, err := d.Magic.Interpret(r.Context(), r.PathValue("id"), req.Utterance)
	if err != nil {
		mapServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, magicReportToDTO(rep, "interpret"))
}

// magicReportToDTO maps the copilot report onto the REST payload.
func magicReportToDTO(rep *magicapp.ApplyReport, mode string) MagicResult {
	out := MagicResult{
		Applied:      rep.Applied,
		Skipped:      rep.Skipped,
		BoardChanged: rep.BoardChanged,
		RulesChanged: rep.RulesChanged,
		Mode:         mode,
	}
	if rep.Interpretation != nil {
		i := rep.Interpretation
		actions := make([]MagicActionDTO, 0, len(i.Actions))
		for _, a := range i.Actions {
			actions = append(actions, MagicActionDTO{
				Kind:       string(a.Kind),
				Params:     a.Params,
				Summary:    a.Summary,
				Executable: a.Executable,
			})
		}
		out.Interpretation = MagicInterpretationDTO{
			Utterance:  i.Utterance,
			Language:   i.Language,
			Actions:    actions,
			Confidence: i.Confidence,
			Reply:      i.Reply,
		}
	}
	return out
}

// --------------------------------------------------------------
// Thermal Ghost
// --------------------------------------------------------------

// handleThermal answers POST /api/v1/projects/{id}/thermal.
func (d *Deps) handleThermal(w http.ResponseWriter, r *http.Request) {
	if d.Thermal == nil {
		writeError(w, r, http.StatusServiceUnavailable, "internal", errNoService)
		return
	}
	// Corps optionnel : {"ambient_c": 40, "sources": [...]} pour du
	// what-if; corps vide/valeurs nulles = composants du projet.
	var req ThermalRequest
	_ = decodeJSON(w, r, &req) // corps vide autorisé

	res, err := d.Thermal.RunWithSources(r.Context(), r.PathValue("id"),
		verificationapp.ThermalOverride{
			AmbientC: req.AmbientC,
			Sources:  thermalSourcesFromDTO(req.Sources),
		})
	if err != nil {
		mapServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, thermalToDTO(res))
}

func thermalSourcesFromDTO(in []ThermalSourceDTO) []verificationapp.ThermalSource {
	if len(in) == 0 {
		return nil
	}
	out := make([]verificationapp.ThermalSource, 0, len(in))
	for _, s := range in {
		out = append(out, verificationapp.ThermalSource{
			Ref: s.Ref, PowerW: s.PowerW, X: s.X, Y: s.Y,
			WidthMM: s.WidthMM, HeightMM: s.HeightMM,
		})
	}
	return out
}

func thermalToDTO(res *verificationapp.ThermalResult) ThermalResultDTO {
	hot := make([]ThermalHotDTO, 0, len(res.Hotspots))
	for _, h := range res.Hotspots {
		hot = append(hot, ThermalHotDTO{
			Rank: h.Rank, X: h.X, Y: h.Y, TempC: h.TempC,
			AboveAmbientC: h.AboveAmbientC, LikelyRef: h.LikelyRef,
		})
	}
	return ThermalResultDTO{
		GridW: res.GridW, GridH: res.GridH, CellMM: res.CellMM,
		AmbientC: res.AmbientC, MaxTempC: res.MaxTempC, MeanTempC: res.MeanTempC,
		MinTempC: res.MinTempC, MaxGradient: res.MaxGradient,
		Hotspots: hot, Grid: res.Grid, Warnings: res.Warnings,
	}
}

// --------------------------------------------------------------
// Eye Oracle (SI)
// --------------------------------------------------------------

// handleSI answers POST /api/v1/projects/{id}/si.
func (d *Deps) handleSI(w http.ResponseWriter, r *http.Request) {
	if d.SI == nil {
		writeError(w, r, http.StatusServiceUnavailable, "internal", errNoService)
		return
	}
	res, err := d.SI.Run(r.Context(), r.PathValue("id"))
	if err != nil {
		mapServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, siToDTO(res))
}

func siToDTO(res *verificationapp.SIResult) SIResultDTO {
	nets := make([]SINetDTO, 0, len(res.Nets))
	for _, n := range res.Nets {
		nets = append(nets, SINetDTO{
			Net: n.Net, LengthMM: n.LengthMM, ViaCount: n.ViaCount,
			WidthMM: n.WidthMM, Z0Ohms: n.Z0Ohms, DelayPS: n.DelayPS,
			ReflectionPS: n.ReflectionPS, EyeHeightPct: n.EyeHeightPct,
			EyeWidthPS: n.EyeWidthPS, JitterPS: n.JitterPS,
			Criticality: string(n.Criticality), Advices: n.Advices,
		})
	}
	return SIResultDTO{
		DriverPS: res.DriverPS, BitPeriod: res.BitPeriod, Analyzed: res.Analyzed,
		OkCount: res.OkCount, WarnCount: res.WarnCount, CritCount: res.CritCount,
		Nets: nets, MaxEyeNet: res.MaxEyeNet, ScorePct: res.ScorePct, Summary: res.Summary,
	}
}

// --------------------------------------------------------------
// AI Arena
// --------------------------------------------------------------

// handleArena answers POST /api/v1/projects/{id}/arena.
func (d *Deps) handleArena(w http.ResponseWriter, r *http.Request) {
	if d.Arena == nil {
		writeError(w, r, http.StatusServiceUnavailable, "internal", errNoService)
		return
	}
	projectID := r.PathValue("id")
	rep, err := d.Arena.Fight(r.Context(), projectID, func(net string, pct float64, msg string) {
		if d.Hub != nil {
			d.Hub.Publish("arena-"+projectID, projectID, layoutapp.RouteProgress{
				JobID: "arena-" + projectID, Stage: "arena",
				CurrentNet: net, Percent: pct, Message: msg,
			})
		}
	})
	if err != nil {
		mapServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, arenaToDTO(rep))
}

// handleArenaLeaderboard answers GET /api/v1/arena/leaderboard.
func (d *Deps) handleArenaLeaderboard(w http.ResponseWriter, r *http.Request) {
	if d.Arena == nil {
		writeError(w, r, http.StatusServiceUnavailable, "internal", errNoService)
		return
	}
	standings := d.Arena.Leaderboard()
	out := make([]ArenaStandingDTO, 0, len(standings))
	for _, st := range standings {
		out = append(out, ArenaStandingDTO{
			Strategy: st.Strategy, Rating: st.Rating, Matches: st.Matches,
			Wins: st.Wins, Losses: st.Losses, Draws: st.Draws,
		})
	}
	writeJSON(w, http.StatusOK, ArenaLeaderboardDTO{Standings: out})
}

func arenaToDTO(rep *arenaapp.MatchReport) ArenaReportDTO {
	return ArenaReportDTO{
		ProjectID: rep.ProjectID,
		Nets:      rep.Nets,
		Greedy:    arenaCardToDTO(rep.Greedy),
		Astar:     arenaCardToDTO(rep.Astar),
		Winner:    rep.Winner,
		Margin:    rep.Margin,
		Elo: [2]ArenaStandingDTO{
			arenaStandingToDTO(rep.Elo[0]), arenaStandingToDTO(rep.Elo[1]),
		},
		Log: rep.Log,
		At:  rep.At.UTC().Format(time.RFC3339),
	}
}

func arenaCardToDTO(c arenaapp.FighterResult) ArenaCardDTO {
	return ArenaCardDTO{
		Strategy: c.Strategy, Completed: c.Completed, Failed: c.Failed,
		TotalLengthMM: c.TotalLengthMM, ViaCount: c.ViaCount,
		Collisions: c.Collisions, DurationMS: c.DurationMS,
		Score: c.Score, NetLog: c.NetLog,
	}
}

func arenaStandingToDTO(s arenaapp.Standing) ArenaStandingDTO {
	return ArenaStandingDTO{
		Strategy: s.Strategy, Rating: s.Rating, Matches: s.Matches,
		Wins: s.Wins, Losses: s.Losses, Draws: s.Draws,
	}
}
