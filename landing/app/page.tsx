import type { Metadata } from "next";
import { MarketingSite } from "@/components/marketing/marketing-site";
import { marketingBaseUrl } from "@/lib/config";

export const metadata: Metadata = {
  title: "AI Receptionist for Nail Salons",
  description: "Tianna AI helps nail salons handle English calls, approved salon questions, and owner-first appointment requests.",
  alternates: { canonical: marketingBaseUrl, languages: { "en-US": marketingBaseUrl, "vi-US": `${marketingBaseUrl}/vi`, "x-default": marketingBaseUrl } },
  openGraph: { title: "Tianna AI — AI Receptionist for Nail Salons", description: "English, salon-aware phone coverage with explicit scheduling workflows.", url: marketingBaseUrl, type: "website", images: ["/brand/tianna-ai-logo.png"] }
};

export default function HomePage() { return <MarketingSite locale="en" />; }
