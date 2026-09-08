package reader

import (
	"errors"
	"fmt"
	"path/filepath"

	schematicapp "github.com/assihervey-coder/IA-PCB/backend/internal/application/schematic"
)

// ErrCadenceBinary is raised for Cadence Allegro / OrCAD binary boards
// (.brd without XML prologue). Converting this closed binary format is on
// the roadmap; the documented workaround is a KiCad or YahriaCad interchange
// export.
var ErrCadenceBinary = errors.New(
	"cadence : fichiers Allegro/OrCAD binaires non supportés (roadmap) : " +
		"réexportez la carte en .kicad_pcb ou au format interchange (.yahriacad.json)")

// readCadenceBinary returns the typed unsupported-format error after the
// registry has detected a non-XML .brd payload.
func readCadenceBinary(path string) (schematicapp.ImportResult, error) {
	return schematicapp.ImportResult{}, fmt.Errorf("%s : %w", filepath.Base(path), ErrCadenceBinary)
}
