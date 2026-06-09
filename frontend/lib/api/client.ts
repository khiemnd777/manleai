import { apiBaseUrl } from "@/lib/config/env";

export type ApiError = {
  code: string;
  message: string;
};

export class RequestError extends Error {
  status: number;
  code: string;

  constructor(status: number, code: string, message: string) {
    super(message);
    this.status = status;
    this.code = code;
  }
}

export function getAccessToken() {
  if (typeof window === "undefined") return "";
  return window.localStorage.getItem("access_token") ?? "";
}

export function setSession(accessToken: string, refreshToken: string) {
  window.localStorage.setItem("access_token", accessToken);
  window.localStorage.setItem("refresh_token", refreshToken);
}

export function clearSession() {
  window.localStorage.removeItem("access_token");
  window.localStorage.removeItem("refresh_token");
}

export async function apiRequest<T>(path: string, init: RequestInit = {}): Promise<T> {
  const headers = new Headers(init.headers);
  headers.set("Accept", "application/json");
  if (init.body && !headers.has("Content-Type")) {
    headers.set("Content-Type", "application/json");
  }
  const token = getAccessToken();
  if (token) {
    headers.set("Authorization", `Bearer ${token}`);
  }

  const response = await fetch(`${apiBaseUrl}${path}`, {
    ...init,
    headers,
    cache: "no-store"
  });

  if (!response.ok) {
    let error: ApiError = { code: "REQUEST_FAILED", message: "Request failed." };
    try {
      const parsed = await response.json();
      error = parsed.error ?? error;
    } catch {
      // Keep structured fallback when a proxy or provider returns non-JSON.
    }
    throw new RequestError(response.status, error.code, error.message);
  }

  return response.json() as Promise<T>;
}

