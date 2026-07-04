import { LoginForm } from "@/features/auth/login-form";
import { Card } from "@/components/ui/card";

export default function LoginPage() {
  return (
    <main className="flex min-h-screen bg-shell">
      <section className="hidden flex-1 items-center justify-center bg-ink px-10 text-white lg:flex">
        <div className="max-w-lg">
          <div className="mb-8 inline-flex rounded-full border border-white/20 px-3 py-1 text-sm text-white/80">
            POS-first production for Square Appointments
          </div>
          <h1 className="text-5xl font-bold leading-tight">
            AI Phone Receptionist for Nail Salons
          </h1>
          <p className="mt-5 text-lg leading-8 text-white/72">
            Answers booking calls, keeps Square as the source of truth, and gives owners a
            clean dashboard for setup, logs, and integration health.
          </p>
          <div className="mt-10 grid gap-3 text-sm text-white/80">
            <div className="rounded-md border border-white/15 p-4">
              Booking is confirmed only after Square returns success.
            </div>
            <div className="rounded-md border border-white/15 p-4">
              Square-specific payloads stay inside the adapter layer.
            </div>
          </div>
        </div>
      </section>
      <section className="flex flex-1 items-center justify-center px-5">
        <Card className="w-full max-w-md">
          <div className="mb-6">
            <div className="text-sm font-semibold text-brand">Owner dashboard</div>
            <h2 className="mt-2 text-2xl font-bold text-ink">Sign in</h2>
            <p className="mt-2 text-sm leading-6 text-muted">
              Use the owner account created for this deployment.
            </p>
          </div>
          <LoginForm />
        </Card>
      </section>
    </main>
  );
}
