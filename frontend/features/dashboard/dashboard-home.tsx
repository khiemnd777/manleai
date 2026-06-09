"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { CalendarDays, CheckCircle2, Link2, PhoneCall, Settings } from "lucide-react";
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

export function DashboardHome() {
  const [salons, setSalons] = useState<Salon[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");

  useEffect(() => {
    let mounted = true;
    apiRequest<SalonListResponse>("/api/salons")
      .then((data) => {
        if (mounted) setSalons(data.salons);
      })
      .catch((err) => {
        if (mounted) setError(err instanceof Error ? err.message : "Could not load dashboard.");
      })
      .finally(() => {
        if (mounted) setLoading(false);
      });
    return () => {
      mounted = false;
    };
  }, []);

  if (loading) {
    return (
      <div className="space-y-6">
        <Skeleton className="h-9 w-72" />
        <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-4">
          {Array.from({ length: 4 }).map((_, index) => (
            <Skeleton key={index} className="h-32" />
          ))}
        </div>
        <Skeleton className="h-72" />
      </div>
    );
  }

  if (error) {
    return <Alert title="Dashboard unavailable" message={error} />;
  }

  const primarySalon = salons[0];

  return (
    <div className="space-y-6">
      <div className="flex flex-col justify-between gap-3 md:flex-row md:items-end">
        <div>
          <h1 className="text-2xl font-bold text-ink">Dashboard</h1>
          <p className="mt-1 text-sm text-muted">
            Configure the salon, connect Square, and monitor pilot readiness.
          </p>
        </div>
        {primarySalon ? <Badge value={primarySalon.ai_enabled ? "active" : "disabled"} /> : null}
      </div>

      {salons.length === 0 ? (
        <Card>
          <CardTitle>No salon profile yet</CardTitle>
          <CardDescription>
            Create a salon through the API or onboarding flow before connecting Square.
          </CardDescription>
          <div className="mt-5">
            <Link href="/onboarding">
              <Button type="button">Create salon profile</Button>
            </Link>
          </div>
        </Card>
      ) : (
        <>
          <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-4">
            <MetricCard icon={PhoneCall} label="Calls Today" value="0" subtext="Voice starts later" />
            <MetricCard icon={CalendarDays} label="Bookings Created" value="0" subtext="Milestone 3" />
            <MetricCard icon={Link2} label="POS Status" value="Square" subtext="Open Integrations" />
            <MetricCard
              icon={CheckCircle2}
              label="AI Status"
              value={primarySalon.ai_enabled ? "Enabled" : "Disabled"}
              subtext="Requires Square checks"
            />
          </div>

          <div className="grid gap-4 xl:grid-cols-[1.3fr_0.7fr]">
            <Card>
              <CardTitle>{primarySalon.name}</CardTitle>
              <CardDescription>
                {primarySalon.phone} · {primarySalon.city || "City not set"}{" "}
                {primarySalon.state || ""}
              </CardDescription>
              <dl className="mt-5 grid gap-4 sm:grid-cols-2">
                <Info label="Timezone" value={primarySalon.timezone} />
                <Info label="Primary language" value={primarySalon.primary_language.toUpperCase()} />
                <Info label="Secondary language" value={primarySalon.secondary_language.toUpperCase()} />
                <Info label="Handoff phone" value={primarySalon.handoff_phone || "Not configured"} />
              </dl>
            </Card>
            <Card>
              <div className="flex items-start gap-3">
                <Settings className="mt-1 h-5 w-5 text-brand" />
                <div>
                  <CardTitle>Pilot readiness</CardTitle>
                  <CardDescription>
                    Milestone 1/2 covers auth, salon profile, and Square connection foundation.
                    Booking tests and AI enablement are gated until Milestone 3.
                  </CardDescription>
                </div>
              </div>
            </Card>
          </div>
        </>
      )}
    </div>
  );
}

function MetricCard({
  icon: Icon,
  label,
  value,
  subtext
}: {
  icon: React.ElementType;
  label: string;
  value: string;
  subtext: string;
}) {
  return (
    <Card>
      <div className="flex items-center justify-between">
        <span className="text-sm font-medium text-muted">{label}</span>
        <Icon className="h-4 w-4 text-brand" />
      </div>
      <div className="mt-4 text-2xl font-bold text-ink">{value}</div>
      <div className="mt-1 text-xs text-muted">{subtext}</div>
    </Card>
  );
}

function Info({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <dt className="text-xs font-semibold uppercase tracking-wide text-muted">{label}</dt>
      <dd className="mt-1 text-sm font-medium text-ink">{value}</dd>
    </div>
  );
}
