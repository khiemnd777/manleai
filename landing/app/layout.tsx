import type { Metadata } from "next";
import { headers } from "next/headers";
import { marketingBaseUrl } from "@/lib/config";
import "./globals.css";

export const dynamic = "force-dynamic";

export const metadata: Metadata = {
  metadataBase: new URL(marketingBaseUrl),
  title: { default: "Tianna AI — AI Receptionist for Nail Salons", template: "%s | Tianna AI" },
  description: "English AI phone receptionist and owner-first call workflows for US nail salons.",
  icons: {
    icon: [
      { url: "/brand/favicon-32.png", sizes: "32x32", type: "image/png" },
      { url: "/brand/icon-192.png", sizes: "192x192", type: "image/png" },
      { url: "/brand/icon-512.png", sizes: "512x512", type: "image/png" }
    ],
    apple: [{ url: "/brand/apple-touch-icon.png", sizes: "180x180", type: "image/png" }]
  },
  openGraph: {
    type: "website",
    title: "Tianna AI — AI Receptionist for Nail Salons",
    description: "English AI phone receptionist and owner-first call workflows for US nail salons.",
    images: [{ url: "/brand/tianna-ai-logo.png", width: 1024, height: 1024, alt: "Tianna AI" }]
  }
};

export default async function RootLayout({ children }: { children: React.ReactNode }) {
  const locale = (await headers()).get("x-manleai-locale") === "vi" ? "vi" : "en";
  return (
    <html lang={locale}>
      <body>{children}</body>
    </html>
  );
}
