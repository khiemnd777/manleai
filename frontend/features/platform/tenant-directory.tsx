"use client";

import { useEffect, useMemo, useState } from "react";
import { Building2, ChevronRight, RefreshCcw, Search } from "lucide-react";
import Link from "next/link";
import { Alert } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardDescription, CardTitle } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { isSampleData, listBusinessSalons, type BusinessSalonSummary, type DataClassification } from "@/lib/api/business";

type ClassificationFilter = "all" | DataClassification;

export function PlatformTenantDirectory() {
  const [salons, setSalons] = useState<BusinessSalonSummary[]>([]);
  const [query, setQuery] = useState("");
  const [classification, setClassification] = useState<ClassificationFilter>("all");
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");

  async function load() {
    setLoading(true); setError("");
    try {
      const response = await listBusinessSalons("platform");
      setSalons(response.salons);
    } catch (failure) {
      setError(failure instanceof Error ? failure.message : "Could not load nail salons.");
    } finally { setLoading(false); }
  }
  useEffect(() => { void load(); }, []);
  const filtered = useMemo(() => {
    const needle = query.trim().toLowerCase();
    return salons.filter((salon) => {
      if (classification !== "all" && salon.data_classification !== classification) return false;
      if (!needle) return true;
      return [salon.name, salon.city, salon.state, salon.public_slug].some((value) => value?.toLowerCase().includes(needle));
    });
  }, [classification, query, salons]);

  return (
    <div className="space-y-5">
      <div className="flex flex-col gap-3 sm:flex-row sm:items-end sm:justify-between"><div><h1 className="text-2xl font-bold text-ink">Nail salons</h1><p className="mt-1 text-sm text-muted">Open one salon to manage Business, AI Receptionist, Platform Controls, and History.</p></div><Button type="button" variant="secondary" onClick={() => void load()} disabled={loading}><RefreshCcw className="h-4 w-4" />Refresh</Button></div>
      <Card><div className="grid gap-3 sm:grid-cols-[minmax(0,1fr)_180px]"><label className="block"><span className="text-sm font-semibold text-ink">Search salons</span><div className="mt-2 flex h-11 items-center gap-2 rounded-md border border-line bg-white px-3"><Search className="h-4 w-4 text-muted" /><input className="w-full bg-transparent text-sm outline-none" value={query} onChange={(event) => setQuery(event.target.value)} placeholder="Name, city, state, or public slug" /></div></label><label className="block"><span className="text-sm font-semibold text-ink">Data</span><select className="field mt-2 h-11" value={classification} onChange={(event) => setClassification(event.target.value as ClassificationFilter)}><option value="all">All data</option><option value="live">Live</option><option value="sample_test">Sample test</option></select></label></div></Card>
      {error ? <Alert title="Could not load salons" message={error}><Button type="button" variant="secondary" className="mt-4" onClick={() => void load()}>Retry</Button></Alert> : null}
      {loading ? <div className="space-y-3">{[0,1,2].map((item) => <Skeleton key={item} className="h-24 w-full" />)}</div> : null}
      {!loading && !error && filtered.length === 0 ? <Card className="py-10 text-center"><Building2 className="mx-auto h-7 w-7 text-muted" /><CardTitle className="mt-3">No salons found</CardTitle><CardDescription>{salons.length ? "Try a different search." : "No salon is currently assigned to this Platform account."}</CardDescription></Card> : null}
      {!loading && !error && filtered.length ? <div className="overflow-hidden rounded-lg border border-line bg-white shadow-soft"><div className="hidden grid-cols-[minmax(0,2fr)_minmax(0,1fr)_160px_48px] gap-4 border-b border-line bg-slate-50 px-5 py-3 text-xs font-semibold uppercase tracking-wide text-muted md:grid"><span>Salon</span><span>Location</span><span>Access</span><span /></div>{filtered.map((salon) => <Link key={salon.id} href={`/platform/tenants/${salon.id}/business`} className="grid gap-3 border-b border-line px-5 py-4 transition last:border-0 hover:bg-slate-50 md:grid-cols-[minmax(0,2fr)_minmax(0,1fr)_160px_48px] md:items-center"><div className="min-w-0"><div className="flex min-w-0 items-center gap-2"><div className="truncate font-semibold text-ink">{salon.name}</div>{isSampleData(salon) ? <Badge value="sample_test" /> : null}</div><div className="mt-1 truncate text-xs text-muted">{salon.public_slug ? `/s/${salon.public_slug}` : "Public page not configured"}</div></div><div className="text-sm text-slate-700">{[salon.city, salon.state].filter(Boolean).join(", ") || salon.timezone}</div><div><Badge value={salon.business_access} /></div><ChevronRight className="hidden h-4 w-4 text-muted md:block" /></Link>)}</div> : null}
    </div>
  );
}
