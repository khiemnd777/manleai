import type { Locale } from "./pricing-catalog";

export const marketingContent = {
  en: {
    nav: {
      why: "Why Manle",
      features: "Features",
      workflow: "How it works",
      pricing: "Pricing",
      faq: "FAQ",
      request: "Book a demo"
    },
    hero: {
      eyebrow: "AI receptionist made for nail salons",
      title: "Never miss the",
      titleAccent: "next booking.",
      lead: "ManleAI helps nail salons handle English calls, answer from salon-approved information, capture appointment requests, and hand exceptions back to the team.",
      primary: "Get a free demo",
      secondary: "Hear how it works",
      trust: ["English call handling", "Nail-salon workflows", "Human handoff"],
      alwaysOn: "always on",
      callLanguage: "call handling",
      incomingCall: "Incoming call",
      newCustomer: "New customer",
      note: "By default, appointment requests remain pending for owner review. Confirmation is available only through a configured, ready scheduling workflow."
    },
    quickValues: [
      { title: "Answer every call", body: "Even while every technician is busy." },
      { title: "Route with confidence", body: "Services, staff, timing, and salon rules." },
      { title: "Keep the human touch", body: "Return complex calls with clear context." }
    ],
    problems: {
      kicker: "The front-desk problem",
      title: "Your hands are busy.",
      titleAccent: "The phone is still ringing.",
      lead: "A missed call can become a missed opportunity. ManleAI protects the team’s focus without leaving callers without a clear next step.",
      items: [
        { tag: "Missed opportunity", title: "Calls go unanswered", body: "Customers often call the next salon instead of leaving a voicemail." },
        { tag: "Broken focus", title: "Services get interrupted", body: "Technicians stop mid-service to answer pricing, availability, and direction questions." },
        { tag: "Inconsistent answers", title: "Details get lost", body: "Services, prices and policies can be described differently when the team is not using the salon’s approved information." }
      ]
    },
    features: {
      kicker: "Built for nail salons",
      title: "Not a generic chatbot.",
      titleAccent: "A real front-desk teammate.",
      lead: "Configure ManleAI around salon services, prices, business hours, policies, staff preferences, and the explicitly selected scheduling workflow.",
      items: [
        { title: "Natural English conversations", body: "Callers can speak naturally in English while ManleAI follows the salon’s approved information and call flow." },
        { title: "Salon-approved answers", body: "Services, prices, hours, and policies come from the salon’s managed information." },
        { title: "Request-safe scheduling", body: "Owner Manual records a pending owner-review request and never confirms automatically." },
        { title: "Smart human handoff", body: "Complex or sensitive requests can return to the salon team with useful context." },
        { title: "Evidence before confirmation", body: "A ready ManleAI Calendar or supported external workflow confirms only after durable booking evidence exists." }
      ],
      notices: [
        { title: "Request received", body: "Pending salon review" },
        { title: "Call context saved", body: "Available where runtime evidence exists" }
      ]
    },
    workflow: {
      kicker: "From hello to the next step",
      title: "One smooth call.",
      titleAccent: "Four clear steps.",
      lead: "ManleAI follows the salon’s configured phone and scheduling rules, then uses wording that matches the evidence actually returned.",
      cta: "See it for my salon",
      steps: [
        { title: "Answer promptly", body: "Greet callers through the salon’s configured phone runtime." },
        { title: "Understand the request", body: "Collect service, date, time, staff preference, and contact details." },
        { title: "Use the selected workflow", body: "Route new work through the salon’s explicitly selected scheduling authority." },
        { title: "Respond from evidence", body: "Say pending for Owner Manual, or confirm only after a ready workflow returns durable evidence." }
      ]
    },
    simulation: {
      salon: "Bella Nails Studio",
      role: "AI Receptionist",
      live: "Live call",
      kicker: "A receptionist customers can understand",
      title: "Fast enough for business.",
      titleAccent: "Careful enough for salon operations.",
      lead: "The default example below uses Owner Manual: the request is recorded for salon review and is not presented as a confirmed appointment.",
      lines: [
        { role: "ai", label: "ManleAI", text: "Thanks for calling Bella Nails Studio. How can I help you today?" },
        { role: "customer", label: "Customer", text: "Can I request a gel manicure this Friday afternoon?" },
        { role: "ai", label: "ManleAI", text: "Absolutely. I’ll record the service, preferred time, and your contact details." },
        { role: "customer", label: "Customer", text: "Friday after 2:00 works." },
        { role: "ai", label: "ManleAI", text: "Your request is received and pending salon review. The salon will confirm the final time." }
      ],
      benefits: [
        { title: "Your voice, your rules", body: "Configure greeting, tone, services, and policies." },
        { title: "Clear operational records", body: "Keep the request outcome and caller details organized." },
        { title: "Designed to improve", body: "Review conversation evidence and refine salon knowledge." }
      ]
    },
    outcomes: {
      kicker: "What your team gets back",
      title: "More focus at the table.",
      titleAccent: "Clearer action at the front desk.",
      items: [
        { metric: "24/7", label: "Call coverage when ready" },
        { metric: "ENGLISH", label: "Launch call language" },
        { metric: "1 flow", label: "Call to next action" },
        { metric: "100%", label: "Your salon rules" }
      ],
      note: "Capabilities depend on selected integrations, salon-scoped configuration, phone runtime, scheduling authority, and operation readiness."
    },
    integration: {
      kicker: "Fits your operation",
      title: "Connect the tools",
      titleAccent: "your salon actually uses.",
      body: "ManleAI uses an integration layer for supported calendars, booking systems, POS platforms, messaging, and internal workflows. Square Appointments is the currently implemented external provider; a connection never selects scheduling authority by itself.",
      cta: "Discuss my setup",
      categories: { calendar: "Calendar", messaging: "Messaging", team: "Team" }
    },
    faq: {
      kicker: "Questions, answered",
      title: "Good questions.",
      titleAccent: "Clear answers.",
      lead: "Every salon operates differently. Setup is reviewed against actual services, rules, runtime configuration, and the selected scheduling workflow.",
      items: [
        { question: "Which call language is available at launch?", answer: "English. The marketing site and onboarding contact remain available in English and Vietnamese for salon owners." },
        { question: "Does ManleAI automatically confirm every appointment?", answer: "No. Owner Manual creates a pending owner-review request. ManleAI Calendar or Square confirms only after its configured workflow succeeds with durable evidence." },
        { question: "What happens when a customer needs a person?", answer: "The call can be transferred or escalated according to the salon’s configured rules, with context available where the runtime provides that evidence." },
        { question: "Does connecting Square turn on booking?", answer: "No. Provider connection does not select scheduling authority or prove booking readiness." },
        { question: "Can ManleAI work with every POS?", answer: "No broad compatibility is promised. Square Appointments is the implemented external provider; other integrations require separate evaluation." }
      ]
    },
    final: {
      kicker: "Your next caller could be your next regular",
      title: "Let ManleAI",
      titleAccent: "pick up the phone.",
      body: "Tell us about your salon. Platform Operations reviews the request before any Tenant is provisioned.",
      cta: "Book my free demo"
    },
    footer: {
      text: "The English AI receptionist built for nail salon operations.",
      ready: "Ready to talk?",
      rights: "All rights reserved.",
      note: "AI receptionist for nail salons in the United States."
    }
  },
  vi: {
    nav: {
      why: "Vì sao Manle",
      features: "Tính năng",
      workflow: "Cách hoạt động",
      pricing: "Bảng giá",
      faq: "Hỏi đáp",
      request: "Đặt lịch demo"
    },
    hero: {
      eyebrow: "AI lễ tân được thiết kế riêng cho tiệm nail",
      title: "Không bỏ lỡ",
      titleAccent: "cơ hội đặt lịch tiếp theo.",
      lead: "ManleAI giúp tiệm nail xử lý cuộc gọi tiếng Anh, trả lời từ thông tin được tiệm duyệt, ghi nhận yêu cầu lịch hẹn và chuyển ngoại lệ về cho đội ngũ.",
      primary: "Nhận demo miễn phí",
      secondary: "Xem cách hoạt động",
      trust: ["Cuộc gọi tiếng Anh", "Quy trình riêng cho nail", "Chuyển tiếp nhân viên"],
      alwaysOn: "luôn trực",
      callLanguage: "xử lý cuộc gọi",
      incomingCall: "Cuộc gọi đến",
      newCustomer: "Khách hàng mới",
      note: "Mặc định, yêu cầu lịch hẹn ở trạng thái chờ chủ tiệm review. Chỉ workflow đã cấu hình và sẵn sàng mới có thể xác nhận lịch."
    },
    quickValues: [
      { title: "Nghe mọi cuộc gọi", body: "Ngay cả khi tất cả thợ đang bận." },
      { title: "Điều phối tự tin", body: "Dịch vụ, nhân viên, thời gian và quy tắc tiệm." },
      { title: "Giữ sự phục vụ tận tâm", body: "Trả ngoại lệ về đội ngũ kèm ngữ cảnh rõ ràng." }
    ],
    problems: {
      kicker: "Bài toán tại quầy lễ tân",
      title: "Tay bạn đang bận.",
      titleAccent: "Điện thoại vẫn đang reo.",
      lead: "Một cuộc gọi bị lỡ có thể là một cơ hội bị mất. ManleAI giúp đội ngũ tập trung mà người gọi vẫn có bước tiếp theo rõ ràng.",
      items: [
        { tag: "Mất cơ hội", title: "Cuộc gọi không được nghe", body: "Khách thường gọi sang tiệm khác thay vì để lại lời nhắn thoại." },
        { tag: "Gián đoạn công việc", title: "Dịch vụ bị ngắt quãng", body: "Thợ phải dừng giữa chừng để trả lời về giá, lịch trống hoặc đường đi." },
        { tag: "Câu trả lời thiếu nhất quán", title: "Thông tin dễ bị sai lệch", body: "Dịch vụ, giá và chính sách có thể được mô tả khác nhau khi đội ngũ không dùng thông tin đã được tiệm duyệt." }
      ]
    },
    features: {
      kicker: "Sinh ra cho tiệm nail",
      title: "Không phải chatbot đại trà.",
      titleAccent: "Mà là một đồng đội lễ tân.",
      lead: "Cấu hình ManleAI theo dịch vụ, bảng giá, giờ làm, chính sách, sở thích nhân viên và scheduling workflow được tiệm chọn rõ ràng.",
      items: [
        { title: "Hội thoại tiếng Anh tự nhiên", body: "Khách có thể giao tiếp tự nhiên bằng tiếng Anh trong khi ManleAI tuân thủ thông tin và call flow đã được tiệm duyệt." },
        { title: "Câu trả lời được tiệm duyệt", body: "Dịch vụ, giá, giờ làm và chính sách đến từ thông tin do tiệm quản lý." },
        { title: "Scheduling theo dạng yêu cầu", body: "Owner Manual ghi nhận yêu cầu chờ chủ tiệm review và không tự xác nhận." },
        { title: "Chuyển tiếp thông minh", body: "Yêu cầu phức tạp hoặc nhạy cảm có thể được chuyển về đội ngũ kèm ngữ cảnh hữu ích." },
        { title: "Có evidence trước khi xác nhận", body: "ManleAI Calendar hoặc external workflow được hỗ trợ chỉ xác nhận sau khi có booking evidence bền vững." }
      ],
      notices: [
        { title: "Đã nhận yêu cầu", body: "Đang chờ salon review" },
        { title: "Đã lưu ngữ cảnh cuộc gọi", body: "Khi runtime có evidence tương ứng" }
      ]
    },
    workflow: {
      kicker: "Từ lời chào đến bước tiếp theo",
      title: "Một cuộc gọi mượt mà.",
      titleAccent: "Bốn bước rõ ràng.",
      lead: "ManleAI tuân thủ phone và scheduling rules đã cấu hình, sau đó dùng cách diễn đạt đúng với evidence thực tế được trả về.",
      cta: "Xem demo cho tiệm tôi",
      steps: [
        { title: "Nghe máy kịp thời", body: "Chào khách qua phone runtime đã cấu hình của salon." },
        { title: "Hiểu yêu cầu", body: "Thu thập dịch vụ, ngày, giờ, thợ mong muốn và thông tin liên hệ." },
        { title: "Dùng workflow đã chọn", body: "Điều phối công việc mới qua scheduling authority được salon chọn rõ ràng." },
        { title: "Phản hồi theo evidence", body: "Thông báo chờ review với Owner Manual, hoặc chỉ xác nhận khi workflow sẵn sàng có evidence bền vững." }
      ]
    },
    simulation: {
      salon: "Bella Nails Studio",
      role: "AI Lễ Tân",
      live: "Đang gọi",
      kicker: "Một lễ tân khách hàng dễ hiểu",
      title: "Nhanh cho vận hành.",
      titleAccent: "Cẩn trọng với quy trình salon.",
      lead: "Ví dụ mặc định dưới đây dùng Owner Manual: yêu cầu được ghi nhận để salon review và không được trình bày như lịch đã xác nhận.",
      lines: [
        { role: "ai", label: "ManleAI", text: "Thanks for calling Bella Nails Studio. How can I help you today?" },
        { role: "customer", label: "Customer", text: "Can I request a gel manicure this Friday afternoon?" },
        { role: "ai", label: "ManleAI", text: "Absolutely. I’ll record the service, preferred time, and your contact details." },
        { role: "customer", label: "Customer", text: "Friday after 2:00 works." },
        { role: "ai", label: "ManleAI", text: "Your request is received and pending salon review. The salon will confirm the final time." }
      ],
      benefits: [
        { title: "Giọng điệu và quy tắc của bạn", body: "Cấu hình lời chào, phong cách, dịch vụ và chính sách." },
        { title: "Thông tin vận hành rõ ràng", body: "Sắp xếp kết quả yêu cầu và dữ liệu người gọi." },
        { title: "Được thiết kế để cải thiện", body: "Review conversation evidence và cập nhật kiến thức của tiệm." }
      ]
    },
    outcomes: {
      kicker: "Những gì đội ngũ nhận lại",
      title: "Tập trung hơn tại bàn nail.",
      titleAccent: "Rõ ràng hơn tại quầy lễ tân.",
      items: [
        { metric: "24/7", label: "Tiếp nhận khi runtime sẵn sàng" },
        { metric: "ENGLISH", label: "Ngôn ngữ cuộc gọi ban đầu" },
        { metric: "1 flow", label: "Từ cuộc gọi đến bước tiếp theo" },
        { metric: "100%", label: "Theo quy tắc tiệm" }
      ],
      note: "Khả năng thực tế phụ thuộc integration đã chọn, cấu hình theo salon, phone runtime, scheduling authority và operation readiness."
    },
    integration: {
      kicker: "Phù hợp với vận hành",
      title: "Kết nối những công cụ",
      titleAccent: "tiệm thực sự sử dụng.",
      body: "ManleAI dùng integration layer cho lịch, hệ thống booking, POS, tin nhắn và quy trình nội bộ được hỗ trợ. Square Appointments là external provider hiện đã triển khai; kết nối không tự lựa chọn scheduling authority.",
      cta: "Trao đổi về hệ thống của tôi",
      categories: { calendar: "Lịch", messaging: "Tin nhắn", team: "Nhân viên" }
    },
    faq: {
      kicker: "Giải đáp thắc mắc",
      title: "Câu hỏi thực tế.",
      titleAccent: "Câu trả lời rõ ràng.",
      lead: "Mỗi tiệm có cách vận hành khác nhau. Việc thiết lập được review theo dịch vụ, quy tắc, runtime configuration và scheduling workflow đã chọn.",
      items: [
        { question: "Ngôn ngữ cuộc gọi nào được hỗ trợ khi ra mắt?", answer: "Tiếng Anh. Website marketing và liên hệ onboarding vẫn có tiếng Anh và tiếng Việt cho chủ salon." },
        { question: "ManleAI có tự xác nhận mọi lịch hẹn không?", answer: "Không. Owner Manual tạo yêu cầu chờ chủ tiệm review. ManleAI Calendar hoặc Square chỉ xác nhận sau khi workflow thành công và có evidence bền vững." },
        { question: "Điều gì xảy ra khi khách cần gặp người thật?", answer: "Cuộc gọi có thể được chuyển hoặc escalation theo rules đã cấu hình; ngữ cảnh đi kèm được cung cấp khi runtime có evidence đó." },
        { question: "Kết nối Square có tự bật booking không?", answer: "Không. Kết nối Square không tự bật booking. Salon chỉ dùng Square Appointments sau khi hoàn tất kết nối và thiết lập booking được review." },
        { question: "ManleAI có dùng được với mọi POS không?", answer: "Không có cam kết tương thích rộng. Square Appointments là external provider hiện đã triển khai; integration khác cần đánh giá riêng." }
      ]
    },
    final: {
      kicker: "Người gọi tiếp theo có thể trở thành khách quen",
      title: "Để ManleAI",
      titleAccent: "nghe điện thoại.",
      body: "Cho chúng tôi biết về salon. Platform Operations sẽ review yêu cầu trước khi bất kỳ Tenant nào được provision.",
      cta: "Đặt demo miễn phí"
    },
    footer: {
      text: "AI lễ tân xử lý cuộc gọi tiếng Anh, được thiết kế cho vận hành tiệm nail.",
      ready: "Sẵn sàng trao đổi?",
      rights: "Đã đăng ký bản quyền.",
      note: "AI lễ tân dành cho tiệm nail tại Hoa Kỳ."
    }
  }
} as const satisfies Record<Locale, object>;

export function contentFor(locale: Locale) {
  return marketingContent[locale];
}
