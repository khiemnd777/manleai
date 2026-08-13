import { exactHTTPOrigin, requiredHTTPOrigin } from "@/lib/http-origin";

export const publicApiBaseUrl = requiredHTTPOrigin(process.env.NEXT_PUBLIC_API_BASE_URL, "NEXT_PUBLIC_API_BASE_URL");
export const marketingBaseUrl = exactHTTPOrigin(process.env.MARKETING_BASE_URL || process.env.NEXT_PUBLIC_MARKETING_BASE_URL || "https://ai.knasoftware.com", "MARKETING_BASE_URL");
export const salonPublicBaseUrl = exactHTTPOrigin(process.env.SALON_PUBLIC_BASE_URL || process.env.NEXT_PUBLIC_LANDING_BASE_URL || "https://salon.knasoftware.com", "SALON_PUBLIC_BASE_URL");

export function configuredHost(origin: string) { return new URL(origin).hostname.toLowerCase(); }
