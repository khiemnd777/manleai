"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { ClipboardCheck, RefreshCcw } from "lucide-react";
import { Alert } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardDescription, CardTitle } from "@/components/ui/card";
import { Dialog } from "@/components/ui/dialog";
import { Skeleton } from "@/components/ui/skeleton";
import { CustomerNotificationStatus } from "@/features/dashboard/customer-notification-status";
import { RequestError } from "@/lib/api/client";
import {
  getSchedulingRequest,
  listSchedulingRequests,
  newSchedulingRequestActionKey,
  updateSchedulingRequest
} from "@/lib/api/scheduling-requests";
import type {
  SchedulingRequest,
  SchedulingRequestStatus,
  SchedulingRequestsResponse,
  StaffSelectionMode,
  UpdateSchedulingRequestInput
} from "@/types/api";

type OwnerReviewRequestsProps = {
  salonID: string;
  timezone: string;
};

type StatusFilter = "all" | SchedulingRequestStatus;

type CountSummary = {
  count: number;
  hasMore: boolean;
};

type ActionKeyState = {
  fingerprint: string;
  key: string;
};

const pageSize = 10;
const countProbeLimit = 100;
const requestStatuses: SchedulingRequestStatus[] = ["pending", "contacted", "resolved", "dismissed"];
const statusFilters: Array<{ value: StatusFilter; label: string }> = [
  { value: "all", label: "All" },
  { value: "pending", label: "Pending" },
  { value: "contacted", label: "Contacted" },
  { value: "resolved", label: "Resolved" },
  { value: "dismissed", label: "Dismissed" }
];

export function OwnerReviewRequests({ salonID, timezone }: OwnerReviewRequestsProps) {
  const listRequestIDRef = useRef(0);
  const detailRequestIDRef = useRef(0);
  const actionKeyRef = useRef<ActionKeyState | null>(null);
  const [statusFilter, setStatusFilter] = useState<StatusFilter>("pending");
  const [rows, setRows] = useState<SchedulingRequest[]>([]);
  const [offset, setOffset] = useState(0);
  const [hasMore, setHasMore] = useState(false);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [counts, setCounts] = useState<Partial<Record<StatusFilter, CountSummary>>>({});
  const [countsLoading, setCountsLoading] = useState(true);
  const [selectedRequest, setSelectedRequest] = useState<SchedulingRequest | null>(null);
  const [detailLoading, setDetailLoading] = useState(false);
  const [detailError, setDetailError] = useState("");
  const [resolutionReason, setResolutionReason] = useState("");
  const [note, setNote] = useState("");
  const [saving, setSaving] = useState(false);
  const [actionError, setActionError] = useState("");
  const [successMessage, setSuccessMessage] = useState("");

  const loadCounts = useCallback(async () => {
    setCountsLoading(true);
    const responses = await Promise.allSettled(
      requestStatuses.map((status) =>
        listSchedulingRequests(salonID, { status, limit: countProbeLimit, offset: 0 })
      )
    );

    const next: Partial<Record<StatusFilter, CountSummary>> = {};
    let allCount = 0;
    let allHasMore = false;
    responses.forEach((result, index) => {
      if (result.status !== "fulfilled") return;
      const status = requestStatuses[index];
      const summary = countSummary(result.value);
      next[status] = summary;
      allCount += summary.count;
      allHasMore = allHasMore || summary.hasMore;
    });
    if (Object.keys(next).length === requestStatuses.length) {
      next.all = { count: allCount, hasMore: allHasMore };
    }
    setCounts(next);
    setCountsLoading(false);
  }, [salonID]);

  const loadPage = useCallback(async () => {
    const requestID = ++listRequestIDRef.current;
    setLoading(true);
    setError("");
    try {
      let response = await listSchedulingRequests(salonID, {
        status: statusFilter === "all" ? undefined : statusFilter,
        limit: pageSize,
        offset
      });
      if (requestID !== listRequestIDRef.current) return;

      if (response.scheduling_requests.length === 0 && offset > 0) {
        const previousOffset = Math.max(0, offset - pageSize);
        response = await listSchedulingRequests(salonID, {
          status: statusFilter === "all" ? undefined : statusFilter,
          limit: pageSize,
          offset: previousOffset
        });
        if (requestID !== listRequestIDRef.current) return;
        setOffset(response.offset ?? previousOffset);
      }

      setRows(response.scheduling_requests);
      setHasMore(Boolean(response.has_more));
    } catch (err) {
      if (requestID !== listRequestIDRef.current) return;
      setError(err instanceof Error ? err.message : "Could not load owner review requests.");
    } finally {
      if (requestID === listRequestIDRef.current) setLoading(false);
    }
  }, [offset, salonID, statusFilter]);

  useEffect(() => {
    void loadPage();
  }, [loadPage]);

  useEffect(() => {
    void loadCounts();
  }, [loadCounts]);

  useEffect(
    () => () => {
      listRequestIDRef.current += 1;
      detailRequestIDRef.current += 1;
    },
    []
  );

  function selectStatus(value: StatusFilter) {
    if (value === statusFilter) return;
    setStatusFilter(value);
    setOffset(0);
    setRows([]);
    setHasMore(false);
    setLoading(true);
    setError("");
    setSuccessMessage("");
  }

  async function openReview(request: SchedulingRequest) {
    setSelectedRequest(request);
    setResolutionReason(request.resolution_reason ?? "");
    setNote("");
    setDetailError("");
    setActionError("");
    actionKeyRef.current = null;
    await refreshDetail(request.id);
  }

  async function refreshDetail(requestID: string) {
    const requestIDSequence = ++detailRequestIDRef.current;
    setDetailLoading(true);
    setDetailError("");
    try {
      const response = await getSchedulingRequest(salonID, requestID);
      if (requestIDSequence !== detailRequestIDRef.current) return;
      setSelectedRequest(response.scheduling_request);
      setResolutionReason(response.scheduling_request.resolution_reason ?? "");
    } catch (err) {
      if (requestIDSequence !== detailRequestIDRef.current) return;
      setDetailError(err instanceof Error ? err.message : "Could not load this owner review request.");
    } finally {
      if (requestIDSequence === detailRequestIDRef.current) setDetailLoading(false);
    }
  }

  function closeReview() {
    if (saving) return;
    detailRequestIDRef.current += 1;
    setSelectedRequest(null);
    setDetailError("");
    setActionError("");
    setResolutionReason("");
    setNote("");
    actionKeyRef.current = null;
  }

  async function changeStatus(status: SchedulingRequestStatus) {
    if (!selectedRequest || saving || detailLoading || detailError) return;
    const inputWithoutKey = {
      expected_version: selectedRequest.version,
      status,
      ...(status !== "contacted" && resolutionReason.trim()
        ? { resolution_reason: resolutionReason.trim() }
        : {}),
      ...(note.trim() ? { note: note.trim() } : {})
    };
    const fingerprint = JSON.stringify(inputWithoutKey);
    if (!actionKeyRef.current || actionKeyRef.current.fingerprint !== fingerprint) {
      actionKeyRef.current = { fingerprint, key: newSchedulingRequestActionKey() };
    }
    const input: UpdateSchedulingRequestInput = {
      action_key: actionKeyRef.current.key,
      ...inputWithoutKey
    };

    setSaving(true);
    setActionError("");
    try {
      const response = await updateSchedulingRequest(salonID, selectedRequest.id, input);
      setSelectedRequest(response.scheduling_request);
      actionKeyRef.current = null;
      setSuccessMessage(statusSuccessMessage(status));
      closeReviewAfterSuccess();
      await Promise.all([loadPage(), loadCounts()]);
    } catch (err) {
      if (
        err instanceof RequestError &&
        err.status === 409 &&
        err.code === "SCHEDULING_REQUEST_VERSION_CONFLICT"
      ) {
        setActionError(
          "This request changed after you opened it. The latest version has been reloaded; review it before trying again."
        );
        actionKeyRef.current = null;
        await Promise.all([refreshDetail(selectedRequest.id), loadPage(), loadCounts()]);
      } else {
        setActionError(err instanceof Error ? err.message : "Could not update this owner review request.");
      }
    } finally {
      setSaving(false);
    }
  }

  function closeReviewAfterSuccess() {
    detailRequestIDRef.current += 1;
    setSelectedRequest(null);
    setDetailError("");
    setActionError("");
    setResolutionReason("");
    setNote("");
  }

  const visibleCount = counts[statusFilter];
  const canGoPrevious = offset > 0 && !loading && !saving;
  const canGoNext = hasMore && !loading && !saving;

  return (
    <Card>
      <div className="flex flex-col justify-between gap-3 sm:flex-row sm:items-start">
        <div>
          <CardTitle>Owner review requests</CardTitle>
          <CardDescription>
            AI scheduling requests are recorded for owner review, not confirmed. A selected time is not reserved, and this screen does not claim that an owner notification was delivered.
          </CardDescription>
        </div>
        <Badge value={counts.pending?.count ? "needs_review" : "disabled"} />
      </div>

      {successMessage ? (
        <div className="mt-4" role="status" aria-live="polite">
          <Alert type="success" title="Owner review updated" message={successMessage} />
        </div>
      ) : null}

      <div className="mt-5 grid grid-cols-2 gap-2 sm:flex sm:flex-wrap" aria-label="Filter owner review requests">
        {statusFilters.map((filter) => {
          const summary = counts[filter.value];
          return (
            <Button
              key={filter.value}
              type="button"
              variant={statusFilter === filter.value ? "primary" : "secondary"}
              className="h-9 px-3"
              aria-pressed={statusFilter === filter.value}
              disabled={loading && statusFilter !== filter.value}
              onClick={() => selectStatus(filter.value)}
            >
              {filter.label}
              <span className="tabular-nums">
                {countsLoading && !summary ? "…" : summary ? countLabel(summary) : "—"}
              </span>
            </Button>
          );
        })}
        <Button
          type="button"
          variant="ghost"
          className="h-9 px-3"
          disabled={loading || countsLoading}
          onClick={() => void Promise.all([loadPage(), loadCounts()])}
        >
          <RefreshCcw className="h-4 w-4" />
          Refresh
        </Button>
      </div>

      {loading && rows.length === 0 ? (
        <div className="mt-5 space-y-3" aria-label="Loading owner review requests">
          <Skeleton className="h-16" />
          <Skeleton className="h-16" />
          <Skeleton className="h-16" />
        </div>
      ) : null}

      {error ? (
        <div className="mt-5 space-y-3" role="alert">
          <Alert title="Owner review requests unavailable" message={error} />
          <Button type="button" variant="secondary" disabled={loading} onClick={() => void loadPage()}>
            <RefreshCcw className="h-4 w-4" />
            Retry owner review queue
          </Button>
        </div>
      ) : null}

      {!loading && !error && rows.length === 0 ? (
        <div className="mt-5 rounded-md border border-dashed border-line bg-slate-50 p-8 text-center">
          <ClipboardCheck className="mx-auto h-6 w-6 text-muted" />
          <div className="mt-3 text-sm font-semibold text-ink">No {filterEmptyLabel(statusFilter)} requests</div>
          <div className="mt-1 text-sm leading-6 text-muted">
            AI scheduling requests will appear here after they are recorded for review.
          </div>
        </div>
      ) : null}

      {rows.length > 0 ? (
        <>
          <div className="mt-5 hidden overflow-x-auto rounded-md border border-line lg:block">
            <table className="w-full min-w-[980px] text-left text-sm">
              <thead className="bg-slate-50 text-xs uppercase text-muted">
                <tr>
                  <th className="px-4 py-3">Requested time</th>
                  <th className="px-4 py-3">Customer</th>
                  <th className="px-4 py-3">Type</th>
                  <th className="px-4 py-3">Services</th>
                  <th className="px-4 py-3">Status</th>
                  <th className="px-4 py-3">Action</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-line bg-white">
                {rows.map((request) => (
                  <tr key={request.id}>
                    <td className="px-4 py-3">
                      <div className="font-medium text-ink">{requestedDateLabel(request, timezone)}</div>
                      <div className="mt-1 text-xs text-muted">{requestedTimeLabel(request, timezone)}</div>
                    </td>
                    <td className="px-4 py-3">
                      <div className="font-medium text-ink">{request.redacted ? "Personal details removed" : request.customer_name}</div>
                      <div className="mt-1 text-xs text-muted">{request.redacted ? "Retention policy applied" : request.customer_phone}</div>
                    </td>
                    <td className="px-4 py-3">
                      <div className="flex flex-wrap gap-2">
                        <Badge value="Owner request" />
                        {request.redacted ? <Badge value="Redacted" /> : null}
                        <Badge value={request.operation_type} />
                        <Badge value={targetAuthorityLabel(request.target_scheduling_authority)} />
                      </div>
                    </td>
                    <td className="max-w-sm px-4 py-3 text-muted">{requestServicesLabel(request)}</td>
                    <td className="px-4 py-3"><Badge value={request.status} /></td>
                    <td className="px-4 py-3">
                      <Button type="button" variant="secondary" disabled={saving} onClick={() => void openReview(request)}>
                        Review
                      </Button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>

          <div className="mt-5 space-y-3 lg:hidden">
            {rows.map((request) => (
              <div key={request.id} className="rounded-md border border-line bg-white p-4">
                <div className="flex items-start justify-between gap-3">
                  <div>
                    <div className="text-sm font-semibold text-ink">{request.redacted ? "Personal details removed" : request.customer_name}</div>
                    <div className="mt-1 text-xs text-muted">{request.redacted ? "Retention policy applied" : request.customer_phone}</div>
                  </div>
                  <div className="flex flex-wrap justify-end gap-2">
                    <Badge value="Owner request" />
                    {request.redacted ? <Badge value="Redacted" /> : null}
                    <Badge value={request.status} />
                  </div>
                </div>
                <dl className="mt-4 grid grid-cols-[7rem_1fr] gap-x-3 gap-y-2 text-sm">
                  <dt className="text-muted">Requested</dt>
                  <dd className="text-ink">{requestedDateTimeLabel(request, timezone)}</dd>
                  <dt className="text-muted">Type</dt>
                  <dd className="flex flex-wrap gap-2">
                    <Badge value={request.operation_type} />
                    <Badge value={targetAuthorityLabel(request.target_scheduling_authority)} />
                  </dd>
                  <dt className="text-muted">Services</dt>
                  <dd className="text-ink">{requestServicesLabel(request)}</dd>
                </dl>
                <Button
                  type="button"
                  variant="secondary"
                  className="mt-4 w-full"
                  disabled={saving}
                  onClick={() => void openReview(request)}
                >
                  Review request
                </Button>
              </div>
            ))}
          </div>

          <div className="mt-4 flex flex-col justify-between gap-3 border-t border-line pt-4 sm:flex-row sm:items-center">
            <div className="text-sm text-muted">
              Showing {offset + 1}–{offset + rows.length}
              {visibleCount ? ` of ${countLabel(visibleCount)}` : ""} owner review requests.
            </div>
            <div className="flex gap-2">
              <Button
                type="button"
                variant="secondary"
                disabled={!canGoPrevious}
                onClick={() => setOffset((current) => Math.max(0, current - pageSize))}
              >
                Previous
              </Button>
              <Button
                type="button"
                variant="secondary"
                disabled={!canGoNext}
                onClick={() => setOffset((current) => current + pageSize)}
              >
                Next
              </Button>
            </div>
          </div>
        </>
      ) : null}

      <Dialog
        open={Boolean(selectedRequest)}
        title="Review owner request"
        description="This request is recorded for owner review. Updating its review status does not confirm, reschedule, or cancel an appointment."
        onClose={closeReview}
        closeDisabled={saving}
        className="max-w-2xl"
      >
        {selectedRequest ? (
          <OwnerRequestReviewDialog
            request={selectedRequest}
            timezone={timezone}
            detailLoading={detailLoading}
            detailError={detailError}
            actionError={actionError}
            resolutionReason={resolutionReason}
            note={note}
            saving={saving}
            onResolutionReasonChange={setResolutionReason}
            onNoteChange={setNote}
            onRetry={() => void refreshDetail(selectedRequest.id)}
            onChangeStatus={(status) => void changeStatus(status)}
          />
        ) : null}
      </Dialog>
    </Card>
  );
}

function OwnerRequestReviewDialog({
  request,
  timezone,
  detailLoading,
  detailError,
  actionError,
  resolutionReason,
  note,
  saving,
  onResolutionReasonChange,
  onNoteChange,
  onRetry,
  onChangeStatus
}: {
  request: SchedulingRequest;
  timezone: string;
  detailLoading: boolean;
  detailError: string;
  actionError: string;
  resolutionReason: string;
  note: string;
  saving: boolean;
  onResolutionReasonChange: (value: string) => void;
  onNoteChange: (value: string) => void;
  onRetry: () => void;
  onChangeStatus: (status: SchedulingRequestStatus) => void;
}) {
  const terminal = request.status === "resolved" || request.status === "dismissed";
  const actionsDisabled = saving || detailLoading || Boolean(detailError) || terminal || request.redacted;
  const terminalActionDisabled = actionsDisabled || resolutionReason.trim().length === 0;

  return (
    <div className="space-y-5">
      {detailLoading ? <Skeleton className="h-10" /> : null}
      {detailError ? (
        <div className="space-y-3" role="alert">
          <Alert title="Request details unavailable" message={detailError} />
          <Button type="button" variant="secondary" disabled={detailLoading} onClick={onRetry}>
            <RefreshCcw className="h-4 w-4" />
            Retry details
          </Button>
        </div>
      ) : null}
      {actionError ? <Alert title="Review status not updated" message={actionError} /> : null}

      {request.redacted ? (
        <Alert
          title="Personal details removed"
          message="The retention policy removed customer-entered details. Status, authority, service, timestamp, and audit evidence remain available; this record cannot be changed."
        />
      ) : null}

      <Alert
        title="Requested time is not reserved"
        message={`This request targets ${targetAuthorityLabel(request.target_scheduling_authority)}. Reviewing or updating this request does not confirm, reschedule, or cancel an appointment.`}
      />

      <div className="flex flex-wrap items-center gap-2">
        <Badge value="Owner request" />
        {request.redacted ? <Badge value="Redacted" /> : null}
        <Badge value={request.operation_type} />
        <Badge value={request.status} />
        <span className="text-xs text-muted">Version {request.version}</span>
      </div>

      <dl className="grid gap-x-4 gap-y-3 rounded-md border border-line bg-slate-50 p-4 text-sm sm:grid-cols-[9rem_1fr]">
        <dt className="text-muted">Customer</dt>
        <dd className="font-medium text-ink">{request.redacted ? "Removed by retention policy" : request.customer_name}</dd>
        <dt className="text-muted">Phone</dt>
        <dd className="text-ink">{request.redacted ? "Removed by retention policy" : request.customer_phone}</dd>
        <dt className="text-muted">Email</dt>
        <dd className="break-all text-ink">{request.redacted ? "Removed by retention policy" : request.customer_email || "Not provided"}</dd>
        <dt className="text-muted">Requested time</dt>
        <dd className="text-ink">{requestedDateTimeLabel(request, timezone)}</dd>
        <dt className="text-muted">Party size</dt>
        <dd className="text-ink">{request.party_size || 1}</dd>
        <dt className="text-muted">Source</dt>
        <dd className="text-ink">{request.source.replaceAll("_", " ")}</dd>
        <dt className="text-muted">Scheduling target</dt>
        <dd className="text-ink">{targetAuthorityLabel(request.target_scheduling_authority)}</dd>
        <dt className="text-muted">Services and staff</dt>
        <dd className="space-y-2 text-ink">
          {orderedSegments(request).length > 0 ? (
            orderedSegments(request).map((segment, index) => (
              <div key={segment.id ?? `${segment.sort_order}-${segment.service_id ?? index}`}>
                <span className="font-medium">{segment.service_name}</span>
                {segment.quantity && segment.quantity > 1 ? ` × ${segment.quantity}` : ""}
                <span className="text-muted"> · {segment.staff_name || staffModeLabel(segment.staff_selection_mode)}</span>
                {segment.guest_reference ? <span className="text-muted"> · {segment.guest_reference}</span> : null}
              </div>
            ))
          ) : (
            "Not provided"
          )}
        </dd>
        {request.target_description || request.target_appointment_id ? (
          <>
            <dt className="text-muted">Existing appointment</dt>
            <dd className="text-ink">
              {request.redacted
                ? request.target_appointment_id || "Details removed by retention policy"
                : request.target_description || request.target_appointment_id}
            </dd>
          </>
        ) : null}
        <dt className="text-muted">Request notes</dt>
        <dd className="whitespace-pre-wrap text-ink">{request.redacted ? "Removed by retention policy" : request.notes || "None"}</dd>
        {request.resolution_reason ? (
          <>
            <dt className="text-muted">Resolution reason</dt>
            <dd className="whitespace-pre-wrap text-ink">{request.resolution_reason}</dd>
          </>
        ) : null}
        <dt className="text-muted">Recorded</dt>
        <dd className="text-ink">{formatTimestamp(request.created_at, timezone)}</dd>
        {request.contacted_at ? (
          <>
            <dt className="text-muted">Contacted</dt>
            <dd className="text-ink">{formatTimestamp(request.contacted_at, timezone)}</dd>
          </>
        ) : null}
        {request.resolved_at ? (
          <>
            <dt className="text-muted">Resolved</dt>
            <dd className="text-ink">{formatTimestamp(request.resolved_at, timezone)}</dd>
          </>
        ) : null}
        {request.dismissed_at ? (
          <>
            <dt className="text-muted">Dismissed</dt>
            <dd className="text-ink">{formatTimestamp(request.dismissed_at, timezone)}</dd>
          </>
        ) : null}
      </dl>

      <CustomerNotificationStatus
        salonID={request.salon_id}
        requestID={request.id}
        customerPhone={request.redacted ? "" : request.customer_phone}
        redacted={Boolean(request.redacted)}
      />

      {!terminal ? (
        <div className="space-y-4 border-t border-line pt-5">
          <div>
            <label htmlFor="owner-review-resolution-reason" className="text-sm font-semibold text-ink">
              Resolution reason <span className="font-normal text-muted">(required to resolve or dismiss)</span>
            </label>
            <input
              id="owner-review-resolution-reason"
              value={resolutionReason}
              onChange={(event) => onResolutionReasonChange(event.target.value)}
              disabled={actionsDisabled}
              maxLength={500}
              placeholder="How the owner handled this request"
              className="mt-2 h-10 w-full rounded-md border border-line bg-white px-3 text-sm text-ink outline-none focus:border-brand"
            />
          </div>
          <div>
            <label htmlFor="owner-review-note" className="text-sm font-semibold text-ink">
              Review note <span className="font-normal text-muted">(optional)</span>
            </label>
            <textarea
              id="owner-review-note"
              value={note}
              onChange={(event) => onNoteChange(event.target.value)}
              disabled={actionsDisabled}
              maxLength={2000}
              placeholder="Add context for the review history"
              rows={3}
              className="mt-2 w-full rounded-md border border-line bg-white px-3 py-2 text-sm text-ink outline-none focus:border-brand"
            />
          </div>
          <div className="flex flex-col gap-2 sm:flex-row sm:flex-wrap">
            {request.status === "pending" ? (
              <Button type="button" variant="secondary" disabled={actionsDisabled} onClick={() => onChangeStatus("contacted")}>
                {saving ? "Updating..." : "Mark contacted"}
              </Button>
            ) : null}
            <Button type="button" disabled={terminalActionDisabled} onClick={() => onChangeStatus("resolved")}>
              {saving ? "Updating..." : "Resolve"}
            </Button>
            <Button type="button" variant="danger" disabled={terminalActionDisabled} onClick={() => onChangeStatus("dismissed")}>
              {saving ? "Updating..." : "Dismiss"}
            </Button>
          </div>
          <p className="text-xs leading-5 text-muted">
            These actions update the owner-review queue only. No appointment is confirmed by this screen.
          </p>
        </div>
      ) : (
        <div className="rounded-md border border-line bg-slate-50 p-3 text-sm text-muted">
          This request is closed. Its review history remains visible, and no appointment was confirmed by this status update.
        </div>
      )}
    </div>
  );
}

function targetAuthorityLabel(authority: SchedulingRequest["target_scheduling_authority"]) {
  if (authority === "external_provider") return "Connected scheduling provider";
  if (authority === "manleai_calendar") return "ManleAI Calendar";
  if (authority === "owner_manual") return "Owner-managed request";
  return "Legacy owner request";
}

function countSummary(response: SchedulingRequestsResponse): CountSummary {
  if (typeof response.total === "number") return { count: response.total, hasMore: false };
  return { count: response.scheduling_requests.length, hasMore: Boolean(response.has_more) };
}

function countLabel(summary: CountSummary) {
  return `${summary.count}${summary.hasMore ? "+" : ""}`;
}

function filterEmptyLabel(filter: StatusFilter) {
  return filter === "all" ? "owner review" : filter;
}

function orderedSegments(request: SchedulingRequest) {
  return [...(request.segments ?? [])].sort((left, right) => left.sort_order - right.sort_order);
}

function requestServicesLabel(request: SchedulingRequest) {
  const segments = orderedSegments(request);
  if (segments.length === 0) return "Not provided";
  return segments
    .map((segment) => `${segment.service_name}${segment.quantity && segment.quantity > 1 ? ` × ${segment.quantity}` : ""}`)
    .join(", ");
}

export function staffModeLabel(mode?: StaffSelectionMode) {
  if (mode === "anyone") return "Any available technician";
  if (mode === "specific") return "Specific technician requested";
  return "No staff preference";
}

function requestedDateLabel(request: SchedulingRequest, fallbackTimezone: string) {
  if (!request.requested_start_time) return "Time to be reviewed";
  const requestTimezone = displayTimezone(request.requested_timezone, fallbackTimezone);
  return new Intl.DateTimeFormat("en-US", {
    timeZone: requestTimezone,
    weekday: "short",
    month: "short",
    day: "numeric",
    year: "numeric"
  }).format(new Date(request.requested_start_time));
}

function requestedTimeLabel(request: SchedulingRequest, fallbackTimezone: string) {
  if (!request.requested_start_time) return "No requested time";
  const requestTimezone = displayTimezone(request.requested_timezone, fallbackTimezone);
  const start = formatTime(request.requested_start_time, requestTimezone);
  const end = request.requested_end_time ? formatTime(request.requested_end_time, requestTimezone) : "";
  return end ? `${start}–${end}` : start;
}

function requestedDateTimeLabel(request: SchedulingRequest, fallbackTimezone: string) {
  if (!request.requested_start_time) return "No requested time";
  return `${requestedDateLabel(request, fallbackTimezone)}, ${requestedTimeLabel(request, fallbackTimezone)}`;
}

function formatTime(value: string, timezone: string) {
  return new Intl.DateTimeFormat("en-US", {
    timeZone: timezone,
    hour: "numeric",
    minute: "2-digit"
  }).format(new Date(value));
}

function formatTimestamp(value: string, timezone: string) {
  return new Intl.DateTimeFormat("en-US", {
    timeZone: displayTimezone(undefined, timezone),
    month: "short",
    day: "numeric",
    year: "numeric",
    hour: "numeric",
    minute: "2-digit"
  }).format(new Date(value));
}

function displayTimezone(preferred: string | undefined, fallback: string) {
  for (const timezone of [preferred, fallback, "UTC"]) {
    if (!timezone) continue;
    try {
      new Intl.DateTimeFormat("en-US", { timeZone: timezone }).format();
      return timezone;
    } catch {
      // Try the salon timezone, then the stable UTC presentation fallback.
    }
  }
  return "UTC";
}

function statusSuccessMessage(status: SchedulingRequestStatus) {
  if (status === "contacted") {
    return "The request is marked contacted and remains an owner-review request. No appointment was confirmed.";
  }
  if (status === "resolved") {
    return "The owner-review request is resolved. This status update did not confirm an appointment.";
  }
  if (status === "dismissed") {
    return "The owner-review request is dismissed. This status update did not confirm an appointment.";
  }
  return "The owner-review request was updated. No appointment was confirmed.";
}
