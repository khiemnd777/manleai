import type { Metadata } from "next";
import { MarketingSite } from "@/components/marketing/marketing-site";
import { marketingBaseUrl } from "@/lib/config";

export const metadata: Metadata = {
  title: "AI lễ tân dành cho tiệm nail",
  description: "Tianna AI hỗ trợ cuộc gọi tiếng Anh, trả lời theo dữ liệu salon và ghi nhận yêu cầu lịch hẹn an toàn.",
  alternates: { canonical: `${marketingBaseUrl}/vi`, languages: { "en-US": marketingBaseUrl, "vi-US": `${marketingBaseUrl}/vi`, "x-default": marketingBaseUrl } },
  openGraph: { title: "Tianna AI — AI lễ tân dành cho tiệm nail", description: "Tiếp nhận cuộc gọi tiếng Anh với scheduling workflow được chọn rõ ràng.", url: `${marketingBaseUrl}/vi`, type: "website", images: ["/brand/tianna-ai-logo.png"] }
};
export default function VietnameseHomePage(){return <MarketingSite locale="vi"/>}
