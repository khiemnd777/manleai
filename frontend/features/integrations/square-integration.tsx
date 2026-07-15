"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import type { Dispatch, ReactNode, SetStateAction } from "react";
import {
  AlertTriangle,
  Ban,
  Bot,
  CalendarCheck,
  CheckCircle2,
  ExternalLink,
  KeyRound,
  PhoneCall,
  Power,
  PowerOff,
  RefreshCcw,
  Save,
  Workflow,
  XCircle
} from "lucide-react";
import { Alert } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardDescription, CardTitle } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { apiRequest } from "@/lib/api/client";
import type {
  AvailabilityResult,
  AvailabilitySlot,
  BookingAttempt,
  IntegrationConfigs,
  OpenAIIntegrationConfig,
  POSConnection,
  POSLocation,
  ProviderSwitchMatch,
  ProviderSwitchDryRunReadiness,
  ProviderSwitchRun,
  ProviderSwitchReadiness,
  POSService,
  POSStaffMember,
  Salon,
  SquareIntegrationConfig,
  SquareReadiness,
  SyncLog,
  TwilioIntegrationConfig,
  TestBookingRecord
} from "@/types/api";

type SalonListResponse = {
  salons: Salon[];
};

type StatusResponse = {
  connection: POSConnection;
  sync_logs: SyncLog[];
  readiness: SquareReadiness;
};

type ServicesResponse = {
  services: POSService[];
};

type StaffResponse = {
  staff: POSStaffMember[];
};

type LocationsResponse = {
  locations: POSLocation[];
};

type TestBookingResponse = {
  booking_attempt?: BookingAttempt;
  appointment?: {
    status: string;
  };
  latest_test_booking?: TestBookingRecord;
  readiness: SquareReadiness;
};

type GateResponse = {
  readiness: SquareReadiness;
};

type ProviderSwitchRunResponse = {
  run?: ProviderSwitchRun | null;
};

type ProviderSwitchMatchUpdateResponse = {
  run?: ProviderSwitchRun | null;
};

type TestBookingForm = {
  service_id: string;
  staff_id: string;
  start_time: string;
  customer_name: string;
  customer_phone: string;
  customer_email: string;
  notes: string;
};

type ConfigTab = "square" | "twilio" | "openai";

type SquareConfigForm = {
  environment: string;
  client_id: string;
  client_secret: string;
  clear_client_secret: boolean;
  redirect_url: string;
  api_version: string;
  api_base_url: string;
  webhook_notification_url: string;
  webhook_signature_key: string;
  clear_webhook_signature_key: boolean;
};

type TwilioConfigForm = {
  public_base_url: string;
  auth_token: string;
  clear_auth_token: boolean;
  incoming_path: string;
  turn_path: string;
  recording_path: string;
  stream_path: string;
  voice_transport: string;
};

type OpenAIConfigForm = {
  enabled: boolean;
  api_key: string;
  clear_api_key: boolean;
  base_url: string;
  transcription_model: string;
  reply_model: string;
  speech_model: string;
  speech_voice: string;
  speech_output_mode: "streaming_tts" | "buffered_realtime";
  realtime_enabled: boolean;
  realtime_model: string;
  realtime_voice: string;
  realtime_noise_profile: string;
  realtime_instructions: string;
};

const defaultForm: TestBookingForm = {
  service_id: "",
  staff_id: "",
  start_time: "",
  customer_name: "ManleAI Test Customer",
  customer_phone: "+13125550199",
  customer_email: "",
  notes: "AI booking readiness test. Cancel after verifying Square booking creation."
};

const defaultSquareConfigForm: SquareConfigForm = {
  environment: "sandbox",
  client_id: "",
  client_secret: "",
  clear_client_secret: false,
  redirect_url: "http://localhost:18089/api/integrations/square/callback",
  api_version: "2026-05-20",
  api_base_url: "",
  webhook_notification_url: "",
  webhook_signature_key: "",
  clear_webhook_signature_key: false
};

const defaultTwilioConfigForm: TwilioConfigForm = {
  public_base_url: "",
  auth_token: "",
  clear_auth_token: false,
  incoming_path: "/api/voice/twilio/incoming",
  turn_path: "/api/voice/twilio/turn",
  recording_path: "/api/voice/twilio/recording",
  stream_path: "/api/voice/twilio/stream",
  voice_transport: "recording"
};

const defaultOpenAIConfigForm: OpenAIConfigForm = {
  enabled: false,
  api_key: "",
  clear_api_key: false,
  base_url: "https://api.openai.com/v1",
  transcription_model: "gpt-4o-mini-transcribe",
  reply_model: "gpt-4.1-mini",
  speech_model: "tts-1",
  speech_voice: "alloy",
  speech_output_mode: "streaming_tts",
  realtime_enabled: false,
  realtime_model: "gpt-realtime-2",
  realtime_voice: "alloy",
  realtime_noise_profile: "noisy_salon",
  realtime_instructions: ""
};

function operationKeyForPayload(
  ref: { current: { key: string; fingerprint: string } | null },
  payload: Record<string, unknown>
) {
  const fingerprint = JSON.stringify(payload);
  if (!ref.current || ref.current.fingerprint !== fingerprint) {
    ref.current = { key: crypto.randomUUID(), fingerprint };
  }
  return ref.current.key;
}

function safeTestRetryAttemptID(
  operationType: "book" | "cancel",
  attempt: BookingAttempt | null,
  latest?: TestBookingRecord
) {
  if (
    attempt?.operation_type === operationType &&
    attempt.status === "fallback_pending" &&
    attempt.retry_policy === "safe" &&
    attempt.can_retry
  ) {
    return attempt.id;
  }
  if (
    latest?.operation_type === operationType &&
    latest.status === "fallback_pending" &&
    latest.retry_policy === "safe" &&
    latest.can_retry
  ) {
    return latest.booking_attempt_id;
  }
  return undefined;
}

function testWriteBlocked(attempt: BookingAttempt | null, latest?: TestBookingRecord) {
  return Boolean(
    attempt?.status === "pos_pending" ||
      attempt?.provider_outcome === "in_flight" ||
      attempt?.provider_outcome === "unknown" ||
      attempt?.retry_policy === "blocked" ||
      attempt?.reconciliation_status === "required" ||
      latest?.status === "pos_pending" ||
      latest?.provider_outcome === "in_flight" ||
      latest?.provider_outcome === "unknown" ||
      latest?.retry_policy === "blocked" ||
      latest?.reconciliation_status === "required"
  );
}

function testWriteBlockedReason(attempt: BookingAttempt | null, latest?: TestBookingRecord) {
  return (
    attempt?.retry_blocked_reason ||
    latest?.retry_blocked_reason ||
    "The provider write is still in flight or its result is unknown. Verify the action in Square before submitting another write."
  );
}

export function SquareIntegration() {
  const [salons, setSalons] = useState<Salon[]>([]);
  const [status, setStatus] = useState<StatusResponse | null>(null);
  const [integrationConfigs, setIntegrationConfigs] = useState<IntegrationConfigs | null>(null);
  const [switchReadiness, setSwitchReadiness] = useState<ProviderSwitchReadiness | null>(null);
  const [switchRun, setSwitchRun] = useState<ProviderSwitchRun | null>(null);
  const [switchDryRunReadiness, setSwitchDryRunReadiness] = useState<ProviderSwitchDryRunReadiness | null>(null);
  const [services, setServices] = useState<POSService[]>([]);
  const [staff, setStaff] = useState<POSStaffMember[]>([]);
  const [locations, setLocations] = useState<POSLocation[]>([]);
  const [selectedLocationID, setSelectedLocationID] = useState("");
  const [form, setForm] = useState<TestBookingForm>(defaultForm);
  const [activeConfigTab, setActiveConfigTab] = useState<ConfigTab>("square");
  const [squareConfigForm, setSquareConfigForm] = useState<SquareConfigForm>(defaultSquareConfigForm);
  const [twilioConfigForm, setTwilioConfigForm] = useState<TwilioConfigForm>(defaultTwilioConfigForm);
  const [openAIConfigForm, setOpenAIConfigForm] = useState<OpenAIConfigForm>(defaultOpenAIConfigForm);
  const [bookingDate, setBookingDate] = useState("");
  const [availabilityResult, setAvailabilityResult] = useState<AvailabilityResult | null>(null);
  const [availabilityError, setAvailabilityError] = useState("");
  const [availabilityChecked, setAvailabilityChecked] = useState(false);
  const [checkingAvailability, setCheckingAvailability] = useState(false);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");
  const [success, setSuccess] = useState("");
  const [testWriteAttempt, setTestWriteAttempt] = useState<BookingAttempt | null>(null);
  const testBookingOperationRef = useRef<{ key: string; fingerprint: string } | null>(null);
  const testCancelOperationRef = useRef<{ key: string; fingerprint: string } | null>(null);
  const availabilityRequestIDRef = useRef(0);
  const availabilityExpiryTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  const salon = salons[0];
  const connection = status?.connection;
  const readiness = status?.readiness;
  const latestTest = readiness?.latest_test_booking;
  const selectedLocation =
    locations.find((location) => location.id === (connection?.location_id || selectedLocationID)) ??
    locations.find((location) => location.id === selectedLocationID);
  const squareTimezone = selectedLocation?.timezone || salon?.timezone || "";
  const displayTimezone = squareTimezone || availabilityResult?.timezone || salon?.timezone || undefined;
  const timezoneMismatch =
    Boolean(selectedLocation?.timezone) && Boolean(salon?.timezone) && selectedLocation?.timezone !== salon?.timezone;

  async function load() {
    setError("");
    setLoading(true);
    try {
      const salonResponse = await apiRequest<SalonListResponse>("/api/salons");
      setSalons(salonResponse.salons);
      const firstSalon = salonResponse.salons[0];
      if (!firstSalon) {
        setStatus(null);
        setIntegrationConfigs(null);
        setSwitchReadiness(null);
        setSwitchRun(null);
        setSwitchDryRunReadiness(null);
        setServices([]);
        setStaff([]);
        setLocations([]);
        setSquareConfigForm(defaultSquareConfigForm);
        setTwilioConfigForm(defaultTwilioConfigForm);
        setOpenAIConfigForm(defaultOpenAIConfigForm);
        return;
      }

      const [squareStatus, configResponse, providerSwitchResponse, providerSwitchRunResponse, serviceResponse, staffResponse] = await Promise.all([
        apiRequest<StatusResponse>(`/api/integrations/square/status?salon_id=${firstSalon.id}`),
        apiRequest<IntegrationConfigs>(`/api/salons/${firstSalon.id}/integration-configs`),
        apiRequest<ProviderSwitchReadiness>(`/api/salons/${firstSalon.id}/pos/provider-switch-readiness`),
        apiRequest<ProviderSwitchRunResponse>(`/api/salons/${firstSalon.id}/pos/provider-switch-runs/latest`),
        apiRequest<ServicesResponse>(`/api/salons/${firstSalon.id}/services`),
        apiRequest<StaffResponse>(`/api/salons/${firstSalon.id}/staff`)
      ]);
      setStatus(squareStatus);
      setTestWriteAttempt(null);
      setIntegrationConfigs(configResponse);
      setSquareConfigForm(squareConfigToForm(configResponse.square));
      setTwilioConfigForm(twilioConfigToForm(configResponse.twilio));
      setOpenAIConfigForm(openAIConfigToForm(configResponse.openai));
      setSwitchReadiness(providerSwitchResponse);
      setSwitchRun(providerSwitchRunResponse.run ?? null);
      if (providerSwitchRunResponse.run) {
        const dryRunResponse = await apiRequest<ProviderSwitchDryRunReadiness>(
          `/api/salons/${firstSalon.id}/pos/provider-switch-runs/${providerSwitchRunResponse.run.id}/dry-run-readiness`
        );
        setSwitchDryRunReadiness(dryRunResponse);
      } else {
        setSwitchDryRunReadiness(null);
      }
      setServices(serviceResponse.services);
      setStaff(staffResponse.staff);
      setSelectedLocationID(squareStatus.connection.location_id || "");

      if (squareStatus.connection.id) {
        const locationResponse = await apiRequest<LocationsResponse>(
          `/api/integrations/square/locations?salon_id=${firstSalon.id}`
        );
        setLocations(locationResponse.locations);
        if (!squareStatus.connection.location_id && locationResponse.locations[0]) {
          setSelectedLocationID(locationResponse.locations[0].id);
        }
      } else {
        setLocations([]);
      }

      setForm((current) => ({
        ...current,
        service_id: current.service_id || firstBookableService(serviceResponse.services)?.id || "",
        staff_id: current.staff_id || firstBookableStaff(staffResponse.staff)?.id || "",
        start_time: ""
      }));
      clearAvailability();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Could not load integrations.");
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    void load();
    return () => clearAvailabilityExpiryTimer(availabilityExpiryTimerRef);
  }, []);

  useEffect(() => {
    if (!bookingDate && displayTimezone) {
      setBookingDate(nextBookingDate(displayTimezone));
    }
  }, [bookingDate, displayTimezone]);

  const bookableServices = useMemo(
    () => services.filter(serviceIsBookable),
    [services]
  );
  const bookableStaff = useMemo(
    () => staff.filter(staffIsBookable),
    [staff]
  );

  async function connectSquare() {
    if (!salon) return;
    if (!integrationConfigs?.square.configured) {
      setError("Save Square Appointments credentials before starting OAuth.");
      setActiveConfigTab("square");
      return;
    }
    setBusy("connect");
    setError("");
    setSuccess("");
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

  async function selectLocation() {
    if (!salon || !selectedLocationID) return;
    setBusy("location");
    setError("");
    setSuccess("");
    try {
      await apiRequest<POSConnection>("/api/integrations/square/select-location", {
        method: "POST",
        body: JSON.stringify({ salon_id: salon.id, location_id: selectedLocationID })
      });
      setSuccess("Square location selected.");
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Could not select Square location.");
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
      setSuccess("Square sync completed.");
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Square sync failed.");
    } finally {
      setBusy("");
    }
  }

  async function checkAvailability() {
    if (!salon || !form.service_id || !form.staff_id || !bookingDate) return;
    const requestID = ++availabilityRequestIDRef.current;
    clearAvailabilityExpiryTimer(availabilityExpiryTimerRef);
    setAvailabilityError("");
    setAvailabilityChecked(true);
    setCheckingAvailability(true);
    setForm((current) => ({ ...current, start_time: "" }));
    const payload = {
      service_id: form.service_id,
      staff_id: form.staff_id,
      staff_selection_mode: "specific",
      segments: [
        {
          service_id: form.service_id,
          staff_id: form.staff_id,
          staff_selection_mode: "specific"
        }
      ],
      preferred_date: bookingDate,
      limit: 20
    };
    try {
      const result = await apiRequest<AvailabilityResult>(`/api/salons/${salon.id}/availability`, {
        method: "POST",
        body: JSON.stringify(payload)
      });
      if (requestID !== availabilityRequestIDRef.current) return;
      setAvailabilityResult(result);
      scheduleAvailabilityExpiry(
        availabilityExpiryTimerRef,
        result.expires_at,
        requestID,
        availabilityRequestIDRef,
        () => {
          setAvailabilityResult(null);
          setAvailabilityChecked(false);
          setForm((current) => ({ ...current, start_time: "" }));
          setAvailabilityError("This availability quote expired. Check Square Appointments again before creating the test booking.");
        }
      );
    } catch (err) {
      if (requestID !== availabilityRequestIDRef.current) return;
      setAvailabilityResult(null);
      setAvailabilityError(err instanceof Error ? err.message : "Could not check Square Appointments availability.");
    } finally {
      if (requestID === availabilityRequestIDRef.current) {
        setCheckingAvailability(false);
      }
    }
  }

  async function createTestBooking() {
    if (!salon) return;
    setBusy("test");
    setError("");
    setSuccess("");
    try {
      if (!availabilityResult) {
        throw new Error("Check Square Appointments availability and select a current quote first.");
      }
      const selectedSlot = availabilityResult.slots.find((slot) => slot.start_time === form.start_time);
      if (!selectedSlot) {
        throw new Error("Select a slot from the current Square availability quote.");
      }
      assertAvailabilityQuoteUsable(availabilityResult, selectedSlot);
      const retryOfAttemptID = safeTestRetryAttemptID("book", testWriteAttempt, latestTest);
      const payload = {
        salon_id: salon.id,
        ...form,
        start_time: form.start_time,
        availability_quote_id: availabilityResult.quote_id,
        slot_fingerprint: selectedSlot.fingerprint,
        ...(retryOfAttemptID ? { retry_of_attempt_id: retryOfAttemptID } : {})
      };
      const operationKey = operationKeyForPayload(testBookingOperationRef, payload);
      const response = await apiRequest<TestBookingResponse>("/api/integrations/square/test-booking", {
        method: "POST",
        body: JSON.stringify({
          operation_key: operationKey,
          ...payload
        })
      });
      applyReadiness(response.readiness);
      setTestWriteAttempt(response.booking_attempt ?? null);
      if (response.booking_attempt?.status !== "confirmed" || !response.booking_attempt?.pos_booking_id) {
        if (response.booking_attempt?.retry_policy === "safe") {
          testBookingOperationRef.current = null;
        }
        setError(response.booking_attempt?.error_message || "Test booking is pending owner review.");
      } else {
        testBookingOperationRef.current = null;
        setSuccess("Square test booking created. Cancel it when you finish the optional POS smoke test.");
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : "Could not create Square test booking.");
    } finally {
      setBusy("");
    }
  }

  async function cancelTestBooking() {
    if (!salon || !latestTest?.appointment_id) return;
    setBusy("cancel-test");
    setError("");
    setSuccess("");
    try {
      const retryOfAttemptID = safeTestRetryAttemptID("cancel", testWriteAttempt, latestTest);
      const payload = {
        salon_id: salon.id,
        appointment_id: latestTest.appointment_id,
        reason: "AI booking readiness test cleanup",
        ...(retryOfAttemptID ? { retry_of_attempt_id: retryOfAttemptID } : {})
      };
      const operationKey = operationKeyForPayload(testCancelOperationRef, payload);
      const response = await apiRequest<TestBookingResponse>(
        "/api/integrations/square/cancel-test-booking",
        {
          method: "POST",
          body: JSON.stringify({
            operation_key: operationKey,
            ...payload
          })
        }
      );
      applyReadiness(response.readiness);
      setTestWriteAttempt(response.booking_attempt ?? null);
      if (response.booking_attempt && response.booking_attempt.status !== "cancelled") {
        if (response.booking_attempt.retry_policy === "safe") {
          testCancelOperationRef.current = null;
        }
        setError(response.booking_attempt.error_message || "Test booking cancellation needs owner review.");
      } else {
        testCancelOperationRef.current = null;
        setSuccess("Square test booking cancelled.");
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : "Could not cancel Square test booking.");
    } finally {
      setBusy("");
    }
  }

  async function enableAI() {
    if (!salon) return;
    setBusy("enable");
    setError("");
    setSuccess("");
    try {
      const response = await apiRequest<GateResponse>("/api/integrations/square/enable-ai-booking", {
        method: "POST",
        body: JSON.stringify({ salon_id: salon.id })
      });
      applyReadiness(response.readiness);
      setSalons((items) => items.map((item) => (item.id === salon.id ? { ...item, ai_enabled: true } : item)));
      setSuccess("AI booking enabled for this salon.");
    } catch (err) {
      setError(err instanceof Error ? err.message : "Could not enable AI booking.");
    } finally {
      setBusy("");
    }
  }

  async function disableAI() {
    if (!salon) return;
    setBusy("disable");
    setError("");
    setSuccess("");
    try {
      const response = await apiRequest<GateResponse>("/api/integrations/square/disable-ai-booking", {
        method: "POST",
        body: JSON.stringify({ salon_id: salon.id })
      });
      applyReadiness(response.readiness);
      setSalons((items) => items.map((item) => (item.id === salon.id ? { ...item, ai_enabled: false } : item)));
      setSuccess("AI booking disabled.");
    } catch (err) {
      setError(err instanceof Error ? err.message : "Could not disable AI booking.");
    } finally {
      setBusy("");
    }
  }

  async function saveSquareConfig() {
    if (!salon) return;
    setBusy("save-square-config");
    setError("");
    setSuccess("");
    try {
      const updated = await apiRequest<SquareIntegrationConfig>(`/api/salons/${salon.id}/integration-configs/square`, {
        method: "PUT",
        body: JSON.stringify(squareConfigForm)
      });
      setIntegrationConfigs((current) => ({ ...(current ?? emptyIntegrationConfigs()), square: updated }));
      setSquareConfigForm(squareConfigToForm(updated));
      setSuccess("Square Appointments configuration saved.");
    } catch (err) {
      setError(err instanceof Error ? err.message : "Could not save Square configuration.");
    } finally {
      setBusy("");
    }
  }

  async function saveTwilioConfig() {
    if (!salon) return;
    setBusy("save-twilio-config");
    setError("");
    setSuccess("");
    try {
      const updated = await apiRequest<TwilioIntegrationConfig>(`/api/salons/${salon.id}/integration-configs/twilio`, {
        method: "PUT",
        body: JSON.stringify(twilioConfigForm)
      });
      setIntegrationConfigs((current) => ({ ...(current ?? emptyIntegrationConfigs()), twilio: updated }));
      setTwilioConfigForm(twilioConfigToForm(updated));
      setSuccess("Twilio voice configuration saved.");
    } catch (err) {
      setError(err instanceof Error ? err.message : "Could not save Twilio configuration.");
    } finally {
      setBusy("");
    }
  }

  async function saveOpenAIConfig() {
    if (!salon) return;
    setBusy("save-openai-config");
    setError("");
    setSuccess("");
    try {
      const updated = await apiRequest<OpenAIIntegrationConfig>(`/api/salons/${salon.id}/integration-configs/openai`, {
        method: "PUT",
        body: JSON.stringify(openAIConfigForm)
      });
      setIntegrationConfigs((current) => ({ ...(current ?? emptyIntegrationConfigs()), openai: updated }));
      setOpenAIConfigForm(openAIConfigToForm(updated));
      setSuccess("OpenAI voice AI configuration saved.");
    } catch (err) {
      setError(err instanceof Error ? err.message : "Could not save OpenAI configuration.");
    } finally {
      setBusy("");
    }
  }

  async function updateSwitchMatch(matchID: string, matchStatus: string) {
    if (!salon || !switchRun) return;
    setBusy(`switch-match:${matchID}:${matchStatus}`);
    setError("");
    setSuccess("");
    try {
      const response = await apiRequest<ProviderSwitchMatchUpdateResponse>(
        `/api/salons/${salon.id}/pos/provider-switch-runs/${switchRun.id}/matches/${matchID}`,
        {
          method: "PATCH",
          body: JSON.stringify({ match_status: matchStatus })
        }
      );
      setSwitchRun(response.run ?? null);
      if (response.run) {
        const dryRunResponse = await apiRequest<ProviderSwitchDryRunReadiness>(
          `/api/salons/${salon.id}/pos/provider-switch-runs/${response.run.id}/dry-run-readiness`
        );
        setSwitchDryRunReadiness(dryRunResponse);
      } else {
        setSwitchDryRunReadiness(null);
      }
      setSuccess("Provider switch match updated.");
    } catch (err) {
      setError(err instanceof Error ? err.message : "Could not update provider switch match.");
    } finally {
      setBusy("");
    }
  }

  function applyReadiness(next: SquareReadiness) {
    setStatus((current) => (current ? { ...current, readiness: next } : current));
  }

  function clearAvailability() {
    availabilityRequestIDRef.current += 1;
    clearAvailabilityExpiryTimer(availabilityExpiryTimerRef);
    setAvailabilityResult(null);
    setAvailabilityError("");
    setAvailabilityChecked(false);
    setCheckingAvailability(false);
  }

  function updateService(value: string) {
    setForm((current) => ({ ...current, service_id: value, start_time: "" }));
    clearAvailability();
  }

  function updateStaff(value: string) {
    setForm((current) => ({ ...current, staff_id: value, start_time: "" }));
    clearAvailability();
  }

  function updateBookingDate(value: string) {
    setBookingDate(value);
    setForm((current) => ({ ...current, start_time: "" }));
    clearAvailability();
  }

  if (loading) {
    return (
      <div className="space-y-6">
        <Skeleton className="h-9 w-80" />
        <Skeleton className="h-64" />
        <div className="grid gap-4 xl:grid-cols-[0.9fr_1.1fr]">
          <Skeleton className="h-96" />
          <Skeleton className="h-96" />
        </div>
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

  const canCreateTest =
    Boolean(readiness?.can_test_booking) &&
    Boolean(form.service_id) &&
    Boolean(form.staff_id) &&
    Boolean(form.start_time) &&
    Boolean(availabilityResult?.slots.some((slot) => slot.start_time === form.start_time && slot.fingerprint)) &&
    availabilityQuoteIsUsable(availabilityResult) &&
    !testWriteBlocked(testWriteAttempt, latestTest) &&
    busy === "";
  const canCancelTest =
    Boolean(readiness?.can_cancel_test_booking && latestTest?.appointment_id) &&
    !testWriteBlocked(testWriteAttempt, latestTest) &&
    busy === "";
  const canEnable = Boolean(readiness?.can_enable_ai_booking) && busy === "";
  const aiEnabled = Boolean(readiness?.ai_enabled ?? salon.ai_enabled);
  const squareConfigConfigured = Boolean(integrationConfigs?.square.configured);
  const canCheckAvailability =
    Boolean(form.service_id) && Boolean(form.staff_id) && Boolean(bookingDate) && busy === "" && !checkingAvailability;

  return (
    <div className="space-y-6">
      <div className="flex flex-col justify-between gap-3 md:flex-row md:items-end">
        <div>
          <h1 className="text-2xl font-bold text-ink">Integrations</h1>
          <p className="mt-1 text-sm text-muted">
            Square Appointments is the active supported POS integration in this production release.
          </p>
        </div>
        <div className="flex flex-wrap items-center gap-3">
          <Badge value={aiEnabled ? "active" : "disabled"} />
          <Button type="button" variant="secondary" onClick={() => void load()} disabled={busy !== ""}>
            <RefreshCcw className="h-4 w-4" />
            Refresh
          </Button>
        </div>
      </div>

      {error ? <Alert title="Integration action failed" message={error} /> : null}
      {success ? <Alert type="success" title="Integration updated" message={success} /> : null}

      <ReadinessOverviewPanel
        busy={busy}
        connection={connection}
        latestTest={latestTest}
        readiness={readiness}
        services={services}
        staff={staff}
        squareConfigConfigured={squareConfigConfigured}
        switchReadiness={switchReadiness}
        syncLogCount={status?.sync_logs?.length ?? 0}
        onConnect={() => void connectSquare()}
        onSync={() => void syncSquare()}
      />

      <ProviderConfigurationPanel
        activeTab={activeConfigTab}
        busy={busy}
        configs={integrationConfigs}
        openAIForm={openAIConfigForm}
        setActiveTab={setActiveConfigTab}
        setOpenAIForm={setOpenAIConfigForm}
        setSquareForm={setSquareConfigForm}
        setTwilioForm={setTwilioConfigForm}
        squareForm={squareConfigForm}
        twilioForm={twilioConfigForm}
        onSaveOpenAI={() => void saveOpenAIConfig()}
        onSaveSquare={() => void saveSquareConfig()}
        onSaveTwilio={() => void saveTwilioConfig()}
      />

      <Card>
        <div className="flex flex-col justify-between gap-5 lg:flex-row lg:items-start">
          <div className="flex items-center gap-3">
            <div className="flex h-11 w-11 items-center justify-center rounded-md bg-slate-900 text-sm font-bold text-white">
              SQ
            </div>
            <div>
              <CardTitle>Square Appointments</CardTitle>
              <CardDescription>
                Connect OAuth, choose a location, then sync Square records into this system.
              </CardDescription>
            </div>
          </div>
          <Badge value={connection?.status ?? "not_connected"} />
        </div>

        <div className="mt-6 grid gap-4 md:grid-cols-2 xl:grid-cols-6">
          <Info label="Provider" value="Square" />
          <Info label="Merchant ID" value={connection?.merchant_id || "Not connected"} />
          <Info label="Location ID" value={connection?.location_id || "Not selected"} />
          <Info label="Bookable services" value={String(readiness?.service_count ?? 0)} />
          <Info label="Bookable staff" value={String(readiness?.staff_count ?? 0)} />
          <Info label="Business periods" value={String(readiness?.business_hour_period_count ?? 0)} />
        </div>

        {connection?.error_message ? (
          <div className="mt-5">
            <Alert title="Last Square error" message={connection.error_message} />
          </div>
        ) : null}

        <div className="mt-6 grid gap-3 lg:grid-cols-[1fr_1fr_auto]">
          <Button type="button" onClick={() => void connectSquare()} disabled={busy !== "" || !squareConfigConfigured}>
            <ExternalLink className="h-4 w-4" />
            {busy === "connect" ? "Opening..." : "Connect Square"}
          </Button>
          <div className="flex min-w-0 gap-2">
            <select
              className="h-10 min-w-0 flex-1 rounded-md border border-line bg-white px-3 text-sm text-ink"
              value={selectedLocationID}
              onChange={(event) => setSelectedLocationID(event.target.value)}
              disabled={!connection?.id || locations.length === 0 || busy !== ""}
            >
              {locations.length === 0 ? <option value="">No locations loaded</option> : null}
              {locations.map((location) => (
                <option key={location.id} value={location.id}>
                  {location.name || location.id}
                </option>
              ))}
            </select>
            <Button
              type="button"
              variant="secondary"
              onClick={() => void selectLocation()}
              disabled={!connection?.id || !selectedLocationID || selectedLocationID === connection.location_id || busy !== ""}
            >
              Save
            </Button>
          </div>
          <Button
            type="button"
            variant="secondary"
            onClick={() => void syncSquare()}
            disabled={!connection?.id || busy !== ""}
          >
            <RefreshCcw className="h-4 w-4" />
            {busy === "sync" ? "Syncing..." : "Sync"}
          </Button>
        </div>
      </Card>

      <ProviderSwitchReadinessPanel
        dryRunReadiness={switchDryRunReadiness}
        readiness={switchReadiness}
        switchRun={switchRun}
        busy={busy}
        onRefresh={() => void load()}
        onUpdateMatch={(matchID, matchStatus) => void updateSwitchMatch(matchID, matchStatus)}
      />

      <div className="grid gap-4 xl:grid-cols-[0.9fr_1.1fr]">
        <Card>
          <div className="flex items-start justify-between gap-3">
            <div>
              <CardTitle>AI booking readiness</CardTitle>
              <CardDescription>
                AI booking can be enabled after Square is connected, synced, and has booking-ready services, staff, and hours.
              </CardDescription>
            </div>
            <Badge value={aiEnabled ? "active" : "disabled"} />
          </div>
          {readiness?.booking_write_blocked ? (
            <div className="mt-5 flex gap-3 rounded-md border border-red-200 bg-red-50 p-4 text-sm text-red-900">
              <AlertTriangle className="mt-0.5 h-5 w-5 flex-none text-accent" />
              <div className="min-w-0">
                <div className="font-semibold">Square booking writes are blocked</div>
                <div className="mt-1 leading-6">
                  Square can return availability, but new bookings cannot be POS-confirmed until the seller account and OAuth token allow booking writes.
                </div>
                <div className="mt-2 break-words text-xs text-muted">
                  {readiness.booking_write_blocked_reason || "Square Appointments rejected booking writes."}
                </div>
                <div className="mt-1 text-xs text-muted">
                  Last seen: {formatOptionalDateTime(readiness.booking_write_blocked_at)}
                </div>
              </div>
            </div>
          ) : readiness?.appointment_change_write_blocked ? (
            <div className="mt-5 flex gap-3 rounded-md border border-amber-200 bg-amber-50 p-4 text-sm text-amber-900">
              <AlertTriangle className="mt-0.5 h-5 w-5 flex-none text-amber-700" />
              <div className="min-w-0">
                <div className="font-semibold">Square appointment changes are blocked</div>
                <div className="mt-1 leading-6">
                  New bookings can still be POS-confirmed, but reschedule and cancellation requests will stay pending for owner review until the Square seller account supports Appointments write operations.
                </div>
                <div className="mt-2 break-words text-xs text-muted">
                  {readiness.appointment_change_write_blocked_reason || "Square Appointments rejected appointment-change writes."}
                </div>
              </div>
            </div>
          ) : null}
          <div className="mt-5 space-y-3">
            {readiness?.checks.map((step) => (
              <div key={step.key} className="rounded-md border border-line p-3">
                <div className="flex items-center justify-between gap-3">
                  <div className="flex items-center gap-3">
                    <CheckCircle2
                      className={step.complete ? "h-5 w-5 text-brand" : "h-5 w-5 text-slate-300"}
                    />
                    <span className="text-sm font-medium text-ink">{step.label}</span>
                  </div>
                  <Badge value={step.complete ? "active" : "disabled"} />
                </div>
                {!step.complete && step.message ? (
                  <div className="mt-2 text-xs leading-5 text-muted">{step.message}</div>
                ) : null}
              </div>
            ))}
          </div>
          <div className="mt-5 flex flex-wrap gap-3">
            {aiEnabled ? (
              <Button type="button" variant="danger" onClick={() => void disableAI()} disabled={busy !== ""}>
                <PowerOff className="h-4 w-4" />
                {busy === "disable" ? "Disabling..." : "Disable AI Booking"}
              </Button>
            ) : (
              <Button type="button" onClick={() => void enableAI()} disabled={!canEnable}>
                <Power className="h-4 w-4" />
                {busy === "enable" ? "Enabling..." : "Enable AI Booking"}
              </Button>
            )}
          </div>
        </Card>

        <Card>
          <CardTitle>Test booking</CardTitle>
          <CardDescription>
            Optional Square write smoke test. Create and cancel a real Square booking when you want to verify POS writes.
          </CardDescription>

          <TestBookingGate
            bookableServiceCount={bookableServices.length}
            bookableStaffCount={bookableStaff.length}
            latest={latestTest}
            readiness={readiness}
          />

          {testWriteBlocked(testWriteAttempt, latestTest) ? (
            <div className="mt-5 rounded-md border border-amber-200 bg-amber-50 p-4 text-sm text-amber-950">
              <div className="flex items-start gap-3">
                <AlertTriangle className="mt-0.5 h-5 w-5 flex-none text-amber-700" />
                <div className="min-w-0">
                  <div className="font-semibold">Reconciliation required</div>
                  <p className="mt-1 leading-6">
                    Square may have completed this test action. Check Square Appointments before trying again.
                  </p>
                  <p className="mt-2 text-xs leading-5 text-amber-800">
                    {testWriteBlockedReason(testWriteAttempt, latestTest)}
                  </p>
                  <Button className="mt-3" type="button" variant="secondary" onClick={() => void load()} disabled={busy !== ""}>
                    <RefreshCcw className="h-4 w-4" />
                    Refresh Square status
                  </Button>
                </div>
              </div>
            </div>
          ) : null}

          <div className="mt-5 grid gap-4 md:grid-cols-2">
            <Field label="Service">
              <select
                className="h-10 w-full rounded-md border border-line bg-white px-3 text-sm text-ink"
                value={form.service_id}
                onChange={(event) => updateService(event.target.value)}
                disabled={bookableServices.length === 0 || busy !== ""}
              >
                {bookableServices.length === 0 ? <option value="">No bookable services</option> : null}
                {bookableServices.map((service) => (
                  <option key={service.id} value={service.id}>
                    {service.name} ({service.duration_minutes} min)
                  </option>
                ))}
              </select>
            </Field>
            <Field label="Staff">
              <select
                className="h-10 w-full rounded-md border border-line bg-white px-3 text-sm text-ink"
                value={form.staff_id}
                onChange={(event) => updateStaff(event.target.value)}
                disabled={bookableStaff.length === 0 || busy !== ""}
              >
                {bookableStaff.length === 0 ? <option value="">No bookable staff</option> : null}
                {bookableStaff.map((member) => (
                  <option key={member.id} value={member.id}>
                    {member.name}
                  </option>
                ))}
              </select>
            </Field>
            <Field label="Booking date">
              <input
                className="h-10 w-full rounded-md border border-line px-3 text-sm text-ink"
                type="date"
                value={bookingDate}
                onChange={(event) => updateBookingDate(event.target.value)}
                disabled={busy !== "" || checkingAvailability}
              />
            </Field>
            <div className="md:col-span-2">
              <div className="rounded-md border border-line p-3">
                <div className="grid gap-3 md:grid-cols-2">
                  <Info label="Square timezone" value={squareTimezone || "Not loaded"} />
                  <Info label="Salon timezone" value={salon.timezone || "Not configured"} />
                </div>
                {timezoneMismatch ? (
                  <div className="mt-3 rounded-md border border-amber-200 bg-amber-50 px-3 py-2 text-xs leading-5 text-amber-900">
                    Square location timezone and salon profile timezone differ. Slots below are shown in Square location time.
                  </div>
                ) : null}
                <div className="mt-3">
                  <Button type="button" variant="secondary" onClick={() => void checkAvailability()} disabled={!canCheckAvailability}>
                    <RefreshCcw className="h-4 w-4" />
                    {checkingAvailability ? "Checking..." : "Check Square Availability"}
                  </Button>
                </div>
              </div>
            </div>
            <div className="md:col-span-2">
              <AvailabilityPicker
                checked={availabilityChecked}
                error={availabilityError}
                loading={checkingAvailability}
                result={availabilityResult}
                selectedStartTime={form.start_time}
                timezone={displayTimezone}
                onSelect={(slot) => setForm((current) => ({ ...current, start_time: slot.start_time }))}
              />
            </div>
            <Field label="Customer phone">
              <input
                className="h-10 w-full rounded-md border border-line px-3 text-sm text-ink"
                value={form.customer_phone}
                onChange={(event) => setForm((current) => ({ ...current, customer_phone: event.target.value }))}
                disabled={busy !== ""}
              />
            </Field>
            <Field label="Customer name">
              <input
                className="h-10 w-full rounded-md border border-line px-3 text-sm text-ink"
                value={form.customer_name}
                onChange={(event) => setForm((current) => ({ ...current, customer_name: event.target.value }))}
                disabled={busy !== ""}
              />
            </Field>
            <Field label="Customer email">
              <input
                className="h-10 w-full rounded-md border border-line px-3 text-sm text-ink"
                value={form.customer_email}
                onChange={(event) => setForm((current) => ({ ...current, customer_email: event.target.value }))}
                disabled={busy !== ""}
              />
            </Field>
            <div className="md:col-span-2">
              <Field label="Notes">
                <textarea
                  className="min-h-20 w-full rounded-md border border-line px-3 py-2 text-sm text-ink"
                  value={form.notes}
                  onChange={(event) => setForm((current) => ({ ...current, notes: event.target.value }))}
                  disabled={busy !== ""}
                />
              </Field>
            </div>
          </div>

          <div className="mt-5 flex flex-wrap gap-3">
            <Button type="button" onClick={() => void createTestBooking()} disabled={!canCreateTest}>
              <CalendarCheck className="h-4 w-4" />
              {busy === "test" ? "Creating..." : "Create Test Booking"}
            </Button>
            <Button type="button" variant="danger" onClick={() => void cancelTestBooking()} disabled={!canCancelTest}>
              <Ban className="h-4 w-4" />
              {busy === "cancel-test" ? "Cancelling..." : "Cancel Test Booking"}
            </Button>
          </div>

          <LatestTest latest={latestTest} />
        </Card>
      </div>

      <Card>
        <CardTitle>Recent sync logs</CardTitle>
        <CardDescription>Provider sync activity and failures are stored for troubleshooting.</CardDescription>
        {status?.sync_logs?.length ? (
          <div className="mt-5 overflow-x-auto rounded-md border border-line">
            <table className="w-full min-w-[680px] text-left text-sm">
              <thead className="bg-slate-50 text-xs uppercase text-muted">
                <tr>
                  <th className="px-4 py-3">Type</th>
                  <th className="px-4 py-3">Status</th>
                  <th className="px-4 py-3">Started</th>
                  <th className="px-4 py-3">Message</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-line bg-white">
                {status.sync_logs.map((log) => (
                  <tr key={log.id}>
                    <td className="px-4 py-3 font-medium text-ink">{log.sync_type}</td>
                    <td className="px-4 py-3">
                      <Badge value={log.status === "succeeded" ? "active" : log.status} />
                    </td>
                    <td className="px-4 py-3 text-muted">{new Date(log.started_at).toLocaleString()}</td>
                    <td className="px-4 py-3 text-muted">{log.message || "-"}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : (
          <div className="mt-5 rounded-md border border-line p-6 text-center">
            <RefreshCcw className="mx-auto h-5 w-5 text-muted" />
            <div className="mt-3 text-sm font-semibold text-ink">No sync logs yet</div>
            <div className="mt-1 text-sm leading-6 text-muted">
              {connection?.id
                ? "Run Sync to import Square Appointments records and record the sync result."
                : "Connect Square Appointments before sync activity can be recorded."}
            </div>
          </div>
        )}
      </Card>
    </div>
  );
}

function ProviderConfigurationPanel({
  activeTab,
  busy,
  configs,
  openAIForm,
  setActiveTab,
  setOpenAIForm,
  setSquareForm,
  setTwilioForm,
  squareForm,
  twilioForm,
  onSaveOpenAI,
  onSaveSquare,
  onSaveTwilio
}: {
  activeTab: ConfigTab;
  busy: string;
  configs: IntegrationConfigs | null;
  openAIForm: OpenAIConfigForm;
  setActiveTab: Dispatch<SetStateAction<ConfigTab>>;
  setOpenAIForm: Dispatch<SetStateAction<OpenAIConfigForm>>;
  setSquareForm: Dispatch<SetStateAction<SquareConfigForm>>;
  setTwilioForm: Dispatch<SetStateAction<TwilioConfigForm>>;
  squareForm: SquareConfigForm;
  twilioForm: TwilioConfigForm;
  onSaveOpenAI: () => void;
  onSaveSquare: () => void;
  onSaveTwilio: () => void;
}) {
  const square = configs?.square;
  const twilio = configs?.twilio;
  const openAI = configs?.openai;

  return (
    <Card>
      <div className="flex flex-col justify-between gap-4 lg:flex-row lg:items-start">
        <div>
          <CardTitle>Provider configuration</CardTitle>
          <CardDescription>
            Store provider credentials securely in this dashboard. Secret values are write-only.
          </CardDescription>
        </div>
        <div className="flex flex-wrap gap-2">
          <ConfigTabButton active={activeTab === "square"} icon={<KeyRound className="h-4 w-4" />} label="Square" onClick={() => setActiveTab("square")} />
          <ConfigTabButton active={activeTab === "twilio"} icon={<PhoneCall className="h-4 w-4" />} label="Twilio" onClick={() => setActiveTab("twilio")} />
          <ConfigTabButton active={activeTab === "openai"} icon={<Bot className="h-4 w-4" />} label="OpenAI" onClick={() => setActiveTab("openai")} />
        </div>
      </div>

      <div className="mt-5 grid gap-3 md:grid-cols-3">
        <ConfigStatusBlock
          label="Square Appointments"
          status={square?.configured ? "configured" : "needs_config"}
          detail={square?.configured ? "OAuth can be started." : "Client ID, secret, and redirect URL are required."}
        />
        <ConfigStatusBlock
          label="Twilio Voice"
          status={twilio?.configured ? "configured" : "needs_config"}
          detail={twilio?.configured ? `Webhook signatures can be verified. Transport: ${twilio.voice_transport || "recording"}.` : "Auth token is required."}
        />
        <ConfigStatusBlock
          label="OpenAI Voice AI"
          status={openAI?.configured ? "configured" : openAI?.enabled ? "needs_config" : "disabled"}
          detail={openAI?.configured
            ? openAI.realtime_enabled && openAI.speech_output_mode === "streaming_tts"
              ? "STT and low-latency streaming speech are ready."
              : `STT, reply, speech, and${openAI.realtime_enabled ? "" : " optional"} realtime settings are ready.`
            : openAI?.enabled ? "API key and models are required." : "External AI voice is off."}
        />
      </div>

      {activeTab === "square" ? (
        <div className="mt-6 space-y-5">
          <div className="grid gap-4 md:grid-cols-2">
            <Field label="Environment">
              <select
                className="h-10 w-full rounded-md border border-line bg-white px-3 text-sm text-ink"
                value={squareForm.environment}
                onChange={(event) => setSquareForm((current) => ({ ...current, environment: event.target.value }))}
                disabled={busy !== ""}
              >
                <option value="sandbox">Sandbox</option>
                <option value="production">Production</option>
              </select>
            </Field>
            <Field label="Square client ID">
              <input
                className="h-10 w-full rounded-md border border-line px-3 text-sm text-ink"
                value={squareForm.client_id}
                onChange={(event) => setSquareForm((current) => ({ ...current, client_id: event.target.value }))}
                disabled={busy !== ""}
              />
            </Field>
            <Field label="Square client secret">
              <input
                className="h-10 w-full rounded-md border border-line px-3 text-sm text-ink"
                type="password"
                value={squareForm.client_secret}
                placeholder={square?.client_secret_configured ? "Stored - leave blank to keep" : "Paste client secret"}
                onChange={(event) => setSquareForm((current) => ({ ...current, client_secret: event.target.value, clear_client_secret: false }))}
                disabled={busy !== "" || squareForm.clear_client_secret}
              />
            </Field>
            <SecretControl
              checked={squareForm.clear_client_secret}
              configured={Boolean(square?.client_secret_configured)}
              source={square?.client_secret_source}
              label="Clear stored Square client secret"
              onChange={(checked) => setSquareForm((current) => ({ ...current, clear_client_secret: checked, client_secret: checked ? "" : current.client_secret }))}
            />
            <Field label="Redirect URL">
              <input
                className="h-10 w-full rounded-md border border-line px-3 text-sm text-ink"
                value={squareForm.redirect_url}
                onChange={(event) => setSquareForm((current) => ({ ...current, redirect_url: event.target.value }))}
                disabled={busy !== ""}
              />
            </Field>
            <Field label="Square API version">
              <input
                className="h-10 w-full rounded-md border border-line px-3 text-sm text-ink"
                value={squareForm.api_version}
                onChange={(event) => setSquareForm((current) => ({ ...current, api_version: event.target.value }))}
                disabled={busy !== ""}
              />
            </Field>
            <div className="md:col-span-2">
              <Field label="Square API base URL">
                <input
                  className="h-10 w-full rounded-md border border-line px-3 text-sm text-ink"
                  value={squareForm.api_base_url}
                  placeholder="Optional override"
                  onChange={(event) => setSquareForm((current) => ({ ...current, api_base_url: event.target.value }))}
                  disabled={busy !== ""}
                />
              </Field>
            </div>
            <div className="md:col-span-2 border-t border-line pt-4">
              <div className="flex flex-col justify-between gap-2 sm:flex-row sm:items-start">
                <div>
                  <div className="text-sm font-semibold text-ink">Booking webhook verification</div>
                  <div className="mt-1 text-xs leading-5 text-muted">
                    The notification URL and write-only signature key let this API verify inbound Square booking events. Stored credentials do not prove that a Square webhook subscription exists or that recent deliveries succeeded.
                  </div>
                </div>
                <Badge value={square?.webhook_configured ? "verification_ready" : "needs_config"} />
              </div>
            </div>
            <div className="md:col-span-2">
              <Field label="Webhook notification URL">
                <input
                  className="h-10 w-full rounded-md border border-line px-3 text-sm text-ink"
                  type="url"
                  value={squareForm.webhook_notification_url}
                  placeholder="https://api.example.com/api/integrations/square/webhook"
                  onChange={(event) =>
                    setSquareForm((current) => ({ ...current, webhook_notification_url: event.target.value }))
                  }
                  disabled={busy !== ""}
                />
              </Field>
            </div>
            <Field label="Square webhook signature key">
              <input
                className="h-10 w-full rounded-md border border-line px-3 text-sm text-ink"
                type="password"
                value={squareForm.webhook_signature_key}
                placeholder={
                  square?.webhook_signature_key_configured ? "Stored - leave blank to keep" : "Paste webhook signature key"
                }
                onChange={(event) =>
                  setSquareForm((current) => ({
                    ...current,
                    webhook_signature_key: event.target.value,
                    clear_webhook_signature_key: false
                  }))
                }
                disabled={busy !== "" || squareForm.clear_webhook_signature_key}
              />
            </Field>
            <SecretControl
              checked={squareForm.clear_webhook_signature_key}
              configured={Boolean(square?.webhook_signature_key_configured)}
              source={square?.webhook_signature_key_source}
              label="Clear stored Square webhook signature key"
              onChange={(checked) =>
                setSquareForm((current) => ({
                  ...current,
                  clear_webhook_signature_key: checked,
                  webhook_signature_key: checked ? "" : current.webhook_signature_key
                }))
              }
            />
          </div>
          <ConfigActions
            busy={busy === "save-square-config"}
            configured={Boolean(square?.configured)}
            label="Save Square settings"
            onSave={onSaveSquare}
          />
        </div>
      ) : null}

      {activeTab === "twilio" ? (
        <div className="mt-6 space-y-5">
          <div className="grid gap-4 md:grid-cols-2">
            <Field label="Public API base URL">
              <input
                className="h-10 w-full rounded-md border border-line px-3 text-sm text-ink"
                value={twilioForm.public_base_url}
                placeholder="https://api.example.com"
                onChange={(event) => setTwilioForm((current) => ({ ...current, public_base_url: event.target.value }))}
                disabled={busy !== ""}
              />
            </Field>
            <Field label="Twilio auth token">
              <input
                className="h-10 w-full rounded-md border border-line px-3 text-sm text-ink"
                type="password"
                value={twilioForm.auth_token}
                placeholder={twilio?.auth_token_configured ? "Stored - leave blank to keep" : "Paste auth token"}
                onChange={(event) => setTwilioForm((current) => ({ ...current, auth_token: event.target.value, clear_auth_token: false }))}
                disabled={busy !== "" || twilioForm.clear_auth_token}
              />
            </Field>
            <SecretControl
              checked={twilioForm.clear_auth_token}
              configured={Boolean(twilio?.auth_token_configured)}
              source={twilio?.auth_token_source}
              label="Clear stored Twilio auth token"
              onChange={(checked) => setTwilioForm((current) => ({ ...current, clear_auth_token: checked, auth_token: checked ? "" : current.auth_token }))}
            />
            <Field label="Voice transport">
              <select
                className="h-10 w-full rounded-md border border-line bg-white px-3 text-sm text-ink"
                value={twilioForm.voice_transport}
                onChange={(event) => setTwilioForm((current) => ({ ...current, voice_transport: event.target.value }))}
                disabled={busy !== ""}
              >
                <option value="recording">Recording fallback</option>
                <option value="realtime_stream">Realtime stream</option>
              </select>
            </Field>
            <Field label="Incoming path">
              <input
                className="h-10 w-full rounded-md border border-line px-3 text-sm text-ink"
                value={twilioForm.incoming_path}
                onChange={(event) => setTwilioForm((current) => ({ ...current, incoming_path: event.target.value }))}
                disabled={busy !== ""}
              />
            </Field>
            <Field label="Turn path">
              <input
                className="h-10 w-full rounded-md border border-line px-3 text-sm text-ink"
                value={twilioForm.turn_path}
                onChange={(event) => setTwilioForm((current) => ({ ...current, turn_path: event.target.value }))}
                disabled={busy !== ""}
              />
            </Field>
            <Field label="Recording path">
              <input
                className="h-10 w-full rounded-md border border-line px-3 text-sm text-ink"
                value={twilioForm.recording_path}
                onChange={(event) => setTwilioForm((current) => ({ ...current, recording_path: event.target.value }))}
                disabled={busy !== ""}
              />
            </Field>
            <Field label="Stream path">
              <input
                className="h-10 w-full rounded-md border border-line px-3 text-sm text-ink"
                value={twilioForm.stream_path}
                onChange={(event) => setTwilioForm((current) => ({ ...current, stream_path: event.target.value }))}
                disabled={busy !== ""}
              />
            </Field>
          </div>
          <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-4">
            <ReadOnlyValue label="Incoming webhook" value={twilio?.inbound_webhook_url || twilioForm.incoming_path} />
            <ReadOnlyValue label="Turn webhook" value={twilio?.turn_webhook_url || twilioForm.turn_path} />
            <ReadOnlyValue label="Recording webhook" value={twilio?.recording_webhook_url || twilioForm.recording_path} />
            <ReadOnlyValue label="Realtime stream" value={twilio?.stream_webhook_url || twilioForm.stream_path} />
          </div>
          <ConfigActions
            busy={busy === "save-twilio-config"}
            configured={Boolean(twilio?.configured)}
            label="Save Twilio settings"
            onSave={onSaveTwilio}
          />
        </div>
      ) : null}

      {activeTab === "openai" ? (
        <div className="mt-6 space-y-5">
          <label className="flex items-center gap-3 rounded-md border border-line p-3 text-sm font-medium text-ink">
            <input
              type="checkbox"
              checked={openAIForm.enabled}
              onChange={(event) => setOpenAIForm((current) => ({ ...current, enabled: event.target.checked }))}
              disabled={busy !== ""}
            />
            Enable OpenAI voice AI
          </label>
          <div className="grid gap-4 md:grid-cols-2">
            <Field label="OpenAI API key">
              <input
                className="h-10 w-full rounded-md border border-line px-3 text-sm text-ink"
                type="password"
                value={openAIForm.api_key}
                placeholder={openAI?.api_key_configured ? "Stored - leave blank to keep" : "Paste API key"}
                onChange={(event) => setOpenAIForm((current) => ({ ...current, api_key: event.target.value, clear_api_key: false }))}
                disabled={busy !== "" || openAIForm.clear_api_key || !openAIForm.enabled}
              />
            </Field>
            <SecretControl
              checked={openAIForm.clear_api_key}
              configured={Boolean(openAI?.api_key_configured)}
              source={openAI?.api_key_source}
              label="Clear stored OpenAI API key"
              onChange={(checked) => setOpenAIForm((current) => ({ ...current, clear_api_key: checked, api_key: checked ? "" : current.api_key }))}
            />
            <Field label="Base URL">
              <input
                className="h-10 w-full rounded-md border border-line px-3 text-sm text-ink"
                value={openAIForm.base_url}
                onChange={(event) => setOpenAIForm((current) => ({ ...current, base_url: event.target.value }))}
                disabled={busy !== "" || !openAIForm.enabled}
              />
            </Field>
            <Field label="Transcription model">
              <input
                className="h-10 w-full rounded-md border border-line px-3 text-sm text-ink"
                value={openAIForm.transcription_model}
                onChange={(event) => setOpenAIForm((current) => ({ ...current, transcription_model: event.target.value }))}
                disabled={busy !== "" || !openAIForm.enabled}
              />
            </Field>
            <Field label="Reply model">
              <input
                className="h-10 w-full rounded-md border border-line px-3 text-sm text-ink"
                value={openAIForm.reply_model}
                onChange={(event) => setOpenAIForm((current) => ({ ...current, reply_model: event.target.value }))}
                disabled={busy !== "" || !openAIForm.enabled}
              />
            </Field>
            <Field label="Speech model">
              <input
                className="h-10 w-full rounded-md border border-line px-3 text-sm text-ink"
                value={openAIForm.speech_model}
                onChange={(event) => setOpenAIForm((current) => ({ ...current, speech_model: event.target.value }))}
                disabled={busy !== "" || !openAIForm.enabled}
              />
            </Field>
            <Field label="Speech voice">
              <input
                className="h-10 w-full rounded-md border border-line px-3 text-sm text-ink"
                value={openAIForm.speech_voice}
                onChange={(event) => setOpenAIForm((current) => ({ ...current, speech_voice: event.target.value }))}
                disabled={busy !== "" || !openAIForm.enabled}
              />
            </Field>
            <Field label="Speech output mode">
              <select
                className="h-10 w-full rounded-md border border-line bg-panel px-3 text-sm text-ink"
                value={openAIForm.speech_output_mode}
                onChange={(event) => setOpenAIForm((current) => ({
                  ...current,
                  speech_output_mode: event.target.value as OpenAIConfigForm["speech_output_mode"]
                }))}
                disabled={busy !== "" || !openAIForm.enabled}
              >
                <option value="streaming_tts">Low-latency streaming TTS (recommended)</option>
                <option value="buffered_realtime">Buffered Realtime output (legacy fallback)</option>
              </select>
              <p className="mt-1 text-xs leading-5 text-muted">
                Streaming TTS sends approved backend speech to the caller as audio arrives. The legacy mode waits for full Realtime output validation.
              </p>
            </Field>
          </div>
          <div className="rounded-md border border-line p-4">
            <div className="flex flex-col justify-between gap-3 md:flex-row md:items-start">
              <div>
                <div className="text-sm font-semibold text-ink">Realtime voice</div>
                <div className="mt-1 text-xs leading-5 text-muted">
                  Used only when Twilio Voice transport is set to Realtime stream. Booking and confirmations still run through the backend.
                </div>
              </div>
              <label className="flex items-center gap-2 text-sm font-medium text-ink">
                <input
                  type="checkbox"
                  checked={openAIForm.realtime_enabled}
                  onChange={(event) => setOpenAIForm((current) => ({ ...current, realtime_enabled: event.target.checked }))}
                  disabled={busy !== "" || !openAIForm.enabled}
                />
                Enable realtime
              </label>
            </div>
            <div className="mt-4 grid gap-4 md:grid-cols-2">
              <Field label="Realtime model">
                <input
                  className="h-10 w-full rounded-md border border-line px-3 text-sm text-ink"
                  value={openAIForm.realtime_model}
                  onChange={(event) => setOpenAIForm((current) => ({ ...current, realtime_model: event.target.value }))}
                  disabled={busy !== "" || !openAIForm.enabled || !openAIForm.realtime_enabled}
                />
              </Field>
              <Field label="Realtime voice">
                <input
                  className="h-10 w-full rounded-md border border-line px-3 text-sm text-ink"
                  value={openAIForm.realtime_voice}
                  onChange={(event) => setOpenAIForm((current) => ({ ...current, realtime_voice: event.target.value }))}
                  disabled={busy !== "" || !openAIForm.enabled || !openAIForm.realtime_enabled}
                />
              </Field>
              <Field label="Noise profile">
                <select
                  className="h-10 w-full rounded-md border border-line bg-panel px-3 text-sm text-ink"
                  value={openAIForm.realtime_noise_profile}
                  onChange={(event) => setOpenAIForm((current) => ({ ...current, realtime_noise_profile: event.target.value }))}
                  disabled={busy !== "" || !openAIForm.enabled || !openAIForm.realtime_enabled}
                >
                  <option value="noisy_salon">Noisy salon (recommended)</option>
                  <option value="balanced">Balanced</option>
                  <option value="quiet_room">Quiet room</option>
                </select>
                <p className="mt-1 text-xs leading-5 text-muted">
                  Reduces accidental interruption from salon background noise. Caller interruption still works after AI audio starts.
                </p>
              </Field>
              <div className="md:col-span-2">
                <Field label="Realtime instructions">
                  <textarea
                    className="min-h-24 w-full rounded-md border border-line px-3 py-2 text-sm text-ink"
                    value={openAIForm.realtime_instructions}
                    onChange={(event) => setOpenAIForm((current) => ({ ...current, realtime_instructions: event.target.value }))}
                    disabled={busy !== "" || !openAIForm.enabled || !openAIForm.realtime_enabled}
                    placeholder="Optional operating notes for the audio bridge. Do not put secrets here."
                  />
                </Field>
              </div>
            </div>
          </div>
          <ConfigActions
            busy={busy === "save-openai-config"}
            configured={Boolean(openAI?.configured)}
            label="Save OpenAI settings"
            onSave={onSaveOpenAI}
          />
        </div>
      ) : null}
    </Card>
  );
}

function ConfigTabButton({
  active,
  icon,
  label,
  onClick
}: {
  active: boolean;
  icon: ReactNode;
  label: string;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      className={`inline-flex h-10 items-center gap-2 rounded-md border px-3 text-sm font-semibold transition ${
        active ? "border-brand bg-teal-50 text-brand" : "border-line bg-white text-ink hover:bg-slate-50"
      }`}
      onClick={onClick}
    >
      {icon}
      {label}
    </button>
  );
}

function ConfigStatusBlock({ label, status, detail }: { label: string; status: string; detail: string }) {
  return (
    <div className="rounded-md border border-line p-3">
      <div className="flex items-center justify-between gap-3">
        <div className="text-sm font-semibold text-ink">{label}</div>
        <Badge value={status} />
      </div>
      <div className="mt-2 text-xs leading-5 text-muted">{detail}</div>
    </div>
  );
}

function SecretControl({
  checked,
  configured,
  label,
  onChange,
  source
}: {
  checked: boolean;
  configured: boolean;
  label: string;
  onChange: (checked: boolean) => void;
  source?: string;
}) {
  return (
    <div className="rounded-md border border-line p-3">
      <div className="flex items-start justify-between gap-3">
        <div>
          <div className="text-sm font-medium text-ink">Secret status</div>
          <div className="mt-1 text-xs leading-5 text-muted">
            {configured ? `Configured from ${secretSourceLabel(source)}.` : "No secret is configured."}
          </div>
        </div>
        <Badge value={configured ? "configured" : "missing"} />
      </div>
      <label className="mt-3 flex items-center gap-2 text-xs font-medium text-slate-700">
        <input type="checkbox" checked={checked} disabled={!configured || source !== "database"} onChange={(event) => onChange(event.target.checked)} />
        {label}
      </label>
    </div>
  );
}

function ReadOnlyValue({ label, value }: { label: string; value: string }) {
  return (
    <div className="min-w-0 rounded-md border border-line p-3">
      <div className="text-xs font-semibold uppercase text-muted">{label}</div>
      <div className="mt-2 break-all text-sm font-medium text-ink">{value || "-"}</div>
    </div>
  );
}

function ConfigActions({
  busy,
  configured,
  label,
  onSave
}: {
  busy: boolean;
  configured: boolean;
  label: string;
  onSave: () => void;
}) {
  return (
    <div className="flex flex-col justify-between gap-3 border-t border-line pt-4 md:flex-row md:items-center">
      <div className="text-sm text-muted">
        {configured ? "Configuration is ready. Blank secret fields keep the stored value." : "Save the required fields before dependent workflows can run."}
      </div>
      <Button type="button" onClick={onSave} disabled={busy}>
        <Save className="h-4 w-4" />
        {busy ? "Saving..." : label}
      </Button>
    </div>
  );
}

function squareConfigToForm(config?: SquareIntegrationConfig): SquareConfigForm {
  if (!config) return defaultSquareConfigForm;
  return {
    environment: config.environment || "sandbox",
    client_id: config.client_id || "",
    client_secret: "",
    clear_client_secret: false,
    redirect_url: config.redirect_url || defaultSquareConfigForm.redirect_url,
    api_version: config.api_version || defaultSquareConfigForm.api_version,
    api_base_url: config.api_base_url || "",
    webhook_notification_url: config.webhook_notification_url || "",
    webhook_signature_key: "",
    clear_webhook_signature_key: false
  };
}

function twilioConfigToForm(config?: TwilioIntegrationConfig): TwilioConfigForm {
  if (!config) return defaultTwilioConfigForm;
  return {
    public_base_url: config.public_base_url || "",
    auth_token: "",
    clear_auth_token: false,
    incoming_path: config.incoming_path || defaultTwilioConfigForm.incoming_path,
    turn_path: config.turn_path || defaultTwilioConfigForm.turn_path,
    recording_path: config.recording_path || defaultTwilioConfigForm.recording_path,
    stream_path: config.stream_path || defaultTwilioConfigForm.stream_path,
    voice_transport: config.voice_transport || defaultTwilioConfigForm.voice_transport
  };
}

function openAIConfigToForm(config?: OpenAIIntegrationConfig): OpenAIConfigForm {
  if (!config) return defaultOpenAIConfigForm;
  return {
    enabled: config.enabled,
    api_key: "",
    clear_api_key: false,
    base_url: config.base_url || defaultOpenAIConfigForm.base_url,
    transcription_model: config.transcription_model || defaultOpenAIConfigForm.transcription_model,
    reply_model: config.reply_model || defaultOpenAIConfigForm.reply_model,
    speech_model: config.speech_model || defaultOpenAIConfigForm.speech_model,
    speech_voice: config.speech_voice || defaultOpenAIConfigForm.speech_voice,
    speech_output_mode: config.speech_output_mode === "buffered_realtime" ? "buffered_realtime" : "streaming_tts",
    realtime_enabled: config.realtime_enabled,
    realtime_model: config.realtime_model || defaultOpenAIConfigForm.realtime_model,
    realtime_voice: config.realtime_voice || defaultOpenAIConfigForm.realtime_voice,
    realtime_noise_profile: config.realtime_noise_profile || defaultOpenAIConfigForm.realtime_noise_profile,
    realtime_instructions: config.realtime_instructions || ""
  };
}

function emptyIntegrationConfigs(): IntegrationConfigs {
  return {
    square: {
      provider: "square",
      configured: false,
      environment: defaultSquareConfigForm.environment,
      client_id: defaultSquareConfigForm.client_id,
      redirect_url: defaultSquareConfigForm.redirect_url,
      api_version: defaultSquareConfigForm.api_version,
      api_base_url: defaultSquareConfigForm.api_base_url,
      client_secret_configured: false,
      client_secret_source: "none",
      webhook_notification_url: defaultSquareConfigForm.webhook_notification_url,
      webhook_configured: false,
      webhook_signature_key_configured: false,
      webhook_signature_key_source: "none"
    },
    twilio: {
      provider: "twilio",
      configured: false,
      public_base_url: defaultTwilioConfigForm.public_base_url,
      incoming_path: defaultTwilioConfigForm.incoming_path,
      turn_path: defaultTwilioConfigForm.turn_path,
      recording_path: defaultTwilioConfigForm.recording_path,
      stream_path: defaultTwilioConfigForm.stream_path,
      voice_transport: defaultTwilioConfigForm.voice_transport,
      inbound_webhook_url: defaultTwilioConfigForm.incoming_path,
      turn_webhook_url: defaultTwilioConfigForm.turn_path,
      recording_webhook_url: defaultTwilioConfigForm.recording_path,
      stream_webhook_url: defaultTwilioConfigForm.stream_path,
      auth_token_configured: false,
      auth_token_source: "none"
    },
    openai: {
      provider: "openai",
      enabled: false,
      configured: false,
      base_url: defaultOpenAIConfigForm.base_url,
      transcription_model: defaultOpenAIConfigForm.transcription_model,
      reply_model: defaultOpenAIConfigForm.reply_model,
      speech_model: defaultOpenAIConfigForm.speech_model,
      speech_voice: defaultOpenAIConfigForm.speech_voice,
      speech_output_mode: defaultOpenAIConfigForm.speech_output_mode,
      realtime_enabled: defaultOpenAIConfigForm.realtime_enabled,
      realtime_model: defaultOpenAIConfigForm.realtime_model,
      realtime_voice: defaultOpenAIConfigForm.realtime_voice,
      realtime_noise_profile: defaultOpenAIConfigForm.realtime_noise_profile,
      realtime_instructions: defaultOpenAIConfigForm.realtime_instructions,
      api_key_configured: false,
      api_key_source: "none"
    }
  };
}

function secretSourceLabel(source?: string) {
  if (source === "database") return "dashboard storage";
  if (source === "environment") return "environment fallback";
  return "no source";
}

function ReadinessOverviewPanel({
  busy,
  connection,
  latestTest,
  onConnect,
  onSync,
  readiness,
  services,
  staff,
  squareConfigConfigured,
  switchReadiness,
  syncLogCount
}: {
  busy: string;
  connection?: POSConnection;
  latestTest?: TestBookingRecord;
  onConnect: () => void;
  onSync: () => void;
  readiness?: SquareReadiness;
  services: POSService[];
  staff: POSStaffMember[];
  squareConfigConfigured: boolean;
  switchReadiness: ProviderSwitchReadiness | null;
  syncLogCount: number;
}) {
  const squareStatus = connection?.error_message ? "error" : connection?.id ? "connected" : "not_connected";
  const squareMessage = squareConfigConfigured
    ? squareOverviewMessage(connection, syncLogCount)
    : "Save Square Appointments app credentials before starting OAuth.";
  const bookableServiceCount = readiness?.service_count ?? services.filter(serviceIsBookable).length;
  const bookableStaffCount = readiness?.staff_count ?? staff.filter(staffIsBookable).length;
  const syncFailedCount =
    switchReadiness?.mapping.sync_failed_count ??
    services.filter((service) => service.sync_status === "sync_failed").length +
      staff.filter((member) => member.sync_status === "sync_failed").length;
  const unmappedCount =
    switchReadiness
      ? switchReadiness.mapping.unmapped_service_count + switchReadiness.mapping.unmapped_staff_count
      : services.filter((service) => service.sync_status === "unmapped" || service.sync_status === "local_only").length +
        staff.filter((member) => member.sync_status === "unmapped" || member.sync_status === "local_only").length;
  const bookingDataReady = bookableServiceCount > 0 && bookableStaffCount > 0;
  const bookingDataMessage = bookingDataOverviewMessage(readiness, bookableServiceCount, bookableStaffCount, syncFailedCount);
  const testGate = testBookingGateState(readiness, latestTest);
  const switchStatus = switchReadiness?.can_start_switch ? "pending" : "disabled";
  const switchMessage =
    switchReadiness?.blocked_reason ||
    firstIncompleteCheckMessage(switchReadiness?.checks) ||
    "Square Appointments is the only native POS adapter in the current production release.";

  return (
    <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-4">
      <OverviewCard
        action={
          connection?.id ? (
            <Button type="button" variant="secondary" onClick={onSync} disabled={busy !== ""}>
              <RefreshCcw className="h-4 w-4" />
              {busy === "sync" ? "Syncing..." : "Sync"}
            </Button>
          ) : (
            <Button type="button" onClick={onConnect} disabled={busy !== "" || !squareConfigConfigured}>
              <ExternalLink className="h-4 w-4" />
              {busy === "connect" ? "Opening..." : squareConfigConfigured ? "Connect Square" : "Config required"}
            </Button>
          )
        }
        description={squareMessage}
        details={[
          { label: "Connection", value: connection?.id ? connection.status || "connected" : "Not connected" },
          { label: "Location", value: connection?.location_id ? "Selected" : "Not selected" },
          { label: "Latest sync", value: formatOptionalDateTime(connection?.last_sync_at) },
          { label: "Sync logs", value: String(syncLogCount) }
        ]}
        icon={<ExternalLink className="h-5 w-5 text-brand" />}
        status={squareStatus}
        title="Square Appointments"
      />

      <OverviewCard
        action={
          <a
            className="inline-flex h-10 items-center justify-center rounded-md border border-line bg-white px-4 text-sm font-semibold text-ink hover:bg-slate-50"
            href="/dashboard/services"
          >
            Review services
          </a>
        }
        description={bookingDataMessage}
        details={[
          { label: "Bookable services", value: String(bookableServiceCount) },
          { label: "Bookable staff", value: String(bookableStaffCount) },
          { label: "Unmapped records", value: String(unmappedCount) },
          { label: "Sync failures", value: String(syncFailedCount) }
        ]}
        icon={<CheckCircle2 className="h-5 w-5 text-brand" />}
        status={bookingDataReady ? "ready" : "blocked"}
        title="Booking-ready data"
      />

      <OverviewCard
        description={testGate.message}
        details={[
          { label: "Latest test", value: latestTest?.appointment_status || latestTest?.status || "Not created" },
          { label: "POS booking", value: latestTest?.pos_booking_id ? "Returned" : "Not returned" },
          { label: "AI booking", value: readiness?.ai_enabled ? "Enabled" : "Disabled" }
        ]}
        icon={<CalendarCheck className="h-5 w-5 text-brand" />}
        status={testGate.status}
        title="Test booking gate"
      />

      <OverviewCard
        action={
          <Button type="button" disabled>
            <Workflow className="h-4 w-4" />
            Start switch run
          </Button>
        }
        description={switchMessage}
        details={[
          { label: "Active provider", value: switchReadiness?.active_provider_label || "Square Appointments" },
          { label: "Installed adapters", value: String(switchReadiness?.installed_providers.length ?? 1) },
          { label: "Alternate adapters", value: String(switchReadiness?.installed_providers.filter((provider) => !provider.active).length ?? 0) }
        ]}
        icon={<Workflow className="h-5 w-5 text-brand" />}
        status={switchStatus}
        title="Provider switch"
      />
    </div>
  );
}

function OverviewCard({
  action,
  description,
  details,
  icon,
  status,
  title
}: {
  action?: ReactNode;
  description: string;
  details: { label: string; value: string }[];
  icon: ReactNode;
  status: string;
  title: string;
}) {
  return (
    <Card className={overviewCardClass(status)}>
      <div className="flex items-start justify-between gap-3">
        <div className="flex min-w-0 gap-3">
          <div className="mt-0.5 flex-none">{icon}</div>
          <div className="min-w-0">
            <CardTitle>{title}</CardTitle>
            <CardDescription className={overviewDescriptionClass(status)}>{description}</CardDescription>
          </div>
        </div>
        <Badge value={status} />
      </div>
      <dl className="mt-4 grid gap-3 text-sm">
        {details.map((item) => (
          <div key={item.label}>
            <dt className="text-xs font-semibold uppercase tracking-wide text-muted">{item.label}</dt>
            <dd className="mt-1 break-words font-medium text-ink">{item.value}</dd>
          </div>
        ))}
      </dl>
      {action ? <div className="mt-4">{action}</div> : null}
    </Card>
  );
}

function TestBookingGate({
  bookableServiceCount,
  bookableStaffCount,
  latest,
  readiness
}: {
  bookableServiceCount: number;
  bookableStaffCount: number;
  latest?: TestBookingRecord;
  readiness?: SquareReadiness;
}) {
  const gate = testBookingGateState(readiness, latest);
  const ready = gate.status === "ready";
  return (
    <div className={ready ? "mt-5 rounded-md border border-emerald-200 bg-emerald-50 p-4" : "mt-5 rounded-md border border-amber-200 bg-amber-50 p-4"}>
      <div className="flex gap-3">
        {ready ? (
          <CheckCircle2 className="mt-0.5 h-5 w-5 flex-none text-emerald-700" />
        ) : (
          <AlertTriangle className="mt-0.5 h-5 w-5 flex-none text-amber-700" />
        )}
        <div className="min-w-0">
          <div className="flex flex-wrap items-center gap-2">
            <div className="text-sm font-semibold text-ink">Optional Square smoke test</div>
            <Badge value={gate.status} />
          </div>
          <div className={ready ? "mt-1 text-sm leading-6 text-emerald-800" : "mt-1 text-sm leading-6 text-amber-900"}>
            {gate.message}
          </div>
          <div className="mt-3 grid gap-3 text-sm sm:grid-cols-3">
            <Info label="Bookable services" value={String(bookableServiceCount)} />
            <Info label="Bookable staff" value={String(bookableStaffCount)} />
            <Info label="Latest POS booking" value={latest?.pos_booking_id ? "Returned" : "Not returned"} />
          </div>
        </div>
      </div>
    </div>
  );
}

function ProviderSwitchReadinessPanel({
  busy,
  dryRunReadiness,
  onRefresh,
  onUpdateMatch,
  readiness,
  switchRun
}: {
  busy: string;
  dryRunReadiness: ProviderSwitchDryRunReadiness | null;
  onRefresh: () => void;
  onUpdateMatch: (matchID: string, matchStatus: string) => void;
  readiness: ProviderSwitchReadiness | null;
  switchRun: ProviderSwitchRun | null;
}) {
  if (!readiness) {
    return (
      <Card>
        <CardTitle>Provider switch readiness</CardTitle>
        <CardDescription>Provider switch checks load after a salon and active POS provider are available.</CardDescription>
      </Card>
    );
  }

  const allProviders = [...readiness.installed_providers, ...readiness.unavailable_providers];
  const workflowSteps = providerSwitchWorkflowSteps(readiness, switchRun, dryRunReadiness);
  const matchSummary = switchRun?.match_summary;
  const workflowBlockedReason =
    switchRun?.blocked_reason ||
    readiness.blocked_reason ||
    "No alternate production POS adapter is installed in this deployment.";
  return (
    <Card>
      <div className="flex flex-col justify-between gap-3 lg:flex-row lg:items-start">
        <div>
          <CardTitle>Provider switch readiness</CardTitle>
          <CardDescription>
            Keep canonical services, staff, and customers safe before any future POS provider switch.
          </CardDescription>
        </div>
        <Badge value={readiness.can_start_switch ? "ready" : "disabled"} />
      </div>

      <div className="mt-5 grid gap-4 md:grid-cols-2 xl:grid-cols-5">
        <Info label="Current provider" value={readiness.active_provider_label || readiness.active_provider} />
        <Info label="Bookable services" value={String(readiness.mapping.bookable_service_count)} />
        <Info label="Bookable staff" value={String(readiness.mapping.bookable_staff_count)} />
        <Info label="Customer links" value={String(readiness.mapping.linked_customer_count)} />
        <Info label="Unmapped records" value={String(readiness.mapping.unmapped_service_count + readiness.mapping.unmapped_staff_count)} />
      </div>

      <div className="mt-5 grid gap-4 lg:grid-cols-[0.95fr_1.05fr]">
        <div className="rounded-md border border-line p-4">
          <div className="text-sm font-semibold text-ink">Available adapters</div>
          <div className="mt-3 space-y-3">
            {allProviders.map((provider) => (
              <div key={provider.provider || provider.label} className="rounded-md border border-line p-3">
                <div className="flex flex-col justify-between gap-2 sm:flex-row sm:items-start">
                  <div>
                    <div className="text-sm font-medium text-ink">{provider.label}</div>
                    {provider.blocked_reason ? <div className="mt-1 text-xs leading-5 text-muted">{provider.blocked_reason}</div> : null}
                  </div>
                  <div className="flex flex-wrap gap-2">
                    <Badge value={provider.active ? "active" : provider.installed ? "installed" : "disabled"} />
                    <Badge value={provider.status || "not_connected"} />
                  </div>
                </div>
              </div>
            ))}
          </div>
        </div>

        <div className="rounded-md border border-line p-4">
          <div className="text-sm font-semibold text-ink">Readiness checks</div>
          <div className="mt-3 space-y-3">
            {readiness.checks.map((check) => (
              <div key={check.key} className="rounded-md border border-line p-3">
                <div className="flex items-start justify-between gap-3">
                  <div className="flex min-w-0 gap-3">
                    <CheckCircle2 className={check.complete ? "mt-0.5 h-5 w-5 flex-none text-brand" : "mt-0.5 h-5 w-5 flex-none text-slate-300"} />
                    <div>
                      <div className="text-sm font-medium text-ink">{check.label}</div>
                      {!check.complete && check.message ? <div className="mt-1 text-xs leading-5 text-muted">{check.message}</div> : null}
                    </div>
                  </div>
                  <Badge value={check.complete ? "active" : "disabled"} />
                </div>
              </div>
            ))}
          </div>
        </div>
      </div>

      <div className="mt-5 rounded-md border border-line p-4">
        <div className="flex flex-col justify-between gap-3 sm:flex-row sm:items-start">
          <div>
            <div className="text-sm font-semibold text-ink">Switch workflow</div>
            <div className="mt-1 text-xs leading-5 text-muted">
              {switchRun
                ? `Latest run targets ${switchRun.to_provider}. Activation remains disabled until a real alternate adapter and required mappings are ready.`
                : "No switch run has been created. The workflow is blocked until an alternate native POS adapter is installed."}
            </div>
          </div>
          <Badge value={switchRun?.status ?? "blocked"} />
        </div>

        <div className="mt-4 grid gap-3 md:grid-cols-3">
          <Info label="Suggested matches" value={String(matchSummary?.suggested ?? 0)} />
          <Info label="Conflicts" value={String(matchSummary?.conflicts ?? 0)} />
          <Info label="Unmatched" value={String(matchSummary?.unmatched ?? 0)} />
        </div>

        <div className="mt-4 grid gap-3 md:grid-cols-2 xl:grid-cols-4">
          {workflowSteps.map((step) => (
            <div key={step.key} className="rounded-md border border-line p-3">
              <div className="flex items-start justify-between gap-3">
                <div className="min-w-0">
                  <div className="text-sm font-medium text-ink">{step.label}</div>
                  <div className="mt-1 text-xs leading-5 text-muted">{step.message}</div>
                </div>
                <Badge value={step.complete ? "active" : step.status} />
              </div>
            </div>
          ))}
        </div>

        {workflowBlockedReason ? (
          <div className="mt-4 rounded-md border border-amber-200 bg-amber-50 px-3 py-2 text-xs leading-5 text-amber-900">
            {workflowBlockedReason}
          </div>
        ) : null}
      </div>

      <ProviderSwitchImportWizard
        dryRunReadiness={dryRunReadiness}
        readiness={readiness}
        switchRun={switchRun}
      />

      <ProviderSwitchDryRunChecklist
        dryRunReadiness={dryRunReadiness}
        switchRun={switchRun}
      />

      <ProviderSwitchMatchReview
        busy={busy}
        readiness={readiness}
        switchRun={switchRun}
        onUpdateMatch={onUpdateMatch}
      />

      {readiness.blocked_reason ? (
        <div className="mt-5 rounded-md border border-amber-200 bg-amber-50 px-4 py-3 text-sm leading-6 text-amber-900">
          {readiness.blocked_reason}
        </div>
      ) : null}

      <div className="mt-5 flex flex-wrap gap-3">
        <Button type="button" variant="secondary" onClick={onRefresh} disabled={busy !== ""}>
          <RefreshCcw className="h-4 w-4" />
          Refresh readiness
        </Button>
        <Button type="button" disabled>
          Start switch run
        </Button>
      </div>
    </Card>
  );
}

function ProviderSwitchImportWizard({
  dryRunReadiness,
  readiness,
  switchRun
}: {
  dryRunReadiness: ProviderSwitchDryRunReadiness | null;
  readiness: ProviderSwitchReadiness;
  switchRun: ProviderSwitchRun | null;
}) {
  const targetProviders = readiness.installed_providers.filter((provider) => !provider.active);
  const selectedTarget = targetProviders[0]?.provider ?? "";
  const importSteps = providerSwitchImportSteps(readiness, switchRun, dryRunReadiness);
  const status = readiness.can_start_switch ? "pending" : "disabled";

  return (
    <div className="mt-5 rounded-md border border-line p-4">
      <div className="flex flex-col justify-between gap-3 sm:flex-row sm:items-start">
        <div>
          <div className="text-sm font-semibold text-ink">Import wizard</div>
          <div className="mt-1 text-xs leading-5 text-muted">
            Provider import stays gated until an alternate native POS adapter is installed.
          </div>
        </div>
        <Badge value={status} />
      </div>

      <div className="mt-4 grid gap-4 lg:grid-cols-[0.85fr_1.15fr]">
        <div className="rounded-md border border-line p-3">
          <label className="block">
            <span className="text-xs font-semibold uppercase tracking-wide text-muted">Target provider</span>
            <select
              className="mt-2 h-10 w-full rounded-md border border-line bg-white px-3 text-sm text-ink disabled:bg-slate-50 disabled:text-slate-500"
              defaultValue={selectedTarget}
              disabled={!readiness.can_start_switch || targetProviders.length === 0}
            >
              {targetProviders.length === 0 ? <option value="">No alternate POS adapter installed</option> : null}
              {targetProviders.map((provider) => (
                <option key={provider.provider} value={provider.provider}>
                  {provider.label}
                </option>
              ))}
            </select>
          </label>
          <div className="mt-3 text-xs leading-5 text-muted">
            Square Appointments is the only native POS adapter in the current production release.
          </div>
        </div>

        <div className="rounded-md border border-line p-3">
          <div className="text-sm font-semibold text-ink">Wizard steps</div>
          <div className="mt-3 grid gap-3 md:grid-cols-2">
            {importSteps.map((step) => (
              <div key={step.key} className="rounded-md border border-line p-3">
                <div className="flex items-start justify-between gap-3">
                  <div className="min-w-0">
                    <div className="text-sm font-medium text-ink">{step.label}</div>
                    <div className="mt-1 text-xs leading-5 text-muted">{step.message}</div>
                  </div>
                  <Badge value={step.complete ? "active" : step.status} />
                </div>
              </div>
            ))}
          </div>
        </div>
      </div>

      <div className="mt-4 rounded-md border border-amber-200 bg-amber-50 px-3 py-2 text-xs leading-5 text-amber-900">
        Provider switching is gated until a real alternate POS adapter is installed. Existing Square links and appointment history remain preserved.
      </div>

      <div className="mt-4">
        <Button type="button" disabled>
          <Workflow className="h-4 w-4" />
          Start import wizard
        </Button>
      </div>
    </div>
  );
}

function ProviderSwitchDryRunChecklist({
  dryRunReadiness,
  switchRun
}: {
  dryRunReadiness: ProviderSwitchDryRunReadiness | null;
  switchRun: ProviderSwitchRun | null;
}) {
  if (!switchRun) {
    return (
      <div className="mt-5 rounded-md border border-line p-4">
        <div className="flex flex-col justify-between gap-3 sm:flex-row sm:items-start">
          <div>
            <div className="text-sm font-semibold text-ink">Dry-run checklist</div>
            <div className="mt-1 text-xs leading-5 text-muted">
              Create a switch run before dry-run checks are available.
            </div>
          </div>
          <Badge value="disabled" />
        </div>
      </div>
    );
  }

  if (!dryRunReadiness) {
    return (
      <div className="mt-5 rounded-md border border-line p-4">
        <div className="flex flex-col justify-between gap-3 sm:flex-row sm:items-start">
          <div>
            <div className="text-sm font-semibold text-ink">Dry-run checklist</div>
            <div className="mt-1 text-xs leading-5 text-muted">
              Dry-run checks load after the latest switch run is available.
            </div>
          </div>
          <Badge value="disabled" />
        </div>
      </div>
    );
  }

  const status = dryRunReadiness.dry_run_ready
    ? "ready"
    : dryRunReadiness.can_run_dry_run
      ? "pending"
      : "blocked";

  return (
    <div className="mt-5 rounded-md border border-line p-4">
      <div className="flex flex-col justify-between gap-3 sm:flex-row sm:items-start">
        <div>
          <div className="text-sm font-semibold text-ink">Dry-run checklist</div>
          <div className="mt-1 text-xs leading-5 text-muted">
            Dry-run booking stays gated until a real alternate provider adapter, imported records, resolved mappings, and execution support exist.
          </div>
        </div>
        <Badge value={status} />
      </div>

      <div className="mt-4 grid gap-3 md:grid-cols-3">
        <Info label="Run status" value={dryRunReadiness.status} />
        <Info label="Target provider" value={dryRunReadiness.to_provider} />
        <Info label="Dry-run ready" value={dryRunReadiness.dry_run_ready ? "Yes" : "No"} />
      </div>

      <div className="mt-4 grid gap-3 md:grid-cols-2">
        {dryRunReadiness.checks.map((check) => (
          <div key={check.key} className="rounded-md border border-line p-3">
            <div className="flex items-start justify-between gap-3">
              <div className="flex min-w-0 gap-3">
                <CheckCircle2 className={check.complete ? "mt-0.5 h-5 w-5 flex-none text-brand" : "mt-0.5 h-5 w-5 flex-none text-slate-300"} />
                <div>
                  <div className="text-sm font-medium text-ink">{check.label}</div>
                  {!check.complete && check.message ? <div className="mt-1 text-xs leading-5 text-muted">{check.message}</div> : null}
                </div>
              </div>
              <Badge value={check.complete ? "active" : "blocked"} />
            </div>
          </div>
        ))}
      </div>

      {dryRunReadiness.blocked_reason ? (
        <div className="mt-4 rounded-md border border-amber-200 bg-amber-50 px-3 py-2 text-xs leading-5 text-amber-900">
          {dryRunReadiness.blocked_reason}
        </div>
      ) : null}

      <div className="mt-4">
        <Button type="button" disabled>
          <CalendarCheck className="h-4 w-4" />
          Run dry-run
        </Button>
      </div>
    </div>
  );
}

function ProviderSwitchMatchReview({
  busy,
  onUpdateMatch,
  readiness,
  switchRun
}: {
  busy: string;
  onUpdateMatch: (matchID: string, matchStatus: string) => void;
  readiness: ProviderSwitchReadiness;
  switchRun: ProviderSwitchRun | null;
}) {
  const matches = switchRun?.matches ?? [];
  const actionsDisabled = busy !== "" || !readiness.can_start_switch || !switchRun || switchRun.status === "blocked";
  const disabledMessage = !readiness.can_start_switch
    ? "Match actions are gated until an alternate native POS adapter is installed."
    : switchRun?.status === "blocked"
      ? "Blocked switch runs cannot be edited."
      : "";

  return (
    <div className="mt-5 rounded-md border border-line p-4">
      <div className="flex flex-col justify-between gap-3 sm:flex-row sm:items-start">
        <div>
          <div className="text-sm font-semibold text-ink">Match review</div>
          <div className="mt-1 text-xs leading-5 text-muted">
            Review provider records before any future provider activation. Confirming a match does not activate a POS provider.
          </div>
        </div>
        <Badge value={matches.length > 0 ? "needs_review" : "disabled"} />
      </div>

      {disabledMessage ? (
        <div className="mt-4 rounded-md border border-amber-200 bg-amber-50 px-3 py-2 text-xs leading-5 text-amber-900">
          {disabledMessage}
        </div>
      ) : null}

      {matches.length === 0 ? (
        <div className="mt-4 rounded-md border border-line p-4 text-sm text-muted">
          No switch matches to review.
        </div>
      ) : (
        <>
          <div className="mt-4 hidden overflow-x-auto rounded-md border border-line md:block">
            <table className="w-full min-w-[880px] text-left text-sm">
              <thead className="bg-slate-50 text-xs uppercase text-muted">
                <tr>
                  <th className="px-4 py-3">Type</th>
                  <th className="px-4 py-3">Provider record</th>
                  <th className="px-4 py-3">Canonical match</th>
                  <th className="px-4 py-3">Confidence</th>
                  <th className="px-4 py-3">Status</th>
                  <th className="px-4 py-3">Reason</th>
                  <th className="px-4 py-3">Actions</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-line bg-white">
                {matches.map((match) => (
                  <tr key={match.id}>
                    <td className="px-4 py-3">
                      <Badge value={match.entity_type} />
                    </td>
                    <td className="px-4 py-3 font-medium text-ink">{providerSwitchProviderLabel(match)}</td>
                    <td className="px-4 py-3 text-muted">{match.canonical_name || "No canonical match"}</td>
                    <td className="px-4 py-3 text-muted">{match.match_confidence}%</td>
                    <td className="px-4 py-3">
                      <Badge value={match.match_status} />
                    </td>
                    <td className="px-4 py-3 text-muted">{match.match_reason || "-"}</td>
                    <td className="px-4 py-3">
                      <ProviderSwitchMatchActions
                        busy={busy}
                        disabled={actionsDisabled}
                        match={match}
                        onUpdateMatch={onUpdateMatch}
                      />
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>

          <div className="mt-4 space-y-3 md:hidden">
            {matches.map((match) => (
              <div key={match.id} className="rounded-md border border-line p-3">
                <div className="flex items-start justify-between gap-3">
                  <div>
                    <div className="text-sm font-semibold text-ink">{providerSwitchProviderLabel(match)}</div>
                    <div className="mt-1 text-xs text-muted">{match.canonical_name || "No canonical match"}</div>
                  </div>
                  <Badge value={match.match_status} />
                </div>
                <div className="mt-3 grid gap-2 text-sm">
                  <Info label="Type" value={match.entity_type} />
                  <Info label="Confidence" value={`${match.match_confidence}%`} />
                  <Info label="Reason" value={match.match_reason || "-"} />
                </div>
                <div className="mt-3">
                  <ProviderSwitchMatchActions
                    busy={busy}
                    disabled={actionsDisabled}
                    match={match}
                    onUpdateMatch={onUpdateMatch}
                  />
                </div>
              </div>
            ))}
          </div>
        </>
      )}
    </div>
  );
}

function ProviderSwitchMatchActions({
  busy,
  disabled,
  match,
  onUpdateMatch
}: {
  busy: string;
  disabled: boolean;
  match: ProviderSwitchMatch;
  onUpdateMatch: (matchID: string, matchStatus: string) => void;
}) {
  const matchBusy = busy.startsWith(`switch-match:${match.id}:`);
  return (
    <div className="flex flex-wrap gap-2">
      <Button
        type="button"
        variant="secondary"
        onClick={() => onUpdateMatch(match.id, "confirmed")}
        disabled={disabled || !match.canonical_entity_id || match.match_status === "confirmed"}
      >
        <CheckCircle2 className="h-4 w-4" />
        {matchBusy && busy.endsWith(":confirmed") ? "Saving..." : "Confirm"}
      </Button>
      <Button
        type="button"
        variant="secondary"
        onClick={() => onUpdateMatch(match.id, "unmatched")}
        disabled={disabled || match.match_status === "unmatched"}
      >
        <XCircle className="h-4 w-4" />
        {matchBusy && busy.endsWith(":unmatched") ? "Saving..." : "Mark unmatched"}
      </Button>
      <Button
        type="button"
        variant="secondary"
        onClick={() => onUpdateMatch(match.id, "skipped")}
        disabled={disabled || match.match_status === "skipped"}
      >
        <Ban className="h-4 w-4" />
        {matchBusy && busy.endsWith(":skipped") ? "Saving..." : "Skip"}
      </Button>
    </div>
  );
}

function providerSwitchProviderLabel(match: ProviderSwitchMatch) {
  const details = [];
  if (match.provider_duration_minutes) {
    details.push(`${match.provider_duration_minutes} min`);
  }
  if (match.provider_phone) {
    details.push(match.provider_phone);
  }
  if (match.provider_email) {
    details.push(match.provider_email);
  }
  return details.length ? `${match.provider_name} (${details.join(", ")})` : match.provider_name;
}

function providerSwitchImportSteps(
  readiness: ProviderSwitchReadiness,
  switchRun: ProviderSwitchRun | null,
  dryRunReadiness: ProviderSwitchDryRunReadiness | null
) {
  const summary = switchRun?.match_summary;
  const hasMatches = Boolean(summary && summary.total > 0);
  const hasUnresolved = Boolean(summary && (summary.suggested > 0 || summary.conflicts > 0 || summary.unmatched > 0));
  return [
    {
      key: "connect_target_provider",
      label: "Connect target provider",
      complete: readiness.can_start_switch,
      status: "blocked",
      message: readiness.can_start_switch
        ? "Alternate adapter is installed; connection flow belongs to a future provider-specific slice."
        : "No alternate native POS adapter is installed."
    },
    {
      key: "import_provider_records",
      label: "Import provider records",
      complete: hasMatches,
      status: switchRun ? "pending" : "blocked",
      message: hasMatches ? `${summary?.total ?? 0} provider records are available for review.` : "Import waits for a real target provider connection."
    },
    {
      key: "auto_match_records",
      label: "Auto-match canonical records",
      complete: hasMatches,
      status: switchRun ? "pending" : "blocked",
      message: hasMatches
        ? `${summary?.suggested ?? 0} suggested, ${summary?.conflicts ?? 0} conflicts, ${summary?.unmatched ?? 0} unmatched.`
        : "Matching starts after provider records are imported."
    },
    {
      key: "resolve_matches",
      label: "Resolve matches",
      complete: hasMatches && !hasUnresolved,
      status: hasMatches ? "pending" : "blocked",
      message: hasMatches && !hasUnresolved ? "Match review is resolved for this run." : "Owner review is required before dry-run."
    },
    {
      key: "dry_run_readiness",
      label: "Dry-run booking readiness",
      complete: Boolean(dryRunReadiness?.dry_run_ready),
      status: dryRunReadiness?.can_run_dry_run ? "pending" : "blocked",
      message: dryRunReadiness?.blocked_reason || "Dry-run remains gated until import and mapping prerequisites pass."
    },
    {
      key: "activate_provider",
      label: "Activate provider",
      complete: false,
      status: "blocked",
      message: "Activation stays disabled until a real adapter, mappings, and dry-run are complete."
    }
  ];
}

function providerSwitchWorkflowSteps(
  readiness: ProviderSwitchReadiness,
  switchRun: ProviderSwitchRun | null,
  dryRunReadiness: ProviderSwitchDryRunReadiness | null
) {
  const summary = switchRun?.match_summary;
  const hasMatches = Boolean(summary && summary.total > 0);
  const hasUnresolved = Boolean(summary && (summary.conflicts > 0 || summary.unmatched > 0));
  const dryRunBlockedReason =
    dryRunReadiness?.blocked_reason ||
    (readiness.dry_run_booking_ready
      ? "Alternate-provider dry-run is still gated."
      : "Current provider must be synced with bookable service and staff links first.");
  return [
    {
      key: "active_provider_source",
      label: "Active provider source",
      complete: readiness.active_provider !== "",
      status: "disabled",
      message: readiness.active_provider_label || readiness.active_provider || "No active provider selected."
    },
    {
      key: "alternate_adapter",
      label: "Connect alternate provider",
      complete: readiness.can_start_switch,
      status: "blocked",
      message: readiness.can_start_switch
        ? "Alternate adapter is installed."
        : "Square Appointments is the only native POS adapter in the current production release."
    },
    {
      key: "import_records",
      label: "Import provider records",
      complete: hasMatches,
      status: switchRun ? "pending" : "blocked",
      message: hasMatches ? `${summary?.total ?? 0} imported records have match candidates.` : "No imported records from an alternate provider."
    },
    {
      key: "auto_match",
      label: "Auto-match records",
      complete: hasMatches,
      status: switchRun ? "pending" : "blocked",
      message: hasMatches
        ? `${summary?.suggested ?? 0} suggested, ${summary?.conflicts ?? 0} conflicts, ${summary?.unmatched ?? 0} unmatched.`
        : "Matching starts after provider import."
    },
    {
      key: "resolve_conflicts",
      label: "Resolve conflicts",
      complete: hasMatches && !hasUnresolved,
      status: hasMatches ? "pending" : "blocked",
      message: hasMatches && !hasUnresolved ? "No unresolved matches remain." : "Manual conflict resolution is not enabled in this slice."
    },
    {
      key: "dry_run",
      label: "Dry-run booking",
      complete: Boolean(dryRunReadiness?.dry_run_ready ?? switchRun?.dry_run_ready),
      status: dryRunReadiness?.can_run_dry_run ? "pending" : "blocked",
      message: dryRunBlockedReason
    },
    {
      key: "activate_provider",
      label: "Activate provider",
      complete: Boolean(switchRun?.can_activate && readiness.can_activate_provider),
      status: "blocked",
      message: "Activation stays disabled until a real adapter, mappings, and dry-run are complete."
    }
  ];
}

function squareOverviewMessage(connection: POSConnection | undefined, syncLogCount: number) {
  if (connection?.error_message) {
    return connection.error_message;
  }
  if (!connection?.id) {
    return "Connect Square Appointments before syncing records or running booking readiness tests.";
  }
  if (!connection.location_id) {
    return "Select a Square location before booking readiness can pass.";
  }
  if (!connection.last_sync_at && syncLogCount === 0) {
    return "Run Sync to import current Square Appointments records.";
  }
  return `Latest sync ${formatOptionalDateTime(connection.last_sync_at)}. Confirmed bookings still require Square success.`;
}

function bookingDataOverviewMessage(
  readiness: SquareReadiness | undefined,
  bookableServiceCount: number,
  bookableStaffCount: number,
  syncFailedCount: number
) {
  if (bookableServiceCount === 0) {
    return (
      firstIncompleteCheckMessage(readiness?.checks, ["bookable_services", "sync_square"]) ||
      "No active, synced, POS-linked, AI-bookable service is ready for availability or booking."
    );
  }
  if (bookableStaffCount === 0) {
    return (
      firstIncompleteCheckMessage(readiness?.checks, ["bookable_staff", "sync_square"]) ||
      "No active, synced, POS-linked, AI-bookable staff member is ready for availability or booking."
    );
  }
  if (syncFailedCount > 0) {
    return `${syncFailedCount} sync failure${syncFailedCount === 1 ? "" : "s"} need review. Booking only uses records that remain synced and POS-linked.`;
  }
  return "Only active, synced, POS-linked, AI-bookable services and staff are used for availability and booking.";
}

function testBookingGateState(readiness: SquareReadiness | undefined, latest?: TestBookingRecord) {
  if (readiness?.booking_write_blocked) {
    return {
      status: "blocked",
      message:
        readiness.booking_write_blocked_reason ||
        "Square rejected the latest booking write. Reconnect Square or run a test booking after updating seller permissions."
    };
  }
  if (readiness?.ai_enabled) {
    return {
      status: "ready",
      message: "AI booking is enabled. New confirmations still require a successful Square Appointments booking ID."
    };
  }
  if (readiness?.can_enable_ai_booking) {
    return {
      status: "ready",
      message: "Square is connected, synced, and booking-ready. The test booking buttons are optional POS smoke tests."
    };
  }
  if (readiness?.can_cancel_test_booking) {
    return {
      status: "pending",
      message: "Cancel the latest optional Square test booking to clean up the POS smoke test."
    };
  }
  if (latest?.error_message) {
    return {
      status: "blocked",
      message: latest.error_message
    };
  }
  if (readiness?.can_test_booking) {
    return {
      status: "pending",
      message: "Optional: check Square availability, select a real slot, and create one smoke-test booking."
    };
  }
  return {
    status: "blocked",
    message:
      firstIncompleteCheckMessage(readiness?.checks) ||
      "Connect Square Appointments, select a location, and keep at least one booking-ready service and staff member before running a smoke test."
  };
}

function firstIncompleteCheckMessage(
  checks: { key: string; complete: boolean; message?: string }[] | undefined,
  keys?: string[]
) {
  const check = checks?.find((item) => !item.complete && (!keys || keys.includes(item.key)));
  return check?.message || "";
}

function overviewCardClass(status: string) {
  if (status === "ready" || status === "connected" || status === "active") {
    return "border-emerald-200 bg-emerald-50 shadow-none";
  }
  if (status === "error" || status === "blocked" || status === "failed") {
    return "border-red-200 bg-red-50 shadow-none";
  }
  if (status === "pending") {
    return "border-amber-200 bg-amber-50 shadow-none";
  }
  return "shadow-none";
}

function overviewDescriptionClass(status: string) {
  if (status === "ready" || status === "connected" || status === "active") {
    return "text-emerald-800";
  }
  if (status === "error" || status === "blocked" || status === "failed") {
    return "text-red-800";
  }
  if (status === "pending") {
    return "text-amber-900";
  }
  return "text-muted";
}

function formatOptionalDateTime(value?: string) {
  return value ? new Date(value).toLocaleString() : "Not synced";
}

function LatestTest({ latest }: { latest?: TestBookingRecord }) {
  if (!latest) {
    return (
      <div className="mt-5 rounded-md border border-line p-4 text-sm text-muted">
        No Square test booking has been created yet.
      </div>
    );
  }

  const failed = latest.status === "fallback_pending" || latest.error_code;
  return (
    <div className="mt-5 rounded-md border border-line p-4">
      <div className="flex flex-col justify-between gap-3 sm:flex-row sm:items-start">
        <div>
          <div className="text-sm font-semibold text-ink">Latest test</div>
          <div className="mt-1 text-sm text-muted">
            {latest.start_time ? new Date(latest.start_time).toLocaleString() : "No time recorded"}
          </div>
        </div>
        <Badge value={latest.appointment_status || latest.status} />
      </div>
      <div className="mt-4 grid gap-3 text-sm md:grid-cols-2">
        <Info label="POS booking" value={latest.pos_booking_id || "Not returned"} />
        <Info label="Appointment" value={latest.appointment_id || "Not created"} />
      </div>
      {failed ? (
        <div className="mt-4 flex gap-2 rounded-md border border-red-200 bg-red-50 p-3 text-sm text-red-800">
          <XCircle className="mt-0.5 h-4 w-4 flex-none" />
          <div>
            <div className="font-semibold">{latest.error_code || "Test booking failed"}</div>
            <div className="mt-1">{latest.error_message || "Review Square and try again."}</div>
          </div>
        </div>
      ) : null}
    </div>
  );
}

function Field({ label, children }: { label: string; children: ReactNode }) {
  return (
    <label className="block">
      <span className="text-xs font-semibold uppercase tracking-wide text-muted">{label}</span>
      <div className="mt-1">{children}</div>
    </label>
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

function AvailabilityPicker({
  checked,
  error,
  loading,
  onSelect,
  result,
  selectedStartTime,
  timezone
}: {
  checked: boolean;
  error: string;
  loading: boolean;
  onSelect: (slot: AvailabilitySlot) => void;
  result: AvailabilityResult | null;
  selectedStartTime: string;
  timezone?: string;
}) {
  const slots = result?.slots ?? [];
  if (error) {
    return <Alert title="Availability check failed" message={error} />;
  }
  if (loading) {
    return (
      <div className="rounded-md border border-line p-4 text-sm text-muted">
        Checking Square Appointments availability...
      </div>
    );
  }
  if (!checked) {
    return (
      <div className="rounded-md border border-line p-4 text-sm text-muted">
        Check Square availability to select a real bookable slot.
      </div>
    );
  }
  if (slots.length === 0) {
    return (
      <div className="rounded-md border border-line p-4 text-sm text-muted">
        No Square slots returned for this service, staff, and date.
      </div>
    );
  }

  const selected = slots.find((slot) => slot.start_time === selectedStartTime);
  return (
    <div className="rounded-md border border-line p-4">
      <div className="flex flex-col justify-between gap-2 sm:flex-row sm:items-start">
        <div>
          <div className="text-sm font-semibold text-ink">Available slots</div>
          <div className="mt-1 text-xs text-muted">
            Times are shown in {timezone || "the selected location timezone"}.
          </div>
          <div className="mt-1 text-xs text-muted">
            Quote valid until {formatQuoteExpiry(result?.expires_at, timezone)}.
          </div>
        </div>
        {selected ? <Badge value="selected" /> : null}
      </div>
      <div className="mt-4 grid grid-cols-2 gap-2 sm:grid-cols-3 lg:grid-cols-4">
        {slots.map((slot) => {
          const active = slot.start_time === selectedStartTime;
          return (
            <button
              key={slot.fingerprint || `${slot.start_time}-${slot.staff_id ?? ""}`}
              type="button"
              onClick={() => onSelect(slot)}
              className={`min-h-10 rounded-md border px-3 py-2 text-left text-sm font-medium transition ${
                active
                  ? "border-brand bg-emerald-50 text-brand"
                  : "border-line bg-white text-ink hover:border-brand hover:bg-emerald-50"
              }`}
            >
              {formatTime(slot.start_time, timezone)}
            </button>
          );
        })}
      </div>
      {selected ? (
        <div className="mt-4 rounded-md bg-slate-50 p-3 text-sm text-ink">
          Selected: {formatDate(selected.start_time, timezone)} {formatTimeRange(selected.start_time, selected.end_time, timezone)}
        </div>
      ) : (
        <div className="mt-4 text-sm text-muted">Select one Square slot before creating the test booking.</div>
      )}
    </div>
  );
}

function firstBookableService(items: POSService[]) {
  return items.find(serviceIsBookable);
}

function serviceIsBookable(service: POSService) {
  return (
    Boolean(service.id) &&
    service.active &&
    service.ai_bookable &&
    service.sync_status === "synced" &&
    service.pos_linked &&
    Boolean(service.pos_service_id) &&
    Boolean(service.pos_service_version) &&
    service.duration_minutes > 0
  );
}

function firstBookableStaff(items: POSStaffMember[]) {
  return items.find(staffIsBookable);
}

function staffIsBookable(member: POSStaffMember) {
  return (
    Boolean(member.id) &&
    member.active &&
    member.ai_bookable &&
    member.sync_status === "synced" &&
    member.pos_linked &&
    Boolean(member.pos_staff_id)
  );
}

function nextBookingDate(timezone?: string) {
  return addDaysInput(formatDateInput(new Date(), timezone), 1);
}

function formatDate(value: string, timezone?: string) {
  return new Date(value).toLocaleDateString(undefined, {
    month: "short",
    day: "numeric",
    year: "numeric",
    timeZone: timezone
  });
}

function formatTime(value: string, timezone?: string) {
  return new Date(value).toLocaleTimeString(undefined, {
    hour: "numeric",
    minute: "2-digit",
    timeZone: timezone
  });
}

function formatTimeRange(start: string, end: string, timezone?: string) {
  return `${formatTime(start, timezone)} - ${formatTime(end, timezone)}`;
}

function formatQuoteExpiry(value?: string, timezone?: string) {
  if (!value) return "an unknown time";
  return new Date(value).toLocaleTimeString(undefined, {
    hour: "numeric",
    minute: "2-digit",
    second: "2-digit",
    timeZone: timezone
  });
}

function formatDateInput(date: Date, timezone?: string) {
  const parts = new Intl.DateTimeFormat("en-CA", {
    day: "2-digit",
    month: "2-digit",
    timeZone: timezone,
    year: "numeric"
  }).formatToParts(date);
  const values = new Map(parts.map((part) => [part.type, part.value]));
  return `${values.get("year")}-${values.get("month")}-${values.get("day")}`;
}

function addDaysInput(value: string, days: number) {
  const [year, month, day] = value.split("-").map(Number);
  const date = new Date(Date.UTC(year, month - 1, day + days));
  return `${date.getUTCFullYear()}-${String(date.getUTCMonth() + 1).padStart(2, "0")}-${String(
    date.getUTCDate()
  ).padStart(2, "0")}`;
}

function clearAvailabilityExpiryTimer(ref: { current: ReturnType<typeof setTimeout> | null }) {
  if (!ref.current) return;
  clearTimeout(ref.current);
  ref.current = null;
}

function scheduleAvailabilityExpiry(
  timerRef: { current: ReturnType<typeof setTimeout> | null },
  expiresAt: string | undefined,
  requestID: number,
  requestIDRef: { current: number },
  onExpire: () => void
) {
  clearAvailabilityExpiryTimer(timerRef);
  if (!expiresAt) return;
  const expiresAtMs = new Date(expiresAt).getTime();
  const delay = expiresAtMs - Date.now();
  if (!Number.isFinite(expiresAtMs) || delay <= 0) {
    if (requestID === requestIDRef.current) onExpire();
    return;
  }
  timerRef.current = setTimeout(() => {
    if (requestID !== requestIDRef.current) return;
    requestIDRef.current += 1;
    timerRef.current = null;
    onExpire();
  }, Math.min(delay, 2_147_483_647));
}

function availabilityQuoteIsUsable(result: AvailabilityResult | null) {
  if (!result?.quote_id || !result.request_fingerprint || !result.expires_at) return false;
  const expiresAt = new Date(result.expires_at).getTime();
  return Number.isFinite(expiresAt) && expiresAt > Date.now();
}

function assertAvailabilityQuoteUsable(result: AvailabilityResult, slot: AvailabilitySlot) {
  if (!availabilityQuoteIsUsable(result) || !slot.fingerprint) {
    throw new Error("This availability quote is missing, invalid, or expired. Check Square Appointments again.");
  }
}
