export type ContentSecurityPolicyOptions = {
  nonce: string;
  apiBaseURL: string;
  development: boolean;
};

export function buildContentSecurityPolicy(options: ContentSecurityPolicyOptions): string {
  const nonce = validateNonce(options.nonce);
  const apiOrigin = validatedHTTPOrigin(options.apiBaseURL);
  const scriptSources = [`'self'`, `'nonce-${nonce}'`, `'strict-dynamic'`];
  const styleSources = [`'self'`, `'nonce-${nonce}'`];

  if (options.development) {
    scriptSources.push(`'unsafe-eval'`);
    styleSources.push(`'unsafe-inline'`);
  }

  const directives = [
    `default-src 'self'`,
    `script-src ${scriptSources.join(" ")}`,
    `style-src ${styleSources.join(" ")}`,
    options.development ? `style-src-attr 'unsafe-inline'` : `style-src-attr 'none'`,
    `img-src 'self' blob: data:`,
    `font-src 'self' data:`,
    `connect-src 'self' ${apiOrigin}`,
    `media-src 'self' ${apiOrigin}`,
    `worker-src 'self' blob:`,
    `manifest-src 'self'`,
    `object-src 'none'`,
    `base-uri 'self'`,
    `form-action 'self'`,
    `frame-ancestors 'none'`
  ];
  if (!options.development) directives.push("upgrade-insecure-requests");
  return directives.join("; ");
}

function validateNonce(nonce: string): string {
  const value = nonce.trim();
  if (!/^[A-Za-z0-9_-]{16,128}$/.test(value)) {
    throw new Error("CSP nonce is invalid.");
  }
  return value;
}

function validatedHTTPOrigin(value: string): string {
  const url = new URL(value);
  if ((url.protocol !== "http:" && url.protocol !== "https:") || url.username || url.password) {
    throw new Error("CSP API origin must be an HTTP(S) origin without credentials.");
  }
  return url.origin;
}
