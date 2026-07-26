import type { ConfigurationImportResponse } from "@/types/api";
import { Badge } from "@/components/ui/badge";

type ImportSummary = ConfigurationImportResponse["summary"] | null | undefined;
type ImportIssues = ConfigurationImportResponse["warnings"] | null | undefined;

export function ImportScopePreview({ preview }: { preview: ConfigurationImportResponse }) {
  const included = Array.isArray(preview.included_sections) ? preview.included_sections : preview.summary.map((item) => item.section);
  const excluded = Array.isArray(preview.excluded_data) ? preview.excluded_data : [];
  return (
    <div className="space-y-3 rounded-md border border-line bg-slate-50 p-4">
      <div className="flex flex-col justify-between gap-2 sm:flex-row sm:items-start">
        <div>
          <div className="text-sm font-semibold text-ink">Destination keeps its scheduling authority</div>
          <div className="mt-1 text-xs leading-5 text-muted">
            {authorityLabel(preview.target_scheduling_authority)} · version {preview.target_scheduling_authority_version}. Import does not switch authority or move scheduling history.
          </div>
        </div>
        <Badge value={preview.dry_run ? "previewed" : preview.status} className="self-start" />
      </div>
      <div>
        <div className="text-xs font-semibold uppercase tracking-wide text-muted">Included in this bundle</div>
        <div className="mt-2 flex flex-wrap gap-2">
          {included.map((section) => <Badge key={section} value={sectionLabel(section)} />)}
        </div>
      </div>
      <div className="grid gap-2 text-xs leading-5 sm:grid-cols-3">
        <ImportFact label="Source adapter intent" value={providerLabel(preview.source_active_pos_provider)} />
        <ImportFact label="Destination adapter" value={providerLabel(preview.target_active_pos_provider)} />
        <ImportFact label="After import" value={providerLabel(preview.result_active_pos_provider)} />
      </div>
      <div className="grid gap-2 text-xs leading-5 sm:grid-cols-3">
        <ImportFact label="Source booking intent" value={bookingModeLabel(preview.source_booking_mode)} />
        <ImportFact label="Destination booking mode" value={bookingModeLabel(preview.target_booking_mode)} />
        <ImportFact label="After import" value={bookingModeLabel(preview.result_booking_mode)} />
      </div>
      <details className="text-xs leading-5 text-muted">
        <summary className="cursor-pointer font-semibold text-ink">Excluded operational and secret data ({excluded.length})</summary>
        <div className="mt-2 break-words">{excluded.map(dataLabel).join(", ") || "None reported"}</div>
      </details>
    </div>
  );
}

function ImportFact({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-md border border-line bg-white px-3 py-2">
      <div className="font-semibold text-ink">{label}</div>
      <div className="mt-1 break-words text-muted">{value}</div>
    </div>
  );
}

export function ImportSummaryTable({ summary }: { summary: ImportSummary }) {
  const items = Array.isArray(summary) ? summary : [];
  return (
    <>
      <div className="hidden overflow-x-auto rounded-md border border-line md:block">
        <table className="w-full min-w-[620px] text-left text-sm">
          <thead className="bg-slate-50 text-xs uppercase text-muted">
            <tr>
              <th className="px-3 py-2">Section</th>
              <th className="px-3 py-2">Create</th>
              <th className="px-3 py-2">Update</th>
              <th className="px-3 py-2">Unchanged</th>
              <th className="px-3 py-2">Skipped</th>
              <th className="px-3 py-2">Conflicts</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-line">
            {items.map((item) => (
              <tr key={item.section}>
                <td className="px-3 py-2 font-medium text-ink">{sectionLabel(item.section)}</td>
                <td className="px-3 py-2 text-muted">{item.created}</td>
                <td className="px-3 py-2 text-muted">{item.updated}</td>
                <td className="px-3 py-2 text-muted">{item.unchanged}</td>
                <td className="px-3 py-2 text-muted">{item.skipped}</td>
                <td className="px-3 py-2 text-muted">{item.conflicts}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <div className="space-y-3 md:hidden">
        {items.map((item) => (
          <div key={item.section} className="rounded-md border border-line p-3">
            <div className="text-sm font-semibold text-ink">{sectionLabel(item.section)}</div>
            <div className="mt-2 grid grid-cols-2 gap-2 text-xs text-muted">
              <span>Create: {item.created}</span>
              <span>Update: {item.updated}</span>
              <span>Unchanged: {item.unchanged}</span>
              <span>Skipped: {item.skipped}</span>
              <span>Conflicts: {item.conflicts}</span>
            </div>
          </div>
        ))}
      </div>
    </>
  );
}

export function ImportIssueList({
  title,
  issues,
  tone
}: {
  title: string;
  issues: ImportIssues;
  tone: "warning" | "danger";
}) {
  const items = Array.isArray(issues) ? issues : [];
  if (items.length === 0) return null;
  const className = tone === "danger" ? "border-red-200 bg-red-50 text-red-900" : "border-amber-200 bg-amber-50 text-amber-900";
  return (
    <div className={`rounded-md border p-4 text-sm ${className}`}>
      <div className="font-semibold">{title}</div>
      <ul className="mt-2 space-y-2">
        {items.map((issue, index) => (
          <li key={`${issue.code}-${issue.field ?? issue.source_key ?? index}`}>
            {sectionLabel(issue.section)}: {issue.message}
          </li>
        ))}
      </ul>
    </div>
  );
}

export function sectionLabel(section: string) {
  switch (section) {
    case "salon_profile":
      return "Salon profile";
    case "ai_receptionist":
      return "AI receptionist";
    case "public_booking_page":
      return "Public booking page";
    case "integrations":
      return "Integrations";
    case "knowledge_base":
      return "Knowledge base";
    case "service_categories":
      return "Service categories";
    case "service_aliases":
      return "Service aliases";
    case "service_consultation_profiles":
      return "Service consultation profiles";
    default:
      return section;
  }
}

export function listOrNone(values: string[] | null | undefined) {
  const items = Array.isArray(values) ? values : [];
  return items.length ? items.join(", ") : "none";
}

function authorityLabel(authority: ConfigurationImportResponse["target_scheduling_authority"]) {
  switch (authority) {
    case "owner_manual":
      return "Owner confirmation";
    case "manleai_calendar":
      return "ManleAI Calendar";
    case "external_provider":
      return "Square Appointments";
  }
}

function dataLabel(value: string) {
  return value.replaceAll("_", " ");
}

function providerLabel(provider: string) {
  const normalized = provider.trim();
  if (!normalized) return "Not included / unchanged";
  if (normalized === "square") return "Square Appointments";
  return normalized.replaceAll("_", " ");
}

function bookingModeLabel(mode: string) {
  switch (mode.trim()) {
    case "confirmed_booking":
      return "Confirmed booking";
    case "pending_approval":
      return "Pending owner approval";
    case "disabled":
      return "Booking disabled";
    default:
      return "Not included / unchanged";
  }
}
