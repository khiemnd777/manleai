"use client";

import { useEffect } from "react";
import type { ReactNode } from "react";
import { X } from "lucide-react";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils/cn";

type DialogProps = {
  open: boolean;
  title: string;
  description?: string;
  children: ReactNode;
  footer?: ReactNode;
  onClose: () => void;
  closeDisabled?: boolean;
  className?: string;
};

export function Dialog({
  open,
  title,
  description,
  children,
  footer,
  onClose,
  closeDisabled = false,
  className
}: DialogProps) {
  useEffect(() => {
    if (!open) return;

    function onKeyDown(event: KeyboardEvent) {
      if (event.key === "Escape" && !closeDisabled) {
        onClose();
      }
    }

    document.addEventListener("keydown", onKeyDown);
    return () => document.removeEventListener("keydown", onKeyDown);
  }, [closeDisabled, onClose, open]);

  if (!open) return null;

  return (
    <div className="fixed inset-0 z-50 flex items-start justify-center overflow-y-auto px-4 py-6 sm:items-center">
      <button
        type="button"
        className="fixed inset-0 bg-ink/45"
        aria-label="Close dialog"
        onClick={onClose}
        disabled={closeDisabled}
      />
      <section
        role="dialog"
        aria-modal="true"
        aria-labelledby="calendar-dialog-title"
        aria-describedby={description ? "calendar-dialog-description" : undefined}
        className={cn(
          "relative z-10 w-full max-w-3xl rounded-lg border border-line bg-panel p-5 shadow-soft",
          className
        )}
      >
        <div className="flex items-start justify-between gap-3">
          <div className="min-w-0">
            <h2 id="calendar-dialog-title" className="text-base font-semibold text-ink">
              {title}
            </h2>
            {description ? (
              <p id="calendar-dialog-description" className="mt-1 text-sm leading-6 text-muted">
                {description}
              </p>
            ) : null}
          </div>
          <Button
            type="button"
            variant="ghost"
            className="h-9 px-3"
            onClick={onClose}
            disabled={closeDisabled}
            aria-label="Close dialog"
          >
            <X className="h-4 w-4" />
          </Button>
        </div>
        <div className="mt-5">{children}</div>
        {footer ? <div className="mt-5">{footer}</div> : null}
      </section>
    </div>
  );
}
