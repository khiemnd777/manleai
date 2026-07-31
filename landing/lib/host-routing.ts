export type HostRoutingDecision = "allow" | "redirect_salon" | "rewrite_salon_home" | "not_found";

function normalizedHostname(value: string) {
  const normalized = value.trim().toLowerCase();
  if (!normalized || /[\s,/@\\?#]/.test(normalized)) return "";
  if (normalized.startsWith("[") && normalized.endsWith("]")) return normalized.slice(1, -1);
  return normalized;
}

function isLoopbackHostname(hostname: string) {
  return hostname === "localhost" || hostname === "127.0.0.1" || hostname === "::1";
}

export function resolveIncomingHostname(hostHeader: string | null, fallbackHostname: string) {
  if (hostHeader === null) return normalizedHostname(fallbackHostname);
  const value = hostHeader.trim();
  if (!value || /[\s,/@\\?#]/.test(value)) return "";
  try {
    const parsed = new URL(`http://${value}`);
    if (parsed.username || parsed.password || parsed.pathname !== "/" || parsed.search || parsed.hash) return "";
    return normalizedHostname(parsed.hostname);
  } catch {
    return "";
  }
}

export function hostRoutingDecision(input: {
  hostname: string;
  pathname: string;
  production: boolean;
  marketingHost: string;
  salonHost: string;
}): HostRoutingDecision {
  const host = normalizedHostname(input.hostname);
  const marketingHost = normalizedHostname(input.marketingHost);
  const salonHost = normalizedHostname(input.salonHost);
  if (!input.production || (isLoopbackHostname(host) && isLoopbackHostname(marketingHost) && isLoopbackHostname(salonHost))) return "allow";
  if (host === marketingHost) {
    if (input.pathname === "/salon-home") return "not_found";
    return input.pathname === "/s" || input.pathname.startsWith("/s/") ? "redirect_salon" : "allow";
  }
  if (host === salonHost) {
    if (input.pathname === "/") return "rewrite_salon_home";
    if (input.pathname === "/s" || input.pathname.startsWith("/s/")) return "allow";
    return "not_found";
  }
  return "not_found";
}
