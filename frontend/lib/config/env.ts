function exactHTTPOrigin(value: string, name: string) {
  const parsed = new URL(value);
  if ((parsed.protocol !== "http:" && parsed.protocol !== "https:") || parsed.username || parsed.password || parsed.pathname !== "/" || parsed.search || parsed.hash) {
    throw new Error(`${name} must be an exact HTTP(S) origin.`);
  }
  return parsed.origin;
}

export const apiBaseUrl = exactHTTPOrigin(process.env.NEXT_PUBLIC_API_BASE_URL ?? "http://localhost:18089", "NEXT_PUBLIC_API_BASE_URL");

export const landingBaseUrl = exactHTTPOrigin(process.env.NEXT_PUBLIC_LANDING_BASE_URL ?? "http://localhost:3090", "NEXT_PUBLIC_LANDING_BASE_URL");
