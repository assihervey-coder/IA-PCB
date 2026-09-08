package reader

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	schematicapp "github.com/assihervey-coder/IA-PCB/backend/internal/application/schematic"
	domainconstraints "github.com/assihervey-coder/IA-PCB/backend/internal/domain/constraints"
	domainlayout "github.com/assihervey-coder/IA-PCB/backend/internal/domain/layout"
	domainschematic "github.com/assihervey-coder/IA-PCB/backend/internal/domain/schematic"
)

// --------------------------------------------------------------
// Tokenizer + parser s-expression (subset KiCad)
// --------------------------------------------------------------

// sNode is one node of the s-expression tree. A list node keeps its direct
// atoms (the first one being the tag) and its direct sub-lists; a bare atom
// is not represented outside of the atoms slice of its parent.
type sNode struct {
	atoms    []string
	children []*sNode
}

// tag returns the tag of a list node ("segment", "pad", ...) or "".
func (n *sNode) tag() string {
	if n == nil || len(n.atoms) == 0 {
		return ""
	}
	return n.atoms[0]
}

// args returns the atoms following the tag.
func (n *sNode) args() []string {
	if n == nil || len(n.atoms) <= 1 {
		return nil
	}
	return n.atoms[1:]
}

// arg returns the i-th atom after the tag, or "" when missing.
func (n *sNode) arg(i int) string {
	args := n.args()
	if i < 0 || i >= len(args) {
		return ""
	}
	return args[i]
}

// child returns the first direct sub-list carrying the given tag.
func (n *sNode) child(name string) *sNode {
	for _, c := range n.children {
		if c.tag() == name {
			return c
		}
	}
	return nil
}

// childrenWithTag returns every direct sub-list carrying the given tag.
func (n *sNode) childrenWithTag(name string) []*sNode {
	var out []*sNode
	for _, c := range n.children {
		if c.tag() == name {
			out = append(out, c)
		}
	}
	return out
}

// hasAtomIn reports whether one of the direct atoms after the tag equals v.
func (n *sNode) hasAtomIn(v string) bool {
	for _, a := range n.args() {
		if a == v {
			return true
		}
	}
	return false
}

// tokenizeSexpr splits an s-expression payload into "(", ")" and atom
// tokens (quoted strings are unescaped).
func tokenizeSexpr(data []byte) ([]string, error) {
	var toks []string
	i := 0
	for i < len(data) {
		c := data[i]
		switch {
		case c == '(' || c == ')':
			toks = append(toks, string(c))
			i++
		case c == '"':
			s, next, err := readQuoted(data, i)
			if err != nil {
				return nil, err
			}
			toks = append(toks, s)
			i = next
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
		default:
			j := i
			for j < len(data) && data[j] != '(' && data[j] != ')' &&
				data[j] != '"' && data[j] != ' ' && data[j] != '\t' &&
				data[j] != '\n' && data[j] != '\r' {
				j++
			}
			toks = append(toks, string(data[i:j]))
			i = j
		}
	}
	return toks, nil
}

// readQuoted reads a double-quoted atom starting at position start,
// resolving the standard backslash escapes.
func readQuoted(data []byte, start int) (string, int, error) {
	var sb strings.Builder
	i := start + 1
	for i < len(data) {
		c := data[i]
		switch c {
		case '"':
			return sb.String(), i + 1, nil
		case '\\':
			if i+1 >= len(data) {
				return "", 0, fmt.Errorf("s-expression : échappement tronqué")
			}
			switch data[i+1] {
			case 'n':
				sb.WriteByte('\n')
			case 't':
				sb.WriteByte('\t')
			case 'r':
				sb.WriteByte('\r')
			default:
				sb.WriteByte(data[i+1])
			}
			i += 2
		default:
			sb.WriteByte(c)
			i++
		}
	}
	return "", 0, fmt.Errorf("s-expression : chaîne non terminée")
}

// parseSexpr parses a payload into a virtual root node holding every
// top-level list (and every loose atom) as children.
func parseSexpr(data []byte) (*sNode, error) {
	toks, err := tokenizeSexpr(data)
	if err != nil {
		return nil, err
	}
	root := &sNode{}
	pos := 0
	for pos < len(toks) {
		node, next, err := parseNode(toks, pos)
		if err != nil {
			return nil, err
		}
		if node != nil {
			root.children = append(root.children, node)
		}
		pos = next
	}
	if len(root.children) == 0 {
		return nil, fmt.Errorf("s-expression : fichier vide")
	}
	return root, nil
}

// parseNode parses one node starting at pos and returns the next position.
func parseNode(toks []string, pos int) (*sNode, int, error) {
	if pos >= len(toks) {
		return nil, pos, fmt.Errorf("s-expression : fin inattendue")
	}
	if toks[pos] == ")" {
		return nil, pos + 1, fmt.Errorf("s-expression : parenthèse fermante inattendue")
	}
	if toks[pos] != "(" {
		// Atome isolé : représenté comme une liste dégénérée d'un seul atome,
		// ignorée par les lecteurs (les atomes sont portés par leur parent).
		return &sNode{atoms: []string{toks[pos]}}, pos + 1, nil
	}
	pos++
	node := &sNode{}
	for pos < len(toks) {
		switch toks[pos] {
		case ")":
			return node, pos + 1, nil
		case "(":
			child, next, err := parseNode(toks, pos)
			if err != nil {
				return nil, pos, err
			}
			node.children = append(node.children, child)
			pos = next
		default:
			node.atoms = append(node.atoms, toks[pos])
			pos++
		}
	}
	return nil, pos, fmt.Errorf("s-expression : parenthèses déséquilibrées")
}

// sexprFloat parses a numeric atom with a zero fallback.
func sexprFloat(s string) float64 {
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	return v
}

// sexprInt parses an integer atom with a zero fallback.
func sexprInt(s string) int {
	v, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return 0
	}
	return v
}

// --------------------------------------------------------------
// Conventions de couches KiCad
// --------------------------------------------------------------

// kicadLayerIndex maps a KiCad layer name onto a copper layer index
// (0 = F.Cu, layerCount-1 = B.Cu, InN.Cu = N).
func kicadLayerIndex(name string, layerCount int) int {
	switch {
	case name == "F.Cu" || name == "Top":
		return 0
	case name == "B.Cu" || name == "Bottom":
		if layerCount <= 1 {
			return 0
		}
		return layerCount - 1
	case strings.HasPrefix(name, "In") && strings.HasSuffix(name, ".Cu"):
		if n, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(name, "In"), ".Cu")); err == nil {
			if n > 0 && n < layerCount-1 {
				return n
			}
		}
		return 0
	default:
		return 0
	}
}

// padShapeFromKicad maps a KiCad pad shape onto the domain enum.
func padShapeFromKicad(shape string) string {
	switch strings.ToLower(strings.TrimSpace(shape)) {
	case "circle":
		return "circle"
	case "oval", "ellipse":
		return "oval"
	case "roundrect", "round_rect":
		return "roundrect"
	case "rect":
		return "rect"
	default:
		return "rect"
	}
}

// stripLibraryPrefix removes the "Lib:" prefix of a footprint name.
func stripLibraryPrefix(name string) string {
	if idx := strings.LastIndex(name, ":"); idx >= 0 && idx < len(name)-1 {
		return name[idx+1:]
	}
	return name
}

// --------------------------------------------------------------
// Lecteur KiCad PCB (.kicad_pcb) — versions 5 à 10
// --------------------------------------------------------------

// kicadVersionTokens mappe le jeton (version NNNNNNNN) de l'en-tête
// .kicad_pcb vers la version majeure de KiCad correspondante :
//   - 20171130            : KiCad 5 (empreintes "module", nets numérotés)
//   - 20211030/20211230   : KiCad 6 (footprint, propriétés, nets numérotés)
//   - 20221206            : KiCad 7 (Reference/Value en "property")
//   - 20240108            : KiCad 8 (tables, images embarquées — ignorées)
//   - 20241229            : KiCad 9 (embedded_fonts, channels — ignorés)
//   - 20260206            : KiCad 10 (nets référencés PAR NOM, sans
//     déclaration racine ; tél. legacy_teardrops)
var kicadVersionTokens = map[int]string{
	20171130: "5",
	20211030: "6",
	20211230: "6",
	20221206: "7",
	20240108: "8",
	20241229: "9",
	20260206: "10",
}

// kicadFileVersion est la version détectée dans l'en-tête du fichier.
type kicadFileVersion struct {
	token     int    // jeton (version NNNNNNNN)
	generator string // (generator_version "10.0") si présent
}

// readKicadFileVersion extrait la version de l'en-tête (tolérant : un
// en-tête absent ou partiel donne une structure partiellement remplie).
func readKicadFileVersion(pcb *sNode) kicadFileVersion {
	v := kicadFileVersion{}
	if n := pcb.child("version"); n != nil {
		v.token = sexprInt(n.arg(0))
	}
	if n := pcb.child("generator_version"); n != nil {
		v.generator = n.arg(0)
	}
	return v
}

// label renvoie une étiquette lisible : "KiCad 10", "KiCad 5", ou une
// mention explicite quand la version n'est pas reconnue (fichier d'une
// version future ou antérieure aux jetons connus).
func (v kicadFileVersion) label() string {
	if major, ok := kicadVersionTokens[v.token]; ok {
		if major == "5" {
			return "KiCad 5 (legacy)"
		}
		return "KiCad " + major
	}
	if v.token > 0 {
		return fmt.Sprintf("KiCad (version %d non reconnue)", v.token)
	}
	return "KiCad (version absente)"
}

// known reports whether the version token maps onto a supported KiCad major.
func (v kicadFileVersion) known() bool {
	_, ok := kicadVersionTokens[v.token]
	return ok
}

// kicadParseState accumulates the pieces collected during the tree walk.
type kicadParseState struct {
	layerCount int
	netNames   map[int]string // numéro -> nom
	netOrder   []int          // numéros rencontrés, dans l'ordre
	netUsed    map[int]bool   // numéros référencés par un objet cuivre
	netIDs     map[string]int // KiCad 10 : nom -> numéro synthétique
	nextNetID  int            // compteur des numéros synthétiques
	conns      map[int][]schematicPinRef
	footprints []kicadFootprint
	tracks     []domainlayout.Track
	vias       []domainlayout.Via
	warnings   []string
}

// netSyntheticBase : base des numéros attribués aux nets "par nom" de
// KiCad 10, hors de portée des numéros explicites (petits entiers KiCad <= 9).
const netSyntheticBase = 100000

// schematicPinRef is the reader-local (component, pin) pair.
type schematicPinRef struct {
	ref string
	pin string
}

// geometryAABB is a minimal bounding box used for the board outline.
type geometryAABB struct {
	MinX, MinY, MaxX, MaxY float64
}

func (b *geometryAABB) unionPoint(x, y float64) {
	b.MinX = math.Min(b.MinX, x)
	b.MinY = math.Min(b.MinY, y)
	b.MaxX = math.Max(b.MaxX, x)
	b.MaxY = math.Max(b.MaxY, y)
}

func (b *geometryAABB) union(o geometryAABB) {
	b.MinX = math.Min(b.MinX, o.MinX)
	b.MinY = math.Min(b.MinY, o.MinY)
	b.MaxX = math.Max(b.MaxX, o.MaxX)
	b.MaxY = math.Max(b.MaxY, o.MaxY)
}

// kicadFootprint pairs the schematic symbol with its physical footprint.
type kicadFootprint struct {
	ref       string
	value     string
	name      string
	x, y, rot float64
	front     bool
	pads      []domainlayout.Pad
}

// readKiCadPCB parses a .kicad_pcb file (contract §7 subset) into the
// schematic + board pair.
func readKiCadPCB(data []byte, path string) (schematicapp.ImportResult, error) {
	root, err := parseSexpr(data)
	if err != nil {
		return schematicapp.ImportResult{}, fmt.Errorf("kicad %s : %w", path, err)
	}
	pcb := root.child("kicad_pcb")
	if pcb == nil {
		return schematicapp.ImportResult{}, fmt.Errorf(
			"kicad %s : racine (kicad_pcb ...) introuvable", path)
	}

	st := &kicadParseState{
		layerCount: 2,
		netNames:   map[int]string{},
		netUsed:    map[int]bool{},
		netIDs:     map[string]int{},
		nextNetID:  netSyntheticBase,
		conns:      map[int][]schematicPinRef{},
	}

	// Version du fichier : jeton (version NNNNNNNN) + generator_version.
	// Une version future (au-delà de KiCad 10) est acceptée en lecture
	// best-effort avec un avertissement explicite.
	fileVersion := readKicadFileVersion(pcb)
	if !fileVersion.known() && fileVersion.token > 0 {
		st.warnings = append(st.warnings, fmt.Sprintf(
			"version de fichier %s : lecture best-effort (versions supportées : KiCad 6 à 10)",
			fileVersion.label()))
	}

	// Nombre de couches cuivre : entrées "xxx.Cu" du bloc (layers ...).
	if layers := pcb.child("layers"); layers != nil {
		cu := 0
		for _, l := range layers.children {
			name := l.arg(0)
			if strings.HasSuffix(name, ".Cu") {
				cu++
			}
		}
		if cu >= 1 && cu <= 34 {
			st.layerCount = cu
		}
	}

	// Déclarations : (net N "NAME") — KiCad <= 9 — ou (net "NAME") —
	// KiCad 10 (version 20260206), qui ne déclare plus les nets au niveau
	// racine ; ils sont alors synthétisés depuis les références cuivre.
	for _, n := range pcb.childrenWithTag("net") {
		if num, name, ok := st.resolveNetRef(n); ok && num > 0 && name != "" {
			st.registerNet(num, name)
		}
	}

	// Empreintes : (footprint ...) depuis KiCad 6, (module ...) en KiCad 5.
	footprints := pcb.childrenWithTag("footprint")
	footprints = append(footprints, pcb.childrenWithTag("module")...)
	for _, fp := range footprints {
		st.readFootprint(fp)
	}
	for _, seg := range pcb.childrenWithTag("segment") {
		st.readSegment(seg)
	}
	for _, via := range pcb.childrenWithTag("via") {
		st.readVia(via)
	}

	width, height := st.boardOutline(pcb)
	board, err := domainlayout.NewBoard(width, height, st.layerCount)
	if err != nil {
		return schematicapp.ImportResult{}, fmt.Errorf("kicad %s : %w", path, err)
	}

	// Empreintes -> composants placés.
	for i := range st.footprints {
		kf := &st.footprints[i]
		bodyW, bodyH := kicadBodySize(kf.pads)
		pl := domainlayout.PlacedComponent{
			Ref: kf.ref,
			Footprint: domainlayout.Footprint{
				Name:         kf.name,
				Value:        kf.value,
				Pads:         kf.pads,
				BodyWidthMM:  bodyW,
				BodyHeightMM: bodyH,
				HeightMM:     domainlayout.DefaultBoardHeightMM,
			},
			X:        kf.x,
			Y:        kf.y,
			Rotation: kf.rot,
		}
		if err := board.AddComponent(pl); err != nil {
			st.warnings = append(st.warnings, fmt.Sprintf("empreinte ignorée : %v", err))
			continue
		}
	}

	// Pistes et vias — fusion cosmétique des segments contigus/colinéaires
	// en pistes multi-points (géométrie inchangée, jonctions préservées).
	st.tracks = mergeTracks(st.tracks)
	for _, t := range st.tracks {
		board.AddTrack(t)
	}
	for _, v := range st.vias {
		if err := board.AddVia(v); err != nil {
			st.warnings = append(st.warnings, fmt.Sprintf("via ignoré : %v", err))
		}
	}

	// Schéma : composants + nets reconstitués depuis les pastilles.
	sch := domainschematic.New(schematicBaseName(path, "kicad"))
	for i := range st.footprints {
		kf := &st.footprints[i]
		if err := sch.AddComponent(domainschematic.Component{
			Ref:       kf.ref,
			Value:     kf.value,
			Footprint: kf.name,
			X:         kf.x,
			Y:         kf.y,
			Rotation:  kf.rot,
		}); err != nil {
			st.warnings = append(st.warnings, fmt.Sprintf("composant ignoré : %v", err))
		}
	}
	for _, num := range st.netOrder {
		if !st.netUsed[num] {
			continue
		}
		name := st.netNames[num]
		if name == "" {
			name = fmt.Sprintf("NET_%d", num)
		}
		net := domainschematic.Net{Name: name, Class: domainschematic.ClassDefault}
		for _, conn := range st.conns[num] {
			net.Connections = append(net.Connections, domainschematic.PinRef{
				ComponentRef: conn.ref, PinNumber: conn.pin,
			})
		}
		if err := sch.AddNet(net); err != nil {
			st.warnings = append(st.warnings, fmt.Sprintf("net ignoré : %v", err))
		}
	}

	res := schematicapp.ImportResult{
		Format:      "kicad",
		FileVersion: fileVersion.label(),
		Schematic:   sch,
		Board:       board,
		Constraints: domainconstraints.NewDefault(),
		Warnings:    st.warnings,
	}
	return res, nil
}

// readFootprint parses one (footprint ...) node.
func (st *kicadParseState) readFootprint(fp *sNode) {
	kf := kicadFootprint{
		name:  stripLibraryPrefix(fp.arg(0)),
		front: true,
	}

	if layer := fp.child("layer"); layer != nil {
		kf.front = layer.arg(0) != "B.Cu"
	}
	if at := fp.child("at"); at != nil {
		kf.x = sexprFloat(at.arg(0))
		kf.y = sexprFloat(at.arg(1))
		if at.arg(2) != "" {
			kf.rot = sexprFloat(at.arg(2))
		}
	}

	ref := fmt.Sprintf("FP%d", len(st.footprints)+1)
	value := ""
	haveRef, haveValue := false, false
	// pcbnew ≥ 7 stocke référence et valeur dans des (property "Reference" /
	// "Value" …) ; pcbnew ≤ 6 utilisait (fp_text reference/value …). Les
	// deux formes peuvent cohabiter (KiCad 10) : la property, plus
	// récente, est prioritaire — fp_text ne fait que compléter.
	for _, prop := range fp.childrenWithTag("property") {
		switch prop.arg(0) {
		case "Reference":
			if v := prop.arg(1); v != "" {
				ref, haveRef = v, true
			}
		case "Value":
			if v := prop.arg(1); v != "" {
				value, haveValue = v, true
			}
		}
	}
	for _, txt := range fp.childrenWithTag("fp_text") {
		switch txt.arg(0) {
		case "reference":
			if v := txt.arg(1); v != "" && !haveRef {
				ref = v
			}
		case "value":
			if v := txt.arg(1); !haveValue {
				value = v
			}
		}
	}
	kf.ref, kf.value = ref, value

	lastLayer := st.layerCount - 1
	for _, padNode := range fp.childrenWithTag("pad") {
		args := padNode.args()
		padName := ""
		padType := "smd"
		padShape := "rect"
		if len(args) > 0 {
			padName = args[0]
		}
		if len(args) > 1 {
			padType = args[1]
		}
		if len(args) > 2 {
			padShape = args[2]
		}

		pad := domainlayout.Pad{
			Name:  padName,
			Shape: domainlayout.PadShape(padShapeFromKicad(padShape)),
		}
		if at := padNode.child("at"); at != nil {
			pad.X = sexprFloat(at.arg(0))
			pad.Y = sexprFloat(at.arg(1))
			pad.Rotation = sexprFloat(at.arg(2))
		}
		if size := padNode.child("size"); size != nil {
			pad.Width = sexprFloat(size.arg(0))
			pad.Height = sexprFloat(size.arg(1))
		}
		if drill := padNode.child("drill"); drill != nil {
			// (drill 1.0) ou (drill oval 1.0 0.6)
			d := sexprFloat(drill.arg(0))
			if d == 0 {
				d = sexprFloat(drill.arg(1))
			}
			_ = d // le perçage est porté par le diamètre côté domaine
		}

		layersNode := padNode.child("layers")
		thru := padType == "thru_hole" || padType == "np_thru_hole"
		if layersNode != nil && layersNode.hasAtomIn("*.Cu") {
			thru = true
		}
		// Pastille SMD : la liste de couches explicite (F.Cu / B.Cu)
		// prime sur le côté de l'empreinte — pcbnew autorise une
		// pastille sur le cuivre opposé (jumpers, points de test) et
		// le round-trip doit conserver ce côté même si l'empreinte a
		// été réécrite de l'autre côté du plateau.
		explicitFront := layersNode != nil && layersNode.hasAtomIn("F.Cu") &&
			!layersNode.hasAtomIn("B.Cu")
		explicitBack := layersNode != nil && layersNode.hasAtomIn("B.Cu") &&
			!layersNode.hasAtomIn("F.Cu")
		switch {
		case thru:
			pad.Layer = -1
		case explicitBack:
			pad.Layer = lastLayer
		case explicitFront:
			pad.Layer = 0
		case !kf.front:
			pad.Layer = lastLayer
		default:
			pad.Layer = 0
		}

		// (net N "NAME") — KiCad <= 9 — ou (net "NAME") — KiCad 10 :
		// association électrique de la pastille.
		if netNode := padNode.child("net"); netNode != nil {
			if num, name, ok := st.resolveNetRef(netNode); ok && num > 0 &&
				(name != "" || st.netNames[num] != "") {
				st.registerNet(num, name)
				pad.Net = st.netName(num)
				st.netUsed[num] = true
				st.conns[num] = append(st.conns[num],
					schematicPinRef{ref: kf.ref, pin: padName})
			}
		}
		kf.pads = append(kf.pads, pad)
	}

	st.footprints = append(st.footprints, kf)
}

// netName resolves the name of a net number, with a deterministic fallback.
func (st *kicadParseState) netName(num int) string {
	if name := st.netNames[num]; name != "" {
		return name
	}
	return fmt.Sprintf("NET_%d", num)
}

// resolveNetRef interprets a (net ...) node in the two supported formats :
//   - KiCad <= 9 : (net N "NAME") — numéro explicite, nom optionnel ;
//   - KiCad >= 10 (version 20260206) : (net "NAME") — nom sans numéro.
//
// Pour le format par nom seul, un numéro synthétique stable (>=
// netSyntheticBase) est attribué à la première rencontre, si bien que la
// suite du lecteur (netOrder, netUsed, conns) continue de travailler sur
// des entiers. ok=false pour un nœud vide ou un nom vide.
func (st *kicadParseState) resolveNetRef(node *sNode) (int, string, bool) {
	args := node.args()
	if len(args) == 0 {
		return 0, "", false
	}
	if num, err := strconv.Atoi(args[0]); err == nil {
		name := ""
		if len(args) > 1 {
			name = args[1]
		}
		return num, name, true
	}
	name := args[0]
	if name == "" {
		return 0, "", false
	}
	num, seen := st.netIDs[name]
	if !seen {
		st.nextNetID++
		num = st.nextNetID
		st.netIDs[name] = num
	}
	return num, name, true
}

// registerNet enregistre une paire (numéro, nom) : netOrder à la première
// rencontre, netNames sans effacer un nom déjà connu.
func (st *kicadParseState) registerNet(num int, name string) {
	if num <= 0 {
		return
	}
	if _, seen := st.netNames[num]; !seen {
		st.netOrder = append(st.netOrder, num)
	}
	if name != "" {
		st.netNames[num] = name
	}
}

// readSegment parses one (segment (start) (end) (width) (layer) (net)) node.
func (st *kicadParseState) readSegment(seg *sNode) {
	start := seg.child("start")
	end := seg.child("end")
	if start == nil || end == nil {
		return
	}
	track := domainlayout.Track{
		Width: 0.25,
		Layer: 0,
		Points: []domainlayout.TrackPoint{
			{X: sexprFloat(start.arg(0)), Y: sexprFloat(start.arg(1))},
			{X: sexprFloat(end.arg(0)), Y: sexprFloat(end.arg(1))},
		},
	}
	if w := seg.child("width"); w != nil {
		track.Width = sexprFloat(w.arg(0))
	}
	if layer := seg.child("layer"); layer != nil {
		track.Layer = kicadLayerIndex(layer.arg(0), st.layerCount)
	}
	if net := seg.child("net"); net != nil {
		if num, _, ok := st.resolveNetRef(net); ok && num > 0 {
			track.Net = st.netName(num)
			st.netUsed[num] = true
		}
	}
	st.tracks = append(st.tracks, track)
}

// readVia parses one (via (at) (size) (drill) (layers) (net)) node.
func (st *kicadParseState) readVia(via *sNode) {
	at := via.child("at")
	if at == nil {
		return
	}
	diameter := sexprFloat(via.arg(0)) // forme via micro/blind : diamètre direct
	if size := via.child("size"); size != nil {
		diameter = sexprFloat(size.arg(0))
	}
	drill := 0.0
	if d := via.child("drill"); d != nil {
		drill = sexprFloat(d.arg(0))
	}
	if drill <= 0 {
		drill = diameter / 2
	}
	if diameter <= drill {
		diameter = drill + 0.2
	}

	fromLayer, toLayer := 0, st.layerCount-1
	if layers := via.child("layers"); layers != nil && len(layers.args()) >= 2 {
		fromLayer = kicadLayerIndex(layers.arg(0), st.layerCount)
		toLayer = kicadLayerIndex(layers.arg(1), st.layerCount)
		if fromLayer == toLayer {
			toLayer = st.layerCount - 1
		}
	}

	v := domainlayout.Via{
		X:         sexprFloat(at.arg(0)),
		Y:         sexprFloat(at.arg(1)),
		FromLayer: fromLayer,
		ToLayer:   toLayer,
		Diameter:  diameter,
		Drill:     drill,
	}
	if net := via.child("net"); net != nil {
		if num, _, ok := st.resolveNetRef(net); ok && num > 0 {
			v.Net = st.netName(num)
			st.netUsed[num] = true
		}
	}
	st.vias = append(st.vias, v)
}

// boardOutline derives the board size from the Edge.Cuts graphics across all
// KiCad generations : gr_rect (v5-v9 rectangles), gr_line (contours tracés au
// trait, courant dans les fichiers réels v6-v10), gr_arc, gr_circle et
// gr_poly. La boîte englobante est exacte (fidélité round-trip : un
// import → export → ré-import conserve les dimensions). Sans aucun
// graphisme exploitable, repli sur 100 x 80 mm.
func (st *kicadParseState) boardOutline(pcb *sNode) (float64, float64) {
	var box geometryAABB
	has := false
	for _, node := range pcb.children {
		tag := node.tag()
		switch tag {
		case "gr_rect", "gr_line", "gr_arc", "gr_circle", "gr_poly":
		default:
			continue
		}
		layer := node.child("layer")
		if layer == nil || layer.arg(0) != "Edge.Cuts" {
			continue
		}
		for _, p := range edgeCutPoints(tag, node) {
			if !has {
				box = geometryAABB{
					MinX: p[0], MinY: p[1],
					MaxX: p[0], MaxY: p[1],
				}
				has = true
				continue
			}
			box.unionPoint(p[0], p[1])
		}
	}
	if !has {
		return 100, 80
	}
	w := box.MaxX - box.MinX
	h := box.MaxY - box.MinY
	if w <= 0 || h <= 0 {
		return 100, 80
	}
	return w, h
}

// edgeCutPoints collects the geometry-defining points of one Edge.Cuts
// graphic node (endpoints, arc midpoints, polygon vertices, circle rim).
func edgeCutPoints(tag string, node *sNode) [][2]float64 {
	var pts [][2]float64
	add := func(n *sNode) {
		if n == nil {
			return
		}
		x, y := n.arg(0), n.arg(1)
		if x == "" || y == "" {
			return
		}
		pts = append(pts, [2]float64{sexprFloat(x), sexprFloat(y)})
	}
	switch tag {
	case "gr_rect", "gr_line":
		add(node.child("start"))
		add(node.child("end"))
	case "gr_arc":
		// v6-v7 : (start) (end) (angle ...) ; v7+ : (start) (mid) (end).
		add(node.child("start"))
		add(node.child("mid"))
		add(node.child("end"))
	case "gr_circle":
		add(node.child("center"))
		add(node.child("end"))
	case "gr_poly":
		for _, xy := range node.childrenWithTag("xy") {
			add(xy)
		}
	}
	return pts
}

// kicadBodySize derives the footprint body size from the pad bounding box,
// falling back to 2 x 2 mm.
func kicadBodySize(pads []domainlayout.Pad) (float64, float64) {
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
