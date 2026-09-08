// Command yahriacad-server is the HTTP entrypoint of the YahriaCad
// backend: it wires the configuration, the project repository (PostgreSQL
// or in-memory), the AI engine gRPC client, the application services and
// the REST/WebSocket router, then serves with graceful shutdown. The server
// starts even when the AI engine is unreachable or PostgreSQL is absent
// (contracts.md §5).
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	arenaapp "github.com/assihervey-coder/IA-PCB/backend/internal/application/arena"
	collabapp "github.com/assihervey-coder/IA-PCB/backend/internal/application/collab"
	demoapp "github.com/assihervey-coder/IA-PCB/backend/internal/application/demo"
	dfmapp "github.com/assihervey-coder/IA-PCB/backend/internal/application/dfm"
	doctorapp "github.com/assihervey-coder/IA-PCB/backend/internal/application/doctor"
	exportapp "github.com/assihervey-coder/IA-PCB/backend/internal/application/export"
	layoutapp "github.com/assihervey-coder/IA-PCB/backend/internal/application/layout"
	magicapp "github.com/assihervey-coder/IA-PCB/backend/internal/application/magic"
	schematicapp "github.com/assihervey-coder/IA-PCB/backend/internal/application/schematic"
	statsapp "github.com/assihervey-coder/IA-PCB/backend/internal/application/stats"
	timemachineapp "github.com/assihervey-coder/IA-PCB/backend/internal/application/timemachine"
	verificationapp "github.com/assihervey-coder/IA-PCB/backend/internal/application/verification"
	domainconstraints "github.com/assihervey-coder/IA-PCB/backend/internal/domain/constraints"
	domainlayout "github.com/assihervey-coder/IA-PCB/backend/internal/domain/layout"
	domainproject "github.com/assihervey-coder/IA-PCB/backend/internal/domain/project"
	domainschematic "github.com/assihervey-coder/IA-PCB/backend/internal/domain/schematic"
	"github.com/assihervey-coder/IA-PCB/backend/internal/infrastructure/ai"
	"github.com/assihervey-coder/IA-PCB/backend/internal/infrastructure/api/rest"
	yahriacadws "github.com/assihervey-coder/IA-PCB/backend/internal/infrastructure/api/websocket"
	"github.com/assihervey-coder/IA-PCB/backend/internal/infrastructure/auth"
	"github.com/assihervey-coder/IA-PCB/backend/internal/infrastructure/config"
	"github.com/assihervey-coder/IA-PCB/backend/internal/infrastructure/fileio/reader"
	"github.com/assihervey-coder/IA-PCB/backend/internal/infrastructure/fileio/writer"
	memory "github.com/assihervey-coder/IA-PCB/backend/internal/infrastructure/persistence/memory"
	sql "github.com/assihervey-coder/IA-PCB/backend/internal/infrastructure/persistence/sql"
	"github.com/assihervey-coder/IA-PCB/backend/internal/pkg/logger"
)

// version is overridden at build time (-ldflags "-X main.version=…").
var version = "dev"

// shutdownTimeout bounds the graceful drain of in-flight requests.
const shutdownTimeout = 10 * time.Second

// unreachableAI is the fallback AIService used when the gRPC client cannot
// even be constructed: every call fails with the sentinel so the REST layer
// answers 503 ai_unreachable instead of panicking on a nil port.
type unreachableAI struct{}

func (unreachableAI) Health(context.Context) error { return layoutapp.ErrAIUnreachable }

func (unreachableAI) EngineInfo(context.Context) (layoutapp.EngineInfo, error) {
	return layoutapp.EngineInfo{}, layoutapp.ErrAIUnreachable
}

func (unreachableAI) ModelInfo(context.Context) (layoutapp.ModelInfo, error) {
	return layoutapp.ModelInfo{}, layoutapp.ErrAIUnreachable
}

func (unreachableAI) ReloadModel(context.Context, string) (layoutapp.ModelInfo, string, error) {
	return layoutapp.ModelInfo{}, "", layoutapp.ErrAIUnreachable
}

func (unreachableAI) PlanPlacement(context.Context, *domainlayout.Board,
	[]domainschematic.Component, string) ([]domainlayout.PlacedComponent, error) {
	return nil, layoutapp.ErrAIUnreachable
}

func (unreachableAI) RouteBoard(context.Context, *domainlayout.Board,
	[]domainschematic.Net, *domainconstraints.ConstraintSet, string,
	func(layoutapp.RouteProgress)) (*layoutapp.RouteOutcome, error) {
	return nil, layoutapp.ErrAIUnreachable
}

func (unreachableAI) OptimizeRoutes(context.Context, *domainlayout.Board,
	*layoutapp.RouteOutcome, *domainconstraints.ConstraintSet,
	func(layoutapp.RouteProgress)) (*layoutapp.RouteOutcome, error) {
	return nil, layoutapp.ErrAIUnreachable
}

func main() {
	cfg := config.Load()
	log := logger.New(cfg.LogLevel)

	// --------------------------------------------------------------
	// Persistance : PostgreSQL si configuré, sinon adaptateur mémoire.
	// --------------------------------------------------------------
	repo, mode := buildRepository(cfg, log)

	// --------------------------------------------------------------
	// Hub WebSocket + moteur IA.
	// --------------------------------------------------------------
	hub := yahriacadws.NewHub(log)

	aiService, aiClient := buildAIService(cfg, log)

	// --------------------------------------------------------------
	// Services applicatifs.
	// --------------------------------------------------------------
	registry := layoutapp.NewJobRegistry()
	readers := reader.NewRegistry(log)

	importSvc := schematicapp.NewImportService(repo, readers, log)
	validationSvc := schematicapp.NewValidationService(repo, log)
	placeSvc := layoutapp.NewPlaceService(repo, aiService, log)
	routeSvc := layoutapp.NewRouteService(repo, aiService, registry, hub, log)
	optimizeSvc := layoutapp.NewOptimizeService(repo, aiService, registry, hub, log)
	drcChecker := verificationapp.NewDRCChecker(repo)
	ercChecker := verificationapp.NewERCChecker(repo)
	gerberSvc := exportapp.NewGerberService(repo, writer.NewGerberWriter(), cfg.DataDir, log)
	bomSvc := exportapp.NewBOMService(repo, log)
	kicadSvc := exportapp.NewKicadService(repo, writer.NewKicadWriter(), cfg.DataDir, log)
	stepSvc := exportapp.NewSTEPService(repo, writer.NewStepWriter(), cfg.DataDir, log)
	odbSvc := exportapp.NewODBService(repo, writer.NewODBPPWriter(), cfg.DataDir, log)

	// Magic Pack : copilot langage naturel, thermique, oracle d'œil, arène.
	magicSvc := magicapp.NewMagicService(repo, log)
	thermalSvc := verificationapp.NewThermalChecker(repo, 25.0)
	siSvc := verificationapp.NewSIChecker(repo)
	arenaSvc := arenaapp.NewArenaService(repo, log)
	// Benchmark A* vs RL : le fighter RL passe par le moteur IA (le
	// championnat classique greedy/astar reste purement local).
	arenaSvc.AttachAI(aiService)

	// Pack WOW : auto-healer DRC, design doctor, oracle DFM, time machine,
	// stats live (additif, hors contrat figé).
	autoFixSvc := verificationapp.NewAutoFixService(repo, drcChecker, log)
	doctorSvc := doctorapp.NewDoctorService(repo, drcChecker, ercChecker, thermalSvc, siSvc, aiService, log)
	dfmSvc := dfmapp.NewService(repo, log)
	timeMachineSvc := timemachineapp.NewService(repo, log)
	statsSvc := statsapp.NewService(repo, log)
	pourSvc := layoutapp.NewPourService(repo, log)
	netClassSvc := layoutapp.NewNetClassService(repo)
	collabSvc := collabapp.NewService(repo, hub, cfg.DataDir, log)
	demoSvc := demoapp.NewService(repo, log)
	impedanceSvc := verificationapp.NewImpedanceService(repo)

	// --------------------------------------------------------------
	// Authentification JWT (durcissement production, contracts.md §13).
	// --------------------------------------------------------------
	authService := buildAuthService(cfg, log)

	handler := rest.NewRouter(rest.Deps{
		Projects:       repo,
		Import:         importSvc,
		Validation:     validationSvc,
		Place:          placeSvc,
		Route:          routeSvc,
		Optimize:       optimizeSvc,
		DRC:            drcChecker,
		ERC:            ercChecker,
		Thermal:        thermalSvc,
		SI:             siSvc,
		Magic:          magicSvc,
		Arena:          arenaSvc,
		AutoFix:        autoFixSvc,
		Doctor:         doctorSvc,
		DFM:            dfmSvc,
		TimeMachine:    timeMachineSvc,
		Stats:          statsSvc,
		Pours:          pourSvc,
		NetClasses:     netClassSvc,
		Collab:         collabSvc,
		Demo:           demoSvc,
		Impedance:      impedanceSvc,
		Gerber:         gerberSvc,
		BOM:            bomSvc,
		STEP:           stepSvc,
		ODB:            odbSvc,
		Kicad:          kicadSvc,
		Hub:            hub,
		Logger:         log,
		Version:        version,
		AI:             aiService,
		Database:       mode,
		Auth:           authService,
		AllowedOrigins: cfg.AllowedOrigins,
		AIRateRPS:      cfg.AIRateRPS,
		AIRateBurst:    cfg.AIRateBurst,
	})

	server := &http.Server{
		Addr:              ":" + cfg.HTTPPort,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	log.Info("YahriaCad backend démarré",
		"version", version,
		"port", cfg.HTTPPort,
		"database", mode,
		"ai_addr", cfg.AIAddr,
		"data_dir", cfg.DataDir,
		"app_env", cfg.AppEnv,
		"auth", authService != nil)

	// Arrêt propre sur SIGINT/SIGTERM.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		log.Error("serveur HTTP arrêté sur erreur", "err", err)
		shutdownHTTP(server, log)
		closeAI(aiClient, log)
		os.Exit(1)
	case <-ctx.Done():
		log.Info("signal d'arrêt reçu : arrêt gracieux")
		shutdownHTTP(server, log)
		closeAI(aiClient, log)
		if closer, ok := repo.(interface{ Close() error }); ok {
			if err := closer.Close(); err != nil {
				log.Warn("fermeture du dépôt", "err", err)
			}
		}
		log.Info("serveur arrêté")
	}
}

// buildAuthService instantiates the JWT service when the configuration
// enables it. In production a missing secret aborts the startup (fail fast);
// in development an ephemeral secret is generated (tokens die with the
// process, acceptable locally) so the protected mode is testable out of the
// box. When auth stays disabled in production a loud warning is logged.
func buildAuthService(cfg config.Config, log *slog.Logger) *auth.Service {
	if !cfg.AuthEnabled {
		if cfg.AppEnv == "production" {
			log.Warn("API OUVERTE : authentification désactivée en production " +
				"(configurez YAHRIACAD_JWT_SECRET ou YAHRIACAD_AUTH_ENABLED=true)")
		}
		return nil
	}

	secret := strings.TrimSpace(cfg.JWTSecret)
	if secret == "" {
		if cfg.AppEnv == "production" {
			log.Error("auth activée sans secret JWT en production : démarrage impossible " +
				"(YAHRIACAD_JWT_SECRET requis)")
			os.Exit(1)
		}
		generated, err := auth.GenerateEphemeralSecret()
		if err != nil {
			log.Error("secret JWT éphémère impossible", "err", err)
			os.Exit(1)
		}
		secret = generated
		log.Warn("secret JWT absent : secret éphémère généré (les jetons expirent au redémarrage, dev)")
	}

	svc := auth.NewService(secret, cfg.JWTTTL, cfg.AuthUsers, log)
	log.Info("authentification JWT activée (HS256)",
		"utilisateurs", svc.UserCount(),
		"ttl", cfg.JWTTTL.String())
	return svc
}

// buildRepository selects the persistence adapter: PostgreSQL when a DSN is
// configured, the in-memory repository otherwise. A configured but
// unreachable database aborts the startup (fail fast) so misconfigurations
// are visible.
func buildRepository(cfg config.Config, log *slog.Logger) (domainproject.Repository, string) {
	if strings.TrimSpace(cfg.DBURL) == "" {
		log.Info("dépôt de projets : adaptateur mémoire (PostgreSQL non configuré)")
		return memory.NewProjectRepository(), "memory"
	}
	repo, err := sql.NewProjectRepository(cfg.DBURL)
	if err != nil {
		log.Error("base de données injoignable : démarrage impossible", "err", err)
		os.Exit(1)
	}
	log.Info("dépôt de projets : PostgreSQL", "dsn", maskDSN(cfg.DBURL))
	return repo, "postgres"
}

// buildAIService wires the gRPC client toward the AI engine and probes it
// once at startup. A unreachable engine only logs a warning: the server
// must start anyway (contracts.md §5).
func buildAIService(cfg config.Config, log *slog.Logger) (layoutapp.AIService, *ai.Client) {
	client, err := ai.NewClient(cfg.AIAddr, 2*time.Second, log)
	if err != nil {
		log.Warn("client IA non initialisé : routes IA en échec", "addr", cfg.AIAddr, "err", err)
		return unreachableAI{}, nil
	}

	probeCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	if err := client.Health(probeCtx); err != nil {
		log.Warn("moteur IA injoignable au démarrage : routes IA dégradées", "addr", cfg.AIAddr, "err", err)
	} else {
		log.Info("moteur IA joignable", "addr", cfg.AIAddr)
	}
	cancel()
	return client, client
}

// shutdownHTTP drains the connections within the shutdown timeout.
func shutdownHTTP(server *http.Server, log *slog.Logger) {
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		log.Warn("arrêt gracieux incomplet", "err", err)
	}
}

// closeAI releases the gRPC connection.
func closeAI(client *ai.Client, log *slog.Logger) {
	if client == nil {
		return
	}
	if err := client.Close(); err != nil {
		log.Warn("fermeture du client IA", "err", err)
	}
}

// maskDSN hides the password of a postgres:// URL before logging it.
func maskDSN(dsn string) string {
	if i := strings.Index(dsn, "://"); i >= 0 {
		if j := strings.Index(dsn[i+3:], "@"); j >= 0 {
			credentials := dsn[i+3 : i+3+j]
			if k := strings.Index(credentials, ":"); k >= 0 {
				return dsn[:i+3+k+1] + "****" + dsn[i+3+j:]
			}
		}
	}
	return dsn
}
