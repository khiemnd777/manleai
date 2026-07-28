"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { ExternalLink, RefreshCcw, ShieldCheck } from "lucide-react";
import { Alert } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardDescription, CardTitle } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { ProviderConfigurationPanel } from "@/features/integrations/square-integration";
import { newBusinessActionKey } from "@/lib/api/business";
import { apiRequest, RequestError } from "@/lib/api/client";
import {
  openAIConfigPayload,
  openAIConfigToForm,
  platformIntegrationConfigBasePath,
  squareConfigPayload,
  squareConfigToForm,
  twilioConfigPayload,
  twilioConfigToForm
} from "@/lib/api/integration-config-contract";
import type {
  IntegrationConfigProvider,
  OpenAIConfigForm,
  SquareConfigForm,
  TwilioConfigForm
} from "@/lib/api/integration-config-contract";
import type {
  IntegrationConfigs,
  OpenAIIntegrationConfig,
  POSConnection,
  POSLocation,
  SquareIntegrationConfig,
  SquareReadiness,
  SyncLog,
  TwilioIntegrationConfig
} from "@/types/api";

type ProviderAction = { signature: string; key: string };

export function TechnicalIntegrationSettings({ tenantID }: { tenantID: string }) {
  const [configs, setConfigs] = useState<IntegrationConfigs | null>(null);
  const [activeTab, setActiveTab] = useState<IntegrationConfigProvider>("square");
  const [squareForm, setSquareForm] = useState<SquareConfigForm>(() => squareConfigToForm());
  const [twilioForm, setTwilioForm] = useState<TwilioConfigForm>(() => twilioConfigToForm());
  const [openAIForm, setOpenAIForm] = useState<OpenAIConfigForm>(() => openAIConfigToForm());
  const [loading, setLoading] = useState(true);
  const [blocked, setBlocked] = useState(false);
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");
  const [success, setSuccess] = useState("");
  const loadRequestRef = useRef(0);
  const actionKeys = useRef<Partial<Record<IntegrationConfigProvider, ProviderAction>>>({});
  const base = platformIntegrationConfigBasePath(tenantID);

  const load = useCallback(async () => {
    const requestID = ++loadRequestRef.current;
    setLoading(true);
    setError("");
    setBlocked(false);
    try {
      const value = await apiRequest<IntegrationConfigs>(base);
      if (requestID !== loadRequestRef.current) return;
      setConfigs(value);
      setSquareForm(squareConfigToForm(value.square));
      setTwilioForm(twilioConfigToForm(value.twilio));
      setOpenAIForm(openAIConfigToForm(value.openai));
      actionKeys.current = {};
    } catch (failure) {
      if (requestID !== loadRequestRef.current) return;
      if (failure instanceof RequestError && failure.status === 403) setBlocked(true);
      else setError(errorMessage(failure, "Could not load technical integration settings."));
    } finally {
      if (requestID === loadRequestRef.current) setLoading(false);
    }
  }, [base]);

  useEffect(() => {
    setActiveTab("square");
    setConfigs(null);
    setSquareForm(squareConfigToForm());
    setTwilioForm(twilioConfigToForm());
    setOpenAIForm(openAIConfigToForm());
    setSuccess("");
    actionKeys.current = {};
    void load();
    return () => {
      loadRequestRef.current += 1;
    };
  }, [load]);

  async function saveProvider<T>(
    provider: IntegrationConfigProvider,
    payload: Record<string, unknown>
  ): Promise<T | null> {
    if (!configs) return null;
    const expectedVersion = configs[provider].version ?? 0;
    const signature = JSON.stringify({ payload, expectedVersion });
    let action = actionKeys.current[provider];
    if (!action || action.signature !== signature) {
      action = { signature, key: newBusinessActionKey(`technical-${provider}`) };
      actionKeys.current[provider] = action;
    }
    setBusy(`save-${provider}-config`);
    setError("");
    setSuccess("");
    try {
      const value = await apiRequest<T>(`${base}/${provider}`, {
        method: "PUT",
        body: JSON.stringify({
          ...payload,
          action_key: action.key,
          expected_version: expectedVersion
        })
      });
      delete actionKeys.current[provider];
      return value;
    } catch (failure) {
      setError(errorMessage(failure, `Could not save ${providerLabel(provider)} settings.`));
      return null;
    } finally {
      setBusy("");
    }
  }

  async function saveSquare() {
    const updated = await saveProvider<SquareIntegrationConfig>("square", squareConfigPayload(squareForm));
    if (!updated) return;
    setConfigs((current) => (current ? { ...current, square: updated } : current));
    setSquareForm(squareConfigToForm(updated));
    setSuccess("Square Appointments configuration saved. Secret values remain write-only.");
  }

  async function saveTwilio() {
    const updated = await saveProvider<TwilioIntegrationConfig>("twilio", twilioConfigPayload(twilioForm));
    if (!updated) return;
    setConfigs((current) => (current ? { ...current, twilio: updated } : current));
    setTwilioForm(twilioConfigToForm(updated));
    setSuccess("Twilio voice and owner notification configuration saved. Secret values remain write-only.");
  }

  async function saveOpenAI() {
    const updated = await saveProvider<OpenAIIntegrationConfig>("openai", openAIConfigPayload(openAIForm));
    if (!updated) return;
    setConfigs((current) => (current ? { ...current, openai: updated } : current));
    setOpenAIForm(openAIConfigToForm(updated));
    setSuccess("OpenAI voice AI configuration saved. Secret values remain write-only.");
  }

  if (loading) {
    return (
      <div className="space-y-4">
        <Skeleton className="h-12 w-72" />
        <Skeleton className="h-80 w-full" />
      </div>
    );
  }
  if (blocked) {
    return (
      <Alert
        title="Technical access denied"
        message="This Platform account needs technical.read for this exact salon. Tenant membership and Business access do not grant provider configuration access."
      />
    );
  }

  return (
    <div className="space-y-5">
      <div className="flex flex-col gap-3 sm:flex-row sm:items-end sm:justify-between">
        <div>
          <h2 className="text-lg font-bold text-ink">Technical integration settings</h2>
          <p className="mt-1 text-sm text-muted">
            Square, Twilio, and OpenAI configuration is salon-scoped and managed only from Platform UI.
          </p>
        </div>
        <Button type="button" variant="secondary" disabled={Boolean(busy)} onClick={() => void load()}>
          <RefreshCcw className="h-4 w-4" /> Refresh
        </Button>
      </div>

      {error ? <Alert title="Technical settings need attention" message={error} /> : null}
      {success ? <Alert type="success" title="Saved" message={success} /> : null}

      <ProviderConfigurationPanel
        activeTab={activeTab}
        busy={busy}
        configs={configs}
        openAIForm={openAIForm}
        setActiveTab={setActiveTab}
        setOpenAIForm={setOpenAIForm}
        setSquareForm={setSquareForm}
        setTwilioForm={setTwilioForm}
        squareForm={squareForm}
        twilioForm={twilioForm}
        onSaveOpenAI={() => void saveOpenAI()}
        onSaveSquare={() => void saveSquare()}
        onSaveTwilio={() => void saveTwilio()}
      />

      {activeTab === "square" ? <SquareConnectionPanel tenantID={tenantID} /> : null}

      <Card className="border-blue-200 bg-blue-50">
        <div className="flex gap-3">
          <ShieldCheck className="mt-0.5 h-5 w-5 flex-none text-blue-700" />
          <div>
            <CardTitle>Secret handling</CardTitle>
            <CardDescription>
              Configured/source booleans and masked destinations are readable. Client secrets, auth tokens,
              Account SIDs, API keys, sender identities, and clear controls are write-only and never serialized back.
            </CardDescription>
          </div>
        </div>
      </Card>
    </div>
  );
}

type SquareStatus = {
  connection?: POSConnection;
  sync_logs: SyncLog[];
  readiness: SquareReadiness;
};

function SquareConnectionPanel({ tenantID }: { tenantID: string }) {
  const base = `/api/platform/tenants/${encodeURIComponent(tenantID)}/technical/square`;
  const [status, setStatus] = useState<SquareStatus | null>(null);
  const [locations, setLocations] = useState<POSLocation[]>([]);
  const [locationID, setLocationID] = useState("");
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");
  const requestRef = useRef(0);
  const aiAction = useRef<{ enabled: boolean; key: string } | null>(null);

  const load = useCallback(async () => {
    const requestID = ++requestRef.current;
    setError("");
    try {
      const value = await apiRequest<SquareStatus>(`${base}/status`);
      if (requestID !== requestRef.current) return;
      setStatus(value);
      if (value.connection?.id) {
        const locationResponse = await apiRequest<{ locations: POSLocation[] }>(`${base}/locations`);
        if (requestID !== requestRef.current) return;
        setLocations(locationResponse.locations);
        setLocationID(value.connection.location_id || locationResponse.locations[0]?.id || "");
      } else {
        setLocations([]);
        setLocationID("");
      }
    } catch (failure) {
      if (requestID === requestRef.current) {
        setError(errorMessage(failure, "Could not load Square connection status."));
      }
    }
  }, [base]);

  useEffect(() => {
    setStatus(null);
    setLocations([]);
    setLocationID("");
    setError("");
    aiAction.current = null;
    void load();
    return () => {
      requestRef.current += 1;
    };
  }, [load]);

  async function connect() {
    setBusy("connect");
    setError("");
    try {
      const result = await apiRequest<{ url: string }>(`${base}/connect-url`);
      window.location.assign(result.url);
    } catch (failure) {
      setError(errorMessage(failure, "Could not start Square OAuth."));
      setBusy("");
    }
  }

  async function selectLocation() {
    setBusy("location");
    setError("");
    try {
      await apiRequest(`${base}/select-location`, {
        method: "POST",
        body: JSON.stringify({ location_id: locationID })
      });
      await load();
    } catch (failure) {
      setError(errorMessage(failure, "Could not select Square location."));
    } finally {
      setBusy("");
    }
  }

  async function sync() {
    setBusy("sync");
    setError("");
    try {
      await apiRequest(`${base}/sync`, { method: "POST" });
      await load();
    } catch (failure) {
      setError(errorMessage(failure, "Could not sync Square catalog."));
    } finally {
      setBusy("");
    }
  }

  async function setAIEnabled(enabled: boolean) {
    if (!status) return;
    let action = aiAction.current;
    if (!action || action.enabled !== enabled) {
      action = {
        enabled,
        key: newBusinessActionKey(enabled ? "enable-ai-runtime" : "disable-ai-runtime")
      };
      aiAction.current = action;
    }
    setBusy("ai-runtime");
    setError("");
    try {
      await apiRequest(`${base}/ai-booking/${enabled ? "enable" : "disable"}`, {
        method: "POST",
        body: JSON.stringify({
          action_key: action.key,
          expected_version: status.readiness.ai_runtime_version
        })
      });
      aiAction.current = null;
      await load();
    } catch (failure) {
      setError(errorMessage(failure, "Could not update the salon AI runtime."));
    } finally {
      setBusy("");
    }
  }

  const connected = Boolean(status?.connection?.id) && status?.connection?.status !== "not_connected";
  const aiEnabled = Boolean(status?.readiness?.ai_enabled);

  return (
    <Card>
      <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
        <div>
          <CardTitle>Square connection &amp; AI runtime</CardTitle>
          <CardDescription>
            OAuth, location, sync, readiness, and the salon-wide AI runtime switch are operated by Platform staff.
            Connecting does not switch scheduling authority or enable AI implicitly.
          </CardDescription>
        </div>
        <div className="flex gap-2">
          <Badge value={connected ? status?.connection?.status || "connected" : "not_connected"} />
          <Badge value={aiEnabled ? "ai_enabled" : "ai_disabled"} />
        </div>
      </div>
      {error ? (
        <div className="mt-4">
          <Alert title="Square operation failed" message={error} />
        </div>
      ) : null}
      <div className="mt-5 grid gap-4 sm:grid-cols-3">
        <Metric label="Scheduling authority" value={status?.readiness?.scheduling_authority || "Unavailable"} />
        <Metric
          label="Latest sync"
          value={status?.connection?.last_sync_at ? new Date(status.connection.last_sync_at).toLocaleString() : "Never"}
        />
        <Metric label="AI runtime version" value={`v${status?.readiness?.ai_runtime_version ?? 0}`} />
      </div>
      <div className="mt-5 flex flex-col gap-3 sm:flex-row sm:flex-wrap">
        <Button type="button" variant="secondary" disabled={Boolean(busy)} onClick={() => void connect()}>
          <ExternalLink className="h-4 w-4" />
          {busy === "connect" ? "Opening…" : connected ? "Reconnect Square" : "Connect Square"}
        </Button>
        {connected ? (
          <>
            <select
              className="field"
              value={locationID}
              onChange={(event) => setLocationID(event.target.value)}
              aria-label="Square location"
            >
              {locations.map((location) => (
                <option key={location.id} value={location.id}>
                  {location.name}
                </option>
              ))}
            </select>
            <Button
              type="button"
              variant="secondary"
              disabled={Boolean(busy) || !locationID}
              onClick={() => void selectLocation()}
            >
              {busy === "location" ? "Saving…" : "Select location"}
            </Button>
            <Button type="button" disabled={Boolean(busy)} onClick={() => void sync()}>
              <RefreshCcw className="h-4 w-4" />
              {busy === "sync" ? "Syncing…" : "Sync catalog"}
            </Button>
          </>
        ) : null}
        <Button
          type="button"
          variant={aiEnabled ? "danger" : "secondary"}
          disabled={Boolean(busy) || !status}
          onClick={() => void setAIEnabled(!aiEnabled)}
        >
          {busy === "ai-runtime" ? "Saving…" : aiEnabled ? "Disable AI runtime" : "Enable AI runtime"}
        </Button>
      </div>
    </Card>
  );
}

function Metric({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <p className="text-xs font-bold uppercase tracking-wide text-muted">{label}</p>
      <p className="mt-1 text-sm font-semibold text-ink">{value}</p>
    </div>
  );
}

function providerLabel(provider: IntegrationConfigProvider) {
  if (provider === "square") return "Square Appointments";
  if (provider === "twilio") return "Twilio";
  return "OpenAI";
}

function errorMessage(error: unknown, fallback: string) {
  return error instanceof Error && error.message ? error.message : fallback;
}
