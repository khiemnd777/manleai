"use client";

import Link from "next/link";
import { useEffect, useState } from "react";
import { contentFor } from "@/lib/marketing-content";
import { globalPricingDisclaimer, pricingPlans, pricingSupplement, PRICING_CATALOG_VERSION, type Locale, type PlanKey } from "@/lib/pricing-catalog";
import { LanguageLinks, MarketingFooter } from "./marketing-site";
import { RegistrationDialog } from "./registration-dialog";
import styles from "./marketing.module.css";

const c = (...names: Array<string | false | null | undefined>) =>
  names.filter(Boolean).map((name) => styles[name as string]).join(" ");

export function PricingPage({ locale }: { locale: Locale }) {
  const content = contentFor(locale);
  const pricingCopy = pricingSupplement[locale];
  const prefix = locale === "vi" ? "/vi" : "";
  const [menuOpen, setMenuOpen] = useState(false);
  const [scrolled, setScrolled] = useState(false);
  const [openFaq, setOpenFaq] = useState(0);
  const [dialog, setDialog] = useState<{ open: boolean; plan?: PlanKey; trigger?: HTMLElement | null }>({ open: false });

  useEffect(() => {
    const onScroll = () => setScrolled(window.scrollY > 30);
    window.addEventListener("scroll", onScroll, { passive: true });
    onScroll();
    return () => window.removeEventListener("scroll", onScroll);
  }, []);

  const open = (trigger: HTMLElement, plan?: PlanKey) => {
    setMenuOpen(false);
    setDialog({ open: true, plan, trigger });
  };

  const comparisonRows = [
    { label: pricingCopy.comparison.monthly, values: pricingPlans.map((plan) => `${plan.monthly.startsAt ? `${pricingCopy.comparison.startsAt} ` : ""}$${plan.monthly.amount.toLocaleString("en-US")}/${locale === "vi" ? "tháng" : "month"}`) },
    { label: pricingCopy.comparison.setup, values: pricingPlans.map((plan) => `${plan.setup.startsAt ? `${pricingCopy.comparison.startsAt} ` : ""}$${plan.setup.amount.toLocaleString("en-US")}`) },
    { label: pricingCopy.comparison.allowance, values: pricingPlans.map((plan) => plan.allowance[locale]) },
    { label: pricingCopy.comparison.overage, values: pricingPlans.map((plan) => plan.overage[locale]) },
    { label: pricingCopy.comparison.location, values: pricingPlans.map((plan) => plan.location[locale]) }
  ] as const;

  return <div className={c("page-shell")}>
    <a className={c("skip-link")} href="#pricing-main">{locale === "vi" ? "Đi đến bảng giá" : "Skip to pricing"}</a>
    <header className={c("site-header", scrolled && "scrolled")} id="top"><nav className={c("nav", "container")} aria-label={locale === "vi" ? "Điều hướng chính" : "Primary navigation"}>
      <Link className={c("brand")} href={prefix || "/"} aria-label="Tianna AI home"><img src="/brand/icon-192.png" alt="Tianna AI logo" width="54" height="54" /><span className={c("brand-wordmark")}>Tianna <span>AI</span></span></Link>
      <div className={c("nav-links", menuOpen && "open")} id="pricingNavLinks"><Link href={`${prefix || "/"}#why`}>{content.nav.why}</Link><Link href={`${prefix || "/"}#features`}>{content.nav.features}</Link><Link href={`${prefix || "/"}#workflow`}>{content.nav.workflow}</Link><Link href={`${prefix}/pricing`} aria-current="page">{content.nav.pricing}</Link><Link href={`${prefix || "/"}#faq`}>{content.nav.faq}</Link><div className={c("mobile-nav-language")}><span>{locale === "vi" ? "Ngôn ngữ" : "Language"}</span><LanguageLinks locale={locale} pricing /></div></div>
      <div className={c("nav-actions")}><LanguageLinks locale={locale} pricing /><button className={c("button", "button-small", "button-primary", "desktop-cta")} type="button" onClick={(event) => open(event.currentTarget)}>{content.nav.request}</button><button className={c("menu-button")} type="button" aria-expanded={menuOpen} aria-controls="pricingNavLinks" aria-label={locale === "vi" ? "Mở menu" : "Open menu"} onClick={() => setMenuOpen((value) => !value)}><span /><span /><span /></button></div>
    </nav></header>

    <main id="pricing-main">
      <section className={c("pricing-hero")}><div className={c("hero-orb", "hero-orb-blue")} aria-hidden="true" /><div className={c("hero-orb", "hero-orb-red")} aria-hidden="true" /><div className={c("container")}><span className={c("eyebrow")}><span className={c("live-dot")} />{locale === "vi" ? "Bảng giá rõ ràng" : "Straightforward pricing"}</span><h1>{locale === "vi" ? <>Chọn mức hỗ trợ phù hợp<br /><span>với lượng cuộc gọi.</span></> : <>Choose the right level<br /><span>of call coverage.</span></>}</h1><p>{locale === "vi" ? "Không checkout hoặc tự tạo subscription. Mỗi yêu cầu được review trước khi thiết lập tài khoản salon." : "No checkout and no automatic subscription. Every request is reviewed before a salon account is set up."}</p><small>{locale === "vi" ? "Phiên bản bảng giá" : "Pricing catalog version"}: {PRICING_CATALOG_VERSION}</small></div></section>

      <section className={c("pricing-section")}><div className={c("container", "pricing-grid")}>{pricingPlans.map((plan) => <article key={plan.key} className={c("price-card", plan.recommended && "recommended-card")}>
        {plan.recommended ? <div className={c("recommended-label")}>{locale === "vi" ? "Đề xuất" : "Recommended"}</div> : null}
        <div className={c("price-card-head")}><span>{plan.name[locale]}</span><div className={c("price")}><small>{plan.monthly.startsAt ? (locale === "vi" ? "Từ" : "From") : ""}</small><strong>${plan.monthly.amount.toLocaleString("en-US")}</strong><em>/{locale === "vi" ? "tháng" : "month"}</em></div><p>{locale === "vi" ? "Thiết lập ban đầu — trả một lần" : "Initial setup — paid once"}: {plan.setup.startsAt ? (locale === "vi" ? "từ " : "from ") : ""}<b>${plan.setup.amount.toLocaleString("en-US")}</b></p></div>
        <div className={c("usage-box")}><strong>{plan.allowance[locale]}</strong><span>{plan.overage[locale]}</span><span>{plan.location[locale]}</span></div>
        <ul>{plan.features[locale].map((item) => <li key={item}><CheckIcon />{item}</li>)}</ul><p className={c("plan-disclaimer")}>{plan.disclaimer[locale]}</p><button type="button" className={c("button", plan.recommended ? "button-primary" : "button-dark", "full-button")} onClick={(event) => open(event.currentTarget, plan.key)}>{plan.cta[locale]}<ArrowIcon /></button>
      </article>)}</div><p className={c("container", "global-disclaimer")}>{globalPricingDisclaimer[locale]}</p></section>

      <section className={c("pricing-comparison")}><div className={c("container")}><PricingHeading eyebrow={pricingCopy.comparison.eyebrow} title={pricingCopy.comparison.title} lead={pricingCopy.comparison.lead} /><div className={c("comparison-scroller")}><table className={c("comparison-table")}><thead><tr><th scope="col">{pricingCopy.comparison.plan}</th>{pricingPlans.map((plan) => <th scope="col" key={plan.key}>{plan.name[locale]}{plan.recommended ? <small>{locale === "vi" ? "Đề xuất" : "Recommended"}</small> : null}</th>)}</tr></thead><tbody>{comparisonRows.map((row) => <ComparisonRow key={row.label} label={row.label} values={row.values} />)}</tbody></table></div><div className={c("comparison-mobile")} aria-label={locale === "vi" ? "So sánh các gói" : "Plan comparison"}>{pricingPlans.map((plan, planIndex) => <article key={plan.key} className={c("comparison-mobile-plan", plan.recommended && "comparison-mobile-recommended")}><div className={c("comparison-mobile-head")}><h3>{plan.name[locale]}</h3>{plan.recommended ? <small>{locale === "vi" ? "Đề xuất" : "Recommended"}</small> : null}</div><dl>{comparisonRows.map((row) => <div className={c("comparison-mobile-row")} key={`${plan.key}-${row.label}`}><dt>{row.label}</dt><dd>{row.values[planIndex]}</dd></div>)}</dl></article>)}</div></div></section>

      <section className={c("pricing-usage")}><div className={c("container")}><PricingHeading eyebrow={pricingCopy.usage.eyebrow} title={pricingCopy.usage.title} lead={pricingCopy.usage.lead} /><div className={c("usage-explanation-grid")}>{pricingCopy.usage.items.map((item, index) => <article key={item.title}><span>{String(index + 1).padStart(2, "0")}</span><h3>{item.title}</h3><p>{item.body}</p></article>)}</div></div></section>

      <section className={c("pricing-faq")}><div className={c("container", "faq-grid")}><div className={c("faq-heading")}><span className={c("kicker")}>{pricingCopy.faq.eyebrow}</span><h2>{pricingCopy.faq.title}</h2></div><div className={c("accordion")}>{pricingCopy.faq.items.map((item, index) => <article key={item.question} className={c("accordion-item", openFaq === index && "open")}><button type="button" aria-expanded={openFaq === index} onClick={() => setOpenFaq((current) => current === index ? -1 : index)}><span>{item.question}</span><i /></button><div className={c("accordion-content")}><p>{item.answer}</p></div></article>)}</div></div></section>

      <section className={c("pricing-truth")}><div className={c("container")}><span className={c("kicker", "light")}>{locale === "vi" ? "Sẵn sàng đặt lịch" : "Booking readiness"}</span><h2>{locale === "vi" ? "Salon luôn kiểm soát lịch hẹn." : "Calendar control stays with your salon."}</h2><p>{locale === "vi" ? "Tài khoản mới bắt đầu ở chế độ chỉ ghi nhận yêu cầu. Tianna AI Calendar chỉ thực hiện lịch hẹn sau khi thiết lập được hoàn tất, review và kích hoạt. Square Appointments cần hoàn tất kết nối và thiết lập booking riêng." : "New accounts begin in request-only mode. Tianna AI Calendar handles appointments only after setup is completed, reviewed and activated. Square Appointments requires its own completed connection and booking setup."}</p></div></section>
    </main>

    <MarketingFooter locale={locale} onRequest={open} />
    <RegistrationDialog open={dialog.open} locale={locale} source="pricing" plan={dialog.plan} returnFocus={dialog.trigger} onClose={() => setDialog((current) => ({ ...current, open: false }))} />
  </div>;
}

function PricingHeading({ eyebrow, title, lead }: { eyebrow: string; title: string; lead: string }) {
  return <div className={c("pricing-section-heading")}><span className={c("kicker")}>{eyebrow}</span><h2>{title}</h2><p>{lead}</p></div>;
}

function ComparisonRow({ label, values }: { label: string; values: readonly string[] }) {
  return <tr><th scope="row">{label}</th>{values.map((value, index) => <td key={`${label}-${pricingPlans[index].key}`}>{value}</td>)}</tr>;
}

function ArrowIcon() { return <svg aria-hidden="true" viewBox="0 0 24 24"><path d="M5 12h14M13 6l6 6-6 6" /></svg>; }
function CheckIcon() { return <svg aria-hidden="true" viewBox="0 0 24 24"><path d="m5 12 4 4L19 6" /></svg>; }
