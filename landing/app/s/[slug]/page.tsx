import { Clock3, MapPin, Phone, Sparkles, Users } from "lucide-react";
import { notFound } from "next/navigation";
import { getPublicCatalog, PublicApiError } from "@/lib/api";
import type { PublicBusinessHourPeriod, PublicCatalog, PublicSalon, PublicService, PublicStaffMember } from "@/lib/types";

export const dynamic = "force-dynamic";

const dayLabels = ["Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"];
const weekRows = [
  { index: 0, short: "Sun", long: "Sunday" },
  { index: 1, short: "Mon", long: "Monday" },
  { index: 2, short: "Tue", long: "Tuesday" },
  { index: 3, short: "Wed", long: "Wednesday" },
  { index: 4, short: "Thu", long: "Thursday" },
  { index: 5, short: "Fri", long: "Friday" },
  { index: 6, short: "Sat", long: "Saturday" }
];

export default async function PublicSalonPage({ params }: { params: Promise<{ slug: string }> }) {
  const { slug } = await params;
  let catalog: PublicCatalog;
  try {
    catalog = await getPublicCatalog(slug);
  } catch (error) {
    if (error instanceof PublicApiError && error.status === 404) {
      notFound();
    }
    throw error;
  }

  const phoneHref = telHref(catalog.salon.phone);
  const address = formatAddress(catalog.salon);
  const mapHref = address ? `https://www.google.com/maps/search/?api=1&query=${encodeURIComponent(address)}` : "";

  return (
    <main className="min-h-screen bg-shell pb-20 text-ink md:pb-0">
      <SiteHeader salon={catalog.salon} phoneHref={phoneHref} />
      <Hero salon={catalog.salon} address={address} phoneHref={phoneHref} mapHref={mapHref} />

      <div className="mx-auto max-w-7xl space-y-7 px-5 py-7">
        <ServicesSection services={catalog.services} />
        <StaffSection staff={catalog.staff} />
        <HoursSection hours={catalog.hours} phoneHref={phoneHref} timezone={catalog.salon.timezone} />
      </div>

      <footer className="border-t border-line bg-white px-5 py-6">
        <div className="mx-auto flex max-w-7xl flex-col gap-3 text-sm text-muted md:flex-row md:items-center md:justify-between">
          <div>
            <span className="font-semibold text-ink">{catalog.salon.name}</span>
            {address ? <span> - {address}</span> : null}
          </div>
          <div>Call to request an appointment.</div>
        </div>
      </footer>

      <a
        href={phoneHref}
        className="fixed inset-x-4 bottom-4 z-20 inline-flex h-12 items-center justify-center gap-2 rounded-md bg-brand px-4 text-sm font-semibold text-white shadow-soft hover:bg-teal-800 md:hidden"
      >
        <Phone className="h-4 w-4" />
        Call to book
      </a>
    </main>
  );
}

function SiteHeader({ salon, phoneHref }: { salon: PublicSalon; phoneHref: string }) {
  return (
    <header className="sticky top-0 z-30 border-b border-line bg-white/95 px-5 py-4 backdrop-blur">
      <div className="mx-auto flex max-w-7xl items-center justify-between gap-4">
        <a href="#top" className="flex min-w-0 items-center gap-3">
          <div className="flex h-10 w-10 flex-none items-center justify-center rounded-md border border-teal-200 bg-teal-50 text-brand">
            <Sparkles className="h-5 w-5" />
          </div>
          <div className="min-w-0">
            <div className="truncate text-base font-bold text-ink">{salon.name}</div>
            <div className="hidden text-xs text-muted sm:block">Nail services and hours</div>
          </div>
        </a>
        <nav className="hidden items-center gap-7 text-sm font-medium text-ink md:flex">
          <a href="#services" className="hover:text-brand">
            Services
          </a>
          <a href="#staff" className="hover:text-brand">
            Staff
          </a>
          <a href="#hours" className="hover:text-brand">
            Hours
          </a>
        </nav>
        <a
          href={phoneHref}
          className="inline-flex h-10 flex-none items-center justify-center gap-2 rounded-md bg-brand px-4 text-sm font-semibold text-white hover:bg-teal-800"
        >
          <Phone className="h-4 w-4" />
          Call to book
        </a>
      </div>
    </header>
  );
}

function Hero({
  salon,
  address,
  phoneHref,
  mapHref
}: {
  salon: PublicSalon;
  address: string;
  phoneHref: string;
  mapHref: string;
}) {
  return (
    <section id="top" className="relative min-h-[31rem] overflow-hidden bg-white">
      <div className="absolute inset-0 bg-[url('/images/salon-hero.png')] bg-cover bg-center" />
      <div className="absolute inset-0 bg-gradient-to-r from-white via-white/82 to-white/10" />
      <div className="relative mx-auto flex min-h-[31rem] max-w-7xl items-center px-5 py-12">
        <div className="max-w-2xl">
          <h1 className="mt-4 text-4xl font-bold leading-tight text-ink md:text-5xl">{salon.name}</h1>
          <p className="mt-4 max-w-xl text-base leading-7 text-slate-700">
            Nail services, technicians, and salon hours.
          </p>
          {address ? (
            <div className="mt-4 flex items-start gap-2 text-sm text-slate-700">
              <MapPin className="mt-0.5 h-4 w-4 flex-none text-brand" />
              <span>{address}</span>
            </div>
          ) : null}
          <div className="mt-6 flex flex-col gap-3 sm:flex-row">
            <a
              href={phoneHref}
              className="inline-flex h-11 items-center justify-center gap-2 rounded-md bg-brand px-5 text-sm font-semibold text-white hover:bg-teal-800"
            >
              <Phone className="h-4 w-4" />
              Call to book
            </a>
            {mapHref ? (
              <a
                href={mapHref}
                className="inline-flex h-11 items-center justify-center gap-2 rounded-md border border-line bg-white px-5 text-sm font-semibold text-ink hover:bg-slate-50"
              >
                <MapPin className="h-4 w-4" />
                Get directions
              </a>
            ) : null}
          </div>
          <p className="mt-5 max-w-xl text-sm leading-6 text-slate-700">Please call the salon to request an appointment.</p>
        </div>
      </div>
    </section>
  );
}

function ServicesSection({ services }: { services: PublicService[] }) {
  return (
    <section id="services" className="scroll-mt-24">
      <SectionHeader icon={<Sparkles className="h-5 w-5" />} title="Services" action="Choose a service, then call to check openings." />
      {services.length === 0 ? (
        <EmptyPanel title="Services are not listed right now." message="Call the salon for current service options." />
      ) : (
        <div className="mt-4 grid gap-4 md:grid-cols-3">
          {services.map((service) => (
            <article key={service.name} className="rounded-lg border border-line bg-white p-5 shadow-soft">
              <div className="flex items-start justify-between gap-3">
                <h2 className="text-base font-semibold text-ink">{service.name}</h2>
                <div className="flex-none text-sm font-semibold text-ink">{formatPrice(service)}</div>
              </div>
              <p className="mt-3 min-h-12 text-sm leading-6 text-muted">{service.ai_description || service.description || "Call for current service details."}</p>
              <div className="mt-4 flex flex-wrap gap-2 text-xs font-medium text-slate-700">
                <span className="inline-flex items-center gap-1 rounded-md border border-line px-2 py-1">
                  <Clock3 className="h-3.5 w-3.5 text-brand" />
                  {service.duration_minutes} min
                </span>
                <span className="rounded-md border border-line px-2 py-1">{formatPriceDetail(service)}</span>
              </div>
            </article>
          ))}
        </div>
      )}
    </section>
  );
}

function StaffSection({ staff }: { staff: PublicStaffMember[] }) {
  return (
    <section id="staff" className="scroll-mt-24">
      <SectionHeader icon={<Users className="h-5 w-5" />} title="Our technicians" action="Request a technician when you call." />
      {staff.length === 0 ? (
        <EmptyPanel title="Technicians are not listed right now." message="Call the salon to ask who is available today." />
      ) : (
        <div className="mt-4 grid gap-4 md:grid-cols-2">
          {staff.map((member) => (
            <article key={member.name} className="flex items-center gap-4 rounded-lg border border-line bg-white p-4 shadow-soft">
              <div className="flex h-16 w-16 flex-none items-center justify-center rounded-md bg-teal-50 text-lg font-bold text-brand">
                {initials(member.name)}
              </div>
              <div>
                <h2 className="text-base font-semibold text-ink">{member.name}</h2>
              </div>
            </article>
          ))}
        </div>
      )}
    </section>
  );
}

function HoursSection({ hours, phoneHref, timezone }: { hours: PublicBusinessHourPeriod[]; phoneHref: string; timezone: string }) {
  const grouped = groupHours(hours);
  const todayIndex = currentDayIndex(timezone);
  const todayPeriods = grouped.get(todayIndex) ?? [];
  const todayName = dayLabels[todayIndex] ?? "Today";
  const todayHours = todayPeriods.length ? formatPeriods(todayPeriods) : "Closed";

  return (
    <section id="hours" className="scroll-mt-24 rounded-lg border border-line bg-white p-5 shadow-soft">
      <SectionHeader icon={<Clock3 className="h-5 w-5" />} title="Hours" action="Call ahead for holiday hours." />
      {hours.length === 0 ? (
        <div className="mt-4 text-sm leading-6 text-muted">Call the salon for current business hours.</div>
      ) : (
        <div className="mt-4 grid gap-4 lg:grid-cols-[0.8fr_1.2fr]">
          <div className="rounded-md border border-teal-200 bg-teal-50 p-5">
            <div className="flex items-center justify-between gap-3">
              <div className="text-sm font-semibold uppercase tracking-wide text-brand">Today</div>
              <span className="rounded-md border border-teal-200 bg-white px-2.5 py-1 text-xs font-semibold text-brand">
                {todayPeriods.length ? "Open today" : "Closed today"}
              </span>
            </div>
            <div className="mt-4 text-lg font-bold text-ink">{todayName}</div>
            <div className="mt-2 text-base font-semibold text-slate-700">{todayHours}</div>
            <a
              href={phoneHref}
              className="mt-5 inline-flex h-10 items-center justify-center gap-2 rounded-md bg-brand px-4 text-sm font-semibold text-white hover:bg-teal-800"
            >
              <Phone className="h-4 w-4" />
              Call to book
            </a>
          </div>

          <div className="rounded-md border border-line">
            <div className="border-b border-line px-4 py-3 text-sm font-semibold text-ink">Weekly hours</div>
            <div className="divide-y divide-line">
              {weekRows.map((day) => {
                const periods = grouped.get(day.index) ?? [];
                return (
                  <div key={day.index} className="grid grid-cols-[4rem_1fr] gap-3 px-4 py-3 text-sm sm:grid-cols-[6rem_1fr]">
                    <div className="font-semibold text-ink">{day.short}</div>
                    <div className={periods.length ? "text-slate-700" : "text-muted"}>{periods.length ? formatPeriods(periods) : "Closed"}</div>
                  </div>
                );
              })}
            </div>
          </div>
        </div>
      )}
    </section>
  );
}

function SectionHeader({ icon, title, action }: { icon: React.ReactNode; title: string; action: string }) {
  return (
    <div className="flex flex-col justify-between gap-2 sm:flex-row sm:items-center">
      <div className="flex items-center gap-2">
        <div className="flex h-9 w-9 items-center justify-center rounded-md border border-teal-200 bg-teal-50 text-brand">{icon}</div>
        <h2 className="text-2xl font-bold text-ink">{title}</h2>
      </div>
      <div className="text-sm font-medium text-brand">{action}</div>
    </div>
  );
}

function EmptyPanel({ title, message }: { title: string; message: string }) {
  return (
    <div className="mt-4 rounded-lg border border-line bg-white p-5 text-sm shadow-soft">
      <div className="font-semibold text-ink">{title}</div>
      <div className="mt-1 leading-6 text-muted">{message}</div>
    </div>
  );
}

function formatAddress(salon: PublicSalon) {
  return [salon.address, salon.city, salon.state, salon.zip_code].filter(Boolean).join(", ");
}

function telHref(phone: string) {
  return `tel:${phone.replace(/[^\d+]/g, "")}`;
}

function formatPrice(service: PublicService) {
  if (service.price_display) return service.price_display;
  if (typeof service.price_from === "number" && service.price_from > 0) return `$${service.price_from.toFixed(0)}`;
  return "Call";
}

function formatPriceDetail(service: PublicService) {
  if (typeof service.price_from === "number" && service.price_from > 0) return `From $${service.price_from.toFixed(0)}`;
  return "Call for price";
}

function initials(name: string) {
  return name
    .split(" ")
    .filter(Boolean)
    .slice(0, 2)
    .map((part) => part[0]?.toUpperCase())
    .join("");
}

function groupHours(hours: PublicBusinessHourPeriod[]) {
  const grouped = new Map<number, PublicBusinessHourPeriod[]>();
  for (const period of hours) {
    const next = grouped.get(period.day_of_week) ?? [];
    next.push(period);
    grouped.set(
      period.day_of_week,
      next.sort((a, b) => a.start_local_time.localeCompare(b.start_local_time))
    );
  }
  return grouped;
}

function formatPeriods(periods: PublicBusinessHourPeriod[]) {
  return periods.map((period) => `${displayTime(period.start_local_time)} - ${displayTime(period.end_local_time)}`).join(", ");
}

function displayTime(value: string) {
  const [hourValue, minuteValue] = value.slice(0, 5).split(":").map(Number);
  if (Number.isNaN(hourValue) || Number.isNaN(minuteValue)) return value.slice(0, 5);
  const suffix = hourValue >= 12 ? "PM" : "AM";
  const hour = hourValue % 12 || 12;
  return `${hour}:${String(minuteValue).padStart(2, "0")} ${suffix}`;
}

function currentDayIndex(timezone: string) {
  try {
    const weekday = new Intl.DateTimeFormat("en-US", { weekday: "long", timeZone: timezone || "America/Chicago" }).format(new Date());
    const index = dayLabels.indexOf(weekday);
    return index >= 0 ? index : new Date().getDay();
  } catch {
    return new Date().getDay();
  }
}
