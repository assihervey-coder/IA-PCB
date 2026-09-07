/**
 * Authentification côté client : stockage du jeton JWT (localStorage) et
 * helpers partagés par le client REST et la page de connexion. Le jeton
 * voyage dans l'en-tête `Authorization: Bearer` (aucun cookie, donc pas de
 * CSRF) ; une réponse 401 du backend efface le jeton et renvoie à /login.
 */
const TOKEN_KEY = "yahriacad_jwt";

export interface AuthLoginResponse {
  token: string;
  token_type: string;
  expires_at: string;
  username: string;
}

export interface AuthMe {
  username: string;
}

export function getToken(): string | null {
  if (typeof window === "undefined") return null;
  return window.localStorage.getItem(TOKEN_KEY);
}

export function setToken(token: string): void {
  if (typeof window === "undefined") return;
  window.localStorage.setItem(TOKEN_KEY, token);
}

export function clearToken(): void {
  if (typeof window === "undefined") return;
  window.localStorage.removeItem(TOKEN_KEY);
}

export function isLoggedIn(): boolean {
  return getToken() !== null;
}
