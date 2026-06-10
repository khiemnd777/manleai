"use client";

import { FormEvent, useEffect, useMemo, useState } from "react";
import type { ReactNode } from "react";
import { AlertTriangle, RefreshCcw, Search, Users } from "lucide-react";
import { Alert } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardDescription, CardTitle } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { apiRequest } from "@/lib/api/client";
import type {
  CustomerRecord,
  CustomerSummary,
  POSConnection,
  POSCustomer,
  Salon,
  SquareReadiness,
  SyncLog
} from "@/types/api";

type SalonListResponse = {
  salons: Salon[];
};

type StatusResponse = {
  connection: POSConnection;
  sync_logs: SyncLog[];
  readiness: SquareReadiness;
};

type CustomersResponse = {
  customers: CustomerRecord[];
  summary: CustomerSummary;
};

type SearchResponse = {
  customer?: POSCustomer;
  found: boolean;
  provider: string;
};

export function CustomersDashboard() {
  const [salon, setSalon] = useState<Salon | null>(null);
  const [status, setStatus] = useState<StatusResponse | null>(null);
  const [customers, setCustomers] = useState<CustomerRecord[]>([]);
  const [summary, setSummary] = useState<CustomerSummary | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [searchPhone, setSearchPhone] = useState("");
  const [searching, setSearching] = useState(false);
  const [searchError, setSearchError] = useState("");
  const [searchResult, setSearchResult] = useState<SearchResponse | null>(null);

  async function load() {
    setError("");
    setLoading(true);
    try {
      const salonResponse = await apiRequest<SalonListResponse>("/api/salons");
      const firstSalon = salonResponse.salons[0] ?? null;
      setSalon(firstSalon);
      if (!firstSalon) {
        setStatus(null);
        setCustomers([]);
        setSummary(null);
        return;
      }

      const [statusResponse, customerResponse] = await Promise.all([
        apiRequest<StatusResponse>(`/api/integrations/square/status?salon_id=${firstSalon.id}`),
        apiRequest<CustomersResponse>(`/api/salons/${firstSalon.id}/customers?limit=100`)
      ]);
      setStatus(statusResponse);
      setCustomers(customerResponse.customers);
      setSummary(customerResponse.summary);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Could not load customer data.");
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    void load();
  }, []);

  const squareConnected = isSquareConnected(status?.connection);
  const metricSummary = useMemo(() => summarizeCustomers(customers, summary), [customers, summary]);

  async function searchSquare(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!salon || !squareConnected || !searchPhone.trim()) return;

    setSearching(true);
    setSearchError("");
    setSearchResult(null);
    try {
      const response = await apiRequest<SearchResponse>(
        `/api/salons/${salon.id}/customers/search?phone=${encodeURIComponent(searchPhone.trim())}`
      );
      setSearchResult(response);
    } catch (err) {
      setSearchError(err instanceof Error ? err.message : "Could not search Square customers.");
    } finally {
      setSearching(false);
    }
  }

  if (loading) {
    return (
      <div className="space-y-6">
        <Skeleton className="h-9 w-80" />
        <div className="grid gap-4 md:grid-cols-4">
          <Skeleton className="h-28" />
          <Skeleton className="h-28" />
          <Skeleton className="h-28" />
          <Skeleton className="h-28" />
        </div>
        <Skeleton className="h-40" />
        <Skeleton className="h-96" />
      </div>
    );
  }

  if (!salon) {
    return (
      <Card>
        <CardTitle>Create a salon first</CardTitle>
        <CardDescription>
          Customer activity is scoped by salon, so the owner profile must exist before calls or bookings can appear here.
        </CardDescription>
      </Card>
    );
  }

  return (
    <div className="space-y-6">
      <div className="flex flex-col justify-between gap-3 md:flex-row md:items-end">
        <div>
          <h1 className="text-2xl font-bold text-ink">Customers</h1>
          <p className="mt-1 text-sm text-muted">
            Known customers from calls, confirmed appointments, and pending requests.
          </p>
        </div>
        <div className="flex flex-wrap items-center gap-3">
          <Badge value={squareConnected ? "connected" : "not_connected"} />
          <Button type="button" variant="secondary" onClick={() => void load()}>
            <RefreshCcw className="h-4 w-4" />
            Refresh
          </Button>
        </div>
      </div>

      {error ? <Alert title="Customers unavailable" message={error} /> : null}

      <CustomerLookupGate status={status} />

      <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-4">
        <Metric label="Known customers" value={String(metricSummary.total_known_customers)} />
        <Metric label="Confirmed appointments" value={String(metricSummary.confirmed_appointments)} />
        <Metric label="Pending requests" value={String(metricSummary.pending_requests)} />
        <Metric label="Last activity" value={formatOptionalDate(metricSummary.last_customer_activity_at)} />
      </div>

      <Card>
        <div className="flex flex-col justify-between gap-3 lg:flex-row lg:items-start">
          <div>
            <CardTitle>Square customer lookup</CardTitle>
            <CardDescription>
              Search Square Appointments by phone without creating or editing customer records from the dashboard.
            </CardDescription>
          </div>
          <Badge value={squareConnected ? "ready" : "disabled"} />
        </div>
        <form className="mt-5 flex flex-col gap-3 sm:flex-row" onSubmit={(event) => void searchSquare(event)}>
          <label className="sr-only" htmlFor="square-customer-phone">
            Customer phone
          </label>
          <input
            id="square-customer-phone"
            className="h-10 flex-1 rounded-md border border-line bg-white px-3 text-sm text-ink outline-none transition focus:border-brand focus:ring-2 focus:ring-teal-100 disabled:bg-slate-100 disabled:text-slate-500"
            value={searchPhone}
            onChange={(event) => setSearchPhone(event.target.value)}
            placeholder="Search by phone"
            disabled={!squareConnected || searching}
          />
          <Button type="submit" variant="secondary" disabled={!squareConnected || !searchPhone.trim() || searching}>
            <Search className="h-4 w-4" />
            {searching ? "Searching" : "Search Square"}
          </Button>
        </form>
        {!squareConnected ? (
          <div className="mt-3 text-sm leading-6 text-muted">
            Connect Square Appointments before using POS customer lookup.
          </div>
        ) : null}
        {searchError ? (
          <div className="mt-4">
            <Alert title="Square lookup failed" message={searchError} />
          </div>
        ) : null}
        {searchResult ? <SearchResult result={searchResult} /> : null}
      </Card>

      <Card>
        <div className="flex flex-col justify-between gap-3 sm:flex-row sm:items-start">
          <div>
            <CardTitle>Customer activity</CardTitle>
            <CardDescription>
              This list is built from existing calls, booking attempts, handoffs, and Square-confirmed appointments.
            </CardDescription>
          </div>
          <Badge value={customers.length > 0 ? "active" : "disabled"} />
        </div>

        {customers.length === 0 ? (
          <EmptyState
            icon={<Users className="h-5 w-5 text-muted" />}
            title="No customer activity yet"
            message="Customers will appear after calls, confirmed appointments, or pending requests."
          />
        ) : (
          <>
            <div className="mt-5 hidden overflow-x-auto rounded-md border border-line lg:block">
              <table className="w-full min-w-[920px] text-left text-sm">
                <thead className="bg-slate-50 text-xs uppercase text-muted">
                  <tr>
                    <th className="px-4 py-3">Customer</th>
                    <th className="px-4 py-3">Last activity</th>
                    <th className="px-4 py-3">Outcome</th>
                    <th className="px-4 py-3">Confirmed</th>
                    <th className="px-4 py-3">Pending</th>
                    <th className="px-4 py-3">Calls</th>
                    <th className="px-4 py-3">Action</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-line bg-white">
                  {customers.map((item) => (
                    <tr key={item.key}>
                      <td className="px-4 py-3">
                        <div className="font-medium text-ink">{customerName(item)}</div>
                        <div className="mt-1 text-xs text-muted">{item.phone || item.email || "No contact detail"}</div>
                      </td>
                      <td className="px-4 py-3">
                        <div className="font-medium text-ink">{formatDateTime(item.last_activity_at)}</div>
                        <div className="mt-1 text-xs text-muted">{formatActivitySource(item.last_activity_source)}</div>
                      </td>
                      <td className="px-4 py-3">
                        <Badge value={item.last_outcome || "unknown"} />
                      </td>
                      <td className="px-4 py-3 text-muted">{item.confirmed_appointments}</td>
                      <td className="px-4 py-3 text-muted">{item.pending_requests}</td>
                      <td className="px-4 py-3 text-muted">{item.call_count}</td>
                      <td className="px-4 py-3">
                        <ActivityLink item={item} />
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            <div className="mt-5 space-y-3 lg:hidden">
              {customers.map((item) => (
                <CustomerCard key={item.key} item={item} />
              ))}
            </div>
          </>
        )}
      </Card>
    </div>
  );
}

function CustomerLookupGate({ status }: { status: StatusResponse | null }) {
  const connection = status?.connection;
  if (isSquareConnected(connection)) {
    return (
      <Card className="border-emerald-200 bg-emerald-50 shadow-none">
        <div className="flex flex-col justify-between gap-3 sm:flex-row sm:items-center">
          <div>
            <CardTitle>Square customer lookup is available</CardTitle>
            <CardDescription className="text-emerald-800">
              The dashboard can search normalized Square customer records by phone. Customer creation still happens only inside booking flows.
            </CardDescription>
          </div>
          <Badge value="ready" />
        </div>
      </Card>
    );
  }

  return (
    <Card className="border-amber-200 bg-amber-50 shadow-none">
      <div className="flex gap-3">
        <AlertTriangle className="mt-0.5 h-5 w-5 flex-none text-amber-700" />
        <div>
          <CardTitle>Square customer lookup is gated</CardTitle>
          <CardDescription className="text-amber-900">
            Existing customer activity still appears here, but POS lookup requires a connected Square Appointments account.
          </CardDescription>
          <a
            className="mt-3 inline-flex h-10 items-center justify-center rounded-md border border-line bg-white px-4 text-sm font-semibold text-ink hover:bg-slate-50"
            href="/dashboard/integrations"
          >
            Open Square integration
          </a>
        </div>
      </div>
    </Card>
  );
}

function SearchResult({ result }: { result: SearchResponse }) {
  if (!result.found || !result.customer) {
    return (
      <div className="mt-4 rounded-md border border-line p-4 text-sm text-muted">
        No Square customer matched that phone number.
      </div>
    );
  }

  return (
    <div className="mt-4 rounded-md border border-line p-4">
      <div className="flex flex-col justify-between gap-3 sm:flex-row sm:items-start">
        <div>
          <div className="text-sm font-semibold text-ink">{result.customer.name || "Unnamed Square customer"}</div>
          <div className="mt-1 text-sm text-muted">{result.customer.phone || "No phone returned"}</div>
          {result.customer.email ? <div className="mt-1 text-sm text-muted">{result.customer.email}</div> : null}
        </div>
        <Badge value={result.provider || "connected"} />
      </div>
    </div>
  );
}

function Metric({ label, value }: { label: string; value: string }) {
  return (
    <Card>
      <div className="text-xs font-semibold uppercase tracking-wide text-muted">{label}</div>
      <div className="mt-2 text-2xl font-bold text-ink">{value}</div>
    </Card>
  );
}

function EmptyState({ icon, title, message }: { icon: ReactNode; title: string; message: string }) {
  return (
    <div className="mt-5 rounded-md border border-line p-6 text-center">
      <div className="mx-auto flex h-10 w-10 items-center justify-center rounded-md bg-slate-100">{icon}</div>
      <div className="mt-3 text-sm font-semibold text-ink">{title}</div>
      <div className="mt-1 text-sm leading-6 text-muted">{message}</div>
    </div>
  );
}

function CustomerCard({ item }: { item: CustomerRecord }) {
  return (
    <div className="rounded-md border border-line p-4">
      <div className="flex items-start justify-between gap-3">
        <div>
          <div className="text-sm font-semibold text-ink">{customerName(item)}</div>
          <div className="mt-1 text-xs text-muted">{item.phone || item.email || "No contact detail"}</div>
        </div>
        <Badge value={item.last_outcome || "unknown"} />
      </div>
      <InfoGrid
        items={[
          ["Last activity", `${formatDateTime(item.last_activity_at)} · ${formatActivitySource(item.last_activity_source)}`],
          ["Confirmed", String(item.confirmed_appointments)],
          ["Pending", String(item.pending_requests)],
          ["Calls", String(item.call_count)]
        ]}
      />
      <div className="mt-4">
        <ActivityLink item={item} />
      </div>
    </div>
  );
}

function InfoGrid({ items }: { items: [string, string][] }) {
  return (
    <div className="mt-4 grid gap-3 text-sm sm:grid-cols-2">
      {items.map(([label, value]) => (
        <div key={label}>
          <div className="text-xs font-semibold uppercase tracking-wide text-muted">{label}</div>
          <div className="mt-1 break-words font-medium text-ink">{value}</div>
        </div>
      ))}
    </div>
  );
}

function ActivityLink({ item }: { item: CustomerRecord }) {
  if (item.pending_requests > 0 || item.confirmed_appointments > 0) {
    return (
      <a className="text-sm font-semibold text-brand hover:text-teal-800" href="/dashboard/appointments">
        Open appointments
      </a>
    );
  }
  if (item.call_count > 0 || item.handoff_count > 0) {
    return (
      <a className="text-sm font-semibold text-brand hover:text-teal-800" href="/dashboard/calls">
        Open calls
      </a>
    );
  }
  return <span className="text-sm text-muted">No action</span>;
}

function summarizeCustomers(customers: CustomerRecord[], summary: CustomerSummary | null): CustomerSummary {
  if (summary) return summary;
  return customers.reduce<CustomerSummary>(
    (acc, item) => {
      acc.total_known_customers += 1;
      acc.confirmed_appointments += item.confirmed_appointments;
      acc.pending_requests += item.pending_requests;
      if (item.call_count > 0) acc.customers_with_calls += 1;
      if (!acc.last_customer_activity_at || new Date(item.last_activity_at) > new Date(acc.last_customer_activity_at)) {
        acc.last_customer_activity_at = item.last_activity_at;
      }
      return acc;
    },
    {
      total_known_customers: 0,
      confirmed_appointments: 0,
      pending_requests: 0,
      customers_with_calls: 0
    }
  );
}

function isSquareConnected(connection?: POSConnection) {
  return Boolean(connection?.id) && connection?.status !== "not_connected";
}

function customerName(item: CustomerRecord) {
  return item.name || item.phone || item.email || "Unknown customer";
}

function formatActivitySource(value: string) {
  if (value === "booking_attempt") return "Pending request";
  if (value === "appointment") return "Appointment";
  if (value === "call") return "Call";
  if (value === "handoff") return "Owner handoff";
  return "Customer activity";
}

function formatOptionalDate(value?: string) {
  if (!value) return "No activity";
  return formatDate(value);
}

function formatDateTime(value: string) {
  return new Date(value).toLocaleString(undefined, {
    month: "short",
    day: "numeric",
    year: "numeric",
    hour: "numeric",
    minute: "2-digit"
  });
}

function formatDate(value: string) {
  return new Date(value).toLocaleDateString(undefined, {
    month: "short",
    day: "numeric",
    year: "numeric"
  });
}
