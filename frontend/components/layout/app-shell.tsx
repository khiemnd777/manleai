"use client";

import {
  CalendarDays,
  GraduationCap,
  Home,
  Link2,
  LogOut,
  PhoneCall,
  Settings,
  Sparkles,
  Users,
  WalletCards
} from "lucide-react";
import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { logoutSession } from "@/lib/api/client";
import { cn } from "@/lib/utils/cn";
import { Button } from "@/components/ui/button";

const navItems = [
  { label: "Dashboard", href: "/dashboard", icon: Home },
  { label: "Appointments", href: "/dashboard/appointments", icon: CalendarDays },
  { label: "Calls", href: "/dashboard/calls", icon: PhoneCall },
  { label: "Customers", href: "/dashboard/customers", icon: Users },
  { label: "Services", href: "/dashboard/services", icon: Sparkles },
  { label: "Staff", href: "/dashboard/staff", icon: Users },
  { label: "AI Training", href: "/dashboard/training", icon: GraduationCap },
  { label: "Integrations", href: "/dashboard/integrations", icon: Link2 },
  { label: "Settings", href: "/dashboard/settings", icon: Settings },
  { label: "Billing", href: "/dashboard/billing", icon: WalletCards }
];

export function AppShell({ children }: { children: React.ReactNode }) {
  const pathname = usePathname();
  const router = useRouter();

  function logout() {
    void logoutSession().finally(() => router.push("/login"));
  }

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
              <div className="text-xs text-muted">POS-first pilot</div>
            </div>
          </Link>

          <nav className="mt-8 space-y-1">
            {navItems.map((item) => {
              const Icon = item.icon;
              const active = pathname === item.href;
              return (
                <Link
                  key={item.href}
                  href={item.href}
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
          </nav>

          <div className="mt-auto border-t border-line pt-4">
            <Button type="button" variant="ghost" className="w-full justify-start" onClick={logout}>
              <LogOut className="h-4 w-4" />
              Sign out
            </Button>
          </div>
        </div>
      </aside>

      <div className="lg:pl-72">
        <header className="sticky top-0 z-10 border-b border-line bg-white/95 px-5 py-4 backdrop-blur">
          <div className="mx-auto flex max-w-7xl items-center justify-between">
            <div>
              <div className="text-sm font-semibold text-ink">AI Phone Receptionist</div>
              <div className="text-xs text-muted">Nail salon owner dashboard</div>
            </div>
            <Link href="/dashboard/integrations">
              <Button type="button" variant="secondary">
                <Link2 className="h-4 w-4" />
                Square setup
              </Button>
            </Link>
          </div>
        </header>
        <main className="mx-auto max-w-7xl px-5 py-6">{children}</main>
      </div>
    </div>
  );
}
