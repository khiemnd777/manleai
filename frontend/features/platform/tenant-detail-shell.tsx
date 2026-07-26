"use client";

import { useEffect, useState } from "react";
import { ArrowLeft, Building2 } from "lucide-react";
import Link from "next/link";
import { usePathname } from "next/navigation";
import { Alert } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Card } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { businessGet, type BusinessSalonProfile, type BusinessSurface } from "@/lib/api/business";
import { cn } from "@/lib/utils/cn";

const tabs = [
  { key: "business", label: "Business" },
  { key: "technical", label: "Technical settings" },
  { key: "operations", label: "Operations" },
  { key: "audit", label: "Audit" }
];

export function PlatformTenantDetailShell({ tenantID, children }: { tenantID: string; children: React.ReactNode }) {
  const pathname = usePathname();
  const [profile, setProfile] = useState<BusinessSalonProfile | null>(null);
  const [error, setError] = useState("");
  useEffect(() => {
    const surface: BusinessSurface = { kind: "platform", salonID: tenantID };
    businessGet<BusinessSalonProfile>(surface, "profile").then(setProfile).catch((failure: unknown) => setError(failure instanceof Error ? failure.message : "Could not load salon details."));
  }, [tenantID]);
  return <div className="space-y-5"><Link href="/platform/tenants" className="inline-flex items-center gap-2 text-sm font-semibold text-brand hover:text-teal-800"><ArrowLeft className="h-4 w-4" />All nail salons</Link>{error ? <Alert title="Salon unavailable" message={error} /> : null}{!profile && !error ? <Skeleton className="h-24 w-full" /> : null}{profile ? <Card className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between"><div className="flex min-w-0 items-center gap-3"><div className="flex h-11 w-11 flex-none items-center justify-center rounded-md bg-teal-50 text-brand"><Building2 className="h-5 w-5" /></div><div className="min-w-0"><h1 className="truncate text-xl font-bold text-ink">{profile.name}</h1><p className="truncate text-sm text-muted">{[profile.city, profile.state].filter(Boolean).join(", ") || profile.timezone}</p></div></div><Badge value={profile.public_catalog_enabled ? "active" : "not_configured"} /></Card> : null}<nav className="flex gap-1 overflow-x-auto rounded-lg border border-line bg-white p-1 shadow-soft" aria-label="Salon detail sections">{tabs.map((tab) => { const href=`/platform/tenants/${tenantID}/${tab.key}`; const active=pathname===href || pathname.startsWith(`${href}/`); return <Link key={tab.key} href={href} className={cn("whitespace-nowrap rounded-md px-4 py-2 text-sm font-semibold text-slate-600 hover:bg-slate-50", active && "bg-teal-50 text-brand")}>{tab.label}</Link>; })}</nav>{children}</div>;
}
