import type { ConfigurationImportResponse } from "@/types/api";

type ImportSummary = ConfigurationImportResponse["summary"] | null | undefined;
type ImportIssues = ConfigurationImportResponse["warnings"] | null | undefined;

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
    default:
      return section;
  }
}

export function listOrNone(values: string[] | null | undefined) {
  const items = Array.isArray(values) ? values : [];
  return items.length ? items.join(", ") : "none";
}
