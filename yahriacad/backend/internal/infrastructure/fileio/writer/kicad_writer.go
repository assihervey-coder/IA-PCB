package writer

import (
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	domainlayout "github.com/assihervey-coder/IA-PCB/backend/internal/domain/layout"
)

// KicadWriter generates a KiCad board file (.kicad_pcb, s-expression,
// file format version 20241229 as produced by pcbnew 9) from the layout
// domain aggregate. The goal is the KiCad round-trip: a board imported
// from KiCad, placed/routed by YahriaCad and exported back opens in
// pcbnew with its footprints, pads, nets, tracks and vias intact.
//
// Conventions (mirroring the reader in fileio/reader/kicad.go):
//   - footprint (at X Y Rotation) holds the PLACED position; pads carry
//     footprint-LOCAL coordinates, so the file round-trips byte-value
//     stable for pad geometry after a read → write cycle ;
//   - net 0 is the empty net, real nets are numbered from 1 (KiCad rule) ;
//   - copper layer ids follow the KiCad 9 numbering (F.Cu=0, B.Cu=2,
//     In1.Cu=4, In2.Cu=6, …).
type KicadWriter struct{}

// NewKicadWriter builds the .kicad_pcb generator.
func NewKicadWriter() *KicadWriter { return &KicadWriter{} }

// kicadCopperIDs maps a copper index (0 = F.Cu … LayerCount-1 = B.Cu) to
// the KiCad 9 numeric layer id: F.Cu=0, B.Cu=2, In1.Cu=4, In2.Cu=6…
func kicadCopperID(idx, layerCount int) int {
	switch idx {
	case 0:
		return 0
	case layerCount - 1:
		return 2
	default:
		return 2 + 2*idx // In1.Cu → 4, In2.Cu → 6 …
	}
}

// kicadCopperName returns the canonical KiCad name of copper layer idx.
func kicadCopperName(idx, layerCount int) string {
	if idx == 0 {
		return "F.Cu"
	}
	if idx == layerCount-1 {
		return "B.Cu"
	}
	return fmt.Sprintf("In%d.Cu", idx)
}

// kicadFixedLayers is the auxiliary layer set emitted on every export,
// with the KiCad 9 numeric ids observed in pcbnew 9 files.
var kicadFixedLayers = []struct {
	id   int
	name string
}{
	{1, "F.Mask"}, {3, "B.Mask"}, {5, "F.SilkS"}, {7, "B.SilkS"},
	{13, "F.Paste"}, {15, "B.Paste"}, {25, "Edge.Cuts"},
	{29, "B.CrtYd"}, {31, "F.CrtYd"},
}

// fm formats a millimetre coordinate the way pcbnew does: up to 6
// decimals, trailing zeros trimmed.
func fm(v float64) string {
	s := strconv.FormatFloat(v, 'f', 6, 64)
	s = strings.TrimRight(s, "0")
	return strings.TrimSuffix(s, ".")
}

// kicadUUID returns a fresh RFC 4122 v4 identifier (pcbnew stamps one on
// every object; missing uuids are tolerated by the reader but rejected by
// some third-party consumers).
func kicadUUID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "00000000-0000-0000-0000-000000000000"
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// kicadNetTable collects every net referenced by pads, tracks and vias and
// assigns KiCad numbers: net 0 is the empty net, the rest are sorted by
// name and numbered from 1.
func kicadNetTable(b *domainlayout.Board) (map[string]int, []string) {
	set := map[string]bool{}
	for i := range b.Components {
		for _, pad := range b.Components[i].Footprint.Pads {
			if pad.Net != "" {
				set[pad.Net] = true
			}
		}
	}
	for i := range b.Tracks {
		if b.Tracks[i].Net != "" {
			set[b.Tracks[i].Net] = true
		}
	}
	for i := range b.Vias {
		if b.Vias[i].Net != "" {
			set[b.Vias[i].Net] = true
		}
	}
	names := make([]string, 0, len(set))
	for n := range set {
		names = append(names, n)
	}
	sort.Strings(names)
	table := map[string]int{"": 0}
	for i, n := range names {
		table[n] = i + 1
	}
	return table, names
}

// kicadPadShape maps a domain pad shape to a KiCad pad shape token.
func kicadPadShape(s domainlayout.PadShape) string {
	switch s {
	case domainlayout.ShapeRect:
		return "rect"
	case domainlayout.ShapeCircle:
		return "circle"
	case domainlayout.ShapeOval:
		return "oval"
	default:
		return "roundrect"
	}
}

// Write generates the .kicad_pcb file of the board inside outDir and
// returns its path.
func (w *KicadWriter) Write(b *domainlayout.Board, projectName, outDir string) (string, error) {
	if b == nil {
		return "", fmt.Errorf("kicad : carte absente")
	}
	if b.LayerCount < 1 {
		return "", fmt.Errorf("kicad : nombre de couches invalide (%d)", b.LayerCount)
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return "", fmt.Errorf("kicad : répertoire de sortie : %w", err)
	}
	name := slugifyName(projectName)
	if name == "" {
		name = "board"
	}
	path := filepath.Join(outDir, name+".kicad_pcb")

	netTable, netNames := kicadNetTable(b)

	var sb strings.Builder
	e := kicadEmitter{sb: &sb, nets: netTable, layerCnt: b.LayerCount}
	e.header(b)
	e.layers(b)
	e.netDecls(netNames)
	e.footprints(b)
	e.segments(b)
	e.vias(b)
	e.edgeCuts(b)
	e.printf(")\n") // fermeture de (kicad_pcb
	if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
		return "", fmt.Errorf("kicad : écriture du fichier : %w", err)
	}
	return path, nil
}

// kicadEmitter accumulates the s-expression output.
type kicadEmitter struct {
	sb       *strings.Builder
	nets     map[string]int
	layerCnt int
}

func (e *kicadEmitter) printf(format string, args ...any) {
	fmt.Fprintf(e.sb, format, args...)
}

// header emits the file header (version pcbnew 9, board thickness, paper).
func (e *kicadEmitter) header(b *domainlayout.Board) {
	e.printf("(kicad_pcb (version 20241229) (generator \"yahriacad\") (generator_version \"0.1.0\")\n")
	e.printf("\t(general (thickness 1.6) (legacy_teardrops no))\n")
	e.printf("\t(paper \"A4\")\n")
	e.printf("\t(title_block (title %q) (date \"\") (rev \"\"))\n", "YahriaCad board")
}

// layers emits the (layers …) block: copper layers then the fixed
// auxiliary set (mask, silkscreen, paste, Edge.Cuts, courtyard).
func (e *kicadEmitter) layers(b *domainlayout.Board) {
	e.printf("\t(layers\n")
	for idx := 0; idx < b.LayerCount; idx++ {
		id := kicadCopperID(idx, b.LayerCount)
		e.printf("\t\t(%d %q signal)\n", id, kicadCopperName(idx, b.LayerCount))
	}
	for _, l := range kicadFixedLayers {
		e.printf("\t\t(%d %q user)\n", l.id, l.name)
	}
	e.printf("\t)\n")
}

// netDecls emits (net N "name") declarations, net 0 being the empty net.
func (e *kicadEmitter) netDecls(names []string) {
	e.printf("\t(net 0 \"\")\n")
	for i, n := range names {
		e.printf("\t(net %d %q)\n", i+1, n)
	}
}

// padLayers returns the layer list of a pad: through-hole pads span all
// copper layers and both masks, SMD pads stay on their copper layer.
func (e *kicadEmitter) padLayers(p domainlayout.Pad) string {
	if p.Layer < 0 {
		return "\"*.Cu\" \"*.Mask\""
	}
	cu := kicadCopperName(p.Layer, e.layerCnt)
	if p.Layer == 0 {
		return fmt.Sprintf("%q %q %q", cu, "F.Paste", "F.Mask")
	}
	return fmt.Sprintf("%q %q %q", cu, "B.Paste", "B.Mask")
}

// pad emits one (pad …) node with LOCAL footprint coordinates.
func (e *kicadEmitter) pad(p domainlayout.Pad) {
	kind := "smd"
	if p.Layer < 0 {
		kind = "thru_hole"
	}
	shape := kicadPadShape(p.Shape)
	e.printf("\t\t(pad %q %s %s (at %s %s", p.Name, kind, shape, fm(p.X), fm(p.Y))
	if p.Rotation != 0 {
		e.printf(" %s", fm(p.Rotation))
	}
	e.printf(") (size %s %s)", fm(p.Width), fm(p.Height))
	if p.Layer < 0 {
		drill := p.Width / 2
		if p.Height < p.Width {
			drill = p.Height / 2
		}
		e.printf(" (drill %s)", fm(drill))
	}
	if netID, ok := e.nets[p.Net]; ok && netID > 0 {
		e.printf(" (net %d %q)", netID, p.Net)
	}
	e.printf(" (layers %s))\n", e.padLayers(p))
}

// footprintLayer déduit le côté d'une empreinte de ses pastilles : une
// empreinte purement SMD dont toutes les pastilles sont sur le dernier
// cuivre est une empreinte arrière (B.Cu). Les empreintes à pastilles
// traversantes ou mixtes restent écrites côté F.Cu — le domaine ne porte
// pas le côté de l'empreinte, ce chemin est le plus fidèle réversible.
func (e *kicadEmitter) footprintLayer(c *domainlayout.PlacedComponent) string {
	last := e.layerCnt - 1
	smd := false
	for i := range c.Footprint.Pads {
		p := &c.Footprint.Pads[i]
		if p.Layer < 0 || p.Layer == 0 {
			return "F.Cu"
		}
		smd = true
	}
	if smd && last > 0 {
		return "B.Cu"
	}
	return "F.Cu"
}

// footprints emits every placed component.
func (e *kicadEmitter) footprints(b *domainlayout.Board) {
	for i := range b.Components {
		c := &b.Components[i]
		e.printf("\t(footprint %q\n", c.Footprint.Name)
		e.printf("\t\t(layer %q)\n", e.footprintLayer(c))
		e.printf("\t\t(uuid %q)\n", kicadUUID())
		e.printf("\t\t(at %s %s", fm(c.X), fm(c.Y))
		if c.Rotation != 0 {
			e.printf(" %s", fm(c.Rotation))
		}
		e.printf(")\n")
		e.printf("\t\t(property \"Reference\" %q (at 0 0 0) (layer \"F.SilkS\")\n", c.Ref)
		e.printf("\t\t\t(effects (font (size 1 1) (thickness 0.15)))\n\t\t)\n")
		e.printf("\t\t(property \"Value\" %q (at 0 0 0) (layer \"F.Fab\")\n", c.Footprint.Value)
		e.printf("\t\t\t(effects (font (size 1 1) (thickness 0.15)))\n\t\t)\n")
		throughHole := false
		smd := false
		for _, p := range c.Footprint.Pads {
			if p.Layer < 0 {
				throughHole = true
			} else {
				smd = true
			}
		}
		switch {
		case throughHole && !smd:
			e.printf("\t\t(attr through_hole)\n")
		case smd:
			e.printf("\t\t(attr smd)\n")
		}
		for _, p := range c.Footprint.Pads {
			e.pad(p)
		}
		e.printf("\t)\n")
	}
}

// segments emits one (segment …) per consecutive point pair of every track.
func (e *kicadEmitter) segments(b *domainlayout.Board) {
	for i := range b.Tracks {
		t := &b.Tracks[i]
		layerName := kicadCopperName(t.Layer, e.layerCnt)
		netID := e.nets[t.Net]
		for k := 1; k < len(t.Points); k++ {
			a, c := t.Points[k-1], t.Points[k]
			e.printf("\t(segment (start %s %s) (end %s %s) (width %s) (layer %q) (net %d) (uuid %q))\n",
				fm(a.X), fm(a.Y), fm(c.X), fm(c.Y), fm(t.Width), layerName, netID, kicadUUID())
		}
	}
}

// vias emits (via …) nodes ; non through vias are marked blind.
func (e *kicadEmitter) vias(b *domainlayout.Board) {
	last := e.layerCnt - 1
	for i := range b.Vias {
		v := &b.Vias[i]
		e.printf("\t(via")
		if v.FromLayer != 0 || v.ToLayer != last {
			e.printf(" blind")
		}
		e.printf(" (at %s %s) (size %s) (drill %s) (layers %q %q) (net %d) (uuid %q))\n",
			fm(v.X), fm(v.Y), fm(v.Diameter), fm(v.Drill),
			kicadCopperName(v.FromLayer, e.layerCnt), kicadCopperName(v.ToLayer, e.layerCnt),
			e.nets[v.Net], kicadUUID())
	}
}

// edgeCuts emits the board outline as a (gr_rect …) on Edge.Cuts.
func (e *kicadEmitter) edgeCuts(b *domainlayout.Board) {
	e.printf("\t(gr_rect (start 0 0) (end %s %s) (stroke (width 0.1) (type solid)) (fill none) (layer \"Edge.Cuts\") (uuid %q))\n",
		fm(b.WidthMM), fm(b.HeightMM), kicadUUID())
}
