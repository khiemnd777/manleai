"use client";

import { FormEvent, useEffect, useMemo, useState } from "react";
import { BookOpenText, FilePlus2, RefreshCcw, Search, ShieldCheck, TestTube2, X } from "lucide-react";
import { Alert } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardDescription, CardTitle } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { apiRequest } from "@/lib/api/client";
import { businessGet, listBusinessSalons, type BusinessSalonProfile } from "@/lib/api/business";
import { storedActiveTenantSalonID } from "@/lib/api/tenant-context";
import type { KnowledgeItem, OwnerCorrection, POSService, Salon, SchedulingAuthority, ServiceAlias } from "@/types/api";

type SalonListResponse = {
  salons: Salon[];
};

type KnowledgeResponse = {
  knowledge_items: KnowledgeItem[];
};

type CorrectionsResponse = {
  owner_corrections: OwnerCorrection[];
};

type ServicesResponse = {
  services: POSService[];
};

type EvaluationResult = {
  message: string;
  reply: string;
  matched_knowledge?: {
    title: string;
    category: string;
    body: string;
  };
  outcome: string;
  booking_action: string;
  scheduling_authority: SchedulingAuthority;
  confirmation_requirement: "pending_owner_review" | "atomic_internal_commit" | "provider_booking_success";
  confirmation_guardrail: string;
  pos_confirmation_required: boolean;
};

type FormState = {
  id?: string;
  correctionId?: string;
  title: string;
  category: string;
  body: string;
  status: string;
};

type ServiceAliasDraft = {
  correctionId: string;
  serviceId: string;
  alias: string;
};

const emptyForm: FormState = {
  title: "",
  category: "faq",
  body: "",
  status: "draft"
};

const emptyServiceAliasDraft: ServiceAliasDraft = {
  correctionId: "",
  serviceId: "",
  alias: ""
};

const categories = ["faq", "policy", "services", "hours", "handoff", "operations"];
const statuses = ["draft", "active", "archived"];

type TrainingSurface = "tenant" | "platform";

export function TrainingDashboard({ surface = "tenant", salonID: fixedSalonID = "" }: { surface?: TrainingSurface; salonID?: string } = {}) {
  const [salon, setSalon] = useState<Salon | null>(null);
  const [knowledge, setKnowledge] = useState<KnowledgeItem[]>([]);
  const [corrections, setCorrections] = useState<OwnerCorrection[]>([]);
  const [services, setServices] = useState<POSService[]>([]);
  const [form, setForm] = useState<FormState>(emptyForm);
  const [serviceAliasDraft, setServiceAliasDraft] = useState<ServiceAliasDraft>(emptyServiceAliasDraft);
  const [correctionText, setCorrectionText] = useState("");
  const [evaluationMessage, setEvaluationMessage] = useState("");
  const [evaluation, setEvaluation] = useState<EvaluationResult | null>(null);
  const [filter, setFilter] = useState("all");
  const [query, setQuery] = useState("");
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [serviceCatalogLoading, setServiceCatalogLoading] = useState(false);
  const [serviceAliasSaving, setServiceAliasSaving] = useState(false);
  const [evaluating, setEvaluating] = useState(false);
  const [error, setError] = useState("");
  const [serviceCatalogError, setServiceCatalogError] = useState("");
  const [correctionAccessError, setCorrectionAccessError] = useState("");
  const [serviceAliasError, setServiceAliasError] = useState("");
  const [actionError, setActionError] = useState("");
  const [success, setSuccess] = useState("");

  async function load() {
    setError("");
    setLoading(true);
    try {
      const activeSalon = surface === "platform"
        ? await loadPlatformTrainingSalon(fixedSalonID)
        : await loadTenantTrainingSalon();
      setSalon(activeSalon);
      if (!activeSalon) {
        setKnowledge([]);
        setCorrections([]);
        setServices([]);
        setServiceCatalogError("");
        return;
      }
      const [knowledgeResponse] = await Promise.all([
        apiRequest<KnowledgeResponse>(trainingResourcePath(surface, activeSalon.id, "/knowledge-items")),
        loadCorrections(activeSalon.id),
        loadServiceCatalog(activeSalon.id)
      ]);
      setKnowledge(knowledgeResponse.knowledge_items);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Could not load AI training data.");
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    void load();
  }, [fixedSalonID, surface]);

  const filteredKnowledge = useMemo(() => {
    const normalizedQuery = query.trim().toLowerCase();
    return knowledge.filter((item) => {
      const categoryMatch = filter === "all" || item.category === filter;
      const queryMatch =
        normalizedQuery === "" ||
        item.title.toLowerCase().includes(normalizedQuery) ||
        item.body.toLowerCase().includes(normalizedQuery);
      return categoryMatch && queryMatch;
    });
  }, [filter, knowledge, query]);

  const activeCount = knowledge.filter((item) => item.status === "active").length;
  const pendingCorrections = corrections.filter((item) => item.status === "pending").length;
  const canSave = Boolean(form.title.trim() && form.body.trim() && salon);
  const aliasTargetServices = useMemo(
    () =>
      services.filter(
        (service) =>
          Boolean(service.id) &&
          service.active &&
          service.ai_bookable &&
          !service.archived_at &&
          service.sync_status !== "archived"
      ),
    [services]
  );
  const selectedAliasService = aliasTargetServices.find((service) => service.id === serviceAliasDraft.serviceId);
  const actionBusy = saving || serviceAliasSaving;
  const canApplyServiceAlias = Boolean(
    salon &&
      form.correctionId &&
      serviceAliasDraft.correctionId === form.correctionId &&
      selectedAliasService?.id &&
      serviceAliasDraft.alias.trim() &&
      !serviceCatalogLoading &&
      !serviceCatalogError &&
      !actionBusy
  );

  async function saveKnowledge(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!salon || !canSave || serviceAliasSaving) return;
    setSaving(true);
    setActionError("");
    setSuccess("");
    try {
      const payload = {
        title: form.title,
        category: form.category,
        body: form.body,
        status: form.status
      };
      if (form.correctionId) {
        await apiRequest<KnowledgeItem>(trainingResourcePath(surface, salon.id, `/owner-corrections/${form.correctionId}/apply`), {
          method: "POST",
          body: JSON.stringify(payload)
        });
        setSuccess("Correction applied to active knowledge.");
      } else if (form.id) {
        await apiRequest<KnowledgeItem>(trainingResourcePath(surface, salon.id, `/knowledge-items/${form.id}`), {
          method: "PUT",
          body: JSON.stringify(payload)
        });
        setSuccess("Knowledge item updated.");
      } else {
        await apiRequest<KnowledgeItem>(trainingResourcePath(surface, salon.id, "/knowledge-items"), {
          method: "POST",
          body: JSON.stringify(payload)
        });
        setSuccess("Knowledge item created.");
      }
      setForm(emptyForm);
      setServiceAliasDraft(emptyServiceAliasDraft);
      setServiceAliasError("");
      await reloadTraining(salon.id);
    } catch (err) {
      setActionError(err instanceof Error ? err.message : "Could not save knowledge item.");
    } finally {
      setSaving(false);
    }
  }

  async function deleteKnowledge(item: KnowledgeItem) {
    if (!salon || serviceAliasSaving) return;
    setSaving(true);
    setActionError("");
    setSuccess("");
    try {
      await apiRequest<void>(trainingResourcePath(surface, salon.id, `/knowledge-items/${item.id}`), { method: "DELETE" });
      if (form.id === item.id) setForm(emptyForm);
      setSuccess("Knowledge item deleted.");
      await reloadTraining(salon.id);
    } catch (err) {
      setActionError(err instanceof Error ? err.message : "Could not delete knowledge item.");
    } finally {
      setSaving(false);
    }
  }

  async function createCorrection(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!salon || !correctionText.trim() || serviceAliasSaving) return;
    setSaving(true);
    setActionError("");
    setSuccess("");
    try {
      await apiRequest<OwnerCorrection>(trainingResourcePath(surface, salon.id, "/owner-corrections"), {
        method: "POST",
        body: JSON.stringify({ correction: correctionText })
      });
      setCorrectionText("");
      setSuccess("Owner correction captured.");
      await reloadTraining(salon.id);
    } catch (err) {
      setActionError(err instanceof Error ? err.message : "Could not save owner correction.");
    } finally {
      setSaving(false);
    }
  }

  async function evaluateAnswer(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!salon || !evaluationMessage.trim()) return;
    setEvaluating(true);
    setActionError("");
    setSuccess("");
    try {
      const result = await apiRequest<EvaluationResult>(trainingResourcePath(surface, salon.id, "/training/evaluate"), {
        method: "POST",
        body: JSON.stringify({ message: evaluationMessage })
      });
      setEvaluation(result);
    } catch (err) {
      setActionError(err instanceof Error ? err.message : "Could not evaluate training question.");
    } finally {
      setEvaluating(false);
    }
  }

  function reviewCorrection(correction: OwnerCorrection) {
    setForm({
      correctionId: correction.id,
      title: correctionTitle(correction),
      category: "operations",
      body: correction.correction,
      status: "active"
    });
    setServiceAliasDraft({ correctionId: correction.id, serviceId: "", alias: "" });
    setServiceAliasError("");
    setActionError("");
    setSuccess("");
  }

  async function applyCorrectionAsServiceAlias(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!salon || !form.correctionId || !selectedAliasService?.id || !canApplyServiceAlias) return;
    setServiceAliasSaving(true);
    setServiceAliasError("");
    setActionError("");
    setSuccess("");
    try {
      const alias = await apiRequest<ServiceAlias>(
        trainingResourcePath(surface, salon.id, `/owner-corrections/${form.correctionId}/apply-service-alias`),
        {
          method: "POST",
          body: JSON.stringify({
            service_id: selectedAliasService.id,
            alias: serviceAliasDraft.alias.trim()
          })
        }
      );
      setForm(emptyForm);
      setServiceAliasDraft(emptyServiceAliasDraft);
      setSuccess(`Service alias "${alias.alias}" applied to ${alias.service_name}.`);
      try {
        await reloadTraining(salon.id);
      } catch {
        setActionError("The service alias was applied, but the correction list could not refresh. Refresh the page to load the saved result.");
      }
    } catch (err) {
      setServiceAliasError(err instanceof Error ? err.message : "Could not apply correction as a service alias.");
    } finally {
      setServiceAliasSaving(false);
    }
  }

  async function dismissCorrection(correction: OwnerCorrection) {
    if (!salon || serviceAliasSaving) return;
    setSaving(true);
    setActionError("");
    setSuccess("");
    try {
      await apiRequest<OwnerCorrection>(trainingResourcePath(surface, salon.id, `/owner-corrections/${correction.id}/dismiss`), {
        method: "POST"
      });
      setSuccess("Correction dismissed.");
      if (form.correctionId === correction.id) {
        resetEditor();
      }
      await reloadTraining(salon.id);
    } catch (err) {
      setActionError(err instanceof Error ? err.message : "Could not dismiss owner correction.");
    } finally {
      setSaving(false);
    }
  }

  async function reloadTraining(salonID: string) {
    const [knowledgeResponse] = await Promise.all([
      apiRequest<KnowledgeResponse>(trainingResourcePath(surface, salonID, "/knowledge-items")),
      loadCorrections(salonID)
    ]);
    setKnowledge(knowledgeResponse.knowledge_items);
  }

  async function loadCorrections(salonID: string) {
    setCorrectionAccessError("");
    try {
      const response = await apiRequest<CorrectionsResponse>(trainingResourcePath(surface, salonID, "/owner-corrections"));
      setCorrections(response.owner_corrections);
    } catch (err) {
      setCorrections([]);
      setCorrectionAccessError(err instanceof Error ? err.message : "Could not load Owner corrections.");
    }
  }

  async function loadServiceCatalog(salonID: string) {
    setServiceCatalogLoading(true);
    setServiceCatalogError("");
    try {
      const response = await apiRequest<ServicesResponse>(trainingResourcePath(surface, salonID, "/services"));
      setServices(response.services);
    } catch (err) {
      setServices([]);
      setServiceCatalogError(err instanceof Error ? err.message : "Could not load the salon service catalog.");
    } finally {
      setServiceCatalogLoading(false);
    }
  }

  function editKnowledge(item: KnowledgeItem) {
    setForm({
      id: item.id,
      title: item.title,
      category: item.category,
      body: item.body,
      status: item.status
    });
    setServiceAliasDraft(emptyServiceAliasDraft);
    setServiceAliasError("");
    setActionError("");
    setSuccess("");
  }

  function resetEditor() {
    setForm(emptyForm);
    setServiceAliasDraft(emptyServiceAliasDraft);
    setServiceAliasError("");
  }

  if (loading) {
    return (
      <div className="space-y-6">
        <Skeleton className="h-9 w-72" />
        <div className="grid gap-4 md:grid-cols-3">
          <Skeleton className="h-28" />
          <Skeleton className="h-28" />
          <Skeleton className="h-28" />
        </div>
        <div className="grid gap-4 xl:grid-cols-[1.25fr_0.75fr]">
          <Skeleton className="h-[520px]" />
          <Skeleton className="h-[520px]" />
        </div>
      </div>
    );
  }

  if (error) {
    return <Alert title="Training unavailable" message={error}><Button type="button" variant="secondary" className="mt-4" onClick={() => void load()}><RefreshCcw className="h-4 w-4" />Retry</Button></Alert>;
  }

  if (!salon) {
    return (
      <Card>
        <CardTitle>Create a salon first</CardTitle>
        <CardDescription>AI training data is scoped by salon, so the owner profile must exist first.</CardDescription>
      </Card>
    );
  }

  return (
    <div className="space-y-6">
      <div className="flex flex-col justify-between gap-3 md:flex-row md:items-end">
        <div>
          <h1 className="text-2xl font-bold text-ink">AI Training</h1>
          <p className="mt-1 text-sm text-muted">
            Salon-authored knowledge and owner corrections for the AI phone receptionist.
          </p>
        </div>
        <div className="flex flex-wrap items-center gap-3">
          <Badge value="active" />
          <Button type="button" variant="secondary" onClick={() => void load()} disabled={actionBusy}>
            <RefreshCcw className="h-4 w-4" />
            Refresh
          </Button>
        </div>
      </div>

      {actionError ? <Alert title="Training action failed" message={actionError} /> : null}
      {success ? <Alert type="success" title="Training updated" message={success} /> : null}

      <div className="grid gap-4 md:grid-cols-3">
        <Metric label="Active knowledge" value={String(activeCount)} />
        <Metric label="Draft entries" value={String(knowledge.filter((item) => item.status === "draft").length)} />
        <Metric label="Pending corrections" value={String(pendingCorrections)} />
      </div>

      <Card className="border-emerald-200 bg-emerald-50 shadow-none">
        <div className="flex gap-3">
          <ShieldCheck className="mt-0.5 h-5 w-5 flex-none text-emerald-700" />
          <div>
            <CardTitle>Scheduling authority boundary</CardTitle>
            <CardDescription className="text-emerald-800">
              Knowledge can answer salon questions, but confirmation still requires the selected authority&apos;s durable evidence and cannot be inferred from knowledge text.
            </CardDescription>
          </div>
        </div>
      </Card>

      <Card>
        <div className="flex items-start gap-3">
          <TestTube2 className="mt-1 h-5 w-5 text-brand" />
          <div>
            <CardTitle>Test AI answer</CardTitle>
            <CardDescription>
              Preview how active knowledge answers a customer question without creating a call session.
            </CardDescription>
          </div>
        </div>

        <form className="mt-5 space-y-4" onSubmit={evaluateAnswer}>
          <label className="block">
            <span className="text-sm font-medium text-ink">Customer question</span>
            <textarea
              className="mt-2 min-h-24 w-full rounded-md border border-line bg-white px-3 py-2 text-sm leading-6 text-ink outline-none focus:border-brand"
              value={evaluationMessage}
              onChange={(event) => setEvaluationMessage(event.target.value)}
              placeholder="Do you take walk-ins?"
              disabled={evaluating}
            />
          </label>
          <Button type="submit" disabled={evaluating || !evaluationMessage.trim()}>
            <TestTube2 className="h-4 w-4" />
            Test answer
          </Button>
        </form>

        <div className="mt-5 rounded-md border border-line bg-slate-50 p-4">
          {evaluation ? (
            <div className="space-y-4">
              <div>
                <div className="text-xs font-semibold uppercase tracking-wide text-muted">Customer question</div>
                <div className="mt-1 text-sm font-medium text-ink">{evaluation.message}</div>
              </div>
              <div>
                <div className="text-xs font-semibold uppercase tracking-wide text-muted">AI response preview</div>
                <div className="mt-1 text-sm leading-6 text-ink">{evaluation.reply}</div>
              </div>
              {evaluation.matched_knowledge ? (
                <div className="rounded-md border border-line bg-white p-3">
                  <div className="flex flex-wrap items-center gap-2">
                    <Badge value={evaluation.matched_knowledge.category} />
                    <span className="text-sm font-semibold text-ink">{evaluation.matched_knowledge.title}</span>
                  </div>
                  <div className="mt-2 text-sm leading-6 text-muted">{evaluation.matched_knowledge.body}</div>
                </div>
              ) : (
                <div className="rounded-md border border-line bg-white p-3 text-sm text-muted">
                  No active knowledge matched this question.
                </div>
              )}
              <div className="flex flex-wrap items-center gap-2">
                <Badge value={evaluation.outcome} />
                <Badge value={evaluation.booking_action === "none" ? "no_booking_action" : evaluation.booking_action} />
                <Badge value={evaluation.scheduling_authority} />
                <Badge value={evaluation.confirmation_requirement} />
              </div>
              <div className="text-xs leading-5 text-muted">{evaluation.confirmation_guardrail}</div>
            </div>
          ) : (
            <div className="text-sm text-muted">Run a sample question to preview active knowledge.</div>
          )}
        </div>
      </Card>

      <div className="grid gap-4 xl:grid-cols-[1.25fr_0.75fr]">
        <Card>
          <div className="flex flex-col justify-between gap-3 sm:flex-row sm:items-start">
            <div>
              <CardTitle>Knowledge base</CardTitle>
              <CardDescription>Active entries can guide FAQ, policy, hours, and operations replies.</CardDescription>
            </div>
            <Button type="button" variant="secondary" onClick={resetEditor} disabled={actionBusy}>
              <FilePlus2 className="h-4 w-4" />
              Add knowledge
            </Button>
          </div>

          <div className="mt-5 flex flex-col gap-3 lg:flex-row">
            <div className="relative min-w-0 flex-1">
              <Search className="pointer-events-none absolute left-3 top-3 h-4 w-4 text-muted" />
              <input
                className="h-10 w-full rounded-md border border-line bg-white pl-9 pr-3 text-sm text-ink outline-none focus:border-brand"
                value={query}
                onChange={(event) => setQuery(event.target.value)}
                placeholder="Search knowledge"
              />
            </div>
            <select
              className="h-10 rounded-md border border-line bg-white px-3 text-sm text-ink outline-none focus:border-brand"
              value={filter}
              onChange={(event) => setFilter(event.target.value)}
            >
              <option value="all">All categories</option>
              {categories.map((category) => (
                <option key={category} value={category}>
                  {category}
                </option>
              ))}
            </select>
          </div>

          {filteredKnowledge.length === 0 ? (
            <div className="mt-5 rounded-md border border-line p-6 text-center">
              <BookOpenText className="mx-auto h-5 w-5 text-muted" />
              <div className="mt-3 text-sm font-semibold text-ink">No knowledge entries</div>
              <div className="mt-1 text-sm leading-6 text-muted">
                Add the salon policies and operating notes the receptionist should use.
              </div>
            </div>
          ) : (
            <>
              <div className="mt-5 hidden overflow-x-auto rounded-md border border-line lg:block">
                <table className="w-full min-w-[760px] text-left text-sm">
                  <thead className="bg-slate-50 text-xs uppercase text-muted">
                    <tr>
                      <th className="px-4 py-3">Title</th>
                      <th className="px-4 py-3">Category</th>
                      <th className="px-4 py-3">Status</th>
                      <th className="px-4 py-3">Updated</th>
                      <th className="px-4 py-3">Action</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-line bg-white">
                    {filteredKnowledge.map((item) => (
                      <tr key={item.id}>
                        <td className="px-4 py-3">
                          <div className="font-medium text-ink">{item.title}</div>
                          <div className="mt-1 line-clamp-2 text-xs leading-5 text-muted">{item.body}</div>
                        </td>
                        <td className="px-4 py-3">
                          <Badge value={item.category} />
                        </td>
                        <td className="px-4 py-3">
                          <Badge value={item.status} />
                        </td>
                        <td className="px-4 py-3 text-muted">{formatDate(item.updated_at)}</td>
                        <td className="px-4 py-3">
                          <div className="flex gap-2">
                            <Button type="button" variant="secondary" onClick={() => editKnowledge(item)} disabled={actionBusy}>
                              Edit
                            </Button>
                            <Button type="button" variant="ghost" onClick={() => void deleteKnowledge(item)} disabled={actionBusy}>
                              Delete
                            </Button>
                          </div>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
              <div className="mt-5 space-y-3 lg:hidden">
                {filteredKnowledge.map((item) => (
                  <KnowledgeCard key={item.id} item={item} onEdit={() => editKnowledge(item)} onDelete={() => void deleteKnowledge(item)} saving={actionBusy} />
                ))}
              </div>
            </>
          )}
        </Card>

        <Card>
          <CardTitle>{form.correctionId ? "Review correction" : form.id ? "Edit knowledge" : "Add knowledge"}</CardTitle>
          <CardDescription>
            {form.correctionId
              ? "Choose whether this correction becomes reusable knowledge or a structured alias for one explicit service."
              : "Keep entries short, factual, and limited to salon operating knowledge."}
          </CardDescription>
          {form.correctionId ? (
            <div className="mt-5 text-sm font-semibold text-ink">Apply as knowledge</div>
          ) : null}
          <form className="mt-5 space-y-4" onSubmit={saveKnowledge}>
            <Field label="Title">
              <input
                className="h-10 w-full rounded-md border border-line bg-white px-3 text-sm text-ink outline-none focus:border-brand"
                value={form.title}
                onChange={(event) => setForm({ ...form, title: event.target.value })}
                disabled={actionBusy}
              />
            </Field>
            <div className="grid gap-3 sm:grid-cols-2">
              <Field label="Category">
                <select
                  className="h-10 w-full rounded-md border border-line bg-white px-3 text-sm text-ink outline-none focus:border-brand"
                  value={form.category}
                  onChange={(event) => setForm({ ...form, category: event.target.value })}
                  disabled={actionBusy}
                >
                  {categories.map((category) => (
                    <option key={category} value={category}>
                      {category}
                    </option>
                  ))}
                </select>
              </Field>
              <Field label="Status">
                <select
                  className="h-10 w-full rounded-md border border-line bg-white px-3 text-sm text-ink outline-none focus:border-brand"
                  value={form.status}
                  onChange={(event) => setForm({ ...form, status: event.target.value })}
                  disabled={actionBusy}
                >
                  {statuses.map((status) => (
                    <option key={status} value={status}>
                      {status}
                    </option>
                  ))}
                </select>
              </Field>
            </div>
            <Field label="Answer or operating note">
              <textarea
                className="min-h-32 w-full rounded-md border border-line bg-white px-3 py-2 text-sm leading-6 text-ink outline-none focus:border-brand"
                value={form.body}
                onChange={(event) => setForm({ ...form, body: event.target.value })}
                disabled={actionBusy}
              />
            </Field>
            <div className="flex flex-wrap gap-3">
              <Button type="submit" disabled={!canSave || actionBusy}>
                {saving && form.correctionId ? "Applying knowledge..." : form.correctionId ? "Apply correction as knowledge" : "Save knowledge"}
              </Button>
              <Button type="button" variant="secondary" onClick={resetEditor} disabled={actionBusy}>
                <X className="h-4 w-4" />
                Cancel
              </Button>
            </div>
          </form>
          {form.correctionId ? (
            <form className="mt-6 space-y-4 border-t border-line pt-5" onSubmit={applyCorrectionAsServiceAlias}>
              <div>
                <div className="text-sm font-semibold text-ink">Apply as service alias</div>
                <div className="mt-1 text-xs leading-5 text-muted">
                  Choose the real service and enter only the caller phrase that should identify it. No target or phrase is inferred from the correction.
                </div>
              </div>

              {serviceCatalogError ? (
                <div className="space-y-3">
                  <Alert title="Service catalog unavailable" message={serviceCatalogError} />
                  <Button
                    type="button"
                    variant="secondary"
                    onClick={() => void loadServiceCatalog(salon.id)}
                    disabled={serviceCatalogLoading || actionBusy}
                  >
                    <RefreshCcw className="h-4 w-4" />
                    Retry service catalog
                  </Button>
                </div>
              ) : null}

              {serviceAliasError ? <Alert title="Service alias not applied" message={serviceAliasError} /> : null}

              <Field label="Target service">
                <select
                  className="h-10 w-full rounded-md border border-line bg-white px-3 text-sm text-ink outline-none focus:border-brand disabled:bg-slate-50 disabled:text-muted"
                  value={serviceAliasDraft.serviceId}
                  onChange={(event) => {
                    setServiceAliasDraft({ ...serviceAliasDraft, serviceId: event.target.value });
                    setServiceAliasError("");
                  }}
                  disabled={serviceCatalogLoading || Boolean(serviceCatalogError) || aliasTargetServices.length === 0 || actionBusy}
                  required
                >
                  <option value="">
                    {serviceCatalogLoading ? "Loading services..." : "Choose a service"}
                  </option>
                  {aliasTargetServices.map((service) => (
                    <option key={service.id} value={service.id}>
                      {serviceOptionLabel(service)}
                    </option>
                  ))}
                </select>
              </Field>

              {!serviceCatalogLoading && !serviceCatalogError && aliasTargetServices.length === 0 ? (
                <div className="rounded-md border border-line bg-slate-50 p-3 text-xs leading-5 text-muted">
                  No active AI-bookable services are available. Enable an eligible service on the Services page before applying an alias.
                </div>
              ) : null}

              <Field label="Alias phrase">
                <input
                  className="h-10 w-full rounded-md border border-line bg-white px-3 text-sm text-ink outline-none focus:border-brand disabled:bg-slate-50 disabled:text-muted"
                  value={serviceAliasDraft.alias}
                  onChange={(event) => {
                    setServiceAliasDraft({ ...serviceAliasDraft, alias: event.target.value });
                    setServiceAliasError("");
                  }}
                  placeholder="Enter the caller phrase"
                  disabled={serviceCatalogLoading || Boolean(serviceCatalogError) || aliasTargetServices.length === 0 || actionBusy}
                  required
                />
              </Field>

              <Button type="submit" disabled={!canApplyServiceAlias}>
                {serviceAliasSaving ? "Applying service alias..." : "Apply correction as service alias"}
              </Button>
            </form>
          ) : null}
        </Card>
      </div>

      <Card>
        <div className="flex flex-col justify-between gap-3 sm:flex-row sm:items-start">
          <div>
            <CardTitle>Owner corrections</CardTitle>
            <CardDescription>Capture corrections from reviewed calls, then apply each one as knowledge or a structured service alias.</CardDescription>
          </div>
          <Badge value={pendingCorrections > 0 ? "active" : "disabled"} />
        </div>

        {correctionAccessError ? <div className="mt-4"><Alert title="Owner corrections unavailable" message={`${correctionAccessError} Knowledge management remains available; transcript-linked corrections require active Calls PII authorization.`} /></div> : null}

        <form className="mt-5 flex flex-col gap-3 md:flex-row" onSubmit={createCorrection}>
          <input
            className="h-10 min-w-0 flex-1 rounded-md border border-line bg-white px-3 text-sm text-ink outline-none focus:border-brand"
            value={correctionText}
            onChange={(event) => setCorrectionText(event.target.value)}
            placeholder="Add owner correction"
            disabled={actionBusy}
          />
          <Button type="submit" disabled={actionBusy || !correctionText.trim()}>
            Capture correction
          </Button>
        </form>

        {corrections.length === 0 ? (
          <div className="mt-5 rounded-md border border-line p-6 text-center text-sm text-muted">
            No owner corrections yet.
          </div>
        ) : (
          <div className="mt-5 space-y-3">
            {corrections.map((correction) => (
              <div key={correction.id} className="rounded-md border border-line p-4">
                <div className="flex flex-col justify-between gap-3 md:flex-row md:items-start">
                  <div>
                    <div className="text-sm font-medium text-ink">{correction.correction}</div>
                    <div className="mt-1 text-xs leading-5 text-muted">
                      {correctionSource(correction)} / {formatDate(correction.created_at)}
                    </div>
                  </div>
                  <div className="flex flex-wrap items-center gap-2">
                    <Badge value={correction.status} />
                    {correction.status === "pending" ? (
                      <>
                        <Button type="button" variant="secondary" onClick={() => reviewCorrection(correction)} disabled={actionBusy}>
                          Review correction
                        </Button>
                        <Button type="button" variant="ghost" onClick={() => void dismissCorrection(correction)} disabled={actionBusy}>
                          Dismiss
                        </Button>
                      </>
                    ) : null}
                  </div>
                </div>
              </div>
            ))}
          </div>
        )}
      </Card>
    </div>
  );
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <label className="block">
      <span className="text-sm font-medium text-ink">{label}</span>
      <div className="mt-2">{children}</div>
    </label>
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

function KnowledgeCard({
  item,
  onEdit,
  onDelete,
  saving
}: {
  item: KnowledgeItem;
  onEdit: () => void;
  onDelete: () => void;
  saving: boolean;
}) {
  return (
    <div className="rounded-md border border-line p-4">
      <div className="flex flex-wrap items-center gap-2">
        <Badge value={item.category} />
        <Badge value={item.status} />
      </div>
      <div className="mt-3 text-sm font-medium text-ink">{item.title}</div>
      <div className="mt-1 text-sm leading-6 text-muted">{item.body}</div>
      <div className="mt-3 flex gap-2">
        <Button type="button" variant="secondary" onClick={onEdit} disabled={saving}>
          Edit
        </Button>
        <Button type="button" variant="ghost" onClick={onDelete} disabled={saving}>
          Delete
        </Button>
      </div>
    </div>
  );
}

function correctionTitle(correction: OwnerCorrection) {
  const trimmed = correction.correction.trim();
  if (trimmed.length <= 72) return trimmed;
  return `${trimmed.slice(0, 69)}...`;
}

function correctionSource(correction: OwnerCorrection) {
  if (correction.call_session_id && correction.transcript_message_id) {
    return "Call transcript";
  }
  if (correction.call_session_id) {
    return "Call session";
  }
  return "Manual correction";
}

function serviceOptionLabel(service: POSService) {
  const details = [service.category_name, service.duration_minutes > 0 ? `${service.duration_minutes} min` : ""].filter(Boolean);
  return details.length > 0 ? `${service.name} · ${details.join(" · ")}` : service.name;
}

async function loadTenantTrainingSalon() {
  const activeSalonID = storedActiveTenantSalonID();
  return activeSalonID
    ? apiRequest<Salon>(`/api/salons/${activeSalonID}`)
    : (await apiRequest<SalonListResponse>("/api/salons")).salons[0] ?? null;
}

async function loadPlatformTrainingSalon(salonID: string): Promise<Salon | null> {
  if (!salonID) return null;
  const surface = { kind: "platform" as const, salonID };
  const [profile, directory] = await Promise.all([
    businessGet<BusinessSalonProfile>(surface, "profile"),
    listBusinessSalons("platform")
  ]);
  const summary = directory.salons.find((item) => item.id === salonID);
  if (!summary) return null;
  return {
    id: profile.id,
    name: profile.name,
    phone: profile.phone,
    address: profile.address,
    city: profile.city,
    state: profile.state,
    zip_code: profile.zip_code,
    timezone: profile.timezone,
    primary_language: profile.primary_language,
    secondary_language: profile.secondary_language,
    handoff_phone: profile.handoff_phone,
    ai_enabled: summary.ai_enabled,
    active_pos_provider: summary.active_pos_provider,
    public_slug: profile.public_slug,
    public_catalog_enabled: profile.public_catalog_enabled,
    scheduling_authority: summary.scheduling_authority,
    scheduling_authority_version: summary.scheduling_authority_version,
    updated_at: profile.updated_at
  };
}

function trainingResourcePath(surface: TrainingSurface, salonID: string, resource: string) {
  const prefix = surface === "platform"
    ? `/api/platform/tenants/${encodeURIComponent(salonID)}`
    : `/api/salons/${encodeURIComponent(salonID)}`;
  return `${prefix}${resource}`;
}

function formatDate(value: string) {
  return new Intl.DateTimeFormat("en-US", {
    month: "short",
    day: "numeric",
    hour: "numeric",
    minute: "2-digit"
  }).format(new Date(value));
}
