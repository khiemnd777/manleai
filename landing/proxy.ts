import { NextRequest, NextResponse } from "next/server";

import { buildContentSecurityPolicy } from "@/lib/security/content-security-policy";

export function proxy(request: NextRequest) {
  const nonce = crypto.randomUUID().replaceAll("-", "");
  const policy = buildContentSecurityPolicy({
    nonce,
    apiBaseURL: process.env.NEXT_PUBLIC_API_BASE_URL || "http://localhost:18089",
    development: process.env.NODE_ENV !== "production"
  });
  const requestHeaders = new Headers(request.headers);
  requestHeaders.set("x-nonce", nonce);
  requestHeaders.set("Content-Security-Policy", policy);

  const response = NextResponse.next({ request: { headers: requestHeaders } });
  response.headers.set("Content-Security-Policy", policy);
  return response;
}

export const config = {
  matcher: [
    {
      source: "/((?!api|_next/static|_next/image|favicon.ico|sitemap.xml|robots.txt).*)",
      missing: [
        { type: "header", key: "next-router-prefetch" },
        { type: "header", key: "purpose", value: "prefetch" }
      ]
    }
  ]
};
