package rest

import (
	"errors"
	"net/http"
	"strings"

	verificationapp "github.com/kidcad/kidcad-pro-ia/backend/internal/application/verification"
	domainlayout "github.com/kidcad/kidcad-pro-ia/backend/internal/domain/layout"
	"github.com/kidcad/kidcad-pro-ia/backend/pkg/pcb-format"
)

// service guard messages.
const (
	errNoService = "service indisponible"
)

// handleGetLayout answers GET /api/v1/projects/{id}/layout.
func (d *Deps) handleGetLayout(w http.ResponseWriter, r *http.Request) {
	p, err := d.Projects.FindByID(r.Context(), r.PathValue("id"))
	if err != nil {
		mapServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, boardToLayoutData(p.Board(), p.Schematic()))
}

// handlePutLayout answers PUT /api/v1/projects/{id}/layout. The payload
// carries positions / tracks / vias but not the pad definitions, so the
// footprints of the previously stored board are preserved by reference
// designator.
func (d *Deps) handlePutLayout(w http.ResponseWriter, r *http.Request) {
	p, err := d.Projects.FindByID(r.Context(), r.PathValue("id"))
	if err != nil {
		mapServiceError(w, r, err)
		return
	}

	var body LayoutData
	if !decodeJSON(w, r, &body) {
		return
	}

	board, err := layoutDataToBoard(&body, p.Board(), p.LayerCount())
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid", err.Error())
		return
	}

	p.SetBoard(board)
	if err := d.Projects.Update(r.Context(), p); err != nil {
		mapServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, boardToLayoutData(board, p.Schematic()))
}

// handlePlace answers POST /api/v1/projects/{id}/place (synchronous).
func (d *Deps) handlePlace(w http.ResponseWriter, r *http.Request) {
	if d.Place == nil {
		writeError(w, r, http.StatusServiceUnavailable, "ai_unreachable", errNoService)
		return
	}

	body := PlaceJobStart{Strategy: "heuristic"}
	if r.ContentLength != 0 {
		if !decodeJSON(w, r, &body) {
			return
		}
	}
	if strings.TrimSpace(body.Strategy) == "" {
		body.Strategy = "heuristic"
	}

	board, err := d.Place.Place(r.Context(), r.PathValue("id"), body.Strategy)
	if err != nil {
		mapServiceError(w, r, err)
		return
	}

	// Recharger le projet pour renvoyer la vue persistée avec son schéma.
	p, ferr := d.Projects.FindByID(r.Context(), r.PathValue("id"))
	if ferr != nil {
		mapServiceError(w, r, ferr)
		return
	}
	writeJSON(w, http.StatusOK, boardToLayoutData(board, p.Schematic()))
}

// handleRoute answers POST /api/v1/projects/{id}/route (202 + job id).
func (d *Deps) handleRoute(w http.ResponseWriter, r *http.Request) {
	if d.Route == nil {
		writeError(w, r, http.StatusServiceUnavailable, "ai_unreachable", errNoService)
		return
	}

	body := RouteJobStart{Strategy: "astar"}
	if r.ContentLength != 0 {
		if !decodeJSON(w, r, &body) {
			return
		}
	}
	if strings.TrimSpace(body.Strategy) == "" {
		body.Strategy = "astar"
	}

	projectID := r.PathValue("id")
	jobID, err := d.Route.Start(r.Context(), projectID, body.Strategy, body.Nets)
	if err != nil {
		mapServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, JobStarted{JobID: jobID, ProjectID: projectID})
}

// handleOptimize answers POST /api/v1/projects/{id}/optimize (202 + job id).
func (d *Deps) handleOptimize(w http.ResponseWriter, r *http.Request) {
	if d.Optimize == nil {
		writeError(w, r, http.StatusServiceUnavailable, "ai_unreachable", errNoService)
		return
	}

	projectID := r.PathValue("id")
	jobID, err := d.Optimize.Start(r.Context(), projectID)
	if err != nil {
		mapServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, JobStarted{JobID: jobID, ProjectID: projectID})
}

// handleGetJob answers GET /api/v1/projects/{id}/jobs/{jobID}.
func (d *Deps) handleGetJob(w http.ResponseWriter, r *http.Request) {
	jobID := r.PathValue("jobID")

	if d.Route != nil {
		if info, ok := d.Route.JobStatus(jobID); ok {
			writeJSON(w, http.StatusOK, info)
			return
		}
	}
	if d.Optimize != nil {
		if info, ok := d.Optimize.JobStatus(jobID); ok {
			writeJSON(w, http.StatusOK, info)
			return
		}
	}
	writeError(w, r, http.StatusNotFound, "not_found", "job inconnu : "+jobID)
}

// handleDRC answers POST /api/v1/projects/{id}/drc.
func (d *Deps) handleDRC(w http.ResponseWriter, r *http.Request) {
	if d.DRC == nil {
		writeError(w, r, http.StatusServiceUnavailable, "internal", errNoService)
		return
	}
	result, err := d.DRC.Run(r.Context(), r.PathValue("id"))
	if err != nil {
		mapServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, drcToDTO(result))
}

// handleERC answers POST /api/v1/projects/{id}/erc.
func (d *Deps) handleERC(w http.ResponseWriter, r *http.Request) {
	if d.ERC == nil {
		writeError(w, r, http.StatusServiceUnavailable, "internal", errNoService)
		return
	}
	result, err := d.ERC.Run(r.Context(), r.PathValue("id"))
	if err != nil {
		mapServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, ercToDTO(result))
}

// --------------------------------------------------------------
// Conversions LayoutData <-> Board
// --------------------------------------------------------------

// poursToDTO maps domain pours onto the interchange DTO (used by the PUT
// layout preservation path).
func poursToDTO(pours []domainlayout.CopperPour) []pcbformat.PourDTO {
	out := make([]pcbformat.PourDTO, 0, len(pours))
	for i := range pours {
		p := &pours[i]
		pd := pcbformat.PourDTO{
			ID: p.ID, Net: p.Net, Name: p.Name, Layer: p.Layer,
			ClearanceMM: p.ClearanceMM, HatchMM: p.HatchMM,
			IsGround: p.IsGround, FillPct: p.FillPct,
			AreaMM2: p.AreaMM2, Stitched: p.Stitched,
			Outline: make([]pcbformat.PointDTO, 0, len(p.Outline)),
		}
		for _, pt := range p.Outline {
			pd.Outline = append(pd.Outline, pcbformat.PointDTO{X: pt.X, Y: pt.Y})
		}
		out = append(out, pd)
	}
	return out
}

// layoutDataToBoard rebuilds the board aggregate from the REST payload. The
// outline falls back to the previous board (or 100×80, two layers) when the
// payload omits it, and footprints of known references are restored from
// the previous board because the REST payload does not carry pad
// definitions.
func layoutDataToBoard(ld *LayoutData, prev *domainlayout.Board, projectLayers int) (*domainlayout.Board, error) {
	width, height, layers := ld.Board.WidthMM, ld.Board.HeightMM, ld.Board.LayerCount
	if prev != nil {
		if width <= 0 {
			width = prev.WidthMM
		}
		if height <= 0 {
			height = prev.HeightMM
		}
	}
	if width <= 0 {
		width = 100
	}
	if height <= 0 {
		height = 80
	}
	if layers < 1 {
		if prev != nil {
			layers = prev.LayerCount
		} else if projectLayers > 0 {
			layers = projectLayers
		} else {
			layers = 2
		}
	}
	if layers > 34 {
		return nil, errors.New("layout : nombre de couches invalide")
	}

	dto := layoutDataToLayoutDTO(ld)
	dto.Board = pcbformat.BoardDTO{
		WidthMM:    width,
		HeightMM:   height,
		LayerCount: layers,
		LayerNames: ld.Board.LayerNames,
	}

	// Les empreintes ne voyagent pas dans le DTO REST : elles sont
	// reprises du board existant (matched par nom d'empreinte), ce qui
	// préserve pads et nets à travers un aller-retour GET/PUT.
	if prev != nil {
		for i := range prev.Components {
			fp := &prev.Components[i].Footprint
			if fp.Name == "" {
				continue
			}
			if _, ok := dto.Footprints[fp.Name]; !ok {
				dto.Footprints[fp.Name] = footprintToDTO(fp)
			}
		}
		// Les plans de masse suivent la même règle : un payload qui
		// ne les mentionne pas les conserve (round-trip GET/PUT sûr).
		if len(ld.Pours) == 0 && len(prev.Pours) > 0 {
			dto.Pours = poursToDTO(prev.Pours)
		}
	}

	board, err := pcbformat.ToBoard(dto)
	if err != nil {
		return nil, err
	}
	if len(ld.Board.LayerNames) == layers && len(board.LayerNames) != layers {
		board.LayerNames = append([]string(nil), ld.Board.LayerNames...)
	}
	return board, nil
}

// --------------------------------------------------------------
// Conversions DRC / ERC
// --------------------------------------------------------------

// drcToDTO maps the application DRC result onto the REST payload.
func drcToDTO(result *verificationapp.DRCResult) DRCResult {
	if result == nil {
		return DRCResult{Violations: []DRCViolation{}}
	}
	out := DRCResult{
		Passed:       result.Passed,
		CheckedRules: result.CheckedRules,
		Violations:   []DRCViolation{},
		DurationMS:   result.DurationMS,
	}
	for _, v := range result.Violations {
		out.Violations = append(out.Violations, DRCViolation{
			Code: v.Code, Severity: v.Severity, Message: v.Message,
			X: v.X, Y: v.Y, Layer: v.Layer, Net: v.Net,
		})
	}
	return out
}

// ercToDTO maps the application ERC result onto the REST payload.
func ercToDTO(result *verificationapp.ERCResult) ERCResult {
	if result == nil {
		return ERCResult{Violations: []ERCViolation{}}
	}
	out := ERCResult{Passed: result.Passed, Violations: []ERCViolation{}}
	for _, v := range result.Violations {
		out.Violations = append(out.Violations, ERCViolation{
			Code: v.Code, Severity: v.Severity, Message: v.Message,
			ComponentRef: v.ComponentRef,
		})
	}
	return out
}
