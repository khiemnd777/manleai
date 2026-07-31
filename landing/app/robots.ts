import type { MetadataRoute } from "next";
import { marketingBaseUrl } from "@/lib/config";
export default function robots():MetadataRoute.Robots{return{rules:{userAgent:"*",allow:["/","/vi","/pricing","/vi/pricing"],disallow:["/salon-home"]},sitemap:`${marketingBaseUrl}/sitemap.xml`,host:marketingBaseUrl}}
