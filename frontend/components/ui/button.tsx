import { cn } from "@/lib/utils/cn";
import type { ButtonHTMLAttributes } from "react";

type ButtonProps = ButtonHTMLAttributes<HTMLButtonElement> & {
  variant?: "primary" | "secondary" | "ghost" | "danger";
};

export function Button({ className, variant = "primary", ...props }: ButtonProps) {
  const styles = {
    primary: "bg-brand text-white hover:bg-teal-800 disabled:bg-teal-900/35",
    secondary: "border border-line bg-white text-ink hover:bg-slate-50 disabled:text-slate-400",
    ghost: "text-ink hover:bg-slate-100 disabled:text-slate-400",
    danger: "bg-accent text-white hover:bg-red-800 disabled:bg-red-900/35"
  };

  return (
    <button
      className={cn(
        "inline-flex h-10 items-center justify-center gap-2 rounded-md px-4 text-sm font-semibold transition disabled:cursor-not-allowed",
        styles[variant],
        className
      )}
      {...props}
    />
  );
}

