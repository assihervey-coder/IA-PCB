// Package collab implements multi-user collaborative editing of a board:
// an operation-based CRDT (LWW registers for component position/rotation
// and constraint values, additive sets for copper) with per-actor undo /
// redo stacks kept in a durable operation log (JSONL). Two clients working
// offline converge to the same board once their operations are exchanged,
// because every mutation is expressed as a commutative or last-writer-wins
// operation stamped with a Lamport clock.
//
// The operation vocabulary is deliberately small and covers the live
// editing surface of the layout editor:
//
//	component.move    position LWW per component ref
//	component.rotate  rotation LWW per component ref
//	track.add         additive (dedup by op id)
//	track.remove      removal by net (restorable)
//	via.add           additive (dedup by op id)
//	constraint.width  LWW per net class
package collab

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/kidcad/kidcad-pro-ia/backend/internal/pkg/utils"
)

// Operation kinds.
const (
	OpComponentMove   = "component.move"
	OpComponentRotate = "component.rotate"
	OpTrackAdd        = "track.add"
	OpTrackRemove     = "track.remove"
	OpViaAdd          = "via.add"
	OpViaRemove       = "via.remove"
	OpConstraintWidth = "constraint.width"
)

// Point is one 2D coordinate of a track polyline.
type Point struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// TrackSnapshot is the restorable image of one track (used to undo a
// track.remove without re-deriving geometry).
type TrackSnapshot struct {
	Net    string  `json:"net"`
	Layer  int     `json:"layer"`
	Width  float64 `json:"width"`
	Points []Point `json:"points"`
}

// Payload is the flat union of every operation payload. Only the fields
// relevant to Kind are read; the others are ignored, which keeps the wire
// format self-describing and forward compatible.
type Payload struct {
	X        float64 `json:"x,omitempty"`
	Y        float64 `json:"y,omitempty"`
	Rotation float64 `json:"rotation,omitempty"`

	Net       string  `json:"net,omitempty"`
	Layer     int     `json:"layer,omitempty"`
	Width     float64 `json:"width,omitempty"`
	Points    []Point `json:"points,omitempty"`
	FromLayer int     `json:"from_layer,omitempty"`
	ToLayer   int     `json:"to_layer,omitempty"`
	Diameter  float64 `json:"diameter,omitempty"`
	Drill     float64 `json:"drill,omitempty"`

	MM       float64 `json:"mm,omitempty"`
	NetClass string  `json:"net_class,omitempty"`

	// Restored tracks (inverse of track.remove, maintained internally).
	Tracks []TrackSnapshot `json:"tracks,omitempty"`
}

// Op is one collaborative operation. Lamport defines the causal order;
// Actor breaks ties deterministically. ID deduplicates retransmissions.
type Op struct {
	ID      string          `json:"id"`
	Actor   string          `json:"actor"`
	Lamport uint64          `json:"lamport"`
	Kind    string          `json:"kind"`
	Target  string          `json:"target,omitempty"`
	Payload Payload         `json:"payload"`
	At      time.Time       `json:"at"`
	Extra   json.RawMessage `json:"extra,omitempty"`
}

// Validate checks the intrinsic shape of an operation (payload coherency
// is checked at apply time, against the board).
func (o *Op) Validate() error {
	if o.ID == "" {
		return errors.New("collab : identifiant d'opération manquant")
	}
	if o.Actor == "" {
		return errors.New("collab : acteur manquant")
	}
	switch o.Kind {
	case OpComponentMove, OpComponentRotate:
		if o.Target == "" {
			return fmt.Errorf("collab : %s exige une référence de composant", o.Kind)
		}
	case OpTrackAdd:
		if len(o.Payload.Points) < 2 || o.Payload.Net == "" {
			return errors.New("collab : track.add exige un net et une polyligne de 2 points")
		}
	case OpTrackRemove:
		if o.Payload.Net == "" && o.Target == "" {
			return errors.New("collab : track.remove exige un net")
		}
	case OpViaAdd:
		if o.Payload.Net == "" {
			return errors.New("collab : via.add exige un net")
		}
	case OpViaRemove:
		if o.Payload.Net == "" {
			return errors.New("collab : via.remove exige un net")
		}
	case OpConstraintWidth:
		if o.Payload.MM <= 0 || o.Payload.NetClass == "" {
			return errors.New("collab : constraint.width exige une valeur mm et une classe de nets")
		}
	default:
		return fmt.Errorf("collab : type d'opération inconnu %q", o.Kind)
	}
	return nil
}

// lww reports whether the operation resolves as a last-writer-wins
// register (vs an additive op).
func (o *Op) lww() bool {
	switch o.Kind {
	case OpComponentMove, OpComponentRotate, OpConstraintWidth:
		return true
	}
	return false
}

// registerKey identifies the LWW register the op writes to.
func (o *Op) registerKey() string {
	if o.Kind == OpConstraintWidth {
		return o.Kind + ":" + o.Payload.NetClass
	}
	return o.Kind + ":" + o.Target
}

// newOpID mints a server-side operation identifier.
func newOpID() string { return utils.NewID() }
