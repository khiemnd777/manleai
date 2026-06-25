import { apiBaseUrl } from "@/lib/config";
import type { PublicCatalog } from "@/lib/types";

export class PublicApiError extends Error {
  status: number;

  constructor(status: number, message: string) {
    super(message);
    this.status = status;
  }
}

export async function getPublicCatalog(slug: string): Promise<PublicCatalog> {
  const response = await fetch(`${apiBaseUrl}/api/public/salons/${encodeURIComponent(slug)}`, {
    headers: { Accept: "application/json" },
    cache: "no-store"
  });
  if (!response.ok) {
    throw new PublicApiError(response.status, "Could not load public salon page.");
  }
  return response.json() as Promise<PublicCatalog>;
}

export async function getDefaultPublicCatalog(): Promise<PublicCatalog> {
  const response = await fetch(`${apiBaseUrl}/api/public/salon`, {
    headers: { Accept: "application/json" },
    cache: "no-store"
  });
  if (!response.ok) {
    throw new PublicApiError(response.status, "Could not load public salon page.");
  }
  return response.json() as Promise<PublicCatalog>;
}
