"use client";

import { useEffect, useMemo, useState } from "react";
import { ArrowLeft, Building2 } from "lucide-react";
import Link from "next/link";
import { usePathname } from "next/navigation";
import { Alert } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Card } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { isSampleData } from "@/lib/api/business";
import { getPlatformTenantContext } from "@/lib/api/platform-tenant-context";
import type { PlatformTenantContext } from "@/lib/api/platform-tenant-context";
import { cn } from "@/lib/utils/cn";

type NavItem = { label: string; path: string; capability?: string };
type NavGroup = { key: string; label: string; defaultPath: string; paths: string[]; items: NavItem[] };

const groups: NavGroup[] = [
  { key: "overview", label: "Overview", defaultPath: "overview", paths: ["overview"], items: [] },
  {
    key: "business", label: "Business", defaultPath: "business", paths: ["business", "services", "staff", "customers"],
    items: [
      { label: "Profile & hours", path: "business", capability: "business.read" },
      { label: "Staff", path: "staff", capability: "business.read" },
      { label: "Services", path: "services", capability: "services.read" },
      { label: "Customers", path: "customers", capability: "business.read" }
    ]
  },
  {
    key: "ai", label: "AI Receptionist", defaultPath: "calls", paths: ["calls", "training", "runtime"],
    items: [
      { label: "Calls", path: "calls", capability: "calls.read" },
      { label: "Training", path: "training", capability: "training.read" },
      { label: "Runtime", path: "runtime", capability: "technical.read" }
    ]
  },
  {
    key: "controls", label: "Platform Controls", defaultPath: "integrations", paths: ["integrations", "scheduling", "operations", "access", "transfer", "technical"],
    items: [
      { label: "Integrations", path: "integrations", capability: "technical.read" },
      { label: "Scheduling", path: "scheduling", capability: "technical.read" },
      { label: "Operations", path: "operations", capability: "operations.read" },
      { label: "Access", path: "access", capability: "platform.access.manage" },
      { label: "Copy configuration", path: "transfer", capability: "technical.read" }
    ]
  },
  {
    key: "history", label: "History", defaultPath: "audit", paths: ["audit", "configuration-transfers"],
    items: [
      { label: "Audit events", path: "audit", capability: "audit.read" },
      { label: "Configuration transfers", path: "configuration-transfers", capability: "audit.read" }
    ]
  }
];

export function PlatformTenantDetailShell({ tenantID, children }: { tenantID: string; children: React.ReactNode }) {
  const pathname = usePathname();
  const [context, setContext] = useState<PlatformTenantContext | null>(null);
  const [error, setError] = useState("");
  useEffect(() => {
    let active = true;
    setError("");
    getPlatformTenantContext(tenantID)
      .then((result) => { if (active) setContext(result); })
      .catch((failure: unknown) => { if (active) setError(failure instanceof Error ? failure.message : "Could not load salon details."); });
    return () => { active = false; };
  }, [tenantID]);

  const allowed = useMemo(() => new Set(context?.meta.permissions.allowed_actions ?? []), [context]);
  const segment = pathname.split("/").filter(Boolean)[3] ?? "overview";
  const visibleGroups = groups.filter((group) => group.key === "overview" || group.items.some((item) => !item.capability || allowed.has(item.capability)));
  const activeGroup = visibleGroups.find((group) => group.paths.includes(segment)) ?? visibleGroups[0];
  const secondary = activeGroup?.items.filter((item) => !item.capability || allowed.has(item.capability)) ?? [];
  const profile = context?.data;

  return (
    <div className="space-y-5">
      <Link href="/platform/tenants" className="inline-flex items-center gap-2 text-sm font-semibold text-brand hover:text-teal-800"><ArrowLeft className="h-4 w-4" />All nail salons</Link>
      {error ? <Alert title="Salon unavailable" message={error} /> : null}
      {!profile && !error ? <Skeleton className="h-24 w-full" /> : null}
      {profile ? <Card className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between"><div className="flex min-w-0 items-center gap-3"><div className="flex h-11 w-11 flex-none items-center justify-center rounded-md bg-teal-50 text-brand"><Building2 className="h-5 w-5" /></div><div className="min-w-0"><div className="flex min-w-0 items-center gap-2"><h1 className="truncate text-xl font-bold text-ink">{profile.name}</h1>{isSampleData(profile) ? <Badge value="sample_test" /> : null}</div><p className="truncate text-sm text-muted">{[profile.city, profile.state].filter(Boolean).join(", ") || profile.timezone}</p></div></div><Badge value={profile.public_catalog_enabled ? "active" : "not_configured"} /></Card> : null}
      <nav className="flex gap-1 overflow-x-auto rounded-lg border border-line bg-white p-1 shadow-soft" aria-label="Salon workflow groups">
        {visibleGroups.map((group) => { const href = `/platform/tenants/${tenantID}/${group.defaultPath}`; const active = activeGroup?.key === group.key; return <Link key={group.key} href={href} className={cn("whitespace-nowrap rounded-md px-4 py-2 text-sm font-semibold text-slate-600 hover:bg-slate-50", active && "bg-teal-50 text-brand")}>{group.label}</Link>; })}
      </nav>
      {secondary.length > 1 ? <nav className="flex gap-5 overflow-x-auto border-b border-line px-1" aria-label={`${activeGroup.label} sections`}>{secondary.map((item) => { const href = `/platform/tenants/${tenantID}/${item.path}`; const active = segment === item.path || (segment === "technical" && item.path === "integrations"); return <Link key={item.path} href={href} className={cn("whitespace-nowrap border-b-2 border-transparent pb-3 text-sm font-medium text-muted hover:text-ink", active && "border-brand text-brand")}>{item.label}</Link>; })}</nav> : null}
      {children}
    </div>
  );
}
