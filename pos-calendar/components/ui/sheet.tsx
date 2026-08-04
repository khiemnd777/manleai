"use client";

import { useEffect, useId, useRef } from "react";
import type { ReactNode } from "react";
import { X } from "lucide-react";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils/cn";

type SheetProps = {
  open: boolean;
  title: string;
  description?: string;
  children: ReactNode;
  footer?: ReactNode;
  onClose: () => void;
  closeDisabled?: boolean;
  className?: string;
};

const focusableSelector = [
  "button:not([disabled])",
  "a[href]",
  "input:not([disabled])",
  "select:not([disabled])",
  "textarea:not([disabled])",
  "[tabindex]:not([tabindex='-1'])"
].join(",");

export function Sheet({
  open,
  title,
  description,
  children,
  footer,
  onClose,
  closeDisabled = false,
  className
}: SheetProps) {
  const titleID = useId();
  const descriptionID = useId();
  const panelRef = useRef<HTMLElement | null>(null);
  const onCloseRef = useRef(onClose);
  onCloseRef.current = onClose;

  useEffect(() => {
    if (!open) return;
    const previousFocus = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    const previousOverflow = document.body.style.overflow;
    document.body.style.overflow = "hidden";

    const focusTimer = window.setTimeout(() => {
      const firstFocusable = panelRef.current?.querySelector<HTMLElement>(focusableSelector);
      (firstFocusable ?? panelRef.current)?.focus();
    }, 0);

    function onKeyDown(event: KeyboardEvent) {
      if (event.key === "Escape" && !closeDisabled) {
        event.preventDefault();
        onCloseRef.current();
        return;
      }
      if (event.key !== "Tab" || !panelRef.current) return;
      const focusable = Array.from(panelRef.current.querySelectorAll<HTMLElement>(focusableSelector));
      if (focusable.length === 0) {
        event.preventDefault();
        panelRef.current.focus();
        return;
      }
      const first = focusable[0];
      const last = focusable[focusable.length - 1];
      if (event.shiftKey && document.activeElement === first) {
        event.preventDefault();
        last.focus();
      } else if (!event.shiftKey && document.activeElement === last) {
        event.preventDefault();
        first.focus();
      }
    }

    document.addEventListener("keydown", onKeyDown);
    return () => {
      window.clearTimeout(focusTimer);
      document.removeEventListener("keydown", onKeyDown);
      document.body.style.overflow = previousOverflow;
      previousFocus?.focus();
    };
  }, [closeDisabled, open]);

  if (!open) return null;

  return (
    <div className="fixed inset-0 z-[80] flex items-end justify-center">
      <button
        type="button"
        className="absolute inset-0 bg-ink/45"
        aria-label="Close sheet"
        onClick={onClose}
        disabled={closeDisabled}
      />
      <section
        ref={panelRef}
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleID}
        aria-describedby={description ? descriptionID : undefined}
        tabIndex={-1}
        className={cn(
          "pos-calendar-safe-bottom relative z-10 max-h-[88dvh] w-full max-w-lg overflow-hidden rounded-t-lg border border-b-0 border-line bg-panel shadow-2xl",
          className
        )}
      >
        <div className="mx-auto mt-2 h-1 w-10 rounded-full bg-slate-300" aria-hidden="true" />
        <div className="flex items-start justify-between gap-3 border-b border-line px-4 py-3">
          <div className="min-w-0">
            <h2 id={titleID} className="text-base font-semibold text-ink">
              {title}
            </h2>
            {description ? (
              <p id={descriptionID} className="mt-1 text-sm leading-5 text-muted">
                {description}
              </p>
            ) : null}
          </div>
          <Button
            type="button"
            variant="ghost"
            className="h-11 w-11 flex-none px-0"
            onClick={onClose}
            disabled={closeDisabled}
            aria-label="Close sheet"
          >
            <X className="h-4 w-4" />
          </Button>
        </div>
        <div className="max-h-[calc(88dvh-8rem)] overflow-y-auto px-4 py-3">{children}</div>
        {footer ? <div className="border-t border-line px-4 py-3">{footer}</div> : null}
      </section>
    </div>
  );
}
