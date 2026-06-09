import { cn } from "@/lib/utils/cn";

const statusClass: Record<string, string> = {
  active: "bg-emerald-50 text-emerald-700 ring-emerald-200",
  connected: "bg-blue-50 text-blue-700 ring-blue-200",
  syncing: "bg-amber-50 text-amber-700 ring-amber-200",
  error: "bg-red-50 text-red-700 ring-red-200",
  not_connected: "bg-slate-100 text-slate-700 ring-slate-200",
  disabled: "bg-slate-100 text-slate-700 ring-slate-200",
  expired_token: "bg-red-50 text-red-700 ring-red-200"
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

