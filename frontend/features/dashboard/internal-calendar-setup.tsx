"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { Archive, CalendarClock, Plus, RefreshCcw, Save, XCircle } from "lucide-react";
import { Alert } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardDescription, CardTitle } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { SchedulingReadinessCard } from "@/features/dashboard/scheduling-readiness-card";
import {
  activateManleAICalendar,
  archiveManleAICalendarResource,
  cancelManleAICalendarException,
  createManleAICalendarException,
  createManleAICalendarResource,
  getManleAICalendar,
  isManleAICalendarVersionConflict,
  newManleAICalendarActionKey,
  replaceManleAICalendarHours,
  salonLocalDateTimeToISO,
  updateManleAICalendarConfig,
  updateManleAICalendarResource,
  type InternalCalendarSurface
} from "@/lib/api/internal-calendar";
import type {
  ManleAICalendarAggregate,
  ManleAICalendarExceptionEffect,
  ManleAICalendarExceptionScope,
  ManleAICalendarHourPeriodInput,
  ManleAICalendarMutationResponse,
  ManleAICalendarResourcePool
} from "@/types/api";

type ConfigForm = {
  slotStepMinutes: string;
  minimumBookingNoticeMinutes: string;
  bookingHorizonDays: string;
  rescheduleCutoffMinutes: string;
  cancellationCutoffMinutes: string;
  maxPartySize: string;
  defaultBufferBeforeMinutes: string;
  defaultBufferAfterMinutes: string;
};

type HourDraft = {
  key: string;
  dayOfWeek: number;
  start: string;
  end: string;
};

type ResourceForm = {
  id: string;
  name: string;
  capacity: string;
};

type ExceptionForm = {
  scopeType: ManleAICalendarExceptionScope;
  resourcePoolID: string;
  effect: ManleAICalendarExceptionEffect;
  startsAt: string;
  endsAt: string;
  capacityOverride: string;
  reason: string;
};

type InternalCalendarSetupProps = {
  salonID: string;
  timezone: string;
  surface?: InternalCalendarSurface;
};

const dayLabels = ["Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"];

export function InternalCalendarSetup({ salonID, timezone, surface = "tenant" }: InternalCalendarSetupProps) {
  const actionKeysRef = useRef(new Map<string, string>());
  const [calendar, setCalendar] = useState<ManleAICalendarAggregate | null>(null);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState("");
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");
  const [success, setSuccess] = useState("");
  const [configForm, setConfigForm] = useState<ConfigForm>(emptyConfigForm());
  const [hours, setHours] = useState<HourDraft[]>([]);
  const [resourceForm, setResourceForm] = useState<ResourceForm>(emptyResourceForm());
  const [exceptionForm, setExceptionForm] = useState<ExceptionForm>(emptyExceptionForm());

  const applyCalendar = useCallback((next: ManleAICalendarAggregate) => {
    setCalendar(next);
    setConfigForm(configToForm(next));
    setHours(hoursToDrafts(next));
    setResourceForm(emptyResourceForm());
    setExceptionForm(emptyExceptionForm());
  }, []);

  const load = useCallback(async ({ silent = false }: { silent?: boolean } = {}) => {
    if (!silent) setLoading(true);
    setLoadError("");
    try {
      const response = await getManleAICalendar(salonID, surface);
      applyCalendar(response.manleai_calendar);
    } catch (loadFailure) {
      setLoadError(loadFailure instanceof Error ? loadFailure.message : "Could not load ManleAI Calendar setup.");
    } finally {
      if (!silent) setLoading(false);
    }
  }, [applyCalendar, salonID, surface]);

  useEffect(() => {
    void load();
  }, [load]);

  function actionKey(scope: string) {
    const current = actionKeysRef.current.get(scope);
    if (current) return current;
    const created = newManleAICalendarActionKey();
    actionKeysRef.current.set(scope, created);
    return created;
  }

  function resetActionKey(scope: string) {
    actionKeysRef.current.delete(scope);
  }

  async function runMutation(
    scope: string,
    busyKey: string,
    successMessage: string,
    mutation: (key: string, expectedVersion: number) => Promise<ManleAICalendarMutationResponse>
  ) {
    if (!calendar || busy) return;
    setBusy(busyKey);
    setError("");
    setSuccess("");
    try {
      const response = await mutation(actionKey(scope), calendar.config_version);
      resetActionKey(scope);
      applyCalendar(response.manleai_calendar);
      setSuccess(response.replayed ? `${successMessage} The previous safe retry was replayed.` : successMessage);
    } catch (mutationFailure) {
      if (isManleAICalendarVersionConflict(mutationFailure)) {
        resetActionKey(scope);
        await load({ silent: true });
        setError("Calendar settings changed in another session. The latest version was loaded; review it before saving again.");
      } else {
        setError(mutationFailure instanceof Error ? mutationFailure.message : "Could not update ManleAI Calendar.");
      }
    } finally {
      setBusy("");
    }
  }

  function updateConfigForm(next: ConfigForm) {
    resetActionKey("config");
    setConfigForm(next);
  }

  async function saveConfig() {
    if (!calendar) return;
    const requiredValues = [
      configForm.slotStepMinutes,
      configForm.minimumBookingNoticeMinutes,
      configForm.bookingHorizonDays,
      configForm.maxPartySize,
      configForm.defaultBufferBeforeMinutes,
      configForm.defaultBufferAfterMinutes
    ];
    if (requiredValues.some((value) => value.trim() === "")) {
      setError("Complete every required scheduling policy field before saving.");
      return;
    }
    await runMutation("config", "config", "Internal scheduling policy saved.", (key, expectedVersion) =>
      updateManleAICalendarConfig(salonID, {
        action_key: key,
        expected_config_version: expectedVersion,
        slot_step_minutes: Number(configForm.slotStepMinutes),
        minimum_booking_notice_minutes: Number(configForm.minimumBookingNoticeMinutes),
        booking_horizon_days: Number(configForm.bookingHorizonDays),
        reschedule_cutoff_minutes: nullableNumber(configForm.rescheduleCutoffMinutes),
        cancellation_cutoff_minutes: nullableNumber(configForm.cancellationCutoffMinutes),
        max_party_size: Number(configForm.maxPartySize),
        default_buffer_before_minutes: Number(configForm.defaultBufferBeforeMinutes),
        default_buffer_after_minutes: Number(configForm.defaultBufferAfterMinutes)
      }, surface)
    );
  }

  function updateHour(key: string, patch: Partial<HourDraft>) {
    resetActionKey("hours");
    setHours((current) => current.map((period) => (period.key === key ? { ...period, ...patch } : period)));
  }

  function addHour(dayOfWeek: number) {
    resetActionKey("hours");
    setHours((current) => [...current, { key: draftKey(), dayOfWeek, start: "", end: "" }]);
  }

  function removeHour(key: string) {
    resetActionKey("hours");
    setHours((current) => current.filter((period) => period.key !== key));
  }

  async function saveHours() {
    if (!calendar?.config) {
      setError("Save the internal scheduling policy before adding local salon hours.");
      return;
    }
    const periods: ManleAICalendarHourPeriodInput[] = [];
    for (const period of hours) {
      const startMinute = parseTime(period.start);
      const endMinute = parseTime(period.end);
      if (startMinute === null || endMinute === null || startMinute >= endMinute) {
        setError(`Enter a valid opening range for ${dayLabels[period.dayOfWeek]}. End time must be after start time.`);
        return;
      }
      periods.push({ day_of_week: period.dayOfWeek, start_minute: startMinute, end_minute: endMinute });
    }
    await runMutation("hours", "hours", "Local salon hours saved.", (key, expectedVersion) =>
      replaceManleAICalendarHours(salonID, {
        action_key: key,
        expected_config_version: expectedVersion,
        periods
      }, surface)
    );
  }

  function editResource(resource: ManleAICalendarResourcePool) {
    resetActionKey("resource");
    setResourceForm({ id: resource.id, name: resource.name, capacity: String(resource.capacity) });
  }

  function updateResourceForm(next: ResourceForm) {
    resetActionKey("resource");
    setResourceForm(next);
  }

  async function saveResource() {
    if (!calendar?.config) {
      setError("Save the internal scheduling policy before managing resource pools.");
      return;
    }
    if (!resourceForm.name.trim() || !resourceForm.capacity.trim()) {
      setError("Resource name and capacity are required.");
      return;
    }
    const existingID = resourceForm.id;
    await runMutation(
      "resource",
      "resource",
      existingID ? "Resource pool saved." : "Resource pool created.",
      (key, expectedVersion) => {
        const input = {
          action_key: key,
          expected_config_version: expectedVersion,
          name: resourceForm.name.trim(),
          capacity: Number(resourceForm.capacity)
        };
        return existingID
          ? updateManleAICalendarResource(salonID, existingID, input, surface)
          : createManleAICalendarResource(salonID, input, surface);
      }
    );
  }

  async function archiveResource(resource: ManleAICalendarResourcePool) {
    await runMutation(`archive-resource-${resource.id}`, `archive-resource-${resource.id}`, "Resource pool archived.", (key, expectedVersion) =>
      archiveManleAICalendarResource(salonID, resource.id, {
        action_key: key,
        expected_config_version: expectedVersion
      }, surface)
    );
  }

  function updateExceptionForm(next: ExceptionForm) {
    resetActionKey("exception");
    setExceptionForm(next);
  }

  async function addException() {
    if (!calendar?.config) {
      setError("Save the internal scheduling policy before adding exceptions.");
      return;
    }
    if (!exceptionForm.startsAt || !exceptionForm.endsAt) {
      setError("Exception start and end times are required.");
      return;
    }
    if (exceptionForm.scopeType === "resource" && !exceptionForm.resourcePoolID) {
      setError("Choose a resource pool for this exception.");
      return;
    }
    const startsAt = salonLocalDateTimeToISO(exceptionForm.startsAt, timezone);
    const endsAt = salonLocalDateTimeToISO(exceptionForm.endsAt, timezone);
    if (!startsAt || !endsAt || new Date(startsAt).getTime() >= new Date(endsAt).getTime()) {
      setError(`Enter a valid exception range in ${timezone}.`);
      return;
    }
    await runMutation("exception", "exception", "Calendar exception added.", (key, expectedVersion) =>
      createManleAICalendarException(salonID, {
        action_key: key,
        expected_config_version: expectedVersion,
        scope_type: exceptionForm.scopeType,
        resource_pool_id: exceptionForm.scopeType === "resource" ? exceptionForm.resourcePoolID : undefined,
        effect: exceptionForm.effect,
        starts_at: startsAt,
        ends_at: endsAt,
        capacity_override:
          exceptionForm.effect === "capacity_override" ? nullableNumber(exceptionForm.capacityOverride) : null,
        reason: exceptionForm.reason.trim() || undefined
      }, surface)
    );
  }

  async function cancelException(exceptionID: string) {
    await runMutation(`cancel-exception-${exceptionID}`, `cancel-exception-${exceptionID}`, "Calendar exception cancelled.", (key, expectedVersion) =>
      cancelManleAICalendarException(salonID, exceptionID, {
        action_key: key,
        expected_config_version: expectedVersion
      }, surface)
    );
  }

  async function activate() {
    await runMutation("activate", "activate", "Calendar configuration activated for audit.", (key, expectedVersion) =>
      activateManleAICalendar(salonID, {
        action_key: key,
        expected_config_version: expectedVersion
      }, surface)
    );
  }

  if (loading) {
    return (
      <div className="space-y-6">
        <SchedulingReadinessCard calendar={null} loading />
        <Skeleton className="h-96" />
        <Skeleton className="h-80" />
      </div>
    );
  }

  if (loadError || !calendar) {
    return <SchedulingReadinessCard calendar={null} error={loadError} onRetry={() => void load()} />;
  }

  const constraints = calendar.constraints;
  const activeResources = calendar.resources.filter((resource) => !resource.archived_at);
  const visibleExceptions = calendar.exceptions.filter((exception) => exception.scope_type !== "staff");
  const availableExceptionEffects = exceptionEffectsForScope(
    exceptionForm.scopeType,
    constraints.exception_effects
  );
  const activationCurrent = Boolean(
    calendar.config?.activated_at &&
    calendar.config.activated_version !== null &&
    calendar.config.activated_version === calendar.config.version
  );

  return (
    <div className="space-y-6">
      <SchedulingReadinessCard calendar={calendar} showSetupLinks={false} />

      {error ? <Alert title="Calendar setup not updated" message={error} /> : null}
      {success ? <Alert type="success" title="Calendar setup updated" message={success} /> : null}

      <Card>
        <div className="flex flex-col justify-between gap-3 sm:flex-row sm:items-start">
          <div>
            <CardTitle>ManleAI Calendar policy</CardTitle>
            <CardDescription>
              Configure the salon-owned scheduling policy in {calendar.timezone || timezone}. These settings do not switch scheduling authority.
            </CardDescription>
          </div>
          <Badge value={calendar.config ? "configured" : "required"} />
        </div>

        <div className="mt-5 grid gap-4 md:grid-cols-2 xl:grid-cols-4">
          <NumberField
            label="Slot interval (minutes)"
            value={configForm.slotStepMinutes}
            constraint={constraints.slot_step_minutes}
            help={`Must divide ${constraints.slot_step_minutes.must_divide_minutes} minutes evenly.`}
            disabled={Boolean(busy)}
            onChange={(value) => updateConfigForm({ ...configForm, slotStepMinutes: value })}
          />
          <NumberField
            label="Minimum booking notice"
            value={configForm.minimumBookingNoticeMinutes}
            constraint={constraints.minimum_booking_notice_minutes}
            suffix="minutes"
            disabled={Boolean(busy)}
            onChange={(value) => updateConfigForm({ ...configForm, minimumBookingNoticeMinutes: value })}
          />
          <NumberField
            label="Booking horizon"
            value={configForm.bookingHorizonDays}
            constraint={constraints.booking_horizon_days}
            suffix="days"
            disabled={Boolean(busy)}
            onChange={(value) => updateConfigForm({ ...configForm, bookingHorizonDays: value })}
          />
          <NumberField
            label="Maximum party size"
            value={configForm.maxPartySize}
            constraint={constraints.max_party_size}
            disabled={Boolean(busy)}
            onChange={(value) => updateConfigForm({ ...configForm, maxPartySize: value })}
          />
          <NumberField
            label="Default buffer before"
            value={configForm.defaultBufferBeforeMinutes}
            constraint={constraints.buffer_minutes}
            suffix="minutes"
            disabled={Boolean(busy)}
            onChange={(value) => updateConfigForm({ ...configForm, defaultBufferBeforeMinutes: value })}
          />
          <NumberField
            label="Default buffer after"
            value={configForm.defaultBufferAfterMinutes}
            constraint={constraints.buffer_minutes}
            suffix="minutes"
            disabled={Boolean(busy)}
            onChange={(value) => updateConfigForm({ ...configForm, defaultBufferAfterMinutes: value })}
          />
          <NumberField
            label="Reschedule cutoff"
            value={configForm.rescheduleCutoffMinutes}
            constraint={constraints.cutoff_minutes}
            suffix="minutes"
            optional="Leave blank to disable AI rescheduling."
            disabled={Boolean(busy)}
            onChange={(value) => updateConfigForm({ ...configForm, rescheduleCutoffMinutes: value })}
          />
          <NumberField
            label="Cancellation cutoff"
            value={configForm.cancellationCutoffMinutes}
            constraint={constraints.cutoff_minutes}
            suffix="minutes"
            optional="Leave blank to disable AI cancellation."
            disabled={Boolean(busy)}
            onChange={(value) => updateConfigForm({ ...configForm, cancellationCutoffMinutes: value })}
          />
        </div>
        <div className="mt-5 flex justify-end">
          <Button type="button" onClick={() => void saveConfig()} disabled={Boolean(busy)}>
            <Save className="h-4 w-4" />
            {busy === "config" ? "Saving..." : "Save scheduling policy"}
          </Button>
        </div>
      </Card>

      <Card>
        <div className="flex flex-col justify-between gap-3 sm:flex-row sm:items-start">
          <div>
            <CardTitle>Local salon hours</CardTitle>
            <CardDescription>
              These are ManleAI-owned hours. Square-imported hours remain separate provider reference data.
            </CardDescription>
          </div>
          <Badge value={hours.length > 0 ? "configured" : "required"} />
        </div>
        <div className="mt-5 space-y-4">
          {dayLabels.map((day, dayOfWeek) => {
            const dayPeriods = hours.filter((period) => period.dayOfWeek === dayOfWeek);
            return (
              <div key={day} className="rounded-md border border-line p-4">
                <div className="flex flex-col justify-between gap-2 sm:flex-row sm:items-center">
                  <div>
                    <div className="text-sm font-semibold text-ink">{day}</div>
                    <div className="mt-1 text-xs text-muted">{dayPeriods.length ? `${dayPeriods.length} opening period${dayPeriods.length === 1 ? "" : "s"}` : "Closed"}</div>
                  </div>
                  <Button type="button" variant="secondary" className="w-full sm:w-auto" onClick={() => addHour(dayOfWeek)} disabled={Boolean(busy)}>
                    <Plus className="h-4 w-4" />
                    Add period
                  </Button>
                </div>
                {dayPeriods.length > 0 ? (
                  <div className="mt-3 space-y-3">
                    {dayPeriods.map((period) => (
                      <div key={period.key} className="grid gap-2 sm:grid-cols-[1fr_1fr_auto]">
                        <TimeField label="Opens" value={period.start} disabled={Boolean(busy)} onChange={(value) => updateHour(period.key, { start: value })} />
                        <TimeField label="Closes" value={period.end} disabled={Boolean(busy)} onChange={(value) => updateHour(period.key, { end: value })} />
                        <Button type="button" variant="danger" className="self-end" onClick={() => removeHour(period.key)} disabled={Boolean(busy)}>
                          <XCircle className="h-4 w-4" />
                          Remove
                        </Button>
                      </div>
                    ))}
                  </div>
                ) : null}
              </div>
            );
          })}
        </div>
        <div className="mt-5 flex justify-end">
          <Button type="button" onClick={() => void saveHours()} disabled={Boolean(busy) || !calendar.config}>
            <Save className="h-4 w-4" />
            {busy === "hours" ? "Saving..." : "Save local hours"}
          </Button>
        </div>
      </Card>

      <Card>
        <div className="flex flex-col justify-between gap-3 sm:flex-row sm:items-start">
          <div>
            <CardTitle>Shared resource pools</CardTitle>
            <CardDescription>
              Optional shared capacity such as pedicure chairs. Pooled execution remains blocked until the internal capacity engine ships.
            </CardDescription>
          </div>
          <Badge value={calendar.readiness.capabilities?.pooled_capacity === true ? "ready" : "blocked"} />
        </div>
        <div className="mt-5 grid gap-4 md:grid-cols-[1fr_12rem_auto] md:items-end">
          <TextField label="Resource name" value={resourceForm.name} maxLength={constraints.resource_name_max_characters} disabled={Boolean(busy)} onChange={(value) => updateResourceForm({ ...resourceForm, name: value })} />
          <NumberField label="Capacity" value={resourceForm.capacity} constraint={constraints.resource_capacity} disabled={Boolean(busy)} onChange={(value) => updateResourceForm({ ...resourceForm, capacity: value })} />
          <div className="flex gap-2">
            <Button type="button" className="flex-1" onClick={() => void saveResource()} disabled={Boolean(busy) || !calendar.config}>
              <Save className="h-4 w-4" />
              {busy === "resource" ? "Saving..." : resourceForm.id ? "Save" : "Add"}
            </Button>
            {resourceForm.id ? (
              <Button type="button" variant="secondary" onClick={() => setResourceForm(emptyResourceForm())} disabled={Boolean(busy)}>
                Cancel
              </Button>
            ) : null}
          </div>
        </div>
        {calendar.resources.length === 0 ? (
          <div className="mt-5 rounded-md border border-line p-5 text-sm leading-6 text-muted">
            No shared resource pools. Staff-only services do not require one.
          </div>
        ) : (
          <div className="mt-5 grid gap-3 md:grid-cols-2">
            {calendar.resources.map((resource) => (
              <div key={resource.id} className="rounded-md border border-line p-4">
                <div className="flex items-start justify-between gap-3">
                  <div>
                    <div className="font-semibold text-ink">{resource.name}</div>
                    <div className="mt-1 text-sm text-muted">Capacity {resource.capacity}</div>
                  </div>
                  <Badge value={resource.archived_at ? "archived" : "active"} />
                </div>
                <div className="mt-4 grid gap-2 sm:grid-cols-2">
                  <Button type="button" variant="secondary" onClick={() => editResource(resource)} disabled={Boolean(busy) || Boolean(resource.archived_at)}>
                    Edit
                  </Button>
                  <Button type="button" variant="danger" onClick={() => void archiveResource(resource)} disabled={Boolean(busy) || Boolean(resource.archived_at)}>
                    <Archive className="h-4 w-4" />
                    {busy === `archive-resource-${resource.id}` ? "Archiving..." : "Archive"}
                  </Button>
                </div>
              </div>
            ))}
          </div>
        )}
      </Card>

      <Card>
        <div className="flex flex-col justify-between gap-3 sm:flex-row sm:items-start">
          <div>
            <CardTitle>Salon and resource exceptions</CardTitle>
            <CardDescription>
              Add temporary closures, openings, or capacity overrides in {timezone}. Existing exceptions are cancelled, never deleted.
            </CardDescription>
          </div>
          <CalendarClock className="h-5 w-5 text-brand" />
        </div>
        <div className="mt-5 grid gap-4 md:grid-cols-2 xl:grid-cols-3">
          <SelectField label="Scope" value={exceptionForm.scopeType} disabled={Boolean(busy)} onChange={(value) => {
            const scopeType = value as ManleAICalendarExceptionScope;
            const effects = exceptionEffectsForScope(scopeType, constraints.exception_effects);
            updateExceptionForm({ ...exceptionForm, scopeType, resourcePoolID: "", effect: effects[0], capacityOverride: "" });
          }}>
            {constraints.exception_scope_types.filter((scope) => scope !== "staff").map((scope) => <option key={scope} value={scope}>{scope}</option>)}
          </SelectField>
          {exceptionForm.scopeType === "resource" ? (
            <SelectField label="Resource pool" value={exceptionForm.resourcePoolID} disabled={Boolean(busy)} onChange={(value) => updateExceptionForm({ ...exceptionForm, resourcePoolID: value })}>
              <option value="">Choose resource</option>
              {activeResources.map((resource) => <option key={resource.id} value={resource.id}>{resource.name}</option>)}
            </SelectField>
          ) : null}
          <SelectField label="Effect" value={exceptionForm.effect} disabled={Boolean(busy)} onChange={(value) => updateExceptionForm({ ...exceptionForm, effect: value as ManleAICalendarExceptionEffect, capacityOverride: "" })}>
            {availableExceptionEffects.map((effect) => <option key={effect} value={effect}>{effect.replaceAll("_", " ")}</option>)}
          </SelectField>
          <DateTimeField label="Starts" value={exceptionForm.startsAt} timezone={timezone} disabled={Boolean(busy)} onChange={(value) => updateExceptionForm({ ...exceptionForm, startsAt: value })} />
          <DateTimeField label="Ends" value={exceptionForm.endsAt} timezone={timezone} disabled={Boolean(busy)} onChange={(value) => updateExceptionForm({ ...exceptionForm, endsAt: value })} />
          {exceptionForm.effect === "capacity_override" ? (
            <NumberField label="Capacity override" value={exceptionForm.capacityOverride} constraint={constraints.exception_capacity_override} disabled={Boolean(busy)} onChange={(value) => updateExceptionForm({ ...exceptionForm, capacityOverride: value })} />
          ) : null}
          <TextField label="Reason (optional)" value={exceptionForm.reason} help={`Backend limit: ${constraints.exception_reason_max_bytes} bytes.`} disabled={Boolean(busy)} onChange={(value) => updateExceptionForm({ ...exceptionForm, reason: value })} />
        </div>
        <div className="mt-5 flex justify-end">
          <Button type="button" onClick={() => void addException()} disabled={Boolean(busy) || !calendar.config}>
            <Plus className="h-4 w-4" />
            {busy === "exception" ? "Adding..." : "Add exception"}
          </Button>
        </div>
        {visibleExceptions.length === 0 ? (
          <div className="mt-5 rounded-md border border-line p-5 text-sm leading-6 text-muted">
            No salon or resource exceptions.
          </div>
        ) : (
          <div className="mt-5 space-y-3">
            {visibleExceptions.map((exception) => {
              const resource = calendar.resources.find((item) => item.id === exception.resource_pool_id);
              return (
                <div key={exception.id} className="rounded-md border border-line p-4">
                  <div className="flex flex-col justify-between gap-3 sm:flex-row sm:items-start">
                    <div>
                      <div className="flex flex-wrap items-center gap-2">
                        <span className="font-semibold text-ink">{exception.scope_type === "resource" ? resource?.name || "Resource" : "Salon"}</span>
                        <Badge value={exception.effect} />
                        <Badge value={exception.cancelled_at ? "cancelled" : "active"} />
                      </div>
                      <div className="mt-2 text-sm leading-6 text-muted">
                        {formatDateTime(exception.starts_at, timezone)} – {formatDateTime(exception.ends_at, timezone)}
                      </div>
                      {exception.reason ? <div className="mt-1 text-sm text-muted">{exception.reason}</div> : null}
                    </div>
                    <Button type="button" variant="danger" className="w-full sm:w-auto" onClick={() => void cancelException(exception.id)} disabled={Boolean(busy) || Boolean(exception.cancelled_at)}>
                      {busy === `cancel-exception-${exception.id}` ? "Cancelling..." : "Cancel exception"}
                    </Button>
                  </div>
                </div>
              );
            })}
          </div>
        )}
      </Card>

      <Card className="border-blue-200 bg-blue-50 shadow-none">
        <div className="flex flex-col justify-between gap-4 sm:flex-row sm:items-center">
          <div>
            <CardTitle>Activation audit</CardTitle>
            <CardDescription className="text-blue-900">
              Activation records that configuration readiness passed. It does not switch scheduling authority or enable internal appointment execution.
            </CardDescription>
            {calendar.config?.activated_at ? (
              <div className="mt-2 text-xs text-muted">
                {activationCurrent ? "Activated for current version" : "Previous activation is stale"} · {formatDateTime(calendar.config.activated_at, timezone)}
                {calendar.config.activated_version !== null ? ` · version ${calendar.config.activated_version}` : ""}
              </div>
            ) : null}
          </div>
          <Button type="button" onClick={() => void activate()} disabled={Boolean(busy) || !calendar.readiness.configuration_ready || activationCurrent}>
            {busy === "activate" ? "Activating..." : activationCurrent ? "Activated for current version" : calendar.config?.activated_at ? "Re-activate current version" : "Activate configuration"}
          </Button>
        </div>
      </Card>
    </div>
  );
}

function NumberField({ label, value, constraint, suffix, optional, help, disabled, onChange }: {
  label: string;
  value: string;
  constraint: { minimum: number; maximum: number };
  suffix?: string;
  optional?: string;
  help?: string;
  disabled: boolean;
  onChange: (value: string) => void;
}) {
  return (
    <label className="block">
      <span className="text-sm font-medium text-ink">{label}</span>
      <input type="number" min={constraint.minimum} max={constraint.maximum} value={value} onChange={(event) => onChange(event.target.value)} disabled={disabled} className="mt-2 h-10 w-full rounded-md border border-line bg-white px-3 text-sm text-ink outline-none focus:border-brand disabled:bg-slate-100" />
      <span className="mt-1 block text-xs leading-5 text-muted">{optional || help || `${constraint.minimum}–${constraint.maximum}${suffix ? ` ${suffix}` : ""}`}</span>
    </label>
  );
}

function TextField({ label, value, help, maxLength, disabled, onChange }: { label: string; value: string; help?: string; maxLength?: number; disabled: boolean; onChange: (value: string) => void }) {
  return (
    <label className="block">
      <span className="text-sm font-medium text-ink">{label}</span>
      <input value={value} maxLength={maxLength} onChange={(event) => onChange(event.target.value)} disabled={disabled} className="mt-2 h-10 w-full rounded-md border border-line bg-white px-3 text-sm text-ink outline-none focus:border-brand disabled:bg-slate-100" />
      {help ? <span className="mt-1 block text-xs leading-5 text-muted">{help}</span> : null}
    </label>
  );
}

function TimeField({ label, value, disabled, onChange }: { label: string; value: string; disabled: boolean; onChange: (value: string) => void }) {
  return (
    <label className="block">
      <span className="text-xs font-semibold uppercase tracking-wide text-muted">{label}</span>
      <input inputMode="numeric" placeholder="HH:MM" value={value} onChange={(event) => onChange(event.target.value)} disabled={disabled} className="mt-1 h-10 w-full rounded-md border border-line bg-white px-3 text-sm text-ink outline-none focus:border-brand disabled:bg-slate-100" />
    </label>
  );
}

function SelectField({ label, value, disabled, onChange, children }: { label: string; value: string; disabled: boolean; onChange: (value: string) => void; children: React.ReactNode }) {
  return (
    <label className="block">
      <span className="text-sm font-medium text-ink">{label}</span>
      <select value={value} onChange={(event) => onChange(event.target.value)} disabled={disabled} className="mt-2 h-10 w-full rounded-md border border-line bg-white px-3 text-sm text-ink outline-none focus:border-brand disabled:bg-slate-100">
        {children}
      </select>
    </label>
  );
}

function DateTimeField({ label, value, timezone, disabled, onChange }: { label: string; value: string; timezone: string; disabled: boolean; onChange: (value: string) => void }) {
  return (
    <label className="block">
      <span className="text-sm font-medium text-ink">{label}</span>
      <input type="datetime-local" value={value} onChange={(event) => onChange(event.target.value)} disabled={disabled} className="mt-2 h-10 w-full rounded-md border border-line bg-white px-3 text-sm text-ink outline-none focus:border-brand disabled:bg-slate-100" />
      <span className="mt-1 block text-xs text-muted">Salon timezone: {timezone}</span>
    </label>
  );
}

function emptyConfigForm(): ConfigForm {
  return { slotStepMinutes: "", minimumBookingNoticeMinutes: "", bookingHorizonDays: "", rescheduleCutoffMinutes: "", cancellationCutoffMinutes: "", maxPartySize: "", defaultBufferBeforeMinutes: "", defaultBufferAfterMinutes: "" };
}

function configToForm(calendar: ManleAICalendarAggregate): ConfigForm {
  const config = calendar.config;
  if (!config) return emptyConfigForm();
  return {
    slotStepMinutes: String(config.slot_step_minutes),
    minimumBookingNoticeMinutes: String(config.minimum_booking_notice_minutes),
    bookingHorizonDays: String(config.booking_horizon_days),
    rescheduleCutoffMinutes: config.reschedule_cutoff_minutes === null ? "" : String(config.reschedule_cutoff_minutes),
    cancellationCutoffMinutes: config.cancellation_cutoff_minutes === null ? "" : String(config.cancellation_cutoff_minutes),
    maxPartySize: String(config.max_party_size),
    defaultBufferBeforeMinutes: String(config.default_buffer_before_minutes),
    defaultBufferAfterMinutes: String(config.default_buffer_after_minutes)
  };
}

function hoursToDrafts(calendar: ManleAICalendarAggregate): HourDraft[] {
  return calendar.hours.map((period) => ({ key: period.id, dayOfWeek: period.day_of_week, start: minuteToTime(period.start_minute), end: minuteToTime(period.end_minute) }));
}

function emptyResourceForm(): ResourceForm {
  return { id: "", name: "", capacity: "" };
}

function emptyExceptionForm(): ExceptionForm {
  return { scopeType: "salon", resourcePoolID: "", effect: "unavailable", startsAt: "", endsAt: "", capacityOverride: "", reason: "" };
}

function nullableNumber(value: string) {
  return value.trim() === "" ? null : Number(value);
}

function draftKey() {
  return newManleAICalendarActionKey();
}

function minuteToTime(value: number) {
  if (value === 1440) return "24:00";
  const hours = Math.floor(value / 60);
  const minutes = value % 60;
  return `${String(hours).padStart(2, "0")}:${String(minutes).padStart(2, "0")}`;
}

function parseTime(value: string) {
  const match = /^(\d{1,2}):(\d{2})$/.exec(value.trim());
  if (!match) return null;
  const hours = Number(match[1]);
  const minutes = Number(match[2]);
  if (hours === 24 && minutes === 0) return 1440;
  if (hours < 0 || hours > 23 || minutes < 0 || minutes > 59) return null;
  return hours * 60 + minutes;
}

function formatDateTime(value: string, timezone: string) {
  return new Intl.DateTimeFormat("en-US", { timeZone: timezone, dateStyle: "medium", timeStyle: "short" }).format(new Date(value));
}

function exceptionEffectsForScope(
  scope: ManleAICalendarExceptionScope,
  effects: ManleAICalendarExceptionEffect[]
) {
  const allowed = scope === "resource" ? ["capacity_override"] : ["available", "unavailable"];
  return effects.filter((effect) => allowed.includes(effect)) as ManleAICalendarExceptionEffect[];
}
