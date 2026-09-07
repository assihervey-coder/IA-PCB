// ODB++ writer — generates a simplified ODB++ v8 job tree for the board:
// matrix, netlist, step general, per-layer copper features (lines, pads,
// vias) and component placements. The tree is deliberately conservative
// (core grammar only) so downstream CAM tools can ingest it; units are
// micrometres everywhere (UNITS=UM).
package writer

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"

	domainlayout "github.com/assihervey-coder/IA-PCB/backend/internal/domain/layout"
)

// odbScale converts millimetres into ODB++ integer micrometres.
const odbScale = 1000.0

// um converts mm to µm (integer).
func um(mm float64) int { return int(math.Round(mm * odbScale)) }

// ODBPPWriter produces a simplified ODB++ v8 job tree. It satisfies the
// export application port (Write must create outDir when missing and
// return the produced file paths).
type ODBPPWriter struct{}

// NewODBPPWriter builds the ODB++ generator.
func NewODBPPWriter() *ODBPPWriter { return &ODBPPWriter{} }

// Write generates the ODB++ job tree of the board inside outDir (the outDir
// IS the job directory) and returns the produced file paths.
func (w *ODBPPWriter) Write(b *domainlayout.Board, projectName, outDir string) ([]string, error) {
	if b == nil {
		return nil, fmt.Errorf("odb++ : carte absente")
	}
	if b.LayerCount < 1 {
		return nil, fmt.Errorf("odb++ : nombre de couches invalide (%d)", b.LayerCount)
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return nil, fmt.Errorf("odb++ : répertoire de sortie : %w", err)
	}

	var files []string
	writeFile := func(relPath, content string) error {
		abs := filepath.Join(outDir, relPath)
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			return fmt.Errorf("odb++ : arborescence %s : %w", filepath.Dir(relPath), err)
		}
		if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
			return fmt.Errorf("odb++ : écriture de %s : %w", relPath, err)
		}
		files = append(files, abs)
		return nil
	}

	// Pré-calcul des pastilles absolues par couche (traversant = toutes
	// les couches), même convention que le writer Gerber.
	padsByLayer := odbPadsByLayer(b)

	// --- matrix/matrix -------------------------------------------------
	if err := writeFile("matrix/matrix", odbMatrix(b)); err != nil {
		return nil, err
	}

	// --- steps/pcb/general/general ------------------------------------
	step := "steps/pcb"
	if err := writeFile(step+"/general/general", odbGeneral(b)); err != nil {
		return nil, err
	}

	// --- couches cuivre : features + components ------------------------
	symbols := newODBSymbolTable()
	for idx := 0; idx < b.LayerCount; idx++ {
		dir := fmt.Sprintf("%s/layers/pclk%d", step, idx+1)
		features, err := odbCopperFeatures(b, idx, padsByLayer[idx], symbols)
		if err != nil {
			return nil, err
		}
		if err := writeFile(dir+"/features", features); err != nil {
			return nil, err
		}
		// Les composants sont placés sur la couche extérieure de leur
		// corps : F.Cu (idx 0) ou B.Cu (dernière), jamais au milieu.
		if idx == 0 || idx == b.LayerCount-1 {
			if err := writeFile(dir+"/components", odbComponents(b, idx)); err != nil {
				return nil, err
			}
		}
	}

	// --- symbols (après collecte des symboles utilisés) ----------------
	if err := writeFile(step+"/symbols/symbols", symbols.render()); err != nil {
		return nil, err
	}

	// --- netlist/netlist ------------------------------------------------
	if err := writeFile("netlist/netlist", odbNetlist(b)); err != nil {
		return nil, err
	}

	return files, nil
}

// -------------------------------------------------------------- matrix

// odbMatrix renders the layer matrix: one signal row per copper layer
// (simplified single-column table, folders pclk1..pclkN).
func odbMatrix(b *domainlayout.Board) string {
	var sb strings.Builder
	sb.WriteString("# YahriaCad ODB++ (simplifie) — matrice de couches\n")
	sb.WriteString(fmt.Sprintf("TABLE { ROWS=%d COLS=2 }\n", b.LayerCount+1))
	for idx := 0; idx < b.LayerCount; idx++ {
		name := odbLayerName(b, idx)
		sb.WriteString(fmt.Sprintf("L %q (1) = #%d\n", name, idx+1))
	}
	return sb.String()
}

// odbLayerName returns the canonical layer name (F.Cu, In1.Cu, B.Cu) or a
// fallback when LayerNames is incomplete.
func odbLayerName(b *domainlayout.Board, idx int) string {
	if idx < len(b.LayerNames) && b.LayerNames[idx] != "" {
		return b.LayerNames[idx]
	}
	if idx == 0 {
		return "F.Cu"
	}
	if idx == b.LayerCount-1 {
		return "B.Cu"
	}
	return fmt.Sprintf("In%d.Cu", idx)
}

// -------------------------------------------------------------- general

// odbGeneral renders the step header (units, board size).
func odbGeneral(b *domainlayout.Board) string {
	var sb strings.Builder
	sb.WriteString("# YahriaCad ODB++ (simplifie) — en-tete du step 'pcb'\n")
	sb.WriteString("UNITS { UNITS=UM }\n")
	sb.WriteString(fmt.Sprintf("BOUNDS { X1 0 Y1 0 X2 %d Y2 %d }\n", um(b.WidthMM), um(b.HeightMM)))
	return sb.String()
}

// -------------------------------------------------------------- features

// odbPad describes one absolute pad instance (through-hole repeated per
// layer), same convention as the Gerber writer's padFlash.
type odbPad struct {
	Ref           string
	Pad           string
	X, Y          float64
	Width, Height float64
	Shape         domainlayout.PadShape
}

// odbPadsByLayer returns the pads present on every copper layer with their
// absolute position; through-hole pads (Layer < 0) appear on all layers.
func odbPadsByLayer(b *domainlayout.Board) map[int][]odbPad {
	out := make(map[int][]odbPad, b.LayerCount)
	for idx := 0; idx < b.LayerCount; idx++ {
		out[idx] = []odbPad{}
	}
	for i := range b.Components {
		c := &b.Components[i]
		for _, pad := range c.Footprint.Pads {
			pos := c.PadAbsolutePosition(pad)
			width, height := pad.Width, pad.Height
			shape := pad.Shape
			if shape == domainlayout.ShapeRect && isQuarterTurn(pad.Rotation) {
				width, height = height, width
			}
			if shape == "" {
				shape = domainlayout.ShapeRoundRect
			}
			p := odbPad{
				Ref: c.Ref, Pad: pad.Name,
				X: pos.X, Y: pos.Y,
				Width: width, Height: height, Shape: shape,
			}
			if pad.Layer < 0 {
				for idx := 0; idx < b.LayerCount; idx++ {
					out[idx] = append(out[idx], p)
				}
				continue
			}
			if pad.Layer < b.LayerCount {
				out[pad.Layer] = append(out[pad.Layer], p)
			}
		}
	}
	return out
}

// odbSymbolTable collects and renders the symbols referenced by features.
type odbSymbolTable struct {
	keys  []string
	order map[string]string // key -> ligne symbole
}

func newODBSymbolTable() *odbSymbolTable {
	return &odbSymbolTable{order: map[string]string{}}
}

func (t *odbSymbolTable) add(key, line string) {
	if _, ok := t.order[key]; !ok {
		t.keys = append(t.keys, key)
		t.order[key] = line
	}
}

// trackSymbol registers a width symbol and returns its inline form.
func (t *odbSymbolTable) trackSymbol(widthMM float64) string {
	w := um(widthMM)
	if w <= 0 {
		w = 1
	}
	key := fmt.Sprintf("w%d", w)
	t.add(key, fmt.Sprintf("w %d", w))
	return key
}

// padSymbol registers a pad symbol and returns its inline form.
func (t *odbSymbolTable) padSymbol(shape domainlayout.PadShape, widthMM, heightMM float64) string {
	w, h := um(widthMM), um(heightMM)
	if w <= 0 {
		w = 1
	}
	if h <= 0 {
		h = 1
	}
	if shape == domainlayout.ShapeCircle || shape == domainlayout.ShapeRoundRect && widthMM == heightMM {
		key := fmt.Sprintf("c%d", w)
		t.add(key, fmt.Sprintf("c %d", w))
		return key
	}
	key := fmt.Sprintf("r%dx%d", w, h)
	t.add(key, fmt.Sprintf("r %d %d", w, h))
	return key
}

func (t *odbSymbolTable) render() string {
	sort.Strings(t.keys)
	var sb strings.Builder
	sb.WriteString("# YahriaCad ODB++ (simplifie) — symboles (unites : um)\n")
	for _, k := range t.keys {
		sb.WriteString(t.order[k])
		sb.WriteString("\n")
	}
	return sb.String()
}

// odbCopperFeatures renders the features file of one copper layer:
// pads (P), track segments (L) then vias (V), all in µm.
func odbCopperFeatures(b *domainlayout.Board, layerIdx int, pads []odbPad, symbols *odbSymbolTable) (string, error) {
	var sb strings.Builder
	sb.WriteString("#\n#Class = copper\n#\nUNITS=UM\n")

	// Pastilles d'abord (les lecteurs ODB s'attendent au cuivre statique).
	for _, p := range pads {
		sym := symbols.padSymbol(p.Shape, p.Width, p.Height)
		sb.WriteString(fmt.Sprintf("P %d %d %s;\n", um(p.X), um(p.Y), sym))
	}

	// Pistes : chaque segment de polyline devient une ligne L.
	for i := range b.Tracks {
		tr := &b.Tracks[i]
		if tr.Layer != layerIdx || len(tr.Points) < 2 {
			continue
		}
		sym := symbols.trackSymbol(tr.Width)
		for j := 0; j+1 < len(tr.Points); j++ {
			a, c := tr.Points[j], tr.Points[j+1]
			sb.WriteString(fmt.Sprintf("L %d %d %d %d %s;\n",
				um(a.X), um(a.Y), um(c.X), um(c.Y), sym))
		}
	}

	// Vias : record V avec le perçage (d<µm>) — le pad annulaire complet
	// reste couvert par le writer Gerber (simplification documentée).
	for i := range b.Vias {
		v := &b.Vias[i]
		covers := (v.FromLayer <= layerIdx && layerIdx <= v.ToLayer) ||
			(v.ToLayer <= layerIdx && layerIdx <= v.FromLayer)
		if !covers {
			continue
		}
		drill := um(v.Drill)
		if drill <= 0 {
			drill = 1
		}
		sb.WriteString(fmt.Sprintf("V %d %d d%d;\n", um(v.X), um(v.Y), drill))
	}

	return sb.String(), nil
}

// -------------------------------------------------------------- components

// odbComponents renders the components file of the top (idx 0) or bottom
// (last) layer: SMD on its layer, through-hole reported on the top.
func odbComponents(b *domainlayout.Board, layerIdx int) string {
	var sb strings.Builder
	sb.WriteString("#\n#Class = components\n#\nUNITS=UM\n")
	for i := range b.Components {
		c := &b.Components[i]
		isTop := layerIdx == 0
		if !odbComponentOnLayer(c, isTop) {
			continue
		}
		sb.WriteString(fmt.Sprintf("COMP %s {\n", c.Ref))
		sb.WriteString(fmt.Sprintf("\tPLACE_X %d\n", um(c.X)))
		sb.WriteString(fmt.Sprintf("\tPLACE_Y %d\n", um(c.Y)))
		sb.WriteString(fmt.Sprintf("\tROTATION %g\n", math.Round(c.Rotation*100)/100))
		sb.WriteString("}\n")
	}
	return sb.String()
}

// odbComponentOnLayer decides the mounting side: through-hole bodies mount
// on top; SMD follows its first pad's layer.
func odbComponentOnLayer(c *domainlayout.PlacedComponent, isTop bool) bool {
	for _, pad := range c.Footprint.Pads {
		if pad.Layer < 0 {
			return isTop
		}
		return (pad.Layer == 0) == isTop
	}
	return isTop
}

// -------------------------------------------------------------- netlist

// odbNetlist renders the netlist: one net per entry, nodes referencing
// component pads (via the net name of each pad).
func odbNetlist(b *domainlayout.Board) string {
	// Collecte des noeuds (ref, pin) par net.
	nodes := map[string][][]string{} // net -> [[ref, pin], ...]
	nets := []string{}
	seen := map[string]bool{}
	for i := range b.Components {
		c := &b.Components[i]
		for _, pad := range c.Footprint.Pads {
			if pad.Net == "" {
				continue
			}
			if !seen[pad.Net] {
				seen[pad.Net] = true
				nets = append(nets, pad.Net)
			}
			nodes[pad.Net] = append(nodes[pad.Net], []string{c.Ref, pad.Name})
		}
	}
	// Les pistes/vias routés portent aussi des nets : ajouter les nets
	// sans pastille (le routeur IA peut créer des pistes sur un net dont
	// les composants n'existent plus dans la carte).
	for i := range b.Tracks {
		n := b.Tracks[i].Net
		if n != "" && !seen[n] {
			seen[n] = true
			nets = append(nets, n)
		}
	}

	sort.Strings(nets)
	var sb strings.Builder
	sb.WriteString("# YahriaCad ODB++ (simplifie) — netlist\n")
	sb.WriteString("UNIT {\n")
	for _, net := range nets {
		fmt.Fprintf(&sb, "\tNET '%s' {\n", net)
		fmt.Fprintf(&sb, "\t\tSUBNET '%s' {\n", net)
		for _, node := range nodes[net] {
			fmt.Fprintf(&sb, "\t\t\tNODE { COMP '%s' PIN '%s' }\n", node[0], node[1])
		}
		sb.WriteString("\t\t}\n")
		sb.WriteString("\t}\n")
	}
	sb.WriteString("}\n")
	return sb.String()
}
