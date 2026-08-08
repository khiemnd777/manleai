"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { useSearchParams } from "next/navigation";
import { Archive, Pencil, Plus, RefreshCcw, Users } from "lucide-react";
import { Alert } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardDescription, CardTitle } from "@/components/ui/card";
import { Dialog } from "@/components/ui/dialog";
import { Skeleton } from "@/components/ui/skeleton";
import { StaffCalendarProfile } from "@/features/dashboard/staff-calendar-profile";
import { BusinessMutationKeyManager, businessGet, businessMutation, type BusinessService, type BusinessStaff, type BusinessSurface } from "@/lib/api/business";
import { getManleAICalendar } from "@/lib/api/internal-calendar";
import type { ManleAICalendarAggregate } from "@/types/api";

type StaffForm = { name: string; phone: string; email: string; active: boolean; aiBookable: boolean; serviceIDs: string[] };

export function BusinessStaffManager({ surface, title = "Staff" }: { surface: BusinessSurface; title?: string }) {
  const searchParams = useSearchParams();
  const requestedEditID = searchParams.get("edit") ?? "";
  const [staff, setStaff] = useState<BusinessStaff[]>([]);
  const [services, setServices] = useState<BusinessService[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [success, setSuccess] = useState("");
  const [busy, setBusy] = useState("");
  const [editing, setEditing] = useState<BusinessStaff | null>(null);
  const [form, setForm] = useState<StaffForm>(emptyForm());
  const [open, setOpen] = useState(false);
  const [calendar, setCalendar] = useState<ManleAICalendarAggregate | null>(null);
  const [calendarLoading, setCalendarLoading] = useState(surface.kind === "platform");
  const [calendarError, setCalendarError] = useState("");
  const profileAttempt = useRef(new BusinessMutationKeyManager());
  const eligibilityAttempt = useRef(new BusinessMutationKeyManager());
  const openedDeepLink = useRef("");

  const loadCalendar = useCallback(async () => {
    if (surface.kind !== "platform") return;
    setCalendarLoading(true);
    setCalendarError("");
    try {
      const response = await getManleAICalendar(surface.salonID, "platform");
      setCalendar(response.manleai_calendar);
    } catch (failure) {
      setCalendarError(message(failure, "Could not load the staff calendar profile."));
    } finally {
      setCalendarLoading(false);
    }
  }, [surface.kind, surface.salonID]);

  async function load({ silent = false }: { silent?: boolean } = {}) {
    if (!silent) setLoading(true); setError("");
    try {
      const [staffResponse, serviceResponse] = await Promise.all([
        businessGet<{ staff: BusinessStaff[] }>(surface, "staff"),
        businessGet<{ services: BusinessService[] }>(surface, "services")
      ]);
      setStaff(staffResponse.staff); setServices(serviceResponse.services);
      if (editing) {
        const current = staffResponse.staff.find((item) => item.id === editing.id);
        if (current) { setEditing(current); setForm(staffToForm(current)); }
      }
    } catch (failure) { setError(message(failure, "Could not load staff.")); }
    finally { if (!silent) setLoading(false); }
  }
  useEffect(() => { void load(); }, [surface.kind, surface.salonID]);
  useEffect(() => { void loadCalendar(); }, [loadCalendar]);
  useEffect(() => {
    if (loading || !requestedEditID || openedDeepLink.current === requestedEditID) return;
    const requested = staff.find((item) => item.id === requestedEditID);
    if (!requested) return;
    openedDeepLink.current = requestedEditID;
    openEdit(requested);
  }, [loading, requestedEditID, staff]);

  function openCreate() { setEditing(null); setForm(emptyForm()); profileAttempt.current.clear(); eligibilityAttempt.current.clear(); setOpen(true); setError(""); setSuccess(""); }
  function openEdit(item: BusinessStaff) { setEditing(item); setForm(staffToForm(item)); profileAttempt.current.clear(); eligibilityAttempt.current.clear(); setOpen(true); setError(""); setSuccess(""); }
  function update(next: StaffForm) { setForm(next); profileAttempt.current.clear(); eligibilityAttempt.current.clear(); }

  async function saveProfile() {
    if (!form.name.trim()) { setError("Staff name is required."); return; }
    const expectedVersion = editing?.version ?? 0;
    const operational = { name: form.name.trim(), active: form.active, ...(surface.kind === "tenant" ? { phone: form.phone.trim(), email: form.email.trim().toLowerCase() } : {}) };
    const payload = editing?.management_mode === "provider_read_only" ? { ai_bookable: form.aiBookable } : { ...operational, ai_bookable: form.aiBookable };
    const key = profileAttempt.current.forPayload(editing ? "staff-update" : "staff-create", { expectedVersion, payload });
    setBusy("profile"); setError(""); setSuccess("");
    try {
      const response = await businessMutation<BusinessStaff>(surface, editing ? `staff/${editing.id}` : "staff", editing ? "PATCH" : "POST", { action_key: key, expected_version: expectedVersion, ...payload });
      setStaff((items) => upsert(items, response.data)); setEditing(response.data); setForm((current) => ({ ...staffToForm(response.data), serviceIDs: current.serviceIDs })); profileAttempt.current.clear();
      setSuccess(response.replayed ? "The exact staff change was recovered safely." : editing ? "Staff profile saved." : "Staff member created. Assign services before closing if needed.");
      await loadCalendar();
    } catch (failure) { setError(message(failure, "Could not save the staff profile. Retry keeps the same action key while fields are unchanged.")); }
    finally { setBusy(""); }
  }

  async function saveEligibility() {
    if (!editing) { setError("Save the staff profile before assigning services."); return; }
    const payload = { staff_id: editing.id, service_ids: [...form.serviceIDs].sort(), expectedVersion: editing.eligibility_version };
    const key = eligibilityAttempt.current.forPayload("staff-services", payload);
    setBusy("eligibility"); setError(""); setSuccess("");
    try {
      const response = await businessMutation<BusinessStaff>(surface, `staff/${editing.id}/services`, "PUT", { action_key: key, expected_version: editing.eligibility_version, staff_id: editing.id, service_ids: payload.service_ids });
      eligibilityAttempt.current.clear(); setSuccess(response.replayed ? "The exact service assignment was recovered safely." : "Staff service assignments saved.");
      await Promise.all([load({ silent: true }), loadCalendar()]);
    } catch (failure) { setError(message(failure, "Could not save service assignments. Reload if another staff assignment changed.")); }
    finally { setBusy(""); }
  }

  async function archive(item: BusinessStaff) {
    if (item.archived_at || item.management_mode === "provider_read_only" || !window.confirm(`Archive ${item.name}? Historical appointments keep their staff snapshot.`)) return;
    const key = new BusinessMutationKeyManager().forPayload("staff-archive", { id:item.id,version:item.version });
    setBusy(`archive-${item.id}`); setError("");
    try { const response=await businessMutation<BusinessStaff>(surface,`staff/${item.id}/archive`,"POST",{action_key:key,expected_version:item.version});setStaff((items)=>upsert(items,response.data));setSuccess("Staff member archived.");await loadCalendar(); }
    catch (failure) { setError(message(failure,"Could not archive the staff member.")); }
    finally { setBusy(""); }
  }

  return <div className="space-y-5">
    <div className="flex flex-col gap-3 sm:flex-row sm:items-end sm:justify-between"><div><h1 className="text-2xl font-bold text-ink">{title}</h1><p className="mt-1 text-sm text-muted">Manage staff and each staff member&apos;s eligible services from the same canonical records.</p></div><div className="flex gap-2"><Button type="button" variant="secondary" onClick={() => void load()} disabled={loading}><RefreshCcw className="h-4 w-4" />Refresh</Button><Button type="button" onClick={openCreate}><Plus className="h-4 w-4" />Add staff</Button></div></div>
    {error ? <Alert title="Staff action needs attention" message={error} /> : null}{success ? <Alert type="success" title="Saved" message={success} /> : null}
    {loading ? <div className="space-y-3">{[0,1,2].map((item)=><Skeleton key={item} className="h-24 w-full" />)}</div> : null}
    {!loading && staff.length===0 ? <Card className="py-10 text-center"><Users className="mx-auto h-7 w-7 text-muted" /><CardTitle className="mt-3">No staff yet</CardTitle><CardDescription>Add the first real staff member, then assign services in that staff member&apos;s edit flow.</CardDescription><Button type="button" className="mt-4" onClick={openCreate}>Add staff</Button></Card> : null}
    {!loading && staff.length ? <div className="overflow-hidden rounded-lg border border-line bg-white shadow-soft">{staff.map((item)=><div key={item.id} className="grid gap-4 border-b border-line px-5 py-4 last:border-0 md:grid-cols-[minmax(0,2fr)_minmax(180px,1fr)_auto] md:items-center"><div><div className="flex flex-wrap items-center gap-2"><span className="font-semibold text-ink">{item.name}</span><Badge value={item.archived_at?"archived":item.active?"active":"disabled"}/><Badge value={item.management_mode}/></div>{surface.kind==="tenant" ? <p className="mt-1 text-sm text-muted">{item.phone || item.email || "No contact details"}</p> : <p className="mt-1 text-sm text-muted">Contact details are excluded from Platform Business UI.</p>}</div><div className="text-sm text-slate-700">{item.service_ids.length} eligible service{item.service_ids.length===1?"":"s"}<div className="mt-1 text-xs text-muted">AI booking {item.ai_bookable?"enabled":"disabled"}</div></div><div className="flex gap-2"><Button type="button" variant="secondary" onClick={()=>openEdit(item)}><Pencil className="h-4 w-4"/>Edit</Button><Button type="button" variant="ghost" disabled={Boolean(item.archived_at)||item.management_mode==="provider_read_only"||busy===`archive-${item.id}`} onClick={()=>void archive(item)}><Archive className="h-4 w-4"/><span className="sr-only">Archive</span></Button></div></div>)}</div> : null}
    <Dialog className={surface.kind==="platform"&&editing?"max-w-6xl":undefined} open={open} title={editing?`Edit ${editing.name}`:"Add staff"} description={editing?.management_mode==="provider_read_only"?"Name and active status come from the connected POS. AI eligibility and service assignments remain business-owned.":surface.kind==="platform"?"Platform Business UI intentionally excludes staff contact fields.":"Contact fields stay inside this tenant's Business workspace."} onClose={()=>{if(!busy)setOpen(false);}} closeDisabled={Boolean(busy)} footer={<div className="flex flex-col gap-2 sm:flex-row sm:justify-end"><Button type="button" variant="secondary" disabled={Boolean(busy)} onClick={()=>setOpen(false)}>Close</Button>{editing?<Button type="button" variant="secondary" disabled={Boolean(busy)} onClick={()=>void saveEligibility()}>{busy==="eligibility"?"Saving assignments…":"Save service assignments"}</Button>:null}<Button type="button" disabled={Boolean(busy)} onClick={()=>void saveProfile()}>{busy==="profile"?"Saving profile…":"Save profile"}</Button></div>}>
      <div className="grid gap-4 sm:grid-cols-2"><Label text="Staff name"><input className="field" value={form.name} disabled={editing?.management_mode==="provider_read_only"} onChange={(event)=>update({...form,name:event.target.value})}/></Label>{surface.kind==="tenant"?<><Label text="Phone"><input className="field" value={form.phone} disabled={editing?.management_mode==="provider_read_only"} onChange={(event)=>update({...form,phone:event.target.value})}/></Label><Label text="Email"><input className="field" type="email" value={form.email} disabled={editing?.management_mode==="provider_read_only"} onChange={(event)=>update({...form,email:event.target.value})}/></Label></>:null}<label className="flex items-center gap-2 text-sm font-medium text-ink"><input type="checkbox" checked={form.aiBookable} onChange={(event)=>update({...form,aiBookable:event.target.checked})}/>AI-bookable</label><label className="flex items-center gap-2 text-sm font-medium text-ink"><input type="checkbox" checked={form.active} disabled={editing?.management_mode==="provider_read_only"} onChange={(event)=>update({...form,active:event.target.checked})}/>Active</label><div className="sm:col-span-2"><div className="text-sm font-semibold text-ink">Eligible services</div><div className="mt-2 grid gap-2 rounded-md border border-line p-3 sm:grid-cols-2">{services.filter((item)=>!item.archived_at&&item.active).map((service)=><label key={service.id} className="flex items-center gap-2 text-sm text-slate-700"><input type="checkbox" checked={form.serviceIDs.includes(service.id)} onChange={(event)=>update({...form,serviceIDs:event.target.checked?[...form.serviceIDs,service.id]:form.serviceIDs.filter((id)=>id!==service.id)})}/>{service.name}</label>)}{!services.length?<span className="text-sm text-muted">Add services before assigning eligibility.</span>:null}</div></div>{surface.kind==="platform"?<p className="sm:col-span-2 text-xs leading-5 text-muted">The immutable audit records the signed-in Platform actor, never a simulated tenant owner.</p>:null}</div>
      {surface.kind==="platform"&&editing?<StaffCalendarProfile salonID={surface.salonID} timezone={calendar?.timezone||"UTC"} member={editing} calendar={calendar} loading={calendarLoading} error={calendarError} surface="platform" manageServiceEligibility={false} canonicalEligibleServiceIDs={editing.service_ids} onReload={loadCalendar} onCalendarChange={setCalendar}/>:null}
    </Dialog>
  </div>;
}

function Label({text,children}:{text:string;children:React.ReactNode}){return <label className="block"><span className="text-sm font-semibold text-ink">{text}</span><div className="mt-2">{children}</div></label>}
function emptyForm():StaffForm{return{name:"",phone:"",email:"",active:true,aiBookable:true,serviceIDs:[]}}
function staffToForm(item:BusinessStaff):StaffForm{return{name:item.name,phone:item.phone??"",email:item.email??"",active:item.active,aiBookable:item.ai_bookable,serviceIDs:[...item.service_ids]}}
function upsert(items:BusinessStaff[],item:BusinessStaff){return[...items.filter((current)=>current.id!==item.id),item].sort((a,b)=>Number(Boolean(a.archived_at))-Number(Boolean(b.archived_at))||a.name.localeCompare(b.name))}
function message(error:unknown,fallback:string){return error instanceof Error&&error.message?error.message:fallback}
