// Package reader implements the design-file import adapters: KiCad
// s-expression PCB, EAGLE XML (board + schematic), Protel/Altium ASCII
// netlists, KiCad netlists and the YahriaCad interchange format. Binary Altium
// (OLE2) and Cadence Allegro files are detected and rejected with a typed,
// explicit error (documented roadmap). The Registry dispatches on file
// extension then on content sniffing.
package reader

import (
	"bytes"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	schematicapp "github.com/assihervey-coder/IA-PCB/backend/internal/application/schematic"
	domainschematic "github.com/assihervey-coder/IA-PCB/backend/internal/domain/schematic"
)

// ole2Magic is the first 4 bytes of an OLE2 Compound File (Altium .SchDoc /
// .PcbDoc containers).
var ole2Magic = []byte{0xD0, 0xCF, 0x11, 0xE0}

// Registry routes an import file to the right reader.
type Registry struct {
	log *slog.Logger
}

// NewRegistry builds the reader registry with the standard format set.
func NewRegistry(log *slog.Logger) *Registry {
	if log == nil {
		log = slog.Default()
	}
	return &Registry{log: log}
}

// Read loads the file and dispatches to the matching reader. Unknown or
// unsupported inputs yield an explicit error listing the supported formats.
func (r *Registry) Read(path string) (schematicapp.ImportResult, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return schematicapp.ImportResult{}, fmt.Errorf("lecture de %s : %w", path, err)
	}

	ext := strings.ToLower(filepath.Ext(path))
	base := strings.ToLower(filepath.Base(path))
	trimmed := bytes.TrimSpace(data)

	switch {
	case ext == ".kicad_pcb":
		return readKiCadPCB(data, path)

	case ext == ".brd":
		if isXML(trimmed) {
			return readEagle(data, path)
		}
		if isOLE2(trimmed) {
			return readAltiumBinary(path)
		}
		return readCadenceBinary(path)

	case ext == ".sch":
		if isXML(trimmed) {
			return readEagle(data, path)
		}
		return schematicapp.ImportResult{}, fmt.Errorf(
			"schéma %s : seul le format EAGLE XML (version 6+) est supporté", base)

	case ext == ".schdoc" || ext == ".pcbdoc":
		if isOLE2(trimmed) {
			return schematicapp.ImportResult{}, fmt.Errorf("%s : %w", base, ErrAltiumBinary)
		}
		return schematicapp.ImportResult{}, fmt.Errorf(
			"fichier Altium %s non reconnu : exporter une netlist Protel (.net) ou utiliser le format interchange", base)

	case ext == ".net":
		if len(trimmed) > 0 && trimmed[0] == '(' {
			return readKiCadNetlist(data, path)
		}
		if isProtelNetlist(trimmed) {
			return readProtelNetlist(data, path)
		}
		return schematicapp.ImportResult{}, fmt.Errorf(
			"netlist %s non reconnue : formats attendus : netlist KiCad (s-expression) ou Protel/Altium ASCII", base)

	case ext == ".json" || ext == ".yahriacad.json":
		if !isJSON(trimmed) {
			break
		}
		if strings.HasSuffix(base, ".yahriacad.json") {
			return readInterchange(data, path)
		}
		// Sniff : interchange d'abord, netlist JSON générique ensuite.
		if res, err := readInterchange(data, path); err == nil {
			return res, nil
		}
		return readJSONNetlist(data, path)

	default:
		// Extension inconnue : sniff du contenu.
		switch {
		case isOLE2(trimmed):
			return schematicapp.ImportResult{}, fmt.Errorf("%s : %w", base, ErrAltiumBinary)
		case len(trimmed) > 0 && trimmed[0] == '<':
			if isXML(trimmed) {
				return readEagle(data, path)
			}
		case len(trimmed) > 0 && trimmed[0] == '{':
			if res, err := readInterchange(data, path); err == nil {
				return res, nil
			}
			return readJSONNetlist(data, path)
		case isProtelNetlist(trimmed):
			return readProtelNetlist(data, path)
		case len(trimmed) > 0 && trimmed[0] == '(':
			root := parseRootTag(data)
			switch root {
			case "kicad_pcb":
				return readKiCadPCB(data, path)
			case "export":
				return readKiCadNetlist(data, path)
			}
		}
	}

	return schematicapp.ImportResult{}, fmt.Errorf(
		"format d'import non supporté (%s) ; formats acceptés : .kicad_pcb (KiCad), "+
			".brd/.sch (EAGLE XML), .net (netlist KiCad ou Protel), .yahriacad.json (interchange), "+
			".json (netlist JSON) ; Altium .SchDoc/.PcbDoc et Cadence Allegro binaires sont détectés mais non convertis",
		orb(ext, base))
}

func orb(ext, base string) string {
	if ext != "" {
		return "extension " + ext
	}
	return base
}

// isXML reports whether the payload looks like an XML document.
func isXML(trimmed []byte) bool {
	if len(trimmed) == 0 {
		return false
	}
	if bytes.HasPrefix(trimmed, []byte("<?xml")) {
		return true
	}
	return bytes.HasPrefix(trimmed, []byte("<eagle"))
}

// isJSON reports whether the payload looks like a JSON document.
func isJSON(trimmed []byte) bool {
	return len(trimmed) > 0 && (trimmed[0] == '{' || trimmed[0] == '[')
}

// isOLE2 reports whether the payload starts with the OLE2 Compound File
// signature.
func isOLE2(trimmed []byte) bool {
	return bytes.HasPrefix(trimmed, ole2Magic)
}

// isProtelNetlist reports whether the payload looks like a Protel/Altium
// ASCII netlist (sections introduced by *PCB*, *NET*, ...).
func isProtelNetlist(trimmed []byte) bool {
	if len(trimmed) == 0 {
		return false
	}
	head := trimmed
	if len(head) > 512 {
		head = head[:512]
	}
	upper := bytes.ToUpper(head)
	return bytes.HasPrefix(upper, []byte("*PCB*")) ||
		bytes.Contains(upper, []byte("\n*NET*")) ||
		bytes.HasPrefix(upper, []byte("*NET*"))
}

// parseRootTag tokenizes just enough of an s-expression payload to return
// the tag of the first top-level list ("kicad_pcb", "export", ...).
func parseRootTag(data []byte) string {
	toks, err := tokenizeSexpr(data)
	if err != nil || len(toks) < 2 {
		return ""
	}
	if toks[0] != "(" {
		return ""
	}
	return toks[1]
}

// netClassOrDefault keeps the domain class when non-empty.
func netClassOrDefault(c string) domainschematic.NetClass {
	if c == "" {
		return domainschematic.ClassDefault
	}
	return domainschematic.NetClass(c)
}

// splitLines splits a payload into trimmed lines (handles CRLF).
func splitLines(data []byte) []string {
	out := strings.Split(string(data), "\n")
	for i := range out {
		out[i] = strings.TrimSpace(strings.TrimSuffix(out[i], "\r"))
	}
	return out
}

// --------------------------------------------------------------
// Constructeur de schéma tolérant (netlists)
// --------------------------------------------------------------

// netlistBuilder accumulates components and net connections from tolerant
// netlist formats: duplicate refs/connections are ignored silently, which is
// the expected behaviour for lossy ASCII imports.
type netlistBuilder struct {
	components []domainschematic.Component
	compSeen   map[string]bool
	nets       []*domainschematic.Net
	netSeen    map[string]bool
	connSeen   map[string]map[string]bool // net -> "ref\0pin"
}

func newNetlistBuilder() *netlistBuilder {
	return &netlistBuilder{
		compSeen: map[string]bool{},
		netSeen:  map[string]bool{},
		connSeen: map[string]map[string]bool{},
	}
}

// addComponent records a component (ref, value, footprint); duplicates and
// empty refs are skipped.
func (b *netlistBuilder) addComponent(ref, value, footprint string) {
	if ref == "" || b.compSeen[ref] {
		return
	}
	b.compSeen[ref] = true
	b.components = append(b.components, domainschematic.Component{
		Ref: ref, Value: value, Footprint: footprint,
	})
}

// addConnection records a (net, class, ref, pin) membership; the net is
// created on first use.
func (b *netlistBuilder) addConnection(netName, netClass, ref, pin string) {
	if netName == "" || ref == "" {
		return
	}
	n := b.net(netName, netClass)
	key := ref + "\x00" + pin
	if b.connSeen[netName] == nil {
		b.connSeen[netName] = map[string]bool{}
	}
	if b.connSeen[netName][key] {
		return
	}
	b.connSeen[netName][key] = true
	n.Connections = append(n.Connections, domainschematic.PinRef{ComponentRef: ref, PinNumber: pin})
}

// net returns (creating if needed) the accumulator of a net.
func (b *netlistBuilder) net(name, class string) *domainschematic.Net {
	if n, ok := b.netSeen[name]; ok && n {
		for _, existing := range b.nets {
			if existing.Name == name {
				return existing
			}
		}
	}
	n := &domainschematic.Net{Name: name, Class: netClassOrDefault(class)}
	b.nets = append(b.nets, n)
	b.netSeen[name] = true
	return n
}

// build materialises the domain schematic.
func (b *netlistBuilder) build(name string) (*domainschematic.Schematic, error) {
	sch := domainschematic.New(name)
	for _, c := range b.components {
		if err := sch.AddComponent(c); err != nil {
			return nil, err
		}
	}
	for _, n := range b.nets {
		if err := sch.AddNet(*n); err != nil {
			return nil, err
		}
	}
	return sch, nil
}
