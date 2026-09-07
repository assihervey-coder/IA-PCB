// Package writer implements the physical output adapters: RS-274X Gerber
// generation and STEP AP214 3D models. Both work directly on the layout
// domain aggregate and satisfy the export application ports.
package writer

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"

	domainlayout "github.com/kidcad/kidcad-pro-ia/backend/internal/domain/layout"
	"github.com/kidcad/kidcad-pro-ia/backend/internal/pkg/geometry"
)

// GerberWriter produces RS-274X Gerber files (contracts.md §5 naming).
// The generator writes one file per copper layer, the solder masks, the
// silkscreens and the board outline; it is intentionally conservative
// (linear D01 draws only, rectangular/circular flashes) so every CAM
// toolchain can read it.
type GerberWriter struct{}

// NewGerberWriter builds the RS-274X generator.
func NewGerberWriter() *GerberWriter { return &GerberWriter{} }

// gerberScale is the 4.6 coordinate format multiplier (FSLAX46Y46): all
// coordinates are emitted as signed integers in units of 1e-6 mm.
const gerberScale = 1e6

// padFlash is one pad instance with its absolute board position.
type padFlash struct {
	Ref           string
	Pad           string
	X, Y          float64
	Shape         domainlayout.PadShape
	Width, Height float64
}

// apertureSet assigns D-codes (>= 10) to the shapes used by a file. Track
// widths come first (ascending), pad shapes next, so the copper files are
// readable in every viewer.
type apertureSet struct {
	order []string       // clés dans l'ordre d'attribution
	codes map[string]int // clé -> D-code
	defs  []string       // lignes %ADDxx,...*%
}

// Write generates the Gerber set of the board inside outDir and returns the
// produced file paths.
func (w *GerberWriter) Write(b *domainlayout.Board, projectName, outDir string) ([]string, error) {
	if b == nil {
		return nil, fmt.Errorf("gerber : carte absente")
	}
	if b.LayerCount < 1 {
		return nil, fmt.Errorf("gerber : nombre de couches invalide (%d)", b.LayerCount)
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return nil, fmt.Errorf("gerber : répertoire de sortie : %w", err)
	}

	slug := slugifyName(projectName)
	layerCount := b.LayerCount

	// Pré-calcul des pastilles par couche (le cuivre et les masques
	// partagent la même sélection).
	padsByLayer := w.padsByLayer(b)

	var files []string
	writeFile := func(name, content string) error {
		path := filepath.Join(outDir, name)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			return fmt.Errorf("gerber : écriture de %s : %w", name, err)
		}
		files = append(files, path)
		return nil
	}

	// Couches cuivre.
	for idx := 0; idx < layerCount; idx++ {
		content, err := w.copperLayer(b, idx, padsByLayer[idx])
		if err != nil {
			return nil, err
		}
		if err := writeFile(fmt.Sprintf("%s-%s.gbr", slug, copperFileTag(idx, layerCount)), content); err != nil {
			return nil, err
		}
	}

	// Masques de soudure (flash des pastilles, mêmes formes).
	for _, side := range []struct {
		file  string
		layer int
	}{
		{"F_Mask", 0},
		{"B_Mask", layerCount - 1},
	} {
		content := w.maskLayer(b, side.layer, padsByLayer[side.layer])
		if err := writeFile(fmt.Sprintf("%s-%s.gbr", slug, side.file), content); err != nil {
			return nil, err
		}
	}

	// Regroupement des composants par face.
	topComps, bottomComps := splitComponentsBySide(b)

	// Sérigraphies (contours des composants).
	for _, side := range []struct {
		file  string
		comps []int
	}{
		{"F_Silkscreen", topComps},
		{"B_Silkscreen", bottomComps},
	} {
		content := w.silkscreenLayer(b, side.comps)
		if err := writeFile(fmt.Sprintf("%s-%s.gbr", slug, side.file), content); err != nil {
			return nil, err
		}
	}

	// Contour de carte.
	content := w.edgeLayer(b)
	if err := writeFile(fmt.Sprintf("%s-Edge_Cuts.gbr", slug), content); err != nil {
		return nil, err
	}
	return files, nil
}

// padsByLayer returns, for every copper layer index, the pads present on it
// with their absolute position (through-hole pads appear on every layer).
func (w *GerberWriter) padsByLayer(b *domainlayout.Board) map[int][]padFlash {
	out := make(map[int][]padFlash)
	for idx := 0; idx < b.LayerCount; idx++ {
		out[idx] = []padFlash{}
	}
	for i := range b.Components {
		c := &b.Components[i]
		for _, pad := range c.Footprint.Pads {
			pos := c.PadAbsolutePosition(pad)
			shape := pad.Shape
			width, height := pad.Width, pad.Height
			if shape == domainlayout.ShapeRect && isQuarterTurn(pad.Rotation) {
				width, height = height, width
			}
			if shape == "" {
				shape = domainlayout.ShapeRoundRect
			}
			flash := padFlash{
				Ref: c.Ref, Pad: pad.Name,
				X: pos.X, Y: pos.Y,
				Shape: shape, Width: width, Height: height,
			}
			if pad.Layer < 0 {
				for idx := 0; idx < b.LayerCount; idx++ {
					out[idx] = append(out[idx], flash)
				}
				continue
			}
			if pad.Layer >= 0 && pad.Layer < b.LayerCount {
				out[pad.Layer] = append(out[pad.Layer], flash)
			}
		}
	}
	return out
}

func newApertureSet() *apertureSet {
	return &apertureSet{codes: map[string]int{}}
}

func (a *apertureSet) add(key, def string) int {
	if code, ok := a.codes[key]; ok {
		return code
	}
	code := 10 + len(a.order)
	a.codes[key] = code
	a.order = append(a.order, key)
	a.defs = append(a.defs, fmt.Sprintf("%%ADD%d%s*%%", code, def))
	return code
}

func (a *apertureSet) addCircle(diameter float64) int {
	return a.add(fmt.Sprintf("C:%.6f", diameter), fmt.Sprintf("C,%.6f", diameter))
}

func (a *apertureSet) addRect(w, h float64) int {
	return a.add(fmt.Sprintf("R:%.6fx%.6f", w, h), fmt.Sprintf("R,%.6fX%.6f", w, h))
}

func (a *apertureSet) addTrack(width float64) int {
	return a.add(fmt.Sprintf("T:%.6f", width), fmt.Sprintf("C,%.6f", width))
}

func (a *apertureSet) header() string {
	var sb strings.Builder
	for _, def := range a.defs {
		sb.WriteString(def)
		sb.WriteByte('\n')
	}
	return sb.String()
}

// copperLayer renders one copper layer: apertures, tracks (D01) and pad
// copper (D03 flashes).
func (w *GerberWriter) copperLayer(b *domainlayout.Board, idx int, pads []padFlash) (string, error) {
	tracks := tracksForLayer(b, idx)

	widths := distinctTrackWidths(tracks)
	sort.Float64s(widths)

	ap := newApertureSet()
	for _, width := range widths {
		ap.addTrack(width)
	}
	type flashedPad struct {
		flash padFlash
		code  int
	}
	flashed := make([]flashedPad, 0, len(pads))
	for _, p := range pads {
		var code int
		if p.Shape == domainlayout.ShapeCircle {
			code = ap.addCircle(p.Width)
		} else {
			code = ap.addRect(p.Width, p.Height)
		}
		flashed = append(flashed, flashedPad{flash: p, code: code})
	}

	var sb strings.Builder
	writeHeader(&sb, "Copper "+copperFileTag(idx, b.LayerCount))
	sb.WriteString(ap.header())
	sb.WriteString("G01*\n")

	for _, t := range tracks {
		if len(t.Points) == 0 {
			continue
		}
		fmt.Fprintf(&sb, "G04 net %s*\n", sanitizeComment(t.Net))
		fmt.Fprintf(&sb, "D%d*\n", ap.addTrack(trackWidth(t)))
		for i, pt := range t.Points {
			if i == 0 {
				sb.WriteString(moveTo(pt.X, pt.Y))
				continue
			}
			sb.WriteString(drawTo(pt.X, pt.Y))
		}
	}

	for _, f := range flashed {
		fmt.Fprintf(&sb, "G04 pad %s.%s*\n", sanitizeComment(f.flash.Ref), sanitizeComment(f.flash.Pad))
		fmt.Fprintf(&sb, "D%d*\n", f.code)
		sb.WriteString(flashAt(f.flash.X, f.flash.Y))
	}

	sb.WriteString("M02*\n")
	return sb.String(), nil
}

// maskLayer renders the solder mask of one side (pad flashes only).
func (w *GerberWriter) maskLayer(b *domainlayout.Board, idx int, pads []padFlash) string {
	ap := newApertureSet()
	type entry struct {
		flash padFlash
		code  int
	}
	entries := make([]entry, 0, len(pads))
	for _, p := range pads {
		var code int
		if p.Shape == domainlayout.ShapeCircle {
			code = ap.addCircle(p.Width)
		} else {
			code = ap.addRect(p.Width, p.Height)
		}
		entries = append(entries, entry{flash: p, code: code})
	}

	var sb strings.Builder
	writeHeader(&sb, "Solder mask")
	sb.WriteString(ap.header())
	for _, e := range entries {
		fmt.Fprintf(&sb, "D%d*\n", e.code)
		sb.WriteString(flashAt(e.flash.X, e.flash.Y))
	}
	sb.WriteString("M02*\n")
	return sb.String()
}

// silkscreenLayer renders the component body outlines of one side.
func (w *GerberWriter) silkscreenLayer(b *domainlayout.Board, comps []int) string {
	ap := newApertureSet()
	outline := ap.addTrack(0.1)

	var sb strings.Builder
	writeHeader(&sb, "Silkscreen")
	sb.WriteString(ap.header())
	sb.WriteString("G01*\n")

	for _, i := range comps {
		c := &b.Components[i]
		fp := &c.Footprint
		hw := fp.BodyWidthMM / 2
		hh := fp.BodyHeightMM / 2
		if hw <= geometry.Epsilon || hh <= geometry.Epsilon {
			continue
		}
		corners := []geometry.Point{
			{X: -hw, Y: -hh}, {X: hw, Y: -hh}, {X: hw, Y: hh}, {X: -hw, Y: hh},
		}
		fmt.Fprintf(&sb, "G04 refdes %s*\n", sanitizeComment(c.Ref))
		fmt.Fprintf(&sb, "D%d*\n", outline)
		for j, corner := range corners {
			rot := geometry.RotatePoint(corner, geometry.Point{}, c.Rotation)
			x, y := c.X+rot.X, c.Y+rot.Y
			if j == 0 {
				sb.WriteString(moveTo(x, y))
				continue
			}
			sb.WriteString(drawTo(x, y))
		}
		// Fermeture du rectangle.
		first := corners[0]
		rot := geometry.RotatePoint(first, geometry.Point{}, c.Rotation)
		sb.WriteString(drawTo(c.X+rot.X, c.Y+rot.Y))
	}
	sb.WriteString("M02*\n")
	return sb.String()
}

// edgeLayer renders the board outline as a filled region (G36/G37).
func (w *GerberWriter) edgeLayer(b *domainlayout.Board) string {
	var sb strings.Builder
	writeHeader(&sb, "Board outline (Edge.Cuts)")
	sb.WriteString("G36*\n")
	sb.WriteString(moveTo(0, 0))
	sb.WriteString(drawTo(b.WidthMM, 0))
	sb.WriteString(drawTo(b.WidthMM, b.HeightMM))
	sb.WriteString(drawTo(0, b.HeightMM))
	sb.WriteString(drawTo(0, 0))
	sb.WriteString("G37*\n")
	sb.WriteString("M02*\n")
	return sb.String()
}

// --------------------------------------------------------------
// Helpers
// --------------------------------------------------------------

// padFlash grouping helpers.
func splitComponentsBySide(b *domainlayout.Board) (top, bottom []int) {
	for i := range b.Components {
		c := &b.Components[i]
		bottomOnly := len(c.Footprint.Pads) > 0
		for _, pad := range c.Footprint.Pads {
			if pad.Layer <= 0 { // couche 0 ou traversant : face supérieure
				bottomOnly = false
				break
			}
		}
		if bottomOnly {
			bottom = append(bottom, i)
			continue
		}
		top = append(top, i)
	}
	return top, bottom
}

// copperFileTag returns the Gerber file tag of a copper layer index:
// F_Cu, In1_Cu, ..., B_Cu (contracts.md §5).
func copperFileTag(idx, layerCount int) string {
	switch {
	case idx == 0:
		return "F_Cu"
	case idx == layerCount-1 && layerCount > 1:
		return "B_Cu"
	default:
		return fmt.Sprintf("In%d_Cu", idx)
	}
}

// writeHeader emits the mandatory Gerber prologue (contracts.md §5).
func writeHeader(sb *strings.Builder, comment string) {
	sb.WriteString("G04 KidCAD-Pro-IA Gerber Export*\n")
	sb.WriteString(fmt.Sprintf("G04 %s*\n", sanitizeComment(comment)))
	sb.WriteString("%FSLAX46Y46*%\n")
	sb.WriteString("%MOMM*%\n")
	sb.WriteString("%LPD*%\n")
}

// moveTo emits a D02 (pen up) command at (x, y) mm.
func moveTo(x, y float64) string {
	return fmt.Sprintf("X%dY%dD02*\n", gerberCoord(x), gerberCoord(y))
}

// drawTo emits a D01 (linear draw) command to (x, y) mm.
func drawTo(x, y float64) string {
	return fmt.Sprintf("X%dY%dD01*\n", gerberCoord(x), gerberCoord(y))
}

// flashAt emits a D03 (aperture flash) command at (x, y) mm.
func flashAt(x, y float64) string {
	return fmt.Sprintf("X%dY%dD03*\n", gerberCoord(x), gerberCoord(y))
}

// gerberCoord converts millimeters to the 4.6 signed integer format.
func gerberCoord(mm float64) int64 {
	return int64(math.Round(mm * gerberScale))
}

// sanitizeComment strips the Gerber comment terminator so a net or refdes
// name cannot break out of a G04 comment.
func sanitizeComment(s string) string {
	repl := strings.NewReplacer("*", "_", "%", "_", "\n", " ", "\r", " ")
	return repl.Replace(s)
}

// slugifyName converts a project name into a filesystem-friendly slug.
func slugifyName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	var b strings.Builder
	lastDash := true
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		default:
			if !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		out = "projet"
	}
	return out
}

// distinctTrackWidths returns the sorted distinct widths of a track list.
func distinctTrackWidths(tracks []domainlayout.Track) []float64 {
	seen := map[float64]bool{}
	var out []float64
	for _, t := range tracks {
		width := trackWidth(t)
		if width <= 0 || seen[width] {
			continue
		}
		seen[width] = true
		out = append(out, width)
	}
	sort.Float64s(out)
	return out
}

// trackWidth returns the width of a track, defaulting to 0.25 mm when
// undefined.
func trackWidth(t domainlayout.Track) float64 {
	if t.Width > 0 {
		return t.Width
	}
	return 0.25
}

// isQuarterTurn reports whether a rotation is an odd multiple of 90°
// (mod 180°), allowing the axis-aligned swap of rectangular pad dimensions.
func isQuarterTurn(deg float64) bool {
	norm := math.Mod(math.Abs(deg), 180)
	const eps = 1e-6
	return math.Abs(norm-90) < eps
}

// tracksForLayer filters the board tracks belonging to one copper layer.
func tracksForLayer(b *domainlayout.Board, idx int) []domainlayout.Track {
	out := make([]domainlayout.Track, 0)
	for _, t := range b.Tracks {
		if t.Layer == idx {
			out = append(out, t)
		}
	}
	return out
}
