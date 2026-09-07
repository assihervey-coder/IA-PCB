package rest

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"

	verificationapp "github.com/assihervey-coder/IA-PCB/backend/internal/application/verification"
)

// Handler REST de l'oracle d'impédance différentielle — additif, hors
// contrat figé contracts.md :
//
//	POST /api/v1/projects/{id}/impedance
//	    {"targets": {"high-speed": 90}, "gap_mm": 0.2, "tolerance_pct": 10}
//	→ paires détectées (X+/X-, X_P/X_N, XP/XN), Zodd/Zeven/Zdiff/Zcom,
//	  skew intra-paire, largeur et écart recommandés, par classe de nets.
//
// Corps optionnel : un POST sans corps lance l'analyse par défaut
// (cibles 100 Ω, 90 Ω pour les classes contenant « usb », tolérance ±10 %).

// handleImpedance answers POST /api/v1/projects/{id}/impedance.
func (d *Deps) handleImpedance(w http.ResponseWriter, r *http.Request) {
	if d.Impedance == nil {
		writeError(w, r, http.StatusNotImplemented, "unavailable", "oracle d'impédance indisponible")
		return
	}
	var req verificationapp.ImpedanceRequest
	if r.Body != nil {
		data, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			writeError(w, r, http.StatusBadRequest, "invalid", "corps illisible : "+err.Error())
			return
		}
		if len(bytes.TrimSpace(data)) > 0 {
			if err := json.Unmarshal(data, &req); err != nil {
				writeError(w, r, http.StatusBadRequest, "invalid", "corps JSON invalide : "+err.Error())
				return
			}
		}
	}
	res, err := d.Impedance.Run(r.Context(), r.PathValue("id"), req)
	if mapServiceError(w, r, err) {
		return
	}
	writeJSON(w, http.StatusOK, res)
}
