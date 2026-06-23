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
import type {
  AppointmentRecord,
  BookingAttempt,
  ConversationSession,
  POSConnection,
  POSService,
  POSStaffMember,
  Salon,
  SquareReadiness,
  SyncLog,
  VoiceStatus
} from "@/types/api";

type SalonListResponse = {
  salons: Salon[];
};

type StatusResponse = {
  connection: POSConnection;
  sync_logs: SyncLog[];
  readiness: SquareReadiness;
};

type SessionsResponse = {
  sessions: ConversationSession[];
};

type AppointmentsResponse = {
  appointments: AppointmentRecord[];
};

type AttemptsResponse = {
  booking_attempts: BookingAttempt[];
};

type ServicesResponse = {
  services: POSService[];
};

type StaffResponse = {
  staff: POSStaffMember[];
};

export function DashboardHome() {
  const [salons, setSalons] = useState<Salon[]>([]);
  const [status, setStatus] = useState<StatusResponse | null>(null);
  const [voiceStatus, setVoiceStatus] = useState<VoiceStatus | null>(null);
  const [sessions, setSessions] = useState<ConversationSession[]>([]);
  const [appointments, setAppointments] = useState<AppointmentRecord[]>([]);
  const [attempts, setAttempts] = useState<BookingAttempt[]>([]);
  const [services, setServices] = useState<POSService[]>([]);
  const [staff, setStaff] = useState<POSStaffMember[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");

  useEffect(() => {
    let mounted = true;

    async function loadDashboard() {
      setError("");
      setLoading(true);
      try {
        const salonResponse = await apiRequest<SalonListResponse>("/api/salons");
        const firstSalon = salonResponse.salons[0] ?? null;
        if (!mounted) return;
        setSalons(salonResponse.salons);

        if (!firstSalon) {
          setStatus(null);
          setVoiceStatus(null);
          setSessions([]);
          setAppointments([]);
          setAttempts([]);
          setServices([]);
          setStaff([]);
          return;
        }

        const [statusResponse, voiceResponse, sessionsResponse, appointmentResponse, attemptResponse, serviceResponse, staffResponse] =
          await Promise.all([
            apiRequest<StatusResponse>(`/api/integrations/square/status?salon_id=${firstSalon.id}`),
            apiRequest<VoiceStatus>(`/api/salons/${firstSalon.id}/voice/status`),
            apiRequest<SessionsResponse>(`/api/salons/${firstSalon.id}/conversation-sessions?limit=25`),
            apiRequest<AppointmentsResponse>(`/api/salons/${firstSalon.id}/appointments`),
            apiRequest<AttemptsResponse>(`/api/salons/${firstSalon.id}/booking-attempts?limit=50`),
            apiRequest<ServicesResponse>(`/api/salons/${firstSalon.id}/services`),
            apiRequest<StaffResponse>(`/api/salons/${firstSalon.id}/staff`)
          ]);

        if (!mounted) return;
        setStatus(statusResponse);
        setVoiceStatus(voiceResponse);
        setSessions(sessionsResponse.sessions);
        setAppointments(appointmentResponse.appointments);
        setAttempts(attemptResponse.booking_attempts);
        setServices(serviceResponse.services);
        setStaff(staffResponse.staff);
      } catch (err) {
        if (mounted) setError(err instanceof Error ? err.message : "Could not load dashboard.");
      } finally {
        if (mounted) setLoading(false);
      }
    }

    void loadDashboard();
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
  const phoneCount = sessions.filter((item) => item.channel === "phone").length;
  const confirmedAppointments = appointments.filter((item) => item.status === "confirmed").length;
  const pendingRequests = attempts.filter((item) => item.status === "fallback_pending").length;
  const bookingReadyServices = services.filter(serviceIsBookingReady).length;
  const bookingReadyStaff = staff.filter(staffIsBookingReady).length;
  const squareConnected = Boolean(status?.connection.id) && status?.connection.status !== "not_connected";
  const squareBadge = squareConnected ? status?.connection.status || "connected" : "not_connected";
  const squareDescription = squareConnected
    ? `Square Appointments connected. Last sync: ${formatOptionalDate(status?.connection.last_sync_at)}.`
    : status?.connection.error_message || "Connect Square Appointments and select a location before booking readiness can pass.";
  const phoneBookingReady = Boolean(voiceStatus?.phone_booking_ready);
  const canonicalReady = bookingReadyServices > 0 && bookingReadyStaff > 0;

  return (
    <div className="space-y-6">
      <div className="flex flex-col justify-between gap-3 md:flex-row md:items-end">
        <div>
          <h1 className="text-2xl font-bold text-ink">Dashboard</h1>
          <p className="mt-1 text-sm text-muted">
            Monitor POS-first receptionist readiness, pending requests, and Square Appointments booking status.
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
            <MetricCard
              icon={PhoneCall}
              label="Live phone calls"
              value={String(phoneCount)}
              subtext={voiceStatus?.ready ? "Phone webhook sessions" : "Phone not configured"}
            />
            <MetricCard
              icon={CalendarDays}
              label="POS-confirmed appointments"
              value={String(confirmedAppointments)}
              subtext="Square booking ID returned"
            />
            <MetricCard
              icon={Link2}
              label="Pending requests"
              value={String(pendingRequests)}
              subtext="Owner review after POS failure"
            />
            <MetricCard
              icon={CheckCircle2}
              label="Booking-ready records"
              value={`${bookingReadyServices}/${bookingReadyStaff}`}
              subtext="Services / staff"
            />
          </div>

          <div className="grid gap-4 xl:grid-cols-3">
            <ReadinessCard
              icon={Link2}
              title="Square Appointments"
              badge={squareBadge}
              description={squareDescription}
              href="/dashboard/integrations"
              action="Open Integrations"
            />
            <ReadinessCard
              icon={PhoneCall}
              title="Phone booking"
              badge={phoneBookingReady ? "ready" : "not_configured"}
              description={
                phoneBookingReady
                  ? "Phone calls can offer active POS availability and attempt POS-first booking."
                  : voiceStatus?.booking?.blocked_reason || voiceStatus?.blocked_reason || "Configure voice and booking readiness before phone booking."
              }
              href="/dashboard/calls"
              action="Open Calls"
            />
            <ReadinessCard
              icon={CheckCircle2}
              title="Canonical data"
              badge={canonicalReady ? "ready" : "blocked"}
              description={`Booking uses ${bookingReadyServices} booking-ready services and ${bookingReadyStaff} booking-ready staff. Local-only or failed records stay out of availability.`}
              href="/dashboard/services"
              action="Review Services"
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
                  <CardTitle>POS booking boundary</CardTitle>
                  <CardDescription>
                    Appointments are POS-confirmed only after Square Appointments returns a booking ID.
                    POS failures create pending requests for owner review.
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

function ReadinessCard({
  icon: Icon,
  title,
  badge,
  description,
  href,
  action
}: {
  icon: React.ElementType;
  title: string;
  badge: string;
  description: string;
  href: string;
  action: string;
}) {
  return (
    <Card>
      <div className="flex items-start gap-3">
        <Icon className="mt-1 h-5 w-5 text-brand" />
        <div className="min-w-0">
          <div className="flex flex-wrap items-center gap-2">
            <CardTitle>{title}</CardTitle>
            <Badge value={badge} />
          </div>
          <CardDescription>{description}</CardDescription>
          <Link
            className="mt-4 inline-flex h-10 items-center justify-center rounded-md border border-line bg-white px-4 text-sm font-semibold text-ink hover:bg-slate-50"
            href={href}
          >
            {action}
          </Link>
        </div>
      </div>
    </Card>
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

function serviceIsBookingReady(service: POSService) {
  return (
    service.active &&
    !service.archived_at &&
    service.sync_status === "synced" &&
    service.pos_linked &&
    service.ai_bookable &&
    Boolean(service.pos_service_id) &&
    Boolean(service.pos_service_version)
  );
}

function staffIsBookingReady(member: POSStaffMember) {
  return (
    member.active &&
    !member.archived_at &&
    member.sync_status === "synced" &&
    member.pos_linked &&
    member.ai_bookable &&
    Boolean(member.pos_staff_id)
  );
}

function formatOptionalDate(value?: string) {
  if (!value) return "Not synced";
  return new Date(value).toLocaleString();
}

function Info({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <dt className="text-xs font-semibold uppercase tracking-wide text-muted">{label}</dt>
      <dd className="mt-1 text-sm font-medium text-ink">{value}</dd>
    </div>
  );
}
