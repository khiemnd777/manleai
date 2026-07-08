import { cn } from "@/lib/utils/cn";

const statusClass: Record<string, string> = {
  active: "bg-emerald-50 text-emerald-700 ring-emerald-200",
  connected: "bg-blue-50 text-blue-700 ring-blue-200",
  ready: "bg-emerald-50 text-emerald-700 ring-emerald-200",
  syncing: "bg-amber-50 text-amber-700 ring-amber-200",
  sync_failed: "bg-red-50 text-red-700 ring-red-200",
  failed: "bg-red-50 text-red-700 ring-red-200",
  synced: "bg-emerald-50 text-emerald-700 ring-emerald-200",
  imported: "bg-teal-50 text-teal-700 ring-teal-200",
  confirmed: "bg-emerald-50 text-emerald-700 ring-emerald-200",
  rescheduled: "bg-blue-50 text-blue-700 ring-blue-200",
  cancelled: "bg-slate-100 text-slate-700 ring-slate-200",
  fallback_pending: "bg-amber-50 text-amber-700 ring-amber-200",
  pending: "bg-amber-50 text-amber-700 ring-amber-200",
  not_synced: "bg-amber-50 text-amber-700 ring-amber-200",
  available: "bg-emerald-50 text-emerald-700 ring-emerald-200",
  selected: "bg-emerald-50 text-emerald-700 ring-emerald-200",
  anyone: "bg-blue-50 text-blue-700 ring-blue-200",
  specific: "bg-slate-100 text-slate-700 ring-slate-200",
  blocked: "bg-amber-50 text-amber-700 ring-amber-200",
  warning: "bg-amber-50 text-amber-700 ring-amber-200"
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
