import { cn } from "@/lib/utils/cn";

const statusClass: Record<string, string> = {
  active: "bg-emerald-50 text-emerald-700 ring-emerald-200",
  ready: "bg-emerald-50 text-emerald-700 ring-emerald-200",
  connected: "bg-blue-50 text-blue-700 ring-blue-200",
  configured: "bg-blue-50 text-blue-700 ring-blue-200",
  phone: "bg-blue-50 text-blue-700 ring-blue-200",
  simulator: "bg-slate-100 text-slate-700 ring-slate-200",
  syncing: "bg-amber-50 text-amber-700 ring-amber-200",
  error: "bg-red-50 text-red-700 ring-red-200",
  confirmed: "bg-emerald-50 text-emerald-700 ring-emerald-200",
  available: "bg-emerald-50 text-emerald-700 ring-emerald-200",
  allowed: "bg-emerald-50 text-emerald-700 ring-emerald-200",
  anyone: "bg-blue-50 text-blue-700 ring-blue-200",
  specific: "bg-slate-100 text-slate-700 ring-slate-200",
  blocked: "bg-amber-50 text-amber-700 ring-amber-200",
  cancelled: "bg-slate-100 text-slate-700 ring-slate-200",
  rescheduled: "bg-blue-50 text-blue-700 ring-blue-200",
  pos_pending: "bg-blue-50 text-blue-700 ring-blue-200",
  fallback_pending: "bg-amber-50 text-amber-700 ring-amber-200",
  collecting: "bg-blue-50 text-blue-700 ring-blue-200",
  completed: "bg-emerald-50 text-emerald-700 ring-emerald-200",
  handoff: "bg-amber-50 text-amber-700 ring-amber-200",
  booking_confirmed: "bg-emerald-50 text-emerald-700 ring-emerald-200",
  booking_fallback_pending: "bg-amber-50 text-amber-700 ring-amber-200",
  slots_offered: "bg-blue-50 text-blue-700 ring-blue-200",
  slot_selected: "bg-blue-50 text-blue-700 ring-blue-200",
  pending_request: "bg-amber-50 text-amber-700 ring-amber-200",
  not_started: "bg-slate-100 text-slate-700 ring-slate-200",
  handoff_requested: "bg-amber-50 text-amber-700 ring-amber-200",
  ai_disabled: "bg-slate-100 text-slate-700 ring-slate-200",
  booking: "bg-blue-50 text-blue-700 ring-blue-200",
  unknown: "bg-slate-100 text-slate-700 ring-slate-200",
  failed: "bg-red-50 text-red-700 ring-red-200",
  not_connected: "bg-slate-100 text-slate-700 ring-slate-200",
  not_configured: "bg-slate-100 text-slate-700 ring-slate-200",
  disabled: "bg-slate-100 text-slate-700 ring-slate-200",
  expired_token: "bg-red-50 text-red-700 ring-red-200",
  pending: "bg-amber-50 text-amber-700 ring-amber-200",
  applied: "bg-emerald-50 text-emerald-700 ring-emerald-200",
  dismissed: "bg-slate-100 text-slate-700 ring-slate-200",
  draft: "bg-slate-100 text-slate-700 ring-slate-200",
  archived: "bg-slate-100 text-slate-700 ring-slate-200",
  faq: "bg-blue-50 text-blue-700 ring-blue-200",
  policy: "bg-purple-50 text-purple-700 ring-purple-200",
  services: "bg-teal-50 text-teal-700 ring-teal-200",
  hours: "bg-amber-50 text-amber-700 ring-amber-200",
  operations: "bg-slate-100 text-slate-700 ring-slate-200",
  knowledge_answer: "bg-emerald-50 text-emerald-700 ring-emerald-200",
  no_match: "bg-amber-50 text-amber-700 ring-amber-200",
  no_booking_action: "bg-slate-100 text-slate-700 ring-slate-200"
};

export function Badge({ value, className }: { value: string; className?: string }) {
  return (
    <span
      className={cn(
        "inline-flex items-center rounded-full px-2.5 py-1 text-xs font-semibold ring-1",
        statusClass[value] ?? "bg-slate-100 text-slate-700 ring-slate-200",
        className
      )}
    >
      {value.replaceAll("_", " ")}
    </span>
  );
}
