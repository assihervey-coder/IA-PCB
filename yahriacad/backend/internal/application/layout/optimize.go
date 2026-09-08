package layoutapp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	apperrors "github.com/assihervey-coder/IA-PCB/backend/internal/application"
	domainconstraints "github.com/assihervey-coder/IA-PCB/backend/internal/domain/constraints"
	domainlayout "github.com/assihervey-coder/IA-PCB/backend/internal/domain/layout"
	domainproject "github.com/assihervey-coder/IA-PCB/backend/internal/domain/project"
)

// OptimizeService runs the post-routing optimisation use case (rip-up &
// reroute, via reduction). It shares the job registry with RouteService.
type OptimizeService struct {
	projects  domainproject.Repository
	ai        AIService
	registry  *JobRegistry
	publisher ProgressPublisher
	log       *slog.Logger
}

// NewOptimizeService builds the optimisation use case.
func NewOptimizeService(projects domainproject.Repository, ai AIService, reg *JobRegistry,
	pub ProgressPublisher, log *slog.Logger) *OptimizeService {
	return &OptimizeService{projects: projects, ai: ai, registry: reg, publisher: pub, log: log}
}

// Start launches an asynchronous optimisation job and returns its
// identifier. The current routing of the board is snapshot into a
// RouteOutcome that the AI engine improves.
func (s *OptimizeService) Start(ctx context.Context, projectID string) (string, error) {
	p, err := s.projects.FindByID(ctx, projectID)
	if err != nil {
		if errors.Is(err, apperrors.ErrNotFound) {
			return "", fmt.Errorf("optimisation : projet %q introuvable : %w", projectID, apperrors.ErrNotFound)
		}
		return "", fmt.Errorf("optimisation : chargement du projet : %w", err)
	}
	if p.Board() == nil {
		return "", fmt.Errorf("optimisation : le projet %q n'a pas de carte", projectID)
	}

	outcome := outcomeFromBoard(p.Board())
	job := s.registry.Create(projectID, KindOptimize, len(outcome.Nets))

	go s.run(context.WithoutCancel(ctx), job, projectID, &outcome)
	return job.Info().JobID, nil
}

// JobStatus returns the current snapshot of a job owned by this service.
func (s *OptimizeService) JobStatus(jobID string) (JobInfo, bool) {
	h, ok := s.registry.Get(jobID)
	if !ok {
		return JobInfo{}, false
	}
	return h.Info(), true
}

// ListJobs returns every job known to the registry (route + optimize).
func (s *OptimizeService) ListJobs() []JobInfo { return s.registry.List() }

// run executes the optimisation job on a detached context.
func (s *OptimizeService) run(ctx context.Context, job *JobHandle, projectID string, current *RouteOutcome) {
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

	onProgress := func(pr RouteProgress) {
		job.SetProgress(pr.Percent, pr.CurrentNet, pr.Message)
		if pr.JobID == "" {
			pr.JobID = job.Info().JobID
		}
		pr.Stage = KindOptimize
		s.publisher.Publish(pr.JobID, projectID, pr)
	}

	optimized, err := s.ai.OptimizeRoutes(ctx, board, current, cs, onProgress)
	if err != nil {
		s.failJob(ctx, job, projectID, err)
		return
	}

	for i := range optimized.Nets {
		nr := &optimized.Nets[i]
		board.ReplaceRoutesForNet(nr.Net, nr.Tracks, nr.Vias)
		job.IncNetsDone(1)
	}

	p.SetBoard(board)
	if err := s.projects.Update(ctx, p); err != nil {
		s.failJob(ctx, job, projectID, fmt.Errorf("persistance du projet : %w", err))
		return
	}

	job.SetProgress(100, "", "optimisation terminée")
	job.SetState(StateDone)
	s.publisher.Publish(job.Info().JobID, projectID, RouteProgress{
		JobID: job.Info().JobID, Stage: KindOptimize,
		Message: "optimisation terminée", Percent: 100, Done: true,
	})
	s.log.Info("optimisation terminée",
		"project_id", projectID,
		"job_id", job.Info().JobID,
		"nets", len(optimized.Nets),
		"duration_ms", optimized.DurationMS)
}

// failJob marks the job as failed and notifies the subscribers.
func (s *OptimizeService) failJob(ctx context.Context, job *JobHandle, projectID string, cause error) {
	if errors.Is(cause, context.Canceled) {
		job.SetState(StateCancelled)
		return
	}
	msg := cause.Error()
	job.SetError(msg)
	job.SetState(StateFailed)
	s.publisher.Publish(job.Info().JobID, projectID, RouteProgress{
		JobID: job.Info().JobID, Stage: KindOptimize,
		Message: "échec de l'optimisation", Percent: 100, Done: true, Err: msg,
	})
	s.log.Warn("optimisation échouée", "project_id", projectID, "job_id", job.Info().JobID, "err", msg)
}

// outcomeFromBoard snapshots the current board routing into a RouteOutcome:
// tracks and vias are grouped per net in first-appearance order.
func outcomeFromBoard(board *domainlayout.Board) RouteOutcome {
	outcome := RouteOutcome{Strategy: "current"}
	index := map[string]int{} // net -> index in outcome.Nets

	ensure := func(net string) *NetRoute {
		if i, ok := index[net]; ok {
			return &outcome.Nets[i]
		}
		outcome.Nets = append(outcome.Nets, NetRoute{Net: net})
		index[net] = len(outcome.Nets) - 1
		return &outcome.Nets[len(outcome.Nets)-1]
	}

	for _, t := range board.Tracks {
		if t.Net == "" {
			continue
		}
		nr := ensure(t.Net)
		nr.LengthMM += t.Length()
		nr.Tracks = append(nr.Tracks, t)
	}
	for _, v := range board.Vias {
		if v.Net == "" {
			continue
		}
		nr := ensure(v.Net)
		nr.Vias = append(nr.Vias, v)
	}
	for i := range outcome.Nets {
		outcome.Nets[i].Completed = true
	}
	return outcome
}
