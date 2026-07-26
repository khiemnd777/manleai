"use client";

import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { useTenantSalon } from "@/components/layout/tenant-salon-context";
import { BusinessCustomers } from "@/features/business/business-customers";
import { BusinessServices } from "@/features/business/business-services";
import { BusinessSettings } from "@/features/business/business-settings";
import { BusinessStaffManager } from "@/features/business/business-staff";
import type { BusinessSurface } from "@/lib/api/business";

export function TenantBusinessPage({ section }: { section: "services" | "staff" | "customers" | "settings" }) {
  const tenant = useTenantSalon();
  if (tenant.loading) return <div className="space-y-4"><Skeleton className="h-12 w-64" /><Skeleton className="h-72 w-full" /></div>;
  if (tenant.error) return <Alert title="Could not load tenant context" message={tenant.error}><Button type="button" variant="secondary" className="mt-4" onClick={tenant.reload}>Retry</Button></Alert>;
  if (!tenant.activeSalonID) return <Alert title="No salon workspace" message="This account has no active salon membership." />;
  const surface: BusinessSurface={kind:"tenant",salonID:tenant.activeSalonID};
  if(section==="services")return <BusinessServices surface={surface}/>;
  if(section==="staff")return <BusinessStaffManager surface={surface}/>;
  if(section==="customers")return <BusinessCustomers surface={surface}/>;
  return <BusinessSettings surface={surface}/>;
}
