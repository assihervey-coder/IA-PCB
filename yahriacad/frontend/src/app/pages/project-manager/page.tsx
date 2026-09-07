"use client";

import { useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { api, isBackendDown } from "@/lib/api/rest-client";
import type { Project, ProjectCreate } from "@/lib/api/types";
import { useProjectStore } from "@/lib/store/project-store";
import { useUIStore } from "@/lib/store/ui-store";
import { Button } from "@/app/components/buttons/Button";
import { Modal } from "@/app/components/modals/Modal";
import { ConfirmDialog } from "@/app/components/modals/ConfirmDialog";
import { ProjectForm } from "@/app/components/forms/ProjectForm";
import { formatDateFR } from "@/lib/utils/converters";

export default function ProjectManagerPage() {
  const router = useRouter();
  const projects = useProjectStore((s) => s.projects);
  const loading = useProjectStore((s) => s.loading);
  const error = useProjectStore((s) => s.error);
  const demoMode = useProjectStore((s) => s.demoMode);
  const loadProjects = useProjectStore((s) => s.loadProjects);
  const activeModal = useUIStore((s) => s.activeModal);
  const openModal = useUIStore((s) => s.openModal);
  const closeModal = useUIStore((s) => s.closeModal);
  const pushToast = useUIStore((s) => s.pushToast);

  const [deleteTarget, setDeleteTarget] = useState<Project | null>(null);
  const [creating, setCreating] = useState(false);

  useEffect(() => {
    void loadProjects();
  }, [loadProjects]);

  const handleCreate = async (data: ProjectCreate) => {
    setCreating(true);
    try {
      await api.createProject(data);
      pushToast("Projet créé avec succès.", "success");
      closeModal();
      await loadProjects();
    } catch (err) {
      if (isBackendDown(err)) {
        // Mode démo : création simulée localement.
        const now = new Date().toISOString();
        const local: Project = {
          id: `demo-local-${Date.now()}`,
          name: data.name,
          description: data.description ?? "",
          layer_count: data.layer_count ?? 2,
          status: "active",
          created_at: now,
          updated_at: now,
        };
        useProjectStore.setState((st) => ({ projects: [local, ...st.projects] }));
        pushToast("Mode démo : projet ajouté localement.", "info");
        closeModal();
      } else {
        pushToast("Échec de la création du projet.", "error");
      }
    } finally {
      setCreating(false);
    }
  };

  const handleDelete = async (project: Project) => {
    let deleted = false;
    try {
      await api.deleteProject(project.id);
      deleted = true;
      pushToast("Projet supprimé.", "success");
    } catch (err) {
      if (isBackendDown(err)) {
        deleted = true;
        pushToast("Mode démo : suppression simulée.", "info");
      } else {
        pushToast("Échec de la suppression du projet.", "error");
      }
    } finally {
      if (deleted) {
        useProjectStore.setState((st) => ({
          projects: st.projects.filter((p) => p.id !== project.id),
        }));
      }
      setDeleteTarget(null);
    }
  };

  return (
    <div className="page">
      <div className="page-header">
        <div>
          <h1 className="page-title">Projets</h1>
          <p className="page-subtitle">
            Gérez vos cartes : créez un projet, ouvrez le placement PCB ou l&apos;éditeur de schéma.
          </p>
        </div>
        <div className="page-actions">
          <Button data-testid="create-project-btn" onClick={() => openModal("create-project")}>
            Nouveau projet
          </Button>
        </div>
      </div>

      {demoMode ? (
        <div className="demo-banner" data-testid="demo-banner" role="status">
          <svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round">
            <path d="M12 3L2 20h20z" />
            <path d="M12 10v4M12 17.5v.5" />
          </svg>
          Mode démo : backend injoignable, données d&apos;exemple affichées.
        </div>
      ) : null}

      {error ? (
        <div className="demo-banner" role="alert">
          {error}
        </div>
      ) : null}

      {loading ? (
        <div className="grid-skeleton" data-testid="projects-loading">
          {Array.from({ length: 6 }, (_, i) => (
            <div key={i} className="skeleton" />
          ))}
        </div>
      ) : projects.length === 0 ? (
        <div className="empty-state">
          <strong>Aucun projet pour le moment</strong>
          Créez votre premier projet pour commencer à dessiner votre carte.
        </div>
      ) : (
        <div className="grid-cards" data-testid="projects-grid">
          {projects.map((project) => (
            <article key={project.id} className="project-card panel" data-testid="project-card">
              <div className="card-head">
                <h3 className="card-title">{project.name}</h3>
                <span className="badge badge-info">{project.layer_count} couches</span>
              </div>
              <p className="card-desc">{project.description || "Aucune description."}</p>
              <div className="card-meta">
                <span>Mis à jour : {formatDateFR(project.updated_at)}</span>
                <span className={`badge ${project.status === "active" ? "badge-ok" : "badge-muted"}`}>
                  {project.status === "active" ? "Actif" : "Archivé"}
                </span>
              </div>
              <div className="card-actions">
                <Button size="sm" onClick={() => router.push(`/pages/pcb-layout?project=${encodeURIComponent(project.id)}`)}>
                  Ouvrir
                </Button>
                <Button
                  size="sm"
                  variant="secondary"
                  onClick={() => router.push(`/pages/schematic-editor?project=${encodeURIComponent(project.id)}`)}
                >
                  Schéma
                </Button>
                <Button size="sm" variant="danger" onClick={() => setDeleteTarget(project)}>
                  Supprimer
                </Button>
              </div>
            </article>
          ))}
        </div>
      )}

      {activeModal === "create-project" ? (
        <Modal title="Nouveau projet" onClose={closeModal}>
          <ProjectForm submitting={creating} onSubmit={(data) => void handleCreate(data)} onCancel={closeModal} />
        </Modal>
      ) : null}

      {deleteTarget ? (
        <ConfirmDialog
          title="Supprimer le projet"
          message={`Confirmer la suppression définitive de « ${deleteTarget.name} » ? Cette action est irréversible.`}
          onConfirm={() => void handleDelete(deleteTarget)}
          onCancel={() => setDeleteTarget(null)}
        />
      ) : null}
    </div>
  );
}
