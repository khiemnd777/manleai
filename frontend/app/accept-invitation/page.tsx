import type { Metadata } from "next";
import { AcceptOwnerInvitationForm } from "@/features/auth/accept-owner-invitation-form";

export const metadata: Metadata = {
  title: "Activate Owner account | ManleAI",
  robots: { index: false, follow: false },
  referrer: "no-referrer"
};

export default function AcceptInvitationPage() {
  return <main className="flex min-h-screen items-center justify-center bg-slate-50 px-4 py-12"><div className="w-full max-w-md rounded-xl border border-line bg-white p-6 shadow-sm sm:p-8"><AcceptOwnerInvitationForm/></div></main>;
}
