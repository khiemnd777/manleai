import Link from "next/link";
import { CreateAccountForm } from "@/features/auth/create-account-form";
import { Card } from "@/components/ui/card";

export default function CreateAccountPage() {
  return (
    <main className="flex min-h-screen bg-shell">
      <section className="hidden flex-1 items-center justify-center bg-ink px-10 text-white lg:flex">
        <div className="max-w-lg">
          <div className="mb-8 inline-flex rounded-full border border-white/20 px-3 py-1 text-sm text-white/80">
            POS-first pilot for Square Appointments
          </div>
          <h1 className="text-5xl font-bold leading-tight">
            AI Phone Receptionist for Nail Salons
          </h1>
          <p className="mt-5 text-lg leading-8 text-white/72">
            Create the first owner account, then set up the salon profile and connect Square
            Appointments before enabling booking workflows.
          </p>
          <div className="mt-10 grid gap-3 text-sm text-white/80">
            <div className="rounded-md border border-white/15 p-4">
              Booking is confirmed only after Square returns success.
            </div>
            <div className="rounded-md border border-white/15 p-4">
              Owner setup closes after the first account is created.
            </div>
          </div>
        </div>
      </section>
      <section className="flex flex-1 items-center justify-center px-5 py-8">
        <Card className="w-full max-w-md">
          <div className="mb-6">
            <div className="text-sm font-semibold text-brand">Owner account setup</div>
            <h2 className="mt-2 text-2xl font-bold text-ink">Create first owner account</h2>
            <p className="mt-2 text-sm leading-6 text-muted">
              This creates the first dashboard owner and assigns the Salon Owner role.
            </p>
          </div>
          <CreateAccountForm />
          <div className="mt-5 border-t border-line pt-4 text-sm text-muted">
            Already have an account?{" "}
            <Link className="font-semibold text-brand hover:text-teal-800" href="/login">
              Sign in
            </Link>
          </div>
        </Card>
      </section>
    </main>
  );
}
