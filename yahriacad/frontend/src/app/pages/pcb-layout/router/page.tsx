"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { api, isBackendDown } from "@/lib/api/rest-client";
import type { AIStrategy, JobStatus, ProgressEvent, RouteNetResult } from "@/lib/api/types";
import { ProgressSocket, type SocketStatus } from "@/lib/api/ws-client";
import { useProjectStore } from "@/lib/store/project-store";
import { useUIStore } from "@/lib/store/ui-store";
import { Button } from "@/app/components/buttons/Button";
import { SelectField } from "@/app/components/forms/SelectField";
import { colorForNet, percentFormat } from "@/lib/utils/converters";

interface LogLine {
  id: number;
  time: string;
  text: string;
  error: boolean;
}

const STRATEGY_OPTIONS = [
  { value: "astar", label: "A* (déterministe)" },
  { value: "rl", label: "Apprentissage par renforcement (RL)" },
];

const STATE_LABEL: Record<JobStatus["state"], string> = {
  pending: "En attente",
  running: "En cours",
  done: "Terminé",
  failed: "Échec",
  cancelled: "Annulé",
};

const SOCKET_LABEL: Record<SocketStatus, string> = {
  connecting: "WebSocket : connexion…",
  open: "WebSocket : connecté (temps réel)",
  closed: "WebSocket : reconnexion…",
  error: "WebSocket indisponible — sondage REST toutes les 2 s",
};

const POLL_INTERVAL_MS = 2000;
const SIM_STEP_MS = 320;

export default function RouterPage() {
  const layout = useProjectStore((s) => s.layout);
  const currentProject = useProjectStore((s) => s.currentProject);
  const demoMode = useProjectStore((s) => s.demoMode);
  const lastJob = useProjectStore((s) => s.lastJob);
  const applyProgressEvent = useProjectStore((s) => s.applyProgressEvent);
  const setLastJob = useProjectStore((s) => s.setLastJob);
  const refreshLayout = useProjectStore((s) => s.refreshLayout);
  const loadDemoData = useProjectStore((s) => s.loadDemoData);
  const pushToast = useUIStore((s) => s.pushToast);

  const [projectId, setProjectId] = useState<string | null>(null);
  const [strategy, setStrategy] = useState<AIStrategy>("astar");
  const [disabledNets, setDisabledNets] = useState<Set<string>>(new Set());
  const [results, setResults] = useState<RouteNetResult[]>([]);
  const [logs, setLogs] = useState<LogLine[]>([]);
  const [socketStatus, setSocketStatus] = useState<SocketStatus | null>(null);
  const [starting, setStarting] = useState(false);

  const socketRef = useRef<ProgressSocket | null>(null);
  const pollRef = useRef<ReturnType<typeof setInterval> | null>(null);
  const simTimersRef = useRef<Array<ReturnType<typeof setTimeout>>>([]);
  const jobRef = useRef<JobStatus | null>(null);
  const projectIdRef = useRef<string | null>(null);
  const logSeq = useRef(0);

  jobRef.current = lastJob;
  projectIdRef.current = projectId ?? currentProject?.id ?? null;

  const openProject = useProjectStore((s) => s.openProject);

  // Chargement du projet (query ?project=) ou des données de démonstration.
  useEffect(() => {
    const id = new URLSearchParams(window.location.search).get("project");
    setProjectId(id);
    if (id) {
      void openProject(id);
    } else {
      void loadDemoData();
    }
  }, [openProject, loadDemoData]);

  /** Ajoute une ligne de journal + met à jour le job + capture les résultats partiels. */
  const handleEvent = useCallback(
    (event: ProgressEvent) => {
      applyProgressEvent(event);
      const partial = event.partial;
      const time = new Date().toLocaleTimeString("fr-FR");
      let text = "";
      if (event.error) {
        text = event.error;
      } else {
        const parts: string[] = [];
        if (event.current_net) parts.push(`net ${event.current_net}`);
        if (event.message) parts.push(event.message);
        text = parts.join(" — ");
        if (!text) text = "progression";
        text = `${text} (${Math.round(event.percent)} %)`;
      }
      setLogs((prev) => {
        const next = [...prev, { id: ++logSeq.current, time, text, error: Boolean(event.error) }];
        return next.length > 400 ? next.slice(next.length - 400) : next;
      });
      if (partial) {
        setResults((prev) => {
          const idx = prev.findIndex((r) => r.net === partial.net);
          if (idx === -1) return [...prev, partial];
          const next = prev.slice();
          next[idx] = partial;
          return next;
        });
      }
    },
    [applyProgressEvent],
  );

  const patchJob = useCallback(
    (patch: Partial<JobStatus>) => {
      const current = useProjectStore.getState().lastJob;
      if (current) setLastJob({ ...current, ...patch });
    },
    [setLastJob],
  );

  const stopEverything = useCallback(() => {
    socketRef.current?.close();
    socketRef.current = null;
    if (pollRef.current) {
      clearInterval(pollRef.current);
      pollRef.current = null;
    }
    for (const t of simTimersRef.current) clearTimeout(t);
    simTimersRef.current = [];
  }, []);

  // Nettoyage au démontage : socket, sondage, simulateurs.
  useEffect(() => stopEverything, [stopEverything]);

  const nets = layout?.nets.map((n) => n.name) ?? [];
  const selectedNets = nets.filter((n) => !disabledNets.has(n));
  const jobRunning = lastJob?.state === "running" || lastJob?.state === "pending";

  /**
   * Repli mode démo : job simulé localement (événements identiques au contrat
   * ProgressEvent) quand le backend est injoignable.
   */
  const simulateDemoRoute = useCallback(
    (pid: string, netNames: string[], strat: AIStrategy) => {
      const jobId = `demo-route-${Date.now().toString(36)}`;
      setLastJob({
        job_id: jobId,
        project_id: pid,
        kind: "route",
        state: "running",
        percent: 0,
        current_net: "",
        message: `Simulation locale (stratégie ${strat})`,
        error: "",
        nets_done: 0,
        nets_total: netNames.length,
        started_at: new Date().toISOString(),
        finished_at: null,
      });
      const tracks = useProjectStore.getState().layout?.tracks ?? [];
      let index = 0;
      const step = () => {
        if (index >= netNames.length) {
          handleEvent({
            job_id: jobId,
            stage: "route",
            current_net: "",
            percent: 100,
            message: `Routage simulé terminé — ${netNames.length} nets`,
            partial: null,
            done: true,
            error: "",
          });
          simTimersRef.current = [];
          return;
        }
        const net = netNames[index];
        const track = tracks.find((t) => t.net === net);
        const length = track
          ? track.points.reduce((acc, p, i) => {
              if (i === 0) return 0;
              const prev = track.points[i - 1];
              return acc + Math.hypot(p.x - prev.x, p.y - prev.y);
            }, 0)
          : 9.4 + index * 2.9;
        const partial: RouteNetResult = {
          net,
          segments: [],
          vias: [],
          length_mm: Math.round(length * 100) / 100,
          completed: true,
          drc_violations: 0,
        };
        handleEvent({
          job_id: jobId,
          stage: "route",
          current_net: net,
          percent: Math.round(((index + 1) / netNames.length) * 100),
          message: `net routé (${strat})`,
          partial,
          done: false,
          error: "",
        });
        patchJob({ nets_done: index + 1 });
        index += 1;
        simTimersRef.current.push(setTimeout(step, SIM_STEP_MS));
      };
      simTimersRef.current.push(setTimeout(step, 350));
    },
    [handleEvent, patchJob, setLastJob],
  );

  const startRoute = async () => {
    if (starting || jobRunning || selectedNets.length === 0) return;
    stopEverything();
    setResults([]);
    setLogs([]);
    setSocketStatus(null);
    setStarting(true);
    const pid = projectIdRef.current ?? "demo-001";
    try {
      const started = await api.startRoute(pid, { strategy, nets: selectedNets });
      setLastJob({
        job_id: started.job_id,
        project_id: started.project_id,
        kind: "route",
        state: "pending",
        percent: 0,
        current_net: "",
        message: "Job créé, en attente du moteur IA…",
        error: "",
        nets_done: 0,
        nets_total: selectedNets.length,
        started_at: new Date().toISOString(),
        finished_at: null,
      });
      socketRef.current = new ProgressSocket(started.project_id, handleEvent, setSocketStatus);
      pushToast(`Routage lancé (job ${started.job_id.slice(0, 8)}…).`, "info");
    } catch (err) {
      if (isBackendDown(err)) {
        pushToast("Mode démo : simulation locale du routage.", "info");
        simulateDemoRoute(pid, selectedNets, strategy);
      } else {
        pushToast("Impossible de démarrer le routage IA.", "error");
      }
    } finally {
      setStarting(false);
    }
  };

  // Sondage REST de secours quand le WebSocket ne s'ouvre pas.
  useEffect(() => {
    const active = Boolean(lastJob && (lastJob.state === "running" || lastJob.state === "pending"));
    const needPoll = active && (socketStatus === "error" || socketStatus === "closed");
    if (!needPoll) {
      if (pollRef.current) {
        clearInterval(pollRef.current);
        pollRef.current = null;
      }
      return;
    }
    if (pollRef.current) return;
    pollRef.current = setInterval(() => {
      const job = jobRef.current;
      const pid = projectIdRef.current;
      if (!job || !pid) return;
      api
        .getJob(pid, job.job_id)
        .then((status) => {
          setLastJob(status);
          if (status.state === "done" || status.state === "failed" || status.state === "cancelled") {
            if (pollRef.current) {
              clearInterval(pollRef.current);
              pollRef.current = null;
            }
          }
        })
        .catch(() => {
          // backend toujours injoignable : nouvelle tentative au tick suivant
        });
    }, POLL_INTERVAL_MS);
    return () => {
      if (pollRef.current) {
        clearInterval(pollRef.current);
        pollRef.current = null;
      }
    };
  }, [lastJob, socketStatus, setLastJob]);

  // À la fin d'un vrai job : rafraîchir le layout pour voir les pistes en PCB.
  useEffect(() => {
    if (lastJob?.state === "done" && !demoMode) {
      void refreshLayout(projectIdRef.current ?? undefined);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [lastJob?.state]);

  const toggleNet = (name: string) => {
    setDisabledNets((prev) => {
      const next = new Set(prev);
      if (next.has(name)) next.delete(name);
      else next.add(name);
      return next;
    });
  };

  const resetConsole = () => {
    stopEverything();
    setResults([]);
    setLogs([]);
    setSocketStatus(null);
    setLastJob(null);
  };

  const percent = lastJob?.percent ?? 0;
  const showFinalBanner = lastJob?.state === "done" || lastJob?.state === "failed" || lastJob?.state === "cancelled";

  return (
    <div className="page">
      <div className="page-header">
        <div>
          <h1 className="page-title">Routage IA</h1>
          <p className="page-subtitle">
            {currentProject?.name ?? "Projet sans nom"}
            {projectId ? ` — projet ${projectId}` : " — données de démonstration"}
          </p>
        </div>
        <div className="page-actions">
          {lastJob ? (
            <span className={`chip chip-${lastJob.state}`} data-testid="job-state-chip">
              {STATE_LABEL[lastJob.state]}
            </span>
          ) : (
            <span className="chip chip-pending">Aucun job</span>
          )}
        </div>
      </div>

      {demoMode ? (
        <div className="demo-banner" data-testid="demo-banner" role="status">
          Mode démo : backend injoignable, le routage est simulé localement.
        </div>
      ) : null}

      <div className="router-layout">
        <aside className="side-panel">
          <div className="side-box panel">
            <h3 className="panel-title">Configuration</h3>
            <SelectField
              label="Stratégie"
              data-testid="route-strategy"
              options={STRATEGY_OPTIONS}
              value={strategy}
              onChange={(e) => setStrategy(e.target.value as AIStrategy)}
            />
            <Button
              data-testid="route-start-btn"
              onClick={() => void startRoute()}
              disabled={starting || jobRunning || selectedNets.length === 0}
            >
              {starting ? "Démarrage…" : "Lancer le routage IA"}
            </Button>
            {selectedNets.length === 0 ? (
              <p className="muted" style={{ fontSize: 12, marginTop: 8 }}>
                Sélectionnez au moins un net à router.
              </p>
            ) : null}
            {nets.length === 0 ? (
              <p className="muted" style={{ fontSize: 12, marginTop: 8 }}>
                Aucun net disponible : ouvrez un projet ou importez un fichier.
              </p>
            ) : null}
          </div>

          <div className="side-box panel">
            <h3 className="panel-title">Filtre de nets ({selectedNets.length}/{nets.length})</h3>
            <div className="toolbar" style={{ border: "none", background: "transparent", padding: 0, gap: 6 }}>
              <Button size="sm" variant="ghost" onClick={() => setDisabledNets(new Set())}>
                Tout cocher
              </Button>
              <Button size="sm" variant="ghost" onClick={() => setDisabledNets(new Set(nets))}>
                Tout décocher
              </Button>
            </div>
            <div className="check-list" style={{ marginTop: 10 }}>
              {nets.map((name) => (
                <label key={name} className="check-item">
                  <input
                    type="checkbox"
                    checked={!disabledNets.has(name)}
                    onChange={() => toggleNet(name)}
                    disabled={jobRunning}
                  />
                  <span className="net-dot" style={{ background: colorForNet(name) }} />
                  <span className="net-name">{name}</span>
                </label>
              ))}
            </div>
          </div>
        </aside>

        <section className="canvas-wrap panel">
          <div className="toolbar">
            <strong style={{ fontSize: 13 }}>Progression du job</strong>
            <span className="toolbar-sep" />
            {lastJob ? <span className="mono" style={{ fontSize: 12, color: "#8CA0C0" }}>job {lastJob.job_id}</span> : null}
            <span style={{ flex: 1 }} />
            <Button size="sm" variant="ghost" onClick={resetConsole}>
              Réinitialiser
            </Button>
          </div>

          <div style={{ padding: "16px 18px", display: "flex", flexDirection: "column", gap: 18 }}>
            <div>
              <div
                className="progress-outer"
                data-testid="progress-bar"
                role="progressbar"
                aria-valuemin={0}
                aria-valuemax={100}
                aria-valuenow={Math.round(percent)}
              >
                <div className="progress-inner" style={{ width: `${Math.min(100, Math.max(0, percent))}%` }} />
              </div>
              <div className="progress-meta">
                <span>{percentFormat(percent)}</span>
                <span>
                  {lastJob ? `${lastJob.nets_done}/${lastJob.nets_total} nets` : "—"}
                </span>
                <span>{lastJob?.current_net ? `net courant : ${lastJob.current_net}` : ""}</span>
              </div>
              {socketStatus && jobRunning ? <div className="ws-chip">{SOCKET_LABEL[socketStatus]}</div> : null}
            </div>

            {showFinalBanner && lastJob ? (
              <div className={`final-banner ${lastJob.state === "done" ? "ok" : "fail"}`} data-testid="final-banner">
                {lastJob.state === "done"
                  ? `Terminé — ${lastJob.nets_done}/${lastJob.nets_total} nets routés.`
                  : lastJob.state === "failed"
                    ? `Échec du routage${lastJob.error ? ` : ${lastJob.error}` : "."}`
                    : "Job annulé."}
              </div>
            ) : null}

            <div>
              <h3 style={{ fontSize: 13, fontWeight: 700, marginBottom: 8 }}>Journal</h3>
              <div className="progress-log" data-testid="progress-log">
                {logs.length === 0 ? (
                  <span className="log-line muted">En attente d&apos;événements… lancez le routage IA.</span>
                ) : (
                  logs.map((line) => (
                    <span key={line.id} className={`log-line${line.error ? " log-line-error" : ""}`}>
                      [{line.time}] {line.text}
                    </span>
                  ))
                )}
              </div>
            </div>

            <div>
              <h3 style={{ fontSize: 13, fontWeight: 700, marginBottom: 8 }}>
                Résultats par net ({results.length})
              </h3>
              {results.length === 0 ? (
                <p className="muted" style={{ fontSize: 12.5 }}>
                  Les résultats apparaissent ici au fur et à mesure du routage.
                </p>
              ) : (
                <table className="results-table" data-testid="route-results">
                  <thead>
                    <tr>
                      <th>Net</th>
                      <th>Longueur (mm)</th>
                      <th>Vias</th>
                      <th>État</th>
                    </tr>
                  </thead>
                  <tbody>
                    {results.map((r) => (
                      <tr key={r.net}>
                        <td>
                          <span className="net-dot" style={{ background: colorForNet(r.net), display: "inline-block", marginRight: 8 }} />
                          {r.net}
                        </td>
                        <td>{r.length_mm.toFixed(2)}</td>
                        <td>{r.vias.length}</td>
                        <td>
                          {r.completed ? (
                            <span className="badge badge-ok">Routé</span>
                          ) : (
                            <span className="badge badge-warning">Incomplet</span>
                          )}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              )}
            </div>
          </div>
        </section>
      </div>
    </div>
  );
}
