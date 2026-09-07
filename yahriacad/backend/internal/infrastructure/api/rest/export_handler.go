package rest

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
)

// handleImport answers POST /api/v1/projects/{id}/import (multipart, field
// "file"). The upload is streamed to a temporary file so the readers work
// on a real path, then removed.
func (d *Deps) handleImport(w http.ResponseWriter, r *http.Request) {
	if d.Import == nil {
		writeError(w, r, http.StatusServiceUnavailable, "internal", errNoService)
		return
	}

	const maxUpload = 64 << 20 // 64 Mo
	r.Body = http.MaxBytesReader(w, r.Body, maxUpload)
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid", "formulaire multipart invalide : "+err.Error())
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid", "champ « file » absent du formulaire")
		return
	}
	defer file.Close()

	tmp, err := os.CreateTemp("", "yahriacad-import-*-"+filepath.Base(header.Filename))
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "internal", "fichier temporaire : "+err.Error())
		return
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if _, err := tmp.ReadFrom(file); err != nil {
		tmp.Close()
		writeError(w, r, http.StatusBadRequest, "invalid", "lecture du fichier : "+err.Error())
		return
	}
	if err := tmp.Close(); err != nil {
		writeError(w, r, http.StatusInternalServerError, "internal", "fichier temporaire : "+err.Error())
		return
	}

	res, err := d.Import.ImportFromFile(r.Context(), r.PathValue("id"), tmpPath)
	if err != nil {
		mapServiceError(w, r, err)
		return
	}

	components, nets := 0, 0
	if res.Schematic != nil {
		components, nets = len(res.Schematic.Components), len(res.Schematic.Nets)
	}
	tracks, vias := 0, 0
	if res.Board != nil {
		tracks, vias = len(res.Board.Tracks), len(res.Board.Vias)
	}
	warnings := res.Warnings
	if warnings == nil {
		warnings = []string{}
	}
	writeJSON(w, http.StatusOK, ImportResult{
		Format:     res.Format,
		Components: components,
		Nets:       nets,
		Tracks:     tracks,
		Vias:       vias,
		Warnings:   warnings,
	})
}

// handleExportGerber answers GET /api/v1/projects/{id}/export/gerber with
// the zip archive produced by the export use case.
func (d *Deps) handleExportGerber(w http.ResponseWriter, r *http.Request) {
	if d.Gerber == nil {
		writeError(w, r, http.StatusServiceUnavailable, "internal", errNoService)
		return
	}

	projectID := r.PathValue("id")
	zipPath, files, err := d.Gerber.ExportToZip(r.Context(), projectID)
	if err != nil {
		mapServiceError(w, r, err)
		return
	}

	payload, err := os.ReadFile(zipPath)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "internal", "lecture de l'archive : "+err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition",
		fmt.Sprintf("attachment; filename=\"%s\"", filepath.Base(zipPath)))
	w.Header().Set("X-YahriaCad-File-Count", strconv.Itoa(len(files)))
	_, _ = w.Write(payload)
}

// handleExportBOM answers GET /api/v1/projects/{id}/export/bom with the CSV
// bill of materials.
func (d *Deps) handleExportBOM(w http.ResponseWriter, r *http.Request) {
	if d.BOM == nil {
		writeError(w, r, http.StatusServiceUnavailable, "internal", errNoService)
		return
	}

	csv, err := d.BOM.ExportCSV(r.Context(), r.PathValue("id"))
	if err != nil {
		mapServiceError(w, r, err)
		return
	}

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename=\"bom.csv\"")
	_, _ = w.Write(csv)
}

// handleExportSTEP answers GET /api/v1/projects/{id}/export/step with the
// AP214 3D model.
func (d *Deps) handleExportSTEP(w http.ResponseWriter, r *http.Request) {
	if d.STEP == nil {
		writeError(w, r, http.StatusServiceUnavailable, "internal", errNoService)
		return
	}

	path, err := d.STEP.Export(r.Context(), r.PathValue("id"))
	if err != nil {
		mapServiceError(w, r, err)
		return
	}

	payload, err := os.ReadFile(path)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "internal", "lecture du modèle : "+err.Error())
		return
	}

	w.Header().Set("Content-Type", "model/step")
	w.Header().Set("Content-Disposition",
		fmt.Sprintf("attachment; filename=\"%s\"", filepath.Base(path)))
	_, _ = w.Write(payload)
}

// handleExportODBPP answers GET /api/v1/projects/{id}/export/odbpp with
// the .tgz ODB++ job produced by the export use case.
func (d *Deps) handleExportODBPP(w http.ResponseWriter, r *http.Request) {
	if d.ODB == nil {
		writeError(w, r, http.StatusServiceUnavailable, "internal", errNoService)
		return
	}

	projectID := r.PathValue("id")
	tgzPath, files, err := d.ODB.ExportToTgz(r.Context(), projectID)
	if err != nil {
		mapServiceError(w, r, err)
		return
	}

	payload, err := os.ReadFile(tgzPath)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "internal", "lecture de l'archive : "+err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition",
		fmt.Sprintf("attachment; filename=\"%s\"", filepath.Base(tgzPath)))
	w.Header().Set("X-YahriaCad-File-Count", strconv.Itoa(len(files)))
	_, _ = w.Write(payload)
}

// handleExportKicad answers GET /api/v1/projects/{id}/export/kicad with the
// board serialized as a KiCad .kicad_pcb file (s-expression, pcbnew 9),
// closing the KiCad round-trip loop.
func (d *Deps) handleExportKicad(w http.ResponseWriter, r *http.Request) {
	if d.Kicad == nil {
		writeError(w, r, http.StatusServiceUnavailable, "internal", errNoService)
		return
	}

	path, err := d.Kicad.ExportToFile(r.Context(), r.PathValue("id"))
	if err != nil {
		mapServiceError(w, r, err)
		return
	}

	payload, err := os.ReadFile(path)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "internal", "lecture du fichier : "+err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition",
		fmt.Sprintf("attachment; filename=\"%s\"", filepath.Base(path)))
	_, _ = w.Write(payload)
}
