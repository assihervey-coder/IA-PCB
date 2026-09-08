"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { api } from "@/lib/api/rest-client";
import { useProjectStore } from "@/lib/store/project-store";
import { useUIStore } from "@/lib/store/ui-store";
import { IconButton } from "@/app/components/buttons/IconButton";

export function Header() {
  const demoMode = useProjectStore((s) => s.demoMode);
  const toggleSidebar = useUIStore((s) => s.toggleSidebar);
  const [backendUp, setBackendUp] = useState<boolean | null>(null);

  useEffect(() => {
    let cancelled = false;
    const check = async () => {
      const ok = await api.pingBackend();
      if (!cancelled) setBackendUp(ok);
    };
    void check();
    const timer = setInterval(check, 15000);
    return () => {
      cancelled = true;
      clearInterval(timer);
    };
  }, []);

  const statusOk = !demoMode && backendUp === true;
  const statusLabel = demoMode
    ? "Backend injoignable — mode démo"
    : backendUp === null
      ? "Détection du backend…"
      : statusOk
        ? "Backend connecté"
        : "Backend injoignable";

  return (
    <header className="header">
      <div className="header-left">
        <IconButton label="Afficher ou masquer le menu" onClick={toggleSidebar}>
          <svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round">
            <path d="M4 6h16M4 12h16M4 18h16" />
          </svg>
        </IconButton>
        <Link href="/pages/project-manager" className="header-logo">
          YahriaCad<span>-Pro-IA</span>
        </Link>
      </div>
      <div className="header-right">
        {demoMode ? <span className="demo-badge">Mode démo</span> : null}
        <span className="backend-status" title={statusLabel}>
          <span className={`status-dot ${statusOk ? "ok" : "down"}`} />
          {statusLabel}
        </span>
      </div>
    </header>
  );
}
