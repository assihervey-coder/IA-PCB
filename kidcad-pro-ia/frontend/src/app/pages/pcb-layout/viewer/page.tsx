"use client";

import { useEffect } from "react";
import dynamic from "next/dynamic";
import { useProjectStore } from "@/lib/store/project-store";

// three.js ne doit jamais s'exécuter côté serveur : import dynamique ssr:false.
const Viewer3D = dynamic(() => import("./Viewer3D"), {
  ssr: false,
  loading: () => <div className="viewer-loading">Chargement de la visionneuse 3D…</div>,
});

export default function ViewerPage() {
  const openProject = useProjectStore((s) => s.openProject);
  const loadDemoData = useProjectStore((s) => s.loadDemoData);
  const currentProject = useProjectStore((s) => s.currentProject);

  useEffect(() => {
    const id = new URLSearchParams(window.location.search).get("project");
    if (id) {
      void openProject(id);
    } else {
      void loadDemoData();
    }
  }, [openProject, loadDemoData]);

  return (
    <div className="page">
      <div className="page-header">
        <div>
          <h1 className="page-title">Vue 3D</h1>
          <p className="page-subtitle">
            {currentProject?.name ?? "Projet sans nom"} — plaque, pistes, vias et composants.
          </p>
        </div>
      </div>
      <Viewer3D />
    </div>
  );
}
