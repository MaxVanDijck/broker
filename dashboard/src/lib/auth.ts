import { createContext, useContext } from "react";

const TOKEN_KEY = "broker_token";

export interface AuthState {
  token: string | null;
  login: (token: string) => void;
  logout: () => void;
}

export const AuthContext = createContext<AuthState>({
  token: null,
  login: () => {},
  logout: () => {},
});

export function useAuth() {
  return useContext(AuthContext);
}

export function getStoredToken(): string | null {
  return localStorage.getItem(TOKEN_KEY);
}

export function setStoredToken(token: string) {
  localStorage.setItem(TOKEN_KEY, token);
}

export function clearStoredToken() {
  localStorage.removeItem(TOKEN_KEY);
}

export function authFetch(input: RequestInfo | URL, init?: RequestInit): Promise<Response> {
  const token = getStoredToken();
  if (!token) {
    return fetch(input, init);
  }

  const headers = new Headers(init?.headers);
  headers.set("Authorization", "Basic " + btoa("broker:" + token));

  return fetch(input, { ...init, headers });
}
