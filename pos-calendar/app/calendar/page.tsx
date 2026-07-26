import { POSCalendarClient } from "@/features/calendar/pos-calendar-client";
import { headers } from "next/headers";

export default function CalendarPage() {
  const nonce = headers().get("x-nonce") || "";
  return <POSCalendarClient nonce={nonce} />;
}
