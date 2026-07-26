"use client";

import { FormEvent, useEffect, useMemo, useState } from "react";
import type { ReactNode } from "react";
import { AlertTriangle, Archive, ChevronLeft, ChevronRight, Pencil, Plus, RefreshCcw, Search, Users } from "lucide-react";
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
  limit?: number;
  offset?: number;
  has_more?: boolean;
};

type CustomerMutationResponse = {
  customer: CustomerRecord;
};

type SearchResponse = {
  customer?: POSCustomer;
  found: boolean;
  provider: string;
};

type CustomerFormState = {
  name: string;
  phone: string;
  email: string;
  notes: string;
  active: boolean;
};

const defaultCustomerPageSize = 10;
const customerPageSizeOptions = [10, 25, 50] as const;

export function CustomersDashboard() {
  const [salon, setSalon] = useState<Salon | null>(null);
  const [status, setStatus] = useState<StatusResponse | null>(null);
  const [customers, setCustomers] = useState<CustomerRecord[]>([]);
  const [summary, setSummary] = useState<CustomerSummary | null>(null);
  const [customerLimit, setCustomerLimit] = useState(defaultCustomerPageSize);
  const [customerOffset, setCustomerOffset] = useState(0);
  const [customerHasMore, setCustomerHasMore] = useState(false);
  const [loading, setLoading] = useState(true);
  const [customerListLoading, setCustomerListLoading] = useState(false);
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");
  const [success, setSuccess] = useState("");
  const [formOpen, setFormOpen] = useState(false);
  const [editingCustomer, setEditingCustomer] = useState<CustomerRecord | null>(null);
  const [form, setForm] = useState<CustomerFormState>(emptyCustomerForm());
  const [searchPhone, setSearchPhone] = useState("");
  const [searching, setSearching] = useState(false);
  const [searchError, setSearchError] = useState("");
  const [searchResult, setSearchResult] = useState<SearchResponse | null>(null);

  function customerListPath(salonID: string, limit: number, offset: number) {
    const params = new URLSearchParams({
      limit: String(limit),
      offset: String(offset)
    });
    return `/api/salons/${salonID}/customers?${params.toString()}`;
  }

  async function fetchCustomerPage(salonID: string, limit: number, offset: number) {
    return apiRequest<CustomersResponse>(customerListPath(salonID, limit, offset));
  }

  async function fetchCustomerPageWithFallback(salonID: string, limit: number, offset: number) {
    let response = await fetchCustomerPage(salonID, limit, offset);
    if (response.customers.length === 0 && offset > 0) {
      const previousOffset = Math.max(0, offset - limit);
      response = await fetchCustomerPage(salonID, limit, previousOffset);
    }
    return response;
  }

  function applyCustomerPage(response: CustomersResponse, requestedLimit: number, requestedOffset: number) {
    setCustomers(response.customers);
    setSummary(response.summary);
    setCustomerLimit(response.limit ?? requestedLimit);
    setCustomerOffset(response.offset ?? requestedOffset);
    setCustomerHasMore(Boolean(response.has_more));
  }

  async function reloadCustomerRows(salonID: string, offset = customerOffset, limit = customerLimit) {
    setCustomerListLoading(true);
    try {
      const response = await fetchCustomerPageWithFallback(salonID, limit, offset);
      applyCustomerPage(response, limit, offset);
    } finally {
      setCustomerListLoading(false);
    }
  }

  async function load({
    silent = false,
    offset = customerOffset,
    limit = customerLimit
  }: { silent?: boolean; offset?: number; limit?: number } = {}) {
    setError("");
    if (!silent) {
      setLoading(true);
    } else {
      setCustomerListLoading(true);
    }
    try {
      const salonResponse = await apiRequest<SalonListResponse>("/api/salons");
      const firstSalon = salonResponse.salons[0] ?? null;
      setSalon(firstSalon);
      if (!firstSalon) {
        setStatus(null);
        setCustomers([]);
        setSummary(null);
        setCustomerHasMore(false);
        return;
      }

      const [statusResponse, customerResponse] = await Promise.all([
        apiRequest<StatusResponse>(`/api/integrations/square/status?salon_id=${firstSalon.id}`),
        fetchCustomerPageWithFallback(firstSalon.id, limit, offset)
      ]);
      setStatus(statusResponse);
      applyCustomerPage(customerResponse, limit, offset);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Could not load customer data.");
    } finally {
      if (!silent) {
        setLoading(false);
      } else {
        setCustomerListLoading(false);
      }
    }
  }

  useEffect(() => {
    void load();
  }, []);

  const squareConnected = isSquareConnected(status?.connection);
  const metricSummary = useMemo(() => summarizeCustomers(customers, summary), [customers, summary]);
  const metrics = useMemo(() => customerMetrics(customers, metricSummary), [customers, metricSummary]);
  const customerListBusy = customerListLoading || busy !== "" || searching;

  function updateCustomerPageSize(limit: number) {
    setCustomerLimit(limit);
    setCustomerOffset(0);
    if (salon) {
      void reloadCustomerRows(salon.id, 0, limit);
    }
  }

  function goToPreviousCustomerPage() {
    const previousOffset = Math.max(0, customerOffset - customerLimit);
    setCustomerOffset(previousOffset);
    if (salon) {
      void reloadCustomerRows(salon.id, previousOffset, customerLimit);
    }
  }

  function goToNextCustomerPage() {
    if (!customerHasMore) return;
    const nextOffset = customerOffset + customerLimit;
    setCustomerOffset(nextOffset);
    if (salon) {
      void reloadCustomerRows(salon.id, nextOffset, customerLimit);
    }
  }

  function openCreateForm(seed?: Partial<CustomerFormState>) {
    setEditingCustomer(null);
    setForm({ ...emptyCustomerForm(), ...seed });
    setFormOpen(true);
    setError("");
    setSuccess("");
  }

  function openEditForm(customer: CustomerRecord) {
    setEditingCustomer(customer);
    setForm(customerToForm(customer));
    setFormOpen(true);
    setError("");
    setSuccess("");
  }

  async function saveCustomer() {
    if (!salon) return;
    setBusy("save-customer");
    setError("");
    setSuccess("");
    try {
      const body = JSON.stringify(customerPayload(form));
      const response = editingCustomer?.id
        ? await apiRequest<CustomerMutationResponse>(`/api/salons/${salon.id}/customers/${editingCustomer.id}`, {
            method: "PUT",
            body
          })
        : await apiRequest<CustomerMutationResponse>(`/api/salons/${salon.id}/customers`, {
            method: "POST",
            body
          });
      setCustomers((current) => upsertCustomer(current, response.customer));
      setEditingCustomer(response.customer);
      setForm(customerToForm(response.customer));
      setSuccess(editingCustomer ? "Customer saved." : "Customer created. Square customer links are created during booking when needed.");
      await load({ silent: true });
    } catch (err) {
      setError(err instanceof Error ? err.message : "Could not save customer.");
    } finally {
      setBusy("");
    }
  }

  async function archiveCustomer(customer: CustomerRecord) {
    if (!salon || !customer.id || customer.archived_at) return;
    if (!window.confirm(`Archive ${customerName(customer)}? This keeps history but removes the customer from active workflows.`)) return;
    setBusy(`archive-${customer.id}`);
    setError("");
    setSuccess("");
    try {
      const response = await apiRequest<CustomerMutationResponse>(`/api/salons/${salon.id}/customers/${customer.id}/archive`, {
        method: "POST"
      });
      setCustomers((current) => upsertCustomer(current, response.customer));
      if (editingCustomer?.id === response.customer.id) {
        setEditingCustomer(response.customer);
        setForm(customerToForm(response.customer));
      }
      setSuccess("Customer archived. Activity history remains visible.");
      await load({ silent: true });
    } catch (err) {
      setError(err instanceof Error ? err.message : "Could not archive customer.");
    } finally {
      setBusy("");
    }
  }

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
        <CardDescription>Customer records are scoped by salon, so the owner profile must exist first.</CardDescription>
      </Card>
    );
  }

  return (
    <div className="space-y-6">
      <div className="flex flex-col justify-between gap-3 md:flex-row md:items-end">
        <div>
          <h1 className="text-2xl font-bold text-ink">Customers</h1>
          <p className="mt-1 text-sm text-muted">
            Manage ManleAI customer records and review calls, pending requests, and authority-confirmed appointment activity.
          </p>
        </div>
        <div className="flex flex-wrap items-center gap-3">
          <Badge value={squareConnected ? "connected" : "not_connected"} />
          <Button type="button" variant="secondary" onClick={() => void load()} disabled={busy !== ""}>
            <RefreshCcw className="h-4 w-4" />
            Refresh
          </Button>
          <Button type="button" onClick={() => openCreateForm()} disabled={busy !== ""}>
            <Plus className="h-4 w-4" />
            New customer
          </Button>
        </div>
      </div>

      {error ? <Alert title="Customers unavailable" message={error} /> : null}
      {success ? <Alert type="success" title="Customer updated" message={success} /> : null}

      <CustomerLookupGate status={status} />

      <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-4">
        <Metric label="Active customers" value={String(metrics.activeCustomers)} />
        <Metric label="POS linked" value={String(metrics.posLinked)} />
        <Metric label="Pending requests" value={String(metricSummary.pending_requests)} />
        <Metric label="Last activity" value={formatOptionalDate(metricSummary.last_customer_activity_at)} />
      </div>

      {formOpen ? (
        <Card>
          <div className="flex flex-col justify-between gap-3 sm:flex-row sm:items-start">
            <div>
              <CardTitle>{editingCustomer ? "Edit customer" : "New customer"}</CardTitle>
              <CardDescription>
                Local customer records stay in ManleAI. Square Appointments customer links are created during booking when required.
              </CardDescription>
            </div>
            <Badge value={editingCustomer?.archived_at ? "archived" : editingCustomer?.sync_status ?? "local_only"} />
          </div>
          <div className="mt-5 grid gap-4 md:grid-cols-2">
            <Field label="Name" htmlFor="customer-name">
              <input
                id="customer-name"
                className={inputClassName}
                value={form.name}
                onChange={(event) => setForm((current) => ({ ...current, name: event.target.value }))}
                disabled={busy !== "" || Boolean(editingCustomer?.archived_at)}
              />
            </Field>
            <Field label="Phone" htmlFor="customer-phone">
              <input
                id="customer-phone"
                className={inputClassName}
                value={form.phone}
                onChange={(event) => setForm((current) => ({ ...current, phone: event.target.value }))}
                disabled={busy !== "" || Boolean(editingCustomer?.archived_at)}
              />
            </Field>
            <Field label="Email" htmlFor="customer-email">
              <input
                id="customer-email"
                className={inputClassName}
                value={form.email}
                onChange={(event) => setForm((current) => ({ ...current, email: event.target.value }))}
                disabled={busy !== "" || Boolean(editingCustomer?.archived_at)}
              />
            </Field>
            <label className="flex items-center gap-3 rounded-md border border-line px-3 py-2 text-sm text-ink">
              <input
                type="checkbox"
                checked={form.active}
                onChange={(event) => setForm((current) => ({ ...current, active: event.target.checked }))}
                disabled={busy !== "" || Boolean(editingCustomer?.archived_at)}
              />
              Active customer
            </label>
            <div className="md:col-span-2">
              <Field label="Notes" htmlFor="customer-notes">
                <textarea
                  id="customer-notes"
                  className={`${inputClassName} min-h-24 py-2`}
                  value={form.notes}
                  onChange={(event) => setForm((current) => ({ ...current, notes: event.target.value }))}
                  disabled={busy !== "" || Boolean(editingCustomer?.archived_at)}
                />
              </Field>
            </div>
          </div>
          <div className="mt-5 flex flex-wrap justify-end gap-3">
            <Button
              type="button"
              variant="secondary"
              onClick={() => {
                setFormOpen(false);
                setEditingCustomer(null);
                setForm(emptyCustomerForm());
              }}
              disabled={busy !== ""}
            >
              Cancel
            </Button>
            <Button
              type="button"
              onClick={() => void saveCustomer()}
              disabled={busy !== "" || !form.name.trim() || (!form.phone.trim() && !form.email.trim()) || Boolean(editingCustomer?.archived_at)}
            >
              {busy === "save-customer" ? "Saving" : "Save customer"}
            </Button>
          </div>
        </Card>
      ) : null}

      <Card>
        <div className="flex flex-col justify-between gap-3 lg:flex-row lg:items-start">
          <div>
            <CardTitle>Square customer lookup</CardTitle>
            <CardDescription>
              Search Square Appointments by phone. Booking flows link or create the POS customer when needed.
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
            className={inputClassName}
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
          <div className="mt-3 text-sm leading-6 text-muted">Connect Square Appointments before using POS customer lookup.</div>
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
            <CardTitle>Customer records</CardTitle>
            <CardDescription>Canonical records are shown with calls, scheduling attempts, handoffs, and authority-confirmed appointments.</CardDescription>
          </div>
          <Badge value={metricSummary.total_known_customers > 0 ? "active" : "disabled"} />
        </div>

        {customers.length === 0 ? (
          <EmptyState
            icon={<Users className="h-5 w-5 text-muted" />}
            title="No customers yet"
            message="Create a customer record or wait for calls, authority-confirmed appointments, or pending requests to appear."
          />
        ) : (
          <>
            <CustomerPaginationControls
              className="mt-5"
              count={customers.length}
              limit={customerLimit}
              offset={customerOffset}
              hasMore={customerHasMore}
              busy={customerListBusy}
              onPrevious={goToPreviousCustomerPage}
              onNext={goToNextCustomerPage}
              onLimitChange={updateCustomerPageSize}
            />
            <div className="mt-3 hidden overflow-x-auto rounded-md border border-line lg:block">
              <table className="w-full min-w-[1040px] text-left text-sm">
                <thead className="bg-slate-50 text-xs uppercase text-muted">
                  <tr>
                    <th className="px-4 py-3">Customer</th>
                    <th className="px-4 py-3">Contact</th>
                    <th className="px-4 py-3">Status</th>
                    <th className="px-4 py-3">POS link</th>
                    <th className="px-4 py-3">Activity</th>
                    <th className="px-4 py-3">Last activity</th>
                    <th className="px-4 py-3">Actions</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-line bg-white">
                  {customers.map((item) => (
                    <tr key={item.id || item.key}>
                      <td className="px-4 py-3">
                        <div className="font-medium text-ink">{customerName(item)}</div>
                        {item.notes ? <div className="mt-1 max-w-xs truncate text-xs text-muted">{item.notes}</div> : null}
                      </td>
                      <td className="px-4 py-3">
                        <div className="text-ink">{item.phone || "No phone"}</div>
                        <div className="mt-1 text-xs text-muted">{item.email || "No email"}</div>
                      </td>
                      <td className="px-4 py-3">
                        <Badge value={customerStatus(item)} />
                      </td>
                      <td className="px-4 py-3">
                        <div className="flex flex-wrap gap-2">
                          <Badge value={item.pos_linked ? "synced" : item.sync_status || "unmapped"} />
                          <Badge value={item.source || "local"} />
                        </div>
                      </td>
                      <td className="px-4 py-3 text-muted">
                        {item.confirmed_appointments} confirmed / {item.pending_requests} pending / {item.call_count} calls
                      </td>
                      <td className="px-4 py-3">
                        <div className="font-medium text-ink">{formatDateTime(item.last_activity_at)}</div>
                        <div className="mt-1 text-xs text-muted">{formatActivitySource(item.last_activity_source)}</div>
                      </td>
                      <td className="px-4 py-3">
                        <CustomerActions
                          item={item}
                          busy={busy}
                          onCreateFromActivity={() => openCreateForm(activityToFormSeed(item))}
                          onEdit={() => openEditForm(item)}
                          onArchive={() => void archiveCustomer(item)}
                        />
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            <div className="mt-5 space-y-3 lg:hidden">
              {customers.map((item) => (
                <CustomerCard
                  key={item.id || item.key}
                  item={item}
                  busy={busy}
                  onCreateFromActivity={() => openCreateForm(activityToFormSeed(item))}
                  onEdit={() => openEditForm(item)}
                  onArchive={() => void archiveCustomer(item)}
                />
              ))}
            </div>
            <CustomerPaginationControls
              className="mt-4"
              count={customers.length}
              limit={customerLimit}
              offset={customerOffset}
              hasMore={customerHasMore}
              busy={customerListBusy}
              onPrevious={goToPreviousCustomerPage}
              onNext={goToNextCustomerPage}
              onLimitChange={updateCustomerPageSize}
            />
          </>
        )}
      </Card>
    </div>
  );
}

function CustomerPaginationControls({
  className = "",
  count,
  limit,
  offset,
  hasMore,
  busy,
  onPrevious,
  onNext,
  onLimitChange
}: {
  className?: string;
  count: number;
  limit: number;
  offset: number;
  hasMore: boolean;
  busy: boolean;
  onPrevious: () => void;
  onNext: () => void;
  onLimitChange: (limit: number) => void;
}) {
  const page = Math.floor(offset / limit) + 1;

  return (
    <div
      className={`flex flex-col gap-3 rounded-md border border-line bg-slate-50 px-3 py-3 sm:flex-row sm:items-center sm:justify-between ${className}`}
    >
      <div className="text-sm leading-6 text-muted">{customerRangeLabel(count, offset, hasMore)}</div>
      <div className="flex flex-col gap-3 sm:flex-row sm:items-center">
        <label className="flex items-center gap-2 text-sm text-muted">
          Rows per page
          <select
            className="h-9 rounded-md border border-line bg-white px-2 text-sm font-medium text-ink outline-none focus:border-brand disabled:text-slate-400"
            value={limit}
            onChange={(event) => onLimitChange(Number(event.target.value))}
            disabled={busy}
          >
            {customerPageSizeOptions.map((option) => (
              <option key={option} value={option}>
                {option}
              </option>
            ))}
          </select>
        </label>
        <div className="flex items-center gap-2">
          <Button type="button" variant="secondary" className="h-9 px-3" onClick={onPrevious} disabled={busy || offset === 0}>
            <ChevronLeft className="h-4 w-4" />
            Previous
          </Button>
          <span className="min-w-16 text-center text-sm font-semibold text-ink">Page {page}</span>
          <Button type="button" variant="secondary" className="h-9 px-3" onClick={onNext} disabled={busy || !hasMore}>
            Next
            <ChevronRight className="h-4 w-4" />
          </Button>
        </div>
      </div>
    </div>
  );
}

function customerRangeLabel(count: number, offset: number, hasMore: boolean) {
  if (count === 0) {
    return "No customer records";
  }
  const start = offset + 1;
  const end = offset + count;
  const total = hasMore ? `at least ${end + 1}` : String(end);
  return `Showing ${start}-${end} of ${total} customer records`;
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
              Booking can link ManleAI customers to Square Appointments customer records when a POS booking is created.
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
            Local customer records and activity still work, but POS lookup requires a connected Square Appointments account.
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
    return <div className="mt-4 rounded-md border border-line p-4 text-sm text-muted">No Square customer matched that phone number.</div>;
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

function Field({ label, htmlFor, children }: { label: string; htmlFor: string; children: ReactNode }) {
  return (
    <label className="block" htmlFor={htmlFor}>
      <span className="text-xs font-semibold uppercase tracking-wide text-muted">{label}</span>
      <div className="mt-1">{children}</div>
    </label>
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

function CustomerCard({
  item,
  busy,
  onCreateFromActivity,
  onEdit,
  onArchive
}: {
  item: CustomerRecord;
  busy: string;
  onCreateFromActivity: () => void;
  onEdit: () => void;
  onArchive: () => void;
}) {
  return (
    <div className="rounded-md border border-line p-4">
      <div className="flex items-start justify-between gap-3">
        <div>
          <div className="text-sm font-semibold text-ink">{customerName(item)}</div>
          <div className="mt-1 text-xs text-muted">{item.phone || item.email || "No contact detail"}</div>
        </div>
        <Badge value={customerStatus(item)} />
      </div>
      <div className="mt-3 flex flex-wrap gap-2">
        <Badge value={item.pos_linked ? "synced" : item.sync_status || "unmapped"} />
        <Badge value={item.source || "local"} />
      </div>
      <InfoGrid
        items={[
          ["Last activity", `${formatDateTime(item.last_activity_at)} - ${formatActivitySource(item.last_activity_source)}`],
          ["Confirmed appointments", String(item.confirmed_appointments)],
          ["Pending", String(item.pending_requests)],
          ["Calls", String(item.call_count)]
        ]}
      />
      <div className="mt-4">
        <CustomerActions item={item} busy={busy} onCreateFromActivity={onCreateFromActivity} onEdit={onEdit} onArchive={onArchive} />
      </div>
    </div>
  );
}

function CustomerActions({
  item,
  busy,
  onCreateFromActivity,
  onEdit,
  onArchive
}: {
  item: CustomerRecord;
  busy: string;
  onCreateFromActivity: () => void;
  onEdit: () => void;
  onArchive: () => void;
}) {
  if (!item.id) {
    return (
      <Button type="button" variant="secondary" onClick={onCreateFromActivity} disabled={busy !== ""}>
        <Plus className="h-4 w-4" />
        Create record
      </Button>
    );
  }
  return (
    <div className="flex flex-wrap gap-2">
      <Button type="button" variant="secondary" onClick={onEdit} disabled={busy !== ""}>
        <Pencil className="h-4 w-4" />
        Edit
      </Button>
      <Button
        type="button"
        variant="danger"
        onClick={onArchive}
        disabled={busy !== "" || Boolean(item.archived_at)}
      >
        <Archive className="h-4 w-4" />
        Archive
      </Button>
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

function emptyCustomerForm(): CustomerFormState {
  return {
    name: "",
    phone: "",
    email: "",
    notes: "",
    active: true
  };
}

function customerToForm(customer: CustomerRecord): CustomerFormState {
  return {
    name: customer.name ?? "",
    phone: customer.phone ?? "",
    email: customer.email ?? "",
    notes: customer.notes ?? "",
    active: customer.active
  };
}

function activityToFormSeed(customer: CustomerRecord): Partial<CustomerFormState> {
  return {
    name: customer.name ?? "",
    phone: customer.phone ?? "",
    email: customer.email ?? "",
    active: true
  };
}

function customerPayload(form: CustomerFormState) {
  return {
    name: form.name.trim(),
    phone: form.phone.trim(),
    email: form.email.trim(),
    notes: form.notes.trim(),
    active: form.active
  };
}

function upsertCustomer(items: CustomerRecord[], next: CustomerRecord) {
  const key = next.id || next.key;
  const exists = items.some((item) => (item.id || item.key) === key);
  const updated = exists ? items.map((item) => ((item.id || item.key) === key ? next : item)) : [next, ...items];
  return updated.sort((a, b) => {
    if (Boolean(a.archived_at) !== Boolean(b.archived_at)) return a.archived_at ? 1 : -1;
    if (a.active !== b.active) return a.active ? -1 : 1;
    return new Date(b.last_activity_at).getTime() - new Date(a.last_activity_at).getTime();
  });
}

function summarizeCustomers(customers: CustomerRecord[], summary: CustomerSummary | null): CustomerSummary {
  if (summary) return summary;
  return customers.reduce<CustomerSummary>(
    (acc, item) => {
      acc.total_known_customers += 1;
      if (item.id && item.active && !item.archived_at) acc.active_customers = (acc.active_customers ?? 0) + 1;
      if (item.pos_linked) acc.pos_linked_customers = (acc.pos_linked_customers ?? 0) + 1;
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
      active_customers: 0,
      pos_linked_customers: 0,
      confirmed_appointments: 0,
      pending_requests: 0,
      customers_with_calls: 0
    }
  );
}

function customerMetrics(customers: CustomerRecord[], _summary: CustomerSummary) {
  return {
    activeCustomers:
      _summary.active_customers ?? customers.filter((item) => item.id && item.active && !item.archived_at).length,
    posLinked: _summary.pos_linked_customers ?? customers.filter((item) => item.pos_linked).length
  };
}

function isSquareConnected(connection?: POSConnection) {
  return Boolean(connection?.id) && connection?.status !== "not_connected";
}

function customerStatus(item: CustomerRecord) {
  if (item.archived_at) return "archived";
  if (!item.id) return "unmapped";
  if (!item.active) return "disabled";
  return "active";
}

function customerName(item: CustomerRecord) {
  return item.name || item.phone || item.email || "Unknown customer";
}

function formatActivitySource(value: string) {
  if (value === "booking_attempt") return "Pending request";
  if (value === "appointment") return "Confirmed appointment";
  if (value === "call") return "Call";
  if (value === "handoff") return "Owner handoff";
  return "Customer record";
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

const inputClassName =
  "h-10 w-full rounded-md border border-line bg-white px-3 text-sm text-ink outline-none transition focus:border-brand focus:ring-2 focus:ring-teal-100 disabled:bg-slate-100 disabled:text-slate-500";
