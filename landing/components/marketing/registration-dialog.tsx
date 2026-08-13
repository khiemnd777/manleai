"use client";

import { ArrowLeft, ArrowRight, CheckCircle2, Loader2, X } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { publicApiBaseUrl } from "@/lib/config";
import { buildRegistrationSubmission, type RegistrationFormFields, type RegistrationSource } from "@/lib/registration-contract";
import type { Locale, PlanKey } from "@/lib/pricing-catalog";
import styles from "./marketing.module.css";

const c = (...names: Array<string | false | null | undefined>) =>
  names.filter(Boolean).map((name) => styles[name as string]).join(" ");

type Props = {
  open: boolean;
  locale: Locale;
  source: RegistrationSource;
  plan?: PlanKey;
  returnFocus?: HTMLElement | null;
  onClose: () => void;
};

const initialForm: RegistrationFormFields = {
  contact_full_name: "", contact_email: "", contact_phone: "", salon_name: "", salon_phone: "",
  city: "", state: "", zip_code: "", salon_website: "", location_count: 1,
  preferred_contact_language: "en", current_booking_system: "", estimated_weekly_call_volume: "",
  requested_help: "", notes: "", contact_consent: false, website_confirmation: ""
};

const states = ["AL","AK","AZ","AR","CA","CO","CT","DE","FL","GA","HI","ID","IL","IN","IA","KS","KY","LA","ME","MD","MA","MI","MN","MS","MO","MT","NE","NV","NH","NJ","NM","NY","NC","ND","OH","OK","OR","PA","RI","SC","SD","TN","TX","UT","VT","VA","WA","WV","WI","WY","DC"];

export function RegistrationDialog({ open, locale, source, plan, returnFocus, onClose }: Props) {
  const panelRef = useRef<HTMLDivElement>(null);
  const [step, setStep] = useState<1 | 2>(1);
  const [form, setForm] = useState<RegistrationFormFields>(initialForm);
  const [submissionKey, setSubmissionKey] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState("");
  const [reference, setReference] = useState("");
  const copy = locale === "vi" ? viCopy : enCopy;

  useEffect(() => {
    if (!open) return;
    if (!submissionKey) setSubmissionKey(crypto.randomUUID());
    setError("");
    const previousOverflow = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    requestAnimationFrame(() => panelRef.current?.querySelector<HTMLElement>("input:not([type='hidden']),button")?.focus());
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape" && !submitting) close();
      if (event.key !== "Tab" || !panelRef.current) return;
      const focusable = Array.from(panelRef.current.querySelectorAll<HTMLElement>("button:not([disabled]),input:not([disabled]),select:not([disabled]),textarea:not([disabled]),a[href]"));
      if (!focusable.length) return;
      const first = focusable[0]; const last = focusable[focusable.length - 1];
      if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last.focus(); }
      if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first.focus(); }
    };
    document.addEventListener("keydown", onKeyDown);
    return () => { document.body.style.overflow = previousOverflow; document.removeEventListener("keydown", onKeyDown); };
  }, [open, submitting, submissionKey]);

  if (!open) return null;

  function close() {
    if (submitting) return;
    onClose();
    requestAnimationFrame(() => returnFocus?.focus());
  }

  function update<K extends keyof RegistrationFormFields>(key: K, value: RegistrationFormFields[K]) {
    setForm((current) => ({ ...current, [key]: value }));
    setError("");
  }

  function nextStep() {
    const required = Array.from(panelRef.current?.querySelectorAll<HTMLInputElement | HTMLSelectElement>("[data-step='1'][required]") ?? []);
    for (const field of required) {
      if (!field.reportValidity()) { setError(copy.validationError); return; }
    }
    setError("");
    setStep(2); requestAnimationFrame(() => panelRef.current?.querySelector<HTMLElement>("[data-step='2']")?.focus());
  }

  async function submit(event: React.FormEvent) {
    event.preventDefault(); if (submitting || !submissionKey) return;
    if (!(event.currentTarget as HTMLFormElement).reportValidity()) { setError(copy.validationError); return; }
    setSubmitting(true); setError("");
    const payload = buildRegistrationSubmission(form, { submissionKey, locale, source, plan });
    try {
      const response = await fetch(`${publicApiBaseUrl}/api/public/tenant-registration-requests`, {
        method: "POST", headers: { Accept: "application/json", "Content-Type": "application/json" },
        credentials: "omit", cache: "no-store", body: JSON.stringify(payload)
      });
      if (!response.ok) {
        if (response.status === 409) throw new Error(copy.conflict);
        if (response.status === 429) {
          const retryAfter = response.headers.get("Retry-After");
          throw new Error(retryAfter ? `${copy.rateLimit} ${copy.retryAfter} ${retryAfter}s.` : copy.rateLimit);
        }
        const body = await response.json().catch(() => null) as { error?: { message?: string } } | null;
        throw new Error(body?.error?.message || copy.genericError);
      }
      const body = await response.json() as { request_reference: string };
      setReference(body.request_reference); setSubmissionKey(crypto.randomUUID());
    } catch (failure) { setError(failure instanceof Error ? failure.message : copy.genericError); }
    finally { setSubmitting(false); }
  }

  function resetAndClose() { setForm(initialForm); setStep(1); setReference(""); setError(""); close(); }

  return (
    <div className={c("modal")} aria-hidden={false}>
      <button className={c("modal-backdrop")} type="button" aria-label={copy.close} onClick={close} />
      <div ref={panelRef} className={c("modal-panel")} role="dialog" aria-modal="true" aria-labelledby="registration-title">
        <button type="button" className={c("modal-close")} aria-label={copy.close} onClick={close}><X size={20} /></button>
        {reference ? (
          <div className={c("success-state")} role="status" aria-live="polite">
            <CheckCircle2 size={44} /><span className={c("kicker")}>{copy.received}</span>
            <h2 id="registration-title">{copy.successTitle}</h2><p>{copy.successBody}</p>
            <div className={c("reference")}><span>{copy.reference}</span><strong>{reference}</strong></div>
            <p className={c("form-helper")}>{copy.noDeliveryClaim}</p>
            <button type="button" className={c("button", "button-dark")} onClick={resetAndClose}>{copy.done}</button>
          </div>
        ) : (
          <form onSubmit={submit} noValidate>
            <div className={c("modal-brand")}><img src="/brand/tianna-ai-logo-720.png" alt="" /><span>Tianna AI</span></div>
            <span className={c("kicker")}>{copy.kicker}</span><h2 id="registration-title">{copy.title}</h2><p>{copy.lead}</p>
            {error ? <div className={c("form-error")} role="alert">{error}</div> : null}
            <div className={c("stepper")} aria-label={copy.progress}><span className={c(step === 1 && "step-active")}>1 · {copy.contact}</span><span className={c(step === 2 && "step-active")}>2 · {copy.operations}</span></div>
            <div className={c(step === 1 ? "form-step" : "hidden")} aria-hidden={step !== 1}>
              <Field label={copy.fullName}><input data-step="1" required autoComplete="name" value={form.contact_full_name} onChange={(e)=>update("contact_full_name",e.target.value)} /></Field>
              <div className={c("form-grid")}><Field label={copy.email}><input data-step="1" required type="email" autoComplete="email" value={form.contact_email} onChange={(e)=>update("contact_email",e.target.value)} /></Field><Field label={copy.contactPhone}><input data-step="1" required type="tel" autoComplete="tel" value={form.contact_phone} onChange={(e)=>update("contact_phone",e.target.value)} /></Field></div>
              <div className={c("form-grid")}><Field label={copy.salonName}><input data-step="1" required autoComplete="organization" value={form.salon_name} onChange={(e)=>update("salon_name",e.target.value)} /></Field><Field label={copy.salonPhone}><input data-step="1" required type="tel" value={form.salon_phone} onChange={(e)=>update("salon_phone",e.target.value)} /></Field></div>
              <Field label={copy.website} optional={copy.optional}><input data-step="1" type="url" placeholder="https://" value={form.salon_website} onChange={(e)=>update("salon_website",e.target.value)} /></Field>
              <div className={c("location-grid")}><Field label={copy.city}><input data-step="1" required autoComplete="address-level2" value={form.city} onChange={(e)=>update("city",e.target.value)} /></Field><Field label={copy.state}><select data-step="1" required autoComplete="address-level1" value={form.state} onChange={(e)=>update("state",e.target.value)}><option value="">—</option>{states.map((state)=><option key={state}>{state}</option>)}</select></Field><Field label={copy.zip}><input data-step="1" required inputMode="numeric" autoComplete="postal-code" pattern="[0-9]{5}(-[0-9]{4})?" value={form.zip_code} onChange={(e)=>update("zip_code",e.target.value)} /></Field></div>
              <button type="button" className={c("button", "button-primary", "full-button")} onClick={nextStep}>{copy.continue}<ArrowRight size={18}/></button>
            </div>
            <div className={c(step === 2 ? "form-step" : "hidden")} aria-hidden={step !== 2}>
              <div className={c("form-grid")}><Field label={copy.locations}><input data-step="2" required type="number" min={1} max={100} value={form.location_count} onChange={(e)=>update("location_count",Number(e.target.value))} /></Field><Field label={copy.language}><select data-step="2" required value={form.preferred_contact_language} onChange={(e)=>update("preferred_contact_language",e.target.value as Locale)}><option value="en">English</option><option value="vi">Tiếng Việt</option></select></Field></div>
              <div className={c("form-grid")}><Field label={copy.booking} optional={copy.optional}><input value={form.current_booking_system} onChange={(e)=>update("current_booking_system",e.target.value)} /></Field><Field label={copy.volume} optional={copy.optional}><input value={form.estimated_weekly_call_volume} onChange={(e)=>update("estimated_weekly_call_volume",e.target.value)} /></Field></div>
              <Field label={copy.help} optional={copy.optional} helper={copy.safeText}><textarea rows={3} maxLength={4000} value={form.requested_help} onChange={(e)=>update("requested_help",e.target.value)} /></Field>
              <Field label={copy.notes} optional={copy.optional} helper={copy.safeText}><textarea rows={3} maxLength={4000} value={form.notes} onChange={(e)=>update("notes",e.target.value)} /></Field>
              <label className={c("consent")}><input required type="checkbox" checked={form.contact_consent} onChange={(e)=>update("contact_consent",e.target.checked)} /><span>{copy.consent}</span></label>
              <label className={c("honeypot")} aria-hidden="true">Website confirmation<input tabIndex={-1} autoComplete="off" value={form.website_confirmation} onChange={(e)=>update("website_confirmation",e.target.value)} /></label>
              <div className={c("form-actions")}><button type="button" className={c("button", "button-secondary")} onClick={()=>setStep(1)} disabled={submitting}><ArrowLeft size={18}/>{copy.back}</button><button type="submit" className={c("button", "button-primary")} disabled={submitting}>{submitting?<Loader2 className={c("spin")} size={18}/>:null}{submitting?copy.sending:copy.submit}</button></div>
            </div>
          </form>
        )}
      </div>
    </div>
  );
}

function Field({label,optional,helper,children}:{label:string;optional?:string;helper?:string;children:React.ReactNode}) { return <label className={c("field")}><span>{label}{optional?<small> · {optional}</small>:null}</span>{children}{helper?<small>{helper}</small>:null}</label>; }

const enCopy = { close:"Close registration form",kicker:"Personalized demo & setup review",title:"Tell us how your salon handles the phone.",lead:"We’ll review your request before any Tenant is provisioned.",progress:"Registration progress",contact:"Salon contact",operations:"Operations",fullName:"Your full name",email:"Contact email",contactPhone:"Contact phone",salonName:"Salon name",salonPhone:"Salon phone",website:"Salon website",city:"City",state:"State",zip:"ZIP code",continue:"Continue",locations:"Number of locations",language:"Preferred contact language",booking:"Current booking system",volume:"Estimated weekly call volume",help:"What should Tianna AI help with?",notes:"Additional notes",optional:"optional",safeText:"Do not include customer information or provider credentials.",consent:"By submitting, you agree that Tianna AI may contact you by phone or email about your demo and tenant setup request. Do not include customer information or provider credentials.",back:"Back",submit:"Submit request",sending:"Submitting…",validationError:"Please complete the required fields before continuing.",genericError:"We could not save the request. Your entries are preserved; please try again.",conflict:"This retry no longer matches the original submission. Reset the form before trying again.",rateLimit:"Too many requests.",retryAfter:"Try again in",received:"Request received",successTitle:"Your request is safely in the review queue.",successBody:"Platform Operations will review the salon information before setup begins.",reference:"Request reference",noDeliveryClaim:"This page confirms durable receipt only; it does not claim that an email or SMS was sent.",done:"Done"};
const viCopy = { close:"Đóng form đăng ký",kicker:"Demo cá nhân hóa & review thiết lập",title:"Cho chúng tôi biết cách salon đang nghe máy.",lead:"Yêu cầu sẽ được review trước khi bất kỳ Tenant nào được provision.",progress:"Tiến trình đăng ký",contact:"Thông tin salon",operations:"Vận hành",fullName:"Họ tên",email:"Email liên hệ",contactPhone:"Số điện thoại liên hệ",salonName:"Tên salon",salonPhone:"Số điện thoại salon",website:"Website salon",city:"Thành phố",state:"Tiểu bang",zip:"Mã ZIP",continue:"Tiếp tục",locations:"Số địa điểm",language:"Ngôn ngữ liên hệ",booking:"Hệ thống booking hiện tại",volume:"Số cuộc gọi ước tính mỗi tuần",help:"Tianna AI nên hỗ trợ việc gì?",notes:"Ghi chú thêm",optional:"không bắt buộc",safeText:"Không nhập thông tin khách hàng hoặc thông tin đăng nhập nhà cung cấp.",consent:"Bằng việc gửi yêu cầu, bạn đồng ý để Tianna AI liên hệ qua điện thoại hoặc email về yêu cầu demo và thiết lập tiệm. Không gửi thông tin khách hàng hoặc thông tin đăng nhập nhà cung cấp qua biểu mẫu này.",back:"Quay lại",submit:"Gửi yêu cầu",sending:"Đang gửi…",validationError:"Vui lòng hoàn tất các trường bắt buộc trước khi tiếp tục.",genericError:"Chưa thể lưu yêu cầu. Dữ liệu đã nhập vẫn được giữ; vui lòng thử lại.",conflict:"Lần thử lại không còn khớp với submission ban đầu. Hãy reset form trước khi gửi lại.",rateLimit:"Đã gửi quá nhiều yêu cầu.",retryAfter:"Thử lại sau",received:"Đã nhận yêu cầu",successTitle:"Yêu cầu đã được lưu an toàn vào hàng chờ review.",successBody:"Platform Operations sẽ review thông tin salon trước khi bắt đầu thiết lập.",reference:"Mã yêu cầu",noDeliveryClaim:"Trang này chỉ xác nhận đã lưu yêu cầu; không khẳng định email hoặc SMS đã được gửi.",done:"Hoàn tất"};
