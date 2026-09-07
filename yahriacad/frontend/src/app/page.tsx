"use client";

import { useEffect } from "react";
import { useRouter } from "next/navigation";

export default function HomePage() {
  const router = useRouter();

  useEffect(() => {
    router.replace("/pages/project-manager");
  }, [router]);

  return (
    <div className="page-loading">
      <div className="spinner" aria-hidden="true" />
      <p>Redirection vers le gestionnaire de projets…</p>
    </div>
  );
}
