// Handlers REST du modèle RL (additif, hors contrat figé contracts.md §2) :
// inspection détaillée du modèle PyTorch embarqué et rechargement à chaud
// du checkpoint, sans redémarrer le moteur IA ni le backend.
package rest

import (
	"net/http"

	layoutapp "github.com/assihervey-coder/IA-PCB/backend/internal/application/layout"
)

// AIModelReloadResult is the response of POST /api/v1/ai/model/reload.
type AIModelReloadResult struct {
	Loaded  bool                `json:"loaded"`
	Message string              `json:"message"`
	Info    layoutapp.ModelInfo `json:"info"`
}

// --------------------------------------------------------------
// Inspection du modèle RL
// --------------------------------------------------------------

// handleAIModelInfo answers GET /api/v1/ai/model.
func (d *Deps) handleAIModelInfo(w http.ResponseWriter, r *http.Request) {
	if d.AI == nil {
		writeError(w, r, http.StatusServiceUnavailable, "ai_unreachable", errNoService)
		return
	}
	info, err := d.AI.ModelInfo(r.Context())
	if mapServiceError(w, r, err) {
		return
	}
	writeJSON(w, http.StatusOK, info)
}

// --------------------------------------------------------------
// Rechargement à chaud du checkpoint
// --------------------------------------------------------------

// AIModelReload is the (optional) body of POST /api/v1/ai/model/reload.
type AIModelReload struct {
	// CheckpointPath re-targets the engine on another .pt file
	// (absolu ou relatif à la racine ai-engine). Vide : recharge le
	// checkpoint courant — le flux nominal après un ré-entraînement.
	CheckpointPath string `json:"checkpoint_path"`
}

// handleAIModelReload answers POST /api/v1/ai/model/reload.
func (d *Deps) handleAIModelReload(w http.ResponseWriter, r *http.Request) {
	if d.AI == nil {
		writeError(w, r, http.StatusServiceUnavailable, "ai_unreachable", errNoService)
		return
	}

	var req AIModelReload
	_ = decodeJSON(w, r, &req) // corps optionnel : {} = recharge le checkpoint courant

	info, message, err := d.AI.ReloadModel(r.Context(), req.CheckpointPath)
	if mapServiceError(w, r, err) {
		return
	}
	writeJSON(w, http.StatusOK, AIModelReloadResult{
		Loaded:  info.Loaded,
		Message: message,
		Info:    info,
	})
}
