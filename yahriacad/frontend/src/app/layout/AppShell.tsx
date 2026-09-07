"use client";

import type { ReactNode } from "react";
import { Header } from "./Header";
import { Sidebar } from "./Sidebar";
import { MagicBar } from "@/app/components/magic/MagicBar";
import { ShortcutsOverlay } from "@/app/components/shortcuts/ShortcutsOverlay";
import { useUIStore } from "@/lib/store/ui-store";

function ToastStack() {
  const toasts = useUIStore((s) => s.toasts);
  const dismissToast = useUIStore((s) => s.dismissToast);
  if (toasts.length === 0) return null;
  return (
    <div className="toast-stack" aria-live="polite">
      {toasts.map((toast) => (
        <div
          key={toast.id}
          className={`toast toast-${toast.kind}`}
          onClick={() => dismissToast(toast.id)}
          role="status"
        >
          {toast.message}
        </div>
      ))}
    </div>
  );
}

export function AppShell({ children }: { children: ReactNode }) {
  const sidebarOpen = useUIStore((s) => s.sidebarOpen);
  return (
    <div className={`app-shell ${sidebarOpen ? "rail-open" : "rail-collapsed"}`}>
      <Sidebar />
      <div className="app-main">
        <Header />
        <main className="app-content">{children}</main>
      </div>
      <ToastStack />
      <MagicBar />
      <ShortcutsOverlay />
    </div>
  );
}
