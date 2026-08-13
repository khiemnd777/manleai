import { NextRequest, NextResponse } from "next/server";

import { configuredHost, marketingBaseUrl, publicApiBaseUrl, salonPublicBaseUrl } from "@/lib/config";
import { hostRoutingDecision, resolveIncomingHostname } from "@/lib/host-routing";
import { buildContentSecurityPolicy } from "@/lib/security/content-security-policy";

export function proxy(request: NextRequest) {
  const nonce = crypto.randomUUID().replaceAll("-", "");
  const policy = buildContentSecurityPolicy({
    nonce,
    apiBaseURL: publicApiBaseUrl,
    development: process.env.NODE_ENV !== "production"
  });
  const requestHeaders = new Headers(request.headers);
  requestHeaders.set("x-nonce", nonce);
  requestHeaders.set("x-manleai-locale", request.nextUrl.pathname === "/vi" || request.nextUrl.pathname.startsWith("/vi/") ? "vi" : "en");
  requestHeaders.set("Content-Security-Policy", policy);

  const decision = hostRoutingDecision({ hostname: resolveIncomingHostname(request.headers.get("host"), request.nextUrl.hostname), pathname: request.nextUrl.pathname, production: process.env.NODE_ENV === "production", marketingHost: configuredHost(marketingBaseUrl), salonHost: configuredHost(salonPublicBaseUrl) });
  if (decision === "redirect_salon") {
    const target = new URL(request.nextUrl.pathname + request.nextUrl.search, `${salonPublicBaseUrl}/`);
    const response = NextResponse.redirect(target, 308);
    response.headers.set("Content-Security-Policy", policy);
    return response;
  }
  if (decision === "rewrite_salon_home") {
    const response = NextResponse.rewrite(new URL("/salon-home", request.url), { request: { headers: requestHeaders } });
    response.headers.set("Content-Security-Policy", policy);
    return response;
  }
  if (decision === "not_found") {
    const response = NextResponse.rewrite(new URL("/not-found", request.url), { status: 404, request: { headers: requestHeaders } });
    response.headers.set("Content-Security-Policy", policy);
    return response;
  }

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
