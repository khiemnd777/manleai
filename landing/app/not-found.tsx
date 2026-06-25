import Link from "next/link";

export default function NotFound() {
  return (
    <main className="flex min-h-screen items-center justify-center bg-shell px-5">
      <section className="max-w-xl rounded-lg border border-line bg-white p-6 text-center shadow-soft">
        <p className="text-xs font-semibold uppercase tracking-wide text-brand">Page unavailable</p>
        <h1 className="mt-3 text-2xl font-bold text-ink">This salon page is not available.</h1>
        <p className="mt-3 text-sm leading-6 text-muted">Please check the link or call the salon directly.</p>
        <Link
          href="/"
          className="mt-5 inline-flex h-10 items-center justify-center rounded-md border border-line bg-white px-4 text-sm font-semibold text-ink hover:bg-slate-50"
        >
          Back home
        </Link>
      </section>
    </main>
  );
}
