import { ArrowRight, Bot, Building2, History, SlidersHorizontal } from "lucide-react";
import Link from "next/link";
import { Card, CardDescription, CardTitle } from "@/components/ui/card";

const sections = [
  { key: "business", label: "Business", description: "Profile, hours, staff, services, and customers.", icon: Building2 },
  { key: "calls", label: "AI Receptionist", description: "Calls, training, and salon-wide runtime intent.", icon: Bot },
  { key: "integrations", label: "Platform Controls", description: "Integrations, scheduling, operations, access, and configuration copy.", icon: SlidersHorizontal },
  { key: "audit", label: "History", description: "Immutable audit and configuration-transfer evidence.", icon: History }
];

export default function PlatformTenantOverviewPage({ params }: { params: { tenantId: string } }) {
  return <div className="space-y-5"><div><h2 className="text-lg font-bold text-ink">Salon overview</h2><p className="mt-1 text-sm text-muted">Choose the operational area to manage. Each area keeps one primary workflow in focus.</p></div><div className="grid gap-4 md:grid-cols-2">{sections.map((section) => { const Icon = section.icon; return <Link key={section.key} href={`/platform/tenants/${params.tenantId}/${section.key}`}><Card className="h-full transition hover:border-teal-200"><div className="flex items-start justify-between gap-4"><div className="flex gap-3"><div className="flex h-10 w-10 flex-none items-center justify-center rounded-md bg-teal-50 text-brand"><Icon className="h-5 w-5" /></div><div><CardTitle>{section.label}</CardTitle><CardDescription>{section.description}</CardDescription></div></div><ArrowRight className="h-4 w-4 text-muted" /></div></Card></Link>; })}</div></div>;
}
