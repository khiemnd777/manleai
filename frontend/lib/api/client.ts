import { apiBaseUrl } from "../config/env";
import { browserSession } from "./browser-session";
import { storedActiveTenantSalonID } from "./tenant-context";

export type ApiError = {
  code: string;
  message: string;
};

type TokenResponse = {
  access_token: string;
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
  return browserSession.getAccessToken();
}

export function setSession(accessToken: string) {
  browserSession.setAccessToken(accessToken);
}

export function clearSession() {
  browserSession.clear();
}

let refreshPromise: Promise<string | null> | null = null;

export async function apiRequest<T>(path: string, init: RequestInit = {}): Promise<T> {
  const response = await requestWithRefresh(path, init);

  return parseResponse<T>(response);
}

export async function apiRequestWithResponse<T>(
  path: string,
  init: RequestInit = {}
): Promise<{ data: T; response: Response }> {
  const response = await requestWithRefresh(path, init);

  return { data: await parseResponse<T>(response), response };
}

export async function logoutSession() {
  clearSession();

  try {
    await fetch(`${apiBaseUrl}/api/auth/logout`, {
      method: "POST",
      headers: {
        Accept: "application/json",
        "Content-Type": "application/json"
      },
      body: JSON.stringify({}),
      credentials: "include",
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
  const activeTenantSalonID = storedActiveTenantSalonID();
  if (activeTenantSalonID && !isPlatformPath(path)) {
    headers.set("X-Tenant-Salon-ID", activeTenantSalonID);
  }

  return fetch(`${apiBaseUrl}${path}`, {
    ...init,
    headers,
    credentials: "include",
    cache: "no-store"
  });
}

function isPlatformPath(path: string) {
  return path.startsWith("/api/platform/") || path.startsWith("/api/v2/platform/");
}

async function requestWithRefresh(path: string, init: RequestInit) {
  const response = await sendRequest(path, init);
  if (response.status !== 401 || !shouldAttemptRefresh(path)) {
    return response;
  }

  const refreshedToken = await refreshAccessToken();
  if (refreshedToken) {
    return sendRequest(path, init, refreshedToken);
  }
  redirectToLogin();
  return response;
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
    !path.startsWith("/api/auth/bootstrap-owner") &&
    !path.startsWith("/api/auth/owner-invitations/accept")
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
  try {
    const response = await fetch(`${apiBaseUrl}/api/auth/refresh-token`, {
      method: "POST",
      headers: {
        Accept: "application/json",
        "Content-Type": "application/json"
      },
      body: JSON.stringify({}),
      credentials: "include",
      cache: "no-store"
    });
    if (!response.ok) {
      clearSession();
      return null;
    }

    const tokens = (await response.json()) as TokenResponse;
    if (!tokens.access_token) {
      clearSession();
      return null;
    }

    setSession(tokens.access_token);
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
