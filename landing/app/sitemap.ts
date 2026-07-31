import type { MetadataRoute } from "next";
import { marketingBaseUrl } from "@/lib/config";
export default function sitemap():MetadataRoute.Sitemap{return["","/vi","/pricing","/vi/pricing"].map(path=>({url:`${marketingBaseUrl}${path}`,lastModified:new Date("2026-07-31"),changeFrequency:"monthly" as const,priority:path===""?1:.8}))}
