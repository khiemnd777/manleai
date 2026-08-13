import { requiredHTTPOrigin } from "./http-origin";
import type { PublicCatalog } from "./types";

function serverApiBaseUrl(): string {
  return requiredHTTPOrigin(process.env.LANDING_API_BASE_URL, "LANDING_API_BASE_URL");
}

export class PublicApiError extends Error {
  status: number;

  constructor(status: number, message: string) {
    super(message);
    this.status = status;
  }
}

export async function getPublicCatalog(slug: string): Promise<PublicCatalog> {
  const response = await fetch(`${serverApiBaseUrl()}/api/public/salons/${encodeURIComponent(slug)}`, {
    headers: { Accept: "application/json" },
    cache: "no-store"
  });
  if (!response.ok) {
    throw new PublicApiError(response.status, "Could not load public salon page.");
  }
  return response.json() as Promise<PublicCatalog>;
}

export async function getDefaultPublicCatalog(): Promise<PublicCatalog> {
  const response = await fetch(`${serverApiBaseUrl()}/api/public/salon`, {
    headers: { Accept: "application/json" },
    cache: "no-store"
  });
  if (!response.ok) {
    throw new PublicApiError(response.status, "Could not load public salon page.");
  }
  return response.json() as Promise<PublicCatalog>;
}
