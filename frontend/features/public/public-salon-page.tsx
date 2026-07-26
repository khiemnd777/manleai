"use client";

import { useEffect, useMemo, useState } from "react";
import { CalendarDays, Clock3, MapPin, Phone, Sparkles, Users } from "lucide-react";
import { Alert } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Card, CardDescription, CardTitle } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { apiBaseUrl } from "@/lib/config/env";

type PublicCatalog = {
  salon:{slug:string;name:string;phone:string;address?:string;city?:string;state?:string;zip_code?:string;timezone:string;primary_language:string;secondary_language?:string};
  services:Array<{name:string;description?:string;ai_description?:string;duration_minutes:number;price_from?:number;price_display?:string}>;
  staff:Array<{name:string}>;
  hours:Array<{day_of_week:number;start_local_time:string;end_local_time:string}>;
  booking_note:string;
};

export function PublicSalonPage({ slug }: { slug: string }) {
  const [catalog,setCatalog]=useState<PublicCatalog|null>(null);
  const [loading,setLoading]=useState(true);
  const [notFound,setNotFound]=useState(false);
  const [error,setError]=useState("");
  useEffect(()=>{let current=true;setLoading(true);setError("");setNotFound(false);fetch(`${apiBaseUrl}/api/public/salons/${encodeURIComponent(slug)}`,{headers:{Accept:"application/json"},cache:"no-store"}).then(async response=>{if(response.status===404){if(current)setNotFound(true);return null}if(!response.ok)throw new Error("The salon page is temporarily unavailable.");return response.json() as Promise<PublicCatalog>}).then(value=>{if(current&&value)setCatalog(value)}).catch(failure=>{if(current)setError(failure instanceof Error?failure.message:"The salon page is temporarily unavailable.")}).finally(()=>{if(current)setLoading(false)});return()=>{current=false}},[slug]);
  const hours=useMemo(()=>groupHours(catalog?.hours??[]),[catalog?.hours]);
  if(loading)return <PublicShell><div className="space-y-5"><Skeleton className="h-44"/><Skeleton className="h-72"/></div></PublicShell>;
  if(notFound)return <PublicShell><Alert title="Salon page not found" message="This salon page is not published or is not ready for public booking information."/></PublicShell>;
  if(error||!catalog)return <PublicShell><Alert title="Salon page unavailable" message={error||"The salon page could not be loaded."}/></PublicShell>;
  const location=[catalog.salon.address,catalog.salon.city,catalog.salon.state,catalog.salon.zip_code].filter(Boolean).join(", ");
  return <PublicShell><div className="space-y-8"><section className="overflow-hidden rounded-2xl bg-gradient-to-br from-teal-900 via-teal-800 to-slate-900 px-6 py-10 text-white shadow-soft sm:px-10"><div className="max-w-3xl"><Badge value="published"/><h1 className="mt-4 text-3xl font-black tracking-tight sm:text-5xl">{catalog.salon.name}</h1>{location?<p className="mt-4 flex items-start gap-2 text-sm text-teal-50 sm:text-base"><MapPin className="mt-0.5 h-4 w-4 flex-none"/>{location}</p>:null}<a className="mt-6 inline-flex h-11 items-center gap-2 rounded-lg bg-white px-5 text-sm font-bold text-teal-900" href={`tel:${catalog.salon.phone}`}><Phone className="h-4 w-4"/>Call {catalog.salon.phone}</a></div></section><div className="grid gap-6 lg:grid-cols-[minmax(0,2fr)_minmax(280px,1fr)]"><section><div className="mb-4 flex items-center gap-2"><Sparkles className="h-5 w-5 text-brand"/><h2 className="text-xl font-bold text-ink">Services</h2></div><div className="grid gap-4 sm:grid-cols-2">{catalog.services.map(service=><Card key={`${service.name}-${service.duration_minutes}`}><div className="flex items-start justify-between gap-3"><div><CardTitle>{service.name}</CardTitle><CardDescription>{service.description||service.ai_description||"Contact the salon for details."}</CardDescription></div>{service.price_display?<span className="whitespace-nowrap text-sm font-bold text-brand">{service.price_display}</span>:service.price_from!=null?<span className="whitespace-nowrap text-sm font-bold text-brand">From ${service.price_from.toFixed(2)}</span>:null}</div><p className="mt-4 flex items-center gap-2 text-xs font-semibold text-muted"><Clock3 className="h-4 w-4"/>{service.duration_minutes} minutes</p></Card>)}</div></section><aside className="space-y-4"><Card><div className="flex items-center gap-2"><CalendarDays className="h-5 w-5 text-brand"/><CardTitle>Opening hours</CardTitle></div><div className="mt-4 space-y-2">{hours.map(item=><div key={item.day} className="flex justify-between gap-3 text-sm"><span className="font-semibold text-ink">{item.day}</span><span className="text-right text-muted">{item.value}</span></div>)}</div><p className="mt-4 text-xs text-muted">Times shown in {catalog.salon.timezone}.</p></Card>{catalog.staff.length?<Card><div className="flex items-center gap-2"><Users className="h-5 w-5 text-brand"/><CardTitle>Team</CardTitle></div><div className="mt-4 flex flex-wrap gap-2">{catalog.staff.map(member=><span key={member.name} className="rounded-full bg-slate-100 px-3 py-1 text-sm font-semibold text-slate-700">{member.name}</span>)}</div></Card>:null}<Card className="border-teal-200 bg-teal-50"><CardTitle>Request an appointment</CardTitle><CardDescription>{catalog.booking_note}</CardDescription><a className="mt-4 inline-flex h-10 items-center gap-2 rounded-lg bg-brand px-4 text-sm font-bold text-white" href={`tel:${catalog.salon.phone}`}><Phone className="h-4 w-4"/>Call salon</a></Card></aside></div></div></PublicShell>;
}

function PublicShell({children}:{children:React.ReactNode}){return <main className="min-h-screen bg-shell"><div className="mx-auto max-w-6xl px-5 py-8 sm:py-12">{children}<footer className="mt-12 border-t border-line pt-6 text-center text-xs text-muted">Public salon information is isolated by salon slug and publication readiness.</footer></div></main>}
const days=["Sunday","Monday","Tuesday","Wednesday","Thursday","Friday","Saturday"];
function groupHours(periods:PublicCatalog["hours"]){return days.map((day,index)=>{const values=periods.filter(period=>period.day_of_week===index).map(period=>`${formatTime(period.start_local_time)}–${formatTime(period.end_local_time)}`);return{day,value:values.length?values.join(", "):"Closed"}})}
function formatTime(value:string){const[hours,minutes]=value.split(":").map(Number);const suffix=hours>=12?"PM":"AM";return `${hours%12||12}:${String(minutes||0).padStart(2,"0")} ${suffix}`}
