"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { Lock, RefreshCcw, WalletCards } from "lucide-react";
import { Alert } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardDescription, CardTitle } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { apiRequest } from "@/lib/api/client";
import type { Salon } from "@/types/api";

type SalonListResponse = {
  salons: Salon[];
};

export function BillingDashboard() {
  const [salon, setSalon] = useState<Salon | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");

  async function load() {
    setError("");
    setLoading(true);
    try {
      const salonResponse = await apiRequest<SalonListResponse>("/api/salons");
      setSalon(salonResponse.salons[0] ?? null);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Could not load billing status.");
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    void load();
  }, []);

  if (loading) {
    return (
      <div className="space-y-6">
        <Skeleton className="h-9 w-72" />
        <div className="grid gap-4 md:grid-cols-3">
          <Skeleton className="h-28" />
          <Skeleton className="h-28" />
          <Skeleton className="h-28" />
        </div>
        <Skeleton className="h-80" />
      </div>
    );
  }

  if (error) {
    return (
      <div className="space-y-4">
        <Alert title="Billing unavailable" message={error} />
        <Button type="button" variant="secondary" onClick={() => void load()}>
          <RefreshCcw className="h-4 w-4" />
          Retry
        </Button>
      </div>
    );
  }

  if (!salon) {
    return (
      <Card>
        <CardTitle>Create a salon first</CardTitle>
        <CardDescription>Billing status is scoped by salon, so the owner profile must exist first.</CardDescription>
        <div className="mt-5">
          <Link
            href="/onboarding"
            className="inline-flex h-10 items-center justify-center rounded-md bg-brand px-4 text-sm font-semibold text-white hover:bg-teal-800"
          >
            Create salon profile
          </Link>
        </div>
      </Card>
    );
  }

  return (
    <div className="space-y-6">
      <div className="flex flex-col justify-between gap-3 md:flex-row md:items-end">
        <div>
          <h1 className="text-2xl font-bold text-ink">Billing</h1>
          <p className="mt-1 text-sm text-muted">
            Pilot billing is separated from Square Appointments booking behavior.
          </p>
        </div>
        <Badge value="not_configured" className="self-start" />
      </div>

      <div className="grid gap-4 md:grid-cols-3">
        <BillingMetric label="Plan status" value="Pilot gated" badge="pending" />
        <BillingMetric label="Subscription system" value="Not connected" badge="not_configured" />
        <BillingMetric label="POS booking impact" value="None" badge="allowed" />
      </div>

      <Card>
        <div className="flex flex-col justify-between gap-3 sm:flex-row sm:items-start">
          <div className="flex gap-3">
            <WalletCards className="mt-0.5 h-5 w-5 flex-none text-brand" />
            <div>
              <CardTitle>Pilot billing status</CardTitle>
              <CardDescription>
                {salon.name} is in the foundation pilot scope. This release does not include Stripe, payment methods, invoices, or subscription state.
              </CardDescription>
            </div>
          </div>
          <Badge value="blocked" className="self-start" />
        </div>
      </Card>

      <Card>
        <CardTitle>Locked billing workflows</CardTitle>
        <CardDescription>These actions stay disabled until a billing backend exists.</CardDescription>
        <div className="mt-5 grid gap-3 md:grid-cols-3">
          <LockedAction title="Manage subscription" description="Requires subscription state and plan management." />
          <LockedAction title="Add payment method" description="Requires a payment provider tokenization flow." />
          <LockedAction title="View invoices" description="Requires invoice records from the billing provider." />
        </div>
      </Card>

      <Card>
        <CardTitle>Scope boundary</CardTitle>
        <CardDescription>
          Square Appointments remains the POS provider for availability and booking. App subscription billing will be added as a separate Stripe or billing-provider slice after pilot readiness.
        </CardDescription>
        <div className="mt-5">
          <Link
            href="/dashboard/integrations"
            className="inline-flex h-10 items-center justify-center rounded-md border border-line bg-white px-4 text-sm font-semibold text-ink hover:bg-slate-50"
          >
            Open Square setup
          </Link>
        </div>
      </Card>
    </div>
  );
}

function BillingMetric({ label, value, badge }: { label: string; value: string; badge: string }) {
  return (
    <Card>
      <div className="flex items-start justify-between gap-3">
        <div>
          <div className="text-xs font-semibold uppercase text-muted">{label}</div>
          <div className="mt-2 text-base font-semibold text-ink">{value}</div>
        </div>
        <Badge value={badge} />
      </div>
    </Card>
  );
}

function LockedAction({ title, description }: { title: string; description: string }) {
  return (
    <div className="rounded-md border border-line p-4">
      <div className="flex items-start justify-between gap-3">
        <div>
          <div className="text-sm font-semibold text-ink">{title}</div>
          <div className="mt-1 text-xs leading-5 text-muted">{description}</div>
        </div>
        <Lock className="h-4 w-4 flex-none text-muted" />
      </div>
      <Button type="button" variant="secondary" className="mt-4 w-full" disabled>
        Locked
      </Button>
    </div>
  );
}
