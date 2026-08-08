"use client";

import { Building2, ClipboardList, KeyRound, LogOut, Menu, ShieldCheck, X } from "lucide-react";
import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { useEffect, useState } from "react";
import { Button } from "@/components/ui/button";
import { PoweredBy } from "@/components/layout/powered-by";
import { logoutSession } from "@/lib/api/client";
import { getCurrentSession } from "@/lib/api/session";
import { cn } from "@/lib/utils/cn";

const platformNav = [
  { label: "Registration requests", href: "/platform/registration-requests", icon: ClipboardList },
  { label: "Nail salons", href: "/platform/tenants", icon: Building2 },
  { label: "Platform roles", href: "/platform/access", icon: KeyRound, adminOnly: true }
];

function PlatformNavigation({ pathname, canManageAccess, onNavigate }: { pathname: string; canManageAccess: boolean; onNavigate?: () => void }) {
  return <>{platformNav.filter((item) => !item.adminOnly || canManageAccess).map((item) => {
    const Icon = item.icon;
    const active = pathname === item.href || pathname.startsWith(`${item.href}/`);
    return (
      <Link key={item.href} href={item.href} onClick={onNavigate} className={cn(
        "flex h-10 items-center gap-3 rounded-md px-3 text-sm font-medium text-slate-700 transition hover:bg-slate-100",
        active && "bg-teal-50 text-brand"
      )}>
        <Icon className="h-4 w-4" />{item.label}
      </Link>
    );
  })}</>;
}

export function PlatformAppShell({ children }: { children: React.ReactNode }) {
  const pathname = usePathname();
  const router = useRouter();
  const [mobileOpen, setMobileOpen] = useState(false);
  const [canManageAccess, setCanManageAccess] = useState(false);

  useEffect(() => setMobileOpen(false), [pathname]);
  useEffect(() => {
    getCurrentSession()
      .then((session) => setCanManageAccess(session.roles.includes("platform_admin")))
      .catch(() => setCanManageAccess(false));
  }, []);
  useEffect(() => {
    if (!mobileOpen) return;
    const previous = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    const close = (event: KeyboardEvent) => event.key === "Escape" && setMobileOpen(false);
    document.addEventListener("keydown", close);
    return () => {
      document.body.style.overflow = previous;
      document.removeEventListener("keydown", close);
    };
  }, [mobileOpen]);

  function logout() {
    void logoutSession().finally(() => router.push("/login"));
  }

  const brand = (
    <Link href="/platform/tenants" className="flex min-w-0 items-center gap-3 px-2">
      <div className="flex h-10 w-10 flex-none items-center justify-center rounded-md bg-ink text-white"><ShieldCheck className="h-5 w-5" /></div>
      <div className="min-w-0"><div className="truncate text-sm font-bold text-ink">ManleAI Platform</div><div className="truncate text-xs text-muted">Admin &amp; Operations</div></div>
    </Link>
  );

  return (
    <div className="min-h-screen bg-shell">
      <aside className="fixed inset-y-0 left-0 hidden w-72 border-r border-line bg-white px-4 py-5 lg:block">
        <div className="flex h-full flex-col">{brand}<nav className="mt-8 space-y-1"><PlatformNavigation pathname={pathname} canManageAccess={canManageAccess} /></nav><div className="mt-auto border-t border-line pt-4"><Button type="button" variant="ghost" className="w-full justify-start" onClick={logout}><LogOut className="h-4 w-4" />Sign out</Button></div></div>
      </aside>
      {mobileOpen ? (
        <div className="fixed inset-0 z-40 lg:hidden" role="dialog" aria-modal="true" aria-label="Platform navigation">
          <button type="button" className="absolute inset-0 bg-ink/45" aria-label="Close navigation" onClick={() => setMobileOpen(false)} />
          <div className="relative z-10 flex h-full w-80 max-w-[85vw] flex-col border-r border-line bg-white px-4 py-5 shadow-soft">
            <div className="flex items-center justify-between gap-3">{brand}<Button type="button" variant="ghost" className="h-10 w-10 px-0" aria-label="Close navigation" onClick={() => setMobileOpen(false)}><X className="h-4 w-4" /></Button></div>
            <nav className="mt-8 space-y-1"><PlatformNavigation pathname={pathname} canManageAccess={canManageAccess} onNavigate={() => setMobileOpen(false)} /></nav>
            <div className="mt-auto border-t border-line pt-4"><Button type="button" variant="ghost" className="w-full justify-start" onClick={logout}><LogOut className="h-4 w-4" />Sign out</Button></div>
          </div>
        </div>
      ) : null}
      <div className="flex min-h-screen flex-col lg:pl-72">
        <header className="sticky top-0 z-10 border-b border-line bg-white/95 px-5 py-4 backdrop-blur">
          <div className="mx-auto flex max-w-7xl items-center gap-3"><Button type="button" variant="ghost" className="h-10 w-10 px-0 lg:hidden" aria-label="Open navigation" onClick={() => setMobileOpen(true)}><Menu className="h-4 w-4" /></Button><div><div className="text-sm font-semibold text-ink">Platform control plane</div><div className="text-xs text-muted">Platform administration stays outside the tenant workspace</div></div></div>
        </header>
        <main className="mx-auto w-full max-w-7xl flex-1 px-5 py-6">{children}</main>
        <PoweredBy />
      </div>
    </div>
  );
}
