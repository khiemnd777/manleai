import type { Metadata } from "next";
import { PricingPage } from "@/components/marketing/pricing-page";
import { marketingBaseUrl } from "@/lib/config";
export const metadata:Metadata={title:"Pricing",description:"Tianna AI Starter, Growth, and Custom marketing pricing for US nail salons.",alternates:{canonical:`${marketingBaseUrl}/pricing`,languages:{"en-US":`${marketingBaseUrl}/pricing`,"vi-US":`${marketingBaseUrl}/vi/pricing`}},openGraph:{title:"Tianna AI Pricing",description:"Compare Starter, Growth, and Custom call coverage.",url:`${marketingBaseUrl}/pricing`,type:"website",images:["/brand/tianna-ai-logo.png"]}};
export default function Pricing(){return <PricingPage locale="en"/>}
