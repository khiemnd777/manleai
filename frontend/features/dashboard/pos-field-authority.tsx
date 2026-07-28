import Link from "next/link";
import { AlertTriangle, ExternalLink } from "lucide-react";
import type { POSFieldAuthority } from "@/types/api";

export function FieldAuthorityBadge({ authority }: { authority?: POSFieldAuthority }) {
  const providerManaged = authority?.operational_source === "provider";
  const label = authorityLabel(authority);
  return (
    <span
      className={`inline-flex items-center rounded-full px-2.5 py-1 text-xs font-semibold ring-1 ${
        !authority
          ? "bg-amber-50 text-amber-800 ring-amber-200"
          : providerManaged
            ? "bg-teal-50 text-teal-800 ring-teal-200"
            : "bg-blue-50 text-blue-800 ring-blue-200"
      }`}
    >
      {label}
    </span>
  );
}

export function FieldAuthorityPanel({
  authority,
  recordKind,
  syncStatus,
  lastSyncedAt,
  syncError,
  showProviderSetupAction = true
}: {
  authority?: POSFieldAuthority;
  recordKind: "service" | "staff";
  syncStatus: string;
  lastSyncedAt?: string;
  syncError?: string;
  showProviderSetupAction?: boolean;
}) {
  const providerManaged = authority?.operational_source === "provider";
  const providerReadOnly = authority?.operational_write_mode === "provider_read_only";
  const providerSync = authority?.operational_write_mode === "provider_sync";
  const fields =
    recordKind === "service"
      ? "Name, standard description, duration, price, and active status"
      : "Name, phone, email, and active status";
  const description = !authority
    ? "Field ownership could not be verified. Operational editing is disabled until the record is refreshed."
    : providerManaged && providerReadOnly
      ? `${fields} are imported from ${authorityLabel(authority)}. Edit them in the provider, then sync.`
      : providerSync
        ? `${fields} can be edited here and will be synchronized through the active provider adapter.`
        : `${fields} are stored in ManleAI only. This record is not booking-ready until it has a valid active-provider link.`;

  return (
    <div
      className={`mt-5 rounded-md border p-4 ${
        !authority || syncStatus === "sync_failed"
          ? "border-amber-200 bg-amber-50"
          : providerManaged
            ? "border-teal-200 bg-teal-50/60"
            : "border-blue-200 bg-blue-50/60"
      }`}
    >
      <div className="flex flex-col justify-between gap-3 sm:flex-row sm:items-start">
        <div className="min-w-0">
          <div className="flex flex-wrap items-center gap-2">
            {!authority || syncStatus === "sync_failed" ? <AlertTriangle className="h-4 w-4 text-amber-700" /> : null}
            <span className="text-sm font-semibold text-ink">Managed in</span>
            <FieldAuthorityBadge authority={authority} />
          </div>
          <p className="mt-2 text-xs leading-5 text-muted">{description}</p>
          {lastSyncedAt ? <p className="mt-1 text-xs text-muted">Last synced {formatTimestamp(lastSyncedAt)}</p> : null}
          {syncError ? <p className="mt-2 text-xs font-medium leading-5 text-red-700">{syncError}</p> : null}
        </div>
        {showProviderSetupAction && (providerManaged || !authority || syncStatus === "sync_failed") ? (
          <Link
            className="inline-flex h-9 shrink-0 items-center justify-center gap-2 rounded-md border border-line bg-white px-3 text-xs font-semibold text-ink hover:bg-slate-50"
            href="/dashboard/integrations"
          >
            Open Integrations
            <ExternalLink className="h-3.5 w-3.5" />
          </Link>
        ) : null}
      </div>
    </div>
  );
}

export function operationalFieldsEditable(authority?: POSFieldAuthority) {
  return authority?.operational_write_mode === "local" || authority?.operational_write_mode === "provider_sync";
}

export function providerManagedReadOnly(authority?: POSFieldAuthority) {
  return authority?.operational_source === "provider" && authority.operational_write_mode === "provider_read_only";
}

export function authorityLabel(authority?: POSFieldAuthority) {
  if (!authority) return "Authority unavailable";
  if (authority.operational_source === "provider") return authority.provider_label || authority.provider || "Active POS provider";
  if (authority.operational_write_mode === "provider_sync" && authority.provider_label) return `ManleAI · syncs to ${authority.provider_label}`;
  return "ManleAI · local";
}

function formatTimestamp(value: string) {
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString();
}
