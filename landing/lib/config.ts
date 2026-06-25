export const apiBaseUrl = (process.env.LANDING_API_BASE_URL || process.env.NEXT_PUBLIC_API_BASE_URL || "http://localhost:18089").replace(/\/$/, "");
