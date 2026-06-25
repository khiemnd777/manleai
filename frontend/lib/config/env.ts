export const apiBaseUrl =
  process.env.NEXT_PUBLIC_API_BASE_URL?.replace(/\/$/, "") ?? "http://localhost:18089";

export const landingBaseUrl =
  process.env.NEXT_PUBLIC_LANDING_BASE_URL?.replace(/\/$/, "") ?? "http://localhost:3090";
