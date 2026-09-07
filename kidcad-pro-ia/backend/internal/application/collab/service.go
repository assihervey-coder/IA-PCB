// service.go wires the CRDT document to the project repository, a durable
// JSONL op log and the optional WebSocket broadcast. The log lives under
// <data_dir>/collab/<project_id>.jsonl; every entry is one applied
// operation or one undo/redo marker, so the undo/redo history survives
// server restarts and replays deterministically.
package collab

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	apperrors "github.com/kidcad/kidcad-pro-ia/backend/internal/application"
	domainproject "github.com/kidcad/kidcad-pro-ia/backend/internal/domain/project"
	"github.com/kidcad/kidcad-pro-ia/backend/internal/pkg/utils"
)

// OpRequest is the client-facing operation: the identifier and the Lamport
// stamp are optional (server-side generation when absent) but supplying a
// stable client_id makes retransmissions idempotent.
type OpRequest struct {
	ClientID string  `json:"client_id,omitempty"`
	Lamport  uint64  `json:"lamport,omitempty"`
	Kind     string  `json:"kind"`
	Target   string  `json:"target,omitempty"`
	Payload  Payload `json:"payload"`
}

// ApplyRequest is the payload of POST .../collab/ops.
type ApplyRequest struct {
	Actor string      `json:"actor"`
	Ops   []OpRequest `json:"ops"`
}

// ApplyResult is the response of POST .../collab/ops.
type ApplyResult struct {
	ProjectID string       `json:"project_id"`
	Actor     string       `json:"actor"`
	Applied   int          `json:"applied"`
	Seq       int64        `json:"seq"`
	Ops       []Op         `json:"ops"`
	Rejected  []RejectedOp `json:"rejected"`
}

// RejectedOp reports one operation the document refused.
type RejectedOp struct {
	ClientID string `json:"client_id,omitempty"`
	Kind     string `json:"kind"`
	Reason   string `json:"reason"`
}

// CollabState is the response of GET .../collab/state?since=N: the log
// entries after N let a reconnecting client catch up without a full
// refetch.
type CollabState struct {
	ProjectID string            `json:"project_id"`
	Seq       int64             `json:"seq"`
	Clock     map[string]uint64 `json:"clock"`
	Ops       []*appliedOp      `json:"ops,omitempty"`
}

// UndoResult is the response of POST .../collab/undo and .../redo.
type UndoResult struct {
	Actor  string `json:"actor"`
	Undone bool   `json:"undone"`
	Op     *Op    `json:"op,omitempty"` // l'opération (inverse ou ré-appliquée)
	Reason string `json:"reason,omitempty"`
}

// Publisher is the realtime port (implémenté par websocket.Hub.Broadcast).
type Publisher interface {
	Broadcast(projectID string, payload any)
}

// collabEvent is the WS envelope broadcast on every applied op.
type collabEvent struct {
	Type      string `json:"type"` // "collab"
	ProjectID string `json:"project_id"`
	Op        Op     `json:"op"`
	Seq       int64  `json:"seq"`
}

// logEntry is one durable journal record: either an applied operation
// (type "op", with its Prev state for a future undo) or the marker of an
// undo/redo that moved an existing op between the stacks.
type logEntry struct {
	Type    string     `json:"type"` // "op" | "undo" | "redo"
	Record  *appliedOp `json:"record,omitempty"`
	Actor   string     `json:"actor,omitempty"`
	MovedID string     `json:"moved_id,omitempty"`
}

// Service owns the documents and the durable log.
type Service struct {
	projects  domainproject.Repository
	publisher Publisher
	dir       string // "" = journal mémoire seule (tests)
	log       *slog.Logger

	mu   sync.Mutex
	docs map[string]*Doc
}

// NewService builds the collaboration use case. dataDir vide désactive la
// persistance du journal (utile aux tests).
func NewService(projects domainproject.Repository, publisher Publisher, dataDir string, log *slog.Logger) *Service {
	if log == nil {
		log = slog.Default()
	}
	return &Service{
		projects:  projects,
		publisher: publisher,
		dir:       dataDir,
		log:       log,
		docs:      map[string]*Doc{},
	}
}

// Apply validates, stamps and applies a batch of operations. Une op
// rejetée n'annule pas les autres : le résultat détaille les rejets.
func (s *Service) Apply(ctx context.Context, projectID string, req ApplyRequest) (*ApplyResult, error) {
	if req.Actor == "" {
		return nil, errors.New("collab : acteur manquant")
	}
	if len(req.Ops) == 0 {
		return nil, errors.New("collab : aucune opération fournie")
	}

	doc, p, err := s.document(ctx, projectID)
	if err != nil {
		return nil, err
	}

	res := &ApplyResult{ProjectID: projectID, Actor: req.Actor, Ops: []Op{}, Rejected: []RejectedOp{}}
	entries := []logEntry{}
	for _, r := range req.Ops {
		op := Op{
			ID: r.ClientID, Actor: req.Actor, Kind: r.Kind,
			Target: r.Target, Payload: r.Payload, At: time.Now().UTC(),
		}
		if op.ID == "" {
			op.ID = newOpID()
		}
		op.Lamport = doc.nextLamport(req.Actor, r.Lamport)
		if err := op.Validate(); err != nil {
			res.Rejected = append(res.Rejected, RejectedOp{ClientID: r.ClientID, Kind: r.Kind, Reason: err.Error()})
			continue
		}
		rec, err := doc.apply(p, op, true)
		if err != nil {
			res.Rejected = append(res.Rejected, RejectedOp{ClientID: r.ClientID, Kind: r.Kind, Reason: err.Error()})
			continue
		}
		doc.pushUndo(rec)
		entries = append(entries, logEntry{Type: "op", Record: rec})
		res.Ops = append(res.Ops, rec.Op)
		res.Applied++
		s.broadcast(projectID, rec)
	}

	if res.Applied > 0 {
		if err := s.projects.Update(ctx, p); err != nil {
			return nil, fmt.Errorf("collab : persistance du projet : %w", err)
		}
		if err := s.appendLog(projectID, entries); err != nil {
			s.log.Warn("collab : journal non persisté", "project_id", projectID, "err", err)
		}
	}
	res.Seq = doc.seq
	return res, nil
}

// State returns the current sequence, the vector clock and optionally the
// operations after since (rattrapage d'un client reconnecté).
func (s *Service) State(ctx context.Context, projectID string, since int64) (*CollabState, error) {
	doc, _, err := s.document(ctx, projectID)
	if err != nil {
		return nil, err
	}
	clock := make(map[string]uint64, len(doc.clock))
	for actor, l := range doc.clock {
		clock[actor] = l
	}
	st := &CollabState{ProjectID: projectID, Seq: doc.seq, Clock: clock}
	if since >= 0 {
		st.Ops = doc.opsSince(since)
	}
	return st, nil
}

// Undo applies the inverse of the actor's last undoable operation. The
// inverse is a first-class op: it joins the log, is broadcast and is
// durable like any other mutation.
func (s *Service) Undo(ctx context.Context, projectID, actor string) (*UndoResult, error) {
	return s.unRe(ctx, projectID, actor, true)
}

// Redo re-applies the actor's last undone operation.
func (s *Service) Redo(ctx context.Context, projectID, actor string) (*UndoResult, error) {
	return s.unRe(ctx, projectID, actor, false)
}

func (s *Service) unRe(ctx context.Context, projectID, actor string, undo bool) (*UndoResult, error) {
	if actor == "" {
		return nil, errors.New("collab : acteur manquant")
	}
	doc, p, err := s.document(ctx, projectID)
	if err != nil {
		return nil, err
	}

	label := "redo"
	if undo {
		label = "undo"
	}
	var popped *appliedOp
	if undo {
		popped = doc.popUndo(actor)
	} else {
		popped = doc.popRedo(actor)
	}
	if popped == nil {
		return &UndoResult{Actor: actor, Undone: false, Reason: "rien à " + label + " pour " + actor}, nil
	}

	var op Op
	if undo {
		op, err = doc.inverse(popped, actor, doc.nextLamport(actor, 0))
	} else {
		// Redo = ré-application de l'op originale sous un nouveau lamport.
		op = popped.Op
		op.ID = newOpID()
		op.Lamport = doc.nextLamport(actor, 0)
		op.At = time.Now().UTC()
	}
	if err != nil {
		return nil, err
	}
	if err := op.Validate(); err != nil {
		return nil, fmt.Errorf("collab : %s invalide : %w", label, err)
	}

	rec, err := doc.apply(p, op, true)
	if err != nil {
		return nil, err
	}
	if undo {
		doc.pushRedo(popped)
	} else {
		doc.pushUndo(rec)
	}

	if err := s.projects.Update(ctx, p); err != nil {
		return nil, fmt.Errorf("collab : persistance du projet : %w", err)
	}
	entries := []logEntry{
		{Type: "op", Record: rec},
		{Type: label, Actor: actor, MovedID: popped.Op.ID},
	}
	if err := s.appendLog(projectID, entries); err != nil {
		s.log.Warn("collab : journal non persisté", "project_id", projectID, "err", err)
	}
	s.broadcast(projectID, rec)

	return &UndoResult{Actor: actor, Undone: true, Op: &rec.Op}, nil
}

// --------------------------------------------------------------
// Documents + journal durable
// --------------------------------------------------------------

// document returns the project's document, loading (and replaying) the
// durable log on first touch.
func (s *Service) document(ctx context.Context, projectID string) (*Doc, *domainproject.Project, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	doc, ok := s.docs[projectID]
	if !ok {
		doc = newDoc(projectID)
		if s.dir != "" {
			if err := s.replay(projectID, doc); err != nil {
				s.log.Warn("collab : reprise du journal impossible", "project_id", projectID, "err", err)
			}
		}
		s.docs[projectID] = doc
	}

	p, err := s.projects.FindByID(ctx, projectID)
	if err != nil {
		if errors.Is(err, apperrors.ErrNotFound) {
			return nil, nil, fmt.Errorf("collab : projet %q introuvable : %w", projectID, apperrors.ErrNotFound)
		}
		return nil, nil, fmt.Errorf("collab : chargement du projet : %w", err)
	}
	if p.Board() == nil {
		return nil, nil, fmt.Errorf("collab : le projet %q n'a pas de carte", projectID)
	}
	return doc, p, nil
}

// logPath is the durable op log of a project.
func (s *Service) logPath(projectID string) string {
	return filepath.Join(s.dir, "collab", sanitizeFilePart(projectID)+".jsonl")
}

// appendLog appends the entries to the JSONL journal (best effort : le
// service reste fonctionnel sans disque, un warning est émis).
func (s *Service) appendLog(projectID string, entries []logEntry) error {
	if s.dir == "" || len(entries) == 0 {
		return nil
	}
	path := s.logPath(projectID)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for _, e := range entries {
		if err := enc.Encode(e); err != nil {
			return err
		}
	}
	return nil
}

// replay rebuilds the document bookkeeping from the durable log WITHOUT
// mutating the board: the project repository already holds the final state
// produced before the restart. Registres LWW, piles undo/redo, horloge et
// déduplication sont reconstruits pour que les opérations futures
// convergent encore.
func (s *Service) replay(projectID string, doc *Doc) error {
	path := s.logPath(projectID)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	for {
		var e logEntry
		if err := dec.Decode(&e); err != nil {
			break // fin de journal ou ligne tronquée : arrêt propre
		}
		switch e.Type {
		case "op":
			if e.Record == nil {
				continue
			}
			rec := e.Record
			if doc.seen[rec.Op.ID] {
				continue
			}
			doc.seen[rec.Op.ID] = true
			doc.acceptLamport(rec.Op.Actor, rec.Op.Lamport)
			doc.seq++
			rec.Seq = doc.seq
			if rec.Op.lww() {
				key := rec.Op.registerKey()
				stamp := fmt.Sprintf("%020d|%s", rec.Op.Lamport, rec.Op.Actor)
				if prev, ok := doc.registers[key]; !ok || stamp > prev {
					doc.registers[key] = stamp
				}
			}
			doc.log = append(doc.log, rec)
			doc.undoStack[rec.Op.Actor] = append(doc.undoStack[rec.Op.Actor], rec)
		case "undo":
			if rec := doc.popUndoByID(e.Actor, e.MovedID); rec != nil {
				doc.pushRedo(rec)
			}
		case "redo":
			if rec := doc.popRedoByID(e.Actor, e.MovedID); rec != nil {
				doc.undoStack[e.Actor] = append(doc.undoStack[e.Actor], rec)
			}
		}
	}
	return nil
}

// popByID removes and returns the stack entry whose op has the given ID.
func popByID(stack *[]*appliedOp, id string) *appliedOp {
	for i := len(*stack) - 1; i >= 0; i-- {
		if (*stack)[i].Op.ID == id {
			rec := (*stack)[i]
			*stack = append((*stack)[:i], (*stack)[i+1:]...)
			return rec
		}
	}
	return nil
}

func sanitizeFilePart(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_' {
			out = append(out, c)
		} else {
			out = append(out, '_')
		}
	}
	if len(out) == 0 {
		return utils.NewID()
	}
	return string(out)
}

// broadcast publishes the applied op to the live subscribers.
func (s *Service) broadcast(projectID string, rec *appliedOp) {
	if s.publisher == nil {
		return
	}
	s.publisher.Broadcast(projectID, collabEvent{
		Type: "collab", ProjectID: projectID, Op: rec.Op, Seq: rec.Seq,
	})
}
