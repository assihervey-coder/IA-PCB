package reader

import (
	"encoding/json"
	"fmt"

	schematicapp "github.com/assihervey-coder/IA-PCB/backend/internal/application/schematic"
	domainconstraints "github.com/assihervey-coder/IA-PCB/backend/internal/domain/constraints"
	domainlayout "github.com/assihervey-coder/IA-PCB/backend/internal/domain/layout"
	"github.com/assihervey-coder/IA-PCB/backend/pkg/pcb-format"
)

// --------------------------------------------------------------
// Lecteurs génériques : interchange .yahriacad.json, netlist KiCad .net,
// netlist JSON simple
// --------------------------------------------------------------

// readInterchange decodes a YahriaCad interchange document (.yahriacad.json) and
// rebuilds both views (schematic + board) when present.
func readInterchange(data []byte, path string) (schematicapp.ImportResult, error) {
	f, err := pcbformat.Decode(data)
	if err != nil {
		return schematicapp.ImportResult{}, fmt.Errorf("interchange %s : %w", path, err)
	}

	sch, err := pcbformat.ToSchematic(f.Schematic)
	if err != nil {
		return schematicapp.ImportResult{}, fmt.Errorf("interchange %s : schéma : %w", path, err)
	}

	var board *domainlayout.Board
	var warnings []string
	if f.Layout != nil {
		b, err := pcbformat.ToBoard(f.Layout)
		if err != nil {
			return schematicapp.ImportResult{}, fmt.Errorf("interchange %s : layout : %w", path, err)
		}
		board = b
	} else {
		warnings = append(warnings, "layout absent du document interchange : seul le schéma a été importé")
	}

	res := schematicapp.ImportResult{
		Format:      "interchange",
		Schematic:   sch,
		Board:       board,
		Constraints: domainconstraints.NewDefault(),
		Warnings:    warnings,
	}
	return res, nil
}

// readKiCadNetlist parses a KiCad netlist (.net, s-expression):
//
//	(export (version "E")
//	  (components (comp (ref "R1") (value "10k") (footprint "R_0603")))
//	  (nets (net (code "1") (name "GND")
//	         (node (ref "R1") (pin "1")) ...)))
//
// Only the schematic view is produced (a netlist carries no geometry).
func readKiCadNetlist(data []byte, path string) (schematicapp.ImportResult, error) {
	root, err := parseSexpr(data)
	if err != nil {
		return schematicapp.ImportResult{}, fmt.Errorf("netlist kicad %s : %w", path, err)
	}
	export := root.child("export")
	if export == nil {
		return schematicapp.ImportResult{}, fmt.Errorf(
			"netlist kicad %s : racine (export ...) introuvable", path)
	}

	nb := newNetlistBuilder()
	for _, comp := range export.childrenWithTag("comp") {
		nb.addComponent(
			nodeText(comp.child("ref")),
			nodeText(comp.child("value")),
			nodeText(comp.child("footprint")),
		)
	}
	for _, net := range export.childrenWithTag("net") {
		netName := nodeText(net.child("name"))
		if netName == "" {
			continue
		}
		for _, node := range net.childrenWithTag("node") {
			nb.addConnection(netName, "", nodeText(node.child("ref")), nodeText(node.child("pin")))
		}
	}

	sch, err := nb.build(schematicBaseName(path, "kicad-netlist"))
	if err != nil {
		return schematicapp.ImportResult{}, fmt.Errorf("netlist kicad %s : %w", path, err)
	}

	res := schematicapp.ImportResult{
		Format:      "kicad-netlist",
		Schematic:   sch,
		Board:       nil,
		Constraints: domainconstraints.NewDefault(),
		Warnings: []string{
			"carte absente : une netlist KiCad ne contient pas de géométrie PCB",
		},
	}
	return res, nil
}

// nodeText returns the first atom of a (tag "value") node.
func nodeText(n *sNode) string {
	if n == nil {
		return ""
	}
	return n.arg(0)
}

// readJSONNetlist parses the minimal JSON netlist schema
// (`{"components":[...],"nets":[...]}` with interchange-compatible fields).
func readJSONNetlist(data []byte, path string) (schematicapp.ImportResult, error) {
	dto := &pcbformat.SchematicDTO{}
	if err := json.Unmarshal(data, dto); err != nil {
		return schematicapp.ImportResult{}, fmt.Errorf("netlist json %s : %w", path, err)
	}
	if len(dto.Components) == 0 && len(dto.Nets) == 0 {
		return schematicapp.ImportResult{}, fmt.Errorf(
			"netlist json %s : aucune clé 'components' ou 'nets' exploitable", path)
	}

	sch, err := pcbformat.ToSchematic(dto)
	if err != nil {
		return schematicapp.ImportResult{}, fmt.Errorf("netlist json %s : %w", path, err)
	}

	var warnings []string
	if len(sch.Components) == 0 {
		warnings = append(warnings, "aucun composant dans la netlist")
	}
	if len(sch.Nets) == 0 {
		warnings = append(warnings, "aucun net dans la netlist")
	}

	res := schematicapp.ImportResult{
		Format:      "json-netlist",
		Schematic:   sch,
		Board:       nil,
		Constraints: domainconstraints.NewDefault(),
		Warnings: append(warnings,
			"carte absente : une netlist JSON ne contient pas de géométrie PCB"),
	}
	return res, nil
}
