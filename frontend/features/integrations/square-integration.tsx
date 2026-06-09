"use client";

import { useEffect, useMemo, useState } from "react";
import { CheckCircle2, ExternalLink, Link2, RefreshCcw } from "lucide-react";
import { Alert } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardDescription, CardTitle } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { apiRequest } from "@/lib/api/client";
import type { POSConnection, Salon, SyncLog } from "@/types/api";

type SalonListResponse = {
  salons: Salon[];
};

type StatusResponse = {
  connection: POSConnection;
  sync_logs: SyncLog[];
};

export function SquareIntegration() {
  const [salons, setSalons] = useState<Salon[]>([]);
  const [status, setStatus] = useState<StatusResponse | null>(null);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");
  const [success, setSuccess] = useState("");

  const salon = salons[0];

  async function load() {
    setError("");
    setLoading(true);
    try {
      const salonResponse = await apiRequest<SalonListResponse>("/api/salons");
      setSalons(salonResponse.salons);
      const firstSalon = salonResponse.salons[0];
      if (firstSalon) {
        const squareStatus = await apiRequest<StatusResponse>(
          `/api/integrations/square/status?salon_id=${firstSalon.id}`
        );
        setStatus(squareStatus);
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : "Could not load integrations.");
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    void load();
  }, []);

  const steps = useMemo(() => {
    const connection = status?.connection;
    return [
      { label: "Connect Square", complete: Boolean(connection?.id) },
      { label: "Select Location", complete: Boolean(connection?.location_id) },
      { label: "Sync Services", complete: Boolean(connection?.last_sync_at) },
      { label: "Sync Staff", complete: Boolean(connection?.last_sync_at) },
      { label: "Create Test Booking", complete: false },
      { label: "Cancel Test Booking", complete: false },
      { label: "Enable AI Booking", complete: false }
    ];
  }, [status]);

  async function connectSquare() {
    if (!salon) return;
    setBusy("connect");
    setError("");
    try {
      const response = await apiRequest<{ url: string }>(
        `/api/integrations/square/connect-url?salon_id=${salon.id}`
      );
      window.location.href = response.url;
    } catch (err) {
      setError(err instanceof Error ? err.message : "Could not start Square OAuth.");
    } finally {
      setBusy("");
    }
  }

  async function syncSquare() {
    if (!salon) return;
    setBusy("sync");
    setError("");
    setSuccess("");
    try {
      await apiRequest<{ ok: boolean }>("/api/integrations/square/sync", {
        method: "POST",
        body: JSON.stringify({ salon_id: salon.id })
      });
      setSuccess("Services and staff sync completed.");
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Square sync failed.");
    } finally {
      setBusy("");
    }
  }

  if (loading) {
    return (
      <div className="space-y-6">
        <Skeleton className="h-9 w-80" />
        <Skeleton className="h-56" />
        <Skeleton className="h-72" />
      </div>
    );
  }

  if (error && !status) {
    return <Alert title="Integration unavailable" message={error} />;
  }

  if (!salon) {
    return (
      <Card>
        <CardTitle>Create a salon first</CardTitle>
        <CardDescription>
          Square connection is scoped by salon, so the owner profile must exist before OAuth.
        </CardDescription>
      </Card>
    );
  }

  const connection = status?.connection;

  return (
    <div className="space-y-6">
      <div className="flex flex-col justify-between gap-3 md:flex-row md:items-end">
        <div>
          <h1 className="text-2xl font-bold text-ink">Integrations</h1>
          <p className="mt-1 text-sm text-muted">
            Square Appointments is the only native POS integration in this pilot release.
          </p>
        </div>
        <Button type="button" variant="secondary" onClick={() => void load()}>
          <RefreshCcw className="h-4 w-4" />
          Refresh
        </Button>
      </div>

      {error ? <Alert title="Square action failed" message={error} /> : null}
      {success ? <Alert type="success" title="Square updated" message={success} /> : null}

      <Card>
        <div className="flex flex-col justify-between gap-5 lg:flex-row lg:items-start">
          <div>
            <div className="flex items-center gap-3">
              <div className="flex h-11 w-11 items-center justify-center rounded-md bg-slate-900 text-sm font-bold text-white">
                SQ
              </div>
              <div>
                <CardTitle>Square Appointments</CardTitle>
                <CardDescription>
                  Connect OAuth, choose a location, then sync services and staff into this system.
                </CardDescription>
              </div>
            </div>
          </div>
          <Badge value={connection?.status ?? "not_connected"} />
        </div>

        <div className="mt-6 grid gap-4 md:grid-cols-2 xl:grid-cols-4">
          <Info label="Provider" value="Square" />
          <Info label="Merchant ID" value={connection?.merchant_id || "Not connected"} />
          <Info label="Location ID" value={connection?.location_id || "Not selected"} />
          <Info label="Last Sync" value={connection?.last_sync_at || "Never"} />
        </div>

        {connection?.error_message ? (
          <div className="mt-5">
            <Alert title="Last Square error" message={connection.error_message} />
          </div>
        ) : null}

        <div className="mt-6 flex flex-wrap gap-3">
          <Button type="button" onClick={() => void connectSquare()} disabled={busy === "connect"}>
            <ExternalLink className="h-4 w-4" />
            {busy === "connect" ? "Opening..." : "Connect Square"}
          </Button>
          <Button
            type="button"
            variant="secondary"
            onClick={() => void syncSquare()}
            disabled={!connection?.id || busy === "sync"}
          >
            <RefreshCcw className="h-4 w-4" />
            {busy === "sync" ? "Syncing..." : "Sync Services and Staff"}
          </Button>
          <Button type="button" variant="secondary" disabled>
            Create Test Booking
          </Button>
          <Button type="button" variant="secondary" disabled>
            Enable AI Booking
          </Button>
        </div>
      </Card>

      <div className="grid gap-4 xl:grid-cols-[0.85fr_1.15fr]">
        <Card>
          <CardTitle>Setup checks</CardTitle>
          <CardDescription>
            AI booking remains disabled until all checks pass. Test booking gates arrive in
            Milestone 3.
          </CardDescription>
          <div className="mt-5 space-y-3">
            {steps.map((step) => (
              <div key={step.label} className="flex items-center justify-between rounded-md border border-line p-3">
                <div className="flex items-center gap-3">
                  <CheckCircle2
                    className={step.complete ? "h-5 w-5 text-brand" : "h-5 w-5 text-slate-300"}
                  />
                  <span className="text-sm font-medium text-ink">{step.label}</span>
                </div>
                <Badge value={step.complete ? "active" : "disabled"} />
              </div>
            ))}
          </div>
        </Card>

        <Card>
          <CardTitle>Recent sync logs</CardTitle>
          <CardDescription>Provider sync activity and failures are stored for troubleshooting.</CardDescription>
          <div className="mt-5 overflow-hidden rounded-md border border-line">
            <table className="w-full text-left text-sm">
              <thead className="bg-slate-50 text-xs uppercase text-muted">
                <tr>
                  <th className="px-4 py-3">Type</th>
                  <th className="px-4 py-3">Status</th>
                  <th className="px-4 py-3">Started</th>
                  <th className="px-4 py-3">Message</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-line bg-white">
                {status?.sync_logs?.length ? (
                  status.sync_logs.map((log) => (
                    <tr key={log.id}>
                      <td className="px-4 py-3 font-medium text-ink">{log.sync_type}</td>
                      <td className="px-4 py-3">
                        <Badge value={log.status === "succeeded" ? "active" : log.status} />
                      </td>
                      <td className="px-4 py-3 text-muted">{new Date(log.started_at).toLocaleString()}</td>
                      <td className="px-4 py-3 text-muted">{log.message || "-"}</td>
                    </tr>
                  ))
                ) : (
                  <tr>
                    <td className="px-4 py-8 text-center text-muted" colSpan={4}>
                      No sync logs yet.
                    </td>
                  </tr>
                )}
              </tbody>
            </table>
          </div>
        </Card>
      </div>
    </div>
  );
}

function Info({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <div className="text-xs font-semibold uppercase tracking-wide text-muted">{label}</div>
      <div className="mt-1 break-words text-sm font-medium text-ink">{value}</div>
    </div>
  );
}

