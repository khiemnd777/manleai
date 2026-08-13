"use client";

import Link from "next/link";
import { useEffect, useRef, useState } from "react";
import { contentFor } from "@/lib/marketing-content";
import type { Locale, PlanKey } from "@/lib/pricing-catalog";
import { RegistrationDialog } from "./registration-dialog";
import styles from "./marketing.module.css";

const c = (...names: Array<string | false | null | undefined>) =>
  names.filter(Boolean).map((name) => styles[name as string]).join(" ");

export function MarketingSite({ locale }: { locale: Locale }) {
  const content = contentFor(locale);
  const prefix = locale === "vi" ? "/vi" : "";
  const rootRef = useRef<HTMLDivElement>(null);
  const [menuOpen, setMenuOpen] = useState(false);
  const [scrolled, setScrolled] = useState(false);
  const [openFaq, setOpenFaq] = useState(0);
  const [visibleMessages, setVisibleMessages] = useState(0);
  const [elapsed, setElapsed] = useState(18);
  const [dialog, setDialog] = useState<{ open: boolean; plan?: PlanKey; trigger?: HTMLElement | null }>({ open: false });

  const openRequest = (trigger: HTMLElement, plan?: PlanKey) => {
    setMenuOpen(false);
    setDialog({ open: true, plan, trigger });
  };

  useEffect(() => {
    const onScroll = () => setScrolled(window.scrollY > 30);
    window.addEventListener("scroll", onScroll, { passive: true });
    onScroll();
    return () => window.removeEventListener("scroll", onScroll);
  }, []);

  useEffect(() => {
    const elements = rootRef.current?.querySelectorAll<HTMLElement>("[data-reveal]") ?? [];
    if (!("IntersectionObserver" in window)) {
      elements.forEach((element) => element.classList.add(styles.visible));
      return;
    }
    const observer = new IntersectionObserver((entries) => {
      entries.forEach((entry) => {
        if (!entry.isIntersecting) return;
        entry.target.classList.add(styles.visible);
        observer.unobserve(entry.target);
      });
    }, { threshold: 0.13 });
    elements.forEach((element) => observer.observe(element));
    return () => observer.disconnect();
  }, [locale]);

  useEffect(() => {
    setVisibleMessages(0);
    setElapsed(18);
    const messageTimers = content.simulation.lines.map((_, index) =>
      window.setTimeout(() => setVisibleMessages(index + 1), index * 650)
    );
    const clock = window.setInterval(() => setElapsed((value) => value + 1), 1000);
    return () => {
      messageTimers.forEach(window.clearTimeout);
      window.clearInterval(clock);
    };
  }, [content.simulation.lines]);

  const timer = `${String(Math.floor(elapsed / 60)).padStart(2, "0")}:${String(elapsed % 60).padStart(2, "0")}`;

  return (
    <div ref={rootRef} className={c("page-shell")}>
      <a className={c("skip-link")} href="#main">{locale === "vi" ? "Đi đến nội dung" : "Skip to content"}</a>

      <header className={c("site-header", scrolled && "scrolled")} id="top">
        <nav className={c("nav", "container")} aria-label={locale === "vi" ? "Điều hướng chính" : "Primary navigation"}>
          <Brand href={prefix || "/"} />
          <div className={c("nav-links", menuOpen && "open")} id="navLinks">
            <a href="#why" onClick={() => setMenuOpen(false)}>{content.nav.why}</a>
            <a href="#features" onClick={() => setMenuOpen(false)}>{content.nav.features}</a>
            <a href="#workflow" onClick={() => setMenuOpen(false)}>{content.nav.workflow}</a>
            <Link href={`${prefix}/pricing`} onClick={() => setMenuOpen(false)}>{content.nav.pricing}</Link>
            <a href="#faq" onClick={() => setMenuOpen(false)}>{content.nav.faq}</a>
            <div className={c("mobile-nav-language")}>
              <span>{locale === "vi" ? "Ngôn ngữ" : "Language"}</span>
              <LanguageLinks locale={locale} />
            </div>
          </div>
          <div className={c("nav-actions")}>
            <LanguageLinks locale={locale} />
            <button className={c("button", "button-small", "button-primary", "desktop-cta")} type="button" onClick={(event) => openRequest(event.currentTarget)}>{content.nav.request}</button>
            <button className={c("menu-button")} type="button" aria-expanded={menuOpen} aria-controls="navLinks" aria-label={locale === "vi" ? "Mở menu" : "Open menu"} onClick={() => setMenuOpen((value) => !value)}>
              <span /><span /><span />
            </button>
          </div>
        </nav>
      </header>

      <main id="main">
        <section className={c("hero", "section")}>
          <div className={c("hero-orb", "hero-orb-blue")} aria-hidden="true" />
          <div className={c("hero-orb", "hero-orb-red")} aria-hidden="true" />
          <div className={c("container", "hero-grid")}>
            <div className={c("hero-copy", "reveal")} data-reveal>
              <div className={c("eyebrow")}><span className={c("live-dot")} /><span>{content.hero.eyebrow}</span></div>
              <h1>{content.hero.title}<br /><span>{content.hero.titleAccent}</span></h1>
              <p className={c("hero-lead")}>{content.hero.lead}</p>
              <div className={c("hero-actions")}>
                <button className={c("button", "button-primary", "button-large")} type="button" onClick={(event) => openRequest(event.currentTarget)}>{content.hero.primary}<ArrowIcon /></button>
                <a className={c("button", "button-secondary", "button-large")} href="#live-demo"><span className={c("play-icon")} aria-hidden="true">▶</span>{content.hero.secondary}</a>
              </div>
              <div className={c("trust-row")} aria-label={locale === "vi" ? "Điểm nổi bật" : "Product highlights"}>
                {content.hero.trust.map((item) => <span key={item}><CheckIcon /><span>{item}</span></span>)}
              </div>
              <p className={c("hero-safety-note")}>{content.hero.note}</p>
            </div>

            <div className={c("hero-visual", "reveal", "reveal-delay-1")} data-reveal aria-label="Tianna AI product illustration">
              <div className={c("sticker", "sticker-top")}><span>24/7</span><small>{content.hero.alwaysOn}</small></div>
              <div className={c("sticker", "sticker-right")}><span>ENGLISH</span><small>{content.hero.callLanguage}</small></div>
              <div className={c("logo-stage")}>
                <div className={c("spark", "spark-one")} aria-hidden="true">✦</div>
                <div className={c("spark", "spark-two")} aria-hidden="true">✦</div>
                <img className={c("hero-logo")} src="/brand/tianna-ai-logo-720.png" alt="Tianna AI nail salon receptionist" width="620" height="620" />
                <div className={c("floating-call")}>
                  <div className={c("caller-avatar")}>T</div>
                  <div><strong>{content.hero.incomingCall}</strong><span>{content.hero.newCustomer}</span></div>
                  <div className={c("call-equalizer")} aria-hidden="true"><i /><i /><i /><i /></div>
                </div>
              </div>
            </div>
          </div>

          <div className={c("container", "quick-value", "reveal", "reveal-delay-2")} data-reveal>
            {content.quickValues.map((item, index) => <article key={item.title}><span className={c("value-number")}>0{index + 1}</span><div><strong>{item.title}</strong><p>{item.body}</p></div></article>)}
          </div>
        </section>

        <section className={c("section", "problem-section")} id="why">
          <div className={c("container")}>
            <div className={c("section-heading", "centered", "reveal")} data-reveal><span className={c("kicker")}>{content.problems.kicker}</span><h2>{content.problems.title}<br /><span>{content.problems.titleAccent}</span></h2><p>{content.problems.lead}</p></div>
            <div className={c("problem-grid")}>
              {content.problems.items.map((item, index) => <article key={item.title} className={c("problem-card", index === 0 && "tilt-left", index === 1 && "featured", index === 2 && "tilt-right", "reveal", index === 1 && "reveal-delay-1", index === 2 && "reveal-delay-2")} data-reveal>
                <div className={c("card-icon", index === 0 ? "red-icon" : index === 1 ? "blue-icon" : "purple-icon")} aria-hidden="true"><ProblemIcon index={index} /></div>
                <span className={c("card-tag")}>{item.tag}</span><h3>{item.title}</h3><p>{item.body}</p>
              </article>)}
            </div>
          </div>
        </section>

        <section className={c("section", "features-section")} id="features">
          <div className={c("container")}>
            <div className={c("section-heading", "split", "reveal")} data-reveal><div><span className={c("kicker", "light")}>{content.features.kicker}</span><h2>{content.features.title}<br /><span>{content.features.titleAccent}</span></h2></div><p>{content.features.lead}</p></div>
            <div className={c("feature-bento")}>
              <article className={c("feature-card", "feature-card-large", "reveal")} data-reveal><div className={c("feature-copy")}><span className={c("feature-number")}>01</span><h3>{content.features.items[0].title}</h3><p>{content.features.items[0].body}</p><div className={c("language-bubbles")} aria-hidden="true"><span>Hello!</span><span>How can I help?</span><span>Gel manicure?</span><span>Friday afternoon.</span></div></div><div className={c("wave-panel")} aria-hidden="true">{Array.from({ length: 9 }, (_, index) => <span key={index} />)}</div></article>
              {content.features.items.slice(1, 4).map((item, offset) => {
                const index = offset + 1;
                return <article key={item.title} className={c("feature-card", index === 2 && "feature-card-red", index === 3 && "feature-card-dark", "reveal", index === 1 && "reveal-delay-1", index === 2 && "reveal-delay-2")} data-reveal><div className={c("feature-icon")} aria-hidden="true"><FeatureIcon index={index} /></div><span className={c("feature-number")}>0{index + 1}</span><h3>{item.title}</h3><p>{item.body}</p></article>;
              })}
              <article className={c("feature-card", "feature-card-wide", "reveal", "reveal-delay-1")} data-reveal><div><div className={c("feature-icon")} aria-hidden="true"><FeatureIcon index={4} /></div><span className={c("feature-number")}>05</span><h3>{content.features.items[4].title}</h3><p>{content.features.items[4].body}</p></div><div className={c("mini-notifications")} aria-hidden="true">{content.features.notices.map((notice, index) => <div key={notice.title}><span>{index === 0 ? "✓" : "✦"}</span><p><strong>{notice.title}</strong><small>{notice.body}</small></p></div>)}</div></article>
            </div>
          </div>
        </section>

        <section className={c("section", "workflow-section")} id="workflow">
          <div className={c("container", "workflow-grid")}>
            <div className={c("workflow-copy", "reveal")} data-reveal><span className={c("kicker")}>{content.workflow.kicker}</span><h2>{content.workflow.title}<br /><span>{content.workflow.titleAccent}</span></h2><p>{content.workflow.lead}</p><button className={c("button", "button-dark", "button-large")} type="button" onClick={(event) => openRequest(event.currentTarget)}>{content.workflow.cta}<ArrowIcon /></button></div>
            <div className={c("steps-list")}>{content.workflow.steps.map((step, index) => <article key={step.title} className={c("step-card", "reveal", index > 0 && `reveal-delay-${index}`)} data-reveal><span>{index + 1}</span><div><h3>{step.title}</h3><p>{step.body}</p></div></article>)}</div>
          </div>
        </section>

        <section className={c("section", "live-demo-section")} id="live-demo">
          <div className={c("container", "demo-grid")}>
            <div className={c("phone-shell", "reveal")} data-reveal aria-label={locale === "vi" ? "Hội thoại AI mẫu" : "Sample AI receptionist conversation"}>
              <div className={c("phone-topbar")}><span>9:41</span><span className={c("phone-island")} /><span>●●●</span></div>
              <div className={c("call-header")}><div className={c("caller-logo")}><img src="/brand/tianna-ai-logo-720.png" alt="" /></div><span>{content.simulation.salon}</span><strong>{content.simulation.role}</strong><small className={c("call-status")}><i /><span>{content.simulation.live}</span> · <span>{timer}</span></small></div>
              <div className={c("transcript")} aria-live="polite">{content.simulation.lines.slice(0, visibleMessages).map((line, index) => <div key={`${line.label}-${index}`} className={c("message", line.role)}><small>{line.label}</small><span>{line.text}</span></div>)}</div>
              <div className={c("call-wave")} aria-hidden="true">{Array.from({ length: 12 }, (_, index) => <i key={index} />)}</div>
              <div className={c("call-controls")} aria-hidden="true"><span><AudioIcon /></span><span className={c("hangup")}><HangupIcon /></span><span><SpeakerIcon /></span></div>
            </div>
            <div className={c("demo-copy", "reveal", "reveal-delay-1")} data-reveal><span className={c("kicker")}>{content.simulation.kicker}</span><h2>{content.simulation.title}<br /><span>{content.simulation.titleAccent}</span></h2><p>{content.simulation.lead}</p><div className={c("demo-benefits")}>{content.simulation.benefits.map((benefit) => <div key={benefit.title}><span>✦</span><p><strong>{benefit.title}</strong><small>{benefit.body}</small></p></div>)}</div></div>
          </div>
        </section>

        <section className={c("section", "outcomes-section")}>
          <div className={c("container")}><div className={c("section-heading", "centered", "reveal")} data-reveal><span className={c("kicker", "light")}>{content.outcomes.kicker}</span><h2>{content.outcomes.title}<br /><span>{content.outcomes.titleAccent}</span></h2></div><div className={c("outcome-grid")}>{content.outcomes.items.map((item, index) => <article key={item.label} className={c("reveal", index > 0 && `reveal-delay-${index}`)} data-reveal><strong>{item.metric}</strong><span>{item.label}</span></article>)}</div><p className={c("outcomes-note")}>{content.outcomes.note}</p></div>
        </section>

        <section className={c("section", "integrations-section")}>
          <div className={c("container", "integration-card", "reveal")} data-reveal><div><span className={c("kicker")}>{content.integration.kicker}</span><h2>{content.integration.title}<br /><span>{content.integration.titleAccent}</span></h2><p>{content.integration.body}</p><button className={c("button", "button-primary", "button-large")} type="button" onClick={(event) => openRequest(event.currentTarget)}>{content.integration.cta}<ArrowIcon /></button></div><div className={c("integration-map")} aria-label={locale === "vi" ? "Nhóm tích hợp" : "Integration categories"}><div className={c("integration-center")}><img src="/brand/tianna-ai-logo-720.png" alt="Tianna AI" /></div><IntegrationNode className="node-calendar" icon="calendar" label={content.integration.categories.calendar} /><IntegrationNode className="node-pos" icon="pos" label="POS" /><IntegrationNode className="node-sms" icon="message" label={content.integration.categories.messaging} /><IntegrationNode className="node-team" icon="team" label={content.integration.categories.team} /></div></div>
        </section>

        <section className={c("section", "faq-section")} id="faq">
          <div className={c("container", "faq-grid")}><div className={c("faq-heading", "reveal")} data-reveal><span className={c("kicker")}>{content.faq.kicker}</span><h2>{content.faq.title}<br /><span>{content.faq.titleAccent}</span></h2><p>{content.faq.lead}</p></div><div className={c("accordion", "reveal", "reveal-delay-1")} data-reveal>{content.faq.items.map((item, index) => <article key={item.question} className={c("accordion-item", openFaq === index && "open")}><button type="button" aria-expanded={openFaq === index} onClick={() => setOpenFaq((current) => current === index ? -1 : index)}><span>{item.question}</span><i /></button><div className={c("accordion-content")}><p>{item.answer}</p></div></article>)}</div></div>
        </section>

        <section className={c("section", "final-cta-section")}>
          <div className={c("container", "final-cta", "reveal")} data-reveal><div className={c("final-cta-stars")} aria-hidden="true">✦ ✦ ✦</div><img src="/brand/tianna-ai-logo-720.png" alt="Tianna AI" /><span className={c("kicker", "light")}>{content.final.kicker}</span><h2>{content.final.title}<br /><span>{content.final.titleAccent}</span></h2><p>{content.final.body}</p><button className={c("button", "button-white", "button-large")} type="button" onClick={(event) => openRequest(event.currentTarget)}>{content.final.cta}<ArrowIcon /></button></div>
        </section>
      </main>

      <MarketingFooter locale={locale} onRequest={openRequest} />
      <RegistrationDialog open={dialog.open} locale={locale} source="home" plan={dialog.plan} returnFocus={dialog.trigger} onClose={() => setDialog((current) => ({ ...current, open: false }))} />
    </div>
  );
}

function Brand({ href }: { href: string }) {
  return <Link className={c("brand")} href={href} aria-label="Tianna AI home"><img src="/brand/icon-192.png" alt="Tianna AI logo" width="54" height="54" /><span className={c("brand-wordmark")}>Tianna <span>AI</span></span></Link>;
}

export function LanguageLinks({ locale, pricing = false }: { locale: Locale; pricing?: boolean }) {
  return <div className={c("language-switch")} role="group" aria-label="Language switcher"><Link className={c("lang-btn", locale === "en" && "active")} href={pricing ? "/pricing" : "/"} aria-current={locale === "en" ? "page" : undefined}>EN</Link><Link className={c("lang-btn", locale === "vi" && "active")} href={pricing ? "/vi/pricing" : "/vi"} aria-current={locale === "vi" ? "page" : undefined}>VI</Link></div>;
}

export function MarketingFooter({ locale, onRequest }: { locale: Locale; onRequest?: (trigger: HTMLElement) => void }) {
  const content = contentFor(locale);
  const prefix = locale === "vi" ? "/vi" : "";
  return <footer className={c("site-footer")}><div className={c("container", "footer-grid")}><div><Brand href={prefix || "/"} /><p>{content.footer.text}</p></div><div className={c("footer-links")}><a href={`${prefix || "/"}#why`}>{content.nav.why}</a><a href={`${prefix || "/"}#features`}>{content.nav.features}</a><a href={`${prefix || "/"}#workflow`}>{content.nav.workflow}</a><Link href={`${prefix}/pricing`}>{content.nav.pricing}</Link><a href={`${prefix || "/"}#faq`}>{content.nav.faq}</a></div><div className={c("footer-contact")}><strong>{content.footer.ready}</strong>{onRequest ? <button type="button" onClick={(event) => onRequest(event.currentTarget)}>{content.nav.request}</button> : <Link href={`${prefix || "/"}#top`}>{content.nav.request}</Link>}</div></div><div className={c("container", "footer-bottom")}><span>© {new Date().getFullYear()} Tianna AI. {content.footer.rights}</span><div className={c("footer-meta")}><span>{content.footer.note}</span><a className={c("powered-by")} href="https://www.knasoftware.com" target="_blank" rel="noopener">POWERED BY KNASOFTWARE</a></div></div></footer>;
}

function ArrowIcon() { return <svg aria-hidden="true" viewBox="0 0 24 24"><path d="M5 12h14M13 6l6 6-6 6" /></svg>; }
function CheckIcon() { return <svg aria-hidden="true" viewBox="0 0 24 24"><path d="m5 12 4 4L19 6" /></svg>; }
function ProblemIcon({ index }: { index: number }) {
  if (index === 0) return <svg viewBox="0 0 24 24"><path d="M22 16.92v3a2 2 0 0 1-2.18 2 19.79 19.79 0 0 1-8.63-3.07 19.5 19.5 0 0 1-6-6A19.79 19.79 0 0 1 2.12 4.18 2 2 0 0 1 4.11 2h3a2 2 0 0 1 2 1.72c.13 1 .37 1.97.72 2.88a2 2 0 0 1-.45 2.11L8.1 9.99a16 16 0 0 0 6 6l1.28-1.28a2 2 0 0 1 2.11-.45c.91.35 1.88.59 2.88.72A2 2 0 0 1 22 16.92Z" /><path d="m15 2 7 7M22 2l-7 7" /></svg>;
  if (index === 1) return <svg viewBox="0 0 24 24"><path d="M12 22a10 10 0 1 0 0-20 10 10 0 0 0 0 20Z" /><path d="M12 6v6l4 2" /><path d="M5 3 2 6M19 3l3 3" /></svg>;
  return <svg viewBox="0 0 24 24"><path d="M5 8l6 6M4 14l6-6 2-3M2 5h12M7 2h1M22 22l-5-10-5 10M14 18h6" /></svg>;
}

function FeatureIcon({ index }: { index: number }) {
  if (index === 1) return <svg viewBox="0 0 24 24"><rect x="3" y="5" width="18" height="16" rx="2" /><path d="M16 3v4M8 3v4M3 10h18M8 14h.01M12 14h.01M16 14h.01M8 18h.01M12 18h.01" /></svg>;
  if (index === 2) return <svg viewBox="0 0 24 24"><path d="M21 15a4 4 0 0 1-4 4H8l-5 3V7a4 4 0 0 1 4-4h10a4 4 0 0 1 4 4Z" /><path d="M8 9h8M8 13h5" /></svg>;
  if (index === 3) return <svg viewBox="0 0 24 24"><path d="M16 21v-2a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v2" /><circle cx="9" cy="7" r="4" /><path d="M22 11h-6M19 8v6" /></svg>;
  return <svg viewBox="0 0 24 24"><path d="M12 2v20M17 5H9.5a3.5 3.5 0 0 0 0 7H14a3.5 3.5 0 0 1 0 7H6" /></svg>;
}

function AudioIcon() { return <svg viewBox="0 0 24 24"><path d="M12 2v20M5 9v6M19 9v6" /></svg>; }
function HangupIcon() { return <svg viewBox="0 0 24 24"><path d="M4.5 15.5c4.5-4 10.5-4 15 0l-2.5 3-3-2v-2a11 11 0 0 0-4 0v2l-3 2-2.5-3Z" /></svg>; }
function SpeakerIcon() { return <svg viewBox="0 0 24 24"><path d="M11 5 6 9H2v6h4l5 4ZM15.5 8.5a5 5 0 0 1 0 7M18 6a8.5 8.5 0 0 1 0 12" /></svg>; }

function IntegrationNode({ className, icon, label }: { className: string; icon: "calendar" | "pos" | "message" | "team"; label: string }) {
  return <div className={c("integration-node", className)}>{icon === "calendar" ? <svg viewBox="0 0 24 24"><rect x="3" y="5" width="18" height="16" rx="2" /><path d="M16 3v4M8 3v4M3 10h18" /></svg> : icon === "pos" ? <svg viewBox="0 0 24 24"><path d="M4 4h16v6H4zM6 14h12v6H6zM8 7h.01M11 7h.01M14 7h.01" /></svg> : icon === "message" ? <svg viewBox="0 0 24 24"><path d="M21 15a4 4 0 0 1-4 4H8l-5 3V7a4 4 0 0 1 4-4h10a4 4 0 0 1 4 4Z" /></svg> : <svg viewBox="0 0 24 24"><path d="M16 21v-2a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v2M9 11a4 4 0 1 0 0-8 4 4 0 0 0 0 8ZM22 21v-2a4 4 0 0 0-3-3.87M16 3.13a4 4 0 0 1 0 7.75" /></svg>}<span>{label}</span></div>;
}
