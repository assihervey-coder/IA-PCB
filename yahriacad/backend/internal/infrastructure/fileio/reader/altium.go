package reader

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	schematicapp "github.com/assihervey-coder/IA-PCB/backend/internal/application/schematic"
	domainconstraints "github.com/assihervey-coder/IA-PCB/backend/internal/domain/constraints"
)

// ErrAltiumBinary is raised for OLE2 Compound Files (.SchDoc / .PcbDoc).
// Converting the binary Altium format is on the roadmap; the documented
// workaround is a Protel ASCII netlist export or the interchange format.
var ErrAltiumBinary = errors.New(
	"conversion Altium binaire non supportée : exporter une netlist Protel (.net) ou utiliser le format interchange")

// readAltiumBinary returns the typed unsupported-format error. The registry
// calls it once the OLE2 signature has been detected, so the user gets a
// precise message instead of a raw parse failure.
func readAltiumBinary(path string) (schematicapp.ImportResult, error) {
	return schematicapp.ImportResult{}, fmt.Errorf("%s : %w", filepath.Base(path), ErrAltiumBinary)
}

// --------------------------------------------------------------
// Netlist Protel / Altium ASCII
// --------------------------------------------------------------

// readProtelNetlist parses a Protel/Altium ASCII netlist:
//
//	*PCB*
//	*NET*
//	GND U1-4 J1-2 R1-2
//	VCC U1-8 J1-1
//
// Components are discovered from the pin tokens (REF-PIN, split at the first
// dash). Only the schematic view is produced (no board geometry).
func readProtelNetlist(data []byte, path string) (schematicapp.ImportResult, error) {
	var warnings []string
	lines := splitLines(data)

	nb := newNetlistBuilder()
	section := ""
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "*") {
			section = strings.ToUpper(line)
			continue
		}
		if section != "*NET*" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue // section *POW* ou ligne de légende : ignorée
		}
		netName := fields[0]
		for _, pin := range fields[1:] {
			ref, pinNum, ok := splitProtelPin(pin)
			if !ok {
				continue
			}
			nb.addComponent(ref, "", "")
			nb.addConnection(netName, "", ref, pinNum)
		}
	}

	sch, err := nb.build(schematicBaseName(path, "protel"))
	if err != nil {
		return schematicapp.ImportResult{}, fmt.Errorf("netlist Protel %s : %w", filepath.Base(path), err)
	}
	if len(sch.Nets) == 0 {
		return schematicapp.ImportResult{}, fmt.Errorf(
			"netlist Protel %s : aucune section *NET* exploitable", filepath.Base(path))
	}
	warnings = append(warnings,
		"carte absente : la netlist Protel ne contient pas de géométrie (placement et routage à générer)")

	res := schematicapp.ImportResult{
		Format:      "protel-netlist",
		Schematic:   sch,
		Constraints: domainconstraints.NewDefault(),
		Warnings:    warnings,
	}
	return res, nil
}

// splitProtelPin splits a "REF-PIN" token at the first dash.
func splitProtelPin(token string) (string, string, bool) {
	idx := strings.Index(token, "-")
	if idx <= 0 || idx == len(token)-1 {
		return "", "", false
	}
	return token[:idx], token[idx+1:], true
}

// schematicBaseName returns the file name without extension, used as the
// schematic name.
func schematicBaseName(path, fallback string) string {
	base := filepath.Base(path)
	if ext := filepath.Ext(base); ext != "" {
		base = strings.TrimSuffix(base, ext)
	}
	if strings.TrimSpace(base) == "" {
		return fallback
	}
	return base
}
