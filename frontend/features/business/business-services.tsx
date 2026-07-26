"use client";

import { useEffect, useRef, useState } from "react";
import { Archive, Pencil, Plus, RefreshCcw, Tags } from "lucide-react";
import { Alert } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardDescription, CardTitle } from "@/components/ui/card";
import { Dialog } from "@/components/ui/dialog";
import { Skeleton } from "@/components/ui/skeleton";
import {
  BusinessMutationKeyManager,
  businessGet,
  businessMutation,
  type BusinessConsultationProfile,
  type BusinessService,
  type BusinessServiceCategory,
  type BusinessSurface
} from "@/lib/api/business";

type ServiceForm = {
  name: string; description: string; aiDescription: string; duration: string;
  price: string; priceDisplay: string; categoryID: string; active: boolean;
  aiBookable: boolean; consultationStatus: "draft" | "ready" | "disabled";
  outcomes: string; systems: string; maintenanceNote: string; ownerSummary: string;
  lengthCapabilities: string[]; priorityTags: string[]; finishOptions: string[];
};

export function BusinessServices({ surface, title = "Services" }: { surface: BusinessSurface; title?: string }) {
  const [services, setServices] = useState<BusinessService[]>([]);
  const [categories, setCategories] = useState<BusinessServiceCategory[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [success, setSuccess] = useState("");
  const [busy, setBusy] = useState("");
  const [editing, setEditing] = useState<BusinessService | null>(null);
  const [formOpen, setFormOpen] = useState(false);
  const [form, setForm] = useState<ServiceForm>(emptyServiceForm());
  const [categoryName, setCategoryName] = useState("");
  const serviceAttempt = useRef(new BusinessMutationKeyManager());
  const categoryAttempt = useRef(new BusinessMutationKeyManager());

  async function load() {
    setLoading(true); setError("");
    try {
      const [serviceResponse, categoryResponse] = await Promise.all([
        businessGet<{ services: BusinessService[] }>(surface, "services"),
        businessGet<{ categories: BusinessServiceCategory[] }>(surface, "service-categories")
      ]);
      setServices(serviceResponse.services);
      setCategories(categoryResponse.categories);
    } catch (failure) { setError(message(failure, "Could not load services.")); }
    finally { setLoading(false); }
  }
  useEffect(() => { void load(); }, [surface.kind, surface.salonID]);

  function openCreate() { setEditing(null); setForm(emptyServiceForm()); setFormOpen(true); setError(""); setSuccess(""); serviceAttempt.current.clear(); }
  function openEdit(item: BusinessService) { setEditing(item); setForm(serviceToForm(item)); setFormOpen(true); setError(""); setSuccess(""); serviceAttempt.current.clear(); }

  async function save() {
    const duration = Number(form.duration);
    const price = form.price.trim() === "" ? undefined : Number(form.price);
    if (!form.name.trim() || !Number.isInteger(duration) || duration <= 0 || price !== undefined && (!Number.isFinite(price) || price < 0)) {
      setError("Enter a service name, a positive whole-minute duration, and a valid non-negative price."); return;
    }
    const profile: BusinessConsultationProfile = {
      status: form.consultationStatus,
      recommended_outcomes: splitValues(form.outcomes),
      compatible_current_systems: splitValues(form.systems),
      length_capabilities: form.lengthCapabilities, priority_tags: form.priorityTags, finish_options: form.finishOptions,
      maintenance_note: form.maintenanceNote.trim(), owner_approved_summary: form.ownerSummary.trim()
    };
    const operational = {
      name: form.name.trim(), description: form.description.trim(), duration_minutes: duration,
      ...(price === undefined ? {} : { price_from: price }), price_display: form.priceDisplay.trim(),
      active: form.active
    };
    const ownerControls = {
      ai_description: form.aiDescription.trim(), ai_bookable: form.aiBookable,
      service_category_id: form.categoryID, consultation_profile: profile
    };
    const payload = editing?.management_mode === "provider_read_only" ? ownerControls : { ...operational, ...ownerControls };
    const expectedVersion = editing?.version ?? 0;
    const key = serviceAttempt.current.forPayload(editing ? "service-update" : "service-create", { expectedVersion, payload });
    setBusy("save"); setError(""); setSuccess("");
    try {
      const response = await businessMutation<BusinessService>(surface, editing ? `services/${editing.id}` : "services", editing ? "PATCH" : "POST", { action_key: key, expected_version: expectedVersion, ...payload });
      setServices((items) => upsert(items, response.data));
      setEditing(response.data); setForm(serviceToForm(response.data)); serviceAttempt.current.clear();
      setSuccess(response.replayed ? "The exact service change was recovered safely." : editing ? "Service saved." : "Service created.");
    } catch (failure) { setError(message(failure, "Could not save the service. Retry keeps the same action key while the form is unchanged.")); }
    finally { setBusy(""); }
  }

  async function archive(item: BusinessService) {
    if (item.archived_at || item.management_mode === "provider_read_only" || !window.confirm(`Archive ${item.name}? Historical appointments keep their service snapshot.`)) return;
    const attempt = new BusinessMutationKeyManager();
    const key = attempt.forPayload("service-archive", { id: item.id, version: item.version });
    setBusy(`archive-${item.id}`); setError("");
    try {
      const response = await businessMutation<BusinessService>(surface, `services/${item.id}/archive`, "POST", { action_key: key, expected_version: item.version });
      setServices((items) => upsert(items, response.data)); setSuccess("Service archived.");
    } catch (failure) { setError(message(failure, "Could not archive the service.")); }
    finally { setBusy(""); }
  }

  async function createCategory() {
    const name = categoryName.trim(); if (!name) return;
    const slug = slugify(name); const expectedVersion = 0;
    const key = categoryAttempt.current.forPayload("category-create", { name, slug, expectedVersion });
    setBusy("category"); setError("");
    try {
      const response = await businessMutation<BusinessServiceCategory>(surface, "service-categories", "POST", { action_key: key, expected_version: expectedVersion, name, slug, description: "", sort_order: categories.length });
      setCategories((items) => [...items.filter((item) => item.id !== response.data.id), response.data].sort((a,b) => a.sort_order-b.sort_order || a.name.localeCompare(b.name)));
      setCategoryName(""); categoryAttempt.current.clear(); setSuccess(response.replayed ? "The exact category change was recovered safely." : "Category created.");
    } catch (failure) { setError(message(failure, "Could not create the category.")); }
    finally { setBusy(""); }
  }

  return <div className="space-y-5">
    <div className="flex flex-col gap-3 sm:flex-row sm:items-end sm:justify-between"><div><h1 className="text-2xl font-bold text-ink">{title}</h1><p className="mt-1 text-sm text-muted">Manage the canonical service catalog used by the salon and AI receptionist.</p></div><div className="flex gap-2"><Button type="button" variant="secondary" onClick={() => void load()} disabled={loading}><RefreshCcw className="h-4 w-4" />Refresh</Button><Button type="button" onClick={openCreate}><Plus className="h-4 w-4" />Add service</Button></div></div>
    {error ? <Alert title="Service action needs attention" message={error} /> : null}{success ? <Alert title="Saved" message={success} type="success" /> : null}
    <Card><CardTitle>Categories</CardTitle><CardDescription>Categories are shared by Tenant and Platform Business workflows.</CardDescription><div className="mt-4 flex flex-col gap-2 sm:flex-row"><input className="h-10 flex-1 rounded-md border border-line px-3 text-sm outline-none focus:border-brand" value={categoryName} onChange={(event) => { setCategoryName(event.target.value); categoryAttempt.current.clear(); }} placeholder="New category name" /><Button type="button" variant="secondary" disabled={!categoryName.trim() || busy === "category"} onClick={() => void createCategory()}><Tags className="h-4 w-4" />{busy === "category" ? "Saving…" : "Add category"}</Button></div><div className="mt-3 flex flex-wrap gap-2">{categories.filter((item) => !item.archived_at).map((item) => <Badge key={item.id} value={item.name} />)}{!categories.length ? <span className="text-sm text-muted">No categories yet.</span> : null}</div></Card>
    {loading ? <div className="space-y-3">{[0,1,2].map((item) => <Skeleton key={item} className="h-24 w-full" />)}</div> : null}
    {!loading && services.length === 0 ? <Card className="py-10 text-center"><CardTitle>No services yet</CardTitle><CardDescription>Add the first real salon service. Nothing is prefilled with demo data.</CardDescription><Button type="button" className="mt-4" onClick={openCreate}>Add service</Button></Card> : null}
    {!loading && services.length ? <div className="overflow-hidden rounded-lg border border-line bg-white shadow-soft">{services.map((item) => <div key={item.id} className="grid gap-4 border-b border-line px-5 py-4 last:border-0 md:grid-cols-[minmax(0,2fr)_minmax(180px,1fr)_auto] md:items-center"><div className="min-w-0"><div className="flex flex-wrap items-center gap-2"><span className="font-semibold text-ink">{item.name}</span><Badge value={item.archived_at ? "archived" : item.active ? "active" : "disabled"} /><Badge value={item.management_mode} /></div><p className="mt-1 text-sm text-muted">{item.duration_minutes} min · {item.price_display || (item.price_from !== undefined ? `$${item.price_from.toFixed(2)}` : "Price not set")}</p></div><div className="text-sm text-slate-700">{item.category?.name || "Uncategorized"}<div className="mt-1 text-xs text-muted">AI booking {item.ai_bookable ? "enabled" : "disabled"}</div></div><div className="flex gap-2"><Button type="button" variant="secondary" onClick={() => openEdit(item)}><Pencil className="h-4 w-4" />Edit</Button><Button type="button" variant="ghost" disabled={Boolean(item.archived_at) || item.management_mode === "provider_read_only" || busy === `archive-${item.id}`} onClick={() => void archive(item)}><Archive className="h-4 w-4" /><span className="sr-only">Archive</span></Button></div></div>)}</div> : null}
    <Dialog open={formOpen} title={editing ? `Edit ${editing.name}` : "Add service"} description={editing?.management_mode === "provider_read_only" ? "Operational fields come from the connected POS. Business-owned AI controls remain editable." : "This canonical record is shared by Tenant and Platform Business workflows."} onClose={() => { if (!busy) setFormOpen(false); }} closeDisabled={Boolean(busy)} footer={<div className="flex flex-col-reverse gap-2 sm:flex-row sm:justify-end"><Button type="button" variant="secondary" disabled={Boolean(busy)} onClick={() => setFormOpen(false)}>Close</Button><Button type="button" disabled={Boolean(busy)} onClick={() => void save()}>{busy === "save" ? "Saving…" : "Save service"}</Button></div>}>
      <ServiceFields form={form} setForm={(next) => { setForm(next); serviceAttempt.current.clear(); }} categories={categories} operationalReadOnly={editing?.management_mode === "provider_read_only"} platform={surface.kind === "platform"} />
    </Dialog>
  </div>;
}

function ServiceFields({ form, setForm, categories, operationalReadOnly, platform }: { form: ServiceForm; setForm: (value: ServiceForm) => void; categories: BusinessServiceCategory[]; operationalReadOnly: boolean; platform: boolean }) {
  const field = (key: keyof ServiceForm, value: string | boolean) => setForm({ ...form, [key]: value });
  return <div className="grid gap-4 sm:grid-cols-2"><Label text="Service name"><input className="field" value={form.name} disabled={operationalReadOnly} onChange={(event) => field("name", event.target.value)} /></Label><Label text="Duration (minutes)"><input className="field" type="number" min="1" value={form.duration} disabled={operationalReadOnly} onChange={(event) => field("duration", event.target.value)} /></Label><Label text="Price from"><input className="field" type="number" min="0" step="0.01" value={form.price} disabled={operationalReadOnly} onChange={(event) => field("price", event.target.value)} /></Label><Label text="Price display"><input className="field" value={form.priceDisplay} disabled={operationalReadOnly} onChange={(event) => field("priceDisplay", event.target.value)} placeholder="From $45" /></Label><Label text="Category"><select className="field" value={form.categoryID} onChange={(event) => field("categoryID", event.target.value)}><option value="">Uncategorized</option>{categories.filter((item) => !item.archived_at).map((item) => <option key={item.id} value={item.id}>{item.name}</option>)}</select></Label><Label text="Consultation profile"><select className="field" value={form.consultationStatus} onChange={(event) => field("consultationStatus", event.target.value)}><option value="draft">Draft</option><option value="ready">Ready</option><option value="disabled">Disabled</option></select></Label><Label text="Description" wide><textarea className="field min-h-24 py-2" value={form.description} disabled={operationalReadOnly} onChange={(event) => field("description", event.target.value)} /></Label><Label text="AI description" wide><textarea className="field min-h-24 py-2" value={form.aiDescription} onChange={(event) => field("aiDescription", event.target.value)} placeholder="Business-approved explanation for callers" /></Label><Label text="Recommended outcomes (comma separated)" wide><input className="field" value={form.outcomes} onChange={(event) => field("outcomes", event.target.value)} placeholder="maintain, shorten" /></Label><Label text="Compatible current systems (comma separated)" wide><input className="field" value={form.systems} onChange={(event) => field("systems", event.target.value)} placeholder="natural, gel" /></Label><Label text="Maintenance note" wide><input className="field" value={form.maintenanceNote} onChange={(event) => field("maintenanceNote", event.target.value)} /></Label><Label text="Owner-approved summary" wide><input className="field" value={form.ownerSummary} onChange={(event) => field("ownerSummary", event.target.value)} /></Label><label className="flex items-center gap-2 text-sm font-medium text-ink"><input type="checkbox" checked={form.aiBookable} onChange={(event) => field("aiBookable", event.target.checked)} />AI-bookable</label><label className="flex items-center gap-2 text-sm font-medium text-ink"><input type="checkbox" checked={form.active} disabled={operationalReadOnly} onChange={(event) => field("active", event.target.checked)} />Active</label>{platform ? <p className="sm:col-span-2 text-xs leading-5 text-muted">Changes are audited as the signed-in Platform actor; the salon owner is not impersonated.</p> : null}</div>;
}

function Label({ text, wide, children }: { text: string; wide?: boolean; children: React.ReactNode }) { return <label className={wide ? "block sm:col-span-2" : "block"}><span className="text-sm font-semibold text-ink">{text}</span><div className="mt-2">{children}</div></label>; }
function emptyServiceForm(): ServiceForm { return { name: "", description: "", aiDescription: "", duration: "30", price: "", priceDisplay: "", categoryID: "", active: true, aiBookable: true, consultationStatus: "draft", outcomes: "", systems: "", maintenanceNote: "", ownerSummary: "", lengthCapabilities: [], priorityTags: [], finishOptions: [] }; }
function serviceToForm(item: BusinessService): ServiceForm { const p=item.consultation_profile; return { name:item.name,description:item.description??"",aiDescription:item.ai_description??"",duration:String(item.duration_minutes),price:item.price_from===undefined?"":String(item.price_from),priceDisplay:item.price_display??"",categoryID:item.category?.id??"",active:item.active,aiBookable:item.ai_bookable,consultationStatus:p?.status??"draft",outcomes:(p?.recommended_outcomes??[]).join(", "),systems:(p?.compatible_current_systems??[]).join(", "),maintenanceNote:p?.maintenance_note??"",ownerSummary:p?.owner_approved_summary??"",lengthCapabilities:p?.length_capabilities??[],priorityTags:p?.priority_tags??[],finishOptions:p?.finish_options??[]}; }
function splitValues(value: string) { return [...new Set(value.split(",").map((item) => item.trim()).filter(Boolean))]; }
function slugify(value: string) { return value.toLowerCase().normalize("NFKD").replace(/[^a-z0-9]+/g,"-").replace(/^-|-$/g,""); }
function upsert(items: BusinessService[], item: BusinessService) { return [...items.filter((current) => current.id !== item.id), item].sort((a,b) => Number(Boolean(a.archived_at))-Number(Boolean(b.archived_at)) || a.name.localeCompare(b.name)); }
function message(error: unknown, fallback: string) { return error instanceof Error && error.message ? error.message : fallback; }
