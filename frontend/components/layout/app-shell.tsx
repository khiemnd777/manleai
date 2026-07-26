"use client";

import {
  CalendarDays,
  GraduationCap,
  Home,
  LogOut,
  Menu,
  PhoneCall,
  Settings,
  Sparkles,
  Users,
  WalletCards,
  X
} from "lucide-react";
import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { useEffect, useState } from "react";
import { logoutSession } from "@/lib/api/client";
import { cn } from "@/lib/utils/cn";
import { Button } from "@/components/ui/button";
import { useTenantSalon } from "@/components/layout/tenant-salon-context";

const navItems = [
  { label: "Dashboard", href: "/dashboard", icon: Home },
  { label: "Appointments", href: "/dashboard/appointments", icon: CalendarDays },
  { label: "Calls", href: "/dashboard/calls", icon: PhoneCall },
  { label: "Customers", href: "/dashboard/customers", icon: Users },
  { label: "Services", href: "/dashboard/services", icon: Sparkles },
  { label: "Staff", href: "/dashboard/staff", icon: Users },
  { label: "AI Training", href: "/dashboard/training", icon: GraduationCap },
  { label: "Business settings", href: "/dashboard/settings", icon: Settings },
  { label: "Billing", href: "/dashboard/billing", icon: WalletCards }
];

function NavigationLinks({
  pathname,
  onNavigate
}: {
  pathname: string;
  onNavigate?: () => void;
}) {
  return (
    <>
      {navItems.map((item) => {
        const Icon = item.icon;
        const active = pathname === item.href;
        return (
          <Link
            key={item.href}
            href={item.href}
            onClick={onNavigate}
            className={cn(
              "flex h-10 items-center gap-3 rounded-md px-3 text-sm font-medium text-slate-700 transition hover:bg-slate-100",
              active && "bg-teal-50 text-brand"
            )}
          >
            <Icon className="h-4 w-4" />
            {item.label}
          </Link>
        );
      })}
    </>
  );
}

export function AppShell({ children }: { children: React.ReactNode }) {
  const pathname = usePathname();
  const router = useRouter();
  const tenant = useTenantSalon();
  const [mobileMenuOpen, setMobileMenuOpen] = useState(false);

  function logout() {
    void logoutSession().finally(() => router.push("/login"));
  }

  useEffect(() => {
    setMobileMenuOpen(false);
  }, [pathname]);

  useEffect(() => {
    if (!mobileMenuOpen) {
      return;
    }

    const previousOverflow = document.body.style.overflow;

    document.getElementById("mobile-navigation-close")?.focus();

    function closeOnEscape(event: KeyboardEvent) {
      if (event.key === "Escape") {
        setMobileMenuOpen(false);
      }
    }

    document.body.style.overflow = "hidden";
    document.addEventListener("keydown", closeOnEscape);

    return () => {
      document.body.style.overflow = previousOverflow;
      document.removeEventListener("keydown", closeOnEscape);
    };
  }, [mobileMenuOpen]);

  return (
    <div className="min-h-screen bg-shell">
      <aside className="fixed inset-y-0 left-0 hidden w-72 border-r border-line bg-white px-4 py-5 lg:block">
        <div className="flex h-full flex-col">
          <Link href="/dashboard" className="flex items-center gap-3 px-2">
            <div className="flex h-10 w-10 items-center justify-center rounded-md bg-brand text-sm font-bold text-white">
              AI
            </div>
            <div>
              <div className="text-sm font-bold text-ink">Salon Receptionist</div>
              <div className="text-xs text-muted">Tenant Business workspace</div>
            </div>
          </Link>

          <nav className="mt-8 space-y-1">
            <NavigationLinks pathname={pathname} />
          </nav>

          <div className="mt-auto border-t border-line pt-4">
            <Button type="button" variant="ghost" className="w-full justify-start" onClick={logout}>
              <LogOut className="h-4 w-4" />
              Sign out
            </Button>
          </div>
        </div>
      </aside>

      {mobileMenuOpen && (
        <div
          className="fixed inset-0 z-40 lg:hidden"
          role="dialog"
          aria-modal="true"
          aria-label="Dashboard navigation"
        >
          <button
            type="button"
            className="absolute inset-0 bg-ink/45"
            aria-label="Close navigation menu"
            onClick={() => setMobileMenuOpen(false)}
          />
          <div
            id="mobile-dashboard-navigation"
            className="relative z-10 flex h-full w-80 max-w-[85vw] flex-col border-r border-line bg-white px-4 py-5 shadow-soft"
          >
            <div className="flex items-center justify-between gap-3">
              <Link
                href="/dashboard"
                className="flex min-w-0 items-center gap-3 px-2"
                onClick={() => setMobileMenuOpen(false)}
              >
                <div className="flex h-10 w-10 flex-none items-center justify-center rounded-md bg-brand text-sm font-bold text-white">
                  AI
                </div>
                <div className="min-w-0">
                  <div className="truncate text-sm font-bold text-ink">Salon Receptionist</div>
                  <div className="truncate text-xs text-muted">Tenant Business workspace</div>
                </div>
              </Link>
              <Button
                id="mobile-navigation-close"
                type="button"
                variant="ghost"
                className="h-10 w-10 flex-none px-0"
                aria-label="Close navigation menu"
                onClick={() => setMobileMenuOpen(false)}
              >
                <X className="h-4 w-4" />
              </Button>
            </div>

            <nav className="mt-8 space-y-1">
              <NavigationLinks pathname={pathname} onNavigate={() => setMobileMenuOpen(false)} />
            </nav>

            <div className="mt-auto border-t border-line pt-4">
              <Button
                type="button"
                variant="ghost"
                className="w-full justify-start"
                onClick={() => {
                  setMobileMenuOpen(false);
                  logout();
                }}
              >
                <LogOut className="h-4 w-4" />
                Sign out
              </Button>
            </div>
          </div>
        </div>
      )}

      <div className="lg:pl-72">
        <header className="sticky top-0 z-10 border-b border-line bg-white/95 px-5 py-4 backdrop-blur">
          <div className="mx-auto flex max-w-7xl flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
            <div className="flex min-w-0 items-center gap-3">
              <Button
                type="button"
                variant="ghost"
                className="h-10 w-10 flex-none px-0 lg:hidden"
                aria-label="Open navigation menu"
                aria-controls="mobile-dashboard-navigation"
                aria-expanded={mobileMenuOpen}
                onClick={() => setMobileMenuOpen(true)}
              >
                <Menu className="h-4 w-4" />
              </Button>
              <div className="min-w-0">
                <div className="truncate text-sm font-semibold text-ink">{tenant.activeSalon?.name || "Tenant Business workspace"}</div>
                <div className="truncate text-xs text-muted">Business operations only</div>
              </div>
            </div>
            {tenant.salons.length > 1 ? <label className="w-full sm:w-auto"><span className="sr-only">Active salon</span><select className="field min-w-56" value={tenant.activeSalonID} onChange={(event) => tenant.setActiveSalonID(event.target.value)}>{tenant.salons.map((salon) => <option key={salon.id} value={salon.id}>{salon.name}</option>)}</select></label> : null}
          </div>
        </header>
        <main className="mx-auto max-w-7xl px-5 py-6">{children}</main>
      </div>
    </div>
  );
}
