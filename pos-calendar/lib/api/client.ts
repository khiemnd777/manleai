import { apiBaseUrl } from "@/lib/config/env";

export type ApiError = {
  code: string;
  message: string;
};

type TokenResponse = {
  access_token: string;
  refresh_token: string;
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

export function getRefreshToken() {
  if (typeof window === "undefined") return "";
  return window.localStorage.getItem("refresh_token") ?? "";
}

export function setSession(accessToken: string, refreshToken: string) {
  window.localStorage.setItem("access_token", accessToken);
  window.localStorage.setItem("refresh_token", refreshToken);
}

export function clearSession() {
  window.localStorage.removeItem("access_token");
  window.localStorage.removeItem("refresh_token");
}

let refreshPromise: Promise<string | null> | null = null;

export async function apiRequest<T>(path: string, init: RequestInit = {}): Promise<T> {
  const response = await sendRequest(path, init);
  if (response.status === 401 && shouldAttemptRefresh(path)) {
    const refreshedToken = await refreshAccessToken();
    if (refreshedToken) {
      return parseResponse<T>(await sendRequest(path, init, refreshedToken));
    }
    redirectToLogin();
  }

  return parseResponse<T>(response);
}

export async function logoutSession() {
  const refreshToken = getRefreshToken();
  clearSession();
  if (!refreshToken) return;

  try {
    await fetch(`${apiBaseUrl}/api/auth/logout`, {
      method: "POST",
      headers: {
        Accept: "application/json",
        "Content-Type": "application/json"
      },
      body: JSON.stringify({ refresh_token: refreshToken }),
      cache: "no-store"
    });
  } catch {
    // Local session has already been cleared; backend revocation failure should not trap the user.
  }
}

async function sendRequest(path: string, init: RequestInit, accessToken = getAccessToken()) {
  const headers = new Headers(init.headers);
  headers.set("Accept", "application/json");
  if (init.body && !headers.has("Content-Type")) {
    headers.set("Content-Type", "application/json");
  }
  if (accessToken && shouldAttachAccessToken(path)) {
    headers.set("Authorization", `Bearer ${accessToken}`);
  } else {
    headers.delete("Authorization");
  }

  return fetch(`${apiBaseUrl}${path}`, {
    ...init,
    headers,
    cache: "no-store"
  });
}

async function parseResponse<T>(response: Response): Promise<T> {
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

  if (response.status === 204) {
    return undefined as T;
  }

  return response.json() as Promise<T>;
}

function shouldAttachAccessToken(path: string) {
  return (
    !path.startsWith("/api/auth/login") &&
    !path.startsWith("/api/auth/refresh-token") &&
    !path.startsWith("/api/auth/bootstrap/") &&
    !path.startsWith("/api/auth/bootstrap-owner")
  );
}

function shouldAttemptRefresh(path: string) {
  return shouldAttachAccessToken(path) && !path.startsWith("/api/auth/logout");
}

function refreshAccessToken() {
  if (refreshPromise) return refreshPromise;

  refreshPromise = requestRefreshToken().finally(() => {
    refreshPromise = null;
  });
  return refreshPromise;
}

async function requestRefreshToken() {
  const refreshToken = getRefreshToken();
  if (!refreshToken) {
    clearSession();
    return null;
  }

  try {
    const response = await fetch(`${apiBaseUrl}/api/auth/refresh-token`, {
      method: "POST",
      headers: {
        Accept: "application/json",
        "Content-Type": "application/json"
      },
      body: JSON.stringify({ refresh_token: refreshToken }),
      cache: "no-store"
    });
    if (!response.ok) {
      clearSession();
      return null;
    }

    const tokens = (await response.json()) as TokenResponse;
    if (!tokens.access_token || !tokens.refresh_token) {
      clearSession();
      return null;
    }

    setSession(tokens.access_token, tokens.refresh_token);
    return tokens.access_token;
  } catch {
    clearSession();
    return null;
  }
}

function redirectToLogin() {
  if (typeof window === "undefined") return;
  if (window.location.pathname !== "/login") {
    window.location.assign("/login");
  }
}
