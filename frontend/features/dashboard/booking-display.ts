import type { ConversationSession, StaffSelectionMode } from "@/types/api";

export type BookingSegmentDisplay = {
  service_id?: string;
  service_name?: string;
  staff_id?: string;
  staff_name?: string;
  staff_selection_mode?: StaffSelectionMode;
  sort_order?: number;
};

export type BookingDisplayRecord = {
  service_id?: string;
  service_name?: string;
  staff_id?: string;
  staff_name?: string;
  staff_selection_mode?: StaffSelectionMode;
  segments?: BookingSegmentDisplay[];
};

export function conversationBookingRecord(session: ConversationSession): BookingDisplayRecord {
  return {
    service_id: session.service_id,
    service_name: session.service_name,
    staff_id: session.staff_id,
    staff_name: session.staff_name,
    staff_selection_mode: session.staff_selection_mode,
    segments: session.booking_segments
  };
}

export function serviceNamesLabel(record: BookingDisplayRecord, serviceNames?: Map<string, string>) {
  const names = orderedSegments(record)
    .map((segment) => displayServiceName(segment, serviceNames))
    .filter(Boolean);
  if (names.length > 0) {
    return unique(names).join(" + ");
  }
  if (record.service_name) {
    return record.service_name;
  }
  if (record.service_id) {
    return serviceNames?.get(record.service_id) || "Unknown service";
  }
  return "-";
}

export function assignedTechniciansLabel(record: BookingDisplayRecord, staffNames?: Map<string, string>) {
  const names = orderedSegments(record)
    .map((segment) => displayStaffName(segment, staffNames))
    .filter(Boolean);
  if (names.length > 0) {
    return unique(names).join(", ");
  }
  if (record.staff_name) {
    return record.staff_name;
  }
  if (record.staff_id) {
    return staffNames?.get(record.staff_id) || "Unknown technician";
  }
  return "Not assigned";
}

export function technicianPreferenceValue(record: BookingDisplayRecord): "anyone" | "specific" {
  if (record.staff_selection_mode === "anyone") {
    return "anyone";
  }
  if (orderedSegments(record).some((segment) => segment.staff_selection_mode === "anyone")) {
    return "anyone";
  }
  return "specific";
}

export function technicianPreferenceLabel(record: BookingDisplayRecord) {
  return technicianPreferenceValue(record) === "anyone" ? "Anyone" : "Specific technician";
}

export function bookingSummaryLabel(record: BookingDisplayRecord, serviceNames?: Map<string, string>, staffNames?: Map<string, string>) {
  return `${serviceNamesLabel(record, serviceNames)} · Preference: ${technicianPreferenceLabel(record)} · Assigned: ${assignedTechniciansLabel(record, staffNames)}`;
}

export function orderedSegments(record: BookingDisplayRecord) {
  return [...(record.segments ?? [])].sort((a, b) => (a.sort_order ?? 0) - (b.sort_order ?? 0));
}

function displayServiceName(segment: BookingSegmentDisplay, serviceNames?: Map<string, string>) {
  if (segment.service_name) {
    return segment.service_name;
  }
  if (segment.service_id) {
    return serviceNames?.get(segment.service_id) || "Unknown service";
  }
  return "";
}

function displayStaffName(segment: BookingSegmentDisplay, staffNames?: Map<string, string>) {
  if (segment.staff_name) {
    return segment.staff_name;
  }
  if (segment.staff_id) {
    return staffNames?.get(segment.staff_id) || "Unknown technician";
  }
  return "";
}

function unique(values: string[]) {
  return Array.from(new Set(values));
}
