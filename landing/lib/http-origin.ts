export function exactHTTPOrigin(value: string, name: string) {
  const parsed = new URL(value);
  if ((parsed.protocol !== "http:" && parsed.protocol !== "https:") || parsed.username || parsed.password || parsed.pathname !== "/" || parsed.search || parsed.hash) {
    throw new Error(`${name} must be an exact HTTP(S) origin without credentials, path, query, or fragment.`);
  }
  return parsed.origin;
}

export function requiredHTTPOrigin(value: string | undefined, name: string) {
  if (!value) throw new Error(`${name} is required.`);
  return exactHTTPOrigin(value, name);
}
