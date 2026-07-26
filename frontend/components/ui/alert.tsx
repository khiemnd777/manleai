import { AlertCircle, CheckCircle2 } from "lucide-react";
import type { ReactNode } from "react";

export function Alert({
  type = "error",
  title,
  message,
  children
}: {
  type?: "error" | "success";
  title: string;
  message: string;
  children?: ReactNode;
}) {
  const Icon = type === "success" ? CheckCircle2 : AlertCircle;
  const classes =
    type === "success"
      ? "border-emerald-200 bg-emerald-50 text-emerald-800"
      : "border-red-200 bg-red-50 text-red-800";
  return (
    <div className={`flex gap-3 rounded-md border p-4 text-sm ${classes}`}>
      <Icon className="mt-0.5 h-4 w-4 flex-none" />
      <div>
        <div className="font-semibold">{title}</div>
        <div className="mt-1 leading-6">{message}</div>
        {children}
      </div>
    </div>
  );
}
