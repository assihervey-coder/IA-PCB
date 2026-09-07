"use client";

import { FormEvent, useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { api, isBackendDown } from "@/lib/api/rest-client";
import { isLoggedIn } from "@/lib/api/auth";
import { Button } from "@/app/components/buttons/Button";
/**
 * Page de connexion JWT. Quand le backend tourne sans authentification
 * (dev), le login renvoie 503 auth_disabled : un bouton « Continuer sans
 * connexion » permet d'entrer dans l'application quand même.
 */
export default function LoginPage() {
  const router = useRouter();
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [authDisabled, setAuthDisabled] = useState(false);
  const [busy, setBusy] = useState(false);

  // Déjà connecté (jeton valide) : vers le gestionnaire de projets.
  useEffect(() => {
    if (isLoggedIn()) {
      api.me().then((me) => {
        if (me) router.replace("/pages/project-manager");
      });
    }
  }, [router]);

  async function handleSubmit(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError(null);
    setAuthDisabled(false);
    try {
      await api.login(username.trim(), password);
      router.replace("/pages/project-manager");
    } catch (err) {
      if (isBackendDown(err)) {
        setError("Backend injoignable : démarrez le serveur Go (make run-backend).");
      } else if (isAuthDisabled(err)) {
        setAuthDisabled(true);
        setError("Authentification désactivée sur ce serveur (API ouverte, mode développement).");
      } else {
        setError("Identifiants invalides.");
      }
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="auth-page">
      <form className="auth-card" onSubmit={handleSubmit} data-testid="login-form">
        <h1 className="auth-title">YahriaCad</h1>
        <p className="auth-subtitle">CAO PCB pilotée par l'IA — connexion</p>

        <label className="auth-field">
          <span>Utilisateur</span>
          <input
            data-testid="login-username"
            type="text"
            autoComplete="username"
            value={username}
            onChange={(e) => setUsername(e.target.value)}
            autoFocus
            required
          />
        </label>

        <label className="auth-field">
          <span>Mot de passe</span>
          <input
            data-testid="login-password"
            type="password"
            autoComplete="current-password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            required
          />
        </label>

        {error && (
          <p className="auth-error" role="alert" data-testid="login-error">
            {error}
          </p>
        )}

        <Button
          type="submit"
          variant="primary"
          className="auth-submit"
          data-testid="login-submit"
          disabled={busy}
        >
          {busy ? "Connexion…" : "Se connecter"}
        </Button>

        {authDisabled && (
          <Button
            type="button"
            variant="ghost"
            className="auth-submit"
            data-testid="login-continue"
            onClick={() => router.replace("/pages/project-manager")}
          >
            Continuer sans connexion →
          </Button>
        )}

        <p className="auth-hint">
          Comptes configurés côté serveur (YAHRIACAD_AUTH_USERS). Les mots de
          passe bcrypt sont recommandés en production.
        </p>
      </form>
    </div>
  );
}

/** Reconnait la 503 auth_disabled du backend (auth non configurée). */
function isAuthDisabled(err: unknown): boolean {
  return (
    typeof err === "object" &&
    err !== null &&
    "response" in err &&
    (err as { response?: { status?: number; data?: { error?: { code?: string } } } })
      .response?.status === 503 &&
    (err as { response?: { data?: { error?: { code?: string } } } }).response?.data?.error
      ?.code === "auth_disabled"
  );
}
