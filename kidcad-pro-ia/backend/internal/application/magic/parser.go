// Package magic implements the Magic Copilot: a natural-language front end
// (French + English) that turns free-form utterances into structured PCB
// actions. It is deliberately dependency-free (stdlib only) so the parser
// stays pure, deterministic and fully unit-testable; the service half
// (service.go) executes the safe actions against the project aggregate.
//
// Example utterances handled:
//
//	"place R1, R2 and C5 near U3"
//	"pose deux condensateurs près de U1"
//	"set track width to 0.3 mm for the power class"
//	"mets la largeur de piste à 12 mil sur la classe power"
//	"route everything with the fast strategy" / "route tout en stratégie rapide"
//	"vérifie les règles de conception" (DRC) / "run drc"
//	"exporte les gerber" / "export bom and step"
//	"supprime C17" / "delete C17"
//	"ajoute une classe de nets high-speed, largeur 0.15 mm"
package magic

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// Kind enumerates the action verbs the copilot understands.
type Kind string

const (
	KindPlaceComponent  Kind = "place_component"
	KindMoveComponent   Kind = "move_component"
	KindDeleteComponent Kind = "delete_component"
	KindSetTrackWidth   Kind = "set_track_width"
	KindAddNetClass     Kind = "add_net_class"
	KindRoute           Kind = "route"
	KindOptimize        Kind = "optimize"
	KindRunDRC          Kind = "run_drc"
	KindRunERC          Kind = "run_erc"
	KindExport          Kind = "export"
)

// milMM converts mil (1/1000 inch) to millimetres.
const milMM = 0.0254

// Action is one structured PCB operation extracted from an utterance.
// Executable marks actions the backend can apply directly on the project
// aggregate (place/move/delete/width/net-class); the others are handed back
// to the caller (frontend or REST client) which triggers the matching
// use-case endpoint.
type Action struct {
	Kind       Kind           `json:"kind"`
	Params     map[string]any `json:"params"`
	Summary    string         `json:"summary"`
	Executable bool           `json:"executable"`
}

// Interpretation is the full copilot answer: the ordered action list, a
// confidence score (0..1) and a human-readable French reply.
type Interpretation struct {
	Utterance  string   `json:"utterance"`
	Language   string   `json:"language"` // "fr" | "en"
	Actions    []Action `json:"actions"`
	Confidence float64  `json:"confidence"`
	Reply      string   `json:"reply"`
}

// component reference designators: R1, C10, U3, SW1, TP4… (1-2 letters).
var refRe = regexp.MustCompile(`(?i)\b([a-su-z]{1,2}[0-9]{1,3})\b`)

// numbers with an optional unit (mm / mil).
var numUnitRe = regexp.MustCompile(`([0-9]+(?:[.,][0-9]+)?)\s*(millim[eè]tres?|millimetres?|mm|mil|mils|milli[i]?nch(?:e?s)?)?`)

// coordinate pairs: "en (12.5, 3)", "at (10, 20)".
var coordParenRe = regexp.MustCompile(`(?:\(|\[)?\s*([0-9]+(?:[.,][0-9]+)?)\s*[,;]\s*([0-9]+(?:[.,][0-9]+)?)\s*(?:\)|\])?`)

// coordinate pairs typed as "x=10 y=20".
var coordKVRe = regexp.MustCompile(`(?:x|y)\s*=?\s*([0-9]+(?:[.,][0-9]+)?)`)

// segmenter splits multi-action utterances on FR/EN sequential connectors.
var segmenterRe = regexp.MustCompile(`(?i)\b(?:puis|ensuite|apr[eè]s quoi|and then|then|after that)\b|;`)

// frenchAccents maps the precomposed French characters onto their ASCII
// base (NFD decomposition is unavailable in the stdlib; an explicit map is
// deterministic and covers French + the few EN edge cases).
var frenchAccents = map[rune]rune{
	'à': 'a', 'á': 'a', 'â': 'a', 'ä': 'a', 'ã': 'a', 'å': 'a',
	'é': 'e', 'è': 'e', 'ê': 'e', 'ë': 'e',
	'î': 'i', 'ï': 'i', 'í': 'i',
	'ô': 'o', 'ö': 'o', 'ò': 'o', 'ó': 'o', 'õ': 'o',
	'ù': 'u', 'û': 'u', 'ü': 'u', 'ú': 'u',
	'ç': 'c', 'ñ': 'n', 'ý': 'y', 'ÿ': 'y',
	'œ': 'o', 'æ': 'a',
}

// stripAccents normalises French accents so matching is accent-insensitive
// ("épaisseur" ~ "epaisseur", "près" ~ "pres") — both for precomposed
// characters (è U+00E8) and decomposed ones (e + U+0300).
func stripAccents(s string) string {
	var b strings.Builder
	for _, r := range s {
		if mapped, ok := frenchAccents[r]; ok {
			b.WriteRune(mapped)
			continue
		}
		if unicode.Is(unicode.Mn, r) { // combining marks (e + U+0301)
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// normalize lowercases, strips accents and collapses whitespace.
func normalize(s string) string {
	lower := strings.ToLower(strings.TrimSpace(s))
	noAcc := stripAccents(lower)
	return strings.Join(strings.Fields(noAcc), " ")
}

// parseFloat parses "12,5" and "12.5" alike.
func parseFloat(raw string) float64 {
	v, err := strconv.ParseFloat(strings.ReplaceAll(raw, ",", "."), 64)
	if err != nil {
		return math.NaN()
	}
	return v
}

// parseLengthMM converts a (number, unit) pair into millimetres; missing
// units default to millimetres.
func parseLengthMM(value, unit string) float64 {
	mm := parseFloat(value)
	if math.IsNaN(mm) {
		return math.NaN()
	}
	u := normalize(unit)
	if strings.HasPrefix(u, "mil") { // mil, mils, millinch…
		return mm * milMM
	}
	return mm
}

// languageOf counts French markers to guess the utterance language (the
// copilot replies in French either way, this only refines tokenisation).
func languageOf(n string) string {
	fr := []string{" largeur ", " epaisseur ", " sur ", " de ", " la ", " les ", " tout ", " pres ", " supprime ", " exporte ", " verifie ", " ensuite ", " puis ", " place ", " pose "}
	en := []string{" set ", " width ", " for ", " the ", " all ", " near ", " delete ", " export ", " check ", " then "}
	score := 0
	for _, m := range fr {
		if strings.Contains(n, m) {
			score++
		}
	}
	for _, m := range en {
		if strings.Contains(n, m) {
			score--
		}
	}
	if score >= 0 {
		return "fr"
	}
	return "en"
}

// extractRefs finds component reference designators (R1, C10, U3…).
func extractRefs(n string) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range refRe.FindAllStringSubmatch(n, -1) {
		ref := strings.ToUpper(m[1])
		if !seen[ref] {
			seen[ref] = true
			out = append(out, ref)
		}
	}
	return out
}

// hasAny reports whether the normalised text contains any of the phrases.
func hasAny(n string, phrases ...string) bool {
	for _, p := range phrases {
		if strings.Contains(n, p) {
			return true
		}
	}
	return false
}

// extractStrategy maps FR/EN quality words onto the backend strategies.
func extractStrategy(n string) (string, bool) {
	switch {
	case hasAny(n, "rapide", "vite", "speed", "fast", "turbo"):
		return "fast", true
	case hasAny(n, "qualite", "meilleur", "quality", "best", "premium"):
		return "quality", true
	case hasAny(n, "equilibre", "balance", "balanced", "normal"):
		return "balanced", true
	}
	return "", false
}

// extractExportFormats returns the requested export targets.
func extractExportFormats(n string) []string {
	var out []string
	if hasAny(n, "gerber") {
		out = append(out, "gerber")
	}
	if hasAny(n, "bom", "nomenclature", "bill of material") {
		out = append(out, "bom")
	}
	if hasAny(n, "step", "3d", "cad 3d") {
		out = append(out, "step")
	}
	return out
}

// isConnector reports whether the token is a list/sequence connector.
func isConnector(tok string) bool {
	switch tok {
	case "et", "and", "ou", "or", "avec", "with", "puis", "then", "ensuite", "ainsi", "que":
		return true
	}
	return false
}

// extractNetNames picks tokens announced by "net"/"nets" (FR/EN) or known
// power names appearing free (GND, VCC, VDD, VSS…).
func extractNetNames(n string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(v string) {
		v = strings.ToUpper(strings.TrimSpace(v))
		if v != "" && !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	fields := strings.Fields(n)
	for i, f := range fields {
		if (f == "net" || f == "nets") && i+1 < len(fields) {
			// collect until a connector word stops the list
			for _, cand := range strings.Split(fields[i+1], ",") {
				cand = strings.Trim(cand, ",")
				if cand == "" || isConnector(cand) {
					break
				}
				add(cand)
			}
		}
	}
	for _, power := range []string{"gnd", "vcc", "vdd", "vss", "vin", "3v3", "5v"} {
		if hasAny(n, " "+power+" ") {
			add(power)
		}
	}
	return out
}

// extractCoordinates parses "(x, y)" or "x=… y=…".
func extractCoordinates(n string) (x, y float64, ok bool) {
	if m := coordParenRe.FindStringSubmatch(n); m != nil {
		x, y = parseFloat(m[1]), parseFloat(m[2])
		return x, y, !math.IsNaN(x) && !math.IsNaN(y)
	}
	var gotX, gotY bool
	for _, m := range coordKVRe.FindAllStringSubmatch(n, -1) {
		v := parseFloat(m[1])
		if math.IsNaN(v) {
			return 0, 0, false
		}
		if strings.HasPrefix(m[0], "x") || strings.HasPrefix(m[0], "X") {
			x, gotX = v, true
		} else {
			y, gotY = v, true
		}
	}
	return x, y, gotX && gotY
}

// nearTargetRef returns the reference following a proximity phrase
// ("près de U3", "near U3", "autour de U3").
func nearTargetRef(n string) (string, bool) {
	fields := strings.Fields(n)
	for i, f := range fields {
		if f == "pres" || f == "near" || f == "autour" || f == "around" || f == "proche" {
			// skip optional articles/prepositions
			for j := i + 1; j < len(fields) && j <= i+3; j++ {
				switch fields[j] {
				case "de", "du", "la", "le", "to", "of", "the", "les":
					continue
				}
				if refs := extractRefs(fields[j]); len(refs) > 0 {
					return refs[0], true
				}
				return "", false
			}
		}
	}
	return "", false
}

// extractWidth grabs the first number explicitly tied to a length unit,
// only when width vocabulary is present (avoids swallowing coordinates).
func extractWidth(n string) (mm float64, ok bool) {
	if !hasAny(n, "largeur", "epaisseur", "width", "track", "piste") {
		return 0, false
	}
	for _, m := range numUnitRe.FindAllStringSubmatch(n, -1) {
		if m[2] == "" {
			continue // bare numbers are handled as coordinates elsewhere
		}
		if v := parseLengthMM(m[1], m[2]); !math.IsNaN(v) && v > 0 && v < 100 {
			return v, true
		}
	}
	return 0, false
}

// extractClassTargets returns the net classes mentioned ("power", "signal",
// "high-speed", "default" or an explicit "classe X").
func extractClassTargets(n string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(c string) {
		if !seen[c] {
			seen[c] = true
			out = append(out, c)
		}
	}
	for _, c := range []string{"power", "high-speed", "signal", "default"} {
		if hasAny(n, " "+c+" ") || strings.HasSuffix(strings.TrimSpace(n), c) {
			add(c)
		}
	}
	fields := strings.Fields(n)
	for i, f := range fields {
		if (f == "classe" || f == "class") && i+1 < len(fields) {
			cand := strings.Trim(fields[i+1], ",")
			if cand != "" && !isConnector(cand) {
				add(cand)
			}
		}
	}
	return out
}

// wantsAll detects "tout", "all", "tous les composants", "everything".
func wantsAll(n string) bool {
	return hasAny(n, " tout ", " all ", "tous les", "everything", " complet", " complete")
}

// Parse is the pure entry point: one utterance in, one interpretation out.
// It never returns a nil Interpretation; a confidence of 0 means "nothing
// understood" and the Reply suggests the supported grammar.
func Parse(utterance string) *Interpretation {
	n := " " + normalize(utterance) + " "
	if strings.TrimSpace(utterance) == "" {
		return &Interpretation{Utterance: utterance, Confidence: 0, Reply: replyUnknown()}
	}
	lang := languageOf(n)

	// Multi-action utterances: split on sequential connectors.
	segments := segmenterRe.Split(strings.TrimSpace(n), -1)
	var actions []Action
	score := 0.0
	for _, seg := range segments {
		if a, s := parseSegment(seg); len(a) > 0 {
			actions = append(actions, a...)
			score = math.Max(score, s)
		}
	}
	if len(actions) == 0 {
		return &Interpretation{Utterance: utterance, Language: lang, Confidence: 0, Reply: replyUnknown()}
	}
	return &Interpretation{
		Utterance:  utterance,
		Language:   lang,
		Actions:    actions,
		Confidence: math.Min(0.99, score),
		Reply:      buildReply(actions, score),
	}
}

// parseSegment recognises one action inside a segment and returns it with
// its confidence.
func parseSegment(seg string) ([]Action, float64) {
	refs := extractRefs(seg)
	cls := extractClassTargets(seg)
	nets := extractNetNames(seg)
	x, y, hasCoord := extractCoordinates(seg)
	nearRef, hasNear := nearTargetRef(seg)
	strategy, hasStrategy := extractStrategy(seg)
	formats := extractExportFormats(seg)

	switch {
	// ---------------------------------------------------------- export
	case hasAny(seg, "export", "genere", "produis"):
		if len(formats) == 0 {
			formats = []string{"gerber"}
		}
		out := make([]Action, 0, len(formats))
		for _, f := range formats {
			out = append(out, Action{
				Kind:    KindExport,
				Params:  map[string]any{"format": f},
				Summary: summaryExport(f),
			})
		}
		return out, 0.85

	// ------------------------------------------------------ verification
	case hasAny(seg, "drc", "regles de conception", "design rule", "controle de conception", "verifie les regles", "check rules"):
		return []Action{{Kind: KindRunDRC, Params: map[string]any{}, Summary: "Lancer un contrôle de conception (DRC) complet."}}, 0.9
	case hasAny(seg, "erc", "regles electriques", "electrical rule", "verifie le schema"):
		return []Action{{Kind: KindRunERC, Params: map[string]any{}, Summary: "Lancer un contrôle électrique (ERC)."}}, 0.9

	// ------------------------------------------------------- width rules
	case hasAny(seg, "largeur", "epaisseur", "width"):
		if mm, ok := extractWidth(seg); ok {
			target := "all"
			if len(cls) > 0 {
				target = cls[0]
			} else if len(nets) > 0 {
				target = nets[0]
			}
			return []Action{{
				Kind:       KindSetTrackWidth,
				Params:     map[string]any{"target": target, "width_mm": mm},
				Summary:    fmt.Sprintf("Largeur de piste réglée à %.3f mm (%s).", mm, target),
				Executable: true,
			}}, 0.9
		}
		return []Action{{
			Kind:    KindSetTrackWidth,
			Params:  map[string]any{},
			Summary: "Largeur demandée mais aucune valeur exploitable (ex. « 0.3 mm » ou « 12 mil »).",
		}}, 0.4

	// ------------------------------------------------------- net classes
	case hasAny(seg, "classe de net", "net class", "classe", "class") && hasAny(seg, "ajoute", "add", "nouvelle", "new", "cree"):
		name := "custom"
		if len(cls) > 0 {
			name = cls[0]
		}
		params := map[string]any{"name": name}
		if mm, ok := extractWidth(seg); ok {
			params["width_mm"] = mm
		}
		return []Action{{
			Kind: KindAddNetClass, Params: params,
			Summary:    fmt.Sprintf("Création de la classe de nets %q.", name),
			Executable: true,
		}}, 0.8

	// ----------------------------------------------------------- routing
	case hasAny(seg, "route", "achemine", "cabl", "rout"):
		params := map[string]any{}
		if wantsAll(seg) || (len(nets) == 0 && len(refs) == 0) {
			params["nets"] = []string{"*"}
		} else if len(nets) > 0 {
			params["nets"] = nets
		}
		if hasStrategy {
			params["strategy"] = strategy
		}
		return []Action{{Kind: KindRoute, Params: params, Summary: summaryRoute(params)}}, 0.85

	case hasAny(seg, "optimis", "optimiz", "raccourci les pistes", "shorten"):
		params := map[string]any{}
		if hasStrategy {
			params["strategy"] = strategy
		}
		return []Action{{Kind: KindOptimize, Params: params, Summary: "Optimisation des pistes existantes."}}, 0.8

	// --------------------------------------------------------- deletion
	case hasAny(seg, "supprime", "delete", "remove", "enleve", "retire"):
		if len(refs) == 0 {
			return []Action{{Kind: KindDeleteComponent, Params: map[string]any{},
				Summary: "Suppression demandée sans référence de composant (ex. « supprime C17 »)."}}, 0.4
		}
		out := make([]Action, 0, len(refs))
		for _, r := range refs {
			out = append(out, Action{Kind: KindDeleteComponent,
				Params: map[string]any{"ref": r}, Summary: "Suppression de " + r + ".", Executable: true})
		}
		return out, 0.9

	// -------------------------------------------------- placement / move
	case hasAny(seg, "place", "pose", "positionne", "deplace", "move", "mov", "met"):
		// The anchor of a "near" phrase is not a target: "place R1
		// près de U3" moves R1 only. Filter it out before branching.
		if hasNear {
			filtered := refs[:0]
			for _, r := range refs {
				if r != nearRef {
					filtered = append(filtered, r)
				}
			}
			refs = filtered
		}

		if len(refs) == 0 && !wantsAll(seg) {
			// Component-kind phrasing: "place deux condensateurs près de U1".
			if kind, count, ok := extractComponentWish(seg); ok {
				return []Action{{
					Kind: KindPlaceComponent,
					Params: map[string]any{"component_kind": kind,
						"count": float64(count),
						"near":  nearRef, "has_near": hasNear, "x": x, "y": y, "has_coord": hasCoord},
					Summary:    fmt.Sprintf("Placement de %d %s%s.", count, kind, nearPhrase(nearRef, hasNear)),
					Executable: true,
				}}, 0.75
			}
			return []Action{{Kind: KindPlaceComponent, Params: map[string]any{},
				Summary: "Placement demandé sans référence ni type de composant (ex. « place R1 près de U3 »)."}}, 0.35
		}

		targets := refs
		if len(targets) == 0 && wantsAll(seg) {
			targets = []string{"*"}
		}
		verb := "Déplacement"
		if hasAny(seg, "place", "pose", "positionne") {
			verb = "Placement"
		}
		out := make([]Action, 0, len(targets))
		for _, r := range targets {
			params := map[string]any{"ref": r}
			if hasCoord {
				params["x"], params["y"] = x, y
			}
			if hasNear {
				params["near"] = nearRef
			}
			sum := verb + " de " + r
			if hasNear {
				sum += " près de " + nearRef
			} else if hasCoord {
				sum += fmt.Sprintf(" en (%.2f, %.2f)", x, y)
			}
			sum += "."
			out = append(out, Action{Kind: KindMoveComponent, Params: params, Summary: sum, Executable: true})
		}
		return out, 0.88
	}

	return nil, 0
}

// componentWishes maps French/EN common component nouns onto canonical kinds.
var componentWishes = map[string]string{
	"condensateur": "capacitor", "condensateurs": "capacitor",
	"capacitor": "capacitor", "capacitors": "capacitor", "cap": "capacitor",
	"resistance": "resistor", "resistances": "resistor",
	"resistor": "resistor", "resistors": "resistor",
	"led": "led", "leds": "led",
	"inductance": "inductor", "inductances": "inductor", "inductor": "inductor",
	"connecteur": "connector", "connecteurs": "connector", "connector": "connector",
}

// numberWords converts small FR/EN quantity words to integers.
var numberWords = map[string]int{
	"un": 1, "une": 1, "a": 1, "an": 1, "one": 1,
	"deux": 2, "two": 2, "trois": 3, "three": 3,
	"quatre": 4, "four": 4, "cinq": 5, "five": 5,
	"six": 6, "sept": 7, "seven": 7, "huit": 8, "eight": 8,
	"dix": 10, "ten": 10,
}

// extractComponentWish recognises "deux condensateurs", "4 caps"…
func extractComponentWish(seg string) (kind string, count int, ok bool) {
	fields := strings.Fields(seg)
	for i, f := range fields {
		if k, isKind := componentWishes[f]; isKind {
			// look backwards for a quantity word or digit
			for j := i - 1; j >= 0 && j >= i-3; j-- {
				if v, isNum := numberWords[fields[j]]; isNum {
					return k, v, true
				}
				if d, err := strconv.Atoi(fields[j]); err == nil && d > 0 && d < 1000 {
					return k, d, true
				}
			}
			return k, 1, true
		}
	}
	return "", 0, false
}

// nearPhrase renders the proximity fragment of a summary.
func nearPhrase(near string, has bool) string {
	if !has {
		return ""
	}
	return " près de " + near
}

func summaryRoute(params map[string]any) string {
	nets, _ := params["nets"].([]string)
	target := "tout le netlist"
	if len(nets) > 0 && nets[0] != "*" {
		target = strings.Join(nets, ", ")
	}
	if strat, _ := params["strategy"].(string); strat != "" {
		return fmt.Sprintf("Routage IA de %s (stratégie %s).", target, strat)
	}
	return fmt.Sprintf("Routage IA de %s.", target)
}

func summaryExport(format string) string {
	switch format {
	case "bom":
		return "Génération de la nomenclature (BOM)."
	case "step":
		return "Export du modèle 3D (STEP)."
	default:
		return "Export des fichiers de fabrication Gerber."
	}
}

// buildReply composes the French natural-language answer.
func buildReply(actions []Action, score float64) string {
	var b strings.Builder
	switch {
	case score >= 0.75:
		b.WriteString("Compris ! ")
	case score >= 0.4:
		b.WriteString("J'ai une interprétation partielle — vérifiez les détails. ")
	default:
		b.WriteString("Interprétation incertaine. ")
	}
	for i, a := range actions {
		if i > 0 {
			b.WriteString(" Puis, ")
		}
		b.WriteString(a.Summary)
	}
	return b.String()
}

func replyUnknown() string {
	return "Je n'ai pas compris. Essayez : « place R1 près de U3 », « largeur de piste 0.3 mm », " +
		"« route tout en rapide », « drc », « exporte gerber puis bom », « supprime C17 »."
}

// SortActions orders executable actions first (nice UX detail: the frontend
// renders what will happen immediately at the top).
func SortActions(actions []Action) {
	sort.SliceStable(actions, func(i, j int) bool {
		if actions[i].Executable != actions[j].Executable {
			return actions[i].Executable
		}
		return actions[i].Kind < actions[j].Kind
	})
}
