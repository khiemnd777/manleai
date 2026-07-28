"use client";

import { useEffect, useRef, useState } from "react";
import { Pencil, RefreshCcw, ShieldAlert, UserCog, UserPlus } from "lucide-react";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Card, CardDescription, CardTitle } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { AccessRows, errorMessage } from "@/features/platform/access-ui";
import { createPlatformUser, listPlatformRoles, mutatePlatformRole, type PlatformRoleAssignment, updatePlatformUser } from "@/lib/api/access";
import { BusinessMutationKeyManager } from "@/lib/api/business";
import { RequestError } from "@/lib/api/client";

export function PlatformAccessConsole() {
  const [roles, setRoles] = useState<PlatformRoleAssignment[]>([]);
  const [editingUserID, setEditingUserID] = useState("");
  const [email, setEmail] = useState("");
  const [fullName, setFullName] = useState("");
  const [password, setPassword] = useState("");
  const [role, setRole] = useState<PlatformRoleAssignment["role"]>("platform_ops");
  const [status, setStatus] = useState<PlatformRoleAssignment["status"]>("active");
  const [loading, setLoading] = useState(true);
  const [adminBlocked, setAdminBlocked] = useState(false);
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");
  const [success, setSuccess] = useState("");
  const mutationKey = useRef(new BusinessMutationKeyManager());

  async function load() {
    setLoading(true);
    setError("");
    setSuccess("");
    setAdminBlocked(false);
    try {
      const result = await listPlatformRoles();
      setRoles(result.assignments);
    } catch (failure) {
      if (failure instanceof RequestError && failure.status === 403) setAdminBlocked(true);
      else setError(errorMessage(failure, "Could not load Platform roles."));
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    void load();
  }, []);

  function editUser(item?: PlatformRoleAssignment) {
    setEditingUserID(item?.user_id ?? "");
    setEmail(item?.user.email ?? "");
    setFullName(item?.user.full_name ?? "");
    setPassword("");
    setRole(item?.role ?? "platform_ops");
    setStatus(item?.status ?? "active");
    mutationKey.current.clear();
  }

  async function saveUser() {
    const existing = roles.find((assignment) => assignment.user_id === editingUserID);
    if (!email.trim() || !fullName.trim() || (!existing && password.length < 8)) return;
    const payload = { userID: editingUserID, email: email.trim(), fullName: fullName.trim(), password, role, status, version: existing?.version ?? 0 };
    const actionKey = mutationKey.current.forPayload(existing ? "platform-user-update" : "platform-user-create", payload);
    setBusy("save-user");
    setError("");
    setSuccess("");
    try {
      const response = existing
        ? await updatePlatformUser(existing.user_id, payload.email, payload.fullName, password, role, status, existing.version, actionKey)
        : await createPlatformUser(payload.email, payload.fullName, password, role, status, actionKey);
      setRoles((items) => upsertRole(items, response.data));
      mutationKey.current.clear();
      editUser();
      setSuccess(response.replayed ? "The exact Platform user change was recovered safely." : existing ? "Platform user updated." : "Platform user created.");
    } catch (failure) {
      setError(errorMessage(failure, "Could not save the Platform user."));
    } finally {
      setBusy("");
    }
  }

  async function toggleRole(item: PlatformRoleAssignment) {
    const status = item.status === "active" ? "revoked" : "active";
    const actionKey = mutationKey.current.forPayload("platform-role-status", { id: item.id, status, version: item.version });
    setBusy(`role-${item.user_id}`);
    setError("");
    setSuccess("");
    try {
      const response = await mutatePlatformRole(item.user_id, item.role, status, item.version, actionKey);
      setRoles((items) => upsertRole(items, response.data));
      mutationKey.current.clear();
      setSuccess(response.replayed ? "The exact Platform role change was recovered safely." : "Platform role status saved.");
    } catch (failure) {
      setError(errorMessage(failure, "Could not change the Platform role status."));
    } finally {
      setBusy("");
    }
  }

  if (loading) {
    return <div className="space-y-4"><Skeleton className="h-12 w-72" /><Skeleton className="h-72 w-full" /></div>;
  }

  if (adminBlocked) {
    return (
      <Card className="border-amber-200 bg-amber-50">
        <div className="flex gap-3">
          <ShieldAlert className="h-5 w-5 flex-none text-amber-700" />
          <div><CardTitle>Platform Administrator only</CardTitle><CardDescription>Only a Platform Administrator can manage global Platform roles.</CardDescription></div>
        </div>
      </Card>
    );
  }

  const existing = roles.find((assignment) => assignment.user_id === editingUserID);

  return (
    <div className="space-y-5">
      <div className="flex flex-col gap-3 sm:flex-row sm:items-end sm:justify-between">
        <div>
          <h1 className="text-2xl font-bold text-ink">Platform roles</h1>
          <p className="mt-1 max-w-3xl text-sm leading-6 text-muted">Create and maintain dedicated Platform identities. Salon-specific Ops access is managed from each nail salon’s Access tab.</p>
        </div>
        <div className="flex gap-2"><Button type="button" variant="secondary" onClick={() => editUser()}><UserPlus className="h-4 w-4" />Create Platform user</Button><Button type="button" variant="secondary" onClick={() => void load()}><RefreshCcw className="h-4 w-4" />Refresh</Button></div>
      </div>

      {error ? <Alert title="Access action needs attention" message={error} /> : null}
      {success ? <Alert type="success" title="Saved" message={success} /> : null}

      <Card>
        <div className="flex items-start gap-3">
          {existing ? <Pencil className="mt-0.5 h-5 w-5 flex-none text-brand" /> : <UserPlus className="mt-0.5 h-5 w-5 flex-none text-brand" />}
          <div>
            <CardTitle>{existing ? "Edit Platform user" : "Create Platform user"}</CardTitle>
            <CardDescription>Platform identities are separate from Tenant salon accounts. Password changes revoke existing refresh sessions.</CardDescription>
          </div>
        </div>
        <div className="mt-5 grid gap-4 sm:grid-cols-2">
          <label className="block space-y-2"><span className="block text-sm font-semibold text-ink">Full name</span><input className="field" value={fullName} onChange={(event) => { setFullName(event.target.value); mutationKey.current.clear(); }} /></label>
          <label className="block space-y-2"><span className="block text-sm font-semibold text-ink">Email</span><input className="field" type="email" value={email} onChange={(event) => { setEmail(event.target.value); mutationKey.current.clear(); }} /></label>
          <label className="block space-y-2"><span className="block text-sm font-semibold text-ink">{existing ? "New password (optional)" : "Password"}</span><input className="field" type="password" minLength={8} value={password} onChange={(event) => { setPassword(event.target.value); mutationKey.current.clear(); }} /><span className="block text-xs text-muted">Minimum 8 characters. Passwords are never displayed after saving.</span></label>
          <label className="block space-y-2">
            <span className="block text-sm font-semibold text-ink">Platform role</span>
            <select className="field" value={role} onChange={(event) => { setRole(event.target.value as PlatformRoleAssignment["role"]); mutationKey.current.clear(); }}>
              <option value="platform_ops">Platform Ops</option>
              <option value="platform_admin">Platform Admin</option>
            </select>
            <span className="block text-xs leading-5 text-muted">Platform Ops receives salon capabilities separately.</span>
          </label>
          {existing ? <label className="block space-y-2"><span className="block text-sm font-semibold text-ink">Status</span><select className="field" value={status} onChange={(event) => { setStatus(event.target.value as PlatformRoleAssignment["status"]); mutationKey.current.clear(); }}><option value="active">Active</option><option value="revoked">Revoked</option></select></label> : null}
        </div>
        <div className="mt-5 flex gap-2"><Button type="button" disabled={!email.trim() || !fullName.trim() || (!existing && password.length < 8) || busy === "save-user"} onClick={() => void saveUser()}><UserCog className="h-4 w-4" />{existing ? "Save changes" : "Create user"}</Button>{existing ? <Button type="button" variant="secondary" onClick={() => editUser()}>Cancel edit</Button> : null}</div>
      </Card>

      <Card>
        <CardTitle>Current Platform roles</CardTitle>
        <CardDescription>The last active Platform Administrator cannot be removed. Changing a Platform role revokes stale salon assignments and temporary sensitive-data grants.</CardDescription>
        <AccessRows
          empty="No Platform role assignments."
          items={roles.map((item) => ({
            id: item.id,
            user: item.user,
            badges: [item.role, item.status],
            detail: `Version ${item.version}`,
            action: <div className="flex gap-2"><Button type="button" variant="secondary" onClick={() => editUser(item)}><Pencil className="h-4 w-4" />Edit</Button><Button type="button" variant="secondary" disabled={busy === `role-${item.user_id}`} onClick={() => void toggleRole(item)}>{item.status === "active" ? "Revoke" : "Reactivate"}</Button></div>
          }))}
        />
      </Card>
    </div>
  );
}

function upsertRole(items: PlatformRoleAssignment[], item: PlatformRoleAssignment) {
  return [...items.filter((current) => current.user_id !== item.user_id), item]
    .sort((left, right) => left.user.full_name.localeCompare(right.user.full_name));
}
