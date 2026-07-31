export function exactHTTPOrigin(value: string, name: string) {
  const parsed = new URL(value);
  if ((parsed.protocol !== "http:" && parsed.protocol !== "https:") || parsed.username || parsed.password || parsed.pathname !== "/" || parsed.search || parsed.hash) {
    throw new Error(`${name} must be an exact HTTP(S) origin without credentials, path, query, or fragment.`);
  }
  return parsed.origin;
}

export const apiBaseUrl = exactHTTPOrigin(process.env.LANDING_API_BASE_URL || process.env.NEXT_PUBLIC_API_BASE_URL || "http://localhost:18089", "LANDING_API_BASE_URL");
export const marketingBaseUrl = exactHTTPOrigin(process.env.MARKETING_BASE_URL || process.env.NEXT_PUBLIC_MARKETING_BASE_URL || "https://manle.knasoftware.com", "MARKETING_BASE_URL");
export const salonPublicBaseUrl = exactHTTPOrigin(process.env.SALON_PUBLIC_BASE_URL || process.env.NEXT_PUBLIC_LANDING_BASE_URL || "https://salon.knasoftware.com", "SALON_PUBLIC_BASE_URL");

export function configuredHost(origin: string) { return new URL(origin).hostname.toLowerCase(); }
