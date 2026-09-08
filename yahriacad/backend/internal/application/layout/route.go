package layoutapp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	apperrors "github.com/assihervey-coder/IA-PCB/backend/internal/application"
	domainconstraints "github.com/assihervey-coder/IA-PCB/backend/internal/domain/constraints"
	domainlayout "github.com/assihervey-coder/IA-PCB/backend/internal/domain/layout"
	domainproject "github.com/assihervey-coder/IA-PCB/backend/internal/domain/project"
	domainschematic "github.com/assihervey-coder/IA-PCB/backend/internal/domain/schematic"
	"github.com/assihervey-coder/IA-PCB/backend/internal/pkg/utils"
)

// Job states (REST JobStatus.state).
const (
	StatePending   = "pending"
	StateRunning   = "running"
	StateDone      = "done"
	StateFailed    = "failed"
	StateCancelled = "cancelled"
)

// Job kinds.
const (
	KindRoute    = "route"
	KindOptimize = "optimize"
)

// JobInfo is the observable snapshot of an asynchronous job (route or
// optimize). It maps 1:1 onto the REST JobStatus DTO.
type JobInfo struct {
	JobID      string     `json:"job_id"`
	ProjectID  string     `json:"project_id"`
	Kind       string     `json:"kind"`
	State      string     `json:"state"`
	Percent    float64    `json:"percent"`
	CurrentNet string     `json:"current_net"`
	Message    string     `json:"message"`
	Error      string     `json:"error"`
	NetsDone   int        `json:"nets_done"`
	NetsTotal  int        `json:"nets_total"`
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at"`
}

// JobHandle is a mutable handle on a running job. All accessors are
// goroutine-safe: the job runs in its own goroutine while the REST layer
// polls the registry.
type JobHandle struct {
	mu   sync.Mutex
	info JobInfo
}

// Info returns a consistent snapshot of the job state.
func (h *JobHandle) Info() JobInfo {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.info
}

// SetRunning switches the job from pending to running.
func (h *JobHandle) SetRunning() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.info.State = StateRunning
}

// SetProgress records the latest progress event.
func (h *JobHandle) SetProgress(percent float64, currentNet, message string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.info.Percent = percent
	h.info.CurrentNet = currentNet
	h.info.Message = message
}

// IncNetsDone increments the completed-net counter by n.
func (h *JobHandle) IncNetsDone(n int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.info.NetsDone += n
}

// SetState sets the terminal state of the job ("done" | "failed" |
// "cancelled") and stamps the finish time.
func (h *JobHandle) SetState(state string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.info.State = state
	now := time.Now().UTC()
	h.info.FinishedAt = &now
}

// SetError records the failure reason (additive helper on top of the
// contract; the JobStatus payload carries a dedicated error field).
func (h *JobHandle) SetError(message string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.info.Error = message
}

// JobRegistry is the shared registry of asynchronous jobs (route +
// optimize). It is safe for concurrent use.
type JobRegistry struct {
	jobs sync.Map // map[string]*JobHandle
}

// NewJobRegistry builds an empty registry.
func NewJobRegistry() *JobRegistry { return &JobRegistry{} }

// Create registers a new job and returns its handle.
func (r *JobRegistry) Create(projectID, kind string, netsTotal int) *JobHandle {
	h := &JobHandle{info: JobInfo{
		JobID:     utils.NewID(),
		ProjectID: projectID,
		Kind:      kind,
		State:     StatePending,
		NetsTotal: netsTotal,
		StartedAt: time.Now().UTC(),
	}}
	r.jobs.Store(h.info.JobID, h)
	return h
}

// Get looks a job up by identifier.
func (r *JobRegistry) Get(jobID string) (*JobHandle, bool) {
	v, ok := r.jobs.Load(jobID)
	if !ok {
		return nil, false
	}
	h, ok := v.(*JobHandle)
	return h, ok
}

// List returns every job, newest first.
func (r *JobRegistry) List() []JobInfo {
	var out []JobInfo
	r.jobs.Range(func(_, v any) bool {
		if h, ok := v.(*JobHandle); ok {
			out = append(out, h.Info())
		}
		return true
	})
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt.After(out[j].StartedAt) })
	return out
}

// ProgressPublisher is the real-time publication port (implemented by
// api/websocket.Hub).
type ProgressPublisher interface {
	Publish(jobID, projectID string, p RouteProgress)
}

// RouteService runs the automatic routing use case. Start returns
// immediately with a job identifier; the actual work happens in a detached
// goroutine that reports progress through the registry and the publisher.
type RouteService struct {
	projects  domainproject.Repository
	ai        AIService
	registry  *JobRegistry
	publisher ProgressPublisher
	log       *slog.Logger
}

// NewRouteService builds the routing use case.
func NewRouteService(projects domainproject.Repository, ai AIService, reg *JobRegistry,
	pub ProgressPublisher, log *slog.Logger) *RouteService {
	return &RouteService{projects: projects, ai: ai, registry: reg, publisher: pub, log: log}
}

// Start launches an asynchronous routing job and returns its identifier.
func (s *RouteService) Start(ctx context.Context, projectID, strategy string,
	netFilter []string) (jobID string, err error) {
	p, err := s.projects.FindByID(ctx, projectID)
	if err != nil {
		if errors.Is(err, apperrors.ErrNotFound) {
			return "", fmt.Errorf("routage : projet %q introuvable : %w", projectID, apperrors.ErrNotFound)
		}
		return "", fmt.Errorf("routage : chargement du projet : %w", err)
	}

	nets := netsForProject(p, netFilter)
	job := s.registry.Create(projectID, KindRoute, len(nets))

	go s.run(context.WithoutCancel(ctx), job, projectID, strategy, nets)
	return job.Info().JobID, nil
}

// JobStatus returns the current snapshot of a job owned by this service.
func (s *RouteService) JobStatus(jobID string) (JobInfo, bool) {
	h, ok := s.registry.Get(jobID)
	if !ok {
		return JobInfo{}, false
	}
	return h.Info(), true
}

// ListJobs returns every job known to the registry (route + optimize).
func (s *RouteService) ListJobs() []JobInfo { return s.registry.List() }

// run executes the routing job. ctx is detached (WithoutCancel) so the job
// survives the HTTP request that started it.
func (s *RouteService) run(ctx context.Context, job *JobHandle, projectID, strategy string,
	nets []domainschematic.Net) {
	job.SetRunning()

	p, err := s.projects.FindByID(ctx, projectID)
	if err != nil {
		s.failJob(ctx, job, projectID, fmt.Errorf("chargement du projet : %w", err))
		return
	}
	board := p.Board()
	if board == nil {
		s.failJob(ctx, job, projectID, errors.New("le projet n'a pas de carte : importez d'abord un design"))
		return
	}
	cs := p.Constraints()
	if cs == nil {
		cs = domainconstraints.NewDefault()
	}

	onProgress := s.progressFunc(job, projectID)
	outcome, err := s.ai.RouteBoard(ctx, board, nets, cs, strategy, onProgress)
	if err != nil {
		s.failJob(ctx, job, projectID, err)
		return
	}

	for i := range outcome.Nets {
		nr := &outcome.Nets[i]
		board.ReplaceRoutesForNet(nr.Net, nr.Tracks, nr.Vias)
		job.IncNetsDone(1)
	}

	p.SetBoard(board)
	if err := s.projects.Update(ctx, p); err != nil {
		s.failJob(ctx, job, projectID, fmt.Errorf("persistance du projet : %w", err))
		return
	}

	job.SetProgress(100, "", "routage terminé")
	job.SetState(StateDone)
	s.publish(job, projectID, RouteProgress{
		Stage: KindRoute, Message: "routage terminé", Percent: 100, Done: true,
	})
	s.log.Info("routage terminé",
		"project_id", projectID,
		"job_id", job.Info().JobID,
		"strategy", strategy,
		"nets", len(outcome.Nets),
		"duration_ms", outcome.DurationMS)
}

// progressFunc returns the callback handed to the AI port: it updates the
// job handle and publishes the event in real time.
func (s *RouteService) progressFunc(job *JobHandle, projectID string) func(RouteProgress) {
	return func(pr RouteProgress) {
		job.SetProgress(pr.Percent, pr.CurrentNet, pr.Message)
		if pr.JobID == "" {
			pr.JobID = job.Info().JobID
		}
		s.publisher.Publish(pr.JobID, projectID, pr)
	}
}

// publish sends a progress event through the publisher, defaulting the job
// identifier.
func (s *RouteService) publish(job *JobHandle, projectID string, pr RouteProgress) {
	if pr.JobID == "" {
		pr.JobID = job.Info().JobID
	}
	s.publisher.Publish(pr.JobID, projectID, pr)
}

// failJob marks the job as failed and notifies the subscribers.
func (s *RouteService) failJob(ctx context.Context, job *JobHandle, projectID string, cause error) {
	if errors.Is(cause, context.Canceled) {
		job.SetState(StateCancelled)
		return
	}
	msg := cause.Error()
	job.SetError(msg)
	job.SetState(StateFailed)
	s.publish(job, projectID, RouteProgress{
		Stage: KindRoute, Message: "échec du routage", Percent: 100, Done: true, Err: msg,
	})
	s.log.Warn("routage échoué", "project_id", projectID, "job_id", job.Info().JobID, "err", msg)
}

// netsForProject builds the net list handed to the AI engine, honouring the
// optional filter. Nets come from the schematic, or are derived from the
// board pads when the project has no schematic yet.
func netsForProject(p *domainproject.Project, filter []string) []domainschematic.Net {
	var nets []domainschematic.Net
	if sch := p.Schematic(); sch != nil && len(sch.Nets) > 0 {
		nets = append([]domainschematic.Net(nil), sch.Nets...)
	} else {
		nets = netsFromBoard(p.Board())
	}
	if len(filter) == 0 {
		return nets
	}
	keep := make(map[string]bool, len(filter))
	for _, n := range filter {
		keep[n] = true
	}
	out := make([]domainschematic.Net, 0, len(filter))
	for _, n := range nets {
		if keep[n.Name] {
			out = append(out, n)
		}
	}
	return out
}

// netsFromBoard derives the logical nets from the pads already present on
// the board (fallback when no schematic has been imported).
func netsFromBoard(board *domainlayout.Board) []domainschematic.Net {
	if board == nil {
		return nil
	}
	index := map[string]*domainschematic.Net{}
	var order []string
	for i := range board.Components {
		bc := &board.Components[i]
		for _, pad := range bc.Footprint.Pads {
			if pad.Net == "" {
				continue
			}
			n, ok := index[pad.Net]
			if !ok {
				n = &domainschematic.Net{Name: pad.Net, Class: domainschematic.ClassDefault}
				index[pad.Net] = n
				order = append(order, pad.Net)
			}
			n.Connections = append(n.Connections, domainschematic.PinRef{
				ComponentRef: bc.Ref, PinNumber: pad.Name,
			})
		}
	}
	out := make([]domainschematic.Net, 0, len(order))
	for _, name := range order {
		out = append(out, *index[name])
	}
	return out
}
