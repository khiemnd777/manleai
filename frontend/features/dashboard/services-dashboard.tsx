"use client";

import { useEffect, useMemo, useState } from "react";
import type { ReactNode } from "react";
import { AlertTriangle, Archive, Check, Pencil, Plus, RefreshCcw, RotateCcw, Settings2, Tags, XCircle } from "lucide-react";
import { Alert } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardDescription, CardTitle } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { apiRequest } from "@/lib/api/client";
import type {
  POSConnection,
  POSService,
  POSServiceCategory,
  POSServiceCategoryAlias,
  Salon,
  ServiceCategorySuggestionRefresh,
  SquareReadiness,
  SyncLog
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

type ServiceCategoriesResponse = {
  service_categories: POSServiceCategory[];
};

type ServiceResponse = {
  service: POSService;
};

type ServiceCategoryResponse = {
  service_category: POSServiceCategory;
};

type ServiceCategoryAliasResponse = {
  service_category_alias: POSServiceCategoryAlias;
};

type RefreshCategoriesResponse = {
  refresh: ServiceCategorySuggestionRefresh;
};

type ServiceFormState = {
  name: string;
  description: string;
  aiDescription: string;
  durationMinutes: string;
  priceFrom: string;
  serviceCategoryID: string;
  active: boolean;
};

type ServiceCategoryFormState = {
  name: string;
  description: string;
  sortOrder: string;
};

type CategoryReviewFilter = "all" | "unassigned" | "suggested" | "manual" | "imported";

const categoryReviewFilters: CategoryReviewFilter[] = ["all", "unassigned", "suggested", "manual", "imported"];

export function ServicesDashboard() {
  const [salon, setSalon] = useState<Salon | null>(null);
  const [status, setStatus] = useState<StatusResponse | null>(null);
  const [services, setServices] = useState<POSService[]>([]);
  const [categories, setCategories] = useState<POSServiceCategory[]>([]);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");
  const [success, setSuccess] = useState("");
  const [formOpen, setFormOpen] = useState(false);
  const [editingService, setEditingService] = useState<POSService | null>(null);
  const [form, setForm] = useState<ServiceFormState>(emptyServiceForm());
  const [categoryFormOpen, setCategoryFormOpen] = useState(false);
  const [editingCategory, setEditingCategory] = useState<POSServiceCategory | null>(null);
  const [categoryForm, setCategoryForm] = useState<ServiceCategoryFormState>(emptyCategoryForm());
  const [aliasDraftCategoryID, setAliasDraftCategoryID] = useState("");
  const [aliasDraft, setAliasDraft] = useState("");
  const [categoryFilter, setCategoryFilter] = useState("all");
  const [reviewFilter, setReviewFilter] = useState<CategoryReviewFilter>("all");

  async function load({ silent = false }: { silent?: boolean } = {}) {
    setError("");
    if (!silent) {
      setLoading(true);
    }
    try {
      const salonResponse = await apiRequest<SalonListResponse>("/api/salons");
      const firstSalon = salonResponse.salons[0] ?? null;
      setSalon(firstSalon);
      if (!firstSalon) {
        setStatus(null);
        setServices([]);
        setCategories([]);
        return;
      }
      const [statusResponse, serviceResponse, categoryResponse] = await Promise.all([
        apiRequest<StatusResponse>(`/api/integrations/square/status?salon_id=${firstSalon.id}`),
        apiRequest<ServicesResponse>(`/api/salons/${firstSalon.id}/services`),
        apiRequest<ServiceCategoriesResponse>(`/api/salons/${firstSalon.id}/service-categories`)
      ]);
      setStatus(statusResponse);
      setServices(serviceResponse.services);
      setCategories(sortCategories(categoryResponse.service_categories));
    } catch (err) {
      setError(err instanceof Error ? err.message : "Could not load services.");
    } finally {
      if (!silent) {
        setLoading(false);
      }
    }
  }

  useEffect(() => {
    void load();
  }, []);

  const metrics = useMemo(() => serviceMetrics(services), [services]);
  const activeCategories = useMemo(() => categories.filter((category) => category.status === "active"), [categories]);
  const filteredServices = useMemo(
    () => filterServices(services, categoryFilter, reviewFilter),
    [services, categoryFilter, reviewFilter]
  );
  const aiEnabled = Boolean(status?.readiness?.ai_enabled ?? salon?.ai_enabled);

  function openCreateForm() {
    setEditingService(null);
    setForm(emptyServiceForm());
    setFormOpen(true);
    setError("");
    setSuccess("");
  }

  function openEditForm(service: POSService) {
    setEditingService(service);
    setForm(serviceToForm(service));
    setFormOpen(true);
    setError("");
    setSuccess("");
  }

  function openCategoryForm(category?: POSServiceCategory) {
    setEditingCategory(category ?? null);
    setCategoryForm(category ? categoryToForm(category) : emptyCategoryForm());
    setCategoryFormOpen(true);
    setError("");
    setSuccess("");
  }

  function closeCategoryForm() {
    setEditingCategory(null);
    setCategoryForm(emptyCategoryForm());
    setCategoryFormOpen(false);
  }

  async function saveService() {
    if (!salon) return;
    setBusy("save-service");
    setError("");
    setSuccess("");
    try {
      const body = JSON.stringify(servicePayload(form));
      const response = editingService?.id
        ? await apiRequest<ServiceResponse>(`/api/salons/${salon.id}/services/${editingService.id}`, {
            method: "PUT",
            body
          })
        : await apiRequest<ServiceResponse>(`/api/salons/${salon.id}/services`, {
            method: "POST",
            body
          });
      setServices((current) => upsertService(current, response.service));
      setSuccess(editingService ? "Service saved." : "Service created. Local-only services are not booking-ready until linked to Square Appointments.");
      setEditingService(response.service);
      setForm(serviceToForm(response.service));
      await load({ silent: true });
    } catch (err) {
      setError(err instanceof Error ? err.message : "Could not save service.");
    } finally {
      setBusy("");
    }
  }

  async function saveCategory() {
    if (!salon) return;
    setBusy("save-category");
    setError("");
    setSuccess("");
    try {
      const body = JSON.stringify(categoryPayload(categoryForm));
      const response = editingCategory
        ? await apiRequest<ServiceCategoryResponse>(`/api/salons/${salon.id}/service-categories/${editingCategory.id}`, {
            method: "PUT",
            body
          })
        : await apiRequest<ServiceCategoryResponse>(`/api/salons/${salon.id}/service-categories`, {
            method: "POST",
            body
          });
      setCategories((current) => upsertCategory(current, response.service_category));
      setSuccess(editingCategory ? "Service category saved." : "Service category created.");
      setEditingCategory(response.service_category);
      setCategoryForm(categoryToForm(response.service_category));
      await load({ silent: true });
    } catch (err) {
      setError(err instanceof Error ? err.message : "Could not save service category.");
    } finally {
      setBusy("");
    }
  }

  async function archiveCategory(category: POSServiceCategory) {
    if (!salon || category.status === "archived") return;
    if (!window.confirm(`Archive ${category.name}? Services in this category will become unassigned.`)) return;
    setBusy(`category-archive-${category.id}`);
    setError("");
    setSuccess("");
    try {
      const response = await apiRequest<ServiceCategoryResponse>(`/api/salons/${salon.id}/service-categories/${category.id}/archive`, {
        method: "POST"
      });
      setCategories((current) => upsertCategory(current, response.service_category));
      setSuccess("Service category archived. Assigned services are no longer grouped under it.");
      await load({ silent: true });
    } catch (err) {
      setError(err instanceof Error ? err.message : "Could not archive service category.");
    } finally {
      setBusy("");
    }
  }

  async function restoreCategory(category: POSServiceCategory) {
    if (!salon || category.status !== "archived") return;
    setBusy(`category-restore-${category.id}`);
    setError("");
    setSuccess("");
    try {
      const response = await apiRequest<ServiceCategoryResponse>(`/api/salons/${salon.id}/service-categories/${category.id}/restore`, {
        method: "POST"
      });
      setCategories((current) => upsertCategory(current, response.service_category));
      setSuccess("Service category restored.");
      await load({ silent: true });
    } catch (err) {
      setError(err instanceof Error ? err.message : "Could not restore service category.");
    } finally {
      setBusy("");
    }
  }

  async function addCategoryAlias(category: POSServiceCategory) {
    if (!salon || !aliasDraft.trim()) return;
    setBusy(`category-alias-${category.id}`);
    setError("");
    setSuccess("");
    try {
      await apiRequest<ServiceCategoryAliasResponse>(`/api/salons/${salon.id}/service-categories/${category.id}/aliases`, {
        method: "POST",
        body: JSON.stringify({ alias: aliasDraft, confidence: 1 })
      });
      setAliasDraft("");
      setAliasDraftCategoryID("");
      setSuccess("Category alias saved.");
      await load({ silent: true });
    } catch (err) {
      setError(err instanceof Error ? err.message : "Could not save category alias.");
    } finally {
      setBusy("");
    }
  }

  async function archiveCategoryAlias(alias: POSServiceCategoryAlias) {
    if (!salon || alias.status === "archived") return;
    setBusy(`category-alias-archive-${alias.id}`);
    setError("");
    setSuccess("");
    try {
      await apiRequest<ServiceCategoryAliasResponse>(`/api/salons/${salon.id}/service-category-aliases/${alias.id}/archive`, {
        method: "POST"
      });
      setSuccess("Category alias archived.");
      await load({ silent: true });
    } catch (err) {
      setError(err instanceof Error ? err.message : "Could not archive category alias.");
    } finally {
      setBusy("");
    }
  }

  async function refreshCategorySuggestions() {
    if (!salon) return;
    setBusy("refresh-categories");
    setError("");
    setSuccess("");
    try {
      const response = await apiRequest<RefreshCategoriesResponse>(`/api/salons/${salon.id}/service-categories/suggestions/refresh`, {
        method: "POST"
      });
      setSuccess(refreshSummary(response.refresh));
      await load({ silent: true });
    } catch (err) {
      setError(err instanceof Error ? err.message : "Could not refresh category suggestions.");
    } finally {
      setBusy("");
    }
  }

  async function assignServiceCategory(service: POSService, categoryID: string) {
    if (!salon || !service.id) return;
    setBusy(`category-assign-${service.id}`);
    setError("");
    setSuccess("");
    try {
      const response = await apiRequest<ServiceResponse>(`/api/salons/${salon.id}/services/${service.id}/category`, {
        method: "PATCH",
        body: JSON.stringify({ service_category_id: categoryID })
      });
      setServices((current) => upsertService(current, response.service));
      if (editingService?.id === response.service.id) {
        setEditingService(response.service);
        setForm(serviceToForm(response.service));
      }
      setSuccess(categoryID ? "Service category confirmed." : "Service category cleared.");
      await load({ silent: true });
    } catch (err) {
      setError(err instanceof Error ? err.message : "Could not update service category.");
    } finally {
      setBusy("");
    }
  }

  async function archiveService(service: POSService) {
    if (!salon || !service.id || service.archived_at) return;
    if (!window.confirm(`Archive ${service.name}? This will disable AI booking for this service.`)) return;
    setBusy(`archive-${service.id}`);
    setError("");
    setSuccess("");
    try {
      const response = await apiRequest<ServiceResponse>(`/api/salons/${salon.id}/services/${service.id}/archive`, {
        method: "POST"
      });
      setServices((current) => upsertService(current, response.service));
      if (editingService?.id === response.service.id) {
        setEditingService(response.service);
        setForm(serviceToForm(response.service));
      }
      setSuccess("Service archived. It will not be used for new availability checks or bookings.");
      await load({ silent: true });
    } catch (err) {
      setError(err instanceof Error ? err.message : "Could not archive service.");
    } finally {
      setBusy("");
    }
  }

  async function updateAIBookable(service: POSService, nextValue: boolean) {
    if (!salon || !service.id) return;
    setBusy(`ai-${service.id}`);
    setError("");
    setSuccess("");
    try {
      const response = await apiRequest<ServiceResponse>(`/api/salons/${salon.id}/services/${service.id}/ai-bookable`, {
        method: "PATCH",
        body: JSON.stringify({ ai_bookable: nextValue })
      });
      setServices((current) => upsertService(current, response.service));
      if (editingService?.id === response.service.id) {
        setEditingService(response.service);
        setForm(serviceToForm(response.service));
      }
      setSuccess(nextValue ? "Service marked booking-ready for the AI receptionist." : "Service removed from AI booking.");
      await load({ silent: true });
    } catch (err) {
      setError(err instanceof Error ? err.message : "Could not update AI booking eligibility.");
    } finally {
      setBusy("");
    }
  }

  if (loading) {
    return (
      <div className="space-y-6">
        <Skeleton className="h-9 w-80" />
        <div className="grid gap-4 md:grid-cols-4">
          <Skeleton className="h-28" />
          <Skeleton className="h-28" />
          <Skeleton className="h-28" />
          <Skeleton className="h-28" />
        </div>
        <Skeleton className="h-[34rem]" />
      </div>
    );
  }

  if (!salon) {
    return (
      <Card>
        <CardTitle>Create a salon first</CardTitle>
        <CardDescription>Services are scoped by salon, so the owner profile must exist first.</CardDescription>
      </Card>
    );
  }

  return (
    <div className="space-y-6">
      <div className="flex flex-col justify-between gap-3 md:flex-row md:items-end">
        <div>
          <h1 className="text-2xl font-bold text-ink">Services</h1>
          <p className="mt-1 text-sm text-muted">
            Manage ManleAI-owned service records. Square Appointments executes availability and booking.
          </p>
        </div>
        <div className="flex flex-wrap items-center gap-3">
          <Badge value={aiEnabled ? "active" : "disabled"} />
          <Button type="button" variant="secondary" onClick={() => void load()} disabled={busy !== ""}>
            <RefreshCcw className="h-4 w-4" />
            Refresh
          </Button>
          <Button type="button" variant="secondary" onClick={() => void refreshCategorySuggestions()} disabled={busy !== ""}>
            <Tags className="h-4 w-4" />
            Refresh category suggestions
          </Button>
          <Button type="button" onClick={openCreateForm} disabled={busy !== ""}>
            <Plus className="h-4 w-4" />
            New service
          </Button>
        </div>
      </div>

      {error ? <Alert title="Services unavailable" message={error} /> : null}
      {success ? <Alert type="success" title="Services updated" message={success} /> : null}

      <ServicesGate status={status} />
      <BookingEligibilityPanel />

      <div className="grid gap-4 md:grid-cols-3 xl:grid-cols-6">
        <Metric label="Total services" value={String(metrics.total)} />
        <Metric label="Synced" value={String(metrics.synced)} />
        <Metric label="Local only" value={String(metrics.localOnly)} />
        <Metric label="Booking-ready" value={String(metrics.aiBookable)} />
        <Metric label="Categorized" value={String(metrics.categorized)} />
        <Metric label="Suggested" value={String(metrics.suggested)} />
      </div>

      <CategoryManagementPanel
        categories={categories}
        formOpen={categoryFormOpen}
        form={categoryForm}
        editingCategory={editingCategory}
        busy={busy}
        aliasDraftCategoryID={aliasDraftCategoryID}
        aliasDraft={aliasDraft}
        onOpenCreate={() => openCategoryForm()}
        onOpenEdit={openCategoryForm}
        onCloseForm={closeCategoryForm}
        onFormChange={setCategoryForm}
        onSave={() => void saveCategory()}
        onArchive={(category) => void archiveCategory(category)}
        onRestore={(category) => void restoreCategory(category)}
        onAliasDraftCategoryChange={setAliasDraftCategoryID}
        onAliasDraftChange={setAliasDraft}
        onAddAlias={(category) => void addCategoryAlias(category)}
        onArchiveAlias={(alias) => void archiveCategoryAlias(alias)}
      />

      {formOpen ? (
        <ServiceForm
          form={form}
          service={editingService}
          categories={activeCategories}
          busy={busy === "save-service"}
          onChange={setForm}
          onCancel={() => {
            setFormOpen(false);
            setEditingService(null);
            setForm(emptyServiceForm());
          }}
          onSave={() => void saveService()}
        />
      ) : null}

      <Card>
        <div className="flex flex-col justify-between gap-3 sm:flex-row sm:items-start">
          <div>
            <CardTitle>Service catalog</CardTitle>
            <CardDescription>
              Only active, synced, POS-linked, AI-bookable services are used for availability and booking.
            </CardDescription>
          </div>
          <Badge value={services.length > 0 ? "active" : "disabled"} />
        </div>

        <ServiceFilters
          categories={activeCategories}
          categoryFilter={categoryFilter}
          reviewFilter={reviewFilter}
          onCategoryFilterChange={setCategoryFilter}
          onReviewFilterChange={setReviewFilter}
        />

        {services.length === 0 ? (
          <EmptyState onCreate={openCreateForm} />
        ) : filteredServices.length === 0 ? (
          <FilteredEmptyState />
        ) : (
          <ServicesTable
            services={filteredServices}
            categories={activeCategories}
            busy={busy}
            onEdit={openEditForm}
            onArchive={(service) => void archiveService(service)}
            onUpdateAI={(service, nextValue) => void updateAIBookable(service, nextValue)}
            onAssignCategory={(service, categoryID) => void assignServiceCategory(service, categoryID)}
          />
        )}
      </Card>
    </div>
  );
}

function ServicesGate({ status }: { status: StatusResponse | null }) {
  const connection = status?.connection;
  const connected = Boolean(connection?.id) && connection?.status !== "not_connected";
  const locationSelected = Boolean(connection?.location_id);
  const lastSync = connection?.last_sync_at;

  if (connected && locationSelected && lastSync) {
    return (
      <Card className="border-emerald-200 bg-emerald-50 shadow-none">
        <div className="flex flex-col justify-between gap-3 sm:flex-row sm:items-center">
          <div>
            <CardTitle>Square sync is ready</CardTitle>
            <CardDescription className="text-emerald-800">
              Last synced {new Date(lastSync).toLocaleString()}. Synced services can become booking-ready after AI booking is allowed.
            </CardDescription>
          </div>
          <Badge value="active" />
        </div>
      </Card>
    );
  }

  return (
    <Card className="border-amber-200 bg-amber-50 shadow-none">
      <div className="flex gap-3">
        <AlertTriangle className="mt-0.5 h-5 w-5 flex-none text-amber-700" />
        <div>
          <CardTitle>Square sync required for AI booking</CardTitle>
          <CardDescription className="text-amber-900">
            Local services can be managed now. Booking remains gated until Square Appointments is connected, a location is selected, and services are synced.
          </CardDescription>
          <a
            className="mt-3 inline-flex h-10 items-center justify-center rounded-md border border-line bg-white px-4 text-sm font-semibold text-ink hover:bg-slate-50"
            href="/dashboard/integrations"
          >
            Open Square integration
          </a>
        </div>
      </div>
    </Card>
  );
}

function BookingEligibilityPanel() {
  return (
    <Card>
      <div className="flex flex-col justify-between gap-3 sm:flex-row sm:items-start">
        <div>
          <CardTitle>Booking eligibility</CardTitle>
          <CardDescription>
            Service records can exist locally, but booking stays gated until the active POS link is ready.
          </CardDescription>
        </div>
        <Badge value="booking" />
      </div>
      <div className="mt-4 grid gap-3 md:grid-cols-3">
        <EligibilityItem
          label="Booking-ready service"
          value="Active, synced, POS linked, and allowed for AI booking."
        />
        <EligibilityItem
          label="Not bookable"
          value="Local-only, unmapped, sync-failed, archived, or inactive services stay out of availability and booking."
        />
        <EligibilityItem
          label="Booking execution"
          value="Square Appointments returns availability and booking IDs before appointments can be confirmed."
        />
      </div>
    </Card>
  );
}

function CategoryManagementPanel({
  categories,
  formOpen,
  form,
  editingCategory,
  busy,
  aliasDraftCategoryID,
  aliasDraft,
  onOpenCreate,
  onOpenEdit,
  onCloseForm,
  onFormChange,
  onSave,
  onArchive,
  onRestore,
  onAliasDraftCategoryChange,
  onAliasDraftChange,
  onAddAlias,
  onArchiveAlias
}: {
  categories: POSServiceCategory[];
  formOpen: boolean;
  form: ServiceCategoryFormState;
  editingCategory: POSServiceCategory | null;
  busy: string;
  aliasDraftCategoryID: string;
  aliasDraft: string;
  onOpenCreate: () => void;
  onOpenEdit: (category: POSServiceCategory) => void;
  onCloseForm: () => void;
  onFormChange: (next: ServiceCategoryFormState) => void;
  onSave: () => void;
  onArchive: (category: POSServiceCategory) => void;
  onRestore: (category: POSServiceCategory) => void;
  onAliasDraftCategoryChange: (categoryID: string) => void;
  onAliasDraftChange: (value: string) => void;
  onAddAlias: (category: POSServiceCategory) => void;
  onArchiveAlias: (alias: POSServiceCategoryAlias) => void;
}) {
  return (
    <Card>
      <div className="flex flex-col justify-between gap-3 sm:flex-row sm:items-start">
        <div>
          <CardTitle>Service categories</CardTitle>
          <CardDescription>
            Categories teach the receptionist service groups such as manicure, pedicure, acrylic, removal, and spa. They are not party bookings.
          </CardDescription>
        </div>
        <Button type="button" variant="secondary" onClick={onOpenCreate} disabled={busy !== ""}>
          <Plus className="h-4 w-4" />
          New category
        </Button>
      </div>

      {formOpen ? (
        <div className="mt-5 rounded-md border border-line bg-slate-50 p-4">
          <div className="flex flex-col justify-between gap-3 sm:flex-row sm:items-start">
            <div>
              <div className="text-sm font-semibold text-ink">{editingCategory ? "Edit category" : "New category"}</div>
              <div className="mt-1 text-xs leading-5 text-muted">
                Category names and aliases are used for clarification, not as directly bookable services.
              </div>
            </div>
            {editingCategory ? <Badge value={editingCategory.status} /> : <Badge value="active" />}
          </div>
          <div className="mt-4 grid gap-4 md:grid-cols-[1fr_1fr_8rem]">
            <Field label="Name">
              <input
                className="h-10 w-full rounded-md border border-line bg-white px-3 text-sm text-ink outline-none focus:border-brand"
                value={form.name}
                onChange={(event) => onFormChange({ ...form, name: event.target.value })}
                disabled={busy !== ""}
              />
            </Field>
            <Field label="Description">
              <input
                className="h-10 w-full rounded-md border border-line bg-white px-3 text-sm text-ink outline-none focus:border-brand"
                value={form.description}
                onChange={(event) => onFormChange({ ...form, description: event.target.value })}
                disabled={busy !== ""}
              />
            </Field>
            <Field label="Sort order">
              <input
                className="h-10 w-full rounded-md border border-line bg-white px-3 text-sm text-ink outline-none focus:border-brand"
                type="number"
                value={form.sortOrder}
                onChange={(event) => onFormChange({ ...form, sortOrder: event.target.value })}
                disabled={busy !== ""}
              />
            </Field>
          </div>
          <div className="mt-4 flex flex-wrap justify-end gap-3">
            <Button type="button" variant="secondary" onClick={onCloseForm} disabled={busy !== ""}>
              Cancel
            </Button>
            <Button type="button" onClick={onSave} disabled={busy !== "" || !form.name.trim()}>
              {busy === "save-category" ? "Saving..." : "Save category"}
            </Button>
          </div>
        </div>
      ) : null}

      {categories.length === 0 ? (
        <div className="mt-5 rounded-md border border-line p-5 text-sm leading-6 text-muted">
          No categories yet. Refresh suggestions to seed the commercial salon taxonomy, or create a category manually.
        </div>
      ) : (
        <div className="mt-5 grid gap-3 lg:grid-cols-2">
          {categories.map((category) => {
            const activeAliases = (category.aliases ?? []).filter((alias) => alias.status !== "archived");
            const aliasBusy = busy === `category-alias-${category.id}`;
            return (
              <div key={category.id} className="rounded-md border border-line p-4">
                <div className="flex flex-col justify-between gap-3 sm:flex-row sm:items-start">
                  <div className="min-w-0">
                    <div className="flex flex-wrap items-center gap-2">
                      <div className="font-semibold text-ink">{category.name}</div>
                      <Badge value={category.status} />
                      <Badge value={category.source} />
                    </div>
                    <div className="mt-1 text-xs leading-5 text-muted">
                      {category.description || "No description."}
                    </div>
                    <div className="mt-2 text-xs text-muted">
                      {category.service_count} service{category.service_count === 1 ? "" : "s"} assigned
                    </div>
                  </div>
                  <div className="flex flex-wrap gap-2">
                    <Button type="button" variant="secondary" onClick={() => onOpenEdit(category)} disabled={busy !== ""}>
                      <Pencil className="h-4 w-4" />
                      Edit
                    </Button>
                    {category.status === "archived" ? (
                      <Button type="button" variant="secondary" onClick={() => onRestore(category)} disabled={busy !== ""}>
                        <RotateCcw className="h-4 w-4" />
                        {busy === `category-restore-${category.id}` ? "Restoring..." : "Restore"}
                      </Button>
                    ) : (
                      <Button type="button" variant="danger" onClick={() => onArchive(category)} disabled={busy !== ""}>
                        <Archive className="h-4 w-4" />
                        {busy === `category-archive-${category.id}` ? "Archiving..." : "Archive"}
                      </Button>
                    )}
                  </div>
                </div>

                <div className="mt-4">
                  <div className="text-xs font-semibold uppercase tracking-wide text-muted">Aliases</div>
                  {activeAliases.length > 0 ? (
                    <div className="mt-2 flex flex-wrap gap-2">
                      {activeAliases.map((alias) => (
                        <span key={alias.id} className="inline-flex items-center gap-1 rounded-full border border-line bg-slate-50 px-2.5 py-1 text-xs font-medium text-ink">
                          {alias.alias}
                          <button
                            type="button"
                            className="text-muted hover:text-red-700"
                            onClick={() => onArchiveAlias(alias)}
                            disabled={busy !== ""}
                            aria-label={`Archive ${alias.alias}`}
                          >
                            <XCircle className="h-3.5 w-3.5" />
                          </button>
                        </span>
                      ))}
                    </div>
                  ) : (
                    <div className="mt-2 text-xs leading-5 text-muted">No active aliases.</div>
                  )}

                  {aliasDraftCategoryID === category.id ? (
                    <div className="mt-3 flex flex-col gap-2 sm:flex-row">
                      <input
                        className="h-10 min-w-0 flex-1 rounded-md border border-line px-3 text-sm text-ink outline-none focus:border-brand"
                        value={aliasDraft}
                        onChange={(event) => onAliasDraftChange(event.target.value)}
                        placeholder="Example: polish removal"
                        disabled={busy !== ""}
                      />
                      <Button type="button" onClick={() => onAddAlias(category)} disabled={busy !== "" || !aliasDraft.trim()}>
                        {aliasBusy ? "Saving..." : "Save alias"}
                      </Button>
                      <Button
                        type="button"
                        variant="secondary"
                        onClick={() => {
                          onAliasDraftCategoryChange("");
                          onAliasDraftChange("");
                        }}
                        disabled={busy !== ""}
                      >
                        Cancel
                      </Button>
                    </div>
                  ) : category.status === "active" ? (
                    <Button
                      type="button"
                      variant="ghost"
                      className="mt-3"
                      onClick={() => {
                        onAliasDraftCategoryChange(category.id);
                        onAliasDraftChange("");
                      }}
                      disabled={busy !== ""}
                    >
                      <Plus className="h-4 w-4" />
                      Add alias
                    </Button>
                  ) : null}
                </div>
              </div>
            );
          })}
        </div>
      )}
    </Card>
  );
}

function EligibilityItem({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-md border border-line p-3">
      <div className="text-sm font-semibold text-ink">{label}</div>
      <div className="mt-1 text-xs leading-5 text-muted">{value}</div>
    </div>
  );
}

function ServiceForm({
  form,
  service,
  categories,
  busy,
  onChange,
  onCancel,
  onSave
}: {
  form: ServiceFormState;
  service: POSService | null;
  categories: POSServiceCategory[];
  busy: boolean;
  onChange: (next: ServiceFormState) => void;
  onCancel: () => void;
  onSave: () => void;
}) {
  const archived = Boolean(service?.archived_at);
  return (
    <Card>
      <div className="flex flex-col justify-between gap-3 sm:flex-row sm:items-start">
        <div>
          <CardTitle>{service ? "Edit service" : "New service"}</CardTitle>
          <CardDescription>
            {service ? serviceGateReason(service) : "New services start as ManleAI local records and are not booking-ready until linked to Square Appointments."}
          </CardDescription>
        </div>
        {service ? <Badge value={service.sync_status || "local_only"} /> : <Badge value="local_only" />}
      </div>

      <div className="mt-5 grid gap-4 md:grid-cols-2">
        <Field label="Name">
          <input
            className="h-10 w-full rounded-md border border-line px-3 text-sm text-ink outline-none focus:border-brand"
            value={form.name}
            onChange={(event) => onChange({ ...form, name: event.target.value })}
            disabled={busy || archived}
          />
        </Field>
        <Field label="Duration minutes">
          <input
            className="h-10 w-full rounded-md border border-line px-3 text-sm text-ink outline-none focus:border-brand"
            type="number"
            min="1"
            value={form.durationMinutes}
            onChange={(event) => onChange({ ...form, durationMinutes: event.target.value })}
            disabled={busy || archived}
          />
        </Field>
        <Field label="Price from">
          <input
            className="h-10 w-full rounded-md border border-line px-3 text-sm text-ink outline-none focus:border-brand"
            type="number"
            min="0"
            step="0.01"
            value={form.priceFrom}
            onChange={(event) => onChange({ ...form, priceFrom: event.target.value })}
            disabled={busy || archived}
          />
        </Field>
        <Field label="Category">
          <select
            className="h-10 w-full rounded-md border border-line bg-white px-3 text-sm text-ink outline-none focus:border-brand"
            value={form.serviceCategoryID}
            onChange={(event) => onChange({ ...form, serviceCategoryID: event.target.value })}
            disabled={busy || archived}
          >
            <option value="">Unassigned</option>
            {categories.map((category) => (
              <option key={category.id} value={category.id}>
                {category.name}
              </option>
            ))}
          </select>
        </Field>
        <label className="flex items-center gap-3 rounded-md border border-line px-3 py-2 text-sm font-medium text-ink">
          <input
            type="checkbox"
            checked={form.active}
            onChange={(event) => onChange({ ...form, active: event.target.checked })}
            disabled={busy || archived}
          />
          Active
        </label>
        <Field label="Description">
          <textarea
            className="min-h-24 w-full rounded-md border border-line px-3 py-2 text-sm text-ink outline-none focus:border-brand"
            value={form.description}
            onChange={(event) => onChange({ ...form, description: event.target.value })}
            disabled={busy || archived}
          />
        </Field>
        <Field label="AI description">
          <textarea
            className="min-h-24 w-full rounded-md border border-line px-3 py-2 text-sm text-ink outline-none focus:border-brand"
            value={form.aiDescription}
            onChange={(event) => onChange({ ...form, aiDescription: event.target.value })}
            disabled={busy || archived}
          />
        </Field>
      </div>

      <div className="mt-5 flex flex-wrap justify-end gap-3">
        <Button type="button" variant="secondary" onClick={onCancel} disabled={busy}>
          Cancel
        </Button>
        <Button type="button" onClick={onSave} disabled={busy || archived}>
          {busy ? "Saving..." : "Save service"}
        </Button>
      </div>
    </Card>
  );
}

function ServicesTable({
  services,
  categories,
  busy,
  onEdit,
  onArchive,
  onUpdateAI,
  onAssignCategory
}: {
  services: POSService[];
  categories: POSServiceCategory[];
  busy: string;
  onEdit: (service: POSService) => void;
  onArchive: (service: POSService) => void;
  onUpdateAI: (service: POSService, nextValue: boolean) => void;
  onAssignCategory: (service: POSService, categoryID: string) => void;
}) {
  return (
    <>
      <div className="mt-5 hidden overflow-x-auto rounded-md border border-line lg:block">
        <table className="w-full min-w-[1220px] text-left text-sm">
          <thead className="bg-slate-50 text-xs uppercase text-muted">
            <tr>
              <th className="px-4 py-3">Service</th>
              <th className="px-4 py-3">Duration</th>
              <th className="px-4 py-3">Price</th>
              <th className="px-4 py-3">Source</th>
              <th className="px-4 py-3">Category</th>
              <th className="px-4 py-3">Sync status</th>
              <th className="px-4 py-3">Booking readiness</th>
              <th className="px-4 py-3">Actions</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-line bg-white">
            {services.map((service) => (
              <tr key={service.id || service.pos_service_id || service.name}>
                <td className="px-4 py-3">
                  <div className="font-medium text-ink">{service.name}</div>
                  <div className="mt-1 max-w-sm text-xs leading-5 text-muted">{service.description || "No description."}</div>
                </td>
                <td className="px-4 py-3 text-muted">{service.duration_minutes > 0 ? `${service.duration_minutes} min` : "Not set"}</td>
                <td className="px-4 py-3 text-muted">{service.price_display || formatPrice(service.price_from)}</td>
                <td className="px-4 py-3">
                  <Badge value={service.source || "local"} />
                </td>
                <td className="px-4 py-3">
                  <CategoryCell service={service} categories={categories} busy={busy} onAssignCategory={onAssignCategory} />
                </td>
                <td className="px-4 py-3">
                  <div className="space-y-1">
                    <Badge value={service.sync_status || "local_only"} />
                    {service.sync_error ? <div className="max-w-44 text-xs leading-5 text-red-700">{service.sync_error}</div> : null}
                  </div>
                </td>
                <td className="px-4 py-3">
                  <AIStatus service={service} />
                </td>
                <td className="px-4 py-3">
                  <ServiceActions service={service} busy={busy} onEdit={onEdit} onArchive={onArchive} onUpdateAI={onUpdateAI} />
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <div className="mt-5 space-y-3 lg:hidden">
        {services.map((service) => (
          <ServiceCard
            key={service.id || service.pos_service_id || service.name}
            service={service}
            categories={categories}
            busy={busy}
            onEdit={onEdit}
            onArchive={onArchive}
            onUpdateAI={onUpdateAI}
            onAssignCategory={onAssignCategory}
          />
        ))}
      </div>
    </>
  );
}

function ServiceCard({
  service,
  categories,
  busy,
  onEdit,
  onArchive,
  onUpdateAI,
  onAssignCategory
}: {
  service: POSService;
  categories: POSServiceCategory[];
  busy: string;
  onEdit: (service: POSService) => void;
  onArchive: (service: POSService) => void;
  onUpdateAI: (service: POSService, nextValue: boolean) => void;
  onAssignCategory: (service: POSService, categoryID: string) => void;
}) {
  return (
    <div className="rounded-md border border-line p-4">
      <div className="flex items-start justify-between gap-3">
        <div>
          <div className="text-sm font-semibold text-ink">{service.name}</div>
          <div className="mt-1 text-xs leading-5 text-muted">{service.description || "No description."}</div>
        </div>
        <Badge value={service.sync_status || "local_only"} />
      </div>
      <InfoGrid
        items={[
          ["Duration", service.duration_minutes > 0 ? `${service.duration_minutes} min` : "Not set"],
          ["Price", service.price_display || formatPrice(service.price_from)],
          ["Source", service.source || "local"],
          ["Category", service.category_name || "Unassigned"],
          ["POS link", service.pos_linked ? "Linked" : "Not linked"]
        ]}
      />
      <div className="mt-4">
        <CategoryCell service={service} categories={categories} busy={busy} onAssignCategory={onAssignCategory} />
      </div>
      <div className="mt-4">
        <AIStatus service={service} />
      </div>
      <div className="mt-4">
        <ServiceActions service={service} busy={busy} onEdit={onEdit} onArchive={onArchive} onUpdateAI={onUpdateAI} />
      </div>
    </div>
  );
}

function ServiceFilters({
  categories,
  categoryFilter,
  reviewFilter,
  onCategoryFilterChange,
  onReviewFilterChange
}: {
  categories: POSServiceCategory[];
  categoryFilter: string;
  reviewFilter: CategoryReviewFilter;
  onCategoryFilterChange: (value: string) => void;
  onReviewFilterChange: (value: CategoryReviewFilter) => void;
}) {
  return (
    <div className="mt-5 grid gap-3 md:grid-cols-[1fr_1fr]">
      <Field label="Category filter">
        <select
          className="h-10 w-full rounded-md border border-line bg-white px-3 text-sm text-ink outline-none focus:border-brand"
          value={categoryFilter}
          onChange={(event) => onCategoryFilterChange(event.target.value)}
        >
          <option value="all">All categories</option>
          <option value="unassigned">Unassigned</option>
          {categories.map((category) => (
            <option key={category.id} value={category.id}>
              {category.name}
            </option>
          ))}
        </select>
      </Field>
      <Field label="Review source">
        <select
          className="h-10 w-full rounded-md border border-line bg-white px-3 text-sm text-ink outline-none focus:border-brand"
          value={reviewFilter}
          onChange={(event) => onReviewFilterChange(event.target.value as CategoryReviewFilter)}
        >
          {categoryReviewFilters.map((filter) => (
            <option key={filter} value={filter}>
              {filter === "all" ? "All review states" : filter.replaceAll("_", " ")}
            </option>
          ))}
        </select>
      </Field>
    </div>
  );
}

function CategoryCell({
  service,
  categories,
  busy,
  onAssignCategory
}: {
  service: POSService;
  categories: POSServiceCategory[];
  busy: string;
  onAssignCategory: (service: POSService, categoryID: string) => void;
}) {
  const categoryBusy = busy === `category-assign-${service.id}`;
  const source = service.category_source || "unassigned";
  const archived = Boolean(service.archived_at);
  return (
    <div className="space-y-2">
      <div className="flex flex-wrap items-center gap-2">
        <span className="text-sm font-medium text-ink">{service.category_name || "Unassigned"}</span>
        <Badge value={source} />
      </div>
      {source === "suggested" && service.service_category_id ? (
        <div className="flex flex-wrap gap-2">
          <Button
            type="button"
            variant="secondary"
            onClick={() => onAssignCategory(service, service.service_category_id || "")}
            disabled={busy !== "" || archived}
          >
            <Check className="h-4 w-4" />
            {categoryBusy ? "Saving..." : "Accept"}
          </Button>
          <Button type="button" variant="ghost" onClick={() => onAssignCategory(service, "")} disabled={busy !== "" || archived}>
            Clear
          </Button>
        </div>
      ) : (
        <select
          className="h-10 w-full min-w-40 rounded-md border border-line bg-white px-3 text-sm text-ink outline-none focus:border-brand"
          value={service.service_category_id || ""}
          onChange={(event) => onAssignCategory(service, event.target.value)}
          disabled={busy !== "" || archived || !service.id}
        >
          <option value="">Unassigned</option>
          {categories.map((category) => (
            <option key={category.id} value={category.id}>
              {category.name}
            </option>
          ))}
        </select>
      )}
      {service.category_confidence && source === "suggested" ? (
        <div className="text-xs text-muted">Confidence {Math.round(service.category_confidence * 100)}%</div>
      ) : null}
    </div>
  );
}

function ServiceActions({
  service,
  busy,
  onEdit,
  onArchive,
  onUpdateAI
}: {
  service: POSService;
  busy: string;
  onEdit: (service: POSService) => void;
  onArchive: (service: POSService) => void;
  onUpdateAI: (service: POSService, nextValue: boolean) => void;
}) {
  const aiBusy = busy === `ai-${service.id}`;
  const archiveBusy = busy === `archive-${service.id}`;
  const archived = Boolean(service.archived_at);
  const canEnable = canEnableAI(service);
  const nextAI = !service.ai_bookable;
  return (
    <div className="flex flex-wrap gap-2">
      <Button type="button" variant="secondary" onClick={() => onEdit(service)} disabled={busy !== ""}>
        <Pencil className="h-4 w-4" />
        Edit
      </Button>
      <Button
        type="button"
        variant={service.ai_bookable ? "secondary" : "primary"}
        onClick={() => onUpdateAI(service, nextAI)}
        disabled={busy !== "" || !service.id || (!service.ai_bookable && !canEnable)}
      >
        {aiBusy ? "Saving..." : service.ai_bookable ? "Block AI booking" : canEnable ? "Allow AI booking" : "AI booking gated"}
      </Button>
      <Button type="button" variant="danger" onClick={() => onArchive(service)} disabled={busy !== "" || archived || !service.id}>
        <Archive className="h-4 w-4" />
        {archiveBusy ? "Archiving..." : "Archive"}
      </Button>
    </div>
  );
}

function AIStatus({ service }: { service: POSService }) {
  return (
    <div className="space-y-1">
      <Badge value={service.ai_bookable && canEnableAI(service) ? "allowed" : "blocked"} />
      <div className="max-w-56 text-xs leading-5 text-muted">{serviceGateReason(service)}</div>
    </div>
  );
}

function Metric({ label, value }: { label: string; value: string }) {
  return (
    <Card>
      <div className="text-xs font-semibold uppercase tracking-wide text-muted">{label}</div>
      <div className="mt-2 text-2xl font-bold text-ink">{value}</div>
    </Card>
  );
}

function EmptyState({ onCreate }: { onCreate: () => void }) {
  return (
    <div className="mt-5 rounded-md border border-line p-6 text-center">
      <div className="mx-auto flex h-10 w-10 items-center justify-center rounded-md bg-slate-100">
        <Settings2 className="h-5 w-5 text-muted" />
      </div>
      <div className="mt-3 text-sm font-semibold text-ink">No services yet</div>
      <div className="mt-1 text-sm leading-6 text-muted">
        Create a local service or sync Square Appointments services. Local records are not bookable until linked.
      </div>
      <div className="mt-4 flex flex-wrap justify-center gap-3">
        <Button type="button" onClick={onCreate}>
          <Plus className="h-4 w-4" />
          New service
        </Button>
        <a
          className="inline-flex h-10 items-center justify-center rounded-md border border-line bg-white px-4 text-sm font-semibold text-ink hover:bg-slate-50"
          href="/dashboard/integrations"
        >
          Open Square integration
        </a>
      </div>
    </div>
  );
}

function FilteredEmptyState() {
  return (
    <div className="mt-5 rounded-md border border-line p-6 text-center">
      <Tags className="mx-auto h-5 w-5 text-muted" />
      <div className="mt-3 text-sm font-semibold text-ink">No services match these filters</div>
      <div className="mt-1 text-sm leading-6 text-muted">
        Change the category or review source filter to inspect more services.
      </div>
    </div>
  );
}

function Field({ label, children }: { label: string; children: ReactNode }) {
  return (
    <label className="block">
      <span className="text-sm font-medium text-ink">{label}</span>
      <span className="mt-2 block">{children}</span>
    </label>
  );
}

function InfoGrid({ items }: { items: [string, string][] }) {
  return (
    <div className="mt-4 grid gap-3 text-sm sm:grid-cols-2">
      {items.map(([label, value]) => (
        <div key={label}>
          <div className="text-xs font-semibold uppercase tracking-wide text-muted">{label}</div>
          <div className="mt-1 break-words font-medium text-ink">{value}</div>
        </div>
      ))}
    </div>
  );
}

function serviceMetrics(services: POSService[]) {
  return {
    total: services.length,
    synced: services.filter((service) => service.sync_status === "synced" && service.pos_linked).length,
    localOnly: services.filter((service) => service.sync_status === "local_only").length,
    aiBookable: services.filter((service) => service.ai_bookable && canEnableAI(service)).length,
    categorized: services.filter((service) => Boolean(service.service_category_id)).length,
    suggested: services.filter((service) => service.category_source === "suggested").length
  };
}

function emptyServiceForm(): ServiceFormState {
  return {
    name: "",
    description: "",
    aiDescription: "",
    durationMinutes: "45",
    priceFrom: "",
    serviceCategoryID: "",
    active: true
  };
}

function serviceToForm(service: POSService): ServiceFormState {
  return {
    name: service.name,
    description: service.description ?? "",
    aiDescription: service.ai_description ?? "",
    durationMinutes: service.duration_minutes > 0 ? String(service.duration_minutes) : "",
    priceFrom: service.price_from ? String(service.price_from) : "",
    serviceCategoryID: service.service_category_id ?? "",
    active: service.active
  };
}

function servicePayload(form: ServiceFormState) {
  const price = form.priceFrom.trim() === "" ? null : Number(form.priceFrom);
  return {
    name: form.name,
    description: form.description,
    ai_description: form.aiDescription,
    duration_minutes: Number(form.durationMinutes),
    price_from: Number.isFinite(price) ? price : null,
    service_category_id: form.serviceCategoryID,
    active: form.active
  };
}

function emptyCategoryForm(): ServiceCategoryFormState {
  return {
    name: "",
    description: "",
    sortOrder: "0"
  };
}

function categoryToForm(category: POSServiceCategory): ServiceCategoryFormState {
  return {
    name: category.name,
    description: category.description ?? "",
    sortOrder: String(category.sort_order)
  };
}

function categoryPayload(form: ServiceCategoryFormState) {
  return {
    name: form.name,
    description: form.description,
    sort_order: Number(form.sortOrder)
  };
}

function upsertService(items: POSService[], service: POSService) {
  const exists = items.some((item) => item.id === service.id);
  const next = exists ? items.map((item) => (item.id === service.id ? service : item)) : [service, ...items];
  return next.sort(compareServices);
}

function upsertCategory(items: POSServiceCategory[], category: POSServiceCategory) {
  const exists = items.some((item) => item.id === category.id);
  const next = exists ? items.map((item) => (item.id === category.id ? category : item)) : [category, ...items];
  return sortCategories(next);
}

function sortCategories(items: POSServiceCategory[]) {
  return [...items].sort((a, b) => {
    if (a.status !== b.status) return a.status === "active" ? -1 : 1;
    if (a.sort_order !== b.sort_order) return a.sort_order - b.sort_order;
    return a.name.localeCompare(b.name);
  });
}

function filterServices(services: POSService[], categoryFilter: string, reviewFilter: CategoryReviewFilter) {
  return services.filter((service) => {
    if (categoryFilter === "unassigned" && service.service_category_id) return false;
    if (categoryFilter !== "all" && categoryFilter !== "unassigned" && service.service_category_id !== categoryFilter) return false;
    if (reviewFilter !== "all" && (service.category_source || "unassigned") !== reviewFilter) return false;
    return true;
  });
}

function refreshSummary(refresh: ServiceCategorySuggestionRefresh) {
  return [
    `Category suggestions refreshed: ${refresh.suggested_services} service suggestion${refresh.suggested_services === 1 ? "" : "s"}.`,
    `${refresh.created_categories} categor${refresh.created_categories === 1 ? "y" : "ies"} created, ${refresh.created_aliases} alias${refresh.created_aliases === 1 ? "" : "es"} created.`,
    refresh.skipped_alias_conflicts > 0 ? `${refresh.skipped_alias_conflicts} alias conflict${refresh.skipped_alias_conflicts === 1 ? "" : "s"} skipped.` : ""
  ]
    .filter(Boolean)
    .join(" ");
}

function compareServices(a: POSService, b: POSService) {
  const archivedA = a.archived_at ? 1 : 0;
  const archivedB = b.archived_at ? 1 : 0;
  if (archivedA !== archivedB) return archivedA - archivedB;
  if (a.active !== b.active) return a.active ? -1 : 1;
  return a.name.localeCompare(b.name);
}

function canEnableAI(service: POSService) {
  return (
    service.active &&
    !service.archived_at &&
    service.sync_status === "synced" &&
    service.pos_linked &&
    Boolean(service.pos_service_id) &&
    Boolean(service.pos_service_version) &&
    service.duration_minutes > 0
  );
}

function serviceGateReason(service: POSService) {
  if (service.archived_at || service.sync_status === "archived") return "Archived services stay visible for history and are not bookable.";
  if (!service.active) return "Inactive services are not bookable by the AI receptionist.";
  if (!service.pos_linked || service.sync_status === "local_only") return "Local-only services need a Square Appointments link before they are booking-ready.";
  if (service.sync_status === "sync_failed") return service.sync_error || "Latest POS sync failed; service is not bookable.";
  if (service.sync_status === "unmapped") return "Service needs an active-provider mapping before it is bookable.";
  if (!service.pos_service_version) return "Square booking metadata is incomplete.";
  if (service.ai_bookable) return "Booking-ready: synced, POS linked, and allowed for AI booking.";
  return "Synced service can be allowed for AI booking.";
}

function formatPrice(value?: number) {
  if (!value) return "Not set";
  return `$${value.toFixed(2)}`;
}
