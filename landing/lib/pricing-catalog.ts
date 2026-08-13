export const PRICING_CATALOG_VERSION = "2026-07-31" as const;
export const PRICING_CURRENCY = "USD" as const;
export const RECOMMENDED_PLAN_KEY = "growth" as const;

export type Locale = "en" | "vi";
export type PlanKey = "starter" | "growth" | "custom";

type LocalizedText = Record<Locale, string>;

type PricingSupplement = {
  comparison: {
    eyebrow: string;
    title: string;
    lead: string;
    plan: string;
    monthly: string;
    setup: string;
    allowance: string;
    overage: string;
    location: string;
    startsAt: string;
  };
  usage: {
    eyebrow: string;
    title: string;
    lead: string;
    items: readonly { title: string; body: string }[];
  };
  faq: {
    eyebrow: string;
    title: string;
    items: readonly { question: string; answer: string }[];
  };
};

export type PricingPlan = {
  key: PlanKey;
  name: LocalizedText;
  monthly: { amount: number; startsAt: boolean };
  setup: { amount: number; startsAt: boolean };
  allowance: LocalizedText;
  overage: LocalizedText;
  location: LocalizedText;
  cta: LocalizedText;
  features: Record<Locale, readonly string[]>;
  disclaimer: LocalizedText;
  recommended: boolean;
};

export const pricingPlans: readonly PricingPlan[] = [
  {
    key: "starter",
    name: { en: "Starter", vi: "Khởi đầu" },
    monthly: { amount: 200, startsAt: false },
    setup: { amount: 300, startsAt: false },
    allowance: { en: "300 connected AI voice minutes / month", vi: "300 phút hội thoại AI voice đã kết nối / tháng" },
    overage: { en: "$0.40 per additional minute", vi: "$0.40 cho mỗi phút vượt hạn mức" },
    location: { en: "One salon location", vi: "Một địa điểm salon" },
    cta: { en: "Request Starter Setup", vi: "Yêu cầu thiết lập Starter" },
    features: {
      en: [
        "AI phone coverage after launch setup is completed.",
        "English call handling.",
        "Answers based on approved salon services, pricing, hours and policies.",
        "Tianna AI Calendar included.",
        "Appointment booking, rescheduling and cancellation after calendar setup is completed.",
        "Call transcripts and summaries.",
        "One salon location."
      ],
      vi: [
        "Tiếp nhận cuộc gọi bằng AI sau khi hoàn tất thiết lập ra mắt.",
        "Xử lý cuộc gọi bằng tiếng Anh.",
        "Trả lời dựa trên dịch vụ, giá, giờ làm việc và chính sách đã được tiệm duyệt.",
        "Bao gồm Tianna AI Calendar.",
        "Đặt, đổi và hủy lịch hẹn sau khi hoàn tất thiết lập lịch.",
        "Transcript và tóm tắt cuộc gọi.",
        "Một địa điểm salon."
      ]
    },
    disclaimer: {
      en: "New accounts begin in request-only mode. Tianna AI Calendar appointment actions become available after calendar setup is completed, reviewed and activated. Usage is measured in connected AI voice conversation minutes.",
      vi: "Tài khoản mới bắt đầu ở chế độ chỉ ghi nhận yêu cầu. Các thao tác lịch hẹn trên Tianna AI Calendar được mở sau khi cấu hình lịch hoàn tất, được review và kích hoạt. Usage được tính theo số phút hội thoại AI voice đã kết nối."
    },
    recommended: false
  },
  {
    key: "growth",
    name: { en: "Growth", vi: "Tăng trưởng" },
    monthly: { amount: 450, startsAt: false },
    setup: { amount: 750, startsAt: false },
    allowance: { en: "1,000 connected AI voice minutes / month", vi: "1.000 phút hội thoại AI voice đã kết nối / tháng" },
    overage: { en: "$0.35 per additional minute", vi: "$0.35 cho mỗi phút vượt hạn mức" },
    location: { en: "One salon location", vi: "Một địa điểm salon" },
    cta: { en: "Choose Growth", vi: "Chọn Growth" },
    features: {
      en: [
        "Everything in Starter.",
        "Higher monthly AI voice allowance.",
        "Lower additional-minute rate.",
        "Square Appointments integration after Square connection and booking setup are completed.",
        "Priority onboarding.",
        "Monthly configuration and conversation-quality review."
      ],
      vi: [
        "Bao gồm toàn bộ tính năng của Starter.",
        "Hạn mức AI voice hàng tháng cao hơn.",
        "Phí phút bổ sung thấp hơn.",
        "Tích hợp Square Appointments sau khi hoàn tất kết nối Square và thiết lập booking.",
        "Ưu tiên onboarding.",
        "Review cấu hình và chất lượng hội thoại mỗi tháng."
      ]
    },
    disclaimer: {
      en: "Selecting Growth does not activate Square Appointments by itself. Square appointment actions become available only after its connection and booking setup are completed and approved for the salon.",
      vi: "Việc chọn Growth không tự kích hoạt Square Appointments. Các thao tác lịch hẹn qua Square chỉ được mở sau khi hoàn tất kết nối và thiết lập booking cho salon."
    },
    recommended: true
  },
  {
    key: "custom",
    name: { en: "Custom", vi: "Theo nhu cầu" },
    monthly: { amount: 900, startsAt: true },
    setup: { amount: 1500, startsAt: true },
    allowance: { en: "Starts at 2,500 connected AI voice minutes / month", vi: "Từ 2.500 phút hội thoại AI voice đã kết nối / tháng" },
    overage: { en: "Allowance and overage set in the signed quote", vi: "Hạn mức và phí vượt mức theo báo giá đã ký" },
    location: { en: "High-volume or multi-location rollout", vi: "Cho salon lưu lượng lớn hoặc nhiều địa điểm" },
    cta: { en: "Talk to Sales", vi: "Liên hệ tư vấn" },
    features: {
      en: [
        "Everything in Growth.",
        "High-volume or multi-location rollout.",
        "Each salon location is set up and managed separately unless the signed agreement states otherwise.",
        "Custom usage allowance.",
        "Dedicated rollout planning.",
        "Contracted support response targets where included in the signed agreement.",
        "Evaluation of additional integrations as separately quoted work."
      ],
      vi: [
        "Bao gồm toàn bộ tính năng của Growth.",
        "Triển khai cho salon có lưu lượng lớn hoặc nhiều địa điểm.",
        "Mỗi địa điểm salon được thiết lập và quản lý riêng, trừ khi thỏa thuận đã ký quy định khác.",
        "Hạn mức sử dụng tùy chỉnh.",
        "Kế hoạch triển khai riêng.",
        "Mục tiêu phản hồi support theo hợp đồng nếu có trong thỏa thuận đã ký.",
        "Đánh giá integration bổ sung theo báo giá riêng."
      ]
    },
    disclaimer: {
      en: "Custom pricing depends on locations, usage, onboarding complexity, supported integrations and contracted support. Custom does not guarantee an unsupported POS or provider.",
      vi: "Giá Custom phụ thuộc vào số địa điểm, mức sử dụng, độ phức tạp onboarding, integration được hỗ trợ và support theo hợp đồng. Custom không đảm bảo triển khai POS hoặc provider chưa được hỗ trợ."
    },
    recommended: false
  }
] as const;

export const globalPricingDisclaimer: LocalizedText = {
  en: "This website does not collect payment or start a subscription. Prices are planning information; taxes, third-party charges and separately quoted integrations are not included unless stated in a signed agreement.",
  vi: "Website này không thu tiền hoặc tự bắt đầu subscription. Giá được cung cấp để tham khảo; thuế, chi phí bên thứ ba và integration có báo giá riêng không được bao gồm, trừ khi thỏa thuận đã ký có quy định khác."
};

export const pricingSupplement: Record<Locale, PricingSupplement> = {
  en: {
    comparison: {
      eyebrow: "Compare plans",
      title: "Three plans for different call volumes.",
      lead: "Plans change included voice usage and rollout support. Appointment features become available after the selected calendar setup is completed.",
      plan: "Plan",
      monthly: "Monthly price",
      setup: "Initial setup — paid once",
      allowance: "Included usage",
      overage: "Additional usage",
      location: "Rollout scope",
      startsAt: "From"
    },
    usage: {
      eyebrow: "Usage & setup",
      title: "Know what the numbers mean before setup starts.",
      lead: "The public price is a planning reference. Final commercial terms and any separately scoped work belong in the signed agreement.",
      items: [
        { title: "Connected AI voice minutes", body: "Usage is measured while the AI voice conversation is connected. The listed monthly allowance is not a count of calls or appointments." },
        { title: "Additional minutes", body: "Starter and Growth list a per-minute amount above the included allowance. Custom allowance and overage terms are set in the signed quote." },
        { title: "Initial setup — paid once", body: "This separate onboarding fee covers the agreed launch scope: salon profile, services, staff, business hours, AI receptionist call flow, Tianna AI Calendar booking rules and launch-readiness review. It is paid once, not every month. Third-party charges and separately quoted integrations are not included." }
      ]
    },
    faq: {
      eyebrow: "Pricing FAQ",
      title: "A few useful details before requesting a demo.",
      items: [
        { question: "Can I purchase a plan on this website?", answer: "No. This website sends a demo and setup request to our Operations team for review. It does not collect payment or start a subscription." },
        { question: "When does automatic appointment booking become available?", answer: "New accounts begin in request-only mode. Tianna AI Calendar appointment actions become available after its setup is completed, reviewed and activated. Square Appointments also requires its own completed connection and booking setup." },
        { question: "What happens when usage exceeds the allowance?", answer: "Starter and Growth show the additional per-minute amount in the current pricing catalog. Invoicing, taxes and other commercial terms are governed by the signed agreement, not by submitting this form." },
        { question: "Does Custom guarantee any POS integration?", answer: "No. Custom can include evaluation of separately scoped integrations, but it does not guarantee an unsupported POS or provider." }
      ]
    }
  },
  vi: {
    comparison: {
      eyebrow: "So sánh gói",
      title: "Ba gói cho các mức lưu lượng cuộc gọi khác nhau.",
      lead: "Mỗi gói có hạn mức voice và mức hỗ trợ triển khai khác nhau. Tính năng lịch hẹn được mở sau khi hoàn tất thiết lập lịch đã chọn.",
      plan: "Gói",
      monthly: "Giá hàng tháng",
      setup: "Thiết lập ban đầu — trả một lần",
      allowance: "Usage bao gồm",
      overage: "Usage bổ sung",
      location: "Phạm vi rollout",
      startsAt: "Từ"
    },
    usage: {
      eyebrow: "Usage & thiết lập",
      title: "Hiểu rõ các con số trước khi bắt đầu thiết lập.",
      lead: "Giá công khai dùng để tham khảo khi lập kế hoạch. Điều khoản thương mại cuối cùng và công việc có scope riêng thuộc thỏa thuận đã ký.",
      items: [
        { title: "Phút hội thoại AI voice đã kết nối", body: "Usage được tính trong thời gian hội thoại AI voice đang kết nối. Hạn mức hàng tháng không phải số cuộc gọi hoặc số lịch hẹn." },
        { title: "Phút sử dụng bổ sung", body: "Starter và Growth công bố mức phí theo phút vượt hạn mức. Hạn mức và phí vượt mức của Custom được xác định trong báo giá đã ký." },
        { title: "Thiết lập ban đầu — trả một lần", body: "Khoản onboarding riêng này bao gồm phạm vi ra mắt đã thống nhất: hồ sơ salon, dịch vụ, nhân viên, giờ làm việc, call flow của AI lễ tân, quy tắc booking trên Tianna AI Calendar và review mức độ sẵn sàng trước khi ra mắt. Khoản này chỉ trả một lần, không thu mỗi tháng. Chi phí bên thứ ba và integration có báo giá riêng không được bao gồm." }
      ]
    },
    faq: {
      eyebrow: "Câu hỏi về giá",
      title: "Một số thông tin cần biết trước khi yêu cầu demo.",
      items: [
        { question: "Có thể mua gói trực tiếp trên website không?", answer: "Không. Website gửi yêu cầu demo và thiết lập đến đội ngũ Operations để review. Website không thu tiền hoặc tự bắt đầu subscription." },
        { question: "Khi nào tính năng tự động đặt lịch được mở?", answer: "Tài khoản mới bắt đầu ở chế độ chỉ ghi nhận yêu cầu. Các thao tác lịch hẹn trên Tianna AI Calendar được mở sau khi hoàn tất thiết lập, review và kích hoạt. Square Appointments cũng cần hoàn tất kết nối và thiết lập booking riêng." },
        { question: "Điều gì xảy ra khi usage vượt hạn mức?", answer: "Starter và Growth công bố phí bổ sung theo phút trong pricing catalog hiện tại. Cách lập hóa đơn, thuế và các điều khoản thương mại khác được điều chỉnh bởi thỏa thuận đã ký, không phải bởi việc gửi form." },
        { question: "Custom có đảm bảo tích hợp với mọi POS không?", answer: "Không. Custom có thể bao gồm việc đánh giá integration theo scope riêng, nhưng không đảm bảo một POS hoặc provider chưa được hỗ trợ." }
      ]
    }
  }
};

export function getPlan(key: PlanKey) {
  const plan = pricingPlans.find((candidate) => candidate.key === key);
  if (!plan) throw new Error(`Unsupported pricing plan: ${key}`);
  return plan;
}

export function isPlanKey(value: string): value is PlanKey {
  return pricingPlans.some((plan) => plan.key === value);
}
