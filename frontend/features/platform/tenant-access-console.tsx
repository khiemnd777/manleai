"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import { Clock3, RefreshCcw, ShieldAlert, UserCog, Users } from "lucide-react";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Card, CardDescription, CardTitle } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { AccessRows, AccessUserSearch, AccessUserSelect, errorMessage } from "@/features/platform/access-ui";
import {
  cancelSupportAccessRequest,
  createSupportAccessRequest,
  grantPII,
  getTenantAccessWorkspace,
  mutateMembership,
  mutateSalonAssignment,
  revokePII,
  revokeSupportAccessRequest,
  type AccessUser,
  type PIIGrant,
  type PlatformRoleAssignment,
  type SalonAssignment,
  type SupportAccessRequest,
  type TenantMembership
} from "@/lib/api/access";
import { applyCapabilitySelection, capabilityLabel, delegableCapabilities, type CapabilityDefinition } from "@/lib/api/access-contract";
import { BusinessMutationKeyManager } from "@/lib/api/business";
import { RequestError } from "@/lib/api/client";

type PIIScope = PIIGrant["scope"];
type AccessSection = "team" | "support" | "temporary" | "sensitive";

const piiScopes: Array<{ value: PIIScope; label: string }> = [
  { value: "customers", label: "Customer records" },
  { value: "appointments", label: "Appointment records" },
  { value: "notifications", label: "Notification records" }
];

export function TenantAccessConsole({ tenantID }: { tenantID: string }) {
	const [section, setSection] = useState<AccessSection>("team");
  const [memberships, setMemberships] = useState<TenantMembership[]>([]);
  const [roles, setRoles] = useState<PlatformRoleAssignment[]>([]);
  const [assignments, setAssignments] = useState<SalonAssignment[]>([]);
  const [grants, setGrants] = useState<PIIGrant[]>([]);
  const [capabilities, setCapabilities] = useState<CapabilityDefinition[]>([]);
  const [supportRequests, setSupportRequests] = useState<SupportAccessRequest[]>([]);
  const [managerUserID, setManagerUserID] = useState("");
  const [assignmentUserID, setAssignmentUserID] = useState("");
  const [assignmentPermissions, setAssignmentPermissions] = useState<string[]>([]);
  const [grantUserID, setGrantUserID] = useState("");
  const [grantScope, setGrantScope] = useState<PIIScope>("customers");
  const [grantReference, setGrantReference] = useState("");
  const [grantHours, setGrantHours] = useState("1");
  const [requestUserID, setRequestUserID] = useState("");
  const [requestCapabilities, setRequestCapabilities] = useState<string[]>([]);
  const [requestCallsPII, setRequestCallsPII] = useState(false);
  const [requestReference, setRequestReference] = useState("");
  const [requestHours, setRequestHours] = useState("24");
  const [loading, setLoading] = useState(true);
  const [adminBlocked, setAdminBlocked] = useState(false);
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");
  const [success, setSuccess] = useState("");
  const mutationKey = useRef(new BusinessMutationKeyManager());
  const grantExpiry = useRef<{ signature: string; expiresAt: string }>();

  async function load() {
    setLoading(true);
    setAdminBlocked(false);
    setError("");
    setSuccess("");
    try {
      const workspace = await getTenantAccessWorkspace(tenantID);
      setMemberships(workspace.memberships);
      setRoles(workspace.platform_roles);
      setAssignments(workspace.operator_assignments);
      setGrants(workspace.pii_grants);
      setCapabilities(workspace.capabilities);
      setSupportRequests(workspace.temporary_authorizations);
    } catch (failure) {
      if (failure instanceof RequestError && failure.status === 403) setAdminBlocked(true);
      else setError(errorMessage(failure, "Could not load salon access."));
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    void load();
  }, [tenantID]);

  const delegable = useMemo(() => delegableCapabilities(capabilities), [capabilities]);
  const activeOpsUsers = useMemo(
    () => roles.filter((item) => item.status === "active" && item.role === "platform_ops").map((item) => item.user),
    [roles]
  );
  const assignedOpsUsers = useMemo(
    () => activeOpsUsers.filter((user) => assignments.some((item) => item.user_id === user.id && item.status === "active")),
    [activeOpsUsers, assignments]
  );
  const supportCapabilities = useMemo(
    () => delegable.filter((item) => /^(services|training|calls)\./.test(item.name)),
    [delegable]
  );
  const callsRequestSelected = requestCapabilities.some((item) => item.startsWith("calls."));

  async function saveManager() {
    if (!managerUserID) return;
    const existing = memberships.find((item) => item.user_id === managerUserID);
    const payload = { tenantID, userID: managerUserID, version: existing?.version ?? 0 };
    const actionKey = mutationKey.current.forPayload("tenant-membership", payload);
    setBusy("membership");
    resetNotices();
    try {
      const response = await mutateMembership(tenantID, managerUserID, "tenant_business_manager", "active", payload.version, actionKey);
      setMemberships((items) => upsertByUser(items, response.data));
      mutationKey.current.clear();
      setSuccess(response.replayed ? "The exact salon-team change was recovered safely." : "Salon manager access saved.");
    } catch (failure) {
      setError(errorMessage(failure, "Could not save salon manager access."));
    } finally {
      setBusy("");
    }
  }

  async function toggleMembership(item: TenantMembership) {
    const status = item.status === "active" ? "revoked" : "active";
    const actionKey = mutationKey.current.forPayload("membership-status", { id: item.id, status, version: item.version });
    setBusy(`membership-${item.user_id}`);
    resetNotices();
    try {
      const response = await mutateMembership(tenantID, item.user_id, item.role, status, item.version, actionKey);
      setMemberships((items) => upsertByUser(items, response.data));
      mutationKey.current.clear();
      setSuccess(response.replayed ? "The exact salon-team change was recovered safely." : "Salon manager status saved.");
    } catch (failure) {
      setError(errorMessage(failure, "Could not change salon manager status."));
    } finally {
      setBusy("");
    }
  }

  function chooseOpsUser(userID: string) {
    setAssignmentUserID(userID);
    const existing = assignments.find((item) => item.user_id === userID);
    setAssignmentPermissions(existing?.permissions ?? []);
    mutationKey.current.clear();
  }

  async function saveAssignment() {
    if (!assignmentUserID || assignmentPermissions.length === 0) return;
    const existing = assignments.find((item) => item.user_id === assignmentUserID);
    const payload = { tenantID, userID: assignmentUserID, permissions: assignmentPermissions, version: existing?.version ?? 0 };
    const actionKey = mutationKey.current.forPayload("salon-assignment", payload);
    setBusy("assignment");
    resetNotices();
    try {
      const response = await mutateSalonAssignment(tenantID, assignmentUserID, "active", assignmentPermissions, payload.version, actionKey);
      setAssignments((items) => upsertByUser(items, response.data));
      mutationKey.current.clear();
      setSuccess(response.replayed ? "The exact support-access change was recovered safely." : "Platform support access saved.");
    } catch (failure) {
      setError(errorMessage(failure, "Could not save Platform support access."));
    } finally {
      setBusy("");
    }
  }

  async function toggleAssignment(item: SalonAssignment) {
    const status = item.status === "active" ? "revoked" : "active";
    const actionKey = mutationKey.current.forPayload("assignment-status", { id: item.id, status, version: item.version });
    setBusy(`assignment-${item.user_id}`);
    resetNotices();
    try {
      const response = await mutateSalonAssignment(tenantID, item.user_id, status, item.permissions, item.version, actionKey);
      setAssignments((items) => upsertByUser(items, response.data));
      mutationKey.current.clear();
      setSuccess(response.replayed ? "The exact support-access change was recovered safely." : "Platform support status saved.");
    } catch (failure) {
      setError(errorMessage(failure, "Could not change Platform support status."));
    } finally {
      setBusy("");
    }
  }

  async function createGrant() {
    const hours = Number(grantHours);
    if (!grantUserID || !grantReference || !Number.isFinite(hours) || hours < 1 || hours > 24) return;
    const signature = JSON.stringify({ tenantID, userID: grantUserID, scope: grantScope, reference: grantReference, hours });
    if (!grantExpiry.current || grantExpiry.current.signature !== signature) {
      grantExpiry.current = { signature, expiresAt: new Date(Date.now() + hours * 60 * 60 * 1000).toISOString() };
    }
    const expiresAt = grantExpiry.current.expiresAt;
    const payload = { tenantID, userID: grantUserID, scope: grantScope, reference: grantReference, hours, expiresAt };
    const actionKey = mutationKey.current.forPayload("pii-grant", payload);
    setBusy("grant");
    resetNotices();
    try {
      const response = await grantPII(tenantID, grantUserID, grantScope, grantReference, expiresAt, actionKey);
      setGrants((items) => [response.data, ...items.filter((item) => item.id !== response.data.id)]);
      mutationKey.current.clear();
      grantExpiry.current = undefined;
      setGrantReference("");
      setSuccess(response.replayed ? "The exact temporary grant was recovered safely." : "Temporary sensitive-data access granted.");
    } catch (failure) {
      setError(errorMessage(failure, "Could not grant temporary sensitive-data access. Confirm the account has the required salon capability and the change reference contains only letters, numbers, '.', '_', ':', '/', or '-'."));
    } finally {
      setBusy("");
    }
  }

  async function revokeGrant(grant: PIIGrant) {
    const actionKey = mutationKey.current.forPayload("pii-revoke", { id: grant.id, version: grant.version });
    setBusy(`grant-${grant.id}`);
    resetNotices();
    try {
      const response = await revokePII(tenantID, grant.id, grant.version, actionKey);
      setGrants((items) => items.map((item) => item.id === response.data.id ? response.data : item));
      mutationKey.current.clear();
      setSuccess(response.replayed ? "The exact revocation was recovered safely." : "Temporary access revoked immediately.");
    } catch (failure) {
      setError(errorMessage(failure, "Could not revoke temporary sensitive-data access."));
    } finally {
      setBusy("");
    }
  }

  async function sendSupportRequest() {
    const hours = Number(requestHours);
    const maxHours = requestCallsPII ? 24 : 720;
    if (!requestUserID || !requestReference || requestCapabilities.length === 0 || !Number.isFinite(hours) || hours < 1 || hours > maxHours) return;
    const expiresAt = new Date(Date.now() + hours * 60 * 60 * 1000).toISOString();
    const payload = { tenantID, userID: requestUserID, capabilities: requestCapabilities, callsPII: requestCallsPII, reference: requestReference, expiresAt };
    const actionKey = mutationKey.current.forPayload("support-ops-grant", payload);
    setBusy("support-request"); resetNotices();
    try {
      const response = await createSupportAccessRequest(tenantID, requestUserID, requestCapabilities, requestCallsPII ? ["calls"] : [], requestReference, expiresAt, actionKey);
      setSupportRequests((items) => [response.data, ...items.filter((item) => item.id !== response.data.id)]);
      mutationKey.current.clear(); setRequestReference("");
      setSuccess(response.replayed ? "The exact temporary Ops grant was recovered safely." : "Temporary Ops authorization granted.");
    } catch (failure) { setError(errorMessage(failure, "Could not grant temporary Ops authorization.")); }
    finally { setBusy(""); }
  }

  async function cancelSupportRequest(item: SupportAccessRequest) {
    const actionKey = mutationKey.current.forPayload("support-owner-cancel", { id: item.id, version: item.version });
    setBusy(`support-${item.id}`); resetNotices();
    try {
      const response = await cancelSupportAccessRequest(tenantID, item.id, item.version, actionKey);
      setSupportRequests((items) => items.map((current) => current.id === response.data.id ? response.data : current));
      mutationKey.current.clear(); setSuccess("Legacy pending request cancelled.");
    } catch (failure) { setError(errorMessage(failure, "Could not cancel the legacy pending request.")); }
    finally { setBusy(""); }
  }

  async function revokeSupportRequest(item: SupportAccessRequest) {
    const actionKey = mutationKey.current.forPayload("support-ops-revoke", { id: item.id, version: item.version });
    setBusy(`support-${item.id}`); resetNotices();
    try {
      const response = await revokeSupportAccessRequest(tenantID, item.id, item.version, actionKey);
      setSupportRequests((items) => items.map((current) => current.id === response.data.id ? response.data : current));
      mutationKey.current.clear(); setSuccess("Temporary Ops authorization revoked.");
    } catch (failure) { setError(errorMessage(failure, "Could not revoke temporary Ops authorization.")); }
    finally { setBusy(""); }
  }

  function resetNotices() {
    setError("");
    setSuccess("");
  }

  if (loading) {
    return <div className="space-y-4"><Skeleton className="h-12 w-72" /><Skeleton className="h-80 w-full" /><Skeleton className="h-80 w-full" /></div>;
  }

  if (adminBlocked) {
    return (
      <Card className="border-amber-200 bg-amber-50">
        <div className="flex gap-3">
          <ShieldAlert className="h-5 w-5 flex-none text-amber-700" />
          <div><CardTitle>Platform Administrator only</CardTitle><CardDescription>Salon access governance requires the global platform.access.manage capability.</CardDescription></div>
        </div>
      </Card>
    );
  }

  const selectedAssignment = assignments.find((item) => item.user_id === assignmentUserID);
  const grantDuration = Number(grantHours);
  const grantDurationValid = Number.isFinite(grantDuration) && grantDuration >= 1 && grantDuration <= 24;

  return (
    <div className="space-y-5">
      <div className="flex flex-col gap-3 sm:flex-row sm:items-end sm:justify-between">
        <div>
          <h2 className="text-xl font-bold text-ink">Access</h2>
          <p className="mt-1 max-w-3xl text-sm leading-6 text-muted">Manage the salon team, Platform support capabilities, and exceptional access to sensitive customer data for this salon.</p>
        </div>
        <Button type="button" variant="secondary" onClick={() => void load()}><RefreshCcw className="h-4 w-4" />Refresh</Button>
      </div>

      {error ? <Alert title="Access action needs attention" message={error} /> : null}
      {success ? <Alert type="success" title="Saved" message={success} /> : null}

      <nav className="grid gap-2 sm:grid-cols-2 xl:grid-cols-4" aria-label="Access workflows">
        {([
          ["team", "Salon team"],
          ["support", "Platform support"],
          ["temporary", "Temporary Ops"],
          ["sensitive", "Sensitive data"]
        ] as Array<[AccessSection, string]>).map(([key, label]) => (
          <button key={key} type="button" onClick={() => setSection(key)} className={`rounded-lg border p-3 text-left text-sm font-semibold shadow-soft ${section === key ? "border-teal-300 bg-teal-50 text-brand" : "border-line bg-white text-slate-700 hover:border-teal-200"}`}>{label}</button>
        ))}
      </nav>

      {section === "temporary" ? <Card>
        <div className="flex items-start gap-3">
          <ShieldAlert className="mt-0.5 h-5 w-5 flex-none text-brand" />
          <div><CardTitle>Temporary Ops authorization</CardTitle><CardDescription>Platform Admin can grant an assigned Ops account time-bounded Services, AI Training, or Calls access. Platform Admin access itself is direct and never depends on this grant.</CardDescription></div>
        </div>
        <div className="mt-5 space-y-4">
          <AccessUserSelect users={assignedOpsUsers} value={requestUserID} onChange={(value) => { setRequestUserID(value); mutationKey.current.clear(); }} label="Platform Ops account" emptyLabel="No assigned active Platform Ops accounts" />
          <fieldset><legend className="text-sm font-semibold text-ink">Temporary capabilities</legend><div className="mt-2 grid gap-2 sm:grid-cols-2 xl:grid-cols-3">{supportCapabilities.map((capability) => <label key={capability.name} className="flex min-h-11 items-start gap-3 rounded-md border border-line bg-white p-3 text-sm text-slate-700"><input className="mt-0.5" type="checkbox" checked={requestCapabilities.includes(capability.name)} onChange={(event) => { const next = applyCapabilitySelection(requestCapabilities, capability.name, event.target.checked, supportCapabilities); setRequestCapabilities(next); if (next.some((item) => item.startsWith("calls."))) { setRequestCallsPII(true); setRequestHours((value) => String(Math.min(Number(value) || 24, 24))); } else if (!next.some((item) => item.startsWith("training."))) setRequestCallsPII(false); mutationKey.current.clear(); }} /><span><span className="font-medium text-ink">{capability.display_name}</span>{capability.requires.length ? <span className="mt-1 block text-xs text-muted">Includes {capability.requires.map((name) => capabilityLabel(name, capabilities)).join(", ")}</span> : null}</span></label>)}</div></fieldset>
          <label className="flex items-start gap-3 rounded-md border border-amber-200 bg-amber-50 p-3 text-sm text-amber-950"><input className="mt-0.5" type="checkbox" checked={requestCallsPII} disabled={callsRequestSelected || !requestCapabilities.some((item) => item.startsWith("training."))} onChange={(event) => { setRequestCallsPII(event.target.checked); if (event.target.checked) setRequestHours((value) => String(Math.min(Number(value) || 24, 24))); mutationKey.current.clear(); }} /><span><span className="font-semibold">Include Calls PII</span><span className="mt-1 block text-xs">Required for every Calls capability and for transcript-linked Training corrections. Maximum 24 hours.</span></span></label>
          <div className="grid gap-4 md:grid-cols-[minmax(0,1fr)_180px_auto] md:items-end"><label className="block space-y-2"><span className="block text-sm font-semibold text-ink">Change reference</span><input className="field" value={requestReference} onChange={(event) => { setRequestReference(event.target.value); mutationKey.current.clear(); }} placeholder="SUPPORT-2048" maxLength={128} /></label><label className="block space-y-2"><span className="block text-sm font-semibold text-ink">Duration (hours)</span><input className="field" type="number" min="1" max={requestCallsPII ? 24 : 720} value={requestHours} onChange={(event) => { setRequestHours(event.target.value); mutationKey.current.clear(); }} /></label><Button type="button" disabled={!requestUserID || !requestReference || requestCapabilities.length === 0 || busy === "support-request"} onClick={() => void sendSupportRequest()}>Grant temporarily</Button></div>
        </div>
        <AccessRows empty="No temporary Ops authorizations for this salon." items={supportRequests.map((item) => ({id:item.id,user:item.user,badges:[item.effective_status,...item.pii_scopes.map((scope)=>`${scope} PII`)],detail:<>{item.capabilities.map((name)=>capabilityLabel(name,capabilities)).join(", ")} · expires {new Date(item.approved_expires_at || item.requested_expires_at).toLocaleString()} · {item.reason}</>,action:item.status === "pending_owner_review" ? <Button type="button" variant="secondary" disabled={busy === `support-${item.id}`} onClick={() => void cancelSupportRequest(item)}>Cancel legacy request</Button> : item.status === "approved" ? <Button type="button" variant="secondary" disabled={busy === `support-${item.id}`} onClick={() => void revokeSupportRequest(item)}>Revoke</Button> : undefined}))} />
      </Card> : null}

      {section === "team" ? <Card>
        <div className="flex items-start gap-3">
          <Users className="mt-0.5 h-5 w-5 flex-none text-brand" />
          <div><CardTitle>Salon team</CardTitle><CardDescription>Add or remove Tenant Business Managers. Platform Admin can also suspend or reactivate the Owner’s Tenant workspace access without changing the salon ownership record.</CardDescription></div>
        </div>
        <div className="mt-5 grid gap-3 lg:grid-cols-[minmax(0,1fr)_auto] lg:items-end">
          <AccessUserSearch
            value={managerUserID}
            onChange={(userID) => { setManagerUserID(userID); mutationKey.current.clear(); }}
            scope="tenant"
            tenantID={tenantID}
            excludeUserIDs={memberships.filter((item) => item.status === "active").map((item) => item.user_id)}
            label="Add a Business Manager"
            help="Only Tenant identities are eligible for salon membership. Platform identities never appear here."
          />
          <Button className="w-full lg:w-auto" type="button" disabled={!managerUserID || busy === "membership"} onClick={() => void saveManager()}>
            {memberships.some((item) => item.user_id === managerUserID) ? "Reactivate manager" : "Add manager"}
          </Button>
        </div>
        <AccessRows
          empty="No salon memberships are available."
          items={memberships.map((item) => ({
            id: item.id,
            user: item.user,
            badges: [item.role, item.status],
            detail: item.is_owner ? `Salon Owner · ownership record preserved · version ${item.version}` : `Version ${item.version}`,
            action: <Button type="button" variant="secondary" disabled={busy === `membership-${item.user_id}`} onClick={() => void toggleMembership(item)}>{item.status === "active" ? "Revoke" : "Reactivate"}</Button>
          }))}
        />
      </Card> : null}

      {section === "support" ? <Card>
        <div className="flex items-start gap-3">
          <UserCog className="mt-0.5 h-5 w-5 flex-none text-brand" />
          <div><CardTitle>Platform support access</CardTitle><CardDescription>Choose exactly what an active Platform Ops account can do for this salon. This does not grant access to sensitive customer data.</CardDescription></div>
        </div>
        <div className="mt-5 space-y-4">
          <AccessUserSelect users={activeOpsUsers} value={assignmentUserID} onChange={chooseOpsUser} label="Platform Ops account" emptyLabel="No active Platform Ops accounts" />
          {delegable.length ? (
            <fieldset>
              <legend className="text-sm font-semibold text-ink">Salon capabilities</legend>
              <div className="mt-2 grid gap-2 sm:grid-cols-2 xl:grid-cols-3">
                {delegable.map((capability) => (
                  <label key={capability.name} className="flex min-h-11 items-start gap-3 rounded-md border border-line bg-white p-3 text-sm text-slate-700">
                    <input
                      className="mt-0.5"
                      type="checkbox"
                      checked={assignmentPermissions.includes(capability.name)}
                      onChange={(event) => {
                        setAssignmentPermissions((selected) => applyCapabilitySelection(selected, capability.name, event.target.checked, delegable));
                        mutationKey.current.clear();
                      }}
                    />
                    <span><span className="font-medium text-ink">{capability.display_name}</span>{capability.requires.length ? <span className="mt-1 block text-xs text-muted">Includes {capability.requires.map((name) => capabilityLabel(name, capabilities)).join(", ")}</span> : null}</span>
                  </label>
                ))}
              </div>
            </fieldset>
          ) : <Alert type="warning" title="No delegable capabilities" message="The backend did not publish any salon-delegable capabilities, so support access cannot be assigned." />}
          <Button className="w-full sm:w-auto" type="button" disabled={!assignmentUserID || assignmentPermissions.length === 0 || busy === "assignment"} onClick={() => void saveAssignment()}>
            {selectedAssignment?.status === "revoked" ? "Reactivate support access" : selectedAssignment ? "Update support access" : "Assign support access"}
          </Button>
        </div>
        <AccessRows
          empty="No Platform support access has been assigned to this salon."
          items={assignments.map((item) => ({
            id: item.id,
            user: item.user,
            badges: [item.status],
            detail: item.permissions.map((permission) => capabilityLabel(permission, capabilities)).join(", ") || "No capabilities",
            action: <Button type="button" variant="secondary" disabled={busy === `assignment-${item.user_id}`} onClick={() => void toggleAssignment(item)}>{item.status === "active" ? "Revoke" : "Reactivate"}</Button>
          }))}
        />
      </Card> : null}

      {section === "sensitive" ? <Card>
        <div className="flex items-start gap-3">
          <Clock3 className="mt-0.5 h-5 w-5 flex-none text-amber-700" />
          <div>
            <CardTitle>Temporary sensitive data access</CardTitle>
            <CardDescription>Exceptional access for an assigned Platform Ops account to customer, appointment, and notification records. Calls PII is granted only with the temporary Calls authorization above.</CardDescription>
          </div>
        </div>
        <div className="mt-4"><Alert type="warning" title="Exceptional Ops access" message="Platform Admin has direct control-plane access. Ops grants are salon-specific, expire automatically, can be revoked early, and are recorded in the audit history." /></div>
        <div className="mt-5 grid gap-4 md:grid-cols-2 xl:grid-cols-[minmax(0,1fr)_220px_minmax(0,1fr)_130px_auto] xl:items-end">
          <AccessUserSelect users={assignedOpsUsers} value={grantUserID} onChange={(userID) => { setGrantUserID(userID); mutationKey.current.clear(); grantExpiry.current = undefined; }} label="Platform Ops account" emptyLabel="No assigned active Platform Ops accounts" />
          <label className="block space-y-2"><span className="block text-sm font-semibold text-ink">Data scope</span><select className="field" value={grantScope} onChange={(event) => { setGrantScope(event.target.value as PIIScope); mutationKey.current.clear(); grantExpiry.current = undefined; }}>{piiScopes.map((scope) => <option key={scope.value} value={scope.value}>{scope.label}</option>)}</select></label>
          <label className="block space-y-2"><span className="block text-sm font-semibold text-ink">Approved change reference</span><input className="field" value={grantReference} onChange={(event) => { setGrantReference(event.target.value); mutationKey.current.clear(); grantExpiry.current = undefined; }} placeholder="Example: INC-2048" maxLength={128} /><span className="block text-xs text-muted">Use an opaque ticket or change ID; do not enter customer data.</span></label>
          <label className="block space-y-2"><span className="block text-sm font-semibold text-ink">Duration (hours)</span><input className="field" type="number" min="1" max="24" step="1" value={grantHours} onChange={(event) => { setGrantHours(event.target.value); mutationKey.current.clear(); grantExpiry.current = undefined; }} /></label>
          <Button className="w-full xl:w-auto" type="button" disabled={!grantUserID || !grantReference || !grantDurationValid || busy === "grant"} onClick={() => void createGrant()}>Grant temporarily</Button>
        </div>
        <AccessRows
          empty="No temporary sensitive-data grants for this salon."
          items={grants.map((grant) => {
            const active = !grant.revoked_at && new Date(grant.expires_at).getTime() > Date.now();
            const status = grant.revoked_at ? "revoked" : active ? "active" : "expired";
            return {
              id: grant.id,
              user: grant.user,
              badges: [piiScopes.find((scope) => scope.value === grant.scope)?.label ?? grant.scope, status],
              detail: <>Expires {new Date(grant.expires_at).toLocaleString()} · Reference {grant.reason}</>,
              action: active ? <Button type="button" variant="secondary" disabled={busy === `grant-${grant.id}`} onClick={() => void revokeGrant(grant)}>Revoke now</Button> : undefined
            };
          })}
        />
      </Card> : null}
    </div>
  );
}

function upsertByUser<T extends { user_id: string; user: AccessUser }>(items: T[], item: T) {
  return [...items.filter((current) => current.user_id !== item.user_id), item]
    .sort((left, right) => left.user.full_name.localeCompare(right.user.full_name));
}
