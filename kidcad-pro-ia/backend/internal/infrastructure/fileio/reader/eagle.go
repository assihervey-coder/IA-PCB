package reader

import (
	"encoding/xml"
	"fmt"
	"math"
	"strconv"
	"strings"

	schematicapp "github.com/kidcad/kidcad-pro-ia/backend/internal/application/schematic"
	domainconstraints "github.com/kidcad/kidcad-pro-ia/backend/internal/domain/constraints"
	domainlayout "github.com/kidcad/kidcad-pro-ia/backend/internal/domain/layout"
)

// --------------------------------------------------------------
// Structures XML EAGLE 6+ (.brd / .sch)
// --------------------------------------------------------------

type eagleXML struct {
	XMLName xml.Name     `xml:"eagle"`
	Drawing eagleDrawing `xml:"drawing"`
}

type eagleDrawing struct {
	Packages  []eaglePackage  `xml:"packages>package"`
	Board     *eagleBoard     `xml:"board"`
	Schematic *eagleSchematic `xml:"schematic"`
}

type eaglePackage struct {
	Name string       `xml:"name,attr"`
	SMDs []eagleSMD   `xml:"smd"`
	Pads []eagleTHPad `xml:"pad"`
}

type eagleSMD struct {
	Name  string  `xml:"name,attr"`
	Layer int     `xml:"layer,attr"`
	X     float64 `xml:"x,attr"`
	Y     float64 `xml:"y,attr"`
	DX    float64 `xml:"dx,attr"`
	DY    float64 `xml:"dy,attr"`
	Rot   string  `xml:"rot,attr"`
}
type eagleTHPad struct {
	Name     string  `xml:"name,attr"`
	X        float64 `xml:"x,attr"`
	Y        float64 `xml:"y,attr"`
	Drill    float64 `xml:"drill,attr"`
	Diameter float64 `xml:"diameter,attr"`
	Shape    string  `xml:"shape,attr"`
	Rot      string  `xml:"rot,attr"`
}

type eagleBoard struct {
	Elements []eagleElement `xml:"elements>element"`
	Signals  []eagleSignal  `xml:"signals>signal"`
	Plain    []eagleWire    `xml:"plain>wire"`
}

type eagleElement struct {
	Name    string  `xml:"name,attr"`
	Package string  `xml:"package,attr"`
	Value   string  `xml:"value,attr"`
	X       float64 `xml:"x,attr"`
	Y       float64 `xml:"y,attr"`
	Rot     string  `xml:"rot,attr"`
}

type eagleSignal struct {
	Name        string            `xml:"name,attr"`
	Wires       []eagleWire       `xml:"wire"`
	Vias        []eagleVia        `xml:"via"`
	ContactRefs []eagleContactRef `xml:"contactref"`
}

type eagleWire struct {
	X1    float64 `xml:"x1,attr"`
	Y1    float64 `xml:"y1,attr"`
	X2    float64 `xml:"x2,attr"`
	Y2    float64 `xml:"y2,attr"`
	Width float64 `xml:"width,attr"`
	Layer int     `xml:"layer,attr"`
}

type eagleVia struct {
	X        float64 `xml:"x,attr"`
	Y        float64 `xml:"y,attr"`
	Extent   string  `xml:"extent,attr"`
	Drill    float64 `xml:"drill,attr"`
	Diameter float64 `xml:"diameter,attr"`
	Always   bool    `xml:"always,attr"`
}

type eagleContactRef struct {
	Element string `xml:"element,attr"`
	Pad     string `xml:"pad,attr"`
	Route   string `xml:"route,attr"`
}

type eagleSchematic struct {
	Parts  []eaglePart  `xml:"parts>part"`
	Sheets []eagleSheet `xml:"sheets>sheet"`
}

type eaglePart struct {
	Name      string `xml:"name,attr"`
	Deviceset string `xml:"deviceset,attr"`
	Device    string `xml:"device,attr"`
	Value     string `xml:"value,attr"`
}

type eagleSheet struct {
	Instances []eagleInstance `xml:"instances>instance"`
	Nets      []eagleNet      `xml:"nets>net"`
}

type eagleInstance struct {
	Part string  `xml:"part,attr"`
	X    float64 `xml:"x,attr"`
	Y    float64 `xml:"y,attr"`
	Rot  string  `xml:"rot,attr"`
}

type eagleNet struct {
	Name     string         `xml:"name,attr"`
	Class    string         `xml:"class,attr"`
	Segments []eagleSegment `xml:"segment"`
}

type eagleSegment struct {
	PinRefs []eaglePinRef `xml:"pinref"`
	Wires   []eagleWire   `xml:"wire"`
}

type eaglePinRef struct {
	Part string `xml:"part,attr"`
	Pin  string `xml:"pin,attr"`
}

// readEagle parses an EAGLE 6+ XML document (.brd or .sch, distinguished by
// the presence of the <board> / <schematic> blocks).
func readEagle(data []byte, path string) (schematicapp.ImportResult, error) {
	var doc eagleXML
	dec := xml.NewDecoder(strings.NewReader(string(data)))
	dec.Strict = false
	if err := dec.Decode(&doc); err != nil {
		return schematicapp.ImportResult{}, fmt.Errorf("eagle %s : XML invalide : %w", path, err)
	}
	if doc.XMLName.Local != "eagle" {
		return schematicapp.ImportResult{}, fmt.Errorf(
			"eagle %s : racine <%s> inattendue, document EAGLE requis", path, doc.XMLName.Local)
	}

	switch {
	case doc.Drawing.Board != nil:
		return readEagleBoard(&doc, path)
	case doc.Drawing.Schematic != nil:
		return readEagleSchematic(&doc, path)
	default:
		return schematicapp.ImportResult{}, fmt.Errorf(
			"eagle %s : ni <board> ni <schematic> dans le document", path)
	}
}

// eagleLayerIndex maps EAGLE copper layers onto domain indices for a 2-layer
// board (1 = Top -> 0, 16 = Bottom -> 1, inner layers clamped).
func eagleLayerIndex(layer int) int {
	switch {
	case layer <= 1:
		return 0
	case layer >= 16:
		return 1
	default:
		return 0
	}
}

// eagleRot parses a rotation attribute ("R90", "M90R45", "R45 90") into
// degrees, ignoring the mirror flag.
func eagleRot(rot string) float64 {
	rot = strings.TrimSpace(rot)
	idx := strings.IndexAny(rot, "Rr")
	if idx < 0 {
		return 0
	}
	rest := strings.Fields(rot[idx+1:])
	if len(rest) == 0 {
		return 0
	}
	v, err := strconv.ParseFloat(rest[0], 64)
	if err != nil {
		return 0
	}
	return v
}

// readEagleBoard converts an EAGLE .brd into schematic + board.
func readEagleBoard(doc *eagleXML, path string) (schematicapp.ImportResult, error) {
	var warnings []string
	boardDoc := doc.Drawing.Board

	// Packages -> empreintes (pads relatifs au centre de l'empreinte).
	packages := map[string]*domainlayout.Footprint{}
	for i := range doc.Drawing.Packages {
		pkg := &doc.Drawing.Packages[i]
		fp := &domainlayout.Footprint{Name: pkg.Name}
		for _, smd := range pkg.SMDs {
			fp.Pads = append(fp.Pads, domainlayout.Pad{
				Name:     smd.Name,
				Shape:    domainlayout.ShapeRect,
				X:        smd.X,
				Y:        smd.Y,
				Width:    smd.DX,
				Height:   smd.DY,
				Rotation: eagleRot(smd.Rot),
				Layer:    eagleLayerIndex(smd.Layer),
			})
		}
		for _, th := range pkg.Pads {
			diameter := th.Diameter
			if diameter <= 0 {
				diameter = th.Drill + 0.4
			}
			shape := domainlayout.ShapeCircle
			if th.Shape == "square" || th.Shape == "long" || th.Shape == "octagon" {
				shape = domainlayout.ShapeRect
			}
			fp.Pads = append(fp.Pads, domainlayout.Pad{
				Name:     th.Name,
				Shape:    shape,
				X:        th.X,
				Y:        th.Y,
				Width:    diameter,
				Height:   diameter,
				Rotation: eagleRot(th.Rot),
				Layer:    -1,
			})
		}
		fp.BodyWidthMM, fp.BodyHeightMM = eagleBodySize(fp.Pads)
		fp.HeightMM = domainlayout.DefaultBoardHeightMM
		packages[pkg.Name] = fp
	}

	// Outline : wires du calque 20 (Dimension).
	width, height := 100.0, 80.0
	var outline geometryAABB
	hasOutline := false
	for _, w := range boardDoc.Plain {
		if w.Layer != 20 {
			continue
		}
		if !hasOutline {
			outline = geometryAABB{MinX: w.X1, MinY: w.Y1, MaxX: w.X1, MaxY: w.Y1}
			hasOutline = true
		}
		outline.unionPoint(w.X1, w.Y1)
		outline.unionPoint(w.X2, w.Y2)
	}
	if hasOutline {
		if w := outline.MaxX - outline.MinX; w > 0 {
			width = w
		}
		if h := outline.MaxY - outline.MinY; h > 0 {
			height = h
		}
	}

	board, err := domainlayout.NewBoard(width, height, 2)
	if err != nil {
		return schematicapp.ImportResult{}, fmt.Errorf("eagle %s : %w", path, err)
	}

	// Elements -> composants placés + symboles.
	nb := newNetlistBuilder()
	for _, el := range boardDoc.Elements {
		fp := domainlayout.Footprint{Name: el.Package, HeightMM: domainlayout.DefaultBoardHeightMM}
		if known, ok := packages[el.Package]; ok && known != nil {
			clone := *known
			clone.Value = el.Value
			fp = clone
		} else {
			fp.BodyWidthMM, fp.BodyHeightMM = 2, 2
			warnings = append(warnings, fmt.Sprintf("package %q inconnu : empreinte minimale substituée", el.Package))
		}
		if err := board.AddComponent(domainlayout.PlacedComponent{
			Ref:       el.Name,
			Footprint: fp,
			X:         el.X,
			Y:         el.Y,
			Rotation:  eagleRot(el.Rot),
		}); err != nil {
			warnings = append(warnings, fmt.Sprintf("élément ignoré : %v", err))
		}
		nb.addComponent(el.Name, el.Value, el.Package)
	}

	// Signals -> pistes, vias et connexions électriques.
	for _, sig := range boardDoc.Signals {
		for _, w := range sig.Wires {
			board.AddTrack(domainlayout.Track{
				Net:   sig.Name,
				Layer: eagleLayerIndex(w.Layer),
				Width: w.Width,
				Points: []domainlayout.TrackPoint{
					{X: w.X1, Y: w.Y1},
					{X: w.X2, Y: w.Y2},
				},
			})
		}
		for _, v := range sig.Vias {
			drill := v.Drill
			if drill <= 0 {
				drill = 0.3
			}
			diameter := v.Diameter
			if diameter <= drill {
				diameter = drill + 0.3
			}
			if err := board.AddVia(domainlayout.Via{
				X: v.X, Y: v.Y,
				FromLayer: 0, ToLayer: 1,
				Diameter: diameter, Drill: drill,
				Net: sig.Name,
			}); err != nil {
				warnings = append(warnings, fmt.Sprintf("via ignoré : %v", err))
			}
		}
		for _, cr := range sig.ContactRefs {
			nb.addConnection(sig.Name, "", cr.Element, cr.Pad)
		}
	}

	sch, err := nb.build(schematicBaseName(path, "eagle"))
	if err != nil {
		return schematicapp.ImportResult{}, fmt.Errorf("eagle %s : %w", path, err)
	}

	res := schematicapp.ImportResult{
		Format:      "eagle",
		Schematic:   sch,
		Board:       board,
		Constraints: domainconstraints.NewDefault(),
		Warnings:    warnings,
	}
	return res, nil
}

// readEagleSchematic converts an EAGLE .sch into a schematic (no board
// geometry is available in a schematic sheet).
func readEagleSchematic(doc *eagleXML, path string) (schematicapp.ImportResult, error) {
	var warnings []string
	nb := newNetlistBuilder()

	for _, part := range doc.Drawing.Schematic.Parts {
		nb.addComponent(part.Name, part.Value, part.Deviceset)
	}

	for _, sheet := range doc.Drawing.Schematic.Sheets {
		for _, net := range sheet.Nets {
			for _, seg := range net.Segments {
				for _, pr := range seg.PinRefs {
					nb.addConnection(net.Name, net.Class, pr.Part, pr.Pin)
				}
			}
		}
	}

	sch, err := nb.build(schematicBaseName(path, "eagle-sch"))
	if err != nil {
		return schematicapp.ImportResult{}, fmt.Errorf("eagle %s : %w", path, err)
	}
	warnings = append(warnings,
		"carte absente : un .sch EAGLE ne contient pas de géométrie PCB (importer le .brd pour le layout)")

	res := schematicapp.ImportResult{
		Format:      "eagle",
		Schematic:   sch,
		Board:       nil,
		Constraints: domainconstraints.NewDefault(),
		Warnings:    warnings,
	}
	return res, nil
}

// eagleBodySize derives the body size from the pad bounding box (2 x 2 mm
// fallback), mirroring the KiCad reader behaviour.
func eagleBodySize(pads []domainlayout.Pad) (float64, float64) {
	minX, minY, maxX, maxY := 0.0, 0.0, 0.0, 0.0
	for i, p := range pads {
		x0, x1 := p.X-p.Width/2, p.X+p.Width/2
		y0, y1 := p.Y-p.Height/2, p.Y+p.Height/2
		if i == 0 {
			minX, maxX, minY, maxY = x0, x1, y0, y1
			continue
		}
		minX = math.Min(minX, x0)
		minY = math.Min(minY, y0)
		maxX = math.Max(maxX, x1)
		maxY = math.Max(maxY, y1)
	}
	if len(pads) == 0 || maxX-minX <= 0 || maxY-minY <= 0 {
		return 2, 2
	}
	return maxX - minX, maxY - minY
}
