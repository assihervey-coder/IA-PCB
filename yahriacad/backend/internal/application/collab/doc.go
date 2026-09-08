package collab

import (
	"encoding/json"
	"fmt"

	domainconstraints "github.com/assihervey-coder/IA-PCB/backend/internal/domain/constraints"
	domainlayout "github.com/assihervey-coder/IA-PCB/backend/internal/domain/layout"
	domainproject "github.com/assihervey-coder/IA-PCB/backend/internal/domain/project"
)

// appliedOp is the durable record of one applied operation: the op itself
// plus the state needed to build its inverse (previous position, removed
// tracks...). Seq is the position in the project op log (1-based).
type appliedOp struct {
	Seq     int64    `json:"seq"`
	Op      Op       `json:"op"`
	Applied bool     `json:"applied"` // false = éclipsé par une op LWW plus récente
	Prev    *Payload `json:"prev,omitempty"`
}

// Doc is the CRDT document of one project: the live op log, the LWW
// registers, the vector clock and the per-actor undo/redo stacks. All
// mutations go through Service which serialises access.
type Doc struct {
	projectID string

	log       []*appliedOp
	seen      map[string]bool         // op ID -> déjà traité (déduplication)
	registers map[string]string       // register key -> horodatage LWW gagnant
	undoStack map[string][]*appliedOp // acteur -> ops encore annulables
	redoStack map[string][]*appliedOp // acteur -> ops encore rétablissables
	clock     map[string]uint64       // acteur -> dernier lamport connu
	seq       int64
}

// newDoc builds an empty document.
func newDoc(projectID string) *Doc {
	return &Doc{
		projectID: projectID,
		seen:      map[string]bool{},
		registers: map[string]string{},
		undoStack: map[string][]*appliedOp{},
		redoStack: map[string][]*appliedOp{},
		clock:     map[string]uint64{},
	}
}

// nextLamport merges a client clock and returns the server-stamped value.
func (d *Doc) nextLamport(actor string, clientLamport uint64) uint64 {
	next := d.clock[actor] + 1
	if clientLamport > next {
		next = clientLamport
	}
	d.clock[actor] = next
	return next
}

// acceptLamport observes a remote lamport without consuming it (used to
// rebuild the clock during replay).
func (d *Doc) acceptLamport(actor string, lamport uint64) {
	if lamport > d.clock[actor] {
		d.clock[actor] = lamport
	}
}

// apply mutates the board according to op and returns the durable record.
// mutate=false replays the bookkeeping only (au démarrage la carte du dépôt
// est déjà à jour ; on reconstruit registres, piles et horloge).
func (d *Doc) apply(p *domainproject.Project, op Op, mutate bool) (*appliedOp, error) {
	if d.seen[op.ID] {
		return nil, fmt.Errorf("collab : opération %s déjà appliquée", op.ID)
	}
	d.seen[op.ID] = true
	d.acceptLamport(op.Actor, op.Lamport)

	rec := &appliedOp{Seq: d.seq + 1, Op: op, Applied: true}
	d.seq = rec.Seq

	// Résolution LWW : l'horodatage "lamport|acteur" le plus grand gagne
	// (largeur fixe => comparaison lexicographique = numérique). Une op
	// éclipsée reste au journal (trace complète) sans toucher la carte.
	if op.lww() {
		key := op.registerKey()
		stamp := fmt.Sprintf("%020d|%s", op.Lamport, op.Actor)
		if prev, ok := d.registers[key]; ok && stamp <= prev {
			rec.Applied = false
			d.log = append(d.log, rec)
			return rec, nil
		}
		d.registers[key] = stamp
	}

	board := p.Board()
	switch op.Kind {
	case OpComponentMove:
		c, ok := board.ComponentByRef(op.Target)
		if !ok {
			return nil, fmt.Errorf("collab : composant %q introuvable", op.Target)
		}
		rec.Prev = &Payload{X: c.X, Y: c.Y}
		if mutate {
			c.X = op.Payload.X
			c.Y = op.Payload.Y
		}
	case OpComponentRotate:
		c, ok := board.ComponentByRef(op.Target)
		if !ok {
			return nil, fmt.Errorf("collab : composant %q introuvable", op.Target)
		}
		rec.Prev = &Payload{Rotation: c.Rotation}
		if mutate {
			c.Rotation = op.Payload.Rotation
		}
	case OpTrackAdd:
		if mutate {
			addTracks(board, op.Payload, op.Extra)
		}
	case OpTrackRemove:
		if mutate {
			removed := removeTracksForNet(board, netOf(op))
			snap := make([]TrackSnapshot, 0, len(removed))
			for _, tr := range removed {
				snap = append(snap, trackToSnapshot(tr))
			}
			rec.Prev = &Payload{Net: netOf(op), Tracks: snap}
		}
	case OpViaAdd:
		if mutate {
			if err := board.AddVia(domainlayout.Via{
				Net: op.Payload.Net, X: op.Payload.X, Y: op.Payload.Y,
				FromLayer: op.Payload.FromLayer, ToLayer: op.Payload.ToLayer,
				Diameter: op.Payload.Diameter, Drill: op.Payload.Drill,
			}); err != nil {
				return nil, fmt.Errorf("collab : via refusé : %w", err)
			}
		}
	case OpViaRemove:
		if mutate {
			if !removeViaAt(board, netOf(op), op.Payload.X, op.Payload.Y) {
				return nil, fmt.Errorf("collab : via (%.3f, %.3f) du net %q introuvable",
					op.Payload.X, op.Payload.Y, netOf(op))
			}
		}
	case OpConstraintWidth:
		cs := p.Constraints()
		if cs == nil {
			cs = domainconstraints.NewDefault()
		}
		if v, ok := cs.Value(domainconstraints.RuleMinTrackWidth, op.Payload.NetClass, domainconstraints.AnyLayer); ok {
			rec.Prev = &Payload{MM: v, NetClass: op.Payload.NetClass}
		}
		if mutate {
			if _, err := cs.SetClassRule(domainconstraints.RuleMinTrackWidth, op.Payload.NetClass, op.Payload.MM); err != nil {
				return nil, fmt.Errorf("collab : règle refusée : %w", err)
			}
			p.SetConstraints(cs)
		}
	default:
		return nil, fmt.Errorf("collab : type d'opération inconnu %q", op.Kind)
	}

	d.log = append(d.log, rec)
	return rec, nil
}

// netOf returns the net targeted by a remove op (payload ou target).
func netOf(op Op) string {
	if op.Payload.Net != "" {
		return op.Payload.Net
	}
	return op.Target
}

// inverse builds the compensating operation of an applied op.
func (d *Doc) inverse(rec *appliedOp, actor string, lamport uint64) (Op, error) {
	op := rec.Op
	inv := Op{
		Actor: actor, Lamport: lamport, Target: op.Target,
	}
	switch op.Kind {
	case OpComponentMove, OpComponentRotate:
		if rec.Prev == nil {
			return Op{}, fmt.Errorf("collab : état précédent de %s indisponible", op.Target)
		}
		inv.ID = newOpID()
		inv.Kind = op.Kind
		inv.Payload = *rec.Prev
	case OpTrackAdd:
		inv.ID = newOpID()
		inv.Kind = OpTrackRemove
		inv.Payload.Net = op.Payload.Net
	case OpTrackRemove:
		if rec.Prev == nil || len(rec.Prev.Tracks) == 0 {
			return Op{}, fmt.Errorf("collab : pistes supprimées indisponibles pour %s", op.ID)
		}
		inv.ID = newOpID()
		inv.Kind = OpTrackAdd
		inv.Payload = tracksPayload(rec.Prev.Tracks[0])
		if len(rec.Prev.Tracks) > 1 {
			data, _ := json.Marshal(rec.Prev.Tracks[1:])
			inv.Extra = data
		}
	case OpViaAdd:
		inv.ID = newOpID()
		inv.Kind = OpViaRemove
		inv.Payload.Net = op.Payload.Net
		inv.Payload.X, inv.Payload.Y = op.Payload.X, op.Payload.Y
	case OpViaRemove:
		inv.ID = newOpID()
		inv.Kind = OpViaAdd
		inv.Payload = *rec.Prev
	case OpConstraintWidth:
		if rec.Prev == nil {
			return Op{}, fmt.Errorf("collab : valeur précédente indisponible")
		}
		inv.ID = newOpID()
		inv.Kind = OpConstraintWidth
		inv.Payload = *rec.Prev
	default:
		return Op{}, fmt.Errorf("collab : annulation de %q non supportée", op.Kind)
	}
	return inv, nil
}

// addTracks inserts the main track of the payload plus the extras carried
// in Extra (restauration composée d'un track.remove).
func addTracks(board *domainlayout.Board, payload Payload, extra []byte) {
	addOneTrack(board, payload)
	for _, snap := range decodeExtraTracks(extra) {
		addOneTrack(board, tracksPayload(snap))
	}
}

func addOneTrack(board *domainlayout.Board, payload Payload) {
	pts := make([]domainlayout.TrackPoint, 0, len(payload.Points))
	for _, pt := range payload.Points {
		pts = append(pts, domainlayout.TrackPoint{X: pt.X, Y: pt.Y})
	}
	board.AddTrack(domainlayout.Track{
		Net: payload.Net, Layer: payload.Layer,
		Width: payload.Width, Points: pts,
	})
}

func tracksPayload(snap TrackSnapshot) Payload {
	return Payload{
		Net: snap.Net, Layer: snap.Layer, Width: snap.Width,
		Points: append([]Point(nil), snap.Points...),
	}
}

func decodeExtraTracks(extra []byte) []TrackSnapshot {
	if len(extra) == 0 {
		return nil
	}
	var out []TrackSnapshot
	if err := json.Unmarshal(extra, &out); err != nil {
		return nil
	}
	return out
}

// removeTracksForNet deletes tracks of the net and returns them (for the
// undo payload).
func removeTracksForNet(board *domainlayout.Board, net string) []domainlayout.Track {
	removed := board.TracksForNet(net)
	out := make([]domainlayout.Track, len(removed))
	copy(out, removed)
	board.RemoveRoutesForNet(net)
	return out
}

// removeViaAt deletes the via of net closest to (x, y) within a 0.05 mm
// tolerance. Returns true when one via was removed.
func removeViaAt(board *domainlayout.Board, net string, x, y float64) bool {
	idx, found := -1, false
	best := 0.05
	for i := range board.Vias {
		v := &board.Vias[i]
		if v.Net != net {
			continue
		}
		d := approxHypot(v.X-x, v.Y-y)
		if d <= best {
			best, idx, found = d, i, true
		}
	}
	if !found {
		return false
	}
	board.Vias = append(board.Vias[:idx], board.Vias[idx+1:]...)
	return true
}

func approxHypot(dx, dy float64) float64 {
	return dx*dx + dy*dy // comparaison relative suffisante
}

func trackToSnapshot(t domainlayout.Track) TrackSnapshot {
	pts := make([]Point, 0, len(t.Points))
	for _, p := range t.Points {
		pts = append(pts, Point{X: p.X, Y: p.Y})
	}
	return TrackSnapshot{Net: t.Net, Layer: t.Layer, Width: t.Width, Points: pts}
}

// pushUndo appends an applied record to the actor's undo stack and clears
// their redo stack (édition après undo = branche perdue, sémantique standard).
func (d *Doc) pushUndo(rec *appliedOp) {
	d.undoStack[rec.Op.Actor] = append(d.undoStack[rec.Op.Actor], rec)
	d.redoStack[rec.Op.Actor] = nil
}

// popUndo retires the last undoable op of the actor.
func (d *Doc) popUndo(actor string) *appliedOp {
	stack := d.undoStack[actor]
	if len(stack) == 0 {
		return nil
	}
	rec := stack[len(stack)-1]
	d.undoStack[actor] = stack[:len(stack)-1]
	return rec
}

// pushRedo memorises an undone op for a later redo.
func (d *Doc) pushRedo(rec *appliedOp) {
	d.redoStack[rec.Op.Actor] = append(d.redoStack[rec.Op.Actor], rec)
}

// popRedo retires the last undone op of the actor.
func (d *Doc) popRedo(actor string) *appliedOp {
	stack := d.redoStack[actor]
	if len(stack) == 0 {
		return nil
	}
	rec := stack[len(stack)-1]
	d.redoStack[actor] = stack[:len(stack)-1]
	return rec
}

// popUndoByID removes the undo-stack entry with the given op id (reprise
// du journal). Returns nil when absent.
func (d *Doc) popUndoByID(actor, id string) *appliedOp {
	stack := d.undoStack[actor]
	for i := len(stack) - 1; i >= 0; i-- {
		if stack[i].Op.ID == id {
			rec := stack[i]
			d.undoStack[actor] = append(stack[:i], stack[i+1:]...)
			return rec
		}
	}
	return nil
}

// popRedoByID removes the redo-stack entry with the given op id (reprise
// du journal). Returns nil when absent.
func (d *Doc) popRedoByID(actor, id string) *appliedOp {
	stack := d.redoStack[actor]
	for i := len(stack) - 1; i >= 0; i-- {
		if stack[i].Op.ID == id {
			rec := stack[i]
			d.redoStack[actor] = append(stack[:i], stack[i+1:]...)
			return rec
		}
	}
	return nil
}

// opsSince returns the log entries after seq (rattrapage des clients).
func (d *Doc) opsSince(since int64) []*appliedOp {
	if since >= d.seq {
		return nil
	}
	// Recherche linéaire bornée : le journal est trié par seq.
	start := 0
	for start < len(d.log) && d.log[start].Seq <= since {
		start++
	}
	return append([]*appliedOp(nil), d.log[start:]...)
}
