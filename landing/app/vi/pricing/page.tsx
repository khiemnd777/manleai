import type { Metadata } from "next";
import { PricingPage } from "@/components/marketing/pricing-page";
import { marketingBaseUrl } from "@/lib/config";
export const metadata:Metadata={title:"Bảng giá",description:"Bảng giá marketing Starter, Growth và Custom của Tianna AI dành cho tiệm nail tại Mỹ.",alternates:{canonical:`${marketingBaseUrl}/vi/pricing`,languages:{"en-US":`${marketingBaseUrl}/pricing`,"vi-US":`${marketingBaseUrl}/vi/pricing`}},openGraph:{title:"Bảng giá Tianna AI",description:"So sánh Starter, Growth và Custom.",url:`${marketingBaseUrl}/vi/pricing`,type:"website",images:["/brand/tianna-ai-logo.png"]}};
export default function VietnamesePricing(){return <PricingPage locale="vi"/>}
