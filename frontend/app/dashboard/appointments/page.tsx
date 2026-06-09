import { DeferredPage } from "@/components/layout/deferred-page";

export default function AppointmentsPage() {
  return (
    <DeferredPage
      title="Appointments"
      milestone="Scheduled for Milestone 3"
      description="Appointment views depend on the booking tables and real Square appointment operations. The current release stops at the POS connection and sync foundation."
    />
  );
}

