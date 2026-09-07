"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import type { ReactNode } from "react";

interface NavItem {
  href: string;
  label: string;
  exact?: boolean;
  icon: ReactNode;
}

const STROKE = {
  fill: "none" as const,
  stroke: "currentColor",
  strokeWidth: 1.8,
  strokeLinecap: "round" as const,
  strokeLinejoin: "round" as const,
};

function IconProjects() {
  return (
    <svg viewBox="0 0 24 24" width="20" height="20" {...STROKE}>
      <path d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z" />
    </svg>
  );
}

function IconSchematic() {
  return (
    <svg viewBox="0 0 24 24" width="20" height="20" {...STROKE}>
      <rect x="7" y="7" width="10" height="10" rx="1.5" />
      <path d="M10 7V3M14 7V3M10 21v-4M14 21v-4M7 10H3M7 14H3M21 10h-4M21 14h-4" />
    </svg>
  );
}

function IconPcb() {
  return (
    <svg viewBox="0 0 24 24" width="20" height="20" {...STROKE}>
      <rect x="3" y="4" width="18" height="16" rx="2" />
      <path d="M7 8h4v4h6M7 16h6" />
      <circle cx="17.5" cy="15.5" r="1.4" />
    </svg>
  );
}

function IconRoute() {
  return (
    <svg viewBox="0 0 24 24" width="20" height="20" {...STROKE}>
      <circle cx="5.5" cy="18.5" r="2" />
      <circle cx="18.5" cy="5.5" r="2" />
      <path d="M7.5 18.5H13a4 4 0 0 0 4-4v-2a4 4 0 0 0-4-4h-2" />
    </svg>
  );
}

function Icon3d() {
  return (
    <svg viewBox="0 0 24 24" width="20" height="20" {...STROKE}>
      <path d="M12 2.5l8.5 4.9v9.2L12 21.5l-8.5-4.9V7.4z" />
      <path d="M12 12l8.5-4.6M12 12L3.5 7.4M12 12v9.5" />
    </svg>
  );
}

function IconExport() {
  return (
    <svg viewBox="0 0 24 24" width="20" height="20" {...STROKE}>
      <path d="M12 3v11M7.5 9.5L12 14l4.5-4.5" />
      <path d="M4 15v3a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2v-3" />
    </svg>
  );
}

const NAV_ITEMS: NavItem[] = [
  { href: "/pages/project-manager", label: "Projets", icon: <IconProjects /> },
  { href: "/pages/schematic-editor", label: "Schéma", icon: <IconSchematic /> },
  { href: "/pages/pcb-layout", label: "PCB", exact: true, icon: <IconPcb /> },
  { href: "/pages/pcb-layout/router", label: "Routage IA", icon: <IconRoute /> },
  { href: "/pages/pcb-layout/viewer", label: "3D", icon: <Icon3d /> },
  { href: "/pages/export", label: "Export", icon: <IconExport /> },
];

export function Sidebar() {
  const pathname = usePathname() ?? "";

  return (
    <aside className="sidebar-rail">
      <div className="rail-brand">
        <svg viewBox="0 0 24 24" width="22" height="22" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round">
          <rect x="3" y="3" width="18" height="18" rx="3" />
          <path d="M9 8v8M9 12l5-4M9 12l5 4" />
        </svg>
        <span>KidCAD</span>
      </div>
      <nav className="rail-nav" aria-label="Navigation principale">
        {NAV_ITEMS.map((item) => {
          const active = item.exact ? pathname === item.href : pathname.startsWith(item.href);
          return (
            <Link
              key={item.href}
              href={item.href}
              className={`rail-item${active ? " active" : ""}`}
              aria-current={active ? "page" : undefined}
            >
              {item.icon}
              <span className="rail-label">{item.label}</span>
            </Link>
          );
        })}
      </nav>
      <div className="rail-footer">
        <span>KidCAD-Pro-IA v1.0</span>
      </div>
    </aside>
  );
}
