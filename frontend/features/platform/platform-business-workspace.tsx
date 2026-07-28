"use client";

import { useState } from "react";
import { Building2, Users, UserRound } from "lucide-react";
import { BusinessCustomers } from "@/features/business/business-customers";
import { BusinessSettings } from "@/features/business/business-settings";
import { BusinessStaffManager } from "@/features/business/business-staff";
import type { BusinessSurface } from "@/lib/api/business";
import { cn } from "@/lib/utils/cn";

type Section = "profile" | "staff" | "customers";

const sections = [
  { key: "profile" as const, label: "Profile, hours & public page", icon: Building2 },
  { key: "staff" as const, label: "Staff & service assignments", icon: Users },
  { key: "customers" as const, label: "Customers", icon: UserRound }
];

export function PlatformBusinessWorkspace({ tenantID }: { tenantID: string }) {
  const [section, setSection] = useState<Section>("profile");
  const surface: BusinessSurface = { kind: "platform", salonID: tenantID };

  return (
    <div className="space-y-5">
      <div>
        <h2 className="text-lg font-bold text-ink">Business management</h2>
        <p className="mt-1 text-sm text-muted">
          Platform Ops can perform the same authorized Business work when a salon delegates it. Services use the
          salon&apos;s complete Services workspace; technical configuration stays in its separate tab.
        </p>
      </div>
      <div className="grid gap-2 sm:grid-cols-2 xl:grid-cols-3">
        {sections.map((item) => {
          const Icon = item.icon;
          return (
            <button
              key={item.key}
              type="button"
              onClick={() => setSection(item.key)}
              className={cn(
                "flex items-center gap-3 rounded-lg border border-line bg-white p-4 text-left text-sm font-semibold text-slate-700 shadow-soft transition hover:border-teal-200",
                section === item.key && "border-teal-300 bg-teal-50 text-brand"
              )}
            >
              <Icon className="h-4 w-4 flex-none" />
              {item.label}
            </button>
          );
        })}
      </div>
      {section === "profile" ? <BusinessSettings surface={surface} title="Profile, hours & public page" /> : null}
      {section === "staff" ? <BusinessStaffManager surface={surface} title="Staff & service assignments" /> : null}
      {section === "customers" ? <BusinessCustomers surface={surface} /> : null}
    </div>
  );
}
