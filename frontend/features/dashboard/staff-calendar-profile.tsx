"use client";

import { useEffect, useRef, useState } from "react";
import { CalendarOff, Plus, Save, Search, XCircle } from "lucide-react";
import { Alert } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { CardDescription } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import {
  cancelManleAICalendarException,
  createManleAICalendarException,
  isManleAICalendarVersionConflict,
  newManleAICalendarActionKey,
  salonLocalDateTimeToISO,
  updateManleAICalendarStaffProfile,
  type InternalCalendarSurface
} from "@/lib/api/internal-calendar";
import type {
  ManleAICalendarAggregate,
  ManleAICalendarMutationResponse,
  ManleAICalendarStaffProfile,
  ManleAICalendarWeeklyPeriodInput,
} from "@/types/api";

type StaffCalendarMember = {
  id?: string;
  name: string;
  active: boolean;
  archived_at?: string | null;
};

type PeriodDraft = {
  key: string;
  dayOfWeek: number;
  start: string;
  end: string;
};

type TimeOffForm = {
  startsAt: string;
  endsAt: string;
  reason: string;
};

type StaffCalendarProfileProps = {
  salonID: string;
  timezone: string;
  member: StaffCalendarMember | null;
  calendar: ManleAICalendarAggregate | null;
  loading: boolean;
  error?: string;
  surface?: InternalCalendarSurface;
  manageServiceEligibility?: boolean;
  canonicalEligibleServiceIDs?: string[];
  onReload: () => Promise<void>;
  onCalendarChange: (calendar: ManleAICalendarAggregate) => void;
};

const dayLabels = ["Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"];

export function StaffCalendarProfile({
  salonID,
  timezone,
  member,
  calendar,
  loading,
  error: loadError = "",
  surface = "tenant",
  manageServiceEligibility = true,
  canonicalEligibleServiceIDs,
  onReload,
  onCalendarChange
}: StaffCalendarProfileProps) {
  const actionKeysRef = useRef(new Map<string, string>());
  const [periods, setPeriods] = useState<PeriodDraft[]>([]);
  const [eligibleServiceIDs, setEligibleServiceIDs] = useState<string[]>([]);
  const [serviceSearch, setServiceSearch] = useState("");
  const [timeOffForm, setTimeOffForm] = useState<TimeOffForm>({ startsAt: "", endsAt: "", reason: "" });
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");
  const [success, setSuccess] = useState("");

  const profile = member?.id
    ? calendar?.staff_profiles.find((item) => item.staff.id === member.id) ?? null
    : null;

  useEffect(() => {
    setPeriods(profileToPeriods(profile));
    setEligibleServiceIDs(profile?.eligible_services.map((service) => service.id) ?? []);
    setTimeOffForm({ startsAt: "", endsAt: "", reason: "" });
    setError("");
    setSuccess("");
    actionKeysRef.current.clear();
  }, [calendar?.config_version, member?.id, profile]);

  if (!member?.id) {
    return (
      <div className="mt-5 rounded-md border border-blue-200 bg-blue-50 p-4">
        <div className="font-semibold text-blue-950">Internal calendar profile</div>
        <div className="mt-1 text-sm leading-6 text-blue-900">
          Save this staff member first. Weekly schedule, service eligibility, and time off belong to the saved staff record.
        </div>
      </div>
    );
  }

  if (loading) {
    return (
      <div className="mt-5 space-y-3 rounded-md border border-line p-4">
        <Skeleton className="h-5 w-52" />
        <Skeleton className="h-28" />
        <Skeleton className="h-28" />
      </div>
    );
  }

  if (loadError || !calendar) {
    return (
      <div className="mt-5 rounded-md border border-line p-4">
        <Alert title="Internal calendar profile unavailable" message={loadError || "Could not load the staff calendar profile."} />
        <Button type="button" variant="secondary" className="mt-3" onClick={() => void onReload()}>
          Retry calendar profile
        </Button>
      </div>
    );
  }

  const calendarData = calendar;
  const staffID = member.id;
  const baseDisabled = Boolean(busy) || !calendar.config;
  const staffUnavailable = Boolean(member.archived_at) || !member.active;
  const addDisabled = baseDisabled || staffUnavailable;
  const services = calendar.service_policies.map((policy) => policy.service);
  const filteredServices = services.filter((service) => service.name.toLocaleLowerCase().includes(serviceSearch.trim().toLocaleLowerCase()));
  const timeOff = calendar.exceptions.filter(
    (exception) => exception.scope_type === "staff" && exception.staff_id === member.id
  );
  const blockers = calendar.readiness.blockers.filter(
    (blocker) => blocker.scope === "staff" && blocker.entity_id === member.id
  );

  function actionKey(scope: string) {
    const existing = actionKeysRef.current.get(scope);
    if (existing) return existing;
    const created = newManleAICalendarActionKey();
    actionKeysRef.current.set(scope, created);
    return created;
  }

  function resetActionKey(scope: string) {
    actionKeysRef.current.delete(scope);
  }

  function resetProfileActionKeys() {
    resetActionKey("schedule");
    resetActionKey("services");
  }

  async function runMutation(
    scope: string,
    busyKey: string,
    successMessage: string,
    mutation: (key: string, expectedVersion: number) => Promise<ManleAICalendarMutationResponse>
  ) {
    if (busy) return;
    setBusy(busyKey);
    setError("");
    setSuccess("");
    try {
      const response = await mutation(actionKey(scope), calendarData.config_version);
      resetActionKey(scope);
      onCalendarChange(response.manleai_calendar);
      setSuccess(response.replayed ? `${successMessage} The previous safe retry was replayed.` : successMessage);
    } catch (mutationFailure) {
      if (isManleAICalendarVersionConflict(mutationFailure)) {
        resetActionKey(scope);
        await onReload();
        setError("Calendar configuration changed in another session. The latest staff profile was loaded; review it before saving again.");
      } else {
        setError(mutationFailure instanceof Error ? mutationFailure.message : "Could not update the staff calendar profile.");
      }
    } finally {
      setBusy("");
    }
  }

  function updatePeriod(key: string, patch: Partial<PeriodDraft>) {
    resetProfileActionKeys();
    setPeriods((current) => current.map((period) => (period.key === key ? { ...period, ...patch } : period)));
  }

  function addPeriod(dayOfWeek: number) {
    resetProfileActionKeys();
    setPeriods((current) => [...current, { key: newManleAICalendarActionKey(), dayOfWeek, start: "", end: "" }]);
  }

  function removePeriod(key: string) {
    resetProfileActionKeys();
    setPeriods((current) => current.filter((period) => period.key !== key));
  }

  function normalizedPeriods() {
    const result: ManleAICalendarWeeklyPeriodInput[] = [];
    for (const period of periods) {
      const startMinute = parseTime(period.start);
      const endMinute = parseTime(period.end);
      if (startMinute === null || endMinute === null || startMinute >= endMinute) return null;
      result.push({ day_of_week: period.dayOfWeek, start_minute: startMinute, end_minute: endMinute });
    }
    return result;
  }

  async function saveProfile(scope: "schedule" | "services") {
    const weeklyPeriods = normalizedPeriods();
    if (!weeklyPeriods) {
      setError("Every weekly schedule period needs a valid start and end time, with end after start.");
      return;
    }
    const persistedEligibleServiceIDs = manageServiceEligibility
      ? eligibleServiceIDs
      : canonicalEligibleServiceIDs ?? profile?.eligible_services.map((service) => service.id) ?? [];
    await runMutation(scope, scope, scope === "schedule" ? "Weekly schedule saved." : "Service eligibility saved.", (key, expectedVersion) =>
      updateManleAICalendarStaffProfile(salonID, staffID, {
        action_key: key,
        expected_config_version: expectedVersion,
        weekly_periods: weeklyPeriods,
        eligible_service_ids: persistedEligibleServiceIDs
      }, surface)
    );
  }

  function toggleService(serviceID: string) {
    resetProfileActionKeys();
    setEligibleServiceIDs((current) =>
      current.includes(serviceID) ? current.filter((id) => id !== serviceID) : [...current, serviceID]
    );
  }

  function updateTimeOff(next: TimeOffForm) {
    resetActionKey("time-off");
    setTimeOffForm(next);
  }

  async function addTimeOff() {
    const startsAt = salonLocalDateTimeToISO(timeOffForm.startsAt, timezone);
    const endsAt = salonLocalDateTimeToISO(timeOffForm.endsAt, timezone);
    if (!startsAt || !endsAt || new Date(startsAt).getTime() >= new Date(endsAt).getTime()) {
      setError(`Enter an unambiguous time-off range in ${timezone}, with end after start.`);
      return;
    }
    await runMutation("time-off", "time-off", "Time off added.", (key, expectedVersion) =>
      createManleAICalendarException(salonID, {
        action_key: key,
        expected_config_version: expectedVersion,
        scope_type: "staff",
        staff_id: staffID,
        effect: "unavailable",
        starts_at: startsAt,
        ends_at: endsAt,
        capacity_override: null,
        reason: timeOffForm.reason.trim() || undefined
      }, surface)
    );
  }

  async function cancelTimeOff(exceptionID: string) {
    await runMutation(`cancel-time-off-${exceptionID}`, `cancel-time-off-${exceptionID}`, "Time off cancelled.", (key, expectedVersion) =>
      cancelManleAICalendarException(salonID, exceptionID, {
        action_key: key,
        expected_config_version: expectedVersion
      }, surface)
    );
  }

  return (
    <div className="mt-5 rounded-md border border-line bg-slate-50 p-4">
      <div className="flex flex-col justify-between gap-3 sm:flex-row sm:items-start">
        <div>
          <div className="text-sm font-semibold text-ink">ManleAI controls · internal calendar profile</div>
          <CardDescription>
            Weekly schedule and time off are owned by this staff record and stay editable independently of provider-managed contact fields.
          </CardDescription>
        </div>
        <div className="flex flex-wrap gap-2">
          <Badge value={calendar.config ? "configured" : "required"} />
          {blockers.length > 0 ? <Badge value="blocked" /> : null}
        </div>
      </div>

      {!calendar.config ? (
        <div className="mt-4 rounded-md border border-amber-200 bg-amber-50 p-3 text-sm leading-6 text-amber-900">
          Save the salon&apos;s ManleAI Calendar policy before configuring staff schedules.
        </div>
      ) : null}
      {member.archived_at || !member.active ? (
        <div className="mt-4 rounded-md border border-amber-200 bg-amber-50 p-3 text-sm leading-6 text-amber-900">
          This staff member is inactive or archived. Cleanup is still allowed: remove existing schedule periods or service assignments and cancel existing time off. New schedules, assignments, and time off are blocked.
        </div>
      ) : null}
      {blockers.length > 0 ? (
        <div className="mt-4 rounded-md border border-line bg-white p-3 text-sm leading-6 text-muted">
          {blockers.map((blocker) => <div key={`${blocker.code}-${blocker.entity_id}`}>{blocker.message}</div>)}
        </div>
      ) : null}
      {error ? <div className="mt-4"><Alert title="Staff calendar not updated" message={error} /></div> : null}
      {success ? <div className="mt-4"><Alert type="success" title="Staff calendar updated" message={success} /></div> : null}

      <section className="mt-5 rounded-md border border-line bg-white p-4">
        <div className="flex flex-col justify-between gap-3 sm:flex-row sm:items-center">
          <div>
            <div className="text-sm font-semibold text-ink">Weekly schedule</div>
            <div className="mt-1 text-xs leading-5 text-muted">Multiple working periods per day are supported. No periods means no recurring weekly availability.</div>
          </div>
          <Button type="button" onClick={() => void saveProfile("schedule")} disabled={baseDisabled}>
            <Save className="h-4 w-4" />
            {busy === "schedule" ? "Saving..." : "Save schedule"}
          </Button>
        </div>
        <div className="mt-4 space-y-3">
          {dayLabels.map((day, dayOfWeek) => {
            const dayPeriods = periods.filter((period) => period.dayOfWeek === dayOfWeek);
            return (
              <div key={day} className="rounded-md border border-line p-3">
                <div className="flex flex-col justify-between gap-2 sm:flex-row sm:items-center">
                  <div className="text-sm font-medium text-ink">{day} <span className="font-normal text-muted">· {dayPeriods.length ? `${dayPeriods.length} period${dayPeriods.length === 1 ? "" : "s"}` : "Not working"}</span></div>
                  <Button type="button" variant="secondary" className="w-full sm:w-auto" onClick={() => addPeriod(dayOfWeek)} disabled={addDisabled}>
                    <Plus className="h-4 w-4" /> Add period
                  </Button>
                </div>
                {dayPeriods.length > 0 ? (
                  <div className="mt-3 space-y-2">
                    {dayPeriods.map((period) => (
                      <div key={period.key} className="grid gap-2 sm:grid-cols-[1fr_1fr_auto]">
                        <TimeInput label="Starts" value={period.start} disabled={addDisabled} onChange={(value) => updatePeriod(period.key, { start: value })} />
                        <TimeInput label="Ends" value={period.end} disabled={addDisabled} onChange={(value) => updatePeriod(period.key, { end: value })} />
                        <Button type="button" variant="danger" className="self-end" onClick={() => removePeriod(period.key)} disabled={baseDisabled}>
                          <XCircle className="h-4 w-4" /> Remove
                        </Button>
                      </div>
                    ))}
                  </div>
                ) : null}
              </div>
            );
          })}
        </div>
      </section>

      {manageServiceEligibility ? <section className="mt-5 rounded-md border border-line bg-white p-4">
        <div className="flex flex-col justify-between gap-3 sm:flex-row sm:items-center">
          <div>
            <div className="text-sm font-semibold text-ink">Services this staff member can perform</div>
            <div className="mt-1 text-xs leading-5 text-muted">Selections come from the canonical salon service catalog.</div>
          </div>
          <Button type="button" onClick={() => void saveProfile("services")} disabled={baseDisabled}>
            <Save className="h-4 w-4" />
            {busy === "services" ? "Saving..." : "Save eligibility"}
          </Button>
        </div>
        {services.length === 0 ? (
          <div className="mt-4 rounded-md border border-line p-4 text-sm text-muted">No canonical services are available.</div>
        ) : (
          <>
            <label className="relative mt-4 block">
              <Search className="pointer-events-none absolute left-3 top-3 h-4 w-4 text-muted" />
              <input value={serviceSearch} onChange={(event) => setServiceSearch(event.target.value)} placeholder="Search services" className="h-10 w-full rounded-md border border-line bg-white pl-9 pr-3 text-sm text-ink outline-none focus:border-brand" />
            </label>
            <div className="mt-3 grid max-h-72 gap-2 overflow-y-auto sm:grid-cols-2">
              {filteredServices.map((service) => (
                <label key={service.id} className="flex items-start gap-3 rounded-md border border-line p-3 text-sm text-ink">
                  <input type="checkbox" className="mt-0.5" checked={eligibleServiceIDs.includes(service.id)} onChange={() => toggleService(service.id)} disabled={baseDisabled || ((staffUnavailable || Boolean(service.archived_at) || !service.active) && !eligibleServiceIDs.includes(service.id))} />
                  <span>
                    <span className="block font-medium">{service.name}</span>
                    <span className="mt-1 block text-xs text-muted">{service.duration_minutes} minutes{service.archived_at || !service.active ? " · inactive" : ""}</span>
                  </span>
                </label>
              ))}
            </div>
            {filteredServices.length === 0 ? <div className="mt-3 text-sm text-muted">No services match this search.</div> : null}
          </>
        )}
      </section> : (
        <section className="mt-5 rounded-md border border-blue-200 bg-blue-50 p-4 text-sm leading-6 text-blue-900">
          Service assignments use the canonical Eligible services control above. Saving a weekly schedule preserves those persisted assignments and does not create a second eligibility source.
        </section>
      )}

      <section className="mt-5 rounded-md border border-line bg-white p-4">
        <div className="flex items-start gap-3">
          <CalendarOff className="mt-0.5 h-5 w-5 flex-none text-brand" />
          <div>
            <div className="text-sm font-semibold text-ink">Time off</div>
            <div className="mt-1 text-xs leading-5 text-muted">Add unavailable periods in {timezone}. Cancel a record to change it; records are never deleted.</div>
          </div>
        </div>
        <div className="mt-4 grid gap-4 md:grid-cols-2 xl:grid-cols-[1fr_1fr_1fr_auto] xl:items-end">
          <DateTimeInput label="Starts" value={timeOffForm.startsAt} timezone={timezone} disabled={addDisabled} onChange={(value) => updateTimeOff({ ...timeOffForm, startsAt: value })} />
          <DateTimeInput label="Ends" value={timeOffForm.endsAt} timezone={timezone} disabled={addDisabled} onChange={(value) => updateTimeOff({ ...timeOffForm, endsAt: value })} />
          <label className="block">
            <span className="text-sm font-medium text-ink">Reason (optional)</span>
            <input value={timeOffForm.reason} onChange={(event) => updateTimeOff({ ...timeOffForm, reason: event.target.value })} disabled={addDisabled} className="mt-2 h-10 w-full rounded-md border border-line px-3 text-sm text-ink outline-none focus:border-brand disabled:bg-slate-100" />
          </label>
          <Button type="button" onClick={() => void addTimeOff()} disabled={addDisabled}>
            <Plus className="h-4 w-4" /> {busy === "time-off" ? "Adding..." : "Add time off"}
          </Button>
        </div>
        {timeOff.length === 0 ? (
          <div className="mt-4 rounded-md border border-line p-4 text-sm text-muted">No time-off exceptions for this staff member.</div>
        ) : (
          <div className="mt-4 space-y-2">
            {timeOff.map((exception) => (
              <div key={exception.id} className="flex flex-col justify-between gap-3 rounded-md border border-line p-3 sm:flex-row sm:items-center">
                <div>
                  <div className="flex flex-wrap items-center gap-2"><span className="text-sm font-medium text-ink">{formatDateTime(exception.starts_at, timezone)} – {formatDateTime(exception.ends_at, timezone)}</span><Badge value={exception.cancelled_at ? "cancelled" : "active"} /></div>
                  {exception.reason ? <div className="mt-1 text-xs text-muted">{exception.reason}</div> : null}
                </div>
                <Button type="button" variant="danger" className="w-full sm:w-auto" onClick={() => void cancelTimeOff(exception.id)} disabled={Boolean(busy) || Boolean(exception.cancelled_at)}>
                  {busy === `cancel-time-off-${exception.id}` ? "Cancelling..." : "Cancel time off"}
                </Button>
              </div>
            ))}
          </div>
        )}
      </section>
    </div>
  );
}

function TimeInput({ label, value, disabled, onChange }: { label: string; value: string; disabled: boolean; onChange: (value: string) => void }) {
  return <label className="block"><span className="text-xs font-semibold uppercase tracking-wide text-muted">{label}</span><input inputMode="numeric" placeholder="HH:MM" value={value} onChange={(event) => onChange(event.target.value)} disabled={disabled} className="mt-1 h-10 w-full rounded-md border border-line px-3 text-sm text-ink outline-none focus:border-brand disabled:bg-slate-100" /></label>;
}

function DateTimeInput({ label, value, timezone, disabled, onChange }: { label: string; value: string; timezone: string; disabled: boolean; onChange: (value: string) => void }) {
  return <label className="block"><span className="text-sm font-medium text-ink">{label}</span><input type="datetime-local" value={value} onChange={(event) => onChange(event.target.value)} disabled={disabled} className="mt-2 h-10 w-full rounded-md border border-line px-3 text-sm text-ink outline-none focus:border-brand disabled:bg-slate-100" /><span className="mt-1 block text-xs text-muted">Salon timezone: {timezone}</span></label>;
}

function profileToPeriods(profile: ManleAICalendarStaffProfile | null): PeriodDraft[] {
  return (profile?.weekly_periods ?? []).map((period) => ({ key: period.id, dayOfWeek: period.day_of_week, start: minuteToTime(period.start_minute), end: minuteToTime(period.end_minute) }));
}

function minuteToTime(value: number) {
  if (value === 1440) return "24:00";
  return `${String(Math.floor(value / 60)).padStart(2, "0")}:${String(value % 60).padStart(2, "0")}`;
}

function parseTime(value: string) {
  const match = /^(\d{1,2}):(\d{2})$/.exec(value.trim());
  if (!match) return null;
  const hours = Number(match[1]);
  const minutes = Number(match[2]);
  if (hours === 24 && minutes === 0) return 1440;
  if (hours > 23 || minutes > 59) return null;
  return hours * 60 + minutes;
}

function formatDateTime(value: string, timezone: string) {
  return new Intl.DateTimeFormat("en-US", { timeZone: timezone, dateStyle: "medium", timeStyle: "short" }).format(new Date(value));
}
