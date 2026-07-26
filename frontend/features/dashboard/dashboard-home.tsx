"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { CalendarDays, CheckCircle2, PhoneCall, Sparkles } from "lucide-react";
import { Alert } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardDescription, CardTitle } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { useTenantSalon } from "@/components/layout/tenant-salon-context";
import { apiRequest } from "@/lib/api/client";
import { businessGet, type BusinessSalonProfile, type BusinessService, type BusinessStaff } from "@/lib/api/business";
import { hasExternalAppointmentConfirmation, hasInternalAppointmentConfirmation } from "@/lib/api/scheduling-evidence";
import type { AppointmentRecord, BookingAttempt, ConversationSession } from "@/types/api";

type SessionsResponse={sessions:ConversationSession[]};
type AppointmentsResponse={appointments:AppointmentRecord[]};
type AttemptsResponse={booking_attempts:BookingAttempt[]};

export function DashboardHome(){
  const tenant=useTenantSalon();
  const[profile,setProfile]=useState<BusinessSalonProfile|null>(null);
  const[sessions,setSessions]=useState<ConversationSession[]>([]);
  const[appointments,setAppointments]=useState<AppointmentRecord[]>([]);
  const[attempts,setAttempts]=useState<BookingAttempt[]>([]);
  const[services,setServices]=useState<BusinessService[]>([]);
  const[staff,setStaff]=useState<BusinessStaff[]>([]);
  const[loading,setLoading]=useState(true);
  const[error,setError]=useState("");
  useEffect(()=>{if(!tenant.activeSalonID){setLoading(false);return}let current=true;const salonID=tenant.activeSalonID;setLoading(true);setError("");Promise.all([
    businessGet<BusinessSalonProfile>({kind:"tenant",salonID},"profile"),
    businessGet<{services:BusinessService[]}>({kind:"tenant",salonID},"services"),
    businessGet<{staff:BusinessStaff[]}>({kind:"tenant",salonID},"staff"),
    apiRequest<SessionsResponse>(`/api/salons/${salonID}/conversation-sessions?limit=25`),
    apiRequest<AppointmentsResponse>(`/api/salons/${salonID}/appointments`),
    apiRequest<AttemptsResponse>(`/api/salons/${salonID}/booking-attempts?limit=50`)
  ]).then(([profileResponse,serviceResponse,staffResponse,sessionResponse,appointmentResponse,attemptResponse])=>{if(!current)return;setProfile(profileResponse);setServices(serviceResponse.services);setStaff(staffResponse.staff);setSessions(sessionResponse.sessions);setAppointments(appointmentResponse.appointments);setAttempts(attemptResponse.booking_attempts)}).catch(failure=>{if(current)setError(failure instanceof Error?failure.message:"Could not load dashboard.")}).finally(()=>{if(current)setLoading(false)});return()=>{current=false}},[tenant.activeSalonID]);
  if(tenant.loading||loading)return <div className="space-y-6"><Skeleton className="h-9 w-72"/><div className="grid gap-4 md:grid-cols-2 xl:grid-cols-4">{Array.from({length:4}).map((_,index)=><Skeleton key={index} className="h-32"/>)}</div><Skeleton className="h-72"/></div>;
  if(tenant.error||error)return <Alert title="Dashboard unavailable" message={tenant.error||error}/>;
  if(!profile)return <Card><CardTitle>No salon workspace</CardTitle><CardDescription>This account has no active salon membership.</CardDescription><div className="mt-5"><Link href="/onboarding"><Button type="button">Create salon profile</Button></Link></div></Card>;
  const phoneCount=sessions.filter(item=>item.channel==="phone").length;
  const confirmed=appointments.filter(item=>hasInternalAppointmentConfirmation(item)||hasExternalAppointmentConfirmation(item)).length;
  const reviewNeeded=attempts.filter(item=>["fallback_pending","provider_pending","pos_pending"].includes(item.status)).length;
  const activeServices=services.filter(item=>item.active&&item.ai_bookable&&!item.archived_at).length;
  const activeStaff=staff.filter(item=>item.active&&item.ai_bookable&&!item.archived_at).length;
  return <div className="space-y-6"><div className="flex flex-col justify-between gap-3 md:flex-row md:items-end"><div><h1 className="text-2xl font-bold text-ink">Business dashboard</h1><p className="mt-1 text-sm text-muted">Calls, appointments, customers, services, and staff for {profile.name}. Technical provider setup is handled by Platform Operations.</p></div><Badge value="business"/></div><div className="grid gap-4 md:grid-cols-2 xl:grid-cols-4"><Metric icon={PhoneCall} label="Phone calls" value={String(phoneCount)} detail="Recent salon call sessions"/><Metric icon={CalendarDays} label="Confirmed appointments" value={String(confirmed)} detail="Durable appointment records"/><Metric icon={CheckCircle2} label="Needs review" value={String(reviewNeeded)} detail="Pending booking follow-up"/><Metric icon={Sparkles} label="Bookable catalog" value={`${activeServices}/${activeStaff}`} detail="Services / staff"/></div><div className="grid gap-4 lg:grid-cols-[1.2fr_0.8fr]"><Card><CardTitle>{profile.name}</CardTitle><CardDescription>{profile.phone} · {[profile.city,profile.state].filter(Boolean).join(", ")||"Location not set"}</CardDescription><dl className="mt-5 grid gap-4 sm:grid-cols-2"><Info label="Timezone" value={profile.timezone}/><Info label="Primary language" value={profile.primary_language.toUpperCase()}/><Info label="Secondary language" value={profile.secondary_language?.toUpperCase()||"Not set"}/><Info label="Handoff phone" value={profile.handoff_phone||"Not set"}/></dl></Card><Card><CardTitle>Business workspace</CardTitle><CardDescription>Tenant users manage the salon’s operating data here. Credentials, adapters, webhooks, worker health, and recovery controls are intentionally absent.</CardDescription><div className="mt-5 flex flex-wrap gap-2"><Link href="/dashboard/appointments"><Button type="button">Appointments</Button></Link><Link href="/dashboard/settings"><Button type="button" variant="secondary">Business settings</Button></Link></div></Card></div></div>;
}

function Metric({icon:Icon,label,value,detail}:{icon:React.ElementType;label:string;value:string;detail:string}){return <Card><div className="flex items-center justify-between"><span className="text-sm font-medium text-muted">{label}</span><Icon className="h-4 w-4 text-brand"/></div><div className="mt-4 text-2xl font-bold text-ink">{value}</div><div className="mt-1 text-xs text-muted">{detail}</div></Card>}
function Info({label,value}:{label:string;value:string}){return <div><dt className="text-xs font-bold uppercase tracking-wide text-muted">{label}</dt><dd className="mt-1 text-sm font-semibold text-ink">{value}</dd></div>}
