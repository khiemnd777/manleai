"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import type { ReactNode } from "react";
import { AlertTriangle, Archive, Check, Pencil, Plus, RefreshCcw, RotateCcw, Settings2, Tags, XCircle } from "lucide-react";
import { Alert } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardDescription, CardTitle } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { SchedulingReadinessCard } from "@/features/dashboard/scheduling-readiness-card";
import {
  authorityLabel,
  FieldAuthorityBadge,
  FieldAuthorityPanel,
  operationalFieldsEditable,
  providerManagedReadOnly
} from "@/features/dashboard/pos-field-authority";
import { apiRequest } from "@/lib/api/client";
import {
  getManleAICalendar,
  isManleAICalendarVersionConflict,
  newManleAICalendarActionKey,
  updateManleAICalendarServicePolicy
} from "@/lib/api/internal-calendar";
import type {
  ManleAICalendarAggregate,
  ManleAICalendarCapacityMode,
  ManleAICalendarMutationResponse,
  ManleAICalendarResourceRequirementInput,
  ManleAICalendarServicePolicy,
  POSConnection,
  POSService,
  POSServiceCategory,
  POSServiceCategoryAlias,
  Salon,
  SchedulingAuthority,
  ServiceAlias,
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

type ServiceAliasesResponse = {
  service_aliases: ServiceAlias[];
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
  consultationStatus: "draft" | "ready" | "disabled";
  recommendedOutcomes: string[];
  compatibleCurrentSystems: string[];
  lengthCapabilities: string[];
  priorityTags: string[];
  finishOptions: string[];
  maintenanceNote: string;
};

type ServiceCategoryFormState = {
  name: string;
  description: string;
  sortOrder: string;
};

type CategoryReviewFilter = "all" | "unassigned" | "suggested" | "manual";

const categoryReviewFilters: CategoryReviewFilter[] = ["all", "unassigned", "suggested", "manual"];

const consultationOptionGroups = {
  recommendedOutcomes: [
    ["maintain", "Maintain current set"], ["shorten", "Shorten"], ["add_length", "Add length"],
    ["add_strength", "Add strength"], ["repair", "Repair"], ["removal", "Removal"], ["color_refresh", "Color refresh"]
  ],
  compatibleCurrentSystems: [
    ["natural", "Natural nails"], ["regular_polish", "Regular polish"], ["gel", "Gel"],
    ["dip", "Dip"], ["acrylic", "Acrylic"], ["extension", "Extensions"]
  ],
  lengthCapabilities: [["keep", "Keep length"], ["shorten", "Shorten"], ["add_length", "Add length"]],
  priorityTags: [
    ["durability", "Durability"], ["lower_maintenance", "Lower maintenance"],
    ["lower_cost", "Lower cost"], ["shorter_visit", "Shorter visit"]
  ],
  finishOptions: [
    ["natural", "Natural"], ["regular_polish", "Regular polish"], ["gel_polish", "Gel polish"],
    ["glossy", "Glossy"], ["matte", "Matte"], ["nail_art", "Nail art"]
  ]
} satisfies Record<string, Array<[string, string]>>;

export function ServicesDashboard() {
  const [salon, setSalon] = useState<Salon | null>(null);
  const [status, setStatus] = useState<StatusResponse | null>(null);
  const [squareStatusError, setSquareStatusError] = useState("");
  const [calendar, setCalendar] = useState<ManleAICalendarAggregate | null>(null);
  const [calendarLoading, setCalendarLoading] = useState(true);
  const [calendarError, setCalendarError] = useState("");
  const [services, setServices] = useState<POSService[]>([]);
  const [categories, setCategories] = useState<POSServiceCategory[]>([]);
  const [serviceAliases, setServiceAliases] = useState<ServiceAlias[]>([]);
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
  const [serviceAliasDraftServiceID, setServiceAliasDraftServiceID] = useState("");
  const [serviceAliasDraft, setServiceAliasDraft] = useState("");
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
        setSquareStatusError("");
        setCalendar(null);
        setCalendarError("");
        setCalendarLoading(false);
        setServices([]);
        setCategories([]);
        setServiceAliases([]);
        return;
      }
      setCalendarLoading(true);
      const [statusResult, serviceResponse, categoryResponse, aliasResponse, calendarResult] = await Promise.all([
        apiRequest<StatusResponse>(`/api/integrations/square/status?salon_id=${firstSalon.id}`)
          .then((value) => ({ value, error: "" }))
          .catch((statusError: unknown) => ({ value: null, error: errorMessage(statusError, "Could not load Square status.") })),
        apiRequest<ServicesResponse>(`/api/salons/${firstSalon.id}/services`),
        apiRequest<ServiceCategoriesResponse>(`/api/salons/${firstSalon.id}/service-categories`),
        apiRequest<ServiceAliasesResponse>(`/api/salons/${firstSalon.id}/service-aliases`),
        getManleAICalendar(firstSalon.id)
          .then((response) => ({ value: response.manleai_calendar, error: "" }))
          .catch((calendarFailure: unknown) => ({ value: null, error: errorMessage(calendarFailure, "Could not load internal calendar readiness.") }))
      ]);
      setStatus(statusResult.value);
      setSquareStatusError(statusResult.error);
      setCalendar(calendarResult.value);
      setCalendarError(calendarResult.error);
      setCalendarLoading(false);
      setServices(serviceResponse.services);
      setCategories(sortCategories(categoryResponse.service_categories));
      setServiceAliases(aliasResponse.service_aliases);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Could not load services.");
    } finally {
      setCalendarLoading(false);
      if (!silent) {
        setLoading(false);
      }
    }
  }

  useEffect(() => {
    void load();
  }, []);

  async function reloadCalendar() {
    if (!salon?.id) return;
    setCalendarLoading(true);
    setCalendarError("");
    try {
      const response = await getManleAICalendar(salon.id);
      setCalendar(response.manleai_calendar);
    } catch (calendarFailure) {
      setCalendarError(errorMessage(calendarFailure, "Could not load internal calendar readiness."));
    } finally {
      setCalendarLoading(false);
    }
  }

  const schedulingAuthority = calendar?.scheduling_authority;
  const activeProvider = salon?.active_pos_provider;
  const metrics = useMemo(() => serviceMetrics(services, schedulingAuthority, activeProvider), [services, schedulingAuthority, activeProvider]);
  const activeCategories = useMemo(() => categories.filter((category) => category.status === "active"), [categories]);
  const filteredServices = useMemo(
    () => filterServices(services, categoryFilter, reviewFilter),
    [services, categoryFilter, reviewFilter]
  );
  const serviceAliasesByServiceID = useMemo(() => groupServiceAliasesByServiceID(serviceAliases), [serviceAliases]);
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
    if (form.consultationStatus === "ready" && !consultationProfileHasStructuredValue(form)) {
      setError("A ready consultation profile needs at least one approved outcome, current system, length capability, priority, or finish option.");
      setSuccess("");
      return;
    }
    setBusy("save-service");
    setError("");
    setSuccess("");
    try {
      const ownerControlsOnly = Boolean(editingService && !operationalFieldsEditable(editingService.field_authority));
      const response = editingService?.id
        ? await apiRequest<ServiceResponse>(
            ownerControlsOnly
              ? `/api/salons/${salon.id}/services/${editingService.id}/owner-controls`
              : `/api/salons/${salon.id}/services/${editingService.id}`,
            {
              method: ownerControlsOnly ? "PATCH" : "PUT",
              body: JSON.stringify(ownerControlsOnly ? serviceOwnerControlsPayload(form) : servicePayload(form))
            }
          )
        : await apiRequest<ServiceResponse>(`/api/salons/${salon.id}/services`, {
            method: "POST",
            body: JSON.stringify(servicePayload(form))
          });
      setServices((current) => upsertService(current, response.service));
      setSuccess(
        editingService
          ? ownerControlsOnly
            ? "ManleAI controls saved. Provider-managed details were unchanged."
            : "Service saved."
          : "Service created. Scheduling eligibility follows the selected authority and its backend readiness checks."
      );
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

  async function addServiceAlias(service: POSService) {
    if (!salon || !service.id || !serviceAliasDraft.trim()) return;
    setBusy(`service-alias-${service.id}`);
    setError("");
    setSuccess("");
    try {
      await apiRequest<ServiceAlias>(`/api/salons/${salon.id}/service-aliases`, {
        method: "POST",
        body: JSON.stringify({
          service_id: service.id,
          alias: serviceAliasDraft
        })
      });
      setServiceAliasDraft("");
      setServiceAliasDraftServiceID("");
      setSuccess("Service alias saved.");
      await load({ silent: true });
    } catch (err) {
      setError(err instanceof Error ? err.message : "Could not save service alias.");
    } finally {
      setBusy("");
    }
  }

  async function archiveServiceAlias(alias: ServiceAlias) {
    if (!salon || alias.status === "archived") return;
    setBusy(`service-alias-archive-${alias.id}`);
    setError("");
    setSuccess("");
    try {
      await apiRequest<ServiceAlias>(`/api/salons/${salon.id}/service-aliases`, {
        method: "POST",
        body: JSON.stringify({
          service_id: alias.service_id,
          alias: alias.alias,
          source: alias.source,
          status: "archived",
          confidence: alias.confidence
        })
      });
      setSuccess("Service alias archived.");
      await load({ silent: true });
    } catch (err) {
      setError(err instanceof Error ? err.message : "Could not archive service alias.");
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
    if (!window.confirm(`Archive ${service.name} in ManleAI? This disables AI booking and keeps the service visible for history. It does not remove the service from the POS provider.`)) return;
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
      setSuccess("Service archived in ManleAI. It will not be used for new availability checks or bookings; the POS provider was not changed.");
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
            Manage services, internal scheduling policy, shared capacity, and optional Square Appointments sync.
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
      {squareStatusError ? <Alert title="Square status unavailable" message={`${squareStatusError} Internal services and calendar setup remain available.`} /> : null}

      <SchedulingReadinessCard calendar={calendar} loading={calendarLoading} error={calendarError} onRetry={() => void reloadCalendar()} />
      {calendar?.scheduling_authority === "external_provider" ? (
        <>
          <ServicesGate status={status} />
          <BookingEligibilityPanel />
        </>
      ) : null}

      <div className="grid gap-4 md:grid-cols-3 xl:grid-cols-6">
        <Metric label="Total services" value={String(metrics.total)} />
        <Metric label="Synced" value={String(metrics.synced)} />
        <Metric label="Local only" value={String(metrics.localOnly)} />
        <Metric label="Authority eligible" value={String(metrics.aiBookable)} />
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
          salonID={salon.id}
          calendar={calendar}
          calendarLoading={calendarLoading}
          calendarError={calendarError}
          activeProvider={activeProvider}
          busy={busy === "save-service"}
          onChange={setForm}
          onCancel={() => {
            setFormOpen(false);
            setEditingService(null);
            setForm(emptyServiceForm());
          }}
          onSave={() => void saveService()}
          onReloadCalendar={reloadCalendar}
          onCalendarChange={setCalendar}
        />
      ) : null}

      <Card>
        <div className="flex flex-col justify-between gap-3 sm:flex-row sm:items-start">
          <div>
            <CardTitle>Service catalog</CardTitle>
            <CardDescription>
              {serviceCatalogDescription(schedulingAuthority)}
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
            schedulingAuthority={schedulingAuthority}
            activeProvider={activeProvider}
            categories={activeCategories}
            serviceAliasesByServiceID={serviceAliasesByServiceID}
            busy={busy}
            serviceAliasDraftServiceID={serviceAliasDraftServiceID}
            serviceAliasDraft={serviceAliasDraft}
            onEdit={openEditForm}
            onArchive={(service) => void archiveService(service)}
            onUpdateAI={(service, nextValue) => void updateAIBookable(service, nextValue)}
            onAssignCategory={(service, categoryID) => void assignServiceCategory(service, categoryID)}
            onServiceAliasDraftServiceChange={setServiceAliasDraftServiceID}
            onServiceAliasDraftChange={setServiceAliasDraft}
            onAddServiceAlias={(service) => void addServiceAlias(service)}
            onArchiveServiceAlias={(alias) => void archiveServiceAlias(alias)}
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
  salonID,
  calendar,
  calendarLoading,
  calendarError,
  activeProvider,
  busy,
  onChange,
  onCancel,
  onSave,
  onReloadCalendar,
  onCalendarChange
}: {
  form: ServiceFormState;
  service: POSService | null;
  categories: POSServiceCategory[];
  salonID: string;
  calendar: ManleAICalendarAggregate | null;
  calendarLoading: boolean;
  calendarError: string;
  activeProvider?: string;
  busy: boolean;
  onChange: (next: ServiceFormState) => void;
  onCancel: () => void;
  onSave: () => void;
  onReloadCalendar: () => Promise<void>;
  onCalendarChange: (calendar: ManleAICalendarAggregate) => void;
}) {
  const archived = Boolean(service?.archived_at);
  const providerReadOnly = Boolean(service && providerManagedReadOnly(service.field_authority));
  const operationalEditable = !service || operationalFieldsEditable(service.field_authority);
  const ownerControlsOnly = Boolean(service && !operationalEditable);
  const consultationGated = archived || !service?.pos_linked;
  const consultationReadyIncomplete = form.consultationStatus === "ready" && !consultationProfileHasStructuredValue(form);
  return (
    <Card>
      <div className="flex flex-col justify-between gap-3 sm:flex-row sm:items-start">
        <div>
          <CardTitle>{service ? (ownerControlsOnly ? "Manage service" : "Edit service") : "New local service"}</CardTitle>
          <CardDescription>
            {service ? serviceGateReason(service, calendar?.scheduling_authority, activeProvider) : "New services start as canonical ManleAI records. Eligibility follows the selected scheduling authority."}
          </CardDescription>
        </div>
        <div className="flex flex-wrap gap-2">
          {service ? <Badge value={service.sync_status || "local_only"} /> : <Badge value="local_only" />}
          <Badge value={form.consultationStatus} />
        </div>
      </div>

      {service ? (
        <FieldAuthorityPanel
          authority={service.field_authority}
          recordKind="service"
          syncStatus={service.sync_status}
          lastSyncedAt={service.last_synced_at}
          syncError={service.sync_error}
        />
      ) : (
        <div className="mt-5 rounded-md border border-blue-200 bg-blue-50 px-4 py-3 text-xs leading-5 text-blue-900">
          {localServiceRecordDescription(calendar?.scheduling_authority)}
        </div>
      )}

      <div className="flex flex-col">
      <div className="order-2 mt-5 rounded-md border border-line bg-slate-50 p-4">
        <div className="flex flex-col justify-between gap-3 sm:flex-row sm:items-start">
          <div>
            <div className="text-sm font-semibold text-ink">ManleAI controls · AI consultation profile</div>
            <p className="mt-1 text-xs leading-5 text-muted">
              Structured, owner-approved facts used for service recommendations. This profile never confirms or creates an appointment.
            </p>
          </div>
          <select
            className="h-10 rounded-md border border-line bg-white px-3 text-sm text-ink outline-none focus:border-brand disabled:bg-slate-100 disabled:text-slate-400"
            value={form.consultationStatus}
            onChange={(event) => onChange({ ...form, consultationStatus: event.target.value as ServiceFormState["consultationStatus"] })}
            disabled={busy || consultationGated}
            aria-label="Consultation profile status"
          >
            <option value="draft">Draft</option>
            <option value="ready">Ready for consultation</option>
            <option value="disabled">Disabled</option>
          </select>
        </div>

        <div className="mt-4 grid gap-4 lg:grid-cols-2">
          <Field label="Category">
            <select
              className="h-10 w-full rounded-md border border-line bg-white px-3 text-sm text-ink outline-none focus:border-brand disabled:bg-slate-100 disabled:text-slate-400"
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
          <Field label="AI receptionist consultation summary">
            <div className="space-y-2">
              <textarea
                className="min-h-24 w-full rounded-md border border-line bg-white px-3 py-2 text-sm text-ink outline-none focus:border-brand disabled:bg-slate-100 disabled:text-slate-400"
                value={form.aiDescription}
                maxLength={320}
                aria-describedby="ai-consultation-summary-help"
                onChange={(event) => onChange({ ...form, aiDescription: event.target.value })}
                disabled={busy || archived}
              />
              <div id="ai-consultation-summary-help" className="flex flex-wrap items-start justify-between gap-2 text-xs text-muted">
                <span>Approved facts the receptionist may use when helping callers compare services. Avoid medical advice or unverified claims.</span>
                <span className="shrink-0 tabular-nums" aria-live="polite">
                  {form.aiDescription.length}/320
                </span>
              </div>
              <p className="text-xs font-medium text-ink">
                {form.aiDescription.trim()
                  ? "Consultation facts ready"
                  : form.description.trim()
                    ? "Using the provider standard description until a consultation summary is added"
                    : "No consultation description; the AI will use only name, category, duration, and price"}
              </p>
            </div>
          </Field>
        </div>

        {consultationGated ? (
          <div className="mt-4 rounded-md border border-amber-200 bg-amber-50 px-3 py-2 text-xs leading-5 text-amber-900">
            Consultation controls become available after this active service is linked to the salon's current POS provider.
          </div>
        ) : null}
        {consultationReadyIncomplete ? (
          <div className="mt-4 rounded-md border border-red-200 bg-red-50 px-3 py-2 text-xs leading-5 text-red-800">
            Select at least one structured consultation value before marking this profile ready.
          </div>
        ) : null}

        <div className="mt-4 grid gap-4 lg:grid-cols-2">
          <ConsultationOptionGroup
            label="Recommended outcomes"
            options={consultationOptionGroups.recommendedOutcomes}
            selected={form.recommendedOutcomes}
            disabled={busy || consultationGated}
            onChange={(recommendedOutcomes) => onChange({ ...form, recommendedOutcomes })}
          />
          <ConsultationOptionGroup
            label="Compatible current systems"
            options={consultationOptionGroups.compatibleCurrentSystems}
            selected={form.compatibleCurrentSystems}
            disabled={busy || consultationGated}
            onChange={(compatibleCurrentSystems) => onChange({ ...form, compatibleCurrentSystems })}
          />
          <ConsultationOptionGroup
            label="Length capabilities"
            options={consultationOptionGroups.lengthCapabilities}
            selected={form.lengthCapabilities}
            disabled={busy || consultationGated}
            onChange={(lengthCapabilities) => onChange({ ...form, lengthCapabilities })}
          />
          <ConsultationOptionGroup
            label="Caller priorities"
            options={consultationOptionGroups.priorityTags}
            selected={form.priorityTags}
            disabled={busy || consultationGated}
            onChange={(priorityTags) => onChange({ ...form, priorityTags })}
          />
          <ConsultationOptionGroup
            label="Finish options"
            options={consultationOptionGroups.finishOptions}
            selected={form.finishOptions}
            disabled={busy || consultationGated}
            onChange={(finishOptions) => onChange({ ...form, finishOptions })}
          />
          <Field label="Maintenance note">
            <textarea
              className="min-h-24 w-full rounded-md border border-line bg-white px-3 py-2 text-sm text-ink outline-none focus:border-brand disabled:bg-slate-100 disabled:text-slate-400"
              value={form.maintenanceNote}
              maxLength={320}
              onChange={(event) => onChange({ ...form, maintenanceNote: event.target.value })}
              disabled={busy || consultationGated}
              placeholder="Owner-approved upkeep or return-visit guidance"
            />
          </Field>
        </div>
      </div>

        <div className="order-1 mt-5 rounded-md border border-line p-4">
          <div className="text-sm font-semibold text-ink">Operational details</div>
          <p className="mt-1 text-xs leading-5 text-muted">
            {providerReadOnly
              ? `Read-only values imported from ${authorityLabel(service?.field_authority)}.`
              : "Values managed in ManleAI. Provider synchronization runs only when the active adapter supports these writes."}
          </p>
          <div className="mt-4 grid gap-4 md:grid-cols-2">
            <Field label="Name">
              <input
                className="h-10 w-full rounded-md border border-line px-3 text-sm text-ink outline-none focus:border-brand disabled:bg-slate-100 disabled:text-slate-500"
                value={form.name}
                onChange={(event) => onChange({ ...form, name: event.target.value })}
                disabled={busy || archived || !operationalEditable}
              />
            </Field>
            <Field label="Duration minutes">
              <input
                className="h-10 w-full rounded-md border border-line px-3 text-sm text-ink outline-none focus:border-brand disabled:bg-slate-100 disabled:text-slate-500"
                type="number"
                min="1"
                value={form.durationMinutes}
                onChange={(event) => onChange({ ...form, durationMinutes: event.target.value })}
                disabled={busy || archived || !operationalEditable}
              />
            </Field>
            <Field label="Price from">
              <input
                className="h-10 w-full rounded-md border border-line px-3 text-sm text-ink outline-none focus:border-brand disabled:bg-slate-100 disabled:text-slate-500"
                type="number"
                min="0"
                step="0.01"
                value={form.priceFrom}
                onChange={(event) => onChange({ ...form, priceFrom: event.target.value })}
                disabled={busy || archived || !operationalEditable}
              />
            </Field>
            <label className="flex items-center gap-3 rounded-md border border-line px-3 py-2 text-sm font-medium text-ink">
              <input
                type="checkbox"
                checked={form.active}
                onChange={(event) => onChange({ ...form, active: event.target.checked })}
                disabled={busy || archived || !operationalEditable}
              />
              Active
            </label>
            <Field label="Standard description">
              <textarea
                className="min-h-24 w-full rounded-md border border-line px-3 py-2 text-sm text-ink outline-none focus:border-brand disabled:bg-slate-100 disabled:text-slate-500"
                value={form.description}
                onChange={(event) => onChange({ ...form, description: event.target.value })}
                disabled={busy || archived || !operationalEditable}
              />
            </Field>
          </div>
        </div>
      </div>

      <ServiceCalendarPolicyEditor
        salonID={salonID}
        service={service}
        calendar={calendar}
        loading={calendarLoading}
        error={calendarError}
        onReload={onReloadCalendar}
        onCalendarChange={onCalendarChange}
      />

      <div className="mt-5 flex flex-wrap justify-end gap-3">
        <Button type="button" variant="secondary" onClick={onCancel} disabled={busy}>
          Cancel
        </Button>
        <Button type="button" onClick={onSave} disabled={busy || archived || consultationReadyIncomplete}>
          {busy ? "Saving..." : ownerControlsOnly ? "Save ManleAI controls" : service ? "Save service" : "Save local service"}
        </Button>
      </div>
    </Card>
  );
}

type ServicePolicyFormState = {
  enabled: boolean;
  capacityMode: "" | ManleAICalendarCapacityMode;
  bufferBefore: string;
  bufferAfter: string;
  requirements: Array<{ key: string; resourcePoolID: string; unitsRequired: string }>;
};

function ServiceCalendarPolicyEditor({
  salonID,
  service,
  calendar,
  loading,
  error: loadError,
  onReload,
  onCalendarChange
}: {
  salonID: string;
  service: POSService | null;
  calendar: ManleAICalendarAggregate | null;
  loading: boolean;
  error: string;
  onReload: () => Promise<void>;
  onCalendarChange: (calendar: ManleAICalendarAggregate) => void;
}) {
  const actionKeyRef = useRef("");
  const [form, setForm] = useState<ServicePolicyFormState>(emptyServicePolicyForm());
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [success, setSuccess] = useState("");
  const policy = service?.id
    ? calendar?.service_policies.find((item) => item.service.id === service.id) ?? null
    : null;

  useEffect(() => {
    setForm(servicePolicyToForm(policy));
    setError("");
    setSuccess("");
    actionKeyRef.current = "";
  }, [calendar?.config_version, policy, service?.id]);

  if (!service?.id) {
    return (
      <div className="mt-5 rounded-md border border-blue-200 bg-blue-50 p-4 text-sm leading-6 text-blue-900">
        Save this service first. Internal capacity, buffers, and resource requirements belong to the saved service record.
      </div>
    );
  }

  if (loading) {
    return <Skeleton className="mt-5 h-64" />;
  }

  if (loadError || !calendar) {
    return (
      <div className="mt-5 rounded-md border border-line p-4">
        <Alert title="Internal service policy unavailable" message={loadError || "Could not load this service's internal scheduling policy."} />
        <Button type="button" variant="secondary" className="mt-3" onClick={() => void onReload()}>
          Retry service policy
        </Button>
      </div>
    );
  }

  const calendarData = calendar;
  const serviceID = service.id;
  const baseDisabled = busy || !calendar.config;
  const serviceUnavailable = Boolean(service.archived_at) || !service.active;
  const editDisabled = baseDisabled || serviceUnavailable;
  const blockers = calendar.readiness.blockers.filter(
    (blocker) => blocker.scope === "service" && blocker.entity_id === serviceID
  );

  function updateForm(next: ServicePolicyFormState) {
    actionKeyRef.current = "";
    setForm(next);
  }

  function addRequirement() {
    updateForm({
      ...form,
      requirements: [
        ...form.requirements,
        { key: newManleAICalendarActionKey(), resourcePoolID: "", unitsRequired: "" }
      ]
    });
  }

  function updateRequirement(key: string, patch: Partial<ServicePolicyFormState["requirements"][number]>) {
    updateForm({
      ...form,
      requirements: form.requirements.map((requirement) =>
        requirement.key === key ? { ...requirement, ...patch } : requirement
      )
    });
  }

  function removeRequirement(key: string) {
    updateForm({ ...form, requirements: form.requirements.filter((requirement) => requirement.key !== key) });
  }

  async function savePolicy() {
    const requirements: ManleAICalendarResourceRequirementInput[] = [];
    if (form.capacityMode === "pooled") {
      for (const requirement of form.requirements) {
        if (!requirement.resourcePoolID || !requirement.unitsRequired.trim()) {
          setError("Choose a resource and units required for every pooled-capacity requirement.");
          return;
        }
        requirements.push({
          resource_pool_id: requirement.resourcePoolID,
          units_required: Number(requirement.unitsRequired)
        });
      }
    }

    setBusy(true);
    setError("");
    setSuccess("");
    if (!actionKeyRef.current) actionKeyRef.current = newManleAICalendarActionKey();
    try {
      const response = await updateManleAICalendarServicePolicy(salonID, serviceID, {
        action_key: actionKeyRef.current,
        expected_config_version: calendarData.config_version,
        enabled: form.enabled,
        capacity_mode: form.capacityMode || null,
        buffer_before_minutes_override: nullableNumber(form.bufferBefore),
        buffer_after_minutes_override: nullableNumber(form.bufferAfter),
        eligible_staff_ids: policy?.eligible_staff.map((staff) => staff.id) ?? [],
        resource_requirements: requirements
      });
      actionKeyRef.current = "";
      onCalendarChange(response.manleai_calendar);
      setSuccess(response.replayed ? "Service policy saved by replaying the previous safe retry." : "Internal service policy saved.");
    } catch (mutationFailure) {
      if (isManleAICalendarVersionConflict(mutationFailure)) {
        actionKeyRef.current = "";
        await onReload();
        setError("Calendar configuration changed in another session. The latest service policy was loaded; review it before saving again.");
      } else {
        setError(mutationFailure instanceof Error ? mutationFailure.message : "Could not save the internal service policy.");
      }
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="mt-5 rounded-md border border-line bg-slate-50 p-4">
      <div className="flex flex-col justify-between gap-3 sm:flex-row sm:items-start">
        <div>
          <div className="text-sm font-semibold text-ink">ManleAI controls · internal scheduling policy</div>
          <p className="mt-1 text-xs leading-5 text-muted">
            Enable internal scheduling, choose capacity ownership, override buffers, and attach shared resources for this service.
          </p>
        </div>
        <div className="flex flex-wrap gap-2">
          <Badge value={policy?.configured ? "configured" : "required"} />
          {blockers.length > 0 ? <Badge value="blocked" /> : null}
        </div>
      </div>

      {!calendar.config ? (
        <div className="mt-4 rounded-md border border-amber-200 bg-amber-50 p-3 text-sm leading-6 text-amber-900">
          Save the salon's ManleAI Calendar policy in Settings before configuring this service.
        </div>
      ) : null}
      {service.archived_at || !service.active ? (
        <div className="mt-4 rounded-md border border-amber-200 bg-amber-50 p-3 text-sm leading-6 text-amber-900">
          This service is inactive or archived. Cleanup is still allowed: disable the internal policy or remove existing resource requirements. Re-enabling the policy and adding new requirements are blocked.
        </div>
      ) : null}
      {blockers.length > 0 ? (
        <div className="mt-4 rounded-md border border-line bg-white p-3 text-sm leading-6 text-muted">
          {blockers.map((blocker) => <div key={`${blocker.code}-${blocker.entity_id}`}>{blocker.message}</div>)}
        </div>
      ) : null}
      {error ? <div className="mt-4"><Alert title="Service policy not updated" message={error} /></div> : null}
      {success ? <div className="mt-4"><Alert type="success" title="Service policy updated" message={success} /></div> : null}

      <div className="mt-5 grid gap-4 md:grid-cols-2 xl:grid-cols-4">
        <label className="flex items-center gap-3 rounded-md border border-line bg-white px-3 py-2 text-sm font-medium text-ink">
          <input type="checkbox" checked={form.enabled} onChange={(event) => updateForm({ ...form, enabled: event.target.checked })} disabled={baseDisabled || (serviceUnavailable && !form.enabled)} />
          Enabled for internal scheduling
        </label>
        <Field label="Capacity mode">
          <select value={form.capacityMode} onChange={(event) => updateForm({ ...form, capacityMode: event.target.value as ServicePolicyFormState["capacityMode"] })} disabled={editDisabled} className="h-10 w-full rounded-md border border-line bg-white px-3 text-sm text-ink outline-none focus:border-brand disabled:bg-slate-100">
            <option value="">Choose capacity mode</option>
            {calendar.constraints.capacity_modes.map((mode) => <option key={mode} value={mode}>{mode.replaceAll("_", " ")}</option>)}
          </select>
        </Field>
        <PolicyNumberField label="Buffer before override" value={form.bufferBefore} constraint={calendar.constraints.buffer_minutes} disabled={editDisabled} onChange={(value) => updateForm({ ...form, bufferBefore: value })} />
        <PolicyNumberField label="Buffer after override" value={form.bufferAfter} constraint={calendar.constraints.buffer_minutes} disabled={editDisabled} onChange={(value) => updateForm({ ...form, bufferAfter: value })} />
      </div>

      <div className="mt-5 rounded-md border border-line bg-white p-4">
        <div className="flex flex-col justify-between gap-3 sm:flex-row sm:items-start">
          <div>
            <div className="text-sm font-semibold text-ink">Shared resource requirements</div>
            <div className="mt-1 text-xs leading-5 text-muted">
              Required only for pooled capacity. Staff eligibility remains managed inside each Staff record.
            </div>
          </div>
          <Button type="button" variant="secondary" onClick={addRequirement} disabled={editDisabled || form.capacityMode !== "pooled"}>
            <Plus className="h-4 w-4" /> Add resource
          </Button>
        </div>
        {form.capacityMode === "pooled" && calendar.readiness.capabilities?.pooled_capacity !== true ? (
          <div className="mt-3 rounded-md border border-amber-200 bg-amber-50 p-3 text-sm leading-6 text-amber-900">
            Pooled policy can be configured now, but backend execution readiness remains blocked until the pooled-capacity engine is available.
          </div>
        ) : null}
        {form.requirements.length === 0 ? (
          <div className="mt-3 text-sm text-muted">No shared resource requirements.</div>
        ) : (
          <div className="mt-3 space-y-3">
            {form.requirements.map((requirement) => (
              <div key={requirement.key} className="grid gap-3 sm:grid-cols-[1fr_12rem_auto] sm:items-end">
                <Field label="Resource pool">
                  <select value={requirement.resourcePoolID} onChange={(event) => updateRequirement(requirement.key, { resourcePoolID: event.target.value })} disabled={editDisabled || form.capacityMode !== "pooled"} className="h-10 w-full rounded-md border border-line bg-white px-3 text-sm text-ink outline-none focus:border-brand disabled:bg-slate-100">
                    <option value="">Choose resource</option>
                    {calendar.resources.map((resource) => <option key={resource.id} value={resource.id} disabled={Boolean(resource.archived_at) && requirement.resourcePoolID !== resource.id}>{resource.name} · capacity {resource.capacity}{resource.archived_at ? " · archived" : ""}</option>)}
                  </select>
                </Field>
                <PolicyNumberField label="Units required" value={requirement.unitsRequired} constraint={calendar.constraints.resource_units_required} disabled={editDisabled || form.capacityMode !== "pooled"} onChange={(value) => updateRequirement(requirement.key, { unitsRequired: value })} />
                <Button type="button" variant="danger" onClick={() => removeRequirement(requirement.key)} disabled={baseDisabled}>
                  <XCircle className="h-4 w-4" /> Remove
                </Button>
              </div>
            ))}
          </div>
        )}
      </div>

      <div className="mt-5 flex justify-end">
        <Button type="button" onClick={() => void savePolicy()} disabled={baseDisabled || (serviceUnavailable && form.enabled)}>
          {busy ? "Saving..." : "Save internal policy"}
        </Button>
      </div>
    </div>
  );
}

function PolicyNumberField({ label, value, constraint, disabled, onChange }: {
  label: string;
  value: string;
  constraint: { minimum: number; maximum: number };
  disabled: boolean;
  onChange: (value: string) => void;
}) {
  return (
    <label className="block">
      <span className="text-sm font-medium text-ink">{label}</span>
      <input type="number" min={constraint.minimum} max={constraint.maximum} value={value} onChange={(event) => onChange(event.target.value)} disabled={disabled} className="mt-2 h-10 w-full rounded-md border border-line bg-white px-3 text-sm text-ink outline-none focus:border-brand disabled:bg-slate-100" />
      <span className="mt-1 block text-xs text-muted">Optional · {constraint.minimum}–{constraint.maximum} minutes</span>
    </label>
  );
}

function emptyServicePolicyForm(): ServicePolicyFormState {
  return { enabled: false, capacityMode: "", bufferBefore: "", bufferAfter: "", requirements: [] };
}

function servicePolicyToForm(policy: ManleAICalendarServicePolicy | null): ServicePolicyFormState {
  if (!policy) return emptyServicePolicyForm();
  return {
    enabled: policy.enabled,
    capacityMode: policy.capacity_mode ?? "",
    bufferBefore: policy.buffer_before_minutes_override === null ? "" : String(policy.buffer_before_minutes_override),
    bufferAfter: policy.buffer_after_minutes_override === null ? "" : String(policy.buffer_after_minutes_override),
    requirements: policy.resource_requirements.map((requirement) => ({
      key: requirement.resource_pool_id,
      resourcePoolID: requirement.resource_pool_id,
      unitsRequired: String(requirement.units_required)
    }))
  };
}

function nullableNumber(value: string) {
  return value.trim() === "" ? null : Number(value);
}

function ServicesTable({
  services,
  schedulingAuthority,
  activeProvider,
  categories,
  serviceAliasesByServiceID,
  busy,
  serviceAliasDraftServiceID,
  serviceAliasDraft,
  onEdit,
  onArchive,
  onUpdateAI,
  onAssignCategory,
  onServiceAliasDraftServiceChange,
  onServiceAliasDraftChange,
  onAddServiceAlias,
  onArchiveServiceAlias
}: {
  services: POSService[];
  schedulingAuthority?: SchedulingAuthority;
  activeProvider?: string;
  categories: POSServiceCategory[];
  serviceAliasesByServiceID: Map<string, ServiceAlias[]>;
  busy: string;
  serviceAliasDraftServiceID: string;
  serviceAliasDraft: string;
  onEdit: (service: POSService) => void;
  onArchive: (service: POSService) => void;
  onUpdateAI: (service: POSService, nextValue: boolean) => void;
  onAssignCategory: (service: POSService, categoryID: string) => void;
  onServiceAliasDraftServiceChange: (serviceID: string) => void;
  onServiceAliasDraftChange: (value: string) => void;
  onAddServiceAlias: (service: POSService) => void;
  onArchiveServiceAlias: (alias: ServiceAlias) => void;
}) {
  return (
    <>
      <div className="mt-5 hidden overflow-x-auto rounded-md border border-line lg:block">
        <table className="w-full min-w-[1360px] text-left text-sm">
          <thead className="bg-slate-50 text-xs uppercase text-muted">
            <tr>
              <th className="px-4 py-3">Service</th>
              <th className="px-4 py-3">Duration</th>
              <th className="px-4 py-3">Price</th>
              <th className="px-4 py-3">Managed in</th>
              <th className="px-4 py-3">Category</th>
              <th className="px-4 py-3">Aliases</th>
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
                  <FieldAuthorityBadge authority={service.field_authority} />
                </td>
                <td className="px-4 py-3">
                  <CategoryCell service={service} categories={categories} busy={busy} onAssignCategory={onAssignCategory} />
                </td>
                <td className="px-4 py-3">
                  <ServiceAliasesCell
                    service={service}
                    schedulingAuthority={schedulingAuthority}
                    activeProvider={activeProvider}
                    aliases={service.id ? serviceAliasesByServiceID.get(service.id) ?? [] : []}
                    busy={busy}
                    draftServiceID={serviceAliasDraftServiceID}
                    draft={serviceAliasDraft}
                    onDraftServiceChange={onServiceAliasDraftServiceChange}
                    onDraftChange={onServiceAliasDraftChange}
                    onAddAlias={onAddServiceAlias}
                    onArchiveAlias={onArchiveServiceAlias}
                  />
                </td>
                <td className="px-4 py-3">
                  <div className="space-y-1">
                    <Badge value={service.sync_status || "local_only"} />
                    {service.sync_error ? <div className="max-w-44 text-xs leading-5 text-red-700">{service.sync_error}</div> : null}
                  </div>
                </td>
                <td className="px-4 py-3">
                  <AIStatus service={service} schedulingAuthority={schedulingAuthority} activeProvider={activeProvider} />
                </td>
                <td className="px-4 py-3">
                  <ServiceActions service={service} schedulingAuthority={schedulingAuthority} activeProvider={activeProvider} busy={busy} onEdit={onEdit} onArchive={onArchive} onUpdateAI={onUpdateAI} />
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
            schedulingAuthority={schedulingAuthority}
            activeProvider={activeProvider}
            categories={categories}
            aliases={service.id ? serviceAliasesByServiceID.get(service.id) ?? [] : []}
            busy={busy}
            serviceAliasDraftServiceID={serviceAliasDraftServiceID}
            serviceAliasDraft={serviceAliasDraft}
            onEdit={onEdit}
            onArchive={onArchive}
            onUpdateAI={onUpdateAI}
            onAssignCategory={onAssignCategory}
            onServiceAliasDraftServiceChange={onServiceAliasDraftServiceChange}
            onServiceAliasDraftChange={onServiceAliasDraftChange}
            onAddServiceAlias={onAddServiceAlias}
            onArchiveServiceAlias={onArchiveServiceAlias}
          />
        ))}
      </div>
    </>
  );
}

function ServiceCard({
  service,
  schedulingAuthority,
  activeProvider,
  categories,
  aliases,
  busy,
  serviceAliasDraftServiceID,
  serviceAliasDraft,
  onEdit,
  onArchive,
  onUpdateAI,
  onAssignCategory,
  onServiceAliasDraftServiceChange,
  onServiceAliasDraftChange,
  onAddServiceAlias,
  onArchiveServiceAlias
}: {
  service: POSService;
  schedulingAuthority?: SchedulingAuthority;
  activeProvider?: string;
  categories: POSServiceCategory[];
  aliases: ServiceAlias[];
  busy: string;
  serviceAliasDraftServiceID: string;
  serviceAliasDraft: string;
  onEdit: (service: POSService) => void;
  onArchive: (service: POSService) => void;
  onUpdateAI: (service: POSService, nextValue: boolean) => void;
  onAssignCategory: (service: POSService, categoryID: string) => void;
  onServiceAliasDraftServiceChange: (serviceID: string) => void;
  onServiceAliasDraftChange: (value: string) => void;
  onAddServiceAlias: (service: POSService) => void;
  onArchiveServiceAlias: (alias: ServiceAlias) => void;
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
          ["Managed in", authorityLabel(service.field_authority)],
          ["Category", service.category_name || "Unassigned"],
          ["POS link", service.pos_linked ? "Linked" : "Not linked"]
        ]}
      />
      <div className="mt-4">
        <CategoryCell service={service} categories={categories} busy={busy} onAssignCategory={onAssignCategory} />
      </div>
      <div className="mt-4">
        <ServiceAliasesCell
          service={service}
          schedulingAuthority={schedulingAuthority}
          activeProvider={activeProvider}
          aliases={aliases}
          busy={busy}
          draftServiceID={serviceAliasDraftServiceID}
          draft={serviceAliasDraft}
          onDraftServiceChange={onServiceAliasDraftServiceChange}
          onDraftChange={onServiceAliasDraftChange}
          onAddAlias={onAddServiceAlias}
          onArchiveAlias={onArchiveServiceAlias}
        />
      </div>
      <div className="mt-4">
        <AIStatus service={service} schedulingAuthority={schedulingAuthority} activeProvider={activeProvider} />
      </div>
      <div className="mt-4">
        <ServiceActions service={service} schedulingAuthority={schedulingAuthority} activeProvider={activeProvider} busy={busy} onEdit={onEdit} onArchive={onArchive} onUpdateAI={onUpdateAI} />
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

function ServiceAliasesCell({
  service,
  schedulingAuthority,
  activeProvider,
  aliases,
  busy,
  draftServiceID,
  draft,
  onDraftServiceChange,
  onDraftChange,
  onAddAlias,
  onArchiveAlias
}: {
  service: POSService;
  schedulingAuthority?: SchedulingAuthority;
  activeProvider?: string;
  aliases: ServiceAlias[];
  busy: string;
  draftServiceID: string;
  draft: string;
  onDraftServiceChange: (serviceID: string) => void;
  onDraftChange: (value: string) => void;
  onAddAlias: (service: POSService) => void;
  onArchiveAlias: (alias: ServiceAlias) => void;
}) {
  const serviceID = service.id ?? "";
  const activeAliases = aliases.filter((alias) => alias.status !== "archived");
  const aliasBusy = busy === `service-alias-${serviceID}`;
  const draftOpen = serviceID !== "" && draftServiceID === serviceID;
  const archived = Boolean(service.archived_at);
  const usedByAI = service.ai_bookable && canEnableAI(service, schedulingAuthority, activeProvider);

  return (
    <div className="min-w-56">
      {activeAliases.length > 0 ? (
        <div className="flex flex-wrap gap-2">
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
        <div className="text-xs leading-5 text-muted">No active aliases.</div>
      )}

      {activeAliases.length > 0 && !usedByAI ? (
        <div className="mt-2 text-xs leading-5 text-muted">Not used by AI: {serviceAliasGateReason(service, schedulingAuthority, activeProvider)}</div>
      ) : null}

      {draftOpen ? (
        <div className="mt-3 flex flex-col gap-2">
          <input
            className="h-10 min-w-0 rounded-md border border-line px-3 text-sm text-ink outline-none focus:border-brand"
            value={draft}
            onChange={(event) => onDraftChange(event.target.value)}
            placeholder="Example: shell manicure"
            disabled={busy !== ""}
          />
          <div className="flex flex-wrap gap-2">
            <Button type="button" onClick={() => onAddAlias(service)} disabled={busy !== "" || !draft.trim()}>
              {aliasBusy ? "Saving..." : "Save alias"}
            </Button>
            <Button
              type="button"
              variant="secondary"
              onClick={() => {
                onDraftServiceChange("");
                onDraftChange("");
              }}
              disabled={busy !== ""}
            >
              Cancel
            </Button>
          </div>
        </div>
      ) : !archived ? (
        <Button
          type="button"
          variant="ghost"
          className="mt-3"
          onClick={() => {
            onDraftServiceChange(serviceID);
            onDraftChange("");
          }}
          disabled={busy !== "" || !serviceID}
        >
          <Plus className="h-4 w-4" />
          Add alias
        </Button>
      ) : null}
    </div>
  );
}

function ServiceActions({
  service,
  schedulingAuthority,
  activeProvider,
  busy,
  onEdit,
  onArchive,
  onUpdateAI
}: {
  service: POSService;
  schedulingAuthority?: SchedulingAuthority;
  activeProvider?: string;
  busy: string;
  onEdit: (service: POSService) => void;
  onArchive: (service: POSService) => void;
  onUpdateAI: (service: POSService, nextValue: boolean) => void;
}) {
  const aiBusy = busy === `ai-${service.id}`;
  const archiveBusy = busy === `archive-${service.id}`;
  const archived = Boolean(service.archived_at);
  const canEnable = canEnableAI(service, schedulingAuthority, activeProvider);
  const nextAI = !service.ai_bookable;
  const readOnlyProvider = providerManagedReadOnly(service.field_authority);
  return (
    <div className="flex flex-wrap gap-2">
      <Button type="button" variant="secondary" onClick={() => onEdit(service)} disabled={busy !== ""}>
        <Pencil className="h-4 w-4" />
        {readOnlyProvider ? "Manage" : "Edit"}
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
        {archiveBusy ? "Archiving..." : "Archive in ManleAI"}
      </Button>
    </div>
  );
}

function AIStatus({ service, schedulingAuthority, activeProvider }: { service: POSService; schedulingAuthority?: SchedulingAuthority; activeProvider?: string }) {
  return (
    <div className="space-y-1">
      <Badge value={service.ai_bookable && canEnableAI(service, schedulingAuthority, activeProvider) ? "allowed" : "blocked"} />
      <div><Badge value={service.consultation_profile?.status || "consultation_draft"} /></div>
      <div className="max-w-56 text-xs leading-5 text-muted">{serviceGateReason(service, schedulingAuthority, activeProvider)}</div>
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
        Create a canonical service, then configure its eligibility for the selected scheduling authority.
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

function ConsultationOptionGroup({
  label,
  options,
  selected,
  disabled,
  onChange
}: {
  label: string;
  options: Array<[string, string]>;
  selected: string[];
  disabled: boolean;
  onChange: (next: string[]) => void;
}) {
  return (
    <fieldset className="rounded-md border border-line bg-white p-3" disabled={disabled}>
      <legend className="px-1 text-sm font-medium text-ink">{label}</legend>
      <div className="mt-2 grid gap-2 sm:grid-cols-2">
        {options.map(([value, optionLabel]) => (
          <label key={value} className="flex items-center gap-2 text-sm text-ink">
            <input
              type="checkbox"
              checked={selected.includes(value)}
              onChange={(event) => onChange(event.target.checked ? [...selected, value] : selected.filter((item) => item !== value))}
            />
            {optionLabel}
          </label>
        ))}
      </div>
    </fieldset>
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

function serviceMetrics(services: POSService[], schedulingAuthority?: SchedulingAuthority, activeProvider?: string) {
  return {
    total: services.length,
    synced: services.filter((service) => service.sync_status === "synced" && service.pos_linked).length,
    localOnly: services.filter((service) => service.sync_status === "local_only").length,
    aiBookable: services.filter((service) => service.ai_bookable && canEnableAI(service, schedulingAuthority, activeProvider)).length,
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
    active: true,
    consultationStatus: "draft",
    recommendedOutcomes: [],
    compatibleCurrentSystems: [],
    lengthCapabilities: [],
    priorityTags: [],
    finishOptions: [],
    maintenanceNote: ""
  };
}

function serviceToForm(service: POSService): ServiceFormState {
  const profile = service.consultation_profile;
  return {
    name: service.name,
    description: service.description ?? "",
    aiDescription: profile?.owner_approved_summary ?? service.ai_description ?? "",
    durationMinutes: service.duration_minutes > 0 ? String(service.duration_minutes) : "",
    priceFrom: service.price_from ? String(service.price_from) : "",
    serviceCategoryID: service.service_category_id ?? "",
    active: service.active,
    consultationStatus: profile?.status === "ready" || profile?.status === "disabled" ? profile.status : "draft",
    recommendedOutcomes: profile?.recommended_outcomes ?? [],
    compatibleCurrentSystems: profile?.compatible_current_systems ?? [],
    lengthCapabilities: profile?.length_capabilities ?? [],
    priorityTags: profile?.priority_tags ?? [],
    finishOptions: profile?.finish_options ?? [],
    maintenanceNote: profile?.maintenance_note ?? ""
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
    active: form.active,
    consultation_profile: {
      status: form.consultationStatus,
      recommended_outcomes: form.recommendedOutcomes,
      compatible_current_systems: form.compatibleCurrentSystems,
      length_capabilities: form.lengthCapabilities,
      priority_tags: form.priorityTags,
      finish_options: form.finishOptions,
      maintenance_note: form.maintenanceNote,
      owner_approved_summary: form.aiDescription
    }
  };
}

function serviceOwnerControlsPayload(form: ServiceFormState) {
  return {
    ai_description: form.aiDescription,
    service_category_id: form.serviceCategoryID,
    consultation_profile: {
      status: form.consultationStatus,
      recommended_outcomes: form.recommendedOutcomes,
      compatible_current_systems: form.compatibleCurrentSystems,
      length_capabilities: form.lengthCapabilities,
      priority_tags: form.priorityTags,
      finish_options: form.finishOptions,
      maintenance_note: form.maintenanceNote,
      owner_approved_summary: form.aiDescription
    }
  };
}

function consultationProfileHasStructuredValue(form: ServiceFormState) {
  return form.recommendedOutcomes.length + form.compatibleCurrentSystems.length + form.lengthCapabilities.length + form.priorityTags.length + form.finishOptions.length > 0;
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

function groupServiceAliasesByServiceID(aliases: ServiceAlias[]) {
  const byServiceID = new Map<string, ServiceAlias[]>();
  aliases.forEach((alias) => {
    const items = byServiceID.get(alias.service_id) ?? [];
    items.push(alias);
    byServiceID.set(alias.service_id, items);
  });
  byServiceID.forEach((items, serviceID) => {
    byServiceID.set(serviceID, items.sort(compareServiceAliases));
  });
  return byServiceID;
}

function refreshSummary(refresh: ServiceCategorySuggestionRefresh) {
  return [
    `Category suggestions refreshed: ${refresh.suggested_services} service suggestion${refresh.suggested_services === 1 ? "" : "s"}.`,
    `${refresh.created_categories} categor${refresh.created_categories === 1 ? "y" : "ies"} created, ${refresh.created_aliases} alias${refresh.created_aliases === 1 ? "" : "es"} created.`,
    `${refresh.created_service_aliases} service alias${refresh.created_service_aliases === 1 ? "" : "es"} created, ${refresh.updated_system_service_aliases} updated.`,
    refresh.skipped_alias_conflicts > 0 ? `${refresh.skipped_alias_conflicts} category alias conflict${refresh.skipped_alias_conflicts === 1 ? "" : "s"} skipped.` : "",
    refresh.skipped_service_alias_conflicts > 0 ? `${refresh.skipped_service_alias_conflicts} service alias conflict${refresh.skipped_service_alias_conflicts === 1 ? "" : "s"} skipped.` : ""
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

function compareServiceAliases(a: ServiceAlias, b: ServiceAlias) {
  if (a.status !== b.status) return a.status === "active" ? -1 : 1;
  return a.alias.localeCompare(b.alias);
}

function serviceCatalogDescription(schedulingAuthority?: SchedulingAuthority) {
  if (schedulingAuthority === "external_provider") {
    return "External-provider eligibility requires the active provider's synced identity and AI permission.";
  }
  if (schedulingAuthority === "owner_manual" || schedulingAuthority === "manleai_calendar") {
    return "Internal authorities use active canonical services with positive duration and AI permission; detailed readiness remains backend-owned.";
  }
  return "Scheduling authority is unavailable; eligibility fails closed until backend state loads.";
}

function localServiceRecordDescription(schedulingAuthority?: SchedulingAuthority) {
  if (schedulingAuthority === "external_provider") {
    return "Managed in ManleAI. The external provider is not updated; external booking requires a valid identity from the active provider.";
  }
  if (schedulingAuthority === "owner_manual" || schedulingAuthority === "manleai_calendar") {
    return "Managed in ManleAI. A POS link is optional; scheduling eligibility still requires an active canonical service, positive duration, AI permission, and backend readiness.";
  }
  return "Managed in ManleAI. Scheduling eligibility stays disabled until backend authority state is available.";
}

function canEnableAI(service: POSService, schedulingAuthority?: SchedulingAuthority, activeProvider?: string) {
  if (!service.active || service.archived_at || service.duration_minutes <= 0) return false;
  if (schedulingAuthority === "owner_manual" || schedulingAuthority === "manleai_calendar") return true;
  if (schedulingAuthority !== "external_provider") return false;
  return Boolean(activeProvider) && service.pos_provider === activeProvider && service.sync_status === "synced" && service.pos_linked;
}

function serviceAliasGateReason(service: POSService, schedulingAuthority?: SchedulingAuthority, activeProvider?: string) {
  if (!service.ai_bookable) return "AI booking is not allowed for this service.";
  return serviceGateReason(service, schedulingAuthority, activeProvider);
}

function serviceGateReason(service: POSService, schedulingAuthority?: SchedulingAuthority, activeProvider?: string) {
  if (service.archived_at || service.sync_status === "archived") return "Archived services stay visible for history and are not bookable.";
  if (!service.active) return "Inactive services are not bookable by the AI receptionist.";
  if (service.duration_minutes <= 0) return "A positive duration is required before this service can be allowed for scheduling.";
  if (schedulingAuthority === "owner_manual" || schedulingAuthority === "manleai_calendar") {
    return service.ai_bookable
      ? "Allowed by the canonical catalog. Authority-specific readiness is shown separately."
      : "This active canonical service can be allowed without a POS link.";
  }
  if (schedulingAuthority !== "external_provider") return "Scheduling authority is unavailable, so eligibility fails closed.";
  if (!activeProvider || service.pos_provider !== activeProvider) return "Service identity does not belong to the salon's active external provider.";
  if (!service.pos_linked || service.sync_status === "local_only") return "Local-only services need a Square Appointments link before they are booking-ready.";
  if (service.sync_status === "sync_failed") return service.sync_error || "Latest POS sync failed; service is not bookable.";
  if (service.sync_status === "unmapped") return "Service needs an active-provider mapping before it is bookable.";
  if (service.ai_bookable) return "Presentation checks pass. The API verifies the authoritative provider link before booking.";
  return "Synced linked service can be allowed for AI booking; the API remains authoritative.";
}

function errorMessage(error: unknown, fallback: string) {
  return error instanceof Error ? error.message : fallback;
}

function formatPrice(value?: number) {
  if (!value) return "Not set";
  return `$${value.toFixed(2)}`;
}
